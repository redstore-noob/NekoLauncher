package bindings

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// onePixelPng 1×1 的合法 PNG（预览图探测会去读图片头，得给真图）。
func onePixelPng(t *testing.T) []byte {
	t.Helper()

	const encoded = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8AAAwAB/AGtE0zaAAAAAElFTkSuQmCC"
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("解码内置 PNG 失败：%v", err)
	}

	return data
}

func writeFileBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
}

// TestWallpaperEnginePresetFollowsDependency 预设型壁纸（只有 dependency，自己不带
// type/file）必须跟到依赖项目去解析：
//
//	创意工坊 #3694164989「新约能天使」→ dependency #884307090（type=web, file=index.html）
//
// 不跟这一层的话，选中的项目里既没有 type 也没有 file，最终只会退回一张静态预览图，
// 表现就是"网页壁纸不能用"。
func TestWallpaperEnginePresetFollowsDependency(t *testing.T) {
	root := t.TempDir()
	presetDir := filepath.Join(root, "431960", "3694164989")
	dependencyDir := filepath.Join(root, "431960", "884307090")

	writeFile := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("建目录失败：%v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("写文件失败：%v", err)
		}
	}

	// 预设项目：没有 type / file，只有 dependency + 自己的标题与预览图
	writeFile(filepath.Join(presetDir, "project.json"), `{
		"dependency": "884307090",
		"preview": "preview.png",
		"title": "新约能天使",
		"preset": {"MuiscVolume": 50}
	}`)
	writeFileBytes(t, filepath.Join(presetDir, "preview.png"), onePixelPng(t))

	// 依赖项目：真正的网页壁纸
	writeFile(filepath.Join(dependencyDir, "project.json"), `{
		"type": "web",
		"file": "index.html",
		"title": "Perfect Wallpaper",
		"preview": "preview.jpg"
	}`)
	writeFile(filepath.Join(dependencyDir, "index.html"),
		"<!DOCTYPE html><html><body>web wallpaper</body></html>")
	writeFile(filepath.Join(dependencyDir, "js", "main.js"), "console.log('wallpaper');")

	t.Cleanup(func() { setWebWallpaperTarget("", "") })

	result, err := wallpaperEngineWallpaperFromProject(presetDir)
	if err != nil {
		t.Fatalf("解析预设壁纸失败：%v", err)
	}

	if result.Type != "web" {
		t.Fatalf("Type = %q，期望 web（应跟随 dependency）", result.Type)
	}
	if result.Web != "index.html" {
		t.Fatalf("Web = %q，期望 index.html", result.Web)
	}
	// 标题与预览图仍取用户实际选中的那个预设（他在 WE 里看到的样子）
	if result.Title != "新约能天使" {
		t.Fatalf("Title = %q，期望预设项目的标题", result.Title)
	}
	if result.Path != filepath.Join(presetDir, "preview.png") {
		t.Fatalf("Path = %q，期望预设项目的预览图", result.Path)
	}
	if result.Source != "" {
		t.Fatalf("Source = %q，网页壁纸不应有可直出的原文件", result.Source)
	}

	// 路由必须能取到依赖项目里的入口与子资源
	handler := NewWebWallpaperHandler()
	for path, wantType := range map[string]string{
		"/wwwallpaper/__entry":    "text/html; charset=utf-8",
		"/wwwallpaper/js/main.js": "text/javascript; charset=utf-8",
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s → %d，期望 200", path, recorder.Code)
		}
		if got := recorder.Header().Get("Content-Type"); got != wantType {
			t.Errorf("GET %s Content-Type = %q，期望 %q", path, got, wantType)
		}
		if recorder.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("GET %s 缺少 CORS 头（sandbox 里的 ES module / fetch 会被拦）", path)
		}
	}
}

// TestWallpaperEngineDependencyGuards dependency 里的越界输入必须被拒绝。
func TestWallpaperEngineDependencyGuards(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "431960", "1")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}

	for _, dependency := range []string{"", "  ", "../2", `..\2`, "a/b", ".", ".."} {
		if dir, _ := weDependencyProjectDirectory(projectDir, dependency); dir != "" {
			t.Fatalf("dependency=%q 应被拒绝，却解析出 %q", dependency, dir)
		}
	}
}
