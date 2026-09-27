// Package tools 全项目共享的路径、HTTP 客户端、JSON 辅助方法。
package tools

import "runtime"

// OSVersionDescription 返回用于 Minecraft `rules[].os.version` 正则匹配的系统版本描述。
//
// 对应 C# 的 RuntimeInformation.OSDescription，Go 标准库没有等价物：
//   - Windows：RtlGetVersion 取到的 "10.0.26200"（见 osversion_windows.go）。
//     **不能**带 "Microsoft Windows " 前缀——Mojang 的规则是 `^10\.` 这类锚定正则。
//   - macOS：`sw_vers -productVersion` 形如 "14.5"；取不到时退回 runtime.Version()。
//   - 其它：runtime.Version()（规则几乎不会命中，与旧行为一致）。
//
// 下载侧与启动侧必须用同一个实现：两边判定不一致会出现"装得上、启不来"
// （1.5.2/1.6.4 在 macOS 上的 lwjgl os.version 规则就是这种情形）。
func OSVersionDescription() string {
	if runtime.GOOS == "windows" {
		if description := windowsOSVersion(); description != "" {
			return description
		}
	}
	if runtime.GOOS == "darwin" {
		if description := darwinOSVersion(); description != "" {
			return description
		}
	}

	return runtime.Version()
}
