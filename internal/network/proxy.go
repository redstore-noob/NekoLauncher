package network

// 代理设置：模式（跟随系统 / 直连 / 自定义）+ 自定义地址与凭据。
//
// 生效方式是整体替换 http.DefaultTransport（Clone 后设置 Proxy）：
//   - 未自定义 Transport 的客户端（auth 认证、启动器更新、皮肤缓存、
//     服务器托管下载等）每个请求动态读取 DefaultTransport，替换即生效；
//   - 带统一 User-Agent 包装的下载客户端（tools.SharedHTTPClient、
//     下载源长连接客户端、Java 安装器客户端）以 base==nil 构造包装器、
//     在 RoundTrip 时才解析 DefaultTransport，同样立即生效。
//
// 自定义 Transport 且 base 非空的只有测试代码，不受影响。

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/config"
)

// 代理模式取值。
const (
	ProxyModeSystem = "system" // 跟随系统：Windows 读注册表、Linux 读 gsettings，其余平台环境变量
	ProxyModeOff    = "off"    // 直连：无视系统与环境变量
	ProxyModeCustom = "custom" // 自定义：手填地址（http / https / socks5）
)

// ProxySettings 代理设置（launcher.yaml：proxyMode / proxyAddress /
// proxyUsername / proxyPassword）。
type ProxySettings struct {
	Mode     string `json:"Mode"`
	Address  string `json:"Address"`
	Username string `json:"Username"`
	Password string `json:"Password"`
}

// LoadProxySettings 读取代理设置；未配置或值非法时回落跟随系统。
func LoadProxySettings() ProxySettings {
	mode := config.GetValue("proxyMode")
	switch mode {
	case ProxyModeOff, ProxyModeCustom:
	default:
		mode = ProxyModeSystem
	}
	return ProxySettings{
		Mode:     mode,
		Address:  strings.TrimSpace(config.GetValue("proxyAddress")),
		Username: config.GetValue("proxyUsername"),
		Password: config.GetValue("proxyPassword"),
	}
}

// SaveProxySettings 校验并保存代理设置。地址归一化后保存（缺省补 http://）。
func SaveProxySettings(settings ProxySettings) error {
	mode := settings.Mode
	switch mode {
	case ProxyModeSystem, ProxyModeOff:
	case ProxyModeCustom:
		address := strings.TrimSpace(settings.Address)
		if address == "" {
			return errors.New("自定义代理必须填写代理地址")
		}
		if _, err := normalizeProxyAddress(address); err != nil {
			return err
		}
	default:
		return fmt.Errorf("未知代理模式: %q", mode)
	}

	// config.SetValue 拒收空串：可选字段为空时改走删除，保持 launcher.yaml 干净
	if !config.SetValue("proxyMode", mode) {
		return errors.New("保存代理模式失败")
	}
	saveOptionalConfigValue("proxyAddress", strings.TrimSpace(settings.Address))
	saveOptionalConfigValue("proxyUsername", strings.TrimSpace(settings.Username))
	// 密码不 trim：空格也是合法密码字符
	saveOptionalConfigValue("proxyPassword", settings.Password)
	return nil
}

// saveOptionalConfigValue 保存可空配置项：空值删除键而非写空串。
func saveOptionalConfigValue(key, value string) {
	if strings.TrimSpace(value) == "" {
		config.ClearValue(key)
		return
	}
	config.SetValue(key, value)
}

// normalizeProxyAddress 归一化代理地址：无 scheme 时补 http://，返回完整 URL 形式。
func normalizeProxyAddress(address string) (string, error) {
	raw := strings.TrimSpace(address)
	if raw == "" {
		return "", errors.New("代理地址不能为空")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("代理地址格式无效: %q（示例 127.0.0.1:7890）", address)
	}
	if parsed.Port() == "" {
		return "", fmt.Errorf("代理地址缺少端口: %q（示例 127.0.0.1:7890）", address)
	}
	return parsed.String(), nil
}

// proxyApplyMu 序列化 ApplyProxySettings 的替换动作（多来源同时保存设置时）。
var proxyApplyMu sync.Mutex

// ApplyProxySettings 按当前配置重建全局默认 Transport。启动时与保存设置后调用。
func ApplyProxySettings() {
	proxyApplyMu.Lock()
	defer proxyApplyMu.Unlock()
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return
	}
	clone := base.Clone()
	clone.Proxy = buildProxyFunc(LoadProxySettings())
	http.DefaultTransport = clone
}

// buildProxyFunc 由设置构造 Transport.Proxy 函数；无法构造（自定义模式地址为空
// 或格式非法）时返回直连。
func buildProxyFunc(settings ProxySettings) func(*http.Request) (*url.URL, error) {
	switch settings.Mode {
	case ProxyModeOff:
		return func(*http.Request) (*url.URL, error) { return nil, nil }
	case ProxyModeCustom:
		normalized, err := normalizeProxyAddress(settings.Address)
		if err != nil {
			return func(*http.Request) (*url.URL, error) { return nil, nil }
		}
		target, err := url.Parse(normalized)
		if err != nil || target.Host == "" {
			return func(*http.Request) (*url.URL, error) { return nil, nil }
		}
		if username := strings.TrimSpace(settings.Username); username != "" {
			target.User = url.UserPassword(username, settings.Password)
		}
		return func(*http.Request) (*url.URL, error) { return target, nil }
	default:
		return systemProxyFunc()
	}
}

// systemProxyFunc 跟随系统：平台各自实现 cachedSystemProxy（注册表 / gsettings，
// 10 秒缓存），拿不到回落环境变量。
func systemProxyFunc() func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		if address := cachedSystemProxy(); address != "" {
			if parsed, err := url.Parse(address); err == nil {
				return parsed, nil
			}
		}
		return http.ProxyFromEnvironment(req)
	}
}

// TestProxy 用给定设置构造独立客户端探测微软服务连通性。
// 不触碰全局 DefaultTransport——测试"保存后是否可用"不影响正在进行的下载。
func TestProxy(settings ProxySettings) (string, error) {
	transport := &http.Transport{
		Proxy:               buildProxyFunc(settings),
		TLSHandshakeTimeout: 10 * time.Second,
	}
	if settings.Mode == ProxyModeCustom && strings.TrimSpace(settings.Address) == "" {
		return "", errors.New("代理地址不能为空")
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: transport}
	response, err := client.Head("https://www.microsoft.com")
	if err != nil {
		return "", fmt.Errorf("经当前代理设置访问微软服务失败：%w", err)
	}
	defer response.Body.Close()
	return fmt.Sprintf("连接成功（HTTP %d）", response.StatusCode), nil
}
