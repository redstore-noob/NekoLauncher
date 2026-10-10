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

// WallpaperEngineScene 场景壁纸渲染载荷。
//
// 自研 THREE.js 渲染器已移除,场景画面完全交给 WebWallGL(前端动态 import),
// 所以这里只留它真正需要的东西:资源基址、设计分辨率、用户属性覆盖表。
// scene.pkg 的解析、贴图解码、puppet 骨骼、脚本沙箱全在库内部完成。
type WallpaperEngineScene struct {
	// Base WebWallGL 的 httpSource 基址(带会话指纹的 /wescene 前缀)。
	// 前端把它交给 httpSource:scene.pkg 原始字节、project.json、松散工程
	// 文件都从这个前缀下取(见 sceneBaseToken)。
	Base string `json:"Base"`
	// DesignWidth/DesignHeight 设计分辨率(scene.json general.orthogonalprojection,
	// 缺省 1920×1080)。库自己也会读;这里带出来供宿主展示与兜底。
	DesignWidth  int `json:"DesignWidth"`
	DesignHeight int `json:"DesignHeight"`
	// Properties 用户属性的「原始键名 → 当前值」扁平表,直接喂给
	// WebWallGL 的 MountOptions.properties:WE 的 applyUserProperties 与
	// scene.json 里的 property 引用都按 schema 原始大小写匹配。
	Properties map[string]any `json:"Properties"`
}

// weSceneFilePayload scene.json 里宿主需要的顶层字段。
type weSceneFilePayload struct {
	General struct {
		OrthogonalProjection struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"orthogonalprojection"`
	} `json:"general"`
}

// weBuildScenePayload 组装场景渲染载荷:项目里有 scene.json(包内或松散目录)
// 就算可用,失败返回 nil(调用方回落到静态提取图)。
//
// configDir 为用户选中项目目录(预设型壁纸与 renderDir 不同),可传空。
// selectedFile 是 WE config 里记录的壁纸文件路径(用于 wproperties 匹配),可传空。
func weBuildScenePayload(renderDir, configDir, selectedFile string) *WallpaperEngineScene {
	properties := weScenePropertyOverrides(renderDir, configDir, selectedFile)
	scene := weSceneFileDimensions(renderDir, configDir)

	// 既没有项目属性表、也没有 scene.json(包内外都找不到)时视为"不是可渲染的
	// 场景":调用方据此回落到静态提取图,而不是挂一个必然空白的渲染器
	if scene == nil && len(properties) == 0 {
		return nil
	}

	payload := &WallpaperEngineScene{
		Base:         sceneRoutePrefix + "v/" + sceneBaseToken(renderDir, configDir, selectedFile),
		DesignWidth:  1920,
		DesignHeight: 1080,
		Properties:   properties,
	}
	if scene != nil && scene.General.OrthogonalProjection.Width > 0 &&
		scene.General.OrthogonalProjection.Height > 0 {
		payload.DesignWidth = scene.General.OrthogonalProjection.Width
		payload.DesignHeight = scene.General.OrthogonalProjection.Height
	}
	return payload
}

// weSceneFileDimensions 读 scene.json 的设计分辨率:优先包内条目,其次松散目录。
// scene.json 本身缺失时返回 nil(不视为错误——松散工程可能改名)。
func weSceneFileDimensions(renderDir, configDir string) *weSceneFilePayload {
	for _, dir := range []string{renderDir, configDir} {
		if dir == "" {
			continue
		}
		if reader, err := wePkgOpenCached(filepath.Join(dir, "scene.pkg")); err == nil {
			if entry := reader.lookup("scene.json"); entry != nil {
				if data, err := reader.readEntry(entry); err == nil {
					var scene weSceneFilePayload
					if json.Unmarshal(data, &scene) == nil {
						return &scene
					}
				}
			}
		}
		if data, err := os.ReadFile(filepath.Join(dir, "scene.json")); err == nil {
			var scene weSceneFilePayload
			if json.Unmarshal(data, &scene) == nil {
				return &scene
			}
		}
	}
	return nil
}

// weProjectProperty 用户属性 schema 条目。
type weProjectProperty struct {
	propertyType string
	text         string
	value        any
}

// weProjectPropertiesRaw 读 project.json 的 general.properties(保留 schema 原始键名)。
// 场景覆盖表(WebWallGL 的 properties)必须用原始大小写:scene.json 里的
// property 引用与 WE 的 applyUserProperties 都按原名匹配。
func weProjectPropertiesRaw(projectDir string) map[string]weProjectProperty {
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
		out[name] = weProjectProperty{
			propertyType: property.Type,
			text:         property.Text,
			value:        property.Value,
		}
	}
	return out
}

// weScenePropertyOverrides 组装「属性名(原始大小写) → 当前值」覆盖表。
//
// 与 GeneralProperties 的区别:那张表是给自研渲染器用的(键小写归一),
// 这张表直接交给 WebWallGL 的 MountOptions.properties,必须保留 schema 原名,
// 否则壁纸读不到用户值、全部回落默认值。
//
// 取值优先级(与 WEB 壁纸 polyfill 同一套):
//  1. 渲染目录(依赖项目)project.json 的默认值;
//  2. 用户项目(预设)project.json 覆盖 —— 预设作者定制的值;
//  3. WE config.json 里用户后来手动改过的值。
func weScenePropertyOverrides(renderDir, configDir, selectedFile string) map[string]any {
	out := map[string]any{}
	schema := map[string]string{} // 小写 → 原始键

	for name, property := range weProjectPropertiesRaw(renderDir) {
		out[name] = property.value
		schema[strings.ToLower(name)] = name
	}
	canonical := func(name string) string {
		if original, ok := schema[strings.ToLower(name)]; ok {
			return original
		}
		return name
	}

	if configDir != "" && !strings.EqualFold(configDir, renderDir) {
		for name, property := range weProjectPropertiesRaw(configDir) {
			if property.value == nil {
				continue
			}
			out[canonical(name)] = property.value
		}
	}
	for name, value := range weWallpaperUserValues(selectedFile) {
		if value == nil {
			continue
		}
		out[canonical(name)] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sceneBaseToken 场景资源基址的会话指纹。
//
// WebWallGL 的 httpSource 用基址字符串当缓存键(scene.pkg 解析结果、松散
// 目录读取都按它缓存),换壁纸后基址不变就会复用上一张的解析结果——把
// 渲染目录/配置目录/选中文件与 scene.pkg 的修改时间、大小搅进指纹里,
// 换壁纸(或包被 WE 重写)后基址必然变化,缓存自然失效。
func sceneBaseToken(renderDir, configDir, selectedFile string) string {
	info, err := os.Stat(filepath.Join(renderDir, "scene.pkg"))
	if err != nil {
		// 无 scene.pkg(松散工程/纯预览)时仍然给一个稳定但随目录变化的键
		digest := sha1.Sum([]byte(strings.ToLower(renderDir + "|" + configDir + "|" + selectedFile)))

		return hex.EncodeToString(digest[:8])
	}
	digest := sha1.Sum([]byte(fmt.Sprintf("%s|%s|%s|%d|%d",
		strings.ToLower(renderDir), strings.ToLower(configDir), strings.ToLower(selectedFile),
		info.ModTime().UnixNano(), info.Size())))

	return hex.EncodeToString(digest[:8])
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
	// 去会话指纹前缀:前端拿到的基址是 /wescene/v/<token>/,WebWallGL 会在
	// 它下面请求 project.json / scene.pkg / 松散文件,这里剥掉再按正常路由分派
	if strings.HasPrefix(relative, "v/") {
		if slash := strings.IndexByte(relative[len("v/"):], '/'); slash >= 0 {
			relative = relative[len("v/")+slash+1:]
		} else {
			relative = ""
		}
	}
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
	case cleaned == "project.json" || cleaned == "scene.pkg" ||
		strings.HasPrefix(cleaned, "scenes/") || cleaned == "gifscene.pkg":
		// WebWallGL 的 httpSource 请求路径:project.json(属性表)、
		// scene.pkg(原始包字节)、scenes/scene.pkg 变体
		if cleaned == "project.json" {
			serveSceneProjectJSON(w, r, target)
			return
		}
		serveScenePkgRaw(w, r, cleaned, target)
	default:
		// 松散工程(WE 工程目录直接放着 scene.json 与贴图,没有 scene.pkg):
		// WebWallGL 声明了 sceneDir 时按名取文件,这里按渲染目录→配置目录找。
		// 包型壁纸的贴图/粒子/材质也走这条路(包内条目直出原始字节,由库解码)。
		serveSceneLooseFile(w, r, cleaned, target)
	}
}

// serveSceneProjectJSON 提供场景壁纸的 project.json。
//
// WebWallGL 用它判定场景形态(project.file 是 .json 且文件存在 → 松散工程,
// 否则按 scene.pkg 走)并读属性表。预设型壁纸的 project.json 在用户项目目录里、
// 包在依赖目录里,所以这里做一次合成:以渲染目录为底,配置目录补上缺的顶层字段
// (尤其 file/type),保证 WE 的两种目录布局都能被正确判定。
func serveSceneProjectJSON(w http.ResponseWriter, r *http.Request, target sceneTarget) {
	merged := map[string]any{}
	for _, dir := range []string{target.Root, target.ConfigRoot} {
		if dir == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, "project.json"))
		if err != nil {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal(data, &fields); err != nil {
			continue
		}
		// 配置目录(用户项目)的同名字段覆盖渲染目录:预设作者定制的值优先
		for key, value := range fields {
			merged[key] = value
		}
	}
	if len(merged) == 0 {
		// 没有 project.json:给一个最小形态,让 WebWallGL 走 scene.pkg 路径。
		// 缺了它 httpSource 拿不到 file,松散判定会误判、属性表也会丢。
		merged = map[string]any{"file": "scene.json", "type": "scene"}
	}
	if _, ok := merged["file"]; !ok {
		merged["file"] = "scene.json"
	}
	if _, ok := merged["type"]; !ok {
		merged["type"] = "scene"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(merged)
}

// serveScenePkgRaw 提供 scene.pkg 原始字节(WebWallGL 自己解析包格式)。
// 与自研渲染器的 /wescene/pkg/ 不同:那条路给的是解包后的单个条目。
func serveScenePkgRaw(w http.ResponseWriter, r *http.Request, name string, target sceneTarget) {
	// 与 sceneReaders 同一顺序:渲染目录优先,配置目录兜底
	for _, dir := range []string{target.Root, target.ConfigRoot} {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, filepath.FromSlash(name))
		// 包含校验:只服务该目录内的文件
		if !strings.HasPrefix(candidate, dir+string(filepath.Separator)) {
			continue
		}
		if !tools.FileExists(candidate) {
			continue
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		http.ServeFile(w, r, candidate)
		return
	}
	http.NotFound(w, r)
}

// serveSceneLooseFile 提供松散工程目录里的文件(按名查渲染目录 → 配置目录)。
// 找不到时再回退到 scene.pkg 内的同名条目:pkg 型壁纸的 scene.json 引用
// 一律走这条路径(httpSource 的 dirRead 或入口读取)。
func serveSceneLooseFile(w http.ResponseWriter, r *http.Request, name string, target sceneTarget) {
	name = strings.ReplaceAll(name, "\\", "/")
	cleaned := strings.TrimPrefix(path.Clean("/"+name), "/")
	if cleaned == "" || strings.Contains(cleaned, "..") {
		http.NotFound(w, r)
		return
	}
	for _, dir := range []string{target.Root, target.ConfigRoot} {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, filepath.FromSlash(cleaned))
		// 包含校验:只服务该目录内的文件
		if !strings.HasPrefix(candidate, dir+string(filepath.Separator)) {
			continue
		}
		if !tools.FileExists(candidate) {
			continue
		}
		if contentType, ok := sceneFileContentType(path.Ext(cleaned)); ok {
			w.Header().Set("Content-Type", contentType)
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		http.ServeFile(w, r, candidate)
		return
	}
	// 包内条目兜底(松散目录不存在或不是松散工程)
	serveScenePkgFile(w, r, cleaned, target)
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

// serveScenePkgFile 把包内条目按原始字节直出。
//
// 这里**不做任何解码**:自研渲染器时代这里给的是解码后的 PNG/JPEG,现在
// 由 WebWallGL 自己处理 .tex,它要的就是包里的原始字节。
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

