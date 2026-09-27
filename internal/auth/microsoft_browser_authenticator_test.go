package auth

// 内嵌浏览器 OAuth 登录端到端测试：走真实回环服务器（127.0.0.1 随机端口），
// openLoginPage 回调里解析授权 URL 后模拟「微软授权完成 → 302 回调」，
// 令牌链路端点全部指向假服务器。离线运行，不访问任何真实微软端点。

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// noRedirectClient 不跟随 302 的 HTTP 客户端：回环服务器的跳转目标
// （wails.localhost / 开发服务器源）在测试进程里不可达，必须拿到原始响应断言。
var noRedirectClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// TestBrowserLoginEndToEnd 主流程：授权 URL 构造（PKCE/state/scope）→
// 回环回调收码 → 授权码换令牌 → XBL/XSTS/Minecraft 链路 → 档案。
func TestBrowserLoginEndToEnd(t *testing.T) {
	server, trace := startFakeMicrosoft(t, msaChainHandler(nil))
	originalAuthorize := authorizeEndpoint
	authorizeEndpoint = server.URL + "/authorize"
	t.Cleanup(func() { authorizeEndpoint = originalAuthorize })

	steps := make([]int, 0, 6)
	var codeChallenge string
	var account MicrosoftAccount
	var loginErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		account, loginErr = NewMicrosoftBrowserAuthenticator(NewMicrosoftDeviceCodeAuthenticator("test-client-id", nil)).StartBrowserLogin(
			context.Background(),
			"http://wails.localhost",
			func(loginURL string) {
				authorize, err := url.Parse(loginURL)
				if err != nil {
					t.Errorf("授权 URL 无法解析：%v", err)
					return
				}
				if !strings.Contains(loginURL, server.URL) {
					t.Errorf("授权 URL 没有指向假服务器：%s", loginURL)
				}
				query := authorize.Query()
				if got := query.Get("client_id"); got != "test-client-id" {
					t.Errorf("client_id = %q", got)
				}
				if got := query.Get("response_type"); got != "code" {
					t.Errorf("response_type = %q，期望 code", got)
				}
				if got := query.Get("code_challenge_method"); got != "S256" {
					t.Errorf("code_challenge_method = %q，期望 S256", got)
				}
				if !strings.Contains(query.Get("scope"), "offline_access") {
					t.Errorf("scope 缺少 offline_access（拿不到 refresh_token）：%q", query.Get("scope"))
				}
				state := query.Get("state")
				if len(state) < 32 {
					t.Errorf("state 过短（防 CSRF 强度不足）：%q", state)
				}
				codeChallenge = query.Get("code_challenge")
				if len(codeChallenge) < 32 {
					t.Errorf("code_challenge 过短：%q", codeChallenge)
					return
				}

				// 模拟用户在微软页完成授权 → 浏览器 302 到本地回环 callback
				redirect := query.Get("redirect_uri")
				if !strings.HasPrefix(redirect, "http://localhost:") || !strings.HasSuffix(redirect, "/callback") {
					t.Errorf("redirect_uri = %q，期望 http://localhost:<端口>/callback（Azure 只放行 localhost 通配端口）", redirect)
					return
				}
				response, err := noRedirectClient.Get(redirect + "?code=auth-code-1&state=" + url.QueryEscape(state))
				if err != nil {
					t.Errorf("回调请求失败：%v", err)
					return
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusFound {
					t.Errorf("回调响应状态 = %d，期望 302", response.StatusCode)
				}
				if location := response.Header.Get("Location"); location != "http://wails.localhost/?msoauth=return" {
					t.Errorf("回调跳转目标 = %q，期望 http://wails.localhost/?msoauth=return", location)
				}
			},
			func(step int) {
				steps = append(steps, step)
			})
	}()
	<-done

	if loginErr != nil {
		t.Fatalf("内嵌登录失败：%v", loginErr)
	}
	if account.Username != "NyaPlayer" {
		t.Errorf("档案玩家名 = %q，期望 NyaPlayer", account.Username)
	}
	if account.Uuid != "069a79f444e94726a5befca90e38aaf5" {
		t.Errorf("档案 UUID = %q，期望 32 位无连字符格式", account.Uuid)
	}
	if account.RefreshToken != "msa-refresh" {
		t.Errorf("RefreshToken = %q，期望 msa-refresh", account.RefreshToken)
	}

	// 令牌请求：grant_type=authorization_code + code + code_verifier + redirect_uri
	tokenRequest := trace.last(t, "/token")
	form := tokenRequest.form()
	if got := form.Get("grant_type"); got != "authorization_code" {
		t.Errorf("grant_type = %q，期望 authorization_code", got)
	}
	if got := form.Get("code"); got != "auth-code-1" {
		t.Errorf("code = %q，期望 auth-code-1", got)
	}
	verifier := form.Get("code_verifier")
	if len(verifier) < 43 || len(verifier) > 128 {
		t.Fatalf("code_verifier 长度 = %d，超出 PKCE 规定的 43~128", len(verifier))
	}
	// PKCE 闭环：code_challenge 必须等于 BASE64URL(SHA256(code_verifier))
	sum := sha256.Sum256([]byte(verifier))
	if expected := base64.RawURLEncoding.EncodeToString(sum[:]); expected != codeChallenge {
		t.Errorf("PKCE 校验失败：challenge = %q，期望 %q", codeChallenge, expected)
	}

	// 步骤回调：授权 → 令牌 → XBL → XSTS → Minecraft（档案完成不再回调）
	wantSteps := []int{LoginStepAuthorized, LoginStepMicrosoftToken, LoginStepXboxLive, LoginStepXsts, LoginStepMinecraft}
	if len(steps) != len(wantSteps) {
		t.Fatalf("进度步骤 = %v，期望 %v", steps, wantSteps)
	}
	for index, step := range wantSteps {
		if steps[index] != step {
			t.Fatalf("进度步骤顺序 = %v，期望 %v", steps, wantSteps)
		}
	}
}

// TestBrowserLoginAccessDenied 用户在登录页拒绝授权：回调带 error 返回，
// 必须报错且绝不继续请求令牌。
func TestBrowserLoginAccessDenied(t *testing.T) {
	server, trace := startFakeMicrosoft(t, msaChainHandler(nil))
	authorizeEndpoint = server.URL + "/authorize"
	t.Cleanup(func() { authorizeEndpoint = strings.TrimSuffix(authorizeEndpoint, "/authorize") })

	_, loginErr := NewMicrosoftBrowserAuthenticator(NewMicrosoftDeviceCodeAuthenticator("test-client-id", nil)).StartBrowserLogin(
		context.Background(),
		"http://wails.localhost",
		func(loginURL string) {
			authorize, err := url.Parse(loginURL)
			if err != nil {
				return
			}
			state := authorize.Query().Get("state")
			redirect := authorize.Query().Get("redirect_uri")
			// 跳转目标应带 msoauth=error 标记（前端据此展示失败说明）
			response, err := noRedirectClient.Get(redirect + "?error=access_denied&state=" + url.QueryEscape(state))
			if err != nil {
				t.Errorf("回调请求失败：%v", err)
				return
			}
			response.Body.Close()
			if location := response.Header.Get("Location"); location != "http://wails.localhost/?msoauth=error" {
				t.Errorf("失败跳转目标 = %q，期望 http://wails.localhost/?msoauth=error", location)
			}
		},
		nil)
	if loginErr == nil {
		t.Fatal("access_denied 应当返回错误")
	}
	if !strings.Contains(loginErr.Error(), "拒绝") {
		t.Errorf("错误信息应说明是用户拒绝了授权：%v", loginErr)
	}
	if trace.count("/token") != 0 {
		t.Error("拒绝授权后不应继续请求令牌")
	}
}

// TestBrowserLoginStateMismatch 回调 state 与发出的不一致（CSRF 伪造场景）：
// 必须报错且不换令牌。
func TestBrowserLoginStateMismatch(t *testing.T) {
	server, trace := startFakeMicrosoft(t, msaChainHandler(nil))
	authorizeEndpoint = server.URL + "/authorize"
	t.Cleanup(func() { authorizeEndpoint = strings.TrimSuffix(authorizeEndpoint, "/authorize") })

	_, loginErr := NewMicrosoftBrowserAuthenticator(NewMicrosoftDeviceCodeAuthenticator("test-client-id", nil)).StartBrowserLogin(
		context.Background(),
		"http://wails.localhost",
		func(loginURL string) {
			authorize, _ := url.Parse(loginURL)
			redirect := authorize.Query().Get("redirect_uri")
			response, err := noRedirectClient.Get(redirect + "?code=auth-code-1&state=forged-state")
			if err != nil {
				t.Errorf("回调请求失败：%v", err)
				return
			}
			response.Body.Close()
		},
		nil)
	if loginErr == nil {
		t.Fatal("state 不匹配应当报错")
	}
	if trace.count("/token") != 0 {
		t.Error("state 校验失败后不应继续请求令牌")
	}
}
