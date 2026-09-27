//go:build !windows

package bindings

// applyAcrylicRuntime 非 Windows 平台暂无运行时切换窗口背景模糊的能力，
// 恒返回 false 让调用方走"写配置 + 重启进程"的旧路径。
func applyAcrylicRuntime(enabled bool) bool { return false }

// fixupAcrylicBackdrop 非 Windows 平台无 DWM，无需启动修正。
func fixupAcrylicBackdrop() {}
