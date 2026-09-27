package bindings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pluginTestFixture 搭一个临时插件根目录：一个合法插件 demo、一个只有目录没有清单的
// notaplugin，以及一个放在插件根目录外的机密文件（用于验证路径穿越被挡住）。
func pluginTestFixture(t *testing.T) (root string, outsideSecret string) {
	t.Helper()

	workspace := t.TempDir()
	root = filepath.Join(workspace, "plugins")
	if err := os.MkdirAll(filepath.Join(root, "demo"), 0o755); err != nil {
		t.Fatalf("创建插件目录失败：%v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "notaplugin"), 0o755); err != nil {
		t.Fatalf("创建目录失败：%v", err)
	}

	manifest := `{"id":"demo","name":"演示插件","version":"1.0.0","api":"1"}`
	files := map[string]string{
		filepath.Join(root, "demo", pluginManifestName): manifest,
		filepath.Join(root, "demo", "index.js"):         "export default () => {};",
		filepath.Join(root, "demo", "style.css"):        ".demo { color: red; }",
		filepath.Join(root, "notaplugin", "readme.txt"): "no manifest here",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("写入 %s 失败：%v", path, err)
		}
	}

	outsideSecret = filepath.Join(workspace, "secret.txt")
	if err := os.WriteFile(outsideSecret, []byte("TOP-SECRET"), 0o644); err != nil {
		t.Fatalf("写入机密文件失败：%v", err)
	}
	return root, outsideSecret
}

func doRequest(handler http.Handler, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

// TestPluginIndexListsOnlyRealPlugins 发现接口只列出带 plugin.yaml 的目录，
// 且载荷直接携带规范化后的清单对象。
func TestPluginIndexListsOnlyRealPlugins(t *testing.T) {
	root, _ := pluginTestFixture(t)
	response := doRequest(newPluginHandler(root), pluginRoutePrefix)

	if response.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", response.Code)
	}
	var payload struct {
		Plugins []struct {
			ID string `json:"id"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("响应不是合法 JSON：%v", err)
	}
	if len(payload.Plugins) != 1 || payload.Plugins[0].ID != "demo" {
		t.Errorf("应只列出 demo，实际 %v", payload.Plugins)
	}
}

// TestPluginIndexWithoutDirectoryIsEmpty 插件目录不存在时应返回空列表而非报错。
func TestPluginIndexWithoutDirectoryIsEmpty(t *testing.T) {
	response := doRequest(newPluginHandler(filepath.Join(t.TempDir(), "absent")), pluginRoutePrefix)

	if response.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"plugins":[]`) {
		t.Errorf("应返回空列表，实际 %q", response.Body.String())
	}
}

// TestPluginFileServedWithImportableContentType 入口与清单要带上可用的 Content-Type：
// JS 的 MIME 不对浏览器会直接拒绝 import()。
func TestPluginFileServedWithImportableContentType(t *testing.T) {
	root, _ := pluginTestFixture(t)
	handler := newPluginHandler(root)

	for _, testCase := range []struct {
		target      string
		contentType string
		body        string
	}{
		{"/plugins/demo/index.js", "text/javascript", "export default"},
		{"/plugins/demo/plugin.yaml", "application/yaml", `"id":"demo"`},
		{"/plugins/demo/style.css", "text/css", ".demo"},
	} {
		response := doRequest(handler, testCase.target)
		if response.Code != http.StatusOK {
			t.Errorf("%s：期望 200，实际 %d", testCase.target, response.Code)
			continue
		}
		if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, testCase.contentType) {
			t.Errorf("%s：Content-Type 期望 %s 开头，实际 %q", testCase.target, testCase.contentType, got)
		}
		if !strings.Contains(response.Body.String(), testCase.body) {
			t.Errorf("%s：响应内容异常 %q", testCase.target, response.Body.String())
		}
	}
}

// TestPluginFileRejectsPathTraversal 插件路由不能读到插件目录以外的文件。
func TestPluginFileRejectsPathTraversal(t *testing.T) {
	root, outsideSecret := pluginTestFixture(t)
	handler := newPluginHandler(root)

	for _, target := range []string{
		"/plugins/../secret.txt",
		"/plugins/%2e%2e/secret.txt",
		"/plugins/demo/../../secret.txt",
		"/plugins/..%2fsecret.txt",
	} {
		response := doRequest(handler, target)
		if strings.Contains(response.Body.String(), "TOP-SECRET") {
			t.Errorf("%s：泄露了插件目录外的文件（状态 %d）", target, response.Code)
		}
		if response.Code == http.StatusOK {
			t.Errorf("%s：不应返回 200", target)
		}
	}
	// 确认机密文件确实存在于磁盘上，否则上面的断言会假通过
	if _, err := os.Stat(outsideSecret); err != nil {
		t.Fatalf("夹具异常，机密文件不存在：%v", err)
	}
}

// TestPluginMissingFileIsNotFound 目录而不是文件、或文件不存在时返回 404。
func TestPluginMissingFileIsNotFound(t *testing.T) {
	root, _ := pluginTestFixture(t)
	handler := newPluginHandler(root)

	for _, target := range []string{"/plugins/demo/missing.js", "/plugins/demo", "/plugins/nope/index.js"} {
		if response := doRequest(handler, target); response.Code != http.StatusNotFound {
			t.Errorf("%s：期望 404，实际 %d", target, response.Code)
		}
	}
}

// TestAssetFallbackHandlerRoutes 组合路由应按前缀分派，未知路径 404。
func TestAssetFallbackHandlerRoutes(t *testing.T) {
	handler := NewAssetFallbackHandler()

	// /localfile 仍然走本地文件路由：这里用一个真实存在的绝对路径 + 非白名单扩展名，
	// 断言被扩展名白名单挡下（而不是落到 404，说明路由确实分派过去了）
	disallowed := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(disallowed, []byte("plain text"), 0o644); err != nil {
		t.Fatalf("写入夹具失败：%v", err)
	}
	target := localFileRoute + "?path=" + url.QueryEscape(disallowed)
	if response := doRequest(handler, target); response.Code != http.StatusForbidden {
		t.Errorf("/localfile 应拒绝非白名单扩展名，实际 %d", response.Code)
	}

	if response := doRequest(handler, "/unknown"); response.Code != http.StatusNotFound {
		t.Errorf("/unknown 期望 404，实际 %d", response.Code)
	}
}
