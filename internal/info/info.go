// Package info 启动器版本信息（前端版本显示的唯一来源，发版只改这里）。
package info

import "fmt"

// 版本号各段，发版只改这里的常量。
// 注意拼接顺序是 主.次.修：要得到 "0.3.2" 要改 FixVersion，改 SubVersion 会变成 "0.4.0"。
const (
	MainVersion = 0
	SubVersion  = 4
	FixVersion  = 1
	Suffix      = ""

	// BuildKind 控制版本用途：
	//   - official：正式版，允许检查更新；
	//   - official-preview：官方预览版，允许检查更新；
	//   - internal：个人开发/测试版，禁用启动器自更新。
	// 内部开发时只需把这里改成 internal，同时可用 versionOverride 标记具体构建。
	BuildKind = "internal"
)

// versionOverride 重建分支的临时版本标识：非空时 Version() 直接返回它，
// 便于整体区分 Rebuild 构建；回到正式版本方案时把它改回空串即可。
const versionOverride = ""

// buildKindOverride 由 CI 发布构建注入，保证发布包仍能检查更新。
var buildKindOverride string

// Version 纯版本字符串。versionOverride 非空时优先返回，
// 否则由上方字段拼接，如 "1.0.0-preview4" / "0.2.0"。
func Version() string {
	if versionOverride != "" {
		return versionOverride
	}
	if Suffix != "" {
		return fmt.Sprintf("%d.%d.%d-%s", MainVersion, SubVersion, FixVersion, Suffix)
	}
	return fmt.Sprintf("%d.%d.%d", MainVersion, SubVersion, FixVersion)
}

// UpdatesEnabled 只有正式版和官方预览版允许检查启动器更新。
func UpdatesEnabled() bool {
	kind := BuildKind
	if buildKindOverride != "" {
		kind = buildKindOverride
	}
	return kind == "official" || kind == "official-preview"
}

// UpdateDisabledReason 返回不允许更新时给用户看的原因。
func UpdateDisabledReason() string {
	return "此版本已禁用更新"
}

// FormatVersionString 格式化版本号，如 "NekoLauncher版本号:1.0.0-preview4"。
func FormatVersionString() string {
	return "NekoLauncher版本号:" + Version()
}
