package auth

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"nekolauncher/internal/config"
)

// TestLiveAccountsStoreReadable 真读一次本机 accounts.yaml，确认账号数据能解开。
//
// 存在的意义：P3-6 实测发现过一个"只影响 Windows"的数据丢失级缺陷——
// platformProtect 忘了加 nyaenc1: 前缀，写出的裸 DPAPI blob 在下次启动被当成
// 明文 JSON，解析失败后静默回落默认离线账号。这条用例把"本机现有数据到底能不能读"
// 变成可复现的检查（只回传计数与布尔值，绝不打印账号名/令牌）。
//
//	NEKO_LIVE_ACCOUNTS=1 go test ./internal/auth/ -run TestLiveAccountsStoreReadable -v
func TestLiveAccountsStoreReadable(t *testing.T) {
	if os.Getenv("NEKO_LIVE_ACCOUNTS") != "1" {
		t.Skip("设置 NEKO_LIVE_ACCOUNTS=1 才读本机真实账号数据")
	}

	stored := config.GetValue(AccountsConfigKey)
	if strings.TrimSpace(stored) == "" {
		t.Skip("本机没有保存过账号（accounts 键为空）")
	}

	hasPrefix := strings.HasPrefix(stored, EncryptedPrefix)
	plaintext := Unprotect(stored)
	if plaintext == "" {
		t.Fatalf("账号数据解不开：前缀=%v，长度=%d（需要重新登录，或平台加密不可用）",
			hasPrefix, len(stored))
	}

	var dtos []accountDto
	if err := json.Unmarshal([]byte(plaintext), &dtos); err != nil {
		t.Fatalf("解密结果不是账号 JSON：%v", err)
	}

	t.Logf("账号数据可读：前缀=%v，账号数=%d", hasPrefix, len(dtos))
}
