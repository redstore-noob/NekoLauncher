package bindings

// Wallpaper Engine「场景」壁纸的完整渲染支持:资源路由与渲染载荷。
//
// 场景壁纸把所有资源打进 scene.pkg(格式见 wallpaper_engine_pkg.go),本文件做三件事:
//
//  1. 钉定当前场景壁纸的目标目录(与 webwallpaper_handler.go 同一套思路):
//     Root 是真正含 scene.pkg 的"渲染目录",ConfigRoot 是用户选中项目的目录——
//     预设型壁纸(带 dependency)两者不同:贴图在依赖项目里,配置在用户项目里;
//  2. /wescene 资源路由:
//     /wescene/scene.json            场景描述(优先渲染目录,其次配置目录)
//     /wescene/pkg/<路径>            包内原始文件(粒子/效果/材质/模型 JSON、字体)
//     /wescene/tex/<路径>            .tex 解码产物(PNG/JPEG 直出,MP4 视频纹理透传),
//                                     按包+条目+修改时间落盘缓存,换壁纸自动失效
//  3. 渲染载荷组装:设计分辨率 + objects 原样透传 + 用户属性(默认值与
//     WE config.json 里用户实际选择的值合并),前端据此渲染并求值
//     visible/颜色/滑杆的用户绑定。
//
// 安全模型与 /wwwallpaper 一致:路由只服务"当前选中的壁纸"目录,路径做包含校验,
// 不接受任意磁盘路径。

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"nekolauncher/internal/config"
	"nekolauncher/internal/tools"
)

// sceneRoutePrefix 场景壁纸资源路由前缀。
const sceneRoutePrefix = "/wescene/"

// sceneTarget 当前选中的场景壁纸目标。
type sceneTarget struct {
	Root       string // 渲染目录:含 scene.pkg(依赖项目)
	ConfigRoot string // 配置目录:用户选中的项目(预设型壁纸与 Root 不同)
}

// sceneCurrent 当前场景壁纸(atomic,轮询线程写、HTTP 路由读)。
var sceneCurrent atomic.Value

func setSceneTarget(root, configRoot string) {
	target := sceneTarget{}
	if root != "" {
		if absolute, err := filepath.Abs(root); err == nil {
			target.Root = absolute
			if configRoot != "" && configRoot != root {
				if absConfig, err := filepath.Abs(configRoot); err == nil {
					target.ConfigRoot = absConfig
				}
			}
		}
	}
	sceneCurrent.Store(target)
}

func currentSceneTarget() sceneTarget {
	value, _ := sceneCurrent.Load().(sceneTarget)
	return value
}

// ---- 渲染载荷 ----

// WallpaperEngineScene 场景壁纸渲染载荷:前端 SceneWallpaperRenderer 的全部输入。
type WallpaperEngineScene struct {
	// Entry 场景描述入口(应用内 URL,前端直接 fetch)。
	Entry string `json:"Entry"`
	// DesignWidth/DesignHeight 设计分辨率(scene.json general.orthogonalprojection,
	// 缺省 1920×1080)。场景坐标系以它为基准,渲染时等比铺满窗口。
	DesignWidth  int `json:"DesignWidth"`
	DesignHeight int `json:"DesignHeight"`
	// Objects scene.json 的 objects 数组原样透传(结构由前端类型定义解释)。
	Objects json.RawMessage `json:"Objects"`
	// GeneralProperties project.json 的 general.properties 用户属性,
	// value 已并入 WE 记录的用户实际取值(wproperties)。
	GeneralProperties map[string]weUserProperty `json:"GeneralProperties"`
}

// weUserProperty 用户属性(schema + 已解析的当前值)。
type weUserProperty struct {
	Type  string `json:"type"`
	Value any    `json:"value"` // bool / "r g b" 颜色字符串 / 数字 / 文本 / combo 选项
	Text  string `json:"text"`  // 显示名(调试用)
}

// weSceneFilePayload scene.json 里前端需要的顶层字段。
type weSceneFilePayload struct {
	General struct {
		OrthogonalProjection struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"orthogonalprojection"`
	} `json:"general"`
	Objects json.RawMessage `json:"objects"`
}

// weBuildScenePayload 组装场景渲染载荷;renderDir 必须含 scene.pkg,失败返回 nil。
// configDir 为用户选中项目目录(预设型壁纸与 renderDir 不同),可传空。
// selectedFile 是 WE config 里记录的壁纸文件路径(用于 wproperties 匹配),可传空。
func weBuildScenePayload(renderDir, configDir, selectedFile string) *WallpaperEngineScene {
	reader, err := wePkgOpenCached(filepath.Join(renderDir, "scene.pkg"))
	if err != nil {
		return nil
	}
	entry := reader.lookup("scene.json")
	if entry == nil {
		return nil
	}
	sceneData, err := reader.readEntry(entry)
	if err != nil {
		return nil
	}
	var scene weSceneFilePayload
	if err := json.Unmarshal(sceneData, &scene); err != nil {
		return nil
	}
	if len(scene.Objects) == 0 {
		return nil
	}

	payload := &WallpaperEngineScene{
		Entry:             sceneRoutePrefix + "scene.json",
		DesignWidth:       scene.General.OrthogonalProjection.Width,
		DesignHeight:      scene.General.OrthogonalProjection.Height,
		Objects:           weInjectPuppets(reader, scene.Objects),
		GeneralProperties: map[string]weUserProperty{},
	}
	if payload.DesignWidth <= 0 || payload.DesignHeight <= 0 {
		payload.DesignWidth, payload.DesignHeight = 1920, 1080
	}

	// 用户属性:项目 schema(用户项目优先于依赖项目) + WE 记录的用户取值
	properties := weProjectProperties(configDir)
	for name, value := range weProjectProperties(renderDir) {
		if _, exists := properties[name]; !exists {
			properties[name] = value
		}
	}
	userValues := weWallpaperUserValues(selectedFile)
	for name, schema := range properties {
		value := schema.value
		if override, ok := userValues[name]; ok {
			value = override
		}
		payload.GeneralProperties[name] = weUserProperty{
			Type: schema.propertyType, Value: value, Text: schema.text,
		}
	}
	return payload
}

// weProjectProperty 用户属性 schema 条目。
type weProjectProperty struct {
	propertyType string
	text         string
	value        any
}

// weProjectProperties 读项目 project.json 的 general.properties。
func weProjectProperties(projectDir string) map[string]weProjectProperty {
	out := map[string]weProjectProperty{}
	if projectDir == "" {
		return out
	}
	data, err := os.ReadFile(filepath.Join(projectDir, "project.json"))
	if err != nil {
		return out
	}
	var project struct {
		General struct {
			Properties map[string]struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				Value any    `json:"value"`
			} `json:"properties"`
		} `json:"general"`
	}
	if err := json.Unmarshal(data, &project); err != nil {
		return out
	}
	for name, property := range project.General.Properties {
		out[strings.ToLower(name)] = weProjectProperty{
			propertyType: property.Type,
			text:         property.Text,
			value:        property.Value,
		}
	}
	return out
}

// weWallpaperUserValues 读 WE config.json 里用户对指定壁纸实际调整过的属性值。
// WE 只记录与默认不同的项;键为壁纸文件路径,值按显示器分层。
func weWallpaperUserValues(selectedFile string) map[string]any {
	if selectedFile == "" {
		return nil
	}
	configPath := wallpaperEngineConfigPath()
	if configPath == "" {
		return nil
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	return weUserValuesFromConfig(data, selectedFile)
}

// weUserValuesFromConfig 从 WE config.json 字节里取指定壁纸的用户属性取值。
// 独立成纯函数方便单测:路径匹配对斜杠方向与大小写不敏感。
func weUserValuesFromConfig(data []byte, selectedFile string) map[string]any {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil
	}
	normalized := strings.ToLower(strings.ReplaceAll(selectedFile, "\\", "/"))
	for _, section := range root {
		sectionMap, ok := section.(map[string]any)
		if !ok {
			continue
		}
		wproperties, _ := sectionMap["wproperties"].(map[string]any)
		for file, monitors := range wproperties {
			if strings.ToLower(strings.ReplaceAll(file, "\\", "/")) != normalized {
				continue
			}
			monitorsMap, _ := monitors.(map[string]any)
			monitorsSorted := make([]string, 0, len(monitorsMap))
			for monitorName := range monitorsMap {
				monitorsSorted = append(monitorsSorted, monitorName)
			}
			sort.Strings(monitorsSorted) // Monitor0 优先
			for _, monitorName := range monitorsSorted {
				if values, _ := monitorsMap[monitorName].(map[string]any); values != nil {
					return values
				}
			}
		}
	}
	return nil
}

// BuildSceneWallpaperForHarness 渲染 harness(开发工具)用的入口:
// 按指定项目目录组装壁纸信息(与 GetWallpaperEngineWallpaper 同一条路径,
// 但不读 WE 当前选择,便于对任意壁纸做渲染回归)。scene/web 类型都会
// pin 对应的资源路由目标。不属于正式 API。
func BuildSceneWallpaperForHarness(projectDir string) (*WallpaperEngineWallpaper, error) {
	return wallpaperEngineWallpaperFromProject(projectDir, "")
}

// ---- 资源路由 ----

// NewSceneWallpaperHandler 返回 /wescene 路由处理器。
func NewSceneWallpaperHandler() http.Handler {
	return http.HandlerFunc(serveSceneWallpaper)
}

func serveSceneWallpaper(w http.ResponseWriter, r *http.Request) {
	target := currentSceneTarget()
	if target.Root == "" {
		http.NotFound(w, r)
		return
	}
	relative := strings.Trim(strings.TrimPrefix(r.URL.Path, sceneRoutePrefix), "/")
	if relative == "" {
		http.NotFound(w, r)
		return
	}
	// 归一化并挡掉越界(与 /wwwallpaper 同一套防御)
	cleaned := strings.TrimPrefix(path.Clean("/"+relative), "/")
	if cleaned == "" || strings.Contains(cleaned, "..") {
		http.NotFound(w, r)
		return
	}

	switch {
	case cleaned == "scene.json":
		serveSceneJSON(w, r, target)
	case cleaned == "diag":
		if r.Method == http.MethodPost {
			NewSceneDiagHandler().ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	case strings.HasPrefix(cleaned, "pkg/"):
		serveScenePkgFile(w, r, strings.TrimPrefix(cleaned, "pkg/"), target)
	case strings.HasPrefix(cleaned, "tex/"):
		serveSceneTexture(w, r, strings.TrimPrefix(cleaned, "tex/"), target)
	default:
		http.NotFound(w, r)
	}
}

// sceneReaders 依次尝试渲染目录与配置目录的 pkg。
func sceneReaders(target sceneTarget) []*wePkgReader {
	readers := []*wePkgReader{}
	if reader, err := wePkgOpenCached(filepath.Join(target.Root, "scene.pkg")); err == nil {
		readers = append(readers, reader)
	}
	if target.ConfigRoot != "" {
		if reader, err := wePkgOpenCached(filepath.Join(target.ConfigRoot, "scene.pkg")); err == nil {
			readers = append(readers, reader)
		}
	}
	return readers
}

func serveSceneJSON(w http.ResponseWriter, r *http.Request, target sceneTarget) {
	for _, reader := range sceneReaders(target) {
		if entry := reader.lookup("scene.json"); entry != nil {
			if data, err := reader.readEntry(entry); err == nil {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				w.Write(data)
				return
			}
		}
	}
	http.NotFound(w, r)
}

// serveScenePkgFile 提供包内原始文件(JSON/字体等)。
func serveScenePkgFile(w http.ResponseWriter, r *http.Request, name string, target sceneTarget) {
	name = strings.ReplaceAll(name, "\\", "/")
	for _, reader := range sceneReaders(target) {
		if entry := reader.lookup(name); entry != nil {
			data, err := reader.readEntry(entry)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			if contentType, ok := sceneFileContentType(path.Ext(name)); ok {
				w.Header().Set("Content-Type", contentType)
			}
			// 壁纸可能随时被 WE 换掉,不做长缓存
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Write(data)
			return
		}
	}
	http.NotFound(w, r)
}

// sceneFileContentType 包内文件的 Content-Type(复用 web 壁纸的映射并补充场景特有格式)。
func sceneFileContentType(ext string) (string, bool) {
	switch strings.ToLower(ext) {
	case ".json":
		return "application/json; charset=utf-8", true
	case ".ttf":
		return "font/ttf", true
	case ".otf":
		return "font/otf", true
	case ".frag", ".vert", ".hlsl":
		return "text/plain; charset=utf-8", true
	default:
		return webWallpaperContentType(ext)
	}
}

// sceneTextureExts 缓存探测/落盘用的扩展名顺序。
var sceneTextureExts = []string{".png", ".jpg", ".mp4"}

// serveSceneTexture 解码并提供 .tex;带磁盘缓存(包/条目/修改时间任一变化即失效)。
func serveSceneTexture(w http.ResponseWriter, r *http.Request, name string, target sceneTarget) {
	name = strings.ReplaceAll(name, "\\", "/")
	for _, reader := range sceneReaders(target) {
		entry := reader.lookup(name)
		if entry == nil {
			continue
		}
		// 命中缓存:扩展名未知,三种都探一遍
		if cacheBase := weTextureCacheBase(reader.path, name); cacheBase != "" {
			for _, ext := range sceneTextureExts {
				cached := cacheBase + ext
				if tools.FileExists(cached) {
					writeTextureFile(w, r, cached)
					return
				}
			}
			if data, mime, ok := weDecodeTexCached(cacheBase, reader, entry); ok {
				w.Header().Set("Content-Type", mime)
				w.Header().Set("Cache-Control", "max-age=300")
				w.Write(data)
				return
			}
		}
		payload, err := reader.readEntry(entry)
		if err != nil {
			continue
		}
		decoded, err := weDecodeTex(payload)
		if err != nil {
			http.Error(w, "纹理解码失败", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", decoded.Mime)
		w.Header().Set("Cache-Control", "no-store")
		w.Write(decoded.ImageData)
		return
	}
	http.NotFound(w, r)
}

// weDecodeTexCached 解码并写入缓存后返回内容;缓存不可用时退回仅解码。
func weDecodeTexCached(cacheBase string, reader *wePkgReader, entry *wePkgEntry) ([]byte, string, bool) {
	payload, err := reader.readEntry(entry)
	if err != nil {
		return nil, "", false
	}
	decoded, err := weDecodeTex(payload)
	if err != nil {
		return nil, "", false
	}
	ext := ".png"
	switch decoded.Mime {
	case "image/jpeg":
		ext = ".jpg"
	case "video/mp4":
		ext = ".mp4"
	}
	if weWriteCacheFile(cacheBase+ext, decoded.ImageData) != nil {
		// 缓存写失败不影响本次响应
		_ = ext
	}
	return decoded.ImageData, decoded.Mime, true
}

// writeTextureFile 从缓存文件回写响应并按扩展名给 Content-Type。
func writeTextureFile(w http.ResponseWriter, r *http.Request, filePath string) {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".mp4":
		w.Header().Set("Content-Type", "video/mp4")
	}
	// 同一壁纸会话内可复用;换壁纸后键变化,天然失效
	w.Header().Set("Cache-Control", "max-age=300")
	http.ServeFile(w, r, filePath)
}

// weTextureCacheBase 贴图缓存路径(不带扩展名,由解码结果决定)。
func weTextureCacheBase(pkgPath, entryPath string) string {
	info, err := os.Stat(pkgPath)
	if err != nil {
		return ""
	}
	digest := sha1.Sum([]byte(fmt.Sprintf("%s|%s|%d|%d",
		strings.ToLower(pkgPath), strings.ToLower(entryPath),
		info.ModTime().UnixNano(), info.Size())))
	return filepath.Join(config.StorageDirectory(), "cache", "wallpaper-engine", "tex",
		hex.EncodeToString(digest[:]))
}

// weWriteCacheFile 原子写缓存文件(临时文件 + 改名)。
func weWriteCacheFile(filePath string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	tmp := filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filePath)
}
