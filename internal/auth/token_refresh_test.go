package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRefreshWithoutRefreshTokenFailsFast 账号没有 refresh_token 时必须立刻报错，
// 且不发出任何网络请求。
// 防的回归：拿空字符串去调令牌端点，用户看到的是微软的 invalid_request 而不是
// 「请重新登录」，同时白白浪费一次往返。
func TestRefreshWithoutRefreshTokenFailsFast(t *testing.T) {
	_, trace := startFakeMicrosoft(t, msaChainHandler(nil))
	authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)

	for _, refreshToken := range []string{"", "   "} {
		account, err := authenticator.Refresh(context.Background(),
			MicrosoftAccount{Username: "NyaPlayer", RefreshToken: refreshToken})
		if err == nil {
			t.Fatalf("refresh_token = %q 时应当失败，实际返回 %+v", refreshToken, account)
		}
		if !strings.Contains(err.Error(), "刷新令牌") {
			t.Fatalf("错误 = %v，期望提示缺少刷新令牌", err)
		}
	}
	if got := len(trace.paths()); got != 0 {
		t.Fatalf("缺少 refresh_token 时不应发出任何请求，实际 %d 次：%v", got, trace.paths())
	}
}

// TestRefreshRequestShapeAndTokenFallback 刷新请求的报文构造，以及服务端未返回
// 新 refresh_token（无轮换策略）时必须回退保留旧值。
// 防的回归：把 refresh_token 写空导致账号刷新后被清空、下次启动必然要求重新登录。
func TestRefreshRequestShapeAndTokenFallback(t *testing.T) {
	_, trace := startFakeMicrosoft(t, msaChainHandler(map[string]msaResponse{
		// 服务端只回 access_token（无轮换策略）
		"/token": {Body: `{"access_token":"new-access","expires_in":3600}`},
	}))
	authenticator := NewMicrosoftDeviceCodeAuthenticator("test-client-id", nil)

	account, err := authenticator.Refresh(context.Background(), MicrosoftAccount{
		Username:     "NyaPlayer",
		Uuid:         "069a79f444e94726a5befca90e38aaf5",
		RefreshToken: "old-refresh",
		ClientId:     "old-client-id",
	})
	if err != nil {
		t.Fatalf("刷新应当成功：%v", err)
	}
	if account.RefreshToken != "old-refresh" {
		t.Fatalf("RefreshToken = %q，期望回退保留旧值 old-refresh", account.RefreshToken)
	}
	if account.AccessToken != "mc-token" {
		t.Fatalf("AccessToken = %q，期望交换后的 Minecraft 令牌", account.AccessToken)
	}
	if account.ClientId != "test-client-id" {
		t.Fatalf("ClientId = %q，期望使用认证器的 clientId", account.ClientId)
	}

	form := trace.last(t, "/token").form()
	if got := form.Get("grant_type"); got != "refresh_token" {
		t.Fatalf("grant_type = %q，期望 refresh_token", got)
	}
	if got := form.Get("refresh_token"); got != "old-refresh" {
		t.Fatalf("refresh_token = %q，期望 old-refresh", got)
	}
	if got := form.Get("client_id"); got != "test-client-id" {
		t.Fatalf("client_id = %q，期望 test-client-id", got)
	}
	if !strings.Contains(form.Get("scope"), "offline_access") {
		t.Fatalf("scope = %q，必须包含 offline_access（否则下次刷新拿不到新 refresh_token）", form.Get("scope"))
	}

	// 后续交换必须用刷新得到的新 access token，而不是旧值
	xbl := decodeJSON(t, trace.last(t, "/user/authenticate").Body)
	properties, ok := xbl["Properties"].(map[string]any)
	if !ok {
		t.Fatalf("XBL Properties 类型 = %T", xbl["Properties"])
	}
	if properties["RpsTicket"] != "d=new-access" {
		t.Fatalf("RpsTicket = %v，期望 d=new-access（刷新后的令牌）", properties["RpsTicket"])
	}
}

// TestRefreshAdoptsRotatedRefreshToken 服务端返回新 refresh_token 时必须采用新值。
// 防的回归：仍然保存旧令牌，而旧令牌在轮换策略下已作废，用户被强制重新登录。
func TestRefreshAdoptsRotatedRefreshToken(t *testing.T) {
	_, trace := startFakeMicrosoft(t, msaChainHandler(map[string]msaResponse{
		"/token": {Body: `{"access_token":"new-access","refresh_token":"rotated-refresh","expires_in":3600}`},
	}))
	authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)

	account, err := authenticator.Refresh(context.Background(),
		MicrosoftAccount{Username: "NyaPlayer", RefreshToken: "old-refresh"})
	if err != nil {
		t.Fatalf("刷新应当成功：%v", err)
	}
	if account.RefreshToken != "rotated-refresh" {
		t.Fatalf("RefreshToken = %q，期望采用轮换后的 rotated-refresh", account.RefreshToken)
	}
	if got := trace.count("/token"); got != 1 {
		t.Fatalf("令牌端点命中次数 = %d，期望 1", got)
	}
}

// TestRefreshFailureBranches 刷新失败的各分支文案与错误码，且失败后不得继续
// 请求 Xbox / Minecraft 服务。
// 防的回归：把 HTTP 错误体（可能是网关 HTML）丢掉，用户只看到「刷新失败」，
// 无法判断是令牌被吊销还是网络问题。
func TestRefreshFailureBranches(t *testing.T) {
	cases := []struct {
		name         string
		response     msaResponse
		wantContains []string
		wantCode     string
	}{
		{
			name:         "HTTP 400 且带错误码",
			response:     msaResponse{Status: 400, Body: `{"error":"invalid_grant","error_description":"AADSTS70008 令牌已过期"}`},
			wantContains: []string{"刷新令牌失败（HTTP 400）", "AADSTS70008"},
			wantCode:     "invalid_grant",
		},
		{
			name:         "HTTP 500 且响应体是 JSON",
			response:     msaResponse{Status: 500, Body: `{"error":"server_error","error_description":"内部错误"}`},
			wantContains: []string{"刷新令牌失败（HTTP 500）", "内部错误"},
			wantCode:     "server_error",
		},
		{
			// 反序列化在状态码判断之前：网关 HTML 错误页只能落到「无法解析」分支，
			// 但必须把原文带回给用户/日志（否则只剩一句无法定位的失败）
			name:         "HTTP 500 非 JSON 响应体（网关错误页）",
			response:     msaResponse{Status: 500, Body: "<html>bad gateway</html>"},
			wantContains: []string{"无法解析的响应", "bad gateway"},
		},
		{
			name:         "HTTP 200 但缺少 access_token",
			response:     msaResponse{Body: `{"refresh_token":"new-refresh"}`},
			wantContains: []string{"刷新令牌失败（HTTP 200）"},
		},
		{
			name:         "HTTP 200 但响应体不是 JSON",
			response:     msaResponse{Body: "not-json"},
			wantContains: []string{"无法解析的响应"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, trace := startFakeMicrosoft(t, msaChainHandler(map[string]msaResponse{
				"/token": testCase.response,
			}))
			authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)

			account, err := authenticator.Refresh(context.Background(),
				MicrosoftAccount{Username: "NyaPlayer", RefreshToken: "old-refresh"})
			if err == nil {
				t.Fatalf("应当失败，实际返回 %+v", account)
			}
			var authErr *MicrosoftAuthenticationError
			if !errors.As(err, &authErr) {
				t.Fatalf("错误类型 = %T，期望 *MicrosoftAuthenticationError", err)
			}
			for _, want := range testCase.wantContains {
				if !strings.Contains(authErr.Message, want) {
					t.Fatalf("错误消息 = %q，期望包含 %q", authErr.Message, want)
				}
			}
			if authErr.ErrorCode != testCase.wantCode {
				t.Fatalf("错误码 = %q，期望 %q", authErr.ErrorCode, testCase.wantCode)
			}
			if got := trace.count("/user/authenticate"); got != 0 {
				t.Fatalf("刷新失败后不应继续请求 Xbox Live，实际 %d 次", got)
			}
		})
	}
}

// TestRefreshReportsRotatedCredentialsWhenExchangeFails 令牌 POST 已成功、但后续
// XBL/XSTS/档案交换失败时，必须用 RotatedCredentialsError 把轮换后的新凭据交给调用方。
// 防的回归：旧 refresh_token 已被消费却没能持久化新值——下次刷新必然失败，
// 用户被迫重新登录（这是本文件里最贵的一次回归）。
func TestRefreshReportsRotatedCredentialsWhenExchangeFails(t *testing.T) {
	stubPollWait(t, 2*time.Millisecond)
	startFakeMicrosoft(t, msaChainHandler(map[string]msaResponse{
		"/token":             {Body: `{"access_token":"new-access","refresh_token":"rotated-refresh","expires_in":3600}`},
		"/user/authenticate": {Status: 500, Body: "boom"},
	}))
	authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)

	original := MicrosoftAccount{
		Username:     "NyaPlayer",
		Uuid:         "069a79f444e94726a5befca90e38aaf5",
		AccessToken:  "old-mc-token",
		RefreshToken: "old-refresh",
		XboxUserId:   "1234567890123456",
		ClientId:     "old-client",
		ExpiresAt:    time.Now().UTC().Add(-time.Hour),
	}
	account, err := authenticator.Refresh(context.Background(), original)
	if err == nil {
		t.Fatalf("交换失败时应当返回错误，实际返回 %+v", account)
	}
	if account != (MicrosoftAccount{}) {
		t.Fatalf("失败时不应返回半成品账号：%+v", account)
	}

	var rotated *RotatedCredentialsError
	if !errors.As(err, &rotated) {
		t.Fatalf("错误类型 = %T，期望 *RotatedCredentialsError（调用方靠它持久化新令牌）", err)
	}
	if rotated.RefreshedAccount.RefreshToken != "rotated-refresh" {
		t.Fatalf("轮换后的 RefreshToken = %q，期望 rotated-refresh",
			rotated.RefreshedAccount.RefreshToken)
	}
	if rotated.RefreshedAccount.Username != original.Username ||
		rotated.RefreshedAccount.Uuid != original.Uuid ||
		rotated.RefreshedAccount.XboxUserId != original.XboxUserId ||
		rotated.RefreshedAccount.ClientId != original.ClientId {
		t.Fatalf("轮换后的账号其它字段应原样保留：%+v", rotated.RefreshedAccount)
	}
	if rotated.Unwrap() == nil {
		t.Fatal("必须保留 Inner 便于排查交换失败原因")
	}
	if !strings.Contains(rotated.Message, "获取 Minecraft 档案失败") {
		t.Fatalf("错误消息 = %q", rotated.Message)
	}
}

// TestValidateSkipsRefreshWhenTokenFresh 令牌未过期时 Validate 必须原样返回，
// 且一次网络请求都不发。
// 防的回归：每次启动都无条件刷新，轮换策略下并发刷新会让账号被强制下线。
func TestValidateSkipsRefreshWhenTokenFresh(t *testing.T) {
	_, trace := startFakeMicrosoft(t, msaChainHandler(nil))
	authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)

	fresh := MicrosoftAccount{
		Username:    "NyaPlayer",
		AccessToken: "mc-token",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	}
	account, err := authenticator.Validate(context.Background(), fresh)
	if err != nil {
		t.Fatalf("未过期时不应报错：%v", err)
	}
	if account != fresh {
		t.Fatalf("未过期时应原样返回：%+v", account)
	}
	if got := len(trace.paths()); got != 0 {
		t.Fatalf("未过期时不应发出请求，实际 %d 次：%v", got, trace.paths())
	}
}

// TestValidateRefreshesExpiredAccount 令牌过期时 Validate 自动走刷新链路。
// 防的回归：过期账号直接拿去启动，游戏侧会话校验失败。
func TestValidateRefreshesExpiredAccount(t *testing.T) {
	stubPollWait(t, 2*time.Millisecond)
	_, trace := startFakeMicrosoft(t, msaChainHandler(map[string]msaResponse{
		"/token": {Body: `{"access_token":"new-access","refresh_token":"rotated-refresh","expires_in":3600}`},
	}))
	authenticator := NewMicrosoftDeviceCodeAuthenticator("cid", nil)

	account, err := authenticator.Validate(context.Background(), MicrosoftAccount{
		Username:     "NyaPlayer",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("过期账号应当自动刷新成功：%v", err)
	}
	if account.AccessToken != "mc-token" || account.RefreshToken != "rotated-refresh" {
		t.Fatalf("刷新结果 = %+v", account)
	}
	if got := trace.count("/token"); got != 1 {
		t.Fatalf("令牌端点命中次数 = %d，期望 1", got)
	}
}

// TestMicrosoftAccountIsExpiredBoundary 过期判定必须带 5 分钟提前量。
// 防的回归：令牌在启动过程中临界过期，游戏侧鉴权失败（用户看到的是
// 「登录失败」而不是被无感刷新）。
func TestMicrosoftAccountIsExpiredBoundary(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{name: "已过期", expiresAt: now.Add(-time.Minute), want: true},
		{name: "4 分钟后到期（提前量内）", expiresAt: now.Add(4 * time.Minute), want: true},
		{name: "6 分钟后到期（提前量外）", expiresAt: now.Add(6 * time.Minute), want: false},
		{name: "一小时后到期", expiresAt: now.Add(time.Hour), want: false},
		{name: "零值时间", expiresAt: time.Time{}, want: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			account := MicrosoftAccount{ExpiresAt: testCase.expiresAt}
			if got := account.IsExpired(); got != testCase.want {
				t.Fatalf("IsExpired() = %v，期望 %v", got, testCase.want)
			}
		})
	}
}

// recordingAuthenticator 假认证器：记录调用并把参数原样交回断言。
type recordingAuthenticator struct {
	mu                sync.Mutex
	authenticateCalls int
	refreshCalls      int
	validateCalls     int
	handlerCalls      int
	lastRefresh       MicrosoftAccount
	lastValidate      MicrosoftAccount
	result            MicrosoftAccount
	err               error
}

func (r *recordingAuthenticator) Authenticate(
	_ context.Context, handler func(DeviceCodeInfo, context.Context),
) (MicrosoftAccount, error) {
	r.mu.Lock()
	r.authenticateCalls++
	r.mu.Unlock()
	if handler != nil {
		r.mu.Lock()
		r.handlerCalls++
		r.mu.Unlock()
		handler(DeviceCodeInfo{UserCode: "UC-1"}, context.Background())
	}
	return r.result, r.err
}

func (r *recordingAuthenticator) Refresh(_ context.Context, account MicrosoftAccount) (MicrosoftAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshCalls++
	r.lastRefresh = account
	return r.result, r.err
}

func (r *recordingAuthenticator) Validate(_ context.Context, account MicrosoftAccount) (MicrosoftAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validateCalls++
	r.lastValidate = account
	return r.result, r.err
}

// TestSharedMicrosoftAuthenticationDelegates 共享门面（登录浮层/启动管线/档案服务
// 共用的入口）必须把三个方法都委托给 DefaultMicrosoftAuthenticator，且参数原样传递。
// 防的回归：替换认证实现时门面漏接了某个方法（例如 Validate 仍打旧实例），
// 表现为「登录成功但启动时提示账号失效」。
func TestSharedMicrosoftAuthenticationDelegates(t *testing.T) {
	original := DefaultMicrosoftAuthenticator
	fake := &recordingAuthenticator{result: MicrosoftAccount{Username: "FakePlayer"}}
	DefaultMicrosoftAuthenticator = fake
	t.Cleanup(func() { DefaultMicrosoftAuthenticator = original })

	ctx := context.Background()
	account, err := MicrosoftAuthentication.Authenticate(ctx, func(DeviceCodeInfo, context.Context) {})
	if err != nil || account.Username != "FakePlayer" {
		t.Fatalf("Authenticate 委托结果 = %+v / %v", account, err)
	}
	if fake.authenticateCalls != 1 || fake.handlerCalls != 1 {
		t.Fatalf("Authenticate 调用次数 = %d，设备码回调次数 = %d",
			fake.authenticateCalls, fake.handlerCalls)
	}

	input := MicrosoftAccount{Username: "NyaPlayer", RefreshToken: "r"}
	if _, err := MicrosoftAuthentication.Refresh(ctx, input); err != nil {
		t.Fatalf("Refresh 委托失败：%v", err)
	}
	if fake.refreshCalls != 1 || fake.lastRefresh != input {
		t.Fatalf("Refresh 调用次数 = %d，入参 = %+v", fake.refreshCalls, fake.lastRefresh)
	}

	if _, err := MicrosoftAuthentication.Validate(ctx, input); err != nil {
		t.Fatalf("Validate 委托失败：%v", err)
	}
	if fake.validateCalls != 1 || fake.lastValidate != input {
		t.Fatalf("Validate 调用次数 = %d，入参 = %+v", fake.validateCalls, fake.lastValidate)
	}
}
