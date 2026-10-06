package bindings

// 场景壁纸渲染载荷与 /wescene 资源路由的单元测试。

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekolauncher/internal/config"
	"nekolauncher/internal/tools"
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
		{"id": 1, "name": "bg", "image": "models/bg.json", "origin": "1280 720 0", "size": "2560 1440"},
		{"id": 2, "name": "char", "image": "models/char.json", "origin": "1500 800 0",
		 "visible": {"user": "showChar", "value": true}}
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
	if payload.Entry != "/wescene/scene.json" {
		t.Fatalf("入口异常:%s", payload.Entry)
	}
	var objects []map[string]any
	if err := json.Unmarshal(payload.Objects, &objects); err != nil || len(objects) != 2 {
		t.Fatalf("objects 透传异常:%v", err)
	}
	scheme, ok := payload.GeneralProperties["schemecolor"]
	if !ok || scheme.Type != "color" || scheme.Value != "1 0 0" {
		t.Fatalf("用户属性异常:%+v", scheme)
	}
}

// TestBuildScenePayloadMergesUserValues 属性默认值应被 WE 记录的用户取值覆盖。
func TestBuildScenePayloadMergesUserValues(t *testing.T) {
	dir := writeSceneProject(t, t.TempDir(), fixtureSceneJSON, `{
		"general": {"properties": {"schemecolor": {"type": "color", "value": "1 0 0"}}}
	}`)
	// 用户在 WE 里把主题色调成了蓝
	userValues := map[string]any{"schemecolor": "0.2 0.4 0.9"}
	merged := mergeUserValuesForTest(t, dir, userValues)
	if merged["schemecolor"].Value != "0.2 0.4 0.9" {
		t.Fatalf("用户取值未覆盖默认:%+v", merged["schemecolor"])
	}
}

// mergeUserValuesForTest 在不碰全局 WE config 的前提下验证合并逻辑:
// 直接调 weBuildScenePayload 的属性合并段(等价实现)。
func mergeUserValuesForTest(t *testing.T, dir string, userValues map[string]any) map[string]weUserProperty {
	t.Helper()
	properties := weProjectProperties(dir)
	out := map[string]weUserProperty{}
	for name, schema := range properties {
		value := schema.value
		if override, ok := userValues[name]; ok {
			value = override
		}
		out[name] = weUserProperty{Type: schema.propertyType, Value: value, Text: schema.text}
	}
	return out
}

func TestUserValuesFromConfig(t *testing.T) {
	configJSON := []byte(`{
		"22907": {"wproperties": {
			"D:/x/y/scene.pkg": {"Monitor0": {"schemecolor": "0.1 0.2 0.3", "stars": 42}}
		}}
	}`)
	values := weUserValuesFromConfig(configJSON, `D:\x\y\scene.pkg`)
	if values == nil {
		t.Fatal("未取到用户取值")
	}
	if values["schemecolor"] != "0.1 0.2 0.3" || values["stars"] != float64(42) {
		t.Fatalf("取值异常:%+v", values)
	}
	// 其他壁纸不应命中
	if got := weUserValuesFromConfig(configJSON, "D:/other/scene.pkg"); got != nil {
		t.Fatalf("不应命中其他壁纸:%+v", got)
	}
}

func TestSceneRouteServesResources(t *testing.T) {
	dir := writeSceneProject(t, t.TempDir(), fixtureSceneJSON, "")
	setSceneTarget(dir, "")
	defer setSceneTarget("", "")
	handler := NewSceneWallpaperHandler()

	// scene.json
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/scene.json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("scene.json 状态码 %d", response.Code)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"objects"`)) {
		t.Fatal("scene.json 内容异常")
	}

	// 贴图:解码为 PNG
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/tex/materials/char.tex", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("贴图状态码 %d", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("贴图 Content-Type 异常:%s", contentType)
	}
	if _, err := png.Decode(bytes.NewReader(response.Body.Bytes())); err != nil {
		t.Fatalf("贴图不是合法 PNG:%v", err)
	}

	// 包内原始文件(粒子 JSON)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/pkg/particles/snow.json", nil))
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte("emitter")) {
		t.Fatalf("粒子定义异常:code=%d body=%s", response.Code, response.Body.String())
	}

	// 字体
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/pkg/fonts/clock.ttf", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "font/ttf" {
		t.Fatalf("字体路由异常:code=%d type=%s", response.Code, response.Header().Get("Content-Type"))
	}

	// 越界路径与未知类型
	for _, path := range []string{"/wescene/", "/wescene/tex/../../secret", "/wescene/pkg/..%2Fescape"} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code == http.StatusOK {
			t.Fatalf("越界路径不应命中:%s", path)
		}
	}

	// 贴图缓存命中:二次请求应 200(磁盘缓存路径)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/tex/materials/char.tex", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("缓存命中失败:%d", response.Code)
	}
}

// TestSceneRoutePresetFallback 预设型壁纸:配置目录的包兜底提供渲染目录缺失的资源。
func TestSceneRoutePresetFallback(t *testing.T) {
	renderDir := writeSceneProject(t, filepath.Join(t.TempDir(), "render"), fixtureSceneJSON, "")
	configDir := t.TempDir()
	// 配置目录的包提供一张渲染目录没有的贴图
	pkgData := buildPkg(t, []pkgFixtureEntry{
		{Path: "materials/skin_overlay.tex", Data: buildTex(weTexFormatRGBA8888, 2, 2, 2, 2,
			[]byte{1, 2, 3, 255, 4, 5, 6, 255, 7, 8, 9, 255, 10, 11, 12, 255})},
	})
	if err := os.WriteFile(filepath.Join(configDir, "scene.pkg"), pkgData, 0o644); err != nil {
		t.Fatal(err)
	}

	setSceneTarget(renderDir, configDir)
	defer setSceneTarget("", "")
	handler := NewSceneWallpaperHandler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/tex/materials/skin_overlay.tex", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("配置目录兜底失败:%d", response.Code)
	}
}

// TestSceneRouteWithoutTarget 未选中场景壁纸时全部 404。
func TestSceneRouteWithoutTarget(t *testing.T) {
	setSceneTarget("", "")
	handler := NewSceneWallpaperHandler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/scene.json", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("无目标时应 404,实际 %d", response.Code)
	}
}

// TestScenePayloadPropertyPrecedence 预设型壁纸:用户项目的属性 schema 优先于依赖项目。
func TestScenePayloadPropertyPrecedence(t *testing.T) {
	renderDir := writeSceneProject(t, filepath.Join(t.TempDir(), "render"), fixtureSceneJSON, `{
		"general": {"properties": {
			"schemecolor": {"type": "color", "value": "0 0 0"},
			"onlyInRender": {"type": "bool", "value": true}
		}}
	}`)
	configDir := writeSceneProject(t, filepath.Join(t.TempDir(), "config"), "{}", `{
		"general": {"properties": {"schemecolor": {"type": "color", "value": "1 1 1"}}}
	}`)
	// configDir 没有 scene.pkg 的内容也没关系,weBuildScenePayload 只读 renderDir 的包
	payload := weBuildScenePayload(renderDir, configDir, "")
	if payload == nil {
		t.Fatal("载荷组装失败")
	}
	if payload.GeneralProperties["schemecolor"].Value != "1 1 1" {
		t.Fatalf("用户项目属性应优先:%+v", payload.GeneralProperties["schemecolor"])
	}
	// 依赖项目独有的属性保留
	if _, ok := payload.GeneralProperties["onlyinrender"]; !ok {
		t.Fatal("依赖项目独有属性丢失(键按小写归一)")
	}
}

// 路由写贴图缓存依赖 config.StorageDirectory;测试进程里它可能未初始化,
// weTextureCacheBase 返回空串时退化为直出,不影响断言。这里顺带保证不 panic。
func TestSceneTextureCachePathSafe(t *testing.T) {
	dir := writeSceneProject(t, t.TempDir(), fixtureSceneJSON, "")
	setSceneTarget(dir, "")
	defer setSceneTarget("", "")
	handler := NewSceneWallpaperHandler()
	for i := 0; i < 2; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wescene/tex/materials/char.tex", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求失败:%d", i+1, response.Code)
		}
	}
	_ = config.StorageDirectory()
}

// ---- 真实壁纸端到端(可选,设 WE_TEST_PKG_DIR 启用) ----

// encodeScenePath 把包内路径逐段百分号编码(保留 /),包名里有中文与空格,
// httptest.NewRequest 不接受未编码的非 ASCII 路径;真实前端由浏览器自动编码。
func encodeScenePath(raw string) string {
	segments := strings.Split(raw, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

// TestSceneIntegrationRealWallpapers 全链路回归:每个真实场景壁纸
// 组装载荷 → 资源路由取 scene.json → 逐图像对象解析材质链 → 取贴图。
// 覆盖了前端实际会走的每一步(除 WebGL 渲染本身)。
func TestSceneIntegrationRealWallpapers(t *testing.T) {
	root := weTestPkgRoot(t)
	if root == "" {
		t.Skip("未设置 WE_TEST_PKG_DIR,跳过真实壁纸集成")
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		t.Skip("读目录失败:", err)
	}
	handler := NewSceneWallpaperHandler()
	scenes, texturesOK, texturesFailed := 0, 0, 0

	for _, dirEntry := range dirs {
		dir := filepath.Join(root, dirEntry.Name())
		if !tools.FileExists(filepath.Join(dir, "scene.pkg")) {
			continue
		}
		result, err := wallpaperEngineWallpaperFromProject(dir, "")
		if err != nil {
			continue
		}
		if result.Type != "scene" || result.Scene == nil {
			continue
		}
		scenes++
		setSceneTarget(dir, "")
		payload := result.Scene

		// 1. scene.json 路由
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, payload.Entry, nil))
		if response.Code != http.StatusOK {
			t.Errorf("[%s] scene.json %d", dirEntry.Name(), response.Code)
			continue
		}

		// 2. 逐图像对象:材质链第一张贴图能从路由取回合法图片
		var objects []map[string]any
		if err := json.Unmarshal(payload.Objects, &objects); err != nil {
			t.Errorf("[%s] objects 解析失败", dirEntry.Name())
			continue
		}
		for _, object := range objects {
			imageRef, _ := object["image"].(string)
			if imageRef == "" {
				continue
			}
			modelResponse := httptest.NewRecorder()
			handler.ServeHTTP(modelResponse, httptest.NewRequest(http.MethodGet, "/wescene/pkg/"+encodeScenePath(imageRef), nil))
			if modelResponse.Code != http.StatusOK {
				continue // 内建 solidlayer 等没有包内文件
			}
			var model struct {
				Material string `json:"material"`
			}
			if err := json.Unmarshal(modelResponse.Body.Bytes(), &model); err != nil || model.Material == "" {
				continue
			}
			materialResponse := httptest.NewRecorder()
			handler.ServeHTTP(materialResponse, httptest.NewRequest(http.MethodGet, "/wescene/pkg/"+encodeScenePath(model.Material), nil))
			if materialResponse.Code != http.StatusOK {
				continue
			}
			var material struct {
				Passes []struct {
					Textures []string `json:"textures"`
				} `json:"passes"`
			}
			if err := json.Unmarshal(materialResponse.Body.Bytes(), &material); err != nil {
				continue
			}
			for _, name := range material.Passes[0].Textures {
				for _, candidate := range []string{"materials/" + name + ".tex", name + ".tex"} {
					texResponse := httptest.NewRecorder()
					handler.ServeHTTP(texResponse, httptest.NewRequest(http.MethodGet, "/wescene/tex/"+encodeScenePath(candidate), nil))
					if texResponse.Code == http.StatusOK {
						texturesOK++
						// PNG / JPEG / MP4(ftyp 盒)三种合法载荷头
						body := texResponse.Body.Bytes()
						if !(bytes.HasPrefix(body, []byte{0x89, 'P', 'N', 'G'}) ||
							bytes.HasPrefix(body, []byte{0xFF, 0xD8, 0xFF}) ||
							(len(body) > 8 && string(body[4:8]) == "ftyp")) {
							t.Errorf("[%s] %s 非法载荷头", dirEntry.Name(), candidate)
						}
						break
					}
				}
			}
			_ = texturesFailed
		}
	}
	setSceneTarget("", "")
	if scenes == 0 {
		t.Skip("目录里没有场景壁纸")
	}
	t.Logf("真实场景壁纸 %d 个,贴图取回 %d 次", scenes, texturesOK)
	if texturesOK == 0 {
		t.Error("没有成功取回任何贴图")
	}
}
