package auth

import "time"

// LaunchAccount 一个可选用的启动账号（持久化于 accounts.yaml 的 accounts 键）。
type LaunchAccount struct {
	// Type "offline" | "microsoft" | "authlib" | 未来其它提供方。
	Type string
	// DisplayName 展示名。
	DisplayName string
	// OfflineName 离线账号的游戏名；非离线账号为空。
	OfflineName string
	// OfflineSkinId 离线账号皮肤；默认 "steve"。
	OfflineSkinId string
	// Microsoft 正版账号凭据；非正版账号为 nil。
	Microsoft *MicrosoftAccount
	// Authlib 皮肤站账号凭据；非皮肤站账号为 nil。
	Authlib *AuthlibCredential
	// OpaqueKey 稳定键的不可逆形式（SHA-256 截断 hex），只填充在下发给
	// WebView 的脱敏副本上（见 bindings.sanitizeAccount）：真实稳定键内含
	// 档案 UUID/XUID 等跨服务可追踪标识符，不能离开后端。域内代码寻址
	// 一律用 AccountStoreService.GetStableKey，不要读写此字段。
	OpaqueKey string
}

// NewLaunchAccount 构造账号并填充默认值（对应 C# 属性初始化器）。
func NewLaunchAccount(accountType, displayName string) *LaunchAccount {
	return &LaunchAccount{
		Type:          accountType,
		DisplayName:   displayName,
		OfflineSkinId: "steve",
	}
}

// Badge 账号类型角标文本。
func (a *LaunchAccount) Badge() string {
	switch a.Type {
	case "microsoft":
		return "正版"
	case "offline":
		return "离线"
	case "authlib":
		return "皮肤站"
	default:
		return "第三方"
	}
}

// DeviceCodeInfo Microsoft OAuth 设备码登录过程中需要展示给用户的信息。
// 调用方应在回调中展示 UserCode 和验证地址，等待用户在浏览器中完成授权。
type DeviceCodeInfo struct {
	UserCode            string
	VerificationUri     string
	DeviceCode          string
	ExpiresIn           time.Duration
	PollIntervalSeconds int
}

// VerificationUriFull 预填了用户码的完整验证地址，可直接用于打开浏览器。
func (d DeviceCodeInfo) VerificationUriFull() string {
	return d.VerificationUri + "?user_code=" + d.UserCode
}
