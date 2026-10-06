//go:build !windows && !linux

// 兜底实现：既没有 Windows 原生托盘（tray_windows.go）也没有 Linux 的
// StatusNotifierItem 实现（tray_linux.go）的平台（macOS 及未知系统）。
//
// macOS 没有跟进实现是刻意的：energye/systray 的 darwin 分支是 cgo +
// Cocoa（#cgo LDFLAGS: -framework Cocoa），既要求 .app 主线程时序，又会让
// CI 的 `GOOS=darwin go build ./...` 交叉检查在 CGO_ENABLED=0 下失去符号。
// 在拿到真机验证之前不引入这条可能让 macOS 端卡死/构建失败的路径。
//
// 关键约束：这些平台必须让 TraySupported() 返回 false——否则"关闭按钮行为 =
// 最小化到托盘"会把窗口藏起来且没有任何入口能显示回来（托盘菜单不存在），
// 应用变成只能杀进程的幽灵。见 close_behavior.go 的守卫与前端对托盘选项的门控。
package bindings

import "context"

// TraySupported 无托盘平台的实现：始终不可用。
func TraySupported() bool { return false }

// StartTray 无托盘平台空实现。
func StartTray(context.Context) {}

// StopTray 无托盘平台空实现。
func StopTray() {}
