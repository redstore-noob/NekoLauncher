package bindings

// 插件启动贡献 → 启动变换的转换层。
//
// 这一层是 launch.MinecraftLaunchTransform 与"插件清单"之间的桥：
// launch 包不依赖插件系统（bindings 反过来依赖 launch），所以由 bindings
// 在启动前把已启用插件的声明解析成变换，经 launch.LaunchTransformProvider 注入。
//
// 安全取向（与 docs/guide/TRUST_MODEL.md一致）：
//   - 这里**只**实现"追加参数"这两种最无害的贡献。Transform 里那些
//     MainClassOverride / JavaExecutableOverride / ReplaceClasspath 能力
//     不从这里开放——插件能做到"把启动的进程换成任意程序"，而 YAML 里
//     一行字看不出这个后果，风险与收益不成比例。
//   - 未声明 capabilities["launch-transform"] 的插件即使写了
//     launchTransform 段也会被忽略（声明制契约，且不静默：调用方拿到警告列表）。
//   - 参数值做基本体检（非空、不含 \0），拒绝会破坏命令行的内容。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekolauncher/internal/launch"
)

// launchTransformCapability 插件声明"我在启动参数上挂钩"的能力键。
const launchTransformCapability = "launch-transform"

// pluginLaunchContribution 一个插件解析出的启动贡献（附带来源插件 id）。
type pluginLaunchContribution struct {
	PluginID         string
	AppendJvmArgs    []string
	AppendGameArgs   []string
	ManifestWarnings []string
}

// launchTransform 汇总所有已启用插件的启动贡献，产出本次启动的变换。
// 返回的警告面向用户（插件页可展示），不阻断启动。
func (a *PluginAPI) launchTransform() (*launch.MinecraftLaunchTransform, []string) {
	contributions, warnings := a.collectLaunchContributions()
	if len(contributions) == 0 {
		return &launch.MinecraftLaunchTransform{}, warnings
	}

	transform := &launch.MinecraftLaunchTransform{}
	for _, contribution := range contributions {
		transform.AppendJvmArguments = append(
			transform.AppendJvmArguments, contribution.AppendJvmArgs...)
		transform.AppendGameArguments = append(
			transform.AppendGameArguments, contribution.AppendGameArgs...)
	}
	return transform, warnings
}

// collectLaunchContributions 读取插件目录，解析出所有生效的启动贡献。
// 顺序按插件 id 排序，保证同样的插件集合产出同样顺序的参数（可复现）。
func (a *PluginAPI) collectLaunchContributions() ([]pluginLaunchContribution, []string) {
	directory := a.directory()
	entries, err := os.ReadDir(directory)
	if err != nil {
		// 插件目录读不到不是启动失败的理由：按"没有插件贡献"处理。
		return nil, nil
	}

	disabled := a.disabledStore().IDs()
	contributions := make([]pluginLaunchContribution, 0, len(entries))
	var warnings []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if disabled[id] {
			continue
		}
		manifest, err := readPluginManifest(filepath.Join(directory, id))
		if err != nil || manifest == nil || manifest.LaunchTransform == nil {
			continue
		}
		if !manifest.Capabilities[launchTransformCapability] {
			warnings = append(warnings, fmt.Sprintf(
				"插件 %s 声明了 launchTransform，但没有 capabilities.%s 权限，已忽略其启动贡献。",
				id, launchTransformCapability))
			continue
		}

		contribution := pluginLaunchContribution{PluginID: id}
		var jvmWarnings, gameWarnings []string
		contribution.AppendJvmArgs, jvmWarnings = sanitizeTransformArguments(
			id, "appendJvmArguments", manifest.LaunchTransform.AppendJvmArguments)
		contribution.AppendGameArgs, gameWarnings = sanitizeTransformArguments(
			id, "appendGameArguments", manifest.LaunchTransform.AppendGameArguments)
		contribution.ManifestWarnings = append(jvmWarnings, gameWarnings...)
		warnings = append(warnings, contribution.ManifestWarnings...)

		if len(contribution.AppendJvmArgs) == 0 && len(contribution.AppendGameArgs) == 0 {
			continue
		}
		contributions = append(contributions, contribution)
	}
	return contributions, warnings
}

// sanitizeTransformArguments 过滤非法的启动参数。
// 返回（合法参数, 警告）：非法项被丢弃并留下可展示的警告，绝不静默吞掉。
func sanitizeTransformArguments(
	pluginID, field string,
	arguments []string,
) ([]string, []string) {
	result := make([]string, 0, len(arguments))
	var warnings []string
	for index, argument := range arguments {
		trimmed := strings.TrimSpace(argument)
		switch {
		case trimmed == "":
			warnings = append(warnings, fmt.Sprintf(
				"插件 %s 的 %s 第 %d 项为空，已忽略。", pluginID, field, index+1))
		case strings.ContainsRune(trimmed, '\x00'):
			warnings = append(warnings, fmt.Sprintf(
				"插件 %s 的 %s 第 %d 项含非法字符，已忽略。", pluginID, field, index+1))
		default:
			result = append(result, trimmed)
		}
	}
	return result, warnings
}
