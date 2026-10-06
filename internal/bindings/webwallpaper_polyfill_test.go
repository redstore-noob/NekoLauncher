package bindings

// WE 网页壁纸兼容层的单元测试:属性合并优先级、注入位置与转义、路由端到端。

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeWebProject 落一个最小网页壁纸项目(模板 + 可选预设)。
func writeWebProject(t *testing.T, dir, projectJSON string, withEntry bool) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "project.json"), []byte(projectJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if withEntry {
		html := `<!DOCTYPE html><html><head><meta charset="utf-8"><script src="js/main.js"></script></head><body></body></html>`
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const templateProjectJSON = `{
	"type": "web",
	"file": "index.html",
	"workshopid": "884307090",
	"general": {"properties": {
		"schemecolor": {"type": "color", "value": "#7a7574"},
		"DateX": {"type": "slider", "value": 50},
		"showClock": {"type": "bool", "value": true},
		"DateFormat": {"type": "combo", "value": 15}
	}}
}`

const presetProjectJSON = `{
	"dependency": "884307090",
	"preset": {
		"DateX": 120,
		"DateFormat": 16,
		"showClock": false
	}
}`

func TestWebUserPropertiesMergeOrder(t *testing.T) {
	renderDir := writeWebProject(t, filepath.Join(t.TempDir(), "render"), templateProjectJSON, false)
	configDir := writeWebProject(t, filepath.Join(t.TempDir(), "config"), presetProjectJSON, false)

	properties := weWebUserProperties(renderDir, configDir, "")
	if properties == nil {
		t.Fatal("属性为空")
	}
	// 预设覆盖默认,且键保持 schema 原始大小写(壁纸 JS 按原样读)
	if properties["DateX"] != float64(120) {
		t.Fatalf("预设 DateX 未覆盖默认: %v", properties["DateX"])
	}
	if properties["DateFormat"] != float64(16) {
		t.Fatalf("预设 DateFormat 未覆盖: %v", properties["DateFormat"])
	}
	if properties["showClock"] != false {
		t.Fatalf("预设 showClock 未覆盖: %v", properties["showClock"])
	}
	// 未覆盖的保留模板默认
	if properties["schemecolor"] != "#7a7574" {
		t.Fatalf("模板默认值丢失: %v", properties["schemecolor"])
	}
	// 大小写变体的 preset 键也归一到 schema 原样(preset 里可能存成 datex)
	configDir2 := writeWebProject(t, filepath.Join(t.TempDir(), "config2"), `{
		"dependency": "884307090",
		"preset": {"datex": 200}
	}`, false)
	properties2 := weWebUserProperties(renderDir, configDir2, "")
	if properties2["DateX"] != float64(200) {
		t.Fatalf("小写 preset 键未归一到 schema 大小写: %v", properties2["DateX"])
	}
}

func TestWebUserPropertiesWpropertiesOverride(t *testing.T) {
	renderDir := writeWebProject(t, t.TempDir(), templateProjectJSON, false)
	// WE wproperties 优先级最高(用户手动改的);键大小写归一到 schema
	configJSON := []byte(`{"u": {"wproperties": {"D:/x/index.html": {"Monitor0": {"DateX": 999}}}}}`)
	if got := weUserValuesFromConfig(configJSON, `D:\x\index.html`); got["DateX"] != float64(999) {
		t.Fatalf("wproperties 解析失败: %v", got)
	}

	properties := weWebUserProperties(renderDir, "", "")
	if properties["DateX"] != float64(50) {
		t.Fatalf("无覆盖时应为模板默认: %v", properties["DateX"])
	}
}

func TestInjectWebPolyfillPlacement(t *testing.T) {
	html := []byte("<html><head><title>x</title></head><body></body></html>")
	out := injectWebPolyfill(html, `{"a":1}`)

	// 注入必须出现在 head 之后、body 之前(先于壁纸脚本执行)
	headIndex := bytes.Index(out, []byte("</head>"))
	scriptIndex := bytes.Index(out, []byte("__wePolyfillInstalled"))
	bodyIndex := bytes.Index(out, []byte("<body"))
	if scriptIndex < 0 || headIndex < 0 || bodyIndex < 0 {
		t.Fatalf("注入丢失")
	}
	if !(scriptIndex < headIndex && scriptIndex < bodyIndex) {
		t.Fatal("polyfill 必须位于 head 内(先于 body 里的壁纸脚本)")
	}
	// 属性注入存在
	if !bytes.Contains(out, []byte(`{"a":1}`)) {
		t.Fatal("用户属性未注入")
	}
}

func TestInjectWebPolyfillNoHead(t *testing.T) {
	html := []byte("<div>no head</div>")
	out := injectWebPolyfill(html, `{}`)
	if !bytes.HasPrefix(out, []byte("<script>")) {
		t.Fatal("无 head 时应前置注入")
	}
	if !bytes.Contains(out, []byte("<div>no head</div>")) {
		t.Fatal("原文内容缺失")
	}
}

func TestWebPropertiesJSONScriptSafe(t *testing.T) {
	// 值里带 </script> 与引号:encoding/json 默认转义 <,不允许逃逸
	properties := map[string]any{
		"evil": "</script><script>alert(1)</script>",
	}
	encoded := weWebPropertiesJSON(properties)
	if strings.Contains(encoded, "</script>") {
		t.Fatalf("JSON 未转义 script 标签: %s", encoded)
	}
	script := weWebPolyfillScript(encoded)
	if bytes.Contains(script, []byte("</script>")) {
		t.Fatal("polyfill 脚本内出现裸 </script>")
	}
}

func TestWebPropertiesVersionShortAndStable(t *testing.T) {
	properties := map[string]any{"a": 1, "b": "x"}
	first := weWebPropertiesVersion(properties)
	if len(first) != 16 {
		t.Fatalf("指纹长度异常: %s", first)
	}
	if weWebPropertiesVersion(properties) != first {
		t.Fatal("同属性集合指纹应稳定")
	}
	if weWebPropertiesVersion(map[string]any{"a": 2, "b": "x"}) == first {
		t.Fatal("属性变化指纹应变化")
	}
}

// 端到端:入口 HTML 响应带注入与属性;project.json 主目录优先;
// 预设独有的资源从配置目录兜底。
func TestWebRouteInjectionAndConfigFallback(t *testing.T) {
	renderDir := writeWebProject(t, filepath.Join(t.TempDir(), "render"), templateProjectJSON, true)
	configDir := writeWebProject(t, filepath.Join(t.TempDir(), "config"), presetProjectJSON, false)
	// 配置目录放一个模板没有的资源(预设素材)
	if err := os.MkdirAll(filepath.Join(configDir, "imgs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "imgs", "preset-bg.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	setWebWallpaperTarget(renderDir, "index.html", configDir, "")
	t.Cleanup(func() { setWebWallpaperTarget("", "", "", "") })
	handler := NewWebWallpaperHandler()

	// 1. 入口(别名)注入
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wwwallpaper/__entry", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("入口状态码 %d", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, "__wePolyfillInstalled") {
		t.Fatal("入口未注入 polyfill")
	}
	if !strings.Contains(body, "wallpaperRegisterAudioListener") {
		t.Fatal("polyfill 缺音频 API")
	}
	// 预设值注入了(DateX=120);同时带 ?v= 参数也不影响
	if !strings.Contains(body, `"DateX":120`) {
		t.Fatalf("预设属性未注入: %.200s", body)
	}

	// 2. project.json 主目录优先(workshopid 校验依赖模板那份)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wwwallpaper/project.json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("project.json 状态码 %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "884307090") {
		t.Fatal("project.json 应返回模板(依赖项目)那份")
	}

	// 3. 预设独有资源从配置目录兜底
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wwwallpaper/imgs/preset-bg.png", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("预设资源兜底失败: %d", response.Code)
	}

	// 4. 子页面 HTML 不注入(只有入口注入)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wwwallpaper/index.html", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("直连入口状态码 %d", response.Code)
	}
	if strings.Contains(response.Body.String(), "__wePolyfillInstalled") {
		t.Fatal("非别名路径不应重复注入")
	}
}

func TestWebUserPropertiesNestedGeneralProperties(t *testing.T) {
	// 回归:预设项目的 general.properties 嵌套在 general 下,顶层 tag 读不到
	renderDir := writeWebProject(t, filepath.Join(t.TempDir(), "render"), templateProjectJSON, false)
	configDir := writeWebProject(t, filepath.Join(t.TempDir(), "config"), `{
		"dependency": "884307090",
		"general": {"properties": {"DateX": {"type": "slider", "value": 77}}},
		"preset": {"DateX": 120}
	}`, false)

	properties := weWebUserProperties(renderDir, configDir, "")
	// preset 对象是权威来源,覆盖 general.properties 里的值;键归一到 schema 原样
	if properties["DateX"] != float64(120) {
		t.Fatalf("preset 应覆盖 general.properties: %v", properties["DateX"])
	}

	// 只有 general.properties 时它生效
	configOnly := writeWebProject(t, filepath.Join(t.TempDir(), "config2"), `{
		"dependency": "884307090",
		"general": {"properties": {"showClock": {"type": "bool", "value": false}}}
	}`, false)
	properties = weWebUserProperties(renderDir, configOnly, "")
	if properties["showClock"] != false {
		t.Fatalf("general.properties 嵌套值未生效: %v", properties["showClock"])
	}
}

func TestWebRouteEntryAliasWithVersionQuery(t *testing.T) {
	renderDir := writeWebProject(t, filepath.Join(t.TempDir(), "render"), templateProjectJSON, true)
	setWebWallpaperTarget(renderDir, "index.html", "", "")
	t.Cleanup(func() { setWebWallpaperTarget("", "", "", "") })
	handler := NewWebWallpaperHandler()

	// 前端拼的 ?v= 指纹参数不影响别名解析与注入
	request := httptest.NewRequest(http.MethodGet, "/wwwallpaper/__entry?v=49c5d7c3f2b1f521", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("带 ?v= 的入口状态码 %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "__wePolyfillInstalled") {
		t.Fatal("带 ?v= 的入口未注入 polyfill")
	}
}

func TestWebRouteFileRedirect(t *testing.T) {
	// file 类型属性重定向:配置目录(预设)的自定义文件优先,模板兜底
	renderDir := writeWebProject(t, filepath.Join(t.TempDir(), "render"), templateProjectJSON, true)
	configDir := writeWebProject(t, filepath.Join(t.TempDir(), "config"), presetProjectJSON, false)
	if err := os.MkdirAll(filepath.Join(configDir, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "files", "wallpaper.webm"), []byte("webm-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	setWebWallpaperTarget(renderDir, "index.html", configDir, "")
	t.Cleanup(func() { setWebWallpaperTarget("", "", "", "") })
	handler := NewWebWallpaperHandler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wwwallpaper/file/files/wallpaper.webm", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("file 重定向失败: %d", response.Code)
	}
	if response.Body.String() != "webm-bytes" {
		t.Fatal("file 内容不符")
	}
	// 越界防御
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wwwallpaper/file/../../secret", nil))
	if response.Code == http.StatusOK {
		t.Fatal("file 路由越界不应命中")
	}
	// polyfill 的重写函数存在且逻辑正确
	script := string(weWebPolyfillScript("{}"))
	if !strings.Contains(script, "rewriteFileUrl") || !strings.Contains(script, "/wwwallpaper/file/") {
		t.Fatal("polyfill 缺少 file:// 重写")
	}
}

func TestWebPolyfillWebGLPointShim(t *testing.T) {
	// 回归:壁纸脚本写 gl.drawArrays(gl.POINT, ...),WebGL 无 gl.POINT(常量是
	// gl.POINTS=0),undefined 传入 drawArrays 被 GL 静默吞掉——粒子一次都不画。
	// polyfill 应 hook getContext 给上下文补 gl.POINT = gl.POINTS。
	script := string(weWebPolyfillScript("{}"))
	for _, marker := range []string{"gl.POINT = gl.POINTS", "HTMLCanvasElement.prototype.getContext"} {
		if !strings.Contains(script, marker) {
			t.Fatalf("polyfill 缺少 WebGL shim 关键段: %s", marker)
		}
	}
}
