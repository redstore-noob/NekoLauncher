package bindings

// 前端敏感设置的加密存储（AI 提供商 API Key 等）。
//
// 此前 AI Key 明文放 localStorage（WebView2 分区目录里的明文 JSON），与账户凭据
// 走 DPAPI 的待遇不一致。这里给前端一对极简的密钥存取绑定：值经 internal/auth
// 的保护器加密（Windows DPAPI / 其他平台密钥文件 + AES-GCM）后落 launcher.yaml，
// localStorage 只保存非敏感设置。键名强制走 "secret:" 前缀命名空间，与普通配置
// 键隔离，且该前缀被 ConfigAPI 的 WebView 守卫拒绝——前端只能经这里读写。
//
// 平台不支持加密时按 auth 包「宁明文不丢数据」的惯例回落明文（plain: 前缀，
// 读取侧按前缀识别），至少不差于原来的 localStorage 明文。

import (
	"strings"

	"nekolauncher/internal/auth"
	"nekolauncher/internal/config"
)

// secretKeyPrefix 密钥存储键的命名空间前缀；普通 ConfigAPI 访问该前缀会被拒绝。
const secretKeyPrefix = "secret:"

// plainSecretPrefix 平台不支持加密时的明文回落前缀（读取侧据此跳过解密）。
const plainSecretPrefix = "plain:"

// isSecretStorageKey 判断 key 是否属于密钥存储命名空间（供 WebView 守卫使用）。
func isSecretStorageKey(key string) bool {
	return strings.HasPrefix(key, secretKeyPrefix)
}

// normalizeSecretKey 校验并补全键名：非空、去掉前后空白、只允许字母数字与
// 「. _ -」，返回带前缀的完整存储键；非法返回空串。
func normalizeSecretKey(key string) string {
	name := strings.TrimSpace(key)
	if name == "" || len(name) > 128 {
		return ""
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
		default:
			return ""
		}
	}

	return secretKeyPrefix + name
}

// StoreSecret 加密存储一个密钥值（覆盖写；清空传空串）。返回是否落盘成功。
func (a *SystemAPI) StoreSecret(key, plaintext string) bool {
	name := normalizeSecretKey(key)
	if name == "" {
		return false
	}
	if plaintext == "" {
		return config.SetValue(name, "")
	}
	stored := auth.Protect(plaintext)
	if stored == "" {
		// 平台不支持加密：明文回落，不丢数据
		stored = plainSecretPrefix + plaintext
	}

	return config.SetValue(name, stored)
}

// ReadSecret 读取密钥明文；不存在或不可解密返回空串。
func (a *SystemAPI) ReadSecret(key string) string {
	name := normalizeSecretKey(key)
	if name == "" {
		return ""
	}
	stored := config.GetValue(name)
	if stored == "" {
		return ""
	}
	if value, ok := strings.CutPrefix(stored, plainSecretPrefix); ok {
		return value
	}

	return auth.Unprotect(stored)
}
