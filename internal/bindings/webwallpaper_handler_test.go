package bindings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeWebWallpaperProject 造一个最小的 WE 网页壁纸项目目录。
func writeWebWallpaperProject(t *testing.T, projectType, file string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"),
		[]byte("<!doctype html><html><body>wallpaper</body></html>"), 0o644); err != nil {
		t.Fatalf("写入口失败：%v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "js"), 0o755); err != nil {
		t.Fatalf("建子目录失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "js", "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatalf("写脚本失败：%v", err)
	}
	manifest := map[string]string{"type": projectType, "title": "demo", "file": file}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("序列化 project.json 失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "project.json"), data, 0o644); err != nil {
		t.Fatalf("写 project.json 失败：%v", err)
	}

	return dir
}

// TestWebWallpaperProjectDetection 网页壁纸要解析出入口，并把资源根钉到该目录。
func TestWebWallpaperProjectDetection(t *testing.T) {
	dir := writeWebWallpaperProject(t, "web", "index.html")

	result, err := wallpaperEngineWallpaperFromProject(dir, "")
	if err != nil {
		t.Fatalf("解析壁纸项目失败：%v", err)
	}
	if result.Web != "index.html" {
		t.Fatalf("入口期望 index.html，实际 %q", result.Web)
	}
	if target := currentWebWallpaperTarget(); target.Root != dir || target.Entry != "index.html" {
		t.Fatalf("资源根未钉住：%+v", target)
	}

	// 非网页类型必须清空，避免路由继续对外提供上一个壁纸的文件
	other := writeWebWallpaperProject(t, "video", "video.mp4")
	if _, err := wallpaperEngineWallpaperFromProject(other, ""); err != nil {
		t.Fatalf("解析视频壁纸失败：%v", err)
	}
	if target := currentWebWallpaperTarget(); target.Root != "" {
		t.Fatalf("非网页类型应清空资源根，实际 %+v", target)
	}
}

// TestWebWallpaperEntryAliasMissing 没有入口 HTML 时不返回 Web（前端会回落预览图）。
func TestWebWallpaperEntryAliasMissing(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"type":"web","title":"demo","file":"missing.html"}`
	if err := os.WriteFile(filepath.Join(dir, "project.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("写 project.json 失败：%v", err)
	}

	result, err := wallpaperEngineWallpaperFromProject(dir, "")
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if result.Web != "" {
		t.Fatalf("找不到入口时 Web 应为空，实际 %q", result.Web)
	}
}

// TestWebWallpaperRouteServesEntryAndAssets 别名取入口、子目录资源可读、越界被挡。
func TestWebWallpaperRouteServesEntryAndAssets(t *testing.T) {
	dir := writeWebWallpaperProject(t, "web", "index.html")
	if _, err := wallpaperEngineWallpaperFromProject(dir, ""); err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	handler := NewWebWallpaperHandler()

	fetch := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		return rec
	}

	entry := fetch(webWallpaperRoutePrefix + webWallpaperEntryAlias)
	if entry.Code != http.StatusOK {
		t.Fatalf("入口别名应返回 200，实际 %d", entry.Code)
	}
	if !strings.Contains(entry.Body.String(), "wallpaper") {
		t.Fatalf("入口内容不对：%q", entry.Body.String())
	}
	if got := entry.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("入口 Content-Type 应为 text/html，实际 %q", got)
	}
	// 沙箱 iframe 是 opaque origin，ES module / fetch 走 CORS，必须放行
	if got := entry.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("缺少 CORS 头，实际 %q", got)
	}

	if asset := fetch(webWallpaperRoutePrefix + "js/app.js"); asset.Code != http.StatusOK {
		t.Fatalf("子目录资源应返回 200，实际 %d", asset.Code)
	}
	// 直接请求 index.html 现在从字节直出(不再被 http.ServeFile 301 到目录路径,
	// 子目录里的 index.html 也不会再被带死);但规范入口仍是 __entry 别名——
	// Wails 的资产层对以 /index.html 结尾的路径有 runtime 注入行为,别名可以绕开
	if direct := fetch(webWallpaperRoutePrefix + "index.html"); direct.Code != http.StatusOK {
		t.Fatalf("index.html 直连应 200,实际 %d", direct.Code)
	}
	if strings.Contains(fetch(webWallpaperRoutePrefix+"index.html").Body.String(), "__wePolyfillInstalled") {
		t.Fatal("直连入口(非别名)不应重复注入 polyfill")
	}

	for _, bad := range []string{
		webWallpaperRoutePrefix + "../project.json",
		webWallpaperRoutePrefix + "..%2f..%2fproject.json",
		webWallpaperRoutePrefix,
	} {
		if rec := fetch(bad); rec.Code == http.StatusOK {
			t.Fatalf("越界/目录请求不该成功：%s → %d", bad, rec.Code)
		}
	}
}

// TestWebWallpaperRouteWithoutTarget 未选中网页壁纸时整条路由 404。
func TestWebWallpaperRouteWithoutTarget(t *testing.T) {
	setWebWallpaperTarget("", "", "", "")
	defer setWebWallpaperTarget("", "", "", "")

	rec := httptest.NewRecorder()
	NewWebWallpaperHandler().ServeHTTP(
		rec, httptest.NewRequest(http.MethodGet, webWallpaperRoutePrefix+webWallpaperEntryAlias, nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("无目标时应 404，实际 %d", rec.Code)
	}
}
