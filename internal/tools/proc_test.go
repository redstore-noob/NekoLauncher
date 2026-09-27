package tools

import (
	"os/exec"
	"runtime"
	"syscall"
	"testing"
)

// TestHideProcessWindowIsSafeOnEveryPlatform 防的回归：
// 非 Windows 平台误用 Windows 专用字段导致编译失败（因此实现拆成了平台文件），
// 以及 Windows 上忘记录 SysProcAttr 导致黑框一闪。
//
// 这条测试在所有平台都要能编译并跑过：非 Windows 只验证"空操作不 panic"。
func TestHideProcessWindowIsSafeOnEveryPlatform(t *testing.T) {
	// 命令只构造不执行：真正的拉起在 proc_windows_test.go 里用 cmd.exe 验证
	command := exec.Command("nyalauncher-not-a-real-binary")

	HideProcessWindow(command)
	if runtime.GOOS == "windows" {
		if command.SysProcAttr == nil {
			t.Fatal("Windows 上 HideProcessWindow 必须写入 SysProcAttr，否则控制台窗口仍会弹出")
		}
	} else if command.SysProcAttr != nil {
		t.Fatal("非 Windows 平台应为空操作，不应创建 SysProcAttr")
	}

	// 幂等：重复调用不 panic，也不应把已设置的字段清掉
	HideProcessWindow(command)
	if runtime.GOOS == "windows" && command.SysProcAttr == nil {
		t.Fatal("重复调用后 SysProcAttr 被清空")
	}

	// 手动预置过 SysProcAttr 的命令同样不能 panic
	preset := exec.Command("nyalauncher-not-a-real-binary")
	preset.SysProcAttr = &syscall.SysProcAttr{}
	HideProcessWindow(preset)
	if preset.SysProcAttr == nil {
		t.Fatal("预置的 SysProcAttr 被置空")
	}
}
