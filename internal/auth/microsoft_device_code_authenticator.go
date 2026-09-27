package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// DefaultMicrosoftClientId 默认使用 NekoLauncher 自建的 Azure 应用注册
// （多租户 + 个人 MSA，公共客户端）。需注意：自建 Client ID 必须通过
// aka.ms/mce-reviewappid 提交 Mojang 审核，放行前链路最后一步
// Minecraft Services 会返回 403 Invalid app registration。
// 可通过构造函数或环境变量 NEKOLAUNCHER_MSA_CLIENT_ID 覆盖。
const DefaultMicrosoftClientId = "427f0a7c-9edd-40ba-a0ec-f189f8328418"

const microsoftScope = "XboxLive.signin offline_access"

// MicrosoftClientIdOverride 允许通过环境变量覆盖 client_id（例如自建 Azure 应用时）。
func MicrosoftClientIdOverride() string {
	return strings.TrimSpace(os.Getenv("NEKOLAUNCHER_MSA_CLIENT_ID"))
}

// 认证链路各阶段端点。写成包级变量（而非常量）只为单元测试可替换：
// 测试用 httptest 假服务器顶掉这些端点，从而在不访问真实网络的前提下覆盖
// 设备码轮询、刷新与 XBL/XSTS/Minecraft 交换的全部成功/失败分支。
var (
	deviceCodeEndpoint      = "https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode"
	tokenEndpoint           = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	xboxLiveAuthenticateURL = "https://user.auth.xboxlive.com/user/authenticate"
	xstsAuthorizeURL        = "https://xsts.auth.xboxlive.com/xsts/authorize"
	minecraftLoginURL       = "https://api.minecraftservices.com/authentication/login_with_xbox"
	minecraftProfileURL     = "https://api.minecraftservices.com/minecraft/profile"
)

// deviceCodePollWait 等待下一轮设备码轮询。抽成包级变量只为单元测试可替换：
// 测试据此断言语义（尤其是 slow_down 的 +5 秒退避）而不必真的等 5 秒。
// 生产行为与原先内联的 select { ctx.Done / time.After } 完全一致。
var deviceCodePollWait = func(ctx context.Context, interval time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(interval):
		return nil
	}
}

// MicrosoftAuthenticator 正版（Microsoft 账号）认证器，负责完成完整的
// Microsoft → Xbox → Minecraft 登录链路，以及令牌的刷新与有效性校验。
type MicrosoftAuthenticator interface {
	// Authenticate 通过设备码流程完成正版登录。
	// deviceCodeHandler 可选回调，在获得设备码后调用，用于向用户展示验证码并等待
	// 用户完成授权。为 nil 时自动尝试打开系统浏览器跳转到验证页面。
	Authenticate(ctx context.Context, deviceCodeHandler func(DeviceCodeInfo, context.Context)) (MicrosoftAccount, error)
	// Refresh 使用刷新令牌无感刷新账号令牌（无需用户重新授权）。
	Refresh(ctx context.Context, account MicrosoftAccount) (MicrosoftAccount, error)
	// Validate 校验账号令牌是否仍然有效；已过期时自动尝试刷新。
	Validate(ctx context.Context, account MicrosoftAccount) (MicrosoftAccount, error)
}

// MicrosoftAuthentication 正版认证器单一入口：登录浮层、启动管线与档案服务共用
// 同一实例（同一 ClientId）。替换认证实现时只改这一处。
var MicrosoftAuthentication SharedMicrosoftAuthentication

type SharedMicrosoftAuthentication struct{}

// Authenticate 通过设备码流程完成正版登录（委托 DefaultMicrosoftAuthenticator）。
func (SharedMicrosoftAuthentication) Authenticate(ctx context.Context, handler func(DeviceCodeInfo, context.Context)) (MicrosoftAccount, error) {
	return DefaultMicrosoftAuthenticator.Authenticate(ctx, handler)
}

// Refresh 使用刷新令牌无感刷新账号令牌（委托 DefaultMicrosoftAuthenticator）。
func (SharedMicrosoftAuthentication) Refresh(ctx context.Context, account MicrosoftAccount) (MicrosoftAccount, error) {
	return DefaultMicrosoftAuthenticator.Refresh(ctx, account)
}

// Validate 校验账号令牌，过期时自动刷新（委托 DefaultMicrosoftAuthenticator）。
func (SharedMicrosoftAuthentication) Validate(ctx context.Context, account MicrosoftAccount) (MicrosoftAccount, error) {
	return DefaultMicrosoftAuthenticator.Validate(ctx, account)
}

// DefaultMicrosoftAuthenticator 全进程共享的设备码认证器实例。
var DefaultMicrosoftAuthenticator MicrosoftAuthenticator = NewMicrosoftDeviceCodeAuthenticator("", nil)

// MicrosoftDeviceCodeAuthenticator Microsoft 设备码认证实现，完整覆盖正版登录链路：
// OAuth 设备码 → Microsoft access token → XBL 3.0 → XSTS → Minecraft 登录 → 档案获取。
// 同时支持使用 refresh_token 进行无感刷新。
type MicrosoftDeviceCodeAuthenticator struct {
	httpClient *http.Client
	clientId   string
}

// NewMicrosoftDeviceCodeAuthenticator 构造认证器；clientId 为空时依次取
// 环境变量覆盖值与默认值，httpClient 为空时使用内置 30 秒超时客户端。
func NewMicrosoftDeviceCodeAuthenticator(clientId string, httpClient *http.Client) *MicrosoftDeviceCodeAuthenticator {
	if strings.TrimSpace(clientId) == "" {
		clientId = MicrosoftClientIdOverride()
		if clientId == "" {
			clientId = DefaultMicrosoftClientId
		}
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &MicrosoftDeviceCodeAuthenticator{httpClient: httpClient, clientId: clientId}
}

// Authenticate 设备码登录全流程。
func (a *MicrosoftDeviceCodeAuthenticator) Authenticate(
	ctx context.Context,
	deviceCodeHandler func(DeviceCodeInfo, context.Context),
) (MicrosoftAccount, error) {
	deviceCode, err := a.requestDeviceCode(ctx)
	if err != nil {
		return MicrosoftAccount{}, err
	}

	if deviceCodeHandler != nil {
		// 等待调用方完成设备码展示（例如 UI 提示 + 自动打开浏览器）。
		deviceCodeHandler(deviceCode, ctx)
	} else {
		tryOpenVerificationBrowser(deviceCode.VerificationUriFull())
	}

	accessToken, refreshToken, err := a.pollForToken(ctx, &deviceCode)
	if err != nil {
		return MicrosoftAccount{}, err
	}

	return a.exchangeForMinecraftAccount(ctx, accessToken, refreshToken, nil)
}

// Refresh 使用 refresh_token 换取新的完整账号。
func (a *MicrosoftDeviceCodeAuthenticator) Refresh(
	ctx context.Context,
	account MicrosoftAccount,
) (MicrosoftAccount, error) {
	if strings.TrimSpace(account.RefreshToken) == "" {
		return MicrosoftAccount{}, newMicrosoftAuthError("账号没有可用的刷新令牌，请重新登录。")
	}

	accessToken, refreshToken, err := a.requestTokenByRefreshToken(
		ctx, account.RefreshToken,
		// 服务端未返回新 refresh_token（无轮换策略）时回退保留旧值，避免账号被清空
		account.RefreshToken)
	if err != nil {
		return MicrosoftAccount{}, err
	}

	result, err := a.exchangeForMinecraftAccount(ctx, accessToken, refreshToken, nil)
	if err != nil {
		// 令牌 POST 已成功，服务端可能已轮换 refresh_token（旧值随时作废）。
		// 即使后续 XBL/XSTS/档案交换失败，也必须让调用方拿到新令牌持久化，
		// 否则下次刷新必然失败、用户被迫重新登录。
		refreshed := account
		refreshed.RefreshToken = refreshToken
		return MicrosoftAccount{}, &RotatedCredentialsError{
			RefreshedAccount: refreshed,
			Message:          fmt.Sprintf("凭据已刷新，但获取 Minecraft 档案失败：%v", err),
			Inner:            err,
		}
	}
	return result, nil
}

// Validate 校验账号令牌；过期时刷新。
func (a *MicrosoftDeviceCodeAuthenticator) Validate(
	ctx context.Context,
	account MicrosoftAccount,
) (MicrosoftAccount, error) {
	if account.IsExpired() {
		return a.Refresh(ctx, account)
	}
	return account, nil
}

// ---------------------------------------------------------------------------
// 内部实现
// ---------------------------------------------------------------------------

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationUri string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type xboxResponse struct {
	Token         string         `json:"Token"`
	DisplayClaims *displayClaims `json:"DisplayClaims"`
}

type displayClaims struct {
	Xui []xuiClaim `json:"xui"`
}

type xuiClaim struct {
	Uhs string `json:"uhs"`
	Xid string `json:"xid"`
}

type minecraftLoginResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type minecraftProfileResponse struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

func (a *MicrosoftDeviceCodeAuthenticator) requestDeviceCode(ctx context.Context) (DeviceCodeInfo, error) {
	form := url.Values{
		"client_id": {a.clientId},
		"scope":     {microsoftScope},
	}
	response, body, err := a.postForm(ctx, deviceCodeEndpoint, form)
	if err != nil {
		return DeviceCodeInfo{}, newMicrosoftAuthErrorWrap("请求设备码失败", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return DeviceCodeInfo{}, newMicrosoftAuthError(
			fmt.Sprintf("请求设备码失败（HTTP %d）：%s", response.StatusCode, body))
	}

	var payload deviceCodeResponse
	if err := json.Unmarshal([]byte(body), &payload); err != nil || strings.TrimSpace(payload.DeviceCode) == "" {
		return DeviceCodeInfo{}, newMicrosoftAuthError("设备码响应格式不正确。")
	}

	expiresIn := payload.ExpiresIn
	if expiresIn < 1 {
		expiresIn = 1
	}
	interval := payload.Interval
	if interval < 1 {
		interval = 1
	}
	return DeviceCodeInfo{
		UserCode:            payload.UserCode,
		VerificationUri:     payload.VerificationUri,
		DeviceCode:          payload.DeviceCode,
		ExpiresIn:           time.Duration(expiresIn) * time.Second,
		PollIntervalSeconds: interval,
	}, nil
}

func (a *MicrosoftDeviceCodeAuthenticator) pollForToken(
	ctx context.Context,
	deviceCode *DeviceCodeInfo,
) (string, string, error) {
	deadline := time.Now().Add(deviceCode.ExpiresIn)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		if err := deviceCodePollWait(ctx,
			time.Duration(deviceCode.PollIntervalSeconds)*time.Second); err != nil {
			return "", "", err
		}

		form := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {a.clientId},
			"device_code": {deviceCode.DeviceCode},
		}
		_, body, err := a.postForm(ctx, tokenEndpoint, form)
		if err != nil {
			// 瞬时网络错误（超时/连接重置/DNS 抖动）不应终止整个设备码登录：
			// 用户正在浏览器等待输入验证码，直接重试下一轮；外部 ctx 取消才退出
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			var netErr net.Error
			if errors.As(err, &netErr) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				continue
			}
			return "", "", newMicrosoftAuthErrorWrap("设备码轮询失败", err)
		}
		token, err := deserializeToken(body)
		if err != nil {
			return "", "", err
		}

		if strings.TrimSpace(token.AccessToken) != "" {
			return token.AccessToken, token.RefreshToken, nil
		}

		switch token.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			// OAuth 2.0 规范：服务器要求放慢轮询，间隔 +5 秒后继续等待
			deviceCode.PollIntervalSeconds += 5
			continue
		case "authorization_declined":
			return "", "", newMicrosoftAuthErrorCode("你在浏览器中拒绝了授权请求。", AuthorizationDeclined)
		case "expired_token":
			return "", "", newMicrosoftAuthErrorCode("设备码已过期，请重新开始登录。", DeviceCodeExpired)
		case "bad_verification_code":
			return "", "", newMicrosoftAuthErrorCode("设备码无效，请重新开始登录。", "bad_verification_code")
		default:
			description := token.ErrorDescription
			if description == "" {
				description = token.Error
			}
			if description == "" {
				description = "未知错误"
			}
			return "", "", newMicrosoftAuthErrorCode(
				fmt.Sprintf("设备码登录失败：%s", description), token.Error)
		}
	}

	return "", "", newMicrosoftAuthErrorCode("设备码登录超时，请重试。", DeviceCodeExpired)
}

func (a *MicrosoftDeviceCodeAuthenticator) requestTokenByRefreshToken(
	ctx context.Context,
	refreshToken, fallbackRefreshToken string,
) (string, string, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {a.clientId},
		"refresh_token": {refreshToken},
		"scope":         {microsoftScope},
	}
	response, body, err := a.postForm(ctx, tokenEndpoint, form)
	if err != nil {
		return "", "", newMicrosoftAuthErrorWrap("刷新令牌失败", err)
	}
	token, err := deserializeToken(body)
	if err != nil {
		return "", "", err
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 || strings.TrimSpace(token.AccessToken) == "" {
		detail := strings.TrimSpace(token.ErrorDescription)
		if detail != "" {
			detail += "\n" + body
		} else {
			detail = body
		}
		return "", "", newMicrosoftAuthErrorCode(
			fmt.Sprintf("刷新令牌失败（HTTP %d）：%s", response.StatusCode, detail),
			token.Error)
	}

	// 服务端未返回新 refresh_token（无轮换策略）时回退保留旧值，防止账号刷新后被清空
	newRefresh := token.RefreshToken
	if strings.TrimSpace(newRefresh) == "" {
		newRefresh = fallbackRefreshToken
	}
	return token.AccessToken, newRefresh, nil
}

// deserializeToken 反序列化令牌响应；非 JSON 错误体（代理错误页等）包装为友好错误。
func deserializeToken(body string) (*tokenResponse, error) {
	var token tokenResponse
	if err := json.Unmarshal([]byte(body), &token); err != nil {
		return nil, newMicrosoftAuthErrorWrap(
			fmt.Sprintf("令牌服务返回了无法解析的响应：%s", truncateString(body, 200)), err)
	}
	return &token, nil
}

func truncateString(value string, maxLength int) string {
	if len(value) <= maxLength {
		return value
	}
	return value[:maxLength] + "…"
}

// authenticateWithXboxLive XBL 3.0 认证，返回 XBL 令牌与用户哈希（uhs）。
// Xbox Live 与 XSTS API 要求属性名为 PascalCase（实测 camelCase 会返回 400）。
func (a *MicrosoftDeviceCodeAuthenticator) authenticateWithXboxLive(
	ctx context.Context,
	microsoftAccessToken string,
) (string, string, error) {
	payload := map[string]any{
		"RelyingParty": "http://auth.xboxlive.com",
		"TokenType":    "JWT",
		"Properties": map[string]any{
			"AuthMethod": "RPS",
			"SiteName":   "user.auth.xboxlive.com",
			"RpsTicket":  "d=" + microsoftAccessToken,
		},
	}
	response, body, err := a.postJSON(ctx, xboxLiveAuthenticateURL, payload,
		map[string]string{"x-xbl-contract-version": "1"})
	if err != nil {
		return "", "", newMicrosoftAuthErrorWrap("Xbox Live 认证失败", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", newMicrosoftAuthError(
			fmt.Sprintf("Xbox Live 认证失败（HTTP %d）：%s", response.StatusCode, body))
	}

	var parsed xboxResponse
	if err := json.Unmarshal([]byte(body), &parsed); err != nil ||
		strings.TrimSpace(parsed.Token) == "" {
		return "", "", newMicrosoftAuthError("Xbox Live 认证响应格式不正确。")
	}
	uhs := ""
	if parsed.DisplayClaims != nil && len(parsed.DisplayClaims.Xui) > 0 {
		uhs = parsed.DisplayClaims.Xui[0].Uhs
	}
	if strings.TrimSpace(uhs) == "" {
		return "", "", newMicrosoftAuthError("Xbox Live 认证响应格式不正确。")
	}
	return parsed.Token, uhs, nil
}

// authenticateWithXsts XSTS 授权，返回 XSTS 身份令牌与 Xbox 用户 ID（xuid，取自 xui[0].xid）。
// xuid 是正版会话的关键标识，官方启动器通过 --xuid 参数传入游戏。
func (a *MicrosoftDeviceCodeAuthenticator) authenticateWithXsts(
	ctx context.Context,
	xblToken string,
) (string, string, error) {
	payload := map[string]any{
		"RelyingParty": "rp://api.minecraftservices.com/",
		"TokenType":    "JWT",
		"Properties": map[string]any{
			"SandboxId":  "RETAIL",
			"UserTokens": []string{xblToken},
		},
	}
	response, body, err := a.postJSON(ctx, xstsAuthorizeURL, payload,
		map[string]string{"x-xbl-contract-version": "1"})
	if err != nil {
		return "", "", newMicrosoftAuthErrorWrap("XSTS 授权失败", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", xstsError(response.StatusCode, body)
	}

	var parsed xboxResponse
	if json.Unmarshal([]byte(body), &parsed) != nil || strings.TrimSpace(parsed.Token) == "" {
		return "", "", newMicrosoftAuthError("XSTS 授权响应格式不正确。")
	}
	xuid := ""
	if parsed.DisplayClaims != nil && len(parsed.DisplayClaims.Xui) > 0 {
		xuid = parsed.DisplayClaims.Xui[0].Xid
	}
	return parsed.Token, xuid, nil
}

func xstsError(statusCode int, body string) error {
	var errorCode string
	var document map[string]any
	if err := json.Unmarshal([]byte(body), &document); err == nil {
		if xErr, ok := document["XErr"].(float64); ok {
			errorCode = fmt.Sprintf("%.0f", xErr)
		}
	}

	var message string
	switch errorCode {
	case XboxNoAccount:
		message = "该 Microsoft 账号没有 Xbox Live 档案，无法完成 Minecraft 登录。"
	case XboxChildAccount:
		message = "该账号是儿童账号，需要家长将其加入家庭组后才能游玩。"
	case XboxConsentRequired:
		message = "该账号需要获得家长同意（可能未满 18 岁），请先完成相关流程。"
	case XboxRegionBlocked:
		message = "当前国家或地区不支持 Minecraft 服务。"
	case XboxAgeVerification:
		message = "该账号需要先完成年龄验证才能游玩。"
	default:
		message = fmt.Sprintf("XSTS 授权失败（HTTP %d）：%s", statusCode, body)
	}
	return newMicrosoftAuthErrorCode(message, errorCode)
}

// loginWithMinecraft 使用 XSTS 身份令牌换取 Minecraft 访问令牌。
func (a *MicrosoftDeviceCodeAuthenticator) loginWithMinecraft(
	ctx context.Context,
	uhs, xstsToken string,
) (string, int, error) {
	payload := map[string]any{"identityToken": fmt.Sprintf("XBL3.0 x=%s;%s", uhs, xstsToken)}
	response, body, err := a.postJSON(ctx, minecraftLoginURL, payload, nil)
	if err != nil {
		return "", 0, newMicrosoftAuthErrorWrap("Minecraft 登录失败", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusForbidden &&
			strings.Contains(strings.ToLower(body), "account_suspended") {
			return "", 0, newMicrosoftAuthError(
				"该账号已被 Minecraft 服务封禁（ACCOUNT_SUSPENDED），无法登录，需联系官方客服申诉。")
		}
		return "", 0, newMicrosoftAuthError(
			fmt.Sprintf("Minecraft 登录失败（HTTP %d）：%s", response.StatusCode, body))
	}

	var parsed minecraftLoginResponse
	if err := json.Unmarshal([]byte(body), &parsed); err != nil || strings.TrimSpace(parsed.AccessToken) == "" {
		return "", 0, newMicrosoftAuthError("Minecraft 登录响应格式不正确。")
	}
	return parsed.AccessToken, parsed.ExpiresIn, nil
}

// fetchMinecraftProfile 获取玩家档案（UUID 与游戏名），顺带确认账号拥有 Minecraft。
func (a *MicrosoftDeviceCodeAuthenticator) fetchMinecraftProfile(
	ctx context.Context,
	minecraftToken string,
) (*minecraftProfileResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, minecraftProfileURL, nil)
	if err != nil {
		return nil, newMicrosoftAuthErrorWrap("获取 Minecraft 档案失败", err)
	}
	request.Header.Set("Authorization", "Bearer "+minecraftToken)

	response, err := a.httpClient.Do(request)
	if err != nil {
		return nil, newMicrosoftAuthErrorWrap("获取 Minecraft 档案失败", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, newMicrosoftAuthErrorWrap("获取 Minecraft 档案失败", err)
	}
	bodyText := string(body)

	if response.StatusCode == http.StatusNotFound {
		return nil, newMicrosoftAuthError(
			"该 Microsoft 账号尚未购买 Minecraft（Java 版），无法启动游戏。")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, newMicrosoftAuthError(
			fmt.Sprintf("获取 Minecraft 档案失败（HTTP %d）：%s", response.StatusCode, bodyText))
	}

	var parsed minecraftProfileResponse
	if err := json.Unmarshal([]byte(bodyText), &parsed); err != nil ||
		strings.TrimSpace(parsed.Id) == "" || strings.TrimSpace(parsed.Name) == "" {
		return nil, newMicrosoftAuthError("Minecraft 档案响应格式不正确。")
	}
	return &parsed, nil
}

// exchangeForMinecraftAccount 将 Microsoft 令牌逐步交换为完整的正版账号。
// onStep 可选，在每个链路阶段完成时回调（步骤常量见 LoginStep*）；
// 传 nil 表示调用方不关心进度（设备码登录）。
func (a *MicrosoftDeviceCodeAuthenticator) exchangeForMinecraftAccount(
	ctx context.Context,
	microsoftAccessToken, refreshToken string,
	onStep func(step int),
) (MicrosoftAccount, error) {
	xblToken, uhs, err := a.authenticateWithXboxLive(ctx, microsoftAccessToken)
	if err != nil {
		return MicrosoftAccount{}, err
	}
	if onStep != nil {
		onStep(LoginStepXboxLive)
	}
	xstsToken, xstsXuid, err := a.authenticateWithXsts(ctx, xblToken)
	if err != nil {
		return MicrosoftAccount{}, err
	}
	if onStep != nil {
		onStep(LoginStepXsts)
	}
	minecraftToken, expiresInSeconds, err := a.loginWithMinecraft(ctx, uhs, xstsToken)
	if err != nil {
		return MicrosoftAccount{}, err
	}
	if onStep != nil {
		onStep(LoginStepMinecraft)
	}

	// xuid（Xbox 用户 ID）优先取 XSTS 响应中的 xui[0].xid，
	// 该字段是官方记录的 xuid 权威来源；解析失败时回退到
	// 从 Minecraft token 的 JWT payload 中提取。
	// 新版 Minecraft 会将 xuid 为空判定为离线模式（皮肤不加载）。
	xuid := xstsXuid
	if strings.TrimSpace(xuid) == "" {
		xuid = extractXuidFromMinecraftToken(minecraftToken)
	}
	profile, err := a.fetchMinecraftProfile(ctx, minecraftToken)
	if err != nil {
		return MicrosoftAccount{}, err
	}

	expiresAt := time.Now().UTC()
	if expiresInSeconds < 1 {
		expiresInSeconds = 1
	}
	expiresAt = expiresAt.Add(time.Duration(expiresInSeconds) * time.Second)
	return MicrosoftAccount{
		Username:     profile.Name,
		Uuid:         formatCompactUuid(profile.Id),
		AccessToken:  minecraftToken,
		RefreshToken: refreshToken,
		XboxUserId:   xuid,
		ClientId:     a.clientId,
		ExpiresAt:    expiresAt,
	}, nil
}

// extractXuidFromMinecraftToken 从 Minecraft 访问令牌（JWT）的 payload 中解析
// Xbox 用户 ID（xuid）。XSTS 授权响应只返回 uhs 而不返回 xid，而 Minecraft token
// 的 JWT payload 中含有 "xuid" claim（例如 "2535423690656432"）。
func extractXuidFromMinecraftToken(accessToken string) string {
	parts := strings.Split(accessToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			// JWT 解析失败不致命；xuid 为空时游戏仍可离线启动。
			return ""
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	if xuid, ok := claims["xuid"].(string); ok {
		return xuid
	}
	return ""
}

// formatCompactUuid 将档案 UUID 归一化为 32 位无连字符格式（Minecraft profile API
// 原生格式）。官方启动器与主流启动器（HMCL、Prism Launcher 等）均以无连字符格式
// 向游戏传递 --uuid 与 --session 中的 uuid。
func formatCompactUuid(id string) string {
	if id == "" {
		return id
	}
	if len(id) == 32 {
		return id
	}
	return strings.ReplaceAll(id, "-", "")
}

func (a *MicrosoftDeviceCodeAuthenticator) postForm(
	ctx context.Context, endpoint string, form url.Values,
) (*http.Response, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return a.do(request)
}

func (a *MicrosoftDeviceCodeAuthenticator) postJSON(
	ctx context.Context, endpoint string, payload any, headers map[string]string,
) (*http.Response, string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return a.do(request)
}

func (a *MicrosoftDeviceCodeAuthenticator) do(request *http.Request) (*http.Response, string, error) {
	response, err := a.httpClient.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return response, "", err
	}
	return response, string(body), nil
}

// tryOpenVerificationBrowser 尝试打开系统浏览器；失败不影响登录流程，
// 用户仍可手动输入验证地址。
func tryOpenVerificationBrowser(uri string) {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", uri)
	case "darwin":
		command = exec.Command("open", uri)
	default:
		command = exec.Command("xdg-open", uri)
	}
	_ = command.Start()
}
