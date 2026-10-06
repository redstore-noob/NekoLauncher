package bindings

// Wallpaper Engine「网页」类壁纸的静态资源路由。
//
// WE 的 web 壁纸是一整个目录（index.html + js/css/贴图/音频），WebView 里只能经
// 应用内 HTTP 路由加载。这条路由不接收任意磁盘路径，只服务"当前选中的那个壁纸
// 项目目录"——目录由 GetWallpaperEngineWallpaper 解析后钉在这里，避免变成任意
// 目录读取通道。
//
//	GET /wwwallpaper/<项目内相对路径>  → 该文件内容
//
// 安全模型与 /plugins 一致：本地单用户桌面应用、无远程访问面，额外做目录包含校验。
// 前端用 sandbox="allow-scripts" 的 iframe 承载（见 BackgroundLayer），壁纸因此拿不到
// 启动器源站的 localStorage 与 window.go 绑定。

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// webWallpaperRoutePrefix 网页壁纸资源路由前缀（应用内路径，非磁盘路径）。
const webWallpaperRoutePrefix = "/wwwallpaper/"

// webWallpaperEntryAlias 入口 HTML 的替身文件名，前端就用它去请求入口。
//
// 为什么不能直接请求 index.html：Wails 的 asset server 会把**所有**以
// "/index.html" 结尾的请求当成"启动器自己的首页"处理（assetserver.go 的
// isRuntimeInjectionMatch），把 /wails/runtime.js 与 IPC 脚本注进文档、
// 并且把整份 HTML 过一遍 HTML 解析器再重新渲染。对壁纸来说这既会塞进一堆
// 用不上的全局脚本，又会改写壁纸自身的标记，解析失败时甚至直接换成错误页。
// 换成这个别名后路径不再命中那条规则，目录层级不变，所以壁纸里的相对路径
// （js/main.js、./style.css）依旧解析到同目录资源。
const webWallpaperEntryAlias = "__entry"

// webWallpaperTarget 当前网页壁纸的项目根目录与入口（相对根目录的斜杠路径）。
type webWallpaperTarget struct {
	Root       string // 主目录（renderDir）：HTML/JS 所在目录
	Entry      string // 入口文件相对路径
	ConfigRoot string // 配置目录（projectDir）：用户选择的项目目录，可能包含配置文件
	// UserProperties 最终用户属性(小写名 → 值,pin 时按
	// 模板默认 < 预设 preset < WE wproperties 合并),入口 HTML 注入用。
	UserProperties map[string]any
}

// webWallpaperCurrent 当前选中的网页壁纸（未选中/非网页类型时为零值）。
var webWallpaperCurrent atomic.Value

// setWebWallpaperTarget 记录当前网页壁纸；root 或 entry 为空时清空。
// configRoot 为配置目录，对于预设型壁纸（有 dependency），它是用户选择的项目目录。
// selectedFile 是 WE 记录的壁纸文件路径(wproperties 匹配键),可为空。
func setWebWallpaperTarget(root, entry, configRoot, selectedFile string) {
	target := webWallpaperTarget{}

	if root != "" && entry != "" {
		if absolute, err := filepath.Abs(root); err == nil {
			target = webWallpaperTarget{Root: absolute, Entry: entry}
			if configRoot != "" && configRoot != root {
				if absConfig, err := filepath.Abs(configRoot); err == nil {
					target.ConfigRoot = absConfig
				}
			}
			// 解析用户属性(模板默认 + 预设值 + WE 手动值),供入口注入
			target.UserProperties = weWebUserProperties(root, target.ConfigRoot, selectedFile)
		}
	}
	webWallpaperCurrent.Store(target)
}

// currentWebWallpaperTarget 读取当前网页壁纸。
func currentWebWallpaperTarget() webWallpaperTarget {
	value, _ := webWallpaperCurrent.Load().(webWallpaperTarget)

	return value
}

// NewWebWallpaperHandler 返回 /wwwallpaper 路由处理器。
func NewWebWallpaperHandler() http.Handler {
	return http.HandlerFunc(serveWebWallpaperFile)
}

// serveWebWallpaperFile 在当前壁纸项目目录内提供单个文件。
func serveWebWallpaperFile(w http.ResponseWriter, r *http.Request) {
	target := currentWebWallpaperTarget()
	if target.Root == "" {
		http.NotFound(w, r)
		return
	}
	relative := strings.Trim(strings.TrimPrefix(r.URL.Path, webWallpaperRoutePrefix), "/")
	if relative == "" {
		// 目录本身也命中 Wails 的首页注入规则，本就不该被请求；直接 404
		http.NotFound(w, r)

		return
	}
	// 先归一化再拼接：Clean 会折叠 ".."，"../../x" 变成 "/x"，仍落在根目录内
	cleaned := strings.TrimPrefix(path.Clean("/"+relative), "/")
	isEntry := false
	// 入口别名 → 真实入口文件（必须同目录，避免别名被拿来跳目录）
	if path.Base(cleaned) == webWallpaperEntryAlias {
		if path.Dir(cleaned) != path.Dir(target.Entry) {
			http.NotFound(w, r)

			return
		}
		cleaned = target.Entry
		isEntry = true
	}
	// file/<路径>:WE file 类型属性(自定义视频/音乐/背景)的重定向目标。
	// 壁纸 JS 把属性值拼成 file:///<值>,polyfill 重写到这里;查找顺序
	// 配置目录(用户项目,预设自带的自定义文件在这里)→ 渲染目录(模板)。
	if strings.HasPrefix(cleaned, "file/") {
		name := strings.TrimPrefix(cleaned, "file/")
		if name == "" {
			http.NotFound(w, r)
			return
		}
		candidates := []string{}
		if target.ConfigRoot != "" {
			candidates = append(candidates, filepath.Join(target.ConfigRoot, filepath.FromSlash(name)))
		}
		candidates = append(candidates, filepath.Join(target.Root, filepath.FromSlash(name)))
		for _, candidate := range candidates {
			if !strings.HasPrefix(candidate, target.Root+string(filepath.Separator)) &&
				!strings.HasPrefix(candidate, target.ConfigRoot+string(filepath.Separator)) {
				continue
			}
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				if contentType, ok := webWallpaperContentType(filepath.Ext(candidate)); ok {
					w.Header().Set("Content-Type", contentType)
				}
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Cache-Control", "no-store")
				http.ServeFile(w, r, candidate)
				return
			}
		}
		http.NotFound(w, r)
		return
	}

	// 尝试从主目录加载文件（HTML/JS 等）
	file := filepath.Join(target.Root, filepath.FromSlash(cleaned))
	if !strings.HasPrefix(file, target.Root+string(filepath.Separator)) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(file)

	// 如果主目录中找不到，且有配置目录，尝试从配置目录加载
	// 这支持预设型壁纸（有 dependency）从用户选择的项目目录加载配置文件。
	// 注意 project.json 保持主目录优先:模板 JS 常用它读 workshopid 做校验,
	// 依赖项目(模板)的那份才是对的;预设的用户取值走注入通道而不是这份文件。
	if (err != nil || info.IsDir()) && target.ConfigRoot != "" && cleaned != "project.json" {
		configFile := filepath.Join(target.ConfigRoot, filepath.FromSlash(cleaned))
		if strings.HasPrefix(configFile, target.ConfigRoot+string(filepath.Separator)) {
			if configInfo, configErr := os.Stat(configFile); configErr == nil && !configInfo.IsDir() {
				file = configFile
				info = configInfo
				err = nil
			}
		}
	}

	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	// 入口 HTML:注入 WE API polyfill + 用户属性,先于壁纸自身脚本执行。
	// 只有 HTML 入口才注入(壁纸内部跳转的子页面不重复注入,幂等保护也在脚本里)。
	if isEntry && weWebEntryIsHTML(cleaned) {
		html, readErr := os.ReadFile(file)
		if readErr == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Cache-Control", "no-store")
			w.Write(injectWebPolyfill(html, weWebPropertiesJSON(target.UserProperties)))
			return
		}
		// 读失败退回普通文件服务
	}
	// HTML(含子页面)从字节直出:http.ServeFile 对 */index.html 会发 301
	// 重定向到目录,而目录路径命中 Wails 首页注入规则,壁纸子页面会被带偏
	if strings.EqualFold(filepath.Ext(file), ".html") || strings.EqualFold(filepath.Ext(file), ".htm") {
		if html, readErr := os.ReadFile(file); readErr == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Cache-Control", "no-store")
			w.Write(html)
			return
		}
	}
	if contentType, ok := webWallpaperContentType(filepath.Ext(file)); ok {
		w.Header().Set("Content-Type", contentType)
	}
	// 壁纸 iframe 的 sandbox 带 allow-scripts 但 origin 可能被特殊化,
	// 对它来说自己的资源可能按跨域处理:ES module(<script type="module">)与
	// fetch() 一律走 CORS,没有这个头就会被浏览器拦掉。这里放行的是用户本机
	// 那个壁纸目录里的静态文件,且路由只服务当前选中的壁纸。
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// 壁纸随时可能在 WE 里被换掉，不做缓存
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, file)
}

// webWallpaperContentType 网页壁纸资源的 Content-Type。
// 除常见前端资源外还要认字体/着色器/音频/3D 模型,否则浏览器会按
// text/plain 拒绝执行(fetch JSON/module 导入会直接抛错)。
func webWallpaperContentType(ext string) (string, bool) {
	if contentType, ok := pluginContentType(ext); ok {
		return contentType, true
	}
	switch strings.ToLower(ext) {
	case ".wasm":
		return "application/wasm", true
	case ".mjs":
		return "application/javascript; charset=utf-8", true
	case ".ttf":
		return "font/ttf", true
	case ".otf":
		return "font/otf", true
	case ".ico":
		return "image/x-icon", true
	case ".cur":
		return "image/x-icon", true
	case ".xml", ".webmanifest":
		return "application/xml; charset=utf-8", true
	case ".vtt", ".srt":
		return "text/vtt; charset=utf-8", true
	case ".obj", ".gltf":
		return "text/plain; charset=utf-8", true
	case ".glb", ".bin":
		return "application/octet-stream", true
	case ".mp3":
		return "audio/mpeg", true
	case ".flac":
		return "audio/flac", true
	case ".m4a", ".aac":
		return "audio/mp4", true
	case ".ogg", ".oga":
		return "audio/ogg", true
	case ".opus":
		return "audio/ogg", true
	case ".wav":
		return "audio/wav", true
	case ".mp4":
		return "video/mp4", true
	case ".webm":
		return "video/webm", true
	case ".glsl", ".frag", ".vert":
		return "text/plain; charset=utf-8", true
	default:
		return "", false
	}
}
