package auth

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// rewritingTransport 把请求的协议与主机改写到本地假服务器（保留路径）。
// 有了它就不必为了可测性把 officialLatestJsonUrl / mirrorLatestJsonUrl 改成变量，
// 同时保证测试永远不会访问 authlib-injector.yushi.moe 与 BMCLAPI 真实端点。
type rewritingTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t *rewritingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	clone.Host = ""
	return t.base.RoundTrip(clone)
}

// installFakeInjectorSource 把注入器下载客户端指向本地假服务器；测试结束还原。
func installFakeInjectorSource(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	original := injectorClient
	injectorClient = &http.Client{
		Timeout:   10 * time.Second,
		Transport: &rewritingTransport{target: target, base: http.DefaultTransport},
	}
	t.Cleanup(func() {
		injectorClient = original
		server.Close()
	})
	return server
}

// fakeInjectorSource 假注入器源：按路径提供 latest.json 与 jar，并统计请求次数。
type fakeInjectorSource struct {
	mu           sync.Mutex
	latestBody   string
	jarBody      []byte
	latestStatus int
	jarStatus    int
	jarHits      int
	latestPaths  []string
}

func newFakeInjectorSource() *fakeInjectorSource { return &fakeInjectorSource{} }

func (f *fakeInjectorSource) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "latest.json") {
			f.latestPaths = append(f.latestPaths, r.URL.Path)
			status := f.latestStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(f.latestBody))
			return
		}
		f.jarHits++
		status := f.jarStatus
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write(f.jarBody)
	}
}

// counts 返回 (jar 请求次数, latest.json 请求次数)。
func (f *fakeInjectorSource) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jarHits, len(f.latestPaths)
}

// latestJsonFor 拼一份现行格式的 latest.json（download_url + checksums.sha256）。
func latestJsonFor(version, downloadUrl, sha256Hex string) string {
	if sha256Hex == "" {
		return fmt.Sprintf(`{"build_number":56,"version":%q,"download_url":%q}`, version, downloadUrl)
	}
	return fmt.Sprintf(`{"build_number":56,"version":%q,"download_url":%q,"checksums":{"sha256":%q}}`,
		version, downloadUrl, sha256Hex)
}

// TestEnsureInjectorRejectsEmptyDirectory 空目录参数必须立刻报错。
// 防的回归：传空串时把注入器下载到进程当前目录（或盘根），并在启动日志里
// 留下一个找不到的 javaagent 路径。
func TestEnsureInjectorRejectsEmptyDirectory(t *testing.T) {
	for _, directory := range []string{"", "   "} {
		if _, err := EnsureInjector(context.Background(), directory, nil); err == nil {
			t.Fatalf("EnsureInjector(%q) 应当报错", directory)
		} else if !strings.Contains(err.Error(), "minecraftDirectory 不能为空") {
			t.Fatalf("错误 = %v", err)
		}
	}
}

// TestEnsureInjectorDownloadsAndReusesCache 下载成功后写入 <mcDir>/authlib-injector/，
// 且哈希一致时复用缓存不再下载。
// 防的回归：每次启动都重新下载注入器（离线/限速时皮肤站账号直接启动不了），
// 以及把 jar 留在 .download 临时文件里，javaagent 路径指向不存在的文件。
func TestEnsureInjectorDownloadsAndReusesCache(t *testing.T) {
	jar := []byte("fake authlib-injector jar v1.2.8")
	sum := sha256.Sum256(jar)
	source := newFakeInjectorSource()
	source.jarBody = jar
	source.latestBody = latestJsonFor("1.2.8", "https://authlib-injector.yushi.moe/artifact/56/authlib-injector-1.2.8.jar",
		hex.EncodeToString(sum[:]))
	installFakeInjectorSource(t, source.handler())

	minecraftDirectory := t.TempDir()
	var messages []string
	log := func(message string) { messages = append(messages, message) }

	path, err := EnsureInjector(context.Background(), minecraftDirectory, log)
	if err != nil {
		t.Fatalf("下载注入器失败：%v", err)
	}
	want := filepath.Join(minecraftDirectory, "authlib-injector", "authlib-injector-1.2.8.jar")
	if path != want {
		t.Fatalf("注入器路径 = %q，期望 %q", path, want)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取注入器失败：%v", err)
	}
	if string(content) != string(jar) {
		t.Fatalf("注入器内容 = %q", content)
	}
	if _, err := os.Stat(path + ".download"); err == nil {
		t.Fatal("不应残留 .download 临时文件")
	}
	jarHits, latestHits := source.counts()
	if jarHits != 1 || latestHits != 1 {
		t.Fatalf("首次下载请求次数 = jar %d / latest %d，期望各 1", jarHits, latestHits)
	}
	if !containsMessage(messages, "下载完成") {
		t.Fatalf("启动日志应提示下载完成：%v", messages)
	}

	// 第二次调用：哈希一致 → 复用缓存，不再下载
	messages = nil
	pathAgain, err := EnsureInjector(context.Background(), minecraftDirectory, log)
	if err != nil {
		t.Fatalf("复用缓存失败：%v", err)
	}
	if pathAgain != path {
		t.Fatalf("复用路径 = %q，期望 %q", pathAgain, path)
	}
	jarHitsAgain, latestHitsAgain := source.counts()
	if jarHitsAgain != 1 {
		t.Fatalf("缓存命中时不应重新下载，jar 请求次数 = %d", jarHitsAgain)
	}
	// latest.json 每次都要读（版本号与哈希只在那里），但哈希一致就不重新下载 jar
	if latestHitsAgain != 2 {
		t.Fatalf("latest.json 请求次数 = %d，期望 2（每次调用读一次元数据）", latestHitsAgain)
	}
	if !containsMessage(messages, "已就绪") {
		t.Fatalf("启动日志应提示已就绪：%v", messages)
	}
}

// TestEnsureInjectorRedownloadsCorruptedCache 本地缓存哈希不匹配时必须删掉重下。
// 防的回归：上次下载被中途打断留下的半截 jar 被当成可用注入器，
// 游戏启动时 javaagent 加载失败（表现为「莫名其妙启动不了」）。
func TestEnsureInjectorRedownloadsCorruptedCache(t *testing.T) {
	jar := []byte("good jar content")
	sum := sha256.Sum256(jar)
	source := newFakeInjectorSource()
	source.jarBody = jar
	source.latestBody = latestJsonFor("1.2.8", "https://authlib-injector.yushi.moe/artifact/56/authlib-injector-1.2.8.jar",
		hex.EncodeToString(sum[:]))
	installFakeInjectorSource(t, source.handler())

	minecraftDirectory := t.TempDir()
	installDirectory := filepath.Join(minecraftDirectory, "authlib-injector")
	if err := os.MkdirAll(installDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(installDirectory, "authlib-injector-1.2.8.jar")
	if err := os.WriteFile(broken, []byte("half downloaded"), 0o644); err != nil {
		t.Fatal(err)
	}

	var messages []string
	path, err := EnsureInjector(context.Background(), minecraftDirectory,
		func(message string) { messages = append(messages, message) })
	if err != nil {
		t.Fatalf("缓存损坏后应当重新下载成功：%v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(jar) {
		t.Fatalf("注入器内容 = %q，期望重新下载后的完整内容", content)
	}
	if !containsMessage(messages, "缓存校验失败") {
		t.Fatalf("启动日志应提示缓存校验失败：%v", messages)
	}
}

// TestEnsureInjectorFailsWhenAllSourcesFail 两个下载源都失败且本地没有可用 jar 时
// 必须报错（而不是返回一个不存在的路径）。
// 防的回归：忽略下载错误把空路径交给启动管线，游戏侧报的是
// 「找不到 javaagent」这种和真实原因无关的错误。
func TestEnsureInjectorFailsWhenAllSourcesFail(t *testing.T) {
	source := newFakeInjectorSource()
	source.latestStatus = http.StatusInternalServerError
	source.latestBody = "boom"
	installFakeInjectorSource(t, source.handler())

	var messages []string
	path, err := EnsureInjector(context.Background(), t.TempDir(),
		func(message string) { messages = append(messages, message) })
	if err == nil {
		t.Fatalf("全部下载源失败时应当报错，实际返回 %q", path)
	}
	if !strings.Contains(err.Error(), "获取 authlib-injector 注入器失败") {
		t.Fatalf("错误 = %v", err)
	}
	if path != "" {
		t.Fatalf("失败时路径应为空，实际 %q", path)
	}
	if len(messages) < 2 {
		t.Fatalf("两个下载源各应记一条日志，实际 %v", messages)
	}
	_, latestHits := source.counts()
	if latestHits != 2 {
		t.Fatalf("官方源与镜像源各应被尝试一次，实际 %d 次", latestHits)
	}
}

// TestEnsureInjectorFallsBackToExistingJar 全部下载源失败时必须回退到本地已有的
// 旧版注入器。
// 防的回归：只有官方源可用性判断、没有本地回退——皮肤站账号在官方源被墙时
// 完全无法启动，哪怕本地就躺着一个可用的旧版本。
func TestEnsureInjectorFallsBackToExistingJar(t *testing.T) {
	source := newFakeInjectorSource()
	source.latestStatus = http.StatusBadGateway
	installFakeInjectorSource(t, source.handler())

	minecraftDirectory := t.TempDir()
	installDirectory := filepath.Join(minecraftDirectory, "authlib-injector")
	if err := os.MkdirAll(installDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(installDirectory, "authlib-injector-1.2.0.jar")
	if err := os.WriteFile(existing, []byte("old jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 干扰项：目录里的其它文件不应被当成注入器
	if err := os.WriteFile(filepath.Join(installDirectory, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var messages []string
	path, err := EnsureInjector(context.Background(), minecraftDirectory,
		func(message string) { messages = append(messages, message) })
	if err != nil {
		t.Fatalf("应当回退到本地旧版注入器：%v", err)
	}
	if path != existing {
		t.Fatalf("回退路径 = %q，期望 %q", path, existing)
	}
	if !containsMessage(messages, "使用本地已有的") {
		t.Fatalf("启动日志应提示使用了本地旧版：%v", messages)
	}
}

// TestEnsureInjectorRejectsIncompleteLatestJson latest.json 缺少下载地址或版本号时
// 必须报错。
// 防的回归：早期格式解析遗漏（url/version 取值恒为空）时静默拼出
// authlib-injector-.jar 这种文件名并把它当成下载成功。
func TestEnsureInjectorRejectsIncompleteLatestJson(t *testing.T) {
	source := newFakeInjectorSource()
	source.latestBody = `{"build_number":56,"download_url":"https://example.invalid/a.jar"}`
	installFakeInjectorSource(t, source.handler())

	_, err := EnsureInjector(context.Background(), t.TempDir(), nil)
	if err == nil {
		t.Fatal("缺少 version 时应当报错")
	}
	if !strings.Contains(err.Error(), "latest.json 缺少下载地址或 version 字段") {
		t.Fatalf("错误 = %v", err)
	}
}

// TestEnsureInjectorHonorsContextCancellation 取消（用户关窗口）后必须立刻返回
// ctx 错误而不是继续尝试下一个源。
// 防的回归：取消后仍去请求镜像源，退出时多等一整个网络超时。
func TestEnsureInjectorHonorsContextCancellation(t *testing.T) {
	source := newFakeInjectorSource()
	source.latestBody = latestJsonFor("1.2.8", "https://example.invalid/a.jar", "")
	installFakeInjectorSource(t, source.handler())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := EnsureInjector(ctx, t.TempDir(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("错误 = %v，期望 context.Canceled", err)
	}
}

// TestFetchLatestArtifactHttpError latest.json 非 2xx 时的错误信息带状态码。
// 防的回归：把 404/500 当成「解析结果为空」，排查时看不到真实原因。
func TestFetchLatestArtifactHttpError(t *testing.T) {
	installFakeInjectorSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	_, err := fetchLatestArtifact(context.Background(), officialLatestJsonUrl)
	if err == nil {
		t.Fatal("404 应当报错")
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("错误 = %v，期望带状态码", err)
	}
}

// TestMatchesHashAndHashMatches 哈希校验的取值优先级与大小写容忍。
// 防的回归：sha256 优先顺序被打乱（用 sha1 校验 sha256 产物）、
// 十六进制大小写不一致被判成校验失败（每次启动都重新下载 jar）。
func TestMatchesHashAndHashMatches(t *testing.T) {
	directory := t.TempDir()
	filePath := filepath.Join(directory, "artifact.jar")
	content := []byte("hash me")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum256 := sha256.Sum256(content)
	sum1 := sha1.Sum(content)

	cases := []struct {
		name     string
		artifact *AuthlibArtifact
		want     bool
		wantErr  bool
	}{
		{
			name: "sha256 匹配",
			artifact: &AuthlibArtifact{Checksums: struct {
				Sha256 string `json:"sha256"`
				Sha1   string `json:"sha1"`
			}{Sha256: hex.EncodeToString(sum256[:])}},
			want: true,
		},
		{
			name: "sha256 大写同样匹配",
			artifact: &AuthlibArtifact{Checksums: struct {
				Sha256 string `json:"sha256"`
				Sha1   string `json:"sha1"`
			}{Sha256: strings.ToUpper(hex.EncodeToString(sum256[:]))}},
			want: true,
		},
		{
			name: "sha256 不匹配（即使 sha1 对）",
			artifact: &AuthlibArtifact{
				Sha256: strings.Repeat("0", 64),
				Checksums: struct {
					Sha256 string `json:"sha256"`
					Sha1   string `json:"sha1"`
				}{Sha1: hex.EncodeToString(sum1[:])},
			},
			want: false,
		},
		{
			name: "只有 sha1 时用 sha1 校验",
			artifact: &AuthlibArtifact{Checksums: struct {
				Sha256 string `json:"sha256"`
				Sha1   string `json:"sha1"`
			}{Sha1: hex.EncodeToString(sum1[:])}},
			want: true,
		},
		{
			name:     "没有任何哈希时视为不匹配",
			artifact: &AuthlibArtifact{},
			want:     false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := matchesHash(filePath, testCase.artifact)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("错误 = %v", err)
			}
			if got != testCase.want {
				t.Fatalf("matchesHash() = %v，期望 %v", got, testCase.want)
			}
		})
	}

	// hasKnownHash 只回答「声明了哈希吗」，与是否匹配无关
	withSha256 := &AuthlibArtifact{}
	withSha256.Checksums.Sha256 = hex.EncodeToString(sum256[:])
	if !hasKnownHash(withSha256) {
		t.Fatal("声明了 sha256 时应视为有已知哈希")
	}
	withSha1 := &AuthlibArtifact{}
	withSha1.Checksums.Sha1 = hex.EncodeToString(sum1[:])
	if !hasKnownHash(withSha1) {
		t.Fatal("声明了 sha1 时应视为有已知哈希")
	}
	if hasKnownHash(&AuthlibArtifact{}) {
		t.Fatal("未声明任何哈希时不应视为有已知哈希")
	}

	if _, err := matchesHash(filepath.Join(directory, "missing.jar"), &AuthlibArtifact{}); err == nil {
		t.Fatal("文件不存在时应当返回错误")
	}
	// 只裁剪 expected 一侧的首尾空白（实际值来自本地计算结果，不会带空白）
	if !hashMatches("abc", " ABC ") {
		t.Fatal("哈希比较应忽略大小写并裁剪期望值的首尾空白")
	}
	if hashMatches("abc", "abd") {
		t.Fatal("不同哈希不应相等")
	}
}

// TestEnsureInjectorSkipsDownloadWithoutKnownHash latest.json 没有声明哈希时，
// 只要本地已有同名 jar 就直接复用（无法校验，避免每次启动重复下载）。
// 防的回归：无哈希时反复重新下载，弱网环境每次启动都要等完整下载。
func TestEnsureInjectorSkipsDownloadWithoutKnownHash(t *testing.T) {
	source := newFakeInjectorSource()
	source.latestBody = latestJsonFor("1.2.8", "https://example.invalid/authlib-injector-1.2.8.jar", "")
	installFakeInjectorSource(t, source.handler())

	minecraftDirectory := t.TempDir()
	installDirectory := filepath.Join(minecraftDirectory, "authlib-injector")
	if err := os.MkdirAll(installDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	cached := filepath.Join(installDirectory, "authlib-injector-1.2.8.jar")
	if err := os.WriteFile(cached, []byte("cached jar"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := EnsureInjector(context.Background(), minecraftDirectory, nil)
	if err != nil {
		t.Fatalf("应当直接复用本地缓存：%v", err)
	}
	if path != cached {
		t.Fatalf("路径 = %q，期望 %q", path, cached)
	}
	if jarHits, _ := source.counts(); jarHits != 0 {
		t.Fatalf("无哈希且缓存已存在时不应下载，jar 请求次数 = %d", jarHits)
	}
}

// containsMessage 启动日志里是否包含某段文案（日志只用于断言行为，不当输出用）。
func containsMessage(messages []string, fragment string) bool {
	for _, message := range messages {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}
