//go:build windows

package auth

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestPlatformSecretUsesDPAPIOnWindows Windows 上必须判定为「有可用加密方案」，
// 否则账号令牌会静默回落明文写进 accounts.yaml。
// 防的回归：平台判定被改坏（例如误返回 false，或密钥文件方案在 Windows 上被误用）
// 导致正版刷新令牌以明文落盘。
func TestPlatformSecretUsesDPAPIOnWindows(t *testing.T) {
	if !platformSecretUsesDPAPI() {
		t.Fatal("Windows 上 platformSecretUsesDPAPI() 必须为 true")
	}
	if !IsAvailable() {
		t.Fatal("Windows 上 DPAPI 恒可用，IsAvailable() 不应为 false")
	}
}

// TestDpapiProtectEmptyPlaintextReturnsEmpty 空明文必须返回空串。
// 防的回归：dpapiProtect 里 &plain[0] 对空切片越界 panic——登录流程里
// 空令牌是常见情况（离线账号 / 服务端没返回 refresh_token），一 panic
// 就是整个账号保存流程崩掉。
func TestDpapiProtectEmptyPlaintextReturnsEmpty(t *testing.T) {
	if got := dpapiProtect(""); got != "" {
		t.Fatalf("dpapiProtect(\"\") = %q，期望空串", got)
	}
	if got := Protect(""); got != "" {
		t.Fatalf("Protect(\"\") = %q，期望空串", got)
	}
}

// TestDpapiUnprotectRejectsGarbage 非 DPAPI 密文必须被拒绝而不是返回垃圾。
// 防的回归：CryptUnprotectData 的返回值被忽略，把未初始化的输出缓冲区
// 当成解密结果（可能把别的内存内容当成令牌写回账号）。
func TestDpapiUnprotectRejectsGarbage(t *testing.T) {
	cases := map[string]string{
		"空串":                      "",
		"非法 Base64":               "!!!not-base64!!!",
		"合法 Base64 但非 DPAPI blob": base64.StdEncoding.EncodeToString([]byte("not-a-dpapi-blob")),
		"被截断的 DPAPI blob":         base64.StdEncoding.EncodeToString([]byte{0x01, 0x00, 0x00, 0x00}),
	}
	for name, stored := range cases {
		if got := dpapiUnprotect(stored); got != "" {
			t.Fatalf("%s：dpapiUnprotect(%q) = %q，期望空串", name, stored, got)
		}
		// 走公共入口时也必须被前缀判断挡住
		if got := Unprotect(EncryptedPrefix + stored); got != "" {
			t.Fatalf("%s：Unprotect 前缀+垃圾 = %q，期望空串", name, got)
		}
	}
}

// TestDpapiProtectOutputFormat DPAPI 分支的产物必须是「前缀 + Base64(blob)」。
// 防的回归：前缀在 DPAPI 分支丢失——Save() 写出的账号密文下次启动被当成
// 明文 JSON 解析失败，用户全部账号静默消失（真实发生过：本机 accounts.yaml
// 里就是一条无前缀的裸 DPAPI blob）。
func TestDpapiProtectOutputFormat(t *testing.T) {
	stored := Protect("refresh-token")
	if !strings.HasPrefix(stored, EncryptedPrefix) {
		t.Fatalf("DPAPI 加密产物缺少 %q 前缀：%q", EncryptedPrefix, stored)
	}
	payload := stored[len(EncryptedPrefix):]
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("前缀之后不是合法 Base64：%v", err)
	}
	if len(raw) == 0 {
		t.Fatal("DPAPI 密文不应为空")
	}
	if got := Unprotect(stored); got != "refresh-token" {
		t.Fatalf("往返结果 = %q，期望 refresh-token", got)
	}
}
