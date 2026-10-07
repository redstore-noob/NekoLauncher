package bindings

// AccountAPI：账号存储、Microsoft 设备码登录、皮肤站（authlib）登录与凭据保护。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nekolauncher/internal/auth"
	"nekolauncher/internal/launch"
)

// ---- 账号存储（auth.Shared） ----

// sanitizeAccount 把账号裁剪成"前端渲染所需的白名单字段"再交给 WebView。
//
// 前端只需要用户名/类型/皮肤站等展示字段，从来用不到
// AccessToken/RefreshToken（登录后的持久化经 AddAccount/Update* 走的是
// 新登录结果，不依赖读回）。而 Wails 绑定挂在 window.go 上，插件 JS
// 与宿主同处一个 WebView，可以绕过插件 API 的权限门直接调用——
// 因此必须按白名单裁剪而非黑名单抹令牌：官方档案 UUID、XUID 这类
// 跨服务可追踪的稳定标识符同样不能下发（未沙箱的插件可借此给
// 全部账号做画像）。裁剪不影响 Remove/MoveToTop/Update*：
// 它们按稳定键匹配身份，不比这些字段。
func sanitizeAccount(account *auth.LaunchAccount) *auth.LaunchAccount {
	if account == nil {
		return nil
	}
	clone := *account
	// 寻址用不可逆键：头像/皮肤/切换选中等前端寻址全部经它完成，
	// 真实稳定键（内含 UUID/登录名原文）不再下发
	clone.OpaqueKey = auth.Shared.OpaqueStableKey(account)
	// 经存储在 gate 读锁内取凭据拷贝：Update* 持写锁原地替换字段，
	// 直接解引用与并发更新构成 data race
	if microsoft := auth.Shared.MicrosoftSnapshot(account); microsoft != nil {
		// 只留游戏内玩家名与令牌到期时间（UI 倒计时用，低敏感）；
		// Uuid/XUID/ClientId/两类令牌全部不下发
		clone.Microsoft = &auth.MicrosoftAccount{
			Username:  microsoft.Username,
			ExpiresAt: microsoft.ExpiresAt,
		}
	}
	if credential := account.Authlib; credential != nil {
		// 只留展示字段；登录用户名（多为邮箱）、角色 UUID、令牌不下发
		clone.Authlib = &auth.AuthlibCredential{
			ProfileName: credential.ProfileName,
			ApiRoot:     credential.ApiRoot,
			ServerName:  credential.ServerName,
		}
	}
	return &clone
}

// GetAccounts 全部账号（凭据令牌已抹除，见 sanitizeAccount）。
func (a *AccountAPI) GetAccounts() []*auth.LaunchAccount {
	accounts := auth.Shared.Current()
	out := make([]*auth.LaunchAccount, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, sanitizeAccount(account))
	}
	return out
}

// GetSelectedAccount 当前选中账号（无则 nil；凭据令牌已抹除）。
func (a *AccountAPI) GetSelectedAccount() *auth.LaunchAccount {
	return sanitizeAccount(auth.Shared.Selected())
}

// AddAccount 添加账号并选中。
func (a *AccountAPI) AddAccount(account *auth.LaunchAccount) { auth.Shared.Add(account) }

// RemoveAccount 删除账号。
func (a *AccountAPI) RemoveAccount(account *auth.LaunchAccount) { auth.Shared.Remove(account) }

// MoveAccountToTop 把账号移到列表顶部。
func (a *AccountAPI) MoveAccountToTop(account *auth.LaunchAccount) { auth.Shared.MoveToTop(account) }

// GetAccountStableKey 账号的 WebView 侧寻址键（不可逆）。
// 真实稳定键内含档案 UUID / 登录名原文，任何情况下都不再下发；
// 头像/皮肤/切换选中等后端寻址（FindByStableKey）同时接受该 opaque 形式。
func (a *AccountAPI) GetAccountStableKey(account *auth.LaunchAccount) string {
	return auth.Shared.OpaqueStableKey(account)
}

// AccountSummary 账号摘要：key 为不可逆寻址键，头像由后端代取。
type AccountSummary struct {
	Key    string `json:"Key"`
	Name   string `json:"Name"`
	Type   string `json:"Type"`
	Avatar string `json:"Avatar"`
}

// GetAccountSummaries 全部账号的摘要（含后端代取的头像）。
// 插件 API 与小组件用它替代"完整账号 + 前端算键 + 前端取头像"的旧链路，
// 旧链路会把真实稳定键暴露给 WebView。
func (a *AccountAPI) GetAccountSummaries() []AccountSummary {
	accounts := auth.Shared.Current()
	out := make([]AccountSummary, 0, len(accounts))
	for _, account := range accounts {
		key := auth.Shared.OpaqueStableKey(account)
		avatar, err := a.GetAvatarUrl(key)
		if err != nil {
			avatar = ""
		}
		out = append(out, AccountSummary{
			Key:    key,
			Name:   account.DisplayName,
			Type:   account.Type,
			Avatar: avatar,
		})
	}
	return out
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
// 后台继续轮询直至完成或失败。成功后**直接在后端入库**（同档案更新并置顶），
// 只把玩家名返回给前端——完整凭据（两类令牌/XUID/UUID）不经过 WebView：
// 插件与宿主同 WebView，任何下发的值都可能被未沙箱的插件截走。
func (a *AccountAPI) LoginMicrosoft() (string, error) {
	// 使用可取消的 ctx，使 CancelMicrosoftLogin 能中断轮询（见 api_account_ext.go）。
	ctx, done := a.beginMicrosoftLogin()
	defer done()
	account, err := a.microsoft.Authenticate(ctx, func(info auth.DeviceCodeInfo, _ context.Context) {
		emit(a.ctx, "auth:deviceCode", info)
	})
	if err != nil {
		return "", err
	}
	a.persistBrowserLoginResult(account)
	return account.Username, nil
}

// ---- 皮肤站（authlib-injector） ----

// ResolveAuthlibServer 解析皮肤站地址并读取元数据。
func (a *AccountAPI) ResolveAuthlibServer(serverUrl string) (*auth.AuthlibServerInfo, error) {
	return a.authlib.ResolveServer(callCtx(a.ctx), serverUrl)
}

// AuthlibProfileView 角色选择的 WebView 视图：只有序号与角色名。
// 角色 UUID 与访问令牌留在后端的待确认会话里。
type AuthlibProfileView struct {
	Index int    `json:"Index"`
	Name  string `json:"Name"`
}

// AuthlibLogin 皮肤站账号密码登录，返回可选角色列表（仅名称与序号）。
// 访问令牌与角色 UUID 存入后端待确认会话，由 ConfirmAuthlibProfile 完成
// 选角与入库——令牌绝不经过 WebView。
func (a *AccountAPI) AuthlibLogin(apiRoot, username, password string) ([]AuthlibProfileView, error) {
	result, err := a.authlib.Authenticate(callCtx(a.ctx), apiRoot, username, password, "")
	if err != nil {
		return nil, err
	}
	a.authlibMu.Lock()
	a.authlibPending = &authlibPendingSession{
		credential: auth.AuthlibCredential{
			Username:    username,
			AccessToken: result.AccessToken,
			ApiRoot:     apiRoot,
			ServerName:  result.ServerName,
		},
		profiles: result.Profiles,
	}
	a.authlibMu.Unlock()
	views := make([]AuthlibProfileView, 0, len(result.Profiles))
	for index, profile := range result.Profiles {
		views = append(views, AuthlibProfileView{Index: index, Name: profile.Name})
	}
	return views, nil
}

// ConfirmAuthlibProfile 选定待确认会话中的角色并入库（同角色 UUID + API 根
// 已存在则更新凭据并置顶），返回角色名。选角（或取消重登）都会消费掉会话。
func (a *AccountAPI) ConfirmAuthlibProfile(index int) (string, error) {
	a.authlibMu.Lock()
	pending := a.authlibPending
	a.authlibPending = nil
	a.authlibMu.Unlock()
	if pending == nil {
		return "", errors.New("没有待确认的皮肤站登录会话，请重新登录")
	}
	if index < 0 || index >= len(pending.profiles) {
		return "", fmt.Errorf("角色序号越界：%d", index)
	}
	profile := pending.profiles[index]
	credential := pending.credential
	credential.ProfileName = profile.Name
	credential.ProfileUuid = profile.Id
	mergeAuthlibCredential(credential)
	return profile.Name, nil
}

// mergeAuthlibCredential 皮肤站凭据入库：同一角色 UUID + API 根已存在
// 则更新凭据并置顶，否则新增。去重必须按稳定键在后端完成。
func mergeAuthlibCredential(credential auth.AuthlibCredential) {
	probe := auth.NewLaunchAccount("authlib", credential.ProfileName)
	probe.Authlib = &credential
	target := auth.Shared.GetStableKey(probe)
	for _, candidate := range auth.Shared.Current() {
		if candidate.Type == "authlib" &&
			strings.EqualFold(auth.Shared.GetStableKey(candidate), target) {
			_ = auth.Shared.UpdateAuthlibAccount(candidate, &credential)
			auth.Shared.MoveToTop(candidate)
			return
		}
	}
	auth.Shared.Add(probe)
}

// ---- 组件展示账号（组件页身份显示） ----

// 凭据加解密（auth.Protect / auth.Unprotect）刻意不暴露成 Wails 命令：
// 它们会把 DPAPI / AES 加解密变成 WebView（含未沙箱的插件 JS）可任意调用的原语，
// 而前端从来不需要它们——账号的加解密全在 Go 侧完成。
