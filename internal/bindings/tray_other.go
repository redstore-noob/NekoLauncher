//go:build !windows

// 非 Windows 平台暂无托盘实现（本项目主要面向 Windows）。
package bindings

import "context"

// StartTray 非 Windows 平台暂无托盘，空实现保持接口一致。
func StartTray(context.Context) {}

// StopTray 非 Windows 平台空实现。
func StopTray() {}
