package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestNewAuthenticatorClientIdResolution client_id 的取值优先级是
// 构造参数 > 环境变量覆盖 > 内置默认值，且默认 HTTP 客户端必须带超时。
// 防的回归：自建 Azure 应用（aka.ms/mce-reviewappid 审核用）覆盖失效被
// 静默忽略，用户只能拿到默认 ClientId 的 403 Invalid app registration；
// 以及默认客户端无超时导致网络挂起时登录界面永久卡住。
func TestNewAuthenticatorClientIdResolution(t *testing.T) {
	t.Setenv("NEKOLAUNCHER_MSA_CLIENT_ID", "")
	if got := MicrosoftClientIdOverride(); got != "" {
		t.Fatalf("未设置环境变量时覆盖值 = %q，期望空串", got)
	}
	if got := NewMicrosoftDeviceCodeAuthenticator("", nil).clientId; got != DefaultMicrosoftClientId {
		t.Fatalf("默认 clientId = %q，期望 %q", got, DefaultMicrosoftClientId)
	}

	t.Setenv("NEKOLAUNCHER_MSA_CLIENT_ID", "  env-client-id  ")
	if got := MicrosoftClientIdOverride(); got != "env-client-id" {
		t.Fatalf("环境变量覆盖值 = %q，期望裁剪首尾空格后的 env-client-id", got)
	}
	if got := NewMicrosoftDeviceCodeAuthenticator("", nil).clientId; got != "env-client-id" {
		t.Fatalf("环境变量未生效，clientId = %q", got)
	}
	if got := NewMicrosoftDeviceCodeAuthenticator("explicit-id", nil).clientId; got != "explicit-id" {
		t.Fatalf("构造参数应优先于环境变量，clientId = %q", got)
	}
	if got := NewMicrosoftDeviceCodeAuthenticator("   ", nil).clientId; got != "env-client-id" {
		t.Fatalf("空白构造参数应回落到环境变量，clientId = %q", got)
	}

	if got := NewMicrosoftDeviceCodeAuthenticator("x", nil).httpClient.Timeout; got != 30*time.Second {
		t.Fatalf("默认客户端超时 = %v，期望 30s（无超时会让登录界面永久卡住）", got)
	}
	custom := &http.Client{Timeout: time.Second}
	if got := NewMicrosoftDeviceCodeAuthenticator("x", custom).httpClient; got != custom {
		t.Fatal("注入的 HTTP 客户端必须原样使用（测试靠它挡掉真实网络）")
	}
}

// TestRequestDeviceCodeClampsIntervalAndExpiry 设备码响应里 expires_in / interval
// 缺失或非法时必须钳到 1 秒。
// 防的回归：interval 为 0 时轮询变成不带间隔的忙轮询（打爆令牌端点），
// expires_in 为 0 时 deadline 立刻过期，用户还没输完验证码就报超时。
func TestRequestDeviceCodeClampsIntervalAndExpiry(t *testing.T) {
	cases := []struct {
		name             string
		body             string
		wantExpiresIn    time.Duration
		wantPollInterval int
	}{
		{
			name:             "正常值原样保留",
			body:             `{"device_code":"dc","user_code":"UC","verification_uri":"v","expires_in":900,"interval":5}`,
			wantExpiresIn:    900 * time.Second,
			wantPollInterval: 5,
		},
		{
			name:             "缺省字段钳到 1 秒",
			body:             `{"device_code":"dc","user_code":"UC","verification_uri":"v"}`,
			wantExpiresIn:    time.Second,
			wantPollInterval: 1,
		},
		{
			name:             "负值同样钳到 1 秒",
			body:             `{"device_code":"dc","user_code":"UC","verification_uri":"v","expires_in":-30,"interval":-1}`,
			wantExpiresIn:    time.Second,
			wantPollInterval: 1,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, trace := startFakeMicrosoft(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
				respondJSON(w, http.StatusOK, testCase.body)
			})
			authenticator := NewMicrosoftDeviceCodeAuthenticator("test-client-id", nil)

			info, err := authenticator.requestDeviceCode(context.Background())
			if err != nil {
				t.Fatalf("请求设备码不应失败：%v", err)
			}
			if info.ExpiresIn != testCase.wantExpiresIn {
				t.Fatalf("ExpiresIn = %v，期望 %v", info.ExpiresIn, testCase.wantExpiresIn)
			}
			if info.PollIntervalSeconds != testCase.wantPollInterval {
				t.Fatalf("PollIntervalSeconds = %d，期望 %d",
					info.PollIntervalSeconds, testCase.wantPollInterval)
			}
			if info.DeviceCode != "dc" || info.UserCode != "UC" || info.VerificationUri != "v" {
				t.Fatalf("设备码字段解析结果 = %+v", info)
			}

			request := trace.last(t, "/devicecode")
			if request.Method != http.MethodPost {
				t.Fatalf("设备码请求方法 = %s，期望 POST", request.Method)
			}
			form := request.form()
			if got := form.Get("client_id"); got != "test-client-id" {
				t.Fatalf("client_id = %q，期望 test-client-id", got)
			}
			scope := form.Get("scope")
			if !strings.Contains(scope, "XboxLive.signin") || !strings.Contains(scope, "offline_access") {
				t.Fatalf("scope = %q，必须同时申请 XboxLive.signin 与 offline_access"+
					"（缺 offline_access 就拿不到 refresh_token，无法无感刷新）", scope)
			}
		})
	}
}

// TestRequestDeviceCodeFailureBranches 设备码申请失败必须报错而不是返回零值继续。
// 防的回归：把代理错误页/HTTP 500 当成设备码响应，用户界面显示空验证码并卡在等待。
func TestRequestDeviceCodeFailureBranches(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		body        string
		wantMessage string
	}{
		{
			name:        "HTTP 500",
			status:      http.StatusInternalServerError,
			body:        "<html>bad gateway</html>",
			wantMessage: "请求设备码失败（HTTP 500）",
		},
		{
			name:        "非 JSON 响应体",
			status:      http.StatusOK,
			body:        "not-json",
			wantMessage: "设备码响应格式不正确",
		},
		{
			name:        "缺少 device_code",
			status:      http.StatusOK,
			body:        `{"user_code":"UC","verification_uri":"v"}`,
			wantMessage: "设备码响应格式不正确",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			startFakeMicrosoft(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
				respondJSON(w, testCase.status, testCase.body)
			})
			authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)

			info, err := authenticator.requestDeviceCode(context.Background())
			if err == nil {
				t.Fatalf("应当失败，实际返回 %+v", info)
			}
			var authErr *MicrosoftAuthenticationError
			if !errors.As(err, &authErr) {
				t.Fatalf("错误类型 = %T，期望 *MicrosoftAuthenticationError", err)
			}
			if !strings.Contains(authErr.Message, testCase.wantMessage) {
				t.Fatalf("错误消息 = %q，期望包含 %q", authErr.Message, testCase.wantMessage)
			}
		})
	}

	t.Run("网络不可达时包装为请求设备码失败", func(t *testing.T) {
		server, _ := startFakeMicrosoft(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
			respondJSON(w, http.StatusOK, fakeDeviceCodeBody)
		})
		server.Close() // 关掉服务器制造连接失败

		authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)
		_, err := authenticator.requestDeviceCode(context.Background())
		if err == nil {
			t.Fatal("服务器不可达时应当失败")
		}
		var authErr *MicrosoftAuthenticationError
		if !errors.As(err, &authErr) || !strings.Contains(authErr.Message, "请求设备码失败") {
			t.Fatalf("错误 = %v，期望包装为「请求设备码失败」", err)
		}
		if authErr.Unwrap() == nil {
			t.Fatal("网络错误必须保留 Inner 便于排查（否则只剩一句面向用户的文案）")
		}
	})
}

// recordingTokenResponder 在 base 之上，让 /token 按调用次数依次返回给定响应
// （超出后重复最后一条），用于构造 authorization_pending / slow_down 之类的轮询序列。
func recordingTokenResponder(
	base func(w http.ResponseWriter, r *http.Request, body string),
	responses ...msaResponse,
) func(w http.ResponseWriter, r *http.Request, body string) {
	var gate sync.Mutex
	index := 0
	return func(w http.ResponseWriter, r *http.Request, body string) {
		if r.URL.Path != "/token" {
			base(w, r, body)
			return
		}
		gate.Lock()
		current := index
		if current >= len(responses) {
			current = len(responses) - 1
		}
		index++
		gate.Unlock()
		response := responses[current]
		if response.Status == 0 {
			response.Status = http.StatusOK
		}
		respondJSON(w, response.Status, response.Body)
	}
}

// TestDeviceCodePollingPendingThenSuccess authorization_pending 必须继续轮询到成功，
// 且间隔保持设备码下发的 interval。
// 防的回归：把 authorization_pending 当成失败直接放弃——用户在浏览器里还没点完
// 「继续」就已经提示登录失败。
func TestDeviceCodePollingPendingThenSuccess(t *testing.T) {
	waits := stubPollWait(t, 2*time.Millisecond)
	_, trace := startFakeMicrosoft(t, recordingTokenResponder(msaChainHandler(nil),
		msaResponse{Body: `{"error":"authorization_pending"}`},
		msaResponse{Body: `{"error":"authorization_pending"}`},
		msaResponse{Body: fakeTokenBody},
	))

	authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)
	handlerCalls := 0
	account, err := authenticator.Authenticate(context.Background(), func(DeviceCodeInfo, context.Context) {
		handlerCalls++
		if got := trace.count("/token"); got != 0 {
			t.Errorf("展示设备码时不应已经发起令牌轮询，实际 %d 次", got)
		}
	})
	if err != nil {
		t.Fatalf("轮询最终应当成功：%v", err)
	}
	if handlerCalls != 1 {
		t.Fatalf("设备码回调次数 = %d，期望 1", handlerCalls)
	}
	if account.AccessToken != "mc-token" || account.RefreshToken != "msa-refresh" {
		t.Fatalf("账号凭据 = %+v", account)
	}
	if got := trace.count("/token"); got != 3 {
		t.Fatalf("令牌轮询次数 = %d，期望 3（2 次 pending + 1 次成功）", got)
	}

	want := []time.Duration{time.Second, time.Second, time.Second}
	got := waits.snapshot()
	if len(got) != len(want) {
		t.Fatalf("等待次数 = %d，期望 %d：%v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("第 %d 次等待 = %v，期望 %v（authorization_pending 不应改变间隔）",
				index+1, got[index], want[index])
		}
	}
}

// TestDeviceCodePollingSlowDownBacksOff 服务端返回 slow_down 后，下一轮等待必须
// 变成 interval+5 秒并保持。
// 防的回归：忽略 slow_down 继续按原间隔轮询——服务端会持续拒绝，设备码流程
// 直接失败（OAuth 2.0 规范要求放慢轮询）。
func TestDeviceCodePollingSlowDownBacksOff(t *testing.T) {
	waits := stubPollWait(t, 2*time.Millisecond)
	_, trace := startFakeMicrosoft(t, recordingTokenResponder(msaChainHandler(nil),
		msaResponse{Body: `{"error":"authorization_pending"}`},
		msaResponse{Body: `{"error":"slow_down"}`},
		msaResponse{Body: fakeTokenBody},
	))

	authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)
	if _, err := authenticator.Authenticate(context.Background(),
		func(DeviceCodeInfo, context.Context) {}); err != nil {
		t.Fatalf("轮询最终应当成功：%v", err)
	}
	if got := trace.count("/token"); got != 3 {
		t.Fatalf("令牌轮询次数 = %d，期望 3", got)
	}

	want := []time.Duration{time.Second, time.Second, 6 * time.Second}
	got := waits.snapshot()
	if len(got) != len(want) {
		t.Fatalf("等待次数 = %d，期望 %d：%v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("第 %d 次等待 = %v，期望 %v（slow_down 应让后续等待 +5 秒）",
				index+1, got[index], want[index])
		}
	}
}

// TestDeviceCodePollingStopsOnTerminalErrors 终止类错误码必须立即结束轮询并带上
// 原始错误码，且不再请求下游。
// 防的回归：用户拒绝授权/设备码过期后继续轮询（白等到超时），错误码丢失导致
// 调用方无法区分「用户拒绝」和「网络失败」。
func TestDeviceCodePollingStopsOnTerminalErrors(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		wantCode     string
		wantContains string
	}{
		{
			name:         "用户拒绝授权",
			body:         `{"error":"authorization_declined"}`,
			wantCode:     AuthorizationDeclined,
			wantContains: "拒绝",
		},
		{
			name:         "设备码过期",
			body:         `{"error":"expired_token"}`,
			wantCode:     DeviceCodeExpired,
			wantContains: "已过期",
		},
		{
			name:         "验证码错误",
			body:         `{"error":"bad_verification_code"}`,
			wantCode:     "bad_verification_code",
			wantContains: "无效",
		},
		{
			name:         "其它错误码带描述",
			body:         `{"error":"invalid_grant","error_description":"AADSTS70016 授权等待中"}`,
			wantCode:     "invalid_grant",
			wantContains: "AADSTS70016",
		},
		{
			name:         "只有错误码没有描述",
			body:         `{"error":"invalid_request"}`,
			wantCode:     "invalid_request",
			wantContains: "invalid_request",
		},
		{
			name:         "完全空的错误响应",
			body:         `{}`,
			wantCode:     "",
			wantContains: "未知错误",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stubPollWait(t, 2*time.Millisecond)
			_, trace := startFakeMicrosoft(t, recordingTokenResponder(msaChainHandler(nil),
				msaResponse{Body: testCase.body},
				msaResponse{Body: fakeTokenBody}, // 若实现错误地继续轮询，这里会「成功」
			))

			authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)
			account, err := authenticator.Authenticate(context.Background(),
				func(DeviceCodeInfo, context.Context) {})
			if err == nil {
				t.Fatalf("终止类错误应当失败，实际返回 %+v", account)
			}
			var authErr *MicrosoftAuthenticationError
			if !errors.As(err, &authErr) {
				t.Fatalf("错误类型 = %T，期望 *MicrosoftAuthenticationError", err)
			}
			if authErr.ErrorCode != testCase.wantCode {
				t.Fatalf("错误码 = %q，期望 %q", authErr.ErrorCode, testCase.wantCode)
			}
			if !strings.Contains(authErr.Message, testCase.wantContains) {
				t.Fatalf("错误消息 = %q，期望包含 %q", authErr.Message, testCase.wantContains)
			}
			if got := trace.count("/token"); got != 1 {
				t.Fatalf("令牌轮询次数 = %d，期望 1（终止错误不得继续轮询）", got)
			}
			if got := trace.count("/user/authenticate"); got != 0 {
				t.Fatalf("终止错误后不应继续请求 Xbox Live，实际 %d 次", got)
			}
		})
	}
}

// TestDeviceCodePollingRetriesTransientNetworkError 轮询期间的瞬时网络错误
// （DNS 抖动 / 连接被重置 / 响应被截断）必须重试下一轮而不是终止整个登录。
// 防的回归：用户正在浏览器里输入验证码时被一次网络抖动踢出登录流程。
func TestDeviceCodePollingRetriesTransientNetworkError(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "DNS 抖动（net.Error）",
			err: &net.DNSError{
				Err:  "lookup login.microsoftonline.com: no such host",
				Name: "login.microsoftonline.com",
			},
		},
		{
			name: "响应被截断（io.ErrUnexpectedEOF）",
			err:  io.ErrUnexpectedEOF,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stubPollWait(t, 2*time.Millisecond)
			_, trace := startFakeMicrosoft(t, msaChainHandler(nil))

			transport := &flakyTransport{
				base:     http.DefaultTransport,
				failures: map[string]int{"/token": 1},
				err:      func(string) error { return testCase.err },
			}
			authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", &http.Client{Transport: transport})

			account, err := authenticator.Authenticate(context.Background(),
				func(DeviceCodeInfo, context.Context) {})
			if err != nil {
				t.Fatalf("瞬时网络错误后应当重试成功：%v", err)
			}
			if account.AccessToken != "mc-token" {
				t.Fatalf("账号访问令牌 = %q", account.AccessToken)
			}
			if got := transport.injectedFailures(); got != 1 {
				t.Fatalf("注入的失败次数 = %d，期望 1", got)
			}
			if got := trace.count("/token"); got != 1 {
				t.Fatalf("令牌端点成功命中次数 = %d，期望 1（失败那次到不了服务端）", got)
			}
		})
	}
}

// TestDeviceCodePollingGivesUpWhenNetworkKeepsFailing 网络持续失败时轮询必须有界：
// 到设备码有效期就结束，而不是无休止重试。
// 防的回归：把「一直重试」写成了死循环，用户关掉窗口后后台仍在打令牌端点。
func TestDeviceCodePollingGivesUpWhenNetworkKeepsFailing(t *testing.T) {
	waits := stubPollWait(t, 2*time.Millisecond)
	server, _ := startFakeMicrosoft(t, msaChainHandler(nil))
	server.Close()

	authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)
	// 直接调 pollForToken：设备码有效期取 60ms，避免测试真的等 1 秒。
	deviceCode := DeviceCodeInfo{
		DeviceCode:          "dc",
		VerificationUri:     "v",
		UserCode:            "UC",
		ExpiresIn:           60 * time.Millisecond,
		PollIntervalSeconds: 1,
	}
	_, _, err := authenticator.pollForToken(context.Background(), &deviceCode)
	if err == nil {
		t.Fatal("网络持续失败时应当以超时结束")
	}
	var authErr *MicrosoftAuthenticationError
	if !errors.As(err, &authErr) || authErr.ErrorCode != DeviceCodeExpired {
		t.Fatalf("错误 = %v，期望错误码 %q（设备码超时）", err, DeviceCodeExpired)
	}
	if !strings.Contains(authErr.Message, "超时") {
		t.Fatalf("错误消息 = %q，期望提示超时", authErr.Message)
	}
	if attempts := len(waits.snapshot()); attempts < 2 {
		t.Fatalf("轮询等待次数 = %d，期望多次重试后放弃（且不能是忙轮询）", attempts)
	}
}

// TestDeviceCodePollingHonorsContextCancellation 用户取消登录（关窗口/超时）后
// 轮询必须立刻退出。
// 防的回归：ctx 取消被忽略，登录 goroutine 一直挂到设备码过期。
func TestDeviceCodePollingHonorsContextCancellation(t *testing.T) {
	stubPollWait(t, 2*time.Millisecond)
	_, trace := startFakeMicrosoft(t, msaChainHandler(nil))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)
	deviceCode := DeviceCodeInfo{DeviceCode: "dc", ExpiresIn: time.Minute, PollIntervalSeconds: 1}
	if _, _, err := authenticator.pollForToken(ctx, &deviceCode); !errors.Is(err, context.Canceled) {
		t.Fatalf("错误 = %v，期望 context.Canceled", err)
	}
	if got := trace.count("/token"); got != 0 {
		t.Fatalf("已取消的 ctx 不应再发出令牌请求，实际 %d 次", got)
	}
}

// TestDeviceCodePollWaitContract 真实等待实现的契约：ctx 取消立即返回取消原因，
// 否则至少等待给定间隔。
// 防的回归：把等待写成 time.Sleep 后 ctx 取消被吞掉（取消后仍要等满间隔）。
func TestDeviceCodePollWaitContract(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := realDeviceCodePollWait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("错误 = %v，期望 context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("取消后等待了 %v，期望立即返回", elapsed)
	}

	start = time.Now()
	if err := realDeviceCodePollWait(context.Background(), 30*time.Millisecond); err != nil {
		t.Fatalf("未取消时不应报错：%v", err)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Fatalf("实际只等了 %v，期望约 30ms", elapsed)
	}
}

// TestAuthenticateCompletesFullChain 全链路登录：设备码 → 令牌 → XBL → XSTS →
// Minecraft 登录 → 档案，并逐段断言报文构造。
// 防的回归：Xbox Live / XSTS 的属性名被写成 camelCase（实测返回 400）、
// RpsTicket 少了 "d=" 前缀、identityToken 少了 "XBL3.0 x="、档案 UUID 没压缩成
// 32 位无连字符格式（游戏侧 --uuid 与会话校验不一致）。
func TestAuthenticateCompletesFullChain(t *testing.T) {
	stubPollWait(t, 2*time.Millisecond)
	_, trace := startFakeMicrosoft(t, msaChainHandler(nil))

	authenticator := NewMicrosoftDeviceCodeAuthenticator("test-client-id", nil)
	var handled DeviceCodeInfo
	before := time.Now().UTC()
	account, err := authenticator.Authenticate(context.Background(),
		func(info DeviceCodeInfo, _ context.Context) { handled = info })
	if err != nil {
		t.Fatalf("全链路登录应当成功：%v", err)
	}

	if handled.UserCode != "UC-1" || handled.DeviceCode != "dc-1" {
		t.Fatalf("回调收到的设备码信息 = %+v", handled)
	}
	if handled.ExpiresIn != 900*time.Second || handled.PollIntervalSeconds != 1 {
		t.Fatalf("回调收到的有效期/间隔 = %v/%d", handled.ExpiresIn, handled.PollIntervalSeconds)
	}

	if account.Username != "NyaPlayer" {
		t.Fatalf("玩家名 = %q", account.Username)
	}
	if account.Uuid != "069a79f444e94726a5befca90e38aaf5" {
		t.Fatalf("档案 UUID = %q，期望去掉连字符的 32 位格式", account.Uuid)
	}
	if account.AccessToken != "mc-token" || account.RefreshToken != "msa-refresh" {
		t.Fatalf("令牌 = %+v", account)
	}
	if account.XboxUserId != "1234567890123456" {
		t.Fatalf("xuid = %q，期望取 XSTS 响应的 xui[0].xid", account.XboxUserId)
	}
	if account.ClientId != "test-client-id" {
		t.Fatalf("ClientId = %q", account.ClientId)
	}
	wantExpiry := before.Add(86400 * time.Second)
	if account.ExpiresAt.Before(wantExpiry.Add(-time.Minute)) ||
		account.ExpiresAt.After(wantExpiry.Add(time.Minute)) {
		t.Fatalf("ExpiresAt = %v，期望约 %v（expires_in 秒之后）", account.ExpiresAt, wantExpiry)
	}

	order := trace.paths()
	wantOrder := []string{
		"/devicecode", "/token", "/user/authenticate", "/xsts/authorize",
		"/authentication/login_with_xbox", "/minecraft/profile",
	}
	if strings.Join(order, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("链路顺序 = %v，期望 %v", order, wantOrder)
	}

	device := trace.last(t, "/devicecode").form()
	if device.Get("client_id") != "test-client-id" {
		t.Fatalf("设备码请求 client_id = %q", device.Get("client_id"))
	}
	token := trace.last(t, "/token").form()
	if token.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
		t.Fatalf("令牌轮询 grant_type = %q", token.Get("grant_type"))
	}
	if token.Get("device_code") != "dc-1" || token.Get("client_id") != "test-client-id" {
		t.Fatalf("令牌轮询表单 = %v", token)
	}

	xblRequest := trace.last(t, "/user/authenticate")
	if got := xblRequest.Header.Get("x-xbl-contract-version"); got != "1" {
		t.Fatalf("XBL 契约版本头 = %q，期望 1", got)
	}
	xbl := decodeJSON(t, xblRequest.Body)
	if xbl["RelyingParty"] != "http://auth.xboxlive.com" || xbl["TokenType"] != "JWT" {
		t.Fatalf("XBL 报文 = %v", xbl)
	}
	if _, camel := xbl["relyingParty"]; camel {
		t.Fatal("XBL 属性名必须是 PascalCase：实测 camelCase 会返回 400")
	}
	xblProperties, ok := xbl["Properties"].(map[string]any)
	if !ok {
		t.Fatalf("XBL Properties 类型 = %T", xbl["Properties"])
	}
	if xblProperties["RpsTicket"] != "d=msa-access" {
		t.Fatalf("RpsTicket = %v，期望 d=<Microsoft access token>", xblProperties["RpsTicket"])
	}
	if xblProperties["SiteName"] != "user.auth.xboxlive.com" || xblProperties["AuthMethod"] != "RPS" {
		t.Fatalf("XBL Properties = %v", xblProperties)
	}

	xstsRequest := trace.last(t, "/xsts/authorize")
	if got := xstsRequest.Header.Get("x-xbl-contract-version"); got != "1" {
		t.Fatalf("XSTS 契约版本头 = %q，期望 1", got)
	}
	xsts := decodeJSON(t, xstsRequest.Body)
	if xsts["RelyingParty"] != "rp://api.minecraftservices.com/" {
		t.Fatalf("XSTS RelyingParty = %v", xsts["RelyingParty"])
	}
	xstsProperties, ok := xsts["Properties"].(map[string]any)
	if !ok {
		t.Fatalf("XSTS Properties 类型 = %T", xsts["Properties"])
	}
	if xstsProperties["SandboxId"] != "RETAIL" {
		t.Fatalf("XSTS SandboxId = %v，期望 RETAIL", xstsProperties["SandboxId"])
	}
	userTokens, ok := xstsProperties["UserTokens"].([]any)
	if !ok || len(userTokens) != 1 || userTokens[0] != "xbl-token" {
		t.Fatalf("XSTS UserTokens = %v", xstsProperties["UserTokens"])
	}

	minecraft := decodeJSON(t, trace.last(t, "/authentication/login_with_xbox").Body)
	if minecraft["identityToken"] != "XBL3.0 x=uhs-1;xsts-token" {
		t.Fatalf("Minecraft identityToken = %v，期望 XBL3.0 x=<uhs>;<xsts>", minecraft["identityToken"])
	}

	profileRequest := trace.last(t, "/minecraft/profile")
	if profileRequest.Method != http.MethodGet {
		t.Fatalf("档案请求方法 = %s，期望 GET", profileRequest.Method)
	}
	if got := profileRequest.Header.Get("Authorization"); got != "Bearer mc-token" {
		t.Fatalf("档案请求 Authorization = %q", got)
	}
}

// TestAuthenticateUsesJwtXuidWhenXstsOmitsIt XSTS 响应没有 xui[0].xid 时必须回退到
// Minecraft 访问令牌 JWT 里的 xuid claim。
// 防的回归：xuid 为空会被新版本游戏判定为离线模式（皮肤不加载）。
func TestAuthenticateUsesJwtXuidWhenXstsOmitsIt(t *testing.T) {
	stubPollWait(t, 2*time.Millisecond)
	token := buildFakeJwt(t, map[string]any{"xuid": "9876543210987654"}, false)
	_, trace := startFakeMicrosoft(t, msaChainHandler(map[string]msaResponse{
		"/xsts/authorize":                 {Body: `{"Token":"xsts-token"}`},
		"/authentication/login_with_xbox": {Body: fmt.Sprintf(`{"access_token":%q,"expires_in":86400}`, token)},
	}))

	authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)
	account, err := authenticator.Authenticate(context.Background(),
		func(DeviceCodeInfo, context.Context) {})
	if err != nil {
		t.Fatalf("登录应当成功：%v", err)
	}
	if account.XboxUserId != "9876543210987654" {
		t.Fatalf("xuid = %q，期望从 Minecraft 令牌 JWT 回退解析", account.XboxUserId)
	}
	if got := trace.count("/minecraft/profile"); got != 1 {
		t.Fatalf("档案请求次数 = %d，期望 1", got)
	}
}

// TestExtractXuidFromMinecraftToken JWT payload 解析的各种边界。
// 防的回归：带 padding 的 base64url payload 解析失败（部分服务端会返回 padding），
// 以及解析失败时 panic 掉整个登录流程。
func TestExtractXuidFromMinecraftToken(t *testing.T) {
	cases := []struct {
		name  string
		token string
		want  string
	}{
		{
			name:  "无 padding 的 payload",
			token: buildFakeJwt(t, map[string]any{"xuid": "1234"}, false),
			want:  "1234",
		},
		{
			name:  "带 padding 的 payload",
			token: buildFakeJwt(t, map[string]any{"xuid": "5678"}, true),
			want:  "5678",
		},
		{name: "没有 xuid claim", token: buildFakeJwt(t, map[string]any{"sub": "x"}, false), want: ""},
		{name: "xuid 不是字符串", token: buildFakeJwt(t, map[string]any{"xuid": 1234}, false), want: ""},
		{name: "payload 不是 JSON", token: "aGVhZGVy.bm90LWpzb24.c2ln", want: ""},
		{name: "payload 不是 base64", token: "aGVhZGVy.!!!.c2ln", want: ""},
		{name: "没有点号分隔", token: "no-dots-at-all", want: ""},
		{name: "空串", token: "", want: ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := extractXuidFromMinecraftToken(testCase.token); got != testCase.want {
				t.Fatalf("extractXuidFromMinecraftToken() = %q，期望 %q", got, testCase.want)
			}
		})
	}
}

// TestFormatCompactUuid 档案 UUID 必须归一化为 32 位无连字符格式。
// 防的回归：把带连字符的 UUID 直接传给游戏，与官方启动器/主流启动器的
// --uuid 传参格式不一致。
func TestFormatCompactUuid(t *testing.T) {
	cases := map[string]string{
		"069a79f4-44e9-4726-a5be-fca90e38aaf5": "069a79f444e94726a5befca90e38aaf5",
		"069a79f444e94726a5befca90e38aaf5":     "069a79f444e94726a5befca90e38aaf5",
		"":                                     "",
	}
	for input, want := range cases {
		if got := formatCompactUuid(input); got != want {
			t.Fatalf("formatCompactUuid(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// TestXstsErrorMessages XErr 错误码到用户可读文案的映射。
// 防的回归：错误码映射丢失后用户只看到一句「XSTS 授权失败（HTTP 401）」，
// 不知道该去加入家庭组还是改地区。
func TestXstsErrorMessages(t *testing.T) {
	cases := []struct {
		xerr         string
		wantContains string
	}{
		{XboxNoAccount, "没有 Xbox Live 档案"},
		{XboxChildAccount, "儿童账号"},
		{XboxConsentRequired, "家长同意"},
		{XboxRegionBlocked, "不支持 Minecraft 服务"},
		{XboxAgeVerification, "年龄验证"},
	}
	for _, testCase := range cases {
		body := fmt.Sprintf(`{"Identity":"0","XErr":%s,"Message":"","Redirect":""}`, testCase.xerr)
		var authErr *MicrosoftAuthenticationError
		if err := xstsError(http.StatusUnauthorized, body); !errors.As(err, &authErr) {
			t.Fatalf("XErr %s 的错误类型 = %T，期望 *MicrosoftAuthenticationError", testCase.xerr, err)
		}
		if authErr.ErrorCode != testCase.xerr {
			t.Fatalf("XErr %s 的错误码 = %q", testCase.xerr, authErr.ErrorCode)
		}
		if !strings.Contains(authErr.Message, testCase.wantContains) {
			t.Fatalf("XErr %s 的文案 = %q，期望包含 %q", testCase.xerr, authErr.Message, testCase.wantContains)
		}
	}

	var unknown *MicrosoftAuthenticationError
	if err := xstsError(http.StatusUnauthorized, `{"XErr":2148916299}`); !errors.As(err, &unknown) {
		t.Fatalf("未知 XErr 的错误类型 = %T", err)
	}
	if !strings.Contains(unknown.Message, "HTTP 401") || unknown.ErrorCode != "2148916299" {
		t.Fatalf("未知 XErr 的处理 = %q / %q", unknown.Message, unknown.ErrorCode)
	}
	// 非 JSON 响应体（网关错误页）不能 panic，且要带上状态码
	var broken *MicrosoftAuthenticationError
	if err := xstsError(http.StatusBadGateway, "<html>502</html>"); !errors.As(err, &broken) {
		t.Fatalf("非 JSON 响应的错误类型 = %T", err)
	}
	if !strings.Contains(broken.Message, "HTTP 502") {
		t.Fatalf("非 JSON 响应的文案 = %q", broken.Message)
	}
}

// TestAuthenticateStageFailureMessages 链路各阶段的失败文案与错误码。
// 防的回归：把「没买游戏」「账号被封禁」「没有 Xbox 档案」都压成一句通用网络错误，
// 用户无法自救（这三种情况用户能做的事完全不同）。
func TestAuthenticateStageFailureMessages(t *testing.T) {
	cases := []struct {
		name         string
		override     map[string]msaResponse
		wantContains string
		wantCode     string
	}{
		{
			name:         "Xbox Live 认证 HTTP 500",
			override:     map[string]msaResponse{"/user/authenticate": {Status: 500, Body: "boom"}},
			wantContains: "Xbox Live 认证失败（HTTP 500）",
		},
		{
			name:         "Xbox Live 响应缺 Token",
			override:     map[string]msaResponse{"/user/authenticate": {Body: `{"DisplayClaims":{"xui":[{"uhs":"u"}]}}`}},
			wantContains: "Xbox Live 认证响应格式不正确",
		},
		{
			name:         "Xbox Live 响应缺 uhs",
			override:     map[string]msaResponse{"/user/authenticate": {Body: `{"Token":"t","DisplayClaims":{"xui":[]}}`}},
			wantContains: "Xbox Live 认证响应格式不正确",
		},
		{
			name:         "XSTS 无 Xbox 档案",
			override:     map[string]msaResponse{"/xsts/authorize": {Status: 401, Body: `{"XErr":2148916233}`}},
			wantContains: "没有 Xbox Live 档案",
			wantCode:     XboxNoAccount,
		},
		{
			name:         "XSTS 响应缺 Token",
			override:     map[string]msaResponse{"/xsts/authorize": {Body: `{"DisplayClaims":{"xui":[{"xid":"1"}]}}`}},
			wantContains: "XSTS 授权响应格式不正确",
		},
		{
			name: "Minecraft 账号被封禁",
			override: map[string]msaResponse{
				"/authentication/login_with_xbox": {Status: 403, Body: `{"error":"ACCOUNT_SUSPENDED"}`},
			},
			wantContains: "封禁",
		},
		{
			name: "Minecraft 登录其它 4xx",
			override: map[string]msaResponse{
				"/authentication/login_with_xbox": {Status: 400, Body: `{"error":"invalid"}`},
			},
			wantContains: "Minecraft 登录失败（HTTP 400）",
		},
		{
			name: "Minecraft 登录响应缺令牌",
			override: map[string]msaResponse{
				"/authentication/login_with_xbox": {Body: `{"expires_in":10}`},
			},
			wantContains: "Minecraft 登录响应格式不正确",
		},
		{
			name:         "账号未购买 Minecraft",
			override:     map[string]msaResponse{"/minecraft/profile": {Status: 404, Body: `{"error":"NOT_FOUND"}`}},
			wantContains: "尚未购买 Minecraft",
		},
		{
			name:         "档案响应缺字段",
			override:     map[string]msaResponse{"/minecraft/profile": {Body: `{"id":"abc","name":""}`}},
			wantContains: "Minecraft 档案响应格式不正确",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stubPollWait(t, 2*time.Millisecond)
			startFakeMicrosoft(t, msaChainHandler(testCase.override))

			authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)
			account, err := authenticator.Authenticate(context.Background(),
				func(DeviceCodeInfo, context.Context) {})
			if err == nil {
				t.Fatalf("应当失败，实际返回 %+v", account)
			}
			var authErr *MicrosoftAuthenticationError
			if !errors.As(err, &authErr) {
				t.Fatalf("错误类型 = %T，期望 *MicrosoftAuthenticationError", err)
			}
			if !strings.Contains(authErr.Message, testCase.wantContains) {
				t.Fatalf("错误消息 = %q，期望包含 %q", authErr.Message, testCase.wantContains)
			}
			if testCase.wantCode != "" && authErr.ErrorCode != testCase.wantCode {
				t.Fatalf("错误码 = %q，期望 %q", authErr.ErrorCode, testCase.wantCode)
			}
		})
	}
}

// TestDeviceCodeInfoVerificationUriFull 展示给用户的验证地址必须预填验证码。
// 防的回归：地址少了 ?user_code=，用户得手抄验证码或在页面上重新输入。
func TestDeviceCodeInfoVerificationUriFull(t *testing.T) {
	info := DeviceCodeInfo{VerificationUri: "https://microsoft.com/link", UserCode: "ABCD-EFGH"}
	if got, want := info.VerificationUriFull(), "https://microsoft.com/link?user_code=ABCD-EFGH"; got != want {
		t.Fatalf("VerificationUriFull() = %q，期望 %q", got, want)
	}
}
