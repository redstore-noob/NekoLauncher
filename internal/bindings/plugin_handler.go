package bindings

// 插件资源路由：把存储目录下 plugins/ 里的文件以 HTTP 形式暴露给 WebView，供前端在
// 运行时动态 import 插件入口。
//
//	GET /plugins/                → {"plugins":["<id>", ...]}（只列含 plugin.yaml 的目录）
//	GET /plugins/<id>/<相对路径>  → 该文件内容
//
// 安全模型与 /localfile 相同：本地单用户桌面应用，没有远程访问面。这里额外把路径限定在
// plugins 目录内，避免这条路由变成任意文件读取通道。

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"nekolauncher/internal/config"
)

// pluginRoutePrefix 插件资源路由前缀（应用内路径，非磁盘路径）。
const pluginRoutePrefix = "/plugins/"

// pluginManifestName 插件清单文件名；只有带它的目录才算插件。
const pluginManifestName = "plugin.yaml"

// pluginIconName 插件图标缺省文件名（清单可用 icon 字段改写）。
const pluginIconName = "icon.png"

// pluginRootDirectory 插件根目录：存储目录下的 plugins/。
func pluginRootDirectory() string {
	return filepath.Join(config.StorageDirectory(), "plugins")
}

// NewPluginHandler 返回 /plugins 路由处理器。
func NewPluginHandler() http.Handler {
	return newPluginHandler(pluginRootDirectory())
}

// newPluginHandler 以指定根目录构造处理器；根目录参数只为便于测试注入。
func newPluginHandler(root string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relative := strings.Trim(strings.TrimPrefix(r.URL.Path, pluginRoutePrefix), "/")
		if relative == "" {
			writePluginIndex(w, root)
			return
		}
		servePluginFile(w, r, root, relative)
	})
}

// writePluginIndex 列出插件目录。目录不存在（还没装过插件）时返回空列表而不是错误。
// 载荷直接携带**规范化后的清单**（YAML 已在宿主侧解析校验）：前端加载器只见 JSON、
// 不用自己拉原始清单文件，一次请求拿全所有插件。
func writePluginIndex(w http.ResponseWriter, root string) {
	disabled := pluginDisabledIDs()
	manifests := make([]pluginManifest, 0, 8)
	disabledIDs := make([]string, 0, 4)
	if entries, err := os.ReadDir(root); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			manifest, err := readPluginManifest(filepath.Join(root, entry.Name()))
			if err != nil {
				// 清单缺失/非法的目录不算插件（管理页会把它列为"装坏了"）
				continue
			}
			// 索引只带规范化后的样式文件列表（相对路径、限 .css），前端拿来即用
			manifest.Styles = manifest.styleFiles()
			manifests = append(manifests, *manifest)
			if disabled[manifest.ID] {
				disabledIDs = append(disabledIDs, manifest.ID)
			}
		}
	}
	sort.Slice(manifests, func(left, right int) bool {
		return manifests[left].ID < manifests[right].ID
	})
	sort.Strings(disabledIDs)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"plugins":  manifests,
		"disabled": disabledIDs,
	}); err != nil {
		return
	}
}

// servePluginFile 在 plugins 目录内提供单个文件。
func servePluginFile(w http.ResponseWriter, r *http.Request, root, relative string) {
	// 先归一化再拼接：Clean 会把 ".." 折叠掉，"../../x" 变成 "/x"，落在 plugins 内
	cleaned := strings.TrimPrefix(path.Clean("/"+relative), "/")

	rootAbsolute, rootErr := filepath.Abs(root)
	target, targetErr := filepath.Abs(filepath.Join(rootAbsolute, filepath.FromSlash(cleaned)))
	// Clean 之后理论上不会越界，这里再确认一次，防止符号链接等意外情况
	if rootErr != nil || targetErr != nil ||
		!strings.HasPrefix(target, rootAbsolute+string(filepath.Separator)) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	if contentType, ok := pluginContentType(filepath.Ext(target)); ok {
		w.Header().Set("Content-Type", contentType)
	}
	// 插件是本地文件、随时可能被作者改动，不做缓存
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, target)
}

// pluginContentType 插件资源常见扩展名的 Content-Type。
// 入口模块必须返回可执行的 JS MIME，否则浏览器拒绝 import()。
func pluginContentType(ext string) (string, bool) {
	switch strings.ToLower(ext) {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8", true
	case ".json", ".map":
		return "application/json; charset=utf-8", true
	case ".yaml", ".yml":
		return "application/yaml; charset=utf-8", true
	case ".css":
		return "text/css; charset=utf-8", true
	case ".html":
		return "text/html; charset=utf-8", true
	case ".svg":
		return "image/svg+xml", true
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	case ".woff":
		return "font/woff", true
	case ".woff2":
		return "font/woff2", true
	default:
		return "", false
	}
}

// NewAssetFallbackHandler 组合内嵌资源未命中时的回退路由：
// /localfile 走本地音频/图片/视频流，/plugins/ 走插件资源，
// /wwwallpaper/ 走当前 Wallpaper Engine 网页壁纸的资源，其余 404。
func NewAssetFallbackHandler() http.Handler {
	localFileHandler := NewLocalFileHandler()
	pluginHandler := NewPluginHandler()
	webWallpaperHandler := NewWebWallpaperHandler()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == localFileRoute:
			localFileHandler.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, pluginRoutePrefix):
			pluginHandler.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, webWallpaperRoutePrefix):
			webWallpaperHandler.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}
