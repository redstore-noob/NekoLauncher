//go:build !windows

package launch

// 游戏进程优先级（非 Windows）：进程启动后用 nice 值调整。
// 负 nice 值（提高优先级）通常需要特权，失败仅记录日志，不影响游戏运行。

import (
	"fmt"
	"os"
	"syscall"
)

// applyProcessPriority 按配置调整进程 nice 值；priority 为空或 "normal" 时不调整。
func applyProcessPriority(process *os.Process, priority string, log func(string)) {
	nice, ok := unixNiceValue(priority)
	if !ok {
		return
	}
	if err := syscall.Setpriority(syscall.PRIO_PROCESS, process.Pid, nice); err != nil {
		logLog(log, fmt.Sprintf("设置进程优先级失败：%v（提高优先级可能需要系统权限）。", err))
		return
	}
	logLog(log, fmt.Sprintf("已设置游戏进程优先级：%s（nice %d）。", priority, nice))
}

// unixNiceValue 优先级取值 → nice 值；无需调整时 ok=false。
func unixNiceValue(priority string) (int, bool) {
	switch priority {
	case "low":
		return 15, true
	case "belownormal":
		return 10, true
	case "abovenormal":
		return -5, true
	case "high":
		return -10, true
	default:
		return 0, false
	}
}
