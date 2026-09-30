package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"nekolauncher/internal/config"
)

// AuthlibServerInfo 解析后的皮肤站信息：归一化的 API 根 + 元数据。
type AuthlibServerInfo struct {
	ApiRoot     string
	ServerName  string
	SkinDomains []string
}

// AuthlibProfileInfo 皮肤站上的一个游戏角色档案（Yggdrasil availableProfiles 条目）。
type AuthlibProfileInfo struct {
	Id   string
	Name string
}

// AuthlibLoginResult 登录成功结果：访问令牌 + 可选角色列表。
type AuthlibLoginResult struct {
	AccessToken string
	Profiles    []AuthlibProfileInfo
	ServerName  string
}

// ClientTokenConfigKey clientToken 的持久化配置键；同一安装内保持不变
// （Yggdrasil 刷新要求一致）。
const ClientTokenConfigKey = "authlibClientToken"

// AuthlibAuthenticator authlib-injector / Yggdrasil 规范皮肤站的认证实现：
// 服务器发现（X-Authlib-Injector-API-Location 归一化）、密码登录、令牌校验与刷新。
type AuthlibAuthenticator struct {
	httpClient *http.Client
}

// DefaultAuthlibAuthenticator 皮肤站认证器单一入口（对应 C# AuthlibAuthentication.Shared）：
// 登录浮层与启动管线共用同一实例。
var DefaultAuthlibAuthenticator = NewAuthlibAuthenticator(nil)

// NewAuthlibAuthenticator 构造认证器；httpClient 为空时使用内置 30 秒超时客户端
// （皮肤站发现阶段依赖最终响应上的 X-Authlib-Injector-API-Location 头，
// 允许自动重定向并读取最终响应头即可）。
func NewAuthlibAuthenticator(httpClient *http.Client) *AuthlibAuthenticator {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &AuthlibAuthenticator{httpClient: httpClient}
}

// GetOrCreateClientToken 读取（或首次生成）本安装的 clientToken。Yggdrasil 规范要求
// 同一客户端的 authenticate/refresh 使用一致的 clientToken，因此持久化到 accounts.yaml。
func GetOrCreateClientToken() string {
	existing := config.GetValue(ClientTokenConfigKey)
	if strings.TrimSpace(existing) != "" {
		return existing
	}
	token := newUUID()
	// 写入失败（极端情况）不影响登录，只退化为每次会话一个新 token：
	// 已有令牌的 validate/refresh 会失败并提示重新登录，不会破坏别的账号
	config.SetValue(ClientTokenConfigKey, token)
	return token
}

// ResolveServer 解析用户输入的皮肤站地址为标准 Yggdrasil API 根：
// 输入可以是根域名（如 littleskin.cn）或完整的 API 根地址；
// 依据 X-Authlib-Injector-API-Location 响应头跳转到真正的 API 根并读取元数据。
func (a *AuthlibAuthenticator) ResolveServer(ctx context.Context, serverUrl string) (*AuthlibServerInfo, error) {
	if strings.TrimSpace(serverUrl) == "" {
		return nil, newAuthlibError("请输入皮肤站地址。")
	}

	trimmed := strings.TrimSpace(serverUrl)
	input, err := normalizeServerUrl(trimmed)
	if err != nil {
		return nil, newAuthlibError(fmt.Sprintf("无法识别的皮肤站地址：%s", serverUrl))
	}

	apiRoot, err := a.discoverApiRoot(ctx, input)
	if err != nil {
		return nil, err
	}
	return a.requestMetadata(ctx, apiRoot)
}

// normalizeServerUrl 允许用户省略协议（如 littleskin.cn），默认按 https 处理。
func normalizeServerUrl(serverUrl string) (string, error) {
	if strings.HasPrefix(serverUrl, "http://") || strings.HasPrefix(serverUrl, "https://") {
		return strings.TrimSuffix(serverUrl, "/"), nil
	}
	return "https://" + serverUrl, nil
}

func (a *AuthlibAuthenticator) discoverApiRoot(ctx context.Context, input string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, input, nil)
	if err != nil {
		return "", newAuthlibErrorWrap(fmt.Sprintf("无法连接皮肤站：%s（请检查地址与网络）", input), err)
	}
	response, err := a.httpClient.Do(request)
	if err != nil {
		return "", newAuthlibErrorWrap(
			fmt.Sprintf("无法连接皮肤站：%s（请检查地址与网络）", hostOf(input)), err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", newAuthlibError(
			fmt.Sprintf("皮肤站 %s 响应异常（HTTP %d）。", hostOf(input), response.StatusCode))
	}

	// 规范：任意页面可通过该头声明真正的 Yggdrasil API 根（相对或绝对地址）
	location := strings.TrimSpace(response.Header.Get("X-Authlib-Injector-API-Location"))
	if location != "" {
		resolved, resolveErr := resolveLocation(input, location)
		if resolveErr == nil {
			// API 根必须带尾斜杠语义上更严谨，但 Yggdrasil 客户端普遍使用无尾斜杠形式，
			// 存储统一为无尾斜杠
			return strings.TrimSuffix(resolved, "/"), nil
		}
	}

	// 未声明 API 位置头：输入地址本身即 API 根（用户直接填了 /api/yggdrasil 的情况）
	return trimToPath(input), nil
}

// resolveLocation 解析相对或绝对的 API 位置声明；相对地址基于最终请求地址。
func resolveLocation(base, location string) (string, error) {
	if strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://") {
		return location, nil
	}
	if strings.HasPrefix(location, "/") {
		index := strings.Index(base[8:], "/")
		if index < 0 {
			return base + location, nil
		}
		return base[:8+index] + location, nil
	}
	// 相对路径：基于当前路径目录解析
	current := trimToPath(base)
	if index := strings.LastIndex(current, "/"); index > 8 {
		current = current[:index]
	}
	return current + "/" + location, nil
}

// trimToPath 去掉查询与片段，仅保留地址路径部分（无尾斜杠）。
func trimToPath(rawUrl string) string {
	result := rawUrl
	for _, separator := range []string{"#", "?"} {
		if index := strings.Index(result, separator); index >= 0 {
			result = result[:index]
		}
	}
	return strings.TrimSuffix(result, "/")
}

func hostOf(rawUrl string) string {
	rest := rawUrl
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(rest, prefix) {
			rest = rest[len(prefix):]
			break
		}
	}
	if index := strings.IndexAny(rest, "/?#"); index >= 0 {
		rest = rest[:index]
	}
	return rest
}

type authlibServerMetadata struct {
	Meta struct {
		ServerName string `json:"serverName"`
	} `json:"meta"`
	SkinDomains []string `json:"skinDomains"`
}

func (a *AuthlibAuthenticator) requestMetadata(ctx context.Context, apiRoot string) (*AuthlibServerInfo, error) {
	response, body, err := a.getWithGuard(ctx, apiRoot, "读取皮肤站元数据失败")
	if err != nil {
		return nil, err
	}
	_ = response

	var metadata authlibServerMetadata
	if err := json.Unmarshal([]byte(body), &metadata); err != nil {
		return nil, newAuthlibErrorWrap("该地址不是有效的 authlib-injector 皮肤站（元数据解析失败）。", err)
	}
	return &AuthlibServerInfo{
		ApiRoot:     apiRoot,
		ServerName:  metadata.Meta.ServerName,
		SkinDomains: metadata.SkinDomains,
	}, nil
}

// Authenticate 使用账号密码登录皮肤站，返回访问令牌与可选角色列表。
func (a *AuthlibAuthenticator) Authenticate(
	ctx context.Context,
	apiRoot, username, password, clientToken string,
) (*AuthlibLoginResult, error) {
	if strings.TrimSpace(apiRoot) == "" || strings.TrimSpace(username) == "" ||
		strings.TrimSpace(password) == "" {
		// 参数来自 UI 输入，缺失按错误返回而非 panic（绑定协程不 recover）
		return nil, errors.New("皮肤站地址、用户名与密码均不能为空。")
	}
	if strings.TrimSpace(clientToken) == "" {
		clientToken = GetOrCreateClientToken()
	}

	payload := map[string]any{
		"agent":       map[string]any{"name": "MinecraftLauncher", "version": 1},
		"username":    strings.TrimSpace(username),
		"password":    password,
		"clientToken": clientToken,
		"requestUser": true,
	}
	response, body, err := a.postJSON(ctx, strings.TrimSuffix(apiRoot, "/")+"/authserver/authenticate", payload)
	if err != nil {
		return nil, newAuthlibErrorWrap("连接皮肤站失败（请检查网络后重试）。", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, createLoginFailureError(response.StatusCode, body)
	}

	var parsed struct {
		AccessToken       string `json:"accessToken"`
		ClientToken       string `json:"clientToken"`
		AvailableProfiles []struct {
			Id   string `json:"id"`
			Name string `json:"name"`
		} `json:"availableProfiles"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, newAuthlibErrorWrap("皮肤站登录响应解析失败。", err)
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return nil, newAuthlibError("皮肤站未返回有效的访问令牌。")
	}

	profiles := make([]AuthlibProfileInfo, 0, len(parsed.AvailableProfiles))
	for _, profile := range parsed.AvailableProfiles {
		if strings.TrimSpace(profile.Id) != "" && strings.TrimSpace(profile.Name) != "" {
			profiles = append(profiles, AuthlibProfileInfo{Id: profile.Id, Name: profile.Name})
		}
	}
	return &AuthlibLoginResult{AccessToken: parsed.AccessToken, Profiles: profiles}, nil
}

// createLoginFailureError 把皮肤站登录失败的响应翻译为用户可读的错误。
func createLoginFailureError(statusCode int, body string) error {
	var errorPayload struct {
		Error        string `json:"error"`
		ErrorMessage string `json:"errorMessage"`
	}
	_ = json.Unmarshal([]byte(body), &errorPayload)
	errorMessage := errorPayload.ErrorMessage
	errorCode := errorPayload.Error

	// 皮肤站开了验证码（Blessing Skin 私有行为，Yggdrasil 规范无此扩展）：
	// 密码登录无法携带验证码，返回结构化错误，由前端引导用户到网页端完成
	// 验证后回来重试（验证通过后站点通常有一段免验证宽限期）
	if errorMessage != "" &&
		(strings.Contains(errorMessage, "验证码") ||
			strings.Contains(strings.ToLower(errorMessage), "captcha")) {
		return newAuthlibCaptchaError(
			"该皮肤站开启了验证码，请在浏览器中打开皮肤站完成一次登录验证，然后回来重试。")
	}

	if errorCode != "" && errorMessage != "" &&
		strings.Contains(errorCode, "ForbiddenOperationException") &&
		(strings.Contains(strings.ToLower(errorMessage), "credentials") ||
			strings.Contains(strings.ToLower(errorMessage), "password") ||
			strings.Contains(errorMessage, "用户")) {
		return newAuthlibError("用户名或密码错误。")
	}

	if strings.TrimSpace(errorMessage) == "" {
		return newAuthlibError(fmt.Sprintf("登录失败（HTTP %d）。", statusCode))
	}
	return newAuthlibError(fmt.Sprintf("登录失败：%s", errorMessage))
}

// ValidateOrRefresh 启动前的令牌保活：validate 通过则原样返回；失效则 refresh 换新令牌；
// refresh 也失败（令牌被吊销/密码已改）时返回错误提示重新登录。
// 返回可用的访问令牌（可能为刷新后的新值）。
func (a *AuthlibAuthenticator) ValidateOrRefresh(
	ctx context.Context,
	credential *AuthlibCredential,
	clientToken string,
) (string, error) {
	if credential == nil {
		return "", errors.New("credential 不能为空")
	}
	if strings.TrimSpace(credential.ApiRoot) == "" || strings.TrimSpace(credential.AccessToken) == "" {
		return "", errors.New("皮肤站凭据缺少 ApiRoot 或 AccessToken。")
	}
	if strings.TrimSpace(clientToken) == "" {
		clientToken = GetOrCreateClientToken()
	}
	root := strings.TrimSuffix(credential.ApiRoot, "/")

	// validate 返回 403/400 表示令牌失效，属预期分支：不抛错误，落到下方 refresh
	response, _, err := a.postJSON(ctx, root+"/authserver/validate", map[string]any{
		"accessToken": credential.AccessToken,
		"clientToken": clientToken,
	})
	if err != nil {
		return "", newAuthlibErrorWrap("校验皮肤站令牌失败（网络异常）。", err)
	}
	// Yggdrasil 规范：204 No Content = 令牌有效
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return credential.AccessToken, nil
	}

	// 令牌失效：尝试无感刷新
	refreshResponse, body, err := a.postJSON(ctx, root+"/authserver/refresh", map[string]any{
		"accessToken": credential.AccessToken,
		"clientToken": clientToken,
		"requestUser": true,
	})
	if err != nil {
		return "", newAuthlibErrorWrap("刷新皮肤站令牌失败（网络异常）。", err)
	}
	if refreshResponse.StatusCode < 200 || refreshResponse.StatusCode >= 300 {
		return "", newAuthlibError("皮肤站登录状态已失效，请重新登录该账号。")
	}

	var parsed struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return "", newAuthlibErrorWrap("皮肤站令牌刷新响应解析失败。", err)
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return "", newAuthlibError("皮肤站登录状态已失效，请重新登录该账号。")
	}
	return parsed.AccessToken, nil
}

func (a *AuthlibAuthenticator) getWithGuard(ctx context.Context, url, failureMessage string) (*http.Response, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", newAuthlibErrorWrap(failureMessage+"（网络异常）。", err)
	}
	response, err := a.httpClient.Do(request)
	if err != nil {
		return nil, "", newAuthlibErrorWrap(failureMessage+"（网络异常）。", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return response, "", newAuthlibErrorWrap(failureMessage+"（网络异常）。", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, "", newAuthlibError(
			fmt.Sprintf("%s（HTTP %d）。", failureMessage, response.StatusCode))
	}
	return response, string(body), nil
}

// postJSON POST JSON；只对网络层异常包装为用户可读消息。
// HTTP 状态码由调用方自行判断（validate 的 403 是预期分支，不能在这里报错）。
func (a *AuthlibAuthenticator) postJSON(ctx context.Context, url string, payload any) (*http.Response, string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("Content-Type", "application/json")
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

// newUUID 生成带连字符小写 UUID（与 C# Guid.NewGuid().ToString("D") 一致）。
func newUUID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		// crypto/rand 失败极罕见；退化用时间戳构造，仍保证格式合法
		binary.BigEndian.PutUint64(bytes[:8], uint64(time.Now().UnixNano()))
		binary.BigEndian.PutUint64(bytes[8:], uint64(time.Now().UnixNano()<<1))
	}
	// RFC 4122 version 4 / variant
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}
