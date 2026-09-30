package bindings

import (
	"testing"

	"nekolauncher/internal/auth"
)

// WebView 出口上的账号必须抹掉凭据令牌：前端渲染用不到它们，
// 留着等于把全部刷新令牌暴露给同 WebView 的插件 JS。
func TestSanitizeAccountStripsTokens(t *testing.T) {
	microsoft := &auth.LaunchAccount{
		Type:        "microsoft",
		DisplayName: "NyaPlayer",
		Microsoft: &auth.MicrosoftAccount{
			Username:     "NyaPlayer",
			Uuid:         "uuid-1",
			AccessToken:  "mc-access",
			RefreshToken: "ms-refresh",
			XboxUserId:   "xuid",
		},
	}
	sanitized := sanitizeAccount(microsoft)
	if sanitized.Microsoft.AccessToken != "" || sanitized.Microsoft.RefreshToken != "" {
		t.Fatalf("微软账号令牌未抹除: %+v", sanitized.Microsoft)
	}
	// 展示字段保留
	if sanitized.Microsoft.Username != "NyaPlayer" || sanitized.Microsoft.Uuid != "uuid-1" {
		t.Fatalf("展示字段不应被抹除: %+v", sanitized.Microsoft)
	}
	// 原对象不受影响（Go 侧启动/刷新仍要用真实令牌）
	if microsoft.Microsoft.AccessToken != "mc-access" || microsoft.Microsoft.RefreshToken != "ms-refresh" {
		t.Fatal("脱敏不应修改存储中的原账号对象")
	}

	authlib := &auth.LaunchAccount{
		Type:        "authlib",
		DisplayName: "皮肤站玩家",
		Authlib: &auth.AuthlibCredential{
			ProfileUuid: "uuid-2",
			ApiRoot:     "https://skin.example/api",
			AccessToken: "ygg-token",
		},
	}
	sanitizedAuthlib := sanitizeAccount(authlib)
	if sanitizedAuthlib.Authlib.AccessToken != "" {
		t.Fatalf("皮肤站令牌未抹除: %+v", sanitizedAuthlib.Authlib)
	}
	if sanitizedAuthlib.Authlib.ApiRoot == "" || sanitizedAuthlib.Authlib.ProfileUuid == "" {
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
