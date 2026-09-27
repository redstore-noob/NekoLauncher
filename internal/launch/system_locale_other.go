//go:build !windows

package launch

import (
	"os"
	"strings"
)

// readSystemLocale 非 Windows 平台从 LANG 环境变量取区域（如 zh_CN.UTF-8），
// 规整成 BCP-47 风格（zh-CN）；拿不到返回空串。
func readSystemLocale() string {
	raw := strings.TrimSpace(os.Getenv("LANG"))
	if raw == "" {
		return ""
	}
	// 去掉编码与修饰符：zh_CN.UTF-8@... → zh_CN
	if idx := strings.IndexAny(raw, ".@"); idx >= 0 {
		raw = raw[:idx]
	}
	parts := strings.SplitN(raw, "_", 2)
	if parts[0] == "" {
		return ""
	}
	locale := strings.ToLower(parts[0])
	if len(parts) == 2 && parts[1] != "" {
		locale += "-" + strings.ToUpper(parts[1])
	}

	return locale
}
