package launch

import (
	"strings"
	"testing"
)

// RedactSecrets 是内存日志/日志文件/launch:logLine 事件的统一脱敏出口，
// 必须覆盖令牌与账号标识参数（与溯源报告 sensitiveArgumentPrefixes 同口径）。
func TestRedactSecretsMasksTokensAndIdentity(t *testing.T) {
	cases := []struct{ name, line string }{
		{"accessToken 分列", "java ... --accessToken eyJhbGciOiJIUzI1NiJ9.SECRET --version 1.21"},
		{"accessToken 等号", "--accessToken=eyJhbGci.SECRET.SIGNATURE"},
		{"username", "--username Steve --version 1.21"},
		{"uuid", "--uuid 069a79f444e94726a5befca90e38aaf5"},
		{"xuid 等号", "--xuid=2535446toplayer"},
		{"clientId", "--clientId 8f4a2d3bSECRET"},
		{"旧会话串", "token:abc123def456ghi789:069a79f444e94726a5befca90e38aaf5"},
		{"Bearer 头", "Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456"},
		{"JSON accessToken", `{"accessToken":"eyJhbGciOiJIUzI1NiJ9.SECRET","selectProfile":true}`},
		{"access_token 参数", "https://sessionserver.mojang.com/join?access_token=eyJSECRET&serverId=x"},
		{"session 参数", "joinserver session=legacysecret123&user=Steve"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			masked := RedactSecrets(testCase.line)
			if strings.Contains(masked, "SECRET") {
				t.Fatalf("秘密泄漏：%q -> %q", testCase.line, masked)
			}
			if strings.Contains(masked, "Steve") ||
				strings.Contains(masked, "069a79f4") ||
				strings.Contains(masked, "2535446") {
				t.Fatalf("账号标识泄漏：%q -> %q", testCase.line, masked)
			}
		})
	}
	// 键名必须保留，否则日志失去解释力
	if got := RedactSecrets("--username Steve"); !strings.Contains(got, "--username") {
		t.Fatalf("键名被破坏：%q", got)
	}
}
