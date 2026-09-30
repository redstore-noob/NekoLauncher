// Package info 启动器版本信息（前端版本显示的唯一来源，发版只改这里）。
package info

import "fmt"

// 版本号各段，发版只改这里的常量。
const (
	MainVersion = 0
	SubVersion  = 3
	FixVersion  = 0
	Suffix      = ""
)

// versionOverride 重建分支的临时版本标识：非空时 Version() 直接返回它，
// 便于整体区分 Rebuild 构建；回到正式版本方案时把它改回空串即可。
const versionOverride = ""

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

// FormatVersionString 格式化版本号，如 "NekoLauncher版本号:1.0.0-preview4"。
func FormatVersionString() string {
	return "NekoLauncher版本号:" + Version()
}
