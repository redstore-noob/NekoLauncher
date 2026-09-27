package auth

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnprotectRejectsPlaintextAndEmptyPayload 没有 nyanc1: 前缀、或前缀后没有内容
// 的存储值必须一律返回空串（而不是把明文当密文解、或返回半个串）。
// 防的回归：把明文令牌/明文 JSON 当密文处理，账号加载直接失败；以及前缀被误剥后
// 用空串去解密（某些平台实现会 panic 或返回垃圾）。
//
// 注意：历史裸密文（无前缀）现在是**允许**尝试解密的（见 TestLegacyEncryptedBodyShape），
// 所以这里只断言「明文与空值不会被当成密文」与「解不开的旧格式值返回空串」。
func TestUnprotectRejectsPlaintextAndEmptyPayload(t *testing.T) {
	cases := []struct {
		name   string
		stored string
	}{
		{name: "空串", stored: ""},
		{name: "只有空白字符", stored: "   "},
		{name: "明文令牌", stored: "ya29.a0AfH6SMBxxxxxxxx"},
		{name: "明文账号 JSON 数组", stored: `[{"Type":"offline","OfflineName":"Steve"}]`},
		{name: "明文账号 JSON 对象", stored: `{"Type":"microsoft","Username":"NyaPlayer"}`},
		{name: "只有前缀", stored: EncryptedPrefix},
		{name: "前缀后的空内容", stored: EncryptedPrefix + " "},
		{
			// 旧格式裸密文的头部（DPAPI blob 前 16 字节）：形状像旧格式但解不开，
			// 必须返回空串，让调用方继续按明文处理（解析失败 → 回落默认账号）
			name:   "像旧格式密文但被截断解不开",
			stored: "AQAAANCMnd8BFdERjHoAwE/Cl+sBAAAA",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Unprotect(testCase.stored); got != "" {
				t.Fatalf("Unprotect(%q) = %q，期望空串（不得把明文/损坏值当密文接受）",
					testCase.stored, got)
			}
		})
	}
}

// TestLegacyEncryptedBodyShape 旧格式兼容分支的形状判断：只有「看起来像一段密文」
// 的无前缀值才允许进入解密尝试。
// 防的回归：形状判断放宽后，明文 JSON、手写注释、损坏内容都会被丢去做无意义的
// 平台解密（DPAPI 会对垃圾调用一次系统 API，AES 分支会浪费一次 GCM 尝试）；
// 判断收紧过头则老用户的裸密文永远解不开（账号静默消失）。
func TestLegacyEncryptedBodyShape(t *testing.T) {
	cases := []struct {
		name   string
		stored string
		want   string
	}{
		{name: "空串", stored: "", want: ""},
		{name: "空白", stored: "  \t ", want: ""},
		{name: "明文 JSON 数组", stored: `[{"Type":"offline"}]`, want: ""},
		{name: "明文 JSON 对象", stored: `{"Type":"offline"}`, want: ""},
		{name: "含引号的注释", stored: `do not edit "this"`, want: ""},
		{name: "非法 Base64", stored: "!!!not-base64!!!", want: ""},
		{
			name:   "合法 Base64 但太短（不足一段密文）",
			stored: base64.StdEncoding.EncodeToString([]byte("short")),
			want:   "",
		},
		{
			name:   "合法 Base64 且足够长（旧格式密文）",
			stored: base64.StdEncoding.EncodeToString(make([]byte, 32)),
			want:   base64.StdEncoding.EncodeToString(make([]byte, 32)),
		},
		{
			name:   "带首尾空格的旧格式密文（裁剪后判定）",
			stored: "  " + base64.StdEncoding.EncodeToString(make([]byte, 32)) + "\n",
			want:   base64.StdEncoding.EncodeToString(make([]byte, 32)),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := legacyEncryptedBody(testCase.stored); got != testCase.want {
				t.Fatalf("legacyEncryptedBody(%q) = %q，期望 %q", testCase.stored, got, testCase.want)
			}
		})
	}
}

// TestUnprotectAcceptsLegacyUnprefixedCiphertext 旧格式（无前缀裸密文）必须仍能解开。
// 防的回归：这条路径断了，早期版本写下的账号数据会在升级后静默消失并被替换成
// 默认离线账号 Player_01（P3-6 实测事故的读侧兜底）。
// 这里不依赖 DPAPI/AES 具体分支：直接取 Protect 的产物去掉前缀再解密。
func TestUnprotectAcceptsLegacyUnprefixedCiphertext(t *testing.T) {
	if !IsAvailable() {
		t.Skip("当前平台没有可用的加密方案，旧格式兼容分支不可达")
	}

	const plaintext = `{"Type":"offline","OfflineName":"Steve","OfflineSkinId":"steve"}`
	stored := Protect(plaintext)
	if stored == "" {
		t.Fatal("平台报告加密可用时 Protect 不应返回空串")
	}
	if !strings.HasPrefix(stored, EncryptedPrefix) {
		t.Fatalf("新格式必须带前缀：%q", stored)
	}

	legacy := strings.TrimPrefix(stored, EncryptedPrefix)
	if got := Unprotect(legacy); got != plaintext {
		t.Fatalf("旧格式（无前缀）解密结果 = %q，期望 %q", got, plaintext)
	}
	if got := Unprotect("  " + legacy + "  "); got != plaintext {
		t.Fatalf("旧格式密文首尾空白应当被容忍，实际 %q", got)
	}
}

// TestProtectUnprotectRoundTripsJsonPayload 账号落盘用的明文 JSON 必须能原样往返。
// 防的回归：加密产物带上前缀后读取端仍按老逻辑剥前缀，导致 accounts.yaml
// 每次启动都解析失败并回落默认账号（数据丢失级）。
func TestProtectUnprotectRoundTripsJsonPayload(t *testing.T) {
	if !IsAvailable() {
		t.Skip("当前平台没有可用的加密方案，Protect 会按契约回落明文")
	}

	plaintext := `[{"Type":"microsoft","Username":"NyaPlayer","AccessToken":"mc-access"},` +
		`{"Type":"offline","OfflineName":"Steve","OfflineSkinId":"steve"}]`
	stored := Protect(plaintext)
	if stored == "" {
		t.Fatal("平台报告加密可用时 Protect 不应返回空串")
	}
	if got := Unprotect(stored); got != plaintext {
		t.Fatalf("往返结果 = %q，期望 %q", got, plaintext)
	}
}

// TestUnprotectRejectsMalformedBase64 前缀之后不是合法 Base64 时必须返回空串。
// 防的回归：Base64 解析错误被忽略，用截断后的字节去解密，把「损坏」当成
// 「解出来是空」——用户重新登录后仍写回坏数据。
func TestUnprotectRejectsMalformedBase64(t *testing.T) {
	cases := []string{
		EncryptedPrefix + "!!!not-base64!!!",
		EncryptedPrefix + "AAAA", // 合法 Base64 但远短于 nonce/DPAPI 最小长度
		EncryptedPrefix + base64.StdEncoding.EncodeToString([]byte("short")),
	}
	for _, stored := range cases {
		if got := Unprotect(stored); got != "" {
			t.Fatalf("Unprotect(%q) = %q，期望空串", stored, got)
		}
	}
}

// TestAdditionalEntropyLength 应用专属附加熵必须保持 16 字节（与 C# 端一致）。
// 防的回归：改动附加熵内容/长度会让历史上所有已加密的账号数据全部解不开，
// 用户被强制重新登录。
func TestAdditionalEntropyLength(t *testing.T) {
	if len(additionalEntropy) != 16 {
		t.Fatalf("附加熵长度 = %d，期望 16（与 C# AccountSecretProtector 一致）",
			len(additionalEntropy))
	}
}

// TestAesProtectUnprotectRoundTrip 非 Windows 平台的密钥文件 + AES-256-GCM 往返。
// 防的回归：aesProtect/aesUnprotect 的 nonce 与密文拼接顺序、AAD 绑定被改坏，
// 表现为 Linux/macOS 上账号一存就解不开（Windows 开发机测不到）。
func TestAesProtectUnprotectRoundTrip(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())

	cases := []string{
		"ya29.a0AfH6SMBxxxxxxxx",
		"刷新令牌-含中文与符号 !@#$%^&*()",
		"a",
		strings.Repeat("long-token-", 500),
	}
	for _, plaintext := range cases {
		stored := aesProtect(plaintext)
		if stored == "" {
			t.Fatalf("aesProtect(%q) 返回空串（密钥文件应当能在临时目录创建）", plaintext)
		}
		if !strings.HasPrefix(stored, EncryptedPrefix) {
			t.Fatalf("密文缺少前缀：%q", stored)
		}
		// 单字符明文必然出现在 Base64 密文里，只对足够长的明文断言不泄露
		if len(plaintext) >= 8 && strings.Contains(stored, plaintext) {
			t.Fatalf("密文里出现了明文：%q", stored)
		}
		if got := aesUnprotect(stored[len(EncryptedPrefix):]); got != plaintext {
			t.Fatalf("解密结果 = %q，期望 %q", got, plaintext)
		}
	}

	// 同一明文两次加密必须不同（随机 nonce）：否则密文可被比对识别出「同一令牌」
	first := aesProtect("same-token")
	second := aesProtect("same-token")
	if first == second {
		t.Fatal("两次加密结果相同，说明 nonce 没有随机化")
	}
	if aesUnprotect(first[len(EncryptedPrefix):]) != "same-token" ||
		aesUnprotect(second[len(EncryptedPrefix):]) != "same-token" {
		t.Fatal("随机 nonce 的两个密文都应当能解回原文")
	}
}

// TestAesUnprotectRejectsTamperedCiphertext 被篡改或被截断的密文必须被拒绝
// （GCM 认证失败即返回空串），而不是解出垃圾数据。
// 防的回归：丢掉 GCM 认证错误检查，损坏的密文被当成有效令牌写回账号。
func TestAesUnprotectRejectsTamperedCiphertext(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())

	stored := aesProtect("refresh-token-to-protect")
	if stored == "" {
		t.Fatal("aesProtect 返回空串，无法继续验证篡改场景")
	}
	raw, err := base64.StdEncoding.DecodeString(stored[len(EncryptedPrefix):])
	if err != nil {
		t.Fatalf("密文不是合法 Base64：%v", err)
	}
	if len(raw) <= 12 {
		t.Fatalf("密文长度 = %d，期望包含 nonce + 密文 + tag", len(raw))
	}

	// 翻转密文正文的一个字节
	tampered := make([]byte, len(raw))
	copy(tampered, raw)
	tampered[len(tampered)-1] ^= 0xff
	if got := aesUnprotect(base64.StdEncoding.EncodeToString(tampered)); got != "" {
		t.Fatalf("篡改密文被解开为 %q，期望空串", got)
	}

	// 只保留 nonce（截断掉密文与认证标签）
	truncated := base64.StdEncoding.EncodeToString(raw[:12])
	if got := aesUnprotect(truncated); got != "" {
		t.Fatalf("截断密文被解开为 %q，期望空串", got)
	}

	// 比 nonce 还短
	if got := aesUnprotect(base64.StdEncoding.EncodeToString(raw[:4])); got != "" {
		t.Fatalf("不足 nonce 长度的密文被解开为 %q，期望空串", got)
	}

	// 正常密文仍然可用（确认前面的拒绝不是「一律返回空串」）
	if got := aesUnprotect(stored[len(EncryptedPrefix):]); got != "refresh-token-to-protect" {
		t.Fatalf("正常密文解密结果 = %q", got)
	}
}

// TestLoadOrCreateAesKeyPersistsAndRebuildsCorruptFile 密钥文件的生命周期：
// 首次生成 32 字节密钥并持久化、复读一致、内容损坏时备份并重建。
// 防的回归：密钥文件损坏后一直返回错误，账号每次保存都静默回落明文
// （刷新令牌明文躺在 accounts.yaml 里），以及反复重读导致每次启动密钥都变。
func TestLoadOrCreateAesKeyPersistsAndRebuildsCorruptFile(t *testing.T) {
	// loadOrCreateAesKey 读的是 config.DefaultStorageDirectory()，
	// 它在调用时取 USERPROFILE，所以这里把 USERPROFILE 指到临时目录。
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	keyPath := filepath.Join(home, "NekoLauncher", "account.secret.key")

	first, err := loadOrCreateAesKey()
	if err != nil {
		t.Fatalf("首次生成密钥失败：%v", err)
	}
	if len(first) != 32 {
		t.Fatalf("密钥长度 = %d，期望 32（AES-256）", len(first))
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("密钥文件应当落在 %s：%v", keyPath, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(decoded) != 32 {
		t.Fatalf("密钥文件内容不是 32 字节的 Base64：%v / %d 字节", err, len(decoded))
	}
	if string(decoded) != string(first) {
		t.Fatal("密钥文件内容与返回值不一致")
	}

	second, err := loadOrCreateAesKey()
	if err != nil {
		t.Fatalf("复读密钥失败：%v", err)
	}
	if string(second) != string(first) {
		t.Fatal("同一安装内重复读取必须得到同一密钥（否则旧密文全部解不开）")
	}

	// 写入损坏内容（合法 Base64 但长度不对 + 完全非法内容两种都要能重建）
	for _, corrupt := range []string{
		base64.StdEncoding.EncodeToString([]byte("only-16-bytes!!!")),
		"这不是-base64-内容-@@@",
	} {
		if err := os.WriteFile(keyPath, []byte(corrupt), 0o600); err != nil {
			t.Fatal(err)
		}
		rebuilt, err := loadOrCreateAesKey()
		if err != nil {
			t.Fatalf("密钥文件损坏时应当重建而不是报错：%v", err)
		}
		if len(rebuilt) != 32 {
			t.Fatalf("重建后的密钥长度 = %d，期望 32", len(rebuilt))
		}
		if string(rebuilt) == string(first) {
			t.Fatal("重建应当生成新密钥（旧密钥已无法解开任何数据）")
		}
		backups, err := filepath.Glob(keyPath + ".corrupt-*")
		if err != nil {
			t.Fatal(err)
		}
		if len(backups) == 0 {
			t.Fatalf("损坏的密钥文件必须留一份备份，便于用户找回：%v", backups)
		}
		// 重建后的文件必须是新密钥
		restored, err := loadOrCreateAesKey()
		if err != nil || string(restored) != string(rebuilt) {
			t.Fatalf("重建后应当稳定读到新密钥：%v", err)
		}
		// 每轮清理备份，保证下一轮断言的是本轮产生的备份
		for _, backup := range backups {
			if err := os.Remove(backup); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestProtectUnprotectContract 平台加密入口的对外契约：平台可用时
// Protect/Unprotect 必须成对可用且产物带前缀；平台不可用时 Protect 必须返回空串，
// 让调用方回落明文（宁可明文也不能丢账号）。
// 防的回归：加密失败时返回了非空但不可解的串，账号数据直接写坏。
func TestProtectUnprotectContract(t *testing.T) {
	if !IsAvailable() {
		if got := Protect("token"); got != "" {
			t.Fatalf("平台不支持加密时 Protect 应返回空串，实际 %q", got)
		}
		t.Skip("当前平台没有可用的加密方案，已按契约断言回落明文")
	}

	const plaintext = "refresh-token-秘密-1234567890"
	stored := Protect(plaintext)
	if stored == "" {
		t.Fatal("平台报告加密可用时 Protect 不应返回空串")
	}
	if !strings.HasPrefix(stored, EncryptedPrefix) {
		t.Fatalf("加密产物缺少 %q 前缀：%q", EncryptedPrefix, stored)
	}
	if strings.Contains(stored, plaintext) {
		t.Fatalf("加密产物里出现了明文：%q", stored)
	}
	if got := Unprotect(stored); got != plaintext {
		t.Fatalf("Unprotect(Protect(x)) = %q，期望 %q", got, plaintext)
	}

	// 第二次加密同一明文不应得到相同产物（DPAPI 与 AES-GCM 都应随机化）
	if second := Protect(plaintext); second == stored {
		t.Fatal("两次加密结果相同，说明实现没有随机化")
	}
}
