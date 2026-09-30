package bindings

import (
	"strings"

	"nekolauncher/internal/config"
)

// builtinCurseForgeAPIKey 编译期注入的内置 CurseForge API Key。
//
// 源码里永远只有空串占位（不提交真实 Key）：发布构建时由 CI 用
//
//	-ldflags "-X nekolauncher/internal/bindings.builtinCurseForgeAPIKey=$CF_API_KEY"
//
// 从环境变量 / Actions Secret 注入。二进制里的字符串可被提取，这只挡住
// "源码里看不到"，不构成保密；内置 Key 是全体用户共享的配额，要有被限流的预期。
var builtinCurseForgeAPIKey string

// effectiveCurseForgeAPIKey 返回生效的 CurseForge API Key：
// 用户在设置页自己配置的优先（也是限流风险的隔离层），未配置时回落到编译期内置值。
func effectiveCurseForgeAPIKey() string {
	if v := strings.TrimSpace(config.GetValue(curseForgeAPIKeyConfigKey)); v != "" {
		return v
	}
	return strings.TrimSpace(builtinCurseForgeAPIKey)
}
