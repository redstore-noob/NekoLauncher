package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"nekolauncher/internal/config"
)

// TestMain 在跑任何测试之前把 USERPROFILE 指向一次性临时目录。
// 防的回归：config.DefaultStorageDirectory()（account.secret.key 的落盘位置）与
// internal/logs 都在「调用时」读 USERPROFILE，一旦测试触发首次写入，密钥文件与
// 日志就会落到真实用户目录（C:\Users\<用户>\NekoLauncher）。这条防线保证测试进程
// 无论怎么跑都不会在真实用户目录下留下任何文件。
func TestMain(m *testing.M) {
	original := os.Getenv("USERPROFILE")
	home, err := os.MkdirTemp("", "nya-auth-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("USERPROFILE", home)
	code := m.Run()
	_ = os.Setenv("USERPROFILE", original)
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// useTempAuthStorage 把启动器存储目录指到临时目录，避免测试读写真实用户配置。
func useTempAuthStorage(t *testing.T) string {
	t.Helper()
	original := config.StorageDirectory()
	directory := t.TempDir()
	if err := config.SetStorageDirectory(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.SetStorageDirectory(original) })
	return directory
}

// ---------------------------------------------------------------------------
// 认证链路端点假服务器
// ---------------------------------------------------------------------------

// pointMicrosoftEndpointsAt 把正版认证链路的全部端点指向假服务器，测试结束自动还原。
// 防的回归：测试误连 login.microsoftonline.com / xboxlive / minecraftservices 等
// 真实端点（会触发真实设备码申请与真实账号探测）。
func pointMicrosoftEndpointsAt(t *testing.T, base string) {
	t.Helper()
	original := []struct {
		target *string
		value  string
	}{
		{&deviceCodeEndpoint, deviceCodeEndpoint},
		{&tokenEndpoint, tokenEndpoint},
		{&xboxLiveAuthenticateURL, xboxLiveAuthenticateURL},
		{&xstsAuthorizeURL, xstsAuthorizeURL},
		{&minecraftLoginURL, minecraftLoginURL},
		{&minecraftProfileURL, minecraftProfileURL},
	}
	deviceCodeEndpoint = base + "/devicecode"
	tokenEndpoint = base + "/token"
	xboxLiveAuthenticateURL = base + "/user/authenticate"
	xstsAuthorizeURL = base + "/xsts/authorize"
	minecraftLoginURL = base + "/authentication/login_with_xbox"
	minecraftProfileURL = base + "/minecraft/profile"
	t.Cleanup(func() {
		for _, item := range original {
			*item.target = item.value
		}
	})
}

// httpRecord 假服务器收到的一条请求快照。
type httpRecord struct {
	Method string
	Path   string
	Header http.Header
	Body   string
}

// form 把请求体按表单解析（非表单请求返回空集合）。
func (r httpRecord) form() url.Values {
	values, _ := url.ParseQuery(r.Body)
	return values
}

// httpTrace 线程安全地记录假服务器收到的请求，供断言报文构造与调用次数。
// 微软认证链路与皮肤站的假服务器共用这套记录器。
type httpTrace struct {
	mu       sync.Mutex
	requests []httpRecord
}

func (tr *httpTrace) add(request *http.Request, body []byte) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.requests = append(tr.requests, httpRecord{
		Method: request.Method,
		Path:   request.URL.Path,
		Header: request.Header.Clone(),
		Body:   string(body),
	})
}

// count 指定路径收到的请求数。
func (tr *httpTrace) count(path string) int {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	total := 0
	for _, request := range tr.requests {
		if request.Path == path {
			total++
		}
	}
	return total
}

// paths 依次返回收到的全部请求路径（用于断言调用顺序）。
func (tr *httpTrace) paths() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	out := make([]string, 0, len(tr.requests))
	for _, request := range tr.requests {
		out = append(out, request.Path)
	}
	return out
}

// last 指定路径的最后一条请求；缺失时直接判失败（避免零值悄悄通过断言）。
func (tr *httpTrace) last(t *testing.T, path string) httpRecord {
	t.Helper()
	tr.mu.Lock()
	defer tr.mu.Unlock()
	for index := len(tr.requests) - 1; index >= 0; index-- {
		if tr.requests[index].Path == path {
			return tr.requests[index]
		}
	}
	t.Fatalf("假服务器没有收到 %s 请求，实际收到：%v", path, tr.requestPathsLocked())
	return httpRecord{}
}

func (tr *httpTrace) requestPathsLocked() []string {
	out := make([]string, 0, len(tr.requests))
	for _, request := range tr.requests {
		out = append(out, request.Path)
	}
	return out
}

// startFakeMicrosoft 启动假认证服务并把链路端点全部指向它。
// respond 拿到请求体原文，按路径返回响应；每个请求都会被记录。
func startFakeMicrosoft(
	t *testing.T,
	respond func(w http.ResponseWriter, r *http.Request, body string),
) (*httptest.Server, *httpTrace) {
	t.Helper()
	trace := &httpTrace{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		trace.add(r, body)
		respond(w, r, string(body))
	}))
	pointMicrosoftEndpointsAt(t, server.URL)
	t.Cleanup(server.Close)
	return server, trace
}

// msaResponse 假响应（状态码为 0 时按 200 处理）。
type msaResponse struct {
	Status int
	Body   string
}

const (
	fakeDeviceCodeBody = `{"device_code":"dc-1","user_code":"UC-1",` +
		`"verification_uri":"https://example.invalid/devicelogin","expires_in":900,"interval":1}`
	fakeTokenBody   = `{"access_token":"msa-access","refresh_token":"msa-refresh","expires_in":3600}`
	fakeXblBody     = `{"Token":"xbl-token","DisplayClaims":{"xui":[{"uhs":"uhs-1"}]}}`
	fakeXstsBody    = `{"Token":"xsts-token","DisplayClaims":{"xui":[{"xid":"1234567890123456"}]}}`
	fakeMcLoginBody = `{"access_token":"mc-token","expires_in":86400}`
	fakeProfileBody = `{"id":"069a79f4-44e9-4726-a5be-fca90e38aaf5","name":"NyaPlayer"}`
)

// msaChainHandler 一条「全部成功」的假链路；overrides 按路径替换单步响应。
func msaChainHandler(overrides map[string]msaResponse) func(http.ResponseWriter, *http.Request, string) {
	defaults := map[string]msaResponse{
		"/devicecode":                     {Body: fakeDeviceCodeBody},
		"/token":                          {Body: fakeTokenBody},
		"/user/authenticate":              {Body: fakeXblBody},
		"/xsts/authorize":                 {Body: fakeXstsBody},
		"/authentication/login_with_xbox": {Body: fakeMcLoginBody},
		"/minecraft/profile":              {Body: fakeProfileBody},
	}
	for path, override := range overrides {
		defaults[path] = override
	}
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		response, ok := defaults[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		status := response.Status
		if status == 0 {
			status = http.StatusOK
		}
		respondJSON(w, status, response.Body)
	}
}

// respondJSON 写回假 JSON 响应。
func respondJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// decodeJSON 解析断言用的 JSON 报文；解析失败直接判失败而不是静默返回空 map。
func decodeJSON(t *testing.T, body string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("响应/请求体不是合法 JSON：%v（原文 %s）", err, body)
	}
	return decoded
}

// buildFakeJwt 拼一个 payload 可控的假 JWT（不做签名，仅用于 payload 解析）。
// padded 为 true 时使用带 padding 的 base64url 编码。
func buildFakeJwt(t *testing.T, claims map[string]any, padded bool) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encoding := base64.RawURLEncoding
	if padded {
		encoding = base64.URLEncoding
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	return header + "." + encoding.EncodeToString(payload) + ".sig"
}

// flakyTransport 在真实（本地假服务器）往返之前注入若干次失败，
// 用于验证设备码轮询对瞬时网络错误的容忍度。
type flakyTransport struct {
	base http.RoundTripper
	// failures 路径 → 还需要注入失败的次数。
	failures map[string]int
	err      func(path string) error

	mu       sync.Mutex
	injected int
}

func (f *flakyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	f.mu.Lock()
	if remaining := f.failures[request.URL.Path]; remaining > 0 {
		f.failures[request.URL.Path] = remaining - 1
		f.injected++
		f.mu.Unlock()
		return nil, f.err(request.URL.Path)
	}
	f.mu.Unlock()
	return f.base.RoundTrip(request)
}

// injectedFailures 已注入的失败次数。
func (f *flakyTransport) injectedFailures() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.injected
}

// ---------------------------------------------------------------------------
// 设备码轮询等待注入
// ---------------------------------------------------------------------------

// realDeviceCodePollWait 生产实现：测试替换成假等待后仍可直接验证真实语义。
var realDeviceCodePollWait = deviceCodePollWait

// pollWaitRecorder 记录设备码轮询每次等待的间隔。
type pollWaitRecorder struct {
	mu        sync.Mutex
	intervals []time.Duration
}

func (r *pollWaitRecorder) record(interval time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.intervals = append(r.intervals, interval)
}

// snapshot 已记录的等待间隔副本。
func (r *pollWaitRecorder) snapshot() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]time.Duration, len(r.intervals))
	copy(out, r.intervals)
	return out
}

// stubPollWait 用可控等待替换真实轮询等待：记录每次间隔，并把真实睡眠压到
// idle 以内（避免 slow_down 的 +5 秒退避真的让测试等 6 秒）。
// 防的回归：只按墙上时间断言退避（慢机器上必抖），以及测试真的等满退避时长。
func stubPollWait(t *testing.T, idle time.Duration) *pollWaitRecorder {
	t.Helper()
	original := deviceCodePollWait
	recorder := &pollWaitRecorder{}
	deviceCodePollWait = func(ctx context.Context, interval time.Duration) error {
		recorder.record(interval)
		if err := ctx.Err(); err != nil {
			return err
		}
		if idle > 0 {
			time.Sleep(idle)
		}
		return nil
	}
	t.Cleanup(func() { deviceCodePollWait = original })
	return recorder
}
