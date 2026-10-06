package bindings

import (
	"strings"
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

// effectiveCurseForgeAPIKey 返回生效的 CurseForge API Key，唯一来源是编译期
// 内置值。曾经支持过"用户在设置里自行配置"（launcher.yaml 的 curseforgeApiKey），
// 已移除——见 api_download_resources.go 的说明；历史配置里的旧值在 Startup
// 清理（wiring.go），不再参与生效判定。
func effectiveCurseForgeAPIKey() string {
	return strings.TrimSpace(builtinCurseForgeAPIKey)
}
