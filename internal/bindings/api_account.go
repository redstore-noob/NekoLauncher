package bindings

// AccountAPI：账号存储、Microsoft 设备码登录、皮肤站（authlib）登录与凭据保护。

import (
	"context"

	"nekolauncher/internal/auth"
	"nekolauncher/internal/launch"
)

// ---- 账号存储（auth.Shared） ----

// GetAccounts 全部账号。
func (a *AccountAPI) GetAccounts() []*auth.LaunchAccount { return auth.Shared.Current() }

// GetSelectedAccount 当前选中账号（无则 nil）。
func (a *AccountAPI) GetSelectedAccount() *auth.LaunchAccount { return auth.Shared.Selected() }

// AddAccount 添加账号并选中。
func (a *AccountAPI) AddAccount(account *auth.LaunchAccount) { auth.Shared.Add(account) }

// RemoveAccount 删除账号。
func (a *AccountAPI) RemoveAccount(account *auth.LaunchAccount) { auth.Shared.Remove(account) }

// MoveAccountToTop 把账号移到列表顶部。
func (a *AccountAPI) MoveAccountToTop(account *auth.LaunchAccount) { auth.Shared.MoveToTop(account) }

// GetAccountStableKey 账号稳定键（跨会话选中标识）。
func (a *AccountAPI) GetAccountStableKey(account *auth.LaunchAccount) string {
	return auth.Shared.GetStableKey(account)
}

// SelectAccountByStableKey 按稳定键选中。
func (a *AccountAPI) SelectAccountByStableKey(key string) bool {
	return auth.Shared.SelectByStableKey(key)
}

// HasOfflineName 是否已存在同名离线账号。
func (a *AccountAPI) HasOfflineName(name string) bool { return auth.Shared.HasOfflineName(name) }

// UpdateMicrosoftAccount 更新正版账号凭据并持久化。
func (a *AccountAPI) UpdateMicrosoftAccount(account *auth.LaunchAccount, ms *auth.MicrosoftAccount) error {
	return auth.Shared.UpdateMicrosoftAccount(account, ms)
}

// UpdateAuthlibAccount 更新皮肤站账号凭据并持久化。
func (a *AccountAPI) UpdateAuthlibAccount(account *auth.LaunchAccount, credential *auth.AuthlibCredential) error {
	return auth.Shared.UpdateAuthlibAccount(account, credential)
}

// UpdateOfflineSkin 更新离线账号皮肤。
func (a *AccountAPI) UpdateOfflineSkin(account *auth.LaunchAccount, skinId string) error {
	return auth.Shared.UpdateOfflineSkin(account, skinId)
}

// CreateOfflineAccount 创建离线账号（校验游戏名合法性、查重）。
func (a *AccountAPI) CreateOfflineAccount(name string) (*auth.LaunchAccount, error) {
	if _, err := launch.NewOfflineAccount(name); err != nil {
		return nil, err
	}
	if auth.Shared.HasOfflineName(name) {
		return nil, errOfflineExists
	}
	account := auth.NewLaunchAccount("offline", name)
	account.OfflineName = name
	auth.Shared.Add(account)
	return account, nil
}

// ---- Microsoft 设备码登录 ----

// LoginMicrosoft 设备码登录全流程。设备码经 "auth:deviceCode" 事件推送给前端展示，
// 后台继续轮询直至完成或失败。返回的账号尚未入库，前端应调用 AddAccount +
// UpdateMicrosoftAccount 持久化（与 C# 侧调用方约定一致）。
func (a *AccountAPI) LoginMicrosoft() (auth.MicrosoftAccount, error) {
	// 使用可取消的 ctx，使 CancelMicrosoftLogin 能中断轮询（见 api_account_ext.go）。
	ctx, done := a.beginMicrosoftLogin()
	defer done()
	return a.microsoft.Authenticate(ctx, func(info auth.DeviceCodeInfo, _ context.Context) {
		emit(a.ctx, "auth:deviceCode", info)
	})
}

// ---- 皮肤站（authlib-injector） ----

// ResolveAuthlibServer 解析皮肤站地址并读取元数据。
func (a *AccountAPI) ResolveAuthlibServer(serverUrl string) (*auth.AuthlibServerInfo, error) {
	return a.authlib.ResolveServer(callCtx(a.ctx), serverUrl)
}

// AuthlibLogin 皮肤站账号密码登录，返回访问令牌与可选角色列表。
// clientToken 传空串时使用本安装的持久化 clientToken。
func (a *AccountAPI) AuthlibLogin(apiRoot, username, password, clientToken string) (*auth.AuthlibLoginResult, error) {
	return a.authlib.Authenticate(callCtx(a.ctx), apiRoot, username, password, clientToken)
}

// GetAuthlibClientToken 读取（或首次生成）本安装的 clientToken。
func (a *AccountAPI) GetAuthlibClientToken() string { return auth.GetOrCreateClientToken() }

// ---- 组件展示账号（组件页身份显示） ----

// 凭据加解密（auth.Protect / auth.Unprotect）刻意不暴露成 Wails 命令：
// 它们会把 DPAPI / AES 加解密变成 WebView（含未沙箱的插件 JS）可任意调用的原语，
// 而前端从来不需要它们——账号的加解密全在 Go 侧完成。
