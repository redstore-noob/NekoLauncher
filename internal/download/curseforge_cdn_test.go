package download

// CurseForge CDN 鉴权与失败翻译的用例。
//
// 背景：官方 2026-06 公告要求 CDN 直链带 x-api-key（2026-07-16 起强制，缺失返回 401）。
// 本启动器此前只在问 API 时带 Key，取文件那一步没带 —— 也就是"解析地址时带了、
// 下载时没带"。这组用例守住这一点，以及"裸 401/404 必须翻译成可执行提示"。

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// withCurseForgeKey 临时设置 CDN Key 钩子，用例结束自动还原。
func withCurseForgeKey(t *testing.T, key string) {
	t.Helper()
	previous := CurseForgeCDNKeyHook
	CurseForgeCDNKeyHook = func() string { return key }
	t.Cleanup(func() { CurseForgeCDNKeyHook = previous })
}

func TestIsCurseForgeCDNHost(t *testing.T) {
	cases := map[string]bool{
		"https://edge.forgecdn.net/files/8965/84/jei.jar":         true,
		"https://mediafilez.forgecdn.net/files/8965/84/jei.jar":   true,
		"https://forgecdn.net/files/1/2/a.jar":                    true,
		"https://cdn.modrinth.com/data/A/b.jar":                   false,
		"https://www.curseforge.com/api/v1/mods/1/files/2/downlo": false,
		"https://piston-data.mojang.com/v1/objects/aa/client.jar": false,
		"":          false,
		"not a url": false,
	}
	for rawURL, want := range cases {
		if got := isCurseForgeCDNHost(rawURL); got != want {
			t.Errorf("isCurseForgeCDNHost(%q) = %v，期望 %v", rawURL, got, want)
		}
	}
}

func TestIsCurseForgePublicDownloadURL(t *testing.T) {
	cases := map[string]bool{
		"https://www.curseforge.com/api/v1/mods/238222/files/123456/download": true,
		"https://www.curseforge.com/api/v1/mods/238222/files/123456":          false,
		"https://edge.forgecdn.net/files/8965/84/jei.jar":                     false,
		"https://cdn.modrinth.com/data/A/b.jar":                               false,
	}
	for rawURL, want := range cases {
		if got := isCurseForgePublicDownloadURL(rawURL); got != want {
			t.Errorf("isCurseForgePublicDownloadURL(%q) = %v，期望 %v", rawURL, got, want)
		}
	}
}

// TestApplyCurseForgeCDNAuthSendsKey 防的回归：CDN 请求不带 x-api-key。
// 强制鉴权后这类请求会 401，用户看到的是"下载失败"而不知道要配 Key。
func TestApplyCurseForgeCDNAuthSendsKey(t *testing.T) {
	withCurseForgeKey(t, "  test-key  ")

	req, err := http.NewRequest("GET", "https://edge.forgecdn.net/files/8965/84/jei.jar", nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCurseForgeCDNAuth(req)

	if got := req.Header.Get("x-api-key"); got != "test-key" {
		t.Fatalf("x-api-key = %q，期望去掉首尾空格的 test-key", got)
	}
}

// TestApplyCurseForgeCDNAuthSkipsOtherHosts 防的回归：把 CurseForge 的
// 凭据发给了无关主机（Modrinth CDN / Mojang 等）—— 那是没必要的凭据外泄。
func TestApplyCurseForgeCDNAuthSkipsOtherHosts(t *testing.T) {
	withCurseForgeKey(t, "test-key")

	for _, rawURL := range []string{
		"https://cdn.modrinth.com/data/A/b.jar",
		"https://piston-data.mojang.com/v1/objects/aa/client.jar",
		"https://www.curseforge.com/api/v1/mods/1/files/2/download",
	} {
		req, err := http.NewRequest("GET", rawURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		applyCurseForgeCDNAuth(req)
		if got := req.Header.Get("x-api-key"); got != "" {
			t.Errorf("%s 不应带上 x-api-key，实际 %q", rawURL, got)
		}
	}
}

// TestApplyCurseForgeCDNAuthWithoutKey 未配置 Key 时不加头（而不是加个空头）。
func TestApplyCurseForgeCDNAuthWithoutKey(t *testing.T) {
	withCurseForgeKey(t, "")

	req, err := http.NewRequest("GET", "https://edge.forgecdn.net/files/8965/84/jei.jar", nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCurseForgeCDNAuth(req)
	if got := req.Header.Get("x-api-key"); got != "" {
		t.Fatalf("未配置 Key 时不应带 x-api-key，实际 %q", got)
	}
}

// TestCurseForgeDownloadErrorTranslates 防的回归：CurseForge 的 401/403/404
// 以裸 HTTP 文案抛给用户，用户无法从中判断"要配 Key"还是"作者禁止分发"。
func TestCurseForgeDownloadErrorTranslates(t *testing.T) {
	const cdnURL = "https://edge.forgecdn.net/files/8965/84/jei.jar"
	const publicURL = "https://www.curseforge.com/api/v1/mods/238222/files/123456/download"

	t.Run("未配 Key 时 401 提示去配 Key", func(t *testing.T) {
		withCurseForgeKey(t, "")
		err := curseForgeDownloadError(&httpStatusError{StatusCode: http.StatusUnauthorized}, cdnURL)
		if err == nil {
			t.Fatal("401 应被翻译")
		}
		if !contains(err.Error(), "401") || !contains(err.Error(), "API Key") {
			t.Errorf("提示应点明 401 与 API Key：%s", err.Error())
		}
	})

	t.Run("配了 Key 仍 401 说明 Key 无效", func(t *testing.T) {
		withCurseForgeKey(t, "k")
		err := curseForgeDownloadError(&httpStatusError{StatusCode: http.StatusUnauthorized}, cdnURL)
		if err == nil {
			t.Fatal("401 应被翻译")
		}
		if !contains(err.Error(), "无效") {
			t.Errorf("应指出 Key 无效/失效：%s", err.Error())
		}
	})

	t.Run("配了 Key 的 403 说明作者禁止分发", func(t *testing.T) {
		withCurseForgeKey(t, "k")
		err := curseForgeDownloadError(&httpStatusError{StatusCode: http.StatusForbidden}, cdnURL)
		if err == nil {
			t.Fatal("403 应被翻译")
		}
		if !contains(err.Error(), "第三方分发") {
			t.Errorf("应指出作者禁止第三方分发：%s", err.Error())
		}
	})

	t.Run("未配 Key 时公开端点 404 引导配 Key", func(t *testing.T) {
		withCurseForgeKey(t, "")
		err := curseForgeDownloadError(&httpStatusError{StatusCode: http.StatusNotFound}, publicURL)
		if err == nil {
			t.Fatal("公开端点的 404 应被翻译（裸 404 会被误读成文件不存在）")
		}
		if !contains(err.Error(), "API Key") {
			t.Errorf("应引导配置 API Key：%s", err.Error())
		}
	})

	t.Run("CDN 上的 404 说明文件可能已删除", func(t *testing.T) {
		withCurseForgeKey(t, "k")
		err := curseForgeDownloadError(&httpStatusError{StatusCode: http.StatusNotFound}, cdnURL)
		if err == nil {
			t.Fatal("CDN 404 应被翻译")
		}
		if !contains(err.Error(), "删除") && !contains(err.Error(), "分发") {
			t.Errorf("应说明可能已删除或禁止分发：%s", err.Error())
		}
	})

	t.Run("包装后的状态错误同样识别", func(t *testing.T) {
		withCurseForgeKey(t, "")
		wrapped := fmt.Errorf("下载失败：%w", errors.Join(errors.New("x"),
			&httpStatusError{StatusCode: http.StatusUnauthorized}))
		if err := curseForgeDownloadError(wrapped, cdnURL); err == nil {
			t.Fatal("包装过的 401 也应被识别")
		}
	})

	t.Run("非 CurseForge 地址与其它状态码不翻译", func(t *testing.T) {
		withCurseForgeKey(t, "")
		if err := curseForgeDownloadError(&httpStatusError{StatusCode: http.StatusNotFound},
			"https://cdn.modrinth.com/data/A/b.jar"); err != nil {
			t.Errorf("Modrinth 地址不应走 CurseForge 翻译：%v", err)
		}
		if err := curseForgeDownloadError(&httpStatusError{StatusCode: http.StatusInternalServerError},
			cdnURL); err != nil {
			t.Errorf("500 属可重试失败，不应被翻译成永久错误：%v", err)
		}
		if err := curseForgeDownloadError(nil, cdnURL); err != nil {
			t.Errorf("nil 错误不应产生提示：%v", err)
		}
	})
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
