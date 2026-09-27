//go:build windows

package tools

import (
	"os/exec"
	"syscall"
)

// createNoWindowFlag Windows CREATE_NO_WINDOW：为控制台子系统子进程禁止分配控制台，
// 避免后台探测/工具调用时闪现黑色 cmd 窗口。
const createNoWindowFlag = 0x08000000

// HideProcessWindow 隐藏子进程可能弹出的控制台窗口。
func HideProcessWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindowFlag
}
