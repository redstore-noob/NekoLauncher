//go:build !windows

package tools

import "os/exec"

// HideProcessWindow 非 Windows 平台为空操作：
// syscall.SysProcAttr 在这些平台上没有 HideWindow / CreationFlags 字段。
func HideProcessWindow(cmd *exec.Cmd) {}
