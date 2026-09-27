// Package auth 账号敏感字段（AccessToken/RefreshToken）的保护器。
// Windows 上使用 DPAPI（CurrentUser 范围，crypt32.dll，经 syscall 懒加载调用；
// 选型说明见 PORTING_NOTES.md）；其他平台使用「密钥文件 + AES-GCM」的等价方案，
// 密钥文件以 0600 权限保存在启动器存储目录，靠文件系统权限隔离其他用户。
// 加密格式：nyaenc1: + Base64(密文)。平台不支持时回落明文（宁明文不丢数据）。
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/logs"
)

// EncryptedPrefix 加密存储值的前缀，与 C# AccountSecretProtector 保持一致。
const EncryptedPrefix = "nyaenc1:"

// additionalEntropy 应用专属附加熵（与 C# 完全相同的 16 字节，
// Windows 上作为 DPAPI entropy；非 Windows 上参与密钥派生）。
// 阻止「裸 DPAPI blob」被直接搬到其他环境解密，属加固而非硬边界。
var additionalEntropy = []byte{
	0x4E, 0x79, 0x61, 0x4C, 0x61, 0x75, 0x6E, 0x63,
	0x68, 0x65, 0x72, 0x2E, 0x41, 0x63, 0x63, 0x6F,
}

// IsAvailable 当前平台是否支持加密。
func IsAvailable() bool { return platformSecretAvailable() }

// Protect 加密文本；成功返回带 EncryptedPrefix 前缀的存储串，
// 平台不支持或加密失败返回空串（调用方应回落明文以不丢数据）。
func Protect(plaintext string) string {
	if !IsAvailable() {
		return ""
	}
	return platformProtect(plaintext)
}

// Unprotect 解密 Protect 产出的存储串；前缀不匹配、平台不支持或解密失败返回空串。
//
// 兼容历史格式（P3-6 实测发现的真实数据丢失事故）：早期 Windows 版本的
// platformProtect 忘了加 EncryptedPrefix，写进 accounts.yaml 的是"裸 DPAPI blob"。
// 那些值现在仍然只能靠这里解出来——按前缀一刀切会让老用户的所有账号凭空消失
// （loadFromDisk 会把它当明文 JSON，解析失败后回落默认离线账号）。
// 因此：没有前缀时也尝试一次平台解密，成功即视为旧格式；失败返回空串，
// 调用方据此继续按明文处理（明文 JSON 本来就以 '{' 开头，不会被误判）。
func Unprotect(stored string) string {
	if !IsAvailable() {
		return ""
	}
	if strings.HasPrefix(stored, EncryptedPrefix) {
		if len(stored) <= len(EncryptedPrefix) {
			return ""
		}

		return platformUnprotect(stored[len(EncryptedPrefix):])
	}
	if legacyEncryptedBody(stored) == "" {
		return ""
	}

	return platformUnprotect(legacyEncryptedBody(stored))
}

// legacyEncryptedBody 判断一个没有前缀的存储值是否"像是旧格式密文"。
//
// 只做形状判断（非空、无花括号、是合法标准 Base64 且长度够一段密文），
// 避免把明文 JSON、损坏内容、用户手写的注释拿去做无意义的解密尝试。
func legacyEncryptedBody(stored string) string {
	value := strings.TrimSpace(stored)
	if value == "" {
		return ""
	}
	// 明文 JSON 一定以 { 或 [ 开头；这类值直接交回调用方按明文解析
	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		return ""
	}
	if strings.ContainsAny(value, "{}\"") {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(raw) < 16 {
		return ""
	}

	return value
}

// ---------------------------------------------------------------------------
// 非 Windows（及非 DPAPI）平台：密钥文件 + AES-256-GCM。
// 密钥文件 account.secret.key 保存在启动器存储目录，权限 0600；
// 目录本身按平台默认权限创建，等效于 C# 中「非 Windows 保持明文 + 用户目录隔离」
// 的安全级别并略有加强。
// ---------------------------------------------------------------------------

var (
	aesGate       sync.Mutex
	aesCipherText cipher.AEAD
)

// platformSecretAvailable 当前平台是否有可用的加密方案。
// 平台判定与 DPAPI 调用都在平台文件里（secret_protector_windows.go /
// secret_protector_other.go）：dpapi* 只在 Windows 上存在，
// 在共用文件里引用会让本包无法跨平台编译。
func platformSecretAvailable() bool {
	if platformSecretUsesDPAPI() {
		return true
	}
	_, err := loadOrCreateAesKey()
	return err == nil
}

// platformProtect 加密明文：Windows 走 DPAPI，其余平台走 AES-256-GCM。
//
// 两条路径都必须带上 EncryptedPrefix（新格式的规范形态）：少了前缀时
// Save() 写出的是「旧格式裸密文」，读取端只能靠 Unprotect 里的旧格式兼容分支
// 兜底，任何环节一改就会让用户账号静默消失并回落默认账号
// （P3-6 实测事故：DPAPI 分支漏了前缀）。
func platformProtect(plaintext string) string {
	if platformSecretUsesDPAPI() {
		encoded := platformProtectDPAPI(plaintext)
		if encoded == "" {
			return ""
		}
		return EncryptedPrefix + encoded
	}
	return aesProtect(plaintext)
}

// aesProtect 非 Windows 平台的加密实现（密钥文件 + AES-256-GCM）。
// 单独抽出（而非内联在 platformProtect 里）只为单元测试可覆盖：平台分派在
// 编译期决定，Windows 开发机上原本永远跑不到这段跨平台共用的格式逻辑。
func aesProtect(plaintext string) string {
	aead, err := sharedAesCipher()
	if err != nil {
		return ""
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return ""
	}
	// 附加熵作为 AAD 绑定应用身份
	sealed := aead.Seal(nil, nonce, []byte(plaintext), additionalEntropy)
	return EncryptedPrefix + base64.StdEncoding.EncodeToString(append(nonce, sealed...))
}

// platformUnprotect 解密（入参已去掉 EncryptedPrefix）。
func platformUnprotect(encodedBase64 string) string {
	if platformSecretUsesDPAPI() {
		return platformUnprotectDPAPI(encodedBase64)
	}
	return aesUnprotect(encodedBase64)
}

// aesUnprotect 非 Windows 平台的解密实现（入参已去掉 EncryptedPrefix）；
// 与 aesProtect 成对抽出，理由同上。
func aesUnprotect(encodedBase64 string) string {
	raw, err := base64.StdEncoding.DecodeString(encodedBase64)
	if err != nil {
		return ""
	}
	aead, err := sharedAesCipher()
	if err != nil {
		return ""
	}
	nonceSize := aead.NonceSize()
	if len(raw) < nonceSize {
		return ""
	}
	plain, err := aead.Open(nil, raw[:nonceSize], raw[nonceSize:], additionalEntropy)
	if err != nil {
		return ""
	}
	return string(plain)
}

// sharedAesCipher 返回共享 AES-GCM 实例。初始化失败（密钥文件损坏等）
// 不缓存结果：修复外部因素后同一进程内可重试，而不是永远回落明文。
func sharedAesCipher() (cipher.AEAD, error) {
	aesGate.Lock()
	defer aesGate.Unlock()
	if aesCipherText != nil {
		return aesCipherText, nil
	}
	key, err := loadOrCreateAesKey()
	if err != nil {
		return nil, fmt.Errorf("凭据加密初始化失败: %w", err)
	}
	block, blockErr := aes.NewCipher(key)
	if blockErr != nil {
		return nil, fmt.Errorf("凭据加密初始化失败: %w", blockErr)
	}
	gcm, gcmErr := cipher.NewGCM(block)
	if gcmErr != nil {
		return nil, fmt.Errorf("凭据加密初始化失败: %w", gcmErr)
	}
	aesCipherText = gcm
	return aesCipherText, nil
}

// loadOrCreateAesKey 读取或首次生成 AES-256 密钥文件（0600 权限）。
//
// 密钥文件存在但内容损坏（不是合法 base64 / 长度不对）时**重建**它：
// 旧密钥本来就解不开任何东西，留着只会让加密永久失效——于是每次保存都静默回落
// 明文，账号里那些刷新令牌就明文躺在 accounts.yaml 里。重建后新数据恢复正常加密，
// 旧数据仍解不开（那条路径会记 ERROR 并让用户重新登录），但不会更差。
func loadOrCreateAesKey() ([]byte, error) {
	keyPath := filepath.Join(config.DefaultStorageDirectory(), "account.secret.key")
	if data, err := os.ReadFile(keyPath); err == nil {
		decoded, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if decodeErr == nil && len(decoded) == 32 {
			return decoded, nil
		}

		backup := fmt.Sprintf("%s.corrupt-%s", keyPath, time.Now().Format("20060102-150405"))
		if renameErr := os.Rename(keyPath, backup); renameErr != nil {
			return nil, fmt.Errorf("密钥文件损坏且无法备份: %w", renameErr)
		}
		logs.Write("ERROR", fmt.Sprintf(
			"账号密钥文件损坏，已备份为 %s 并重新生成；此前保存的账号需要重新登录。", backup))
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(keyPath, []byte(encoded), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}
