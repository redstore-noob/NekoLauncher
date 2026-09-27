//go:build windows

package launch

// 游戏进程优先级（Windows）：进程启动后用 SetPriorityClass 调整。
// 高于普通的档位（abovenormal/high）一般无需特权；失败仅记录日志。

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// applyProcessPriority 按配置调整进程优先级；priority 为空或 "normal" 时不调整。
func applyProcessPriority(process *os.Process, priority string, log func(string)) {
	class, ok := windowsPriorityClass(priority)
	if !ok {
		return
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION, false, uint32(process.Pid))
	if err != nil {
		logLog(log, fmt.Sprintf("设置进程优先级失败：%v。", err))
		return
	}
	defer windows.CloseHandle(handle)
	if err := windows.SetPriorityClass(handle, class); err != nil {
		logLog(log, fmt.Sprintf("设置进程优先级失败：%v。", err))
		return
	}
	logLog(log, fmt.Sprintf("已设置游戏进程优先级：%s。", priority))
}

// windowsPriorityClass 优先级取值 → Windows 优先级类别；无需调整时 ok=false。
func windowsPriorityClass(priority string) (uint32, bool) {
	switch priority {
	case "low":
		return windows.IDLE_PRIORITY_CLASS, true
	case "belownormal":
		return windows.BELOW_NORMAL_PRIORITY_CLASS, true
	case "abovenormal":
		return windows.ABOVE_NORMAL_PRIORITY_CLASS, true
	case "high":
		return windows.HIGH_PRIORITY_CLASS, true
	default:
		return 0, false
	}
}
