package auth

// 正版登录的第二种入口：内嵌浏览器 OAuth（授权码 + PKCE）。
// 与设备码流程共享 ClientId 与 XBL→XSTS→Minecraft 交换链路，差异只在
// Microsoft 令牌的获取方式：把主窗口（Wails WebView）直接导航到微软登录页，
// 授权完成后微软重定向到本地回环服务器（http://localhost:<随机端口>/callback），
// 服务器再 302 跳回启动器页面，全程无需弹出系统浏览器或多开窗口。
//
// 注意：回环重定向使用 localhost（Azure 对 localhost 特殊放行任意端口；
// 127.0.0.1 需要精确端口注册），对应 Azure 应用需注册 "http://localhost" 重定向 URI。

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LoginStep 正版登录链路进度步骤。设备码登录产生除 LoginStepAuthorized 外
// 的全部步骤；浏览器登录按顺序产生全部步骤。
const (
	LoginStepAuthorized     = 1 // 已在微软登录页完成授权（拿到授权码）
	LoginStepMicrosoftToken = 2 // 已换取 Microsoft 访问令牌
	LoginStepXboxLive       = 3 // Xbox Live 认证完成
	LoginStepXsts           = 4 // XSTS 授权完成
	LoginStepMinecraft      = 5 // Minecraft 令牌获取完成
	LoginStepDone           = 6 // 档案获取完成，整条链路结束
)

// browserLoginOverallTimeout 内嵌登录整体预算：从打开登录页到链路结束。
// 微软授权页本身可能停留数分钟，10 分钟足够覆盖"慢速输入密码 + 双重验证"。
const browserLoginOverallTimeout = 10 * time.Minute

// MicrosoftBrowserAuthenticator 内嵌浏览器 OAuth 认证器，复用设备码认证器的
// ClientId 与令牌交换实现。
type MicrosoftBrowserAuthenticator struct {
	inner *MicrosoftDeviceCodeAuthenticator
}

// NewMicrosoftBrowserAuthenticator 构造内嵌浏览器认证器；inner 为空时使用
// 全进程共享的 DefaultMicrosoftAuthenticator（必须为设备码实现）。
func NewMicrosoftBrowserAuthenticator(inner *MicrosoftDeviceCodeAuthenticator) *MicrosoftBrowserAuthenticator {
	if inner == nil {
		inner = DefaultMicrosoftAuthenticator.(*MicrosoftDeviceCodeAuthenticator)
	}
	return &MicrosoftBrowserAuthenticator{inner: inner}
}

// oauthCallbackResult 本地回环服务器收到的一次回调结果。
type oauthCallbackResult struct {
	Code            string
	State           string
	ErrorCode       string
	ErrorDescription string
}

// StartBrowserLogin 完整内嵌登录流程：
//  1. 生成 PKCE 与 state，在 127.0.0.1 随机端口起本地回环服务器；
//  2. openLoginPage(authorizeURL)（bindings 层把主窗口导航到微软登录页）；
//  3. 等待回调（用户授权 / 拒绝 / 取消 / 超时）；
//  4. 授权码换 Microsoft 令牌 → XBL → XSTS → Minecraft → 档案。
//
// returnTo 是授权结束后浏览器跳回的启动器页面（通常传前端 window.location.origin，
// 只保留 scheme://host）；onStep 可选，在链路各阶段完成时回调。
func (a *MicrosoftBrowserAuthenticator) StartBrowserLogin(
	ctx context.Context,
	returnTo string,
	openLoginPage func(loginURL string),
	onStep func(step int),
) (MicrosoftAccount, error) {
	if openLoginPage == nil {
		return MicrosoftAccount{}, newMicrosoftAuthError("内嵌登录缺少页面跳转回调。")
	}
	ctx, cancel := context.WithTimeout(ctx, browserLoginOverallTimeout)
	defer cancel()

	verifier, err := newRandomString()
	if err != nil {
		return MicrosoftAccount{}, newMicrosoftAuthErrorWrap("生成 PKCE 校验串失败", err)
	}
	state, err := newRandomString()
	if err != nil {
		return MicrosoftAccount{}, newMicrosoftAuthErrorWrap("生成防伪 state 失败", err)
	}
	returnBase, err := NormalizeReturnBase(returnTo)
	if err != nil {
		return MicrosoftAccount{}, newMicrosoftAuthErrorWrap("回跳地址无效", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return MicrosoftAccount{}, newMicrosoftAuthErrorWrap("本地回调端口监听失败", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	results := make(chan oauthCallbackResult, 1)
	server := &http.Server{Handler: a.newCallbackHandler(state, returnBase, results)}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		_ = server.Close()
	}()

	authorizeURL, err := a.authorizeURL(redirectURI, verifier, state)
	if err != nil {
		return MicrosoftAccount{}, newMicrosoftAuthErrorWrap("构造授权地址失败", err)
	}
	openLoginPage(authorizeURL)

	var result oauthCallbackResult
	select {
	case <-ctx.Done():
		return MicrosoftAccount{}, newMicrosoftAuthErrorWrap(
			"登录已取消或超时，请重试。", ctx.Err())
	case result = <-results:
	}

	if result.ErrorCode != "" {
		return MicrosoftAccount{}, newOAuthCallbackError(result.ErrorCode, result.ErrorDescription)
	}
	if result.State != state {
		return MicrosoftAccount{}, newMicrosoftAuthError("登录回调校验失败（state 不匹配），请重试。")
	}
	if strings.TrimSpace(result.Code) == "" {
		return MicrosoftAccount{}, newMicrosoftAuthError("登录回调缺少授权码，请重试。")
	}
	if onStep != nil {
		onStep(LoginStepAuthorized)
	}

	accessToken, refreshToken, err := a.inner.requestTokenByAuthorizationCode(
		ctx, result.Code, verifier, redirectURI)
	if err != nil {
		return MicrosoftAccount{}, err
	}
	if onStep != nil {
		onStep(LoginStepMicrosoftToken)
	}
	return a.inner.exchangeForMinecraftAccount(ctx, accessToken, refreshToken, onStep)
}

// requestTokenByAuthorizationCode 用授权码 + PKCE 校验串换取 Microsoft 令牌。
func (a *MicrosoftDeviceCodeAuthenticator) requestTokenByAuthorizationCode(
	ctx context.Context,
	code, verifier, redirectURI string,
) (string, string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {a.clientId},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}
	response, body, err := a.postForm(ctx, tokenEndpoint, form)
	if err != nil {
		return "", "", newMicrosoftAuthErrorWrap("换取 Microsoft 令牌失败", err)
	}
	token, err := deserializeToken(body)
	if err != nil {
		return "", "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || strings.TrimSpace(token.AccessToken) == "" {
		detail := strings.TrimSpace(token.ErrorDescription)
		if detail == "" {
			detail = body
		}
		return "", "", newMicrosoftAuthErrorCode(
			fmt.Sprintf("换取 Microsoft 令牌失败（HTTP %d）：%s", response.StatusCode, detail),
			token.Error)
	}
	refresh := token.RefreshToken
	if strings.TrimSpace(refresh) == "" {
		return "", "", newMicrosoftAuthError("授权服务未返回刷新令牌，请重试登录。")
	}
	return token.AccessToken, refresh, nil
}

// newCallbackHandler 构造 /callback 处理器：校验参数、投递结果、
// 302 跳回启动器页面（附 msoauth 标记便于前端识别本次跳回的语境）。
func (a *MicrosoftBrowserAuthenticator) newCallbackHandler(
	state, returnBase string, results chan<- oauthCallbackResult,
) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		result := oauthCallbackResult{
			Code:            query.Get("code"),
			State:           query.Get("state"),
			ErrorCode:       query.Get("error"),
			ErrorDescription: query.Get("error_description"),
		}
		select {
		case results <- result:
		default:
			// 重复回调（浏览器重试/预取）：照常跳回，结果以第一次为准
		}

		target := returnBase + "?msoauth="
		if result.ErrorCode != "" {
			target += "error"
		} else {
			target += "return"
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, target, http.StatusFound)
		// 302 之外的兜底提示（正常情况下浏览器会直接跟随跳转）
		fmt.Fprintf(w,
			`<!doctype html><meta charset="utf-8"><title>NekoLauncher</title>`+
				`<p>登录已完成，若未自动返回启动器，请手动切换窗口。 <a href="%s">返回</a></p>`,
			html.EscapeString(target))
	})
	return mux
}

// authorizeURL 构造微软授权页地址（公共客户端 + PKCE S256）。
func (a *MicrosoftBrowserAuthenticator) authorizeURL(redirectURI, verifier, state string) (string, error) {
	endpoint, err := url.Parse(authorizeEndpoint)
	if err != nil {
		return "", err
	}
	query := url.Values{
		"client_id":             {a.inner.clientId},
		"response_type":         {"code"},
		"redirect_uri":          {redirectURI},
		"scope":                 {microsoftScope},
		"state":                 {state},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
		"prompt":                {"select_account"},
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

// authorizeEndpoint 授权页端点（与令牌端点同租户：consumers，个人 MSA）。
var authorizeEndpoint = "https://login.microsoftonline.com/consumers/oauth2/v2.0/authorize"

// newOAuthCallbackError 把授权页返回的 error 映射为友好中文。
func newOAuthCallbackError(code, description string) error {
	var message string
	switch code {
	case "access_denied":
		message = "你在登录页拒绝了授权请求。"
	default:
		if strings.TrimSpace(description) != "" {
			message = fmt.Sprintf("微软登录页返回错误：%s（%s）", description, code)
		} else {
			message = fmt.Sprintf("微软登录页返回错误：%s", code)
		}
	}
	return newMicrosoftAuthErrorCode(message, code)
}

// NormalizeReturnBase 校验并归一化回跳地址：只保留 scheme://host/。
// 授权回跳不允许携带来源页的路径/查询，避免开放重定向被借道跳往任意页面；
// 路径补齐为 "/"，让浏览器地址与 SPA 的根路由完全一致。
func NormalizeReturnBase(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("回跳地址无效: %q", raw)
	}
	return parsed.Scheme + "://" + parsed.Host + "/", nil
}

// newRandomString 生成 64 字符的 URL 安全随机串（PKCE verifier / state 共用）。
func newRandomString() (string, error) {
	raw := make([]byte, 48)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// pkceChallenge PKCE S256 挑战：BASE64URL(SHA256(verifier))。
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// MarshalJSONString 把字符串序列化为 JSON 字符串字面量（供 bindings 层
// 安全内插到 WebView 执行的 JS 中）。序列化失败（不可能发生）时返回空串。
func MarshalJSONString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(encoded)
}
