package bindings

import (
	"strings"
	"testing"

	"nekolauncher/internal/auth"
)

// WebView 出口上的账号必须按白名单裁剪：前端渲染只用到展示字段，
// 令牌与跨服务可追踪的标识符（档案 UUID / XUID / 登录邮箱）留在后端，
// 否则等于把全部账号的标识数据暴露给同 WebView 的插件 JS。
func TestSanitizeAccountStripsTokensAndIdentifiers(t *testing.T) {
	microsoft := &auth.LaunchAccount{
		Type:        "microsoft",
		DisplayName: "NyaPlayer",
		Microsoft: &auth.MicrosoftAccount{
			Username:     "NyaPlayer",
			Uuid:         "uuid-1",
			AccessToken:  "mc-access",
			RefreshToken: "ms-refresh",
			XboxUserId:   "xuid-1",
			ClientId:     "client-1",
		},
	}
	sanitized := sanitizeAccount(microsoft)
	ms := sanitized.Microsoft
	if ms == nil {
		t.Fatal("微软凭据对象不应为 nil")
	}
	if ms.AccessToken != "" || ms.RefreshToken != "" {
		t.Fatalf("微软账号令牌未抹除: %+v", ms)
	}
	if ms.Uuid != "" || ms.XboxUserId != "" || ms.ClientId != "" {
		t.Fatalf("标识符（Uuid/XboxUserId/ClientId）必须裁剪: %+v", ms)
	}
	// 展示字段保留
	if ms.Username != "NyaPlayer" {
		t.Fatalf("玩家名不应被抹除: %+v", ms)
	}
	// 寻址键：必须有 opaque 键，且不含稳定键里的 UUID 原文
	if sanitized.OpaqueKey == "" {
		t.Fatal("脱敏副本必须携带不可逆寻址键 OpaqueKey")
	}
	if strings.Contains(sanitized.OpaqueKey, "uuid-1") {
		t.Fatalf("OpaqueKey 不应包含 UUID 原文: %s", sanitized.OpaqueKey)
	}
	// 原对象不受影响（Go 侧启动/刷新仍要用真实令牌）
	if microsoft.Microsoft.AccessToken != "mc-access" ||
		microsoft.Microsoft.Uuid != "uuid-1" {
		t.Fatal("脱敏不应修改存储中的原账号对象")
	}

	authlib := &auth.LaunchAccount{
		Type:        "authlib",
		DisplayName: "皮肤站玩家",
		Authlib: &auth.AuthlibCredential{
			Username:     "player@example.com",
			ProfileName:  "皮肤站玩家",
			ProfileUuid:  "uuid-2",
			ApiRoot:      "https://skin.example/api",
			ServerName:   "示例皮肤站",
			AccessToken:  "ygg-token",
		},
	}
	sanitizedAuthlib := sanitizeAccount(authlib)
	cred := sanitizedAuthlib.Authlib
	if cred == nil {
		t.Fatal("皮肤站凭据对象不应为 nil")
	}
	if cred.AccessToken != "" || cred.Username != "" || cred.ProfileUuid != "" {
		t.Fatalf("皮肤站令牌/登录邮箱/角色 UUID 必须裁剪: %+v", cred)
	}
	if cred.ApiRoot == "" || cred.ServerName != "示例皮肤站" {
		t.Fatal("皮肤站展示字段不应被抹除")
	}

	// 离线账号无凭据，原样（拷贝）返回
	offline := &auth.LaunchAccount{Type: "offline", DisplayName: "Player_01"}
	if sanitized := sanitizeAccount(offline); sanitized.Type != "offline" || sanitized.DisplayName != "Player_01" {
		t.Fatalf("离线账号被意外改动: %+v", sanitized)
	}
	if sanitizeAccount(nil) != nil {
		t.Fatal("nil 账号应返回 nil")
	}
}

// 脱敏副本的身份匹配必须走 opaque 键：Uuid/ProfileUuid 已裁剪，
// 直接按稳定键比对会失配，Remove/MoveToTop/Update* 这些
// "前端回传副本"的入口就全断了。
func TestSanitizedCopyKeepsIdentityMatching(t *testing.T) {
	store := auth.Shared
	original := &auth.LaunchAccount{
		Type:        "microsoft",
		DisplayName: "NyaPlayer",
		Microsoft: &auth.MicrosoftAccount{
			Username: "NyaPlayer",
			Uuid:     "uuid-identity",
		},
	}
	store.Add(original)
	defer store.Remove(original)

	sanitized := sanitizeAccount(original)
	if !store.SelectByStableKey(sanitized.OpaqueKey) {
		t.Fatal("opaque 键应能命中真实账号（FindByStableKey 扩展失效）")
	}
	if found := store.FindByStableKey(sanitized.OpaqueKey); found == nil ||
		found.Microsoft == nil || found.Microsoft.Uuid != "uuid-identity" {
		t.Fatal("opaque 键解析应返回存储中的真实账号")
	}
}
