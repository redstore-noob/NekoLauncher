// rule_evaluator.go 版本 JSON 中 libraries[].rules 的过滤逻辑。
//
// 本包需要独立评估规则（安装与文件校验时决定某个库是否适用于当前系统），
// 而 internal/launch 也有一套等价实现（启动参数装配用）。两套规则语义保持一致：
// 都按 Mojang 契约处理 action / os.name / os.arch / features。
package download

import (
	"regexp"
	"runtime"
	"strings"

	"nekolauncher/internal/tools"
)

// ruleJSON 版本 JSON 的 rules 数组条目。
type ruleJSON struct {
	Action   string          `json:"action"`
	OS       *ruleOSJSON     `json:"os"`
	Features map[string]bool `json:"features"`
}

type ruleOSJSON struct {
	Name    string `json:"name"`
	Arch    string `json:"arch"`
	Version string `json:"version"`
}

// DefaultFeatures 默认特性集（has_custom_resolution 由调用方按窗口参数覆盖）。
func DefaultFeatures() map[string]bool {
	return map[string]bool{"has_custom_resolution": true}
}

// RuleEvaluatorOSName 当前系统名（Mojang 约定：windows / osx / linux）。
func RuleEvaluatorOSName() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "osx"
	default:
		return "linux"
	}
}

// ruleIsAllowed 判断库条目的 rules 是否允许当前系统（与启动器使用同一套规则）。
// 无 rules 时允许。
func ruleIsAllowed(rules []ruleJSON, features map[string]bool) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, rule := range rules {
		if rule.Features != nil {
			match := true
			for k, v := range rule.Features {
				if features[k] != v {
					match = false
					break
				}
			}
			if !match {
				continue
			}
		}
		if rule.OS != nil {
			if rule.OS.Name != "" && rule.OS.Name != RuleEvaluatorOSName() {
				continue
			}
			if rule.OS.Arch != "" {
				// 与启动侧 matchesArchitecture 相同的架构映射：
				// amd64 是 64 位（x86_64），绝不能映射成 32 位的 x86，
				// 否则 32 位专属库的 arch 规则会在 64 位系统上被误放行。
				want := strings.ToLower(strings.TrimSpace(rule.OS.Arch))
				var have string
				switch runtime.GOARCH {
				case "386":
					have = "x86"
				case "amd64":
					have = "x86_64"
				case "arm":
					have = "arm"
				case "arm64":
					have = "aarch64"
				default:
					have = runtime.GOARCH
				}
				if want != have && !(want == "x64" && have == "x86_64") &&
					!(want == "arm64" && have == "aarch64") {
					continue
				}
			}
			// os.version 是正则，必须与启动侧用同一份系统描述来判定。
			// 早先这里没有 Version 字段，等于"版本规则一律视作命中"：macOS 上
			// 1.5.2/1.6.4 的 lwjgl disallow 规则会生效 → 安装阶段跳过那几个 jar，
			// 而启动侧（当时也判定不了）要求它们必须存在 → 装得上、启不来。
			if rule.OS.Version != "" {
				expression, err := regexp.Compile(rule.OS.Version)
				if err != nil || !expression.MatchString(tools.OSVersionDescription()) {
					continue
				}
			}
		}
		if rule.Action == "allow" {
			allowed = true
		} else if rule.Action == "disallow" {
			allowed = false
		}
	}
	return allowed
}
