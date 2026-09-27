package tools

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"nekolauncher/internal/info"
)

// TestPathsEqual 防的回归：把同一个目录的两种写法判成不同目录，
// 于是"当前实例目录 == 默认目录"这类判断永远为假，重复下载/重复建骨架。
func TestPathsEqual(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "versions", "1.21.1")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("建临时目录失败：%v", err)
	}

	cases := []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{name: "完全相同", left: nested, right: nested, want: true},
		{name: "尾部分隔符被忽略", left: root + string(os.PathSeparator), right: root, want: true},
		{name: "正斜杠与反斜杠等价", left: root + "/", right: root, want: true},
		{name: "点号被规范化", left: filepath.Join(root, ".", "versions", "1.21.1"), right: nested, want: true},
		{name: "点点回到父目录", left: filepath.Join(nested, "..", "1.21.1"), right: nested, want: true},
		{name: "不同目录不相等", left: nested, right: root, want: false},
		{name: "左侧为空", left: "", right: root, want: false},
		{name: "右侧为空", left: root, right: "", want: false},
		{name: "两侧都是空白", left: "   ", right: "\t", want: false},
		{name: "一侧是空白", left: "  ", right: root, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PathsEqual(tc.left, tc.right); got != tc.want {
				t.Fatalf("PathsEqual(%q, %q) = %v，期望 %v", tc.left, tc.right, got, tc.want)
			}
		})
	}
}

// TestPathsEqualDoesNotEscapeWithDotDot 防的回归：只用字符串前缀判断"路径在根目录内"，
// 于是 "<root>/../outside" 被误认为位于 root 之下（覆盖安装/清理会写到目录外）。
func TestPathsEqualDoesNotEscapeWithDotDot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("建临时目录失败：%v", err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside")

	// ".." 规范化后确实指到 root 的兄弟目录，这是正确行为，先钉住
	if !PathsEqual(filepath.Join(root, "..", "outside"), outside) {
		t.Fatalf(".. 规范化后应等于 %q", outside)
	}
	if PathsEqual(filepath.Join(root, "..", "outside"), filepath.Join(root, "outside")) {
		t.Fatal(".. 越界后的路径不应与 root 下的同名路径判为相等")
	}
	if PathsEqual(filepath.Join(root, "..", ".."), root) {
		t.Fatal("越界两层后不应与 root 判为相等")
	}
}

// TestPathsEqualIsCaseInsensitiveOnWindows 防的回归：Windows 上把
// "C:\\Users\\A\\.minecraft" 与 "c:\\users\\a\\.minecraft" 判成不同目录。
func TestPathsEqualIsCaseInsensitiveOnWindows(t *testing.T) {
	root := t.TempDir()
	upper := strings.ToUpper(root)

	if runtime.GOOS == "windows" {
		if !PathsEqual(upper, root) {
			t.Fatalf("Windows 下应忽略大小写：%q 与 %q", upper, root)
		}
		return
	}
	// 非 Windows：大小写敏感，两个不同的路径不该被判为同一个
	if upper != root && PathsEqual(upper, root) {
		t.Fatalf("非 Windows 平台应区分大小写：%q 与 %q", upper, root)
	}
}

// TestPathsEqualResolvesRelativeAgainstWorkingDirectory 防的回归：
// 相对路径与绝对路径混比时一律返回 false，导致"当前目录就是实例目录"判断失效。
func TestPathsEqualResolvesRelativeAgainstWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录失败：%v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("切目录失败：%v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(workingDirectory) })

	if !PathsEqual(".", root) {
		t.Fatalf("当前目录 \".\" 应等于 %q", root)
	}
}

// TestPathsEqualFoldMatchesPathsEqual PathsEqualFold 是给比较器（slices.SortFunc 等）
// 用的别名，防的回归：两者语义分叉，排序结果与相等判断互相矛盾。
func TestPathsEqualFoldMatchesPathsEqual(t *testing.T) {
	root := t.TempDir()
	pairs := [][2]string{
		{root, root},
		{root, strings.ToUpper(root)},
		{root, root + string(os.PathSeparator)},
		{"", ""},
		{root, ""},
	}
	for _, pair := range pairs {
		if got, want := PathsEqualFold(pair[0], pair[1]), PathsEqual(pair[0], pair[1]); got != want {
			t.Fatalf("PathsEqualFold(%q, %q) = %v，PathsEqual = %v，两者语义必须一致",
				pair[0], pair[1], got, want)
		}
	}
}

// TestUserHomeDir 防的回归：主目录探测失败时返回带错误的空串而不是回落，
// 使默认 .minecraft 目录变成相对路径 "AppData/Roaming/.minecraft"。
func TestUserHomeDir(t *testing.T) {
	fakeHome := t.TempDir()
	// Windows 取 USERPROFILE，类 Unix 取 HOME：两个都指向临时目录，确保零污染
	t.Setenv("USERPROFILE", fakeHome)
	t.Setenv("HOME", fakeHome)

	got := UserHomeDir()
	if got == "" {
		t.Fatal("主目录可用时不应返回空串")
	}
	if !PathsEqual(got, fakeHome) {
		t.Fatalf("UserHomeDir() = %q，期望 %q", got, fakeHome)
	}
}

// TestSharedHTTPClientUsesLauncherUserAgent 防的回归：
// 共享客户端漏设 User-Agent，Mojang/Modrinth 侧收到 Go-http-client 默认标识；
// 同时也防"调用方自带的 UA 覆盖了启动器标识"。
func TestSharedHTTPClientUsesLauncherUserAgent(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen = request.Header.Get("User-Agent")
		_, _ = writer.Write([]byte("ok"))
	}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("构造请求失败：%v", err)
	}
	request.Header.Set("User-Agent", "someone-else/9.9")

	response, err := SharedHTTPClient.Do(request)
	if err != nil {
		t.Fatalf("请求测试服务器失败：%v", err)
	}
	defer response.Body.Close()
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatalf("读响应失败：%v", err)
	}

	want := "NekoLauncher/" + info.Version()
	if seen != want {
		t.Fatalf("服务端收到的 User-Agent = %q，期望 %q", seen, want)
	}
	// 传输层用的是 Clone，不应把调用方原始请求改脏
	if got := request.Header.Get("User-Agent"); got != "someone-else/9.9" {
		t.Fatalf("原始请求的 User-Agent 被就地改写为 %q", got)
	}
}

// TestSharedHTTPClientHasTimeout 防的回归：共享客户端超时被去掉/设成 0，
// 版本清单请求挂在后台线程上，GUI 一直转圈且没有任何报错。
func TestSharedHTTPClientHasTimeout(t *testing.T) {
	if SharedHTTPClient == nil {
		t.Fatal("SharedHTTPClient 不应为 nil")
	}
	if SharedHTTPClient.Timeout != 15*time.Second {
		t.Fatalf("共享客户端超时 = %v，期望 15s（改这里要同步确认前端等待逻辑）", SharedHTTPClient.Timeout)
	}
	if SharedHTTPClient.Transport == nil {
		t.Fatal("共享客户端必须带 userAgentTransport，否则 User-Agent 契约失效")
	}
}

// TestUserAgentTransportOverridesHeader 直接验证传输层：
// 防的回归：Header.Set 写成只在缺失时设置，导致调用方自带的 UA 一路带到服务端。
func TestUserAgentTransportOverridesHeader(t *testing.T) {
	var received http.Header
	transport := &userAgentTransport{
		base: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			received = request.Header.Clone()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		}),
		ua: "NekoLauncher/test",
	}

	request, err := http.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	if err != nil {
		t.Fatalf("构造请求失败：%v", err)
	}
	request.Header.Set("User-Agent", "caller")
	request.Header.Set("Accept", "application/json")

	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip 失败：%v", err)
	}
	defer response.Body.Close()

	if got := received.Get("User-Agent"); got != "NekoLauncher/test" {
		t.Fatalf("下游收到的 User-Agent = %q，期望 %q（调用方自带的 UA 必须被覆盖）", got, "NekoLauncher/test")
	}
	if got := received.Get("Accept"); got != "application/json" {
		t.Fatalf("其它请求头应原样透传，Accept = %q", got)
	}
	if got := request.Header.Get("User-Agent"); got != "caller" {
		t.Fatalf("原始请求不应被就地改写，User-Agent = %q", got)
	}
}

// roundTripperFunc 把函数适配成 http.RoundTripper，供上面的传输层测试使用。
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
