package launch

import (
	"encoding/json"
	"regexp"
	"runtime"
	"strings"

	"nekolauncher/internal/tools"
)

// MinecraftRuleEvaluator 版本 JSON 库/参数条目的 rules 评估器
// （对应 C# Internal/MinecraftRuleEvaluator）。
var MinecraftRuleEvaluator minecraftRuleEvaluatorNamespace

type minecraftRuleEvaluatorNamespace struct{}

// ruleJSON 版本 JSON rules 数组条目。
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

// CreateDefaultFeatures 创建标准 Minecraft 特性字典（用于库规则评估）。
// 调用方可按需覆盖 hasCustomResolution。
func (minecraftRuleEvaluatorNamespace) CreateDefaultFeatures(hasCustomResolution bool) map[string]bool {
	return map[string]bool{
		"has_custom_resolution":      hasCustomResolution,
		"is_demo_user":               false,
		"has_quick_plays_support":    false,
		"is_quick_play_singleplayer": false,
		"is_quick_play_multiplayer":  false,
		"is_quick_play_realms":       false,
	}
}

// IsAllowed 判断版本 JSON 条目（library / argument）的 rules 是否允许当前环境。
func (minecraftRuleEvaluatorNamespace) IsAllowed(item json.RawMessage, features map[string]bool) bool {
	var wrapper struct {
		Rules []ruleJSON `json:"rules"`
	}
	if err := json.Unmarshal(item, &wrapper); err != nil {
		// 无法解析的条目按无规则处理（保持与 C# TryGetProperty 失败路径一致）
		return true
	}
	if len(wrapper.Rules) == 0 {
		return true
	}

	allowed := false
	for _, rule := range wrapper.Rules {
		if !ruleMatches(rule, features) {
			continue
		}
		allowed = rule.Action == "allow"
	}
	return allowed
}

func ruleMatches(rule ruleJSON, features map[string]bool) bool {
	if rule.OS != nil && !matchesOperatingSystem(rule.OS) {
		return false
	}

	if len(rule.Features) == 0 {
		return true
	}

	for name, required := range rule.Features {
		actual := features[name]
		if actual != required {
			return false
		}
	}
	return true
}

func matchesOperatingSystem(os *ruleOSJSON) bool {
	if os.Name != "" && !strings.EqualFold(os.Name, operatingSystemName()) {
		return false
	}

	if os.Arch != "" && !matchesArchitecture(os.Arch) {
		return false
	}

	if os.Version != "" {
		expression, err := regexp.Compile(os.Version)
		if err != nil {
			return false
		}
		return expression.MatchString(operatingSystemVersionDescription())
	}

	return true
}

// OperatingSystemName Mojang 约定的系统名：windows / osx / linux。
func (minecraftRuleEvaluatorNamespace) OperatingSystemName() string {
	return operatingSystemName()
}

func operatingSystemName() string {
	switch goos {
	case "windows":
		return "windows"
	case "darwin":
		return "osx"
	default:
		return "linux"
	}
}

func matchesArchitecture(expected string) bool {
	if strings.TrimSpace(expected) == "" {
		return true
	}

	var actual string
	switch runtime.GOARCH {
	case "386":
		actual = "x86"
	case "amd64":
		actual = "x86_64"
	case "arm":
		actual = "arm"
	case "arm64":
		actual = "aarch64"
	default:
		actual = runtime.GOARCH
	}

	return strings.EqualFold(expected, actual) ||
		(expected == "x64" && actual == "x86_64") ||
		(expected == "arm64" && actual == "aarch64")
}

// operatingSystemVersionDescription 当前系统描述，供 os.version 规则的正则匹配。
//
// 实现放在 tools 包：下载侧（装哪些库）与启动侧（缺哪些库算致命）必须用同一份
// 判定，否则会出现"装得上、启不来"（macOS 上 1.5.2/1.6.4 的 lwjgl os.version
// 规则就是这么炸的）。
func operatingSystemVersionDescription() string {
	return tools.OSVersionDescription()
}
