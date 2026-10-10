package bindings

// 场景壁纸渲染载荷与 /wescene 资源路由的单元测试。
//
// 自研 THREE.js 渲染器已移除,场景画面由前端动态加载的 WebWallGL 负责,
// 所以这里的契约只有两条:
//  1. 载荷给对资源基址、设计分辨率与用户属性覆盖表;
//  2. /wescene 路由能按 WebWallGL 的 httpSource 请求路径提供原始字节
//     (scene.pkg / 合成的 project.json / 包内或松散目录里的任何文件)。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeSceneProject 在临时目录里落一个最小可用的场景壁纸项目:
// scene.pkg(含 scene.json + 一张贴图)+ project.json(含用户属性)。
func writeSceneProject(t *testing.T, dir, sceneJSON, projectJSON string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 2×2 RGBA 贴图
	px := []byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 255, 255}
	tex := buildTex(weTexFormatRGBA8888, 2, 2, 2, 2, px)
	// buildPkg(t, entries) 在 pkg 测试文件里;scene.json 条目必须存在
	pkgData := buildPkg(t, []pkgFixtureEntry{
		{Path: "scene.json", Data: []byte(sceneJSON)},
		{Path: "materials/char.tex", Data: tex},
		{Path: "particles/snow.json", Data: []byte(`{"emitter":[]}`)},
		{Path: "fonts/clock.ttf", Data: []byte("fake-font-bytes")},
	})
	if err := os.WriteFile(filepath.Join(dir, "scene.pkg"), pkgData, 0o644); err != nil {
		t.Fatal(err)
	}
	if projectJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, "project.json"), []byte(projectJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const fixtureSceneJSON = `{
	"general": {"orthogonalprojection": {"width": 2560, "height": 1440}},
	"objects": [
		{"id": 1, "name": "bg", "image": "models/bg.json", "origin": "1280 720 0", "size": "2560 1440"}
	],
	"version": 5
}`

func TestBuildScenePayload(t *testing.T) {
	dir := writeSceneProject(t, t.TempDir(), fixtureSceneJSON, `{
		"title": "测试壁纸", "type": "scene",
		"general": {"properties": {
			"schemecolor": {"type": "color", "text": "主题色", "value": "1 0 0"},
			"showChar": {"type": "bool", "text": "显示人物", "value": true}
		}}
	}`)

	payload := weBuildScenePayload(dir, dir, "")
	if payload == nil {
		t.Fatal("载荷组装失败")
	}
	if payload.DesignWidth != 2560 || payload.DesignHeight != 1440 {
		t.Fatalf("设计分辨率异常:%dx%d", payload.DesignWidth, payload.DesignHeight)
	}
	// 基址必须带会话指纹前缀:库按基址字符串缓存 scene.pkg 解析结果
	if !strings.HasPrefix(payload.Base, "/wescene/v/") {
		t.Fatalf("资源基址异常:%s", payload.Base)
	}
	if payload.Properties["schemecolor"] != "1 0 0" {
		t.Fatalf("用户属性异常:%+v", payload.Properties)
	}
	if payload.Properties["showChar"] != true {
		t.Fatalf("布尔属性异常:%+v", payload.Properties)
	}
}

// TestBuildScenePayloadLooseProject 松散工程(没有 scene.pkg,只有 scene.json):
// 载荷照样可用,设计分辨率从磁盘上的 scene.json 读。
func TestBuildScenePayloadLooseProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scene.json"),
		[]byte(`{"general":{"orthogonalprojection":{"width":1280,"height":720}},"objects":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := weBuildScenePayload(dir, "", "")
	if payload == nil {
		t.Fatal("松散工程载荷组装失败")
	}
	if payload.DesignWidth != 1280 || payload.DesignHeight != 720 {
		t.Fatalf("松散工程设计分辨率异常:%dx%d", payload.DesignWidth, payload.DesignHeight)
	}
}

// TestBuildScenePayloadNotAScene 既没有 project.json 也没有 scene.json 时
// 返回 nil:调用方据此回落到静态提取图,而不是挂一个必然空白的渲染器。
func TestBuildScenePayloadNotAScene(t *testing.T) {
	if payload := weBuildScenePayload(t.TempDir(), "", ""); payload != nil {
		t.Fatalf("非场景项目不应产出载荷:%+v", payload)
	}
}

// TestScenePropertyOverridesCase 覆盖表必须保留 schema 原始键名并按优先级合并。
func TestScenePropertyOverridesCase(t *testing.T) {
	renderDir := writeSceneProject(t, filepath.Join(t.TempDir(), "render"), fixtureSceneJSON, `{
		"general": {"properties": {
			"SchemeColor": {"type": "color", "value": "0 0 0"},
			"onlyRender": {"type": "bool", "value": true}
		}}
	}`)
	configDir := writeSceneProject(t, filepath.Join(t.TempDir(), "config"), "{}", `{
		"general": {"properties": {"schemeColor": {"type": "color", "value": "1 1 1"}}}
	}`)

	overrides := weScenePropertyOverrides(renderDir, configDir, "")
	// 原始大小写键名保留(SchemeColor),配置目录的小写写法被归一到同名键
	if overrides["SchemeColor"] != "1 1 1" {
		t.Fatalf("覆盖表优先级/大小写异常:%+v", overrides)
	}
	if _, exists := overrides["schemeColor"]; exists {
		t.Fatalf("不应出现小写重复键:%+v", overrides)
	}
	if overrides["onlyRender"] != true {
		t.Fatalf("渲染目录独有属性丢失:%+v", overrides)
	}

	// 会话指纹随 scene.pkg 修改时间变化(换壁纸/包被重写后缓存失效)
	first := sceneBaseToken(renderDir, configDir, "")
	if err := os.Chtimes(filepath.Join(renderDir, "scene.pkg"),
		time.Now().Add(2*time.Hour), time.Now().Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if second := sceneBaseToken(renderDir, configDir, ""); second == first {
		t.Fatalf("包改动后会话指纹应变化:%s", second)
	}
}

// TestSceneRouteServesWebWallGLSources WebWallGL(httpSource)需要的入口:
// 带会话指纹的场景包原始字节、合成的 project.json、包内/松散目录的按名读取。
func TestSceneRouteServesWebWallGLSources(t *testing.T) {
	dir := writeSceneProject(t, t.TempDir(), fixtureSceneJSON, `{
		"file": "scene.json", "type": "scene",
		"general": {"properties": {"schemecolor": {"type": "color", "value": "1 0 0"}}}
	}`)
	setSceneTarget(dir, "")
	defer setSceneTarget("", "")
	handler := NewSceneWallpaperHandler()

	payload := weBuildScenePayload(dir, dir, "")
	if payload == nil {
		t.Fatal("载荷组装失败")
	}
	base := strings.TrimSuffix(payload.Base, "/")

	// scene.pkg 原始字节:必须与盘上文件逐字节一致(库自己解析包)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, base+"/scene.pkg", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("scene.pkg 状态码 %d", response.Code)
	}
	onDisk, err := os.ReadFile(filepath.Join(dir, "scene.pkg"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response.Body.Bytes(), onDisk) {
		t.Fatalf("scene.pkg 字节不一致:响应 %d 字节,盘上 %d 字节",
			response.Body.Len(), len(onDisk))
	}

	// project.json:合成后的 JSON,file/type 齐全
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, base+"/project.json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("project.json 状态码 %d", response.Code)
	}
	var project map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &project); err != nil {
		t.Fatalf("project.json 不是合法 JSON:%v", err)
	}
	if project["file"] != "scene.json" || project["type"] != "scene" {
		t.Fatalf("project.json 字段异常:%+v", project)
	}

	// 包内条目按名直出:scene.json 与 .tex 都必须是**原始字节**,不做解码
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, base+"/scene.json", nil))
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"objects"`)) {
		t.Fatalf("包内 scene.json 读取异常:code=%d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, base+"/materials/char.tex", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("包内贴图读取异常:code=%d", response.Code)
	}
	if !bytes.Equal(response.Body.Bytes(), buildTex(weTexFormatRGBA8888, 2, 2, 2, 2,
		[]byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 255, 255})) {
		t.Fatal("贴图应直出 .tex 原始字节(解码交给 WebWallGL)")
	}

	// 越界被挡
	for _, path := range []string{
		base + "/../../secret",
		base + "/pkg/..%2Fescape",
		"/wescene/",
	} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code == http.StatusOK {
			t.Fatalf("越界/空路径不应命中:%s", path)
		}
	}
}

// TestSceneRoutePresetFallback 预设型壁纸:配置目录的包兜底提供渲染目录缺失的资源。
func TestSceneRoutePresetFallback(t *testing.T) {
	renderDir := writeSceneProject(t, filepath.Join(t.TempDir(), "render"), fixtureSceneJSON, "")
	configDir := t.TempDir()
	pkgData := buildPkg(t, []pkgFixtureEntry{
		{Path: "materials/skin_overlay.tex", Data: []byte("config-only-tex")},
	})
	if err := os.WriteFile(filepath.Join(configDir, "scene.pkg"), pkgData, 0o644); err != nil {
		t.Fatal(err)
	}
	// 配置目录还带 project.json(用户项目的属性表)
	if err := os.WriteFile(filepath.Join(configDir, "project.json"),
		[]byte(`{"title":"预设","file":"scene.json","type":"scene"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	setSceneTarget(renderDir, configDir)
	defer setSceneTarget("", "")
	handler := NewSceneWallpaperHandler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/materials/skin_overlay.tex", nil))
	if response.Code != http.StatusOK || response.Body.String() != "config-only-tex" {
		t.Fatalf("配置目录兜底失败:code=%d body=%q", response.Code, response.Body.String())
	}

	// scene.pkg 也要能落到配置目录
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/scene.pkg", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("配置目录 scene.pkg 兜底失败:%d", response.Code)
	}
}

// TestSceneRouteLooseProject 松散工程(有 scene.json 与贴图、没有 scene.pkg):
// 文件按名从渲染目录直出,project.json 由两个目录合成,缺省给最小形态。
func TestSceneRouteLooseProject(t *testing.T) {
	renderDir := t.TempDir()
	sceneJSON := `{"general":{"orthogonalprojection":{"width":1920,"height":1080}},"objects":[]}`
	if err := os.WriteFile(filepath.Join(renderDir, "scene.json"), []byte(sceneJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(renderDir, "materials"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(renderDir, "materials", "loose.tex"), []byte("loose-tex"), 0o644); err != nil {
		t.Fatal(err)
	}
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "project.json"),
		[]byte(`{"title":"松散工程","file":"scene.json","type":"scene"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	setSceneTarget(renderDir, configDir)
	defer setSceneTarget("", "")
	handler := NewSceneWallpaperHandler()

	// 松散文件按名直出,内容逐字节一致(WebWallGL 自己解码 .tex)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/materials/loose.tex", nil))
	if response.Code != http.StatusOK || response.Body.String() != "loose-tex" {
		t.Fatalf("松散文件读取异常:code=%d body=%q", response.Code, response.Body.String())
	}

	// project.json 合成:渲染目录没有,取配置目录的
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/project.json", nil))
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte("松散工程")) {
		t.Fatalf("合成 project.json 异常:code=%d body=%s", response.Code, response.Body.String())
	}

	// 没有 project.json 时给最小形态(否则库会把 pkg 型壁纸误判成松散工程)
	emptyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(emptyDir, "scene.json"), []byte(sceneJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	setSceneTarget(emptyDir, "")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/project.json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("缺 project.json 时应给最小形态:code=%d", response.Code)
	}
	var minimal map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &minimal); err != nil {
		t.Fatalf("最小形态不是合法 JSON:%v", err)
	}
	if minimal["file"] != "scene.json" {
		t.Fatalf("最小形态缺少 file 字段:%+v", minimal)
	}
}

// TestSceneRouteWithoutTarget 未选中场景壁纸时全部 404。
func TestSceneRouteWithoutTarget(t *testing.T) {
	setSceneTarget("", "")
	handler := NewSceneWallpaperHandler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/scene.pkg", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("无目标时应 404,实际 %d", response.Code)
	}
}
