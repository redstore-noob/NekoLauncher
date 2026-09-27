//go:build windows

package tools

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

// detachedProcessFlag DETACHED_PROCESS：用于验证 HideProcessWindow 是"或上"
// 而不是"覆盖"已有的 CreationFlags。
const detachedProcessFlag = 0x00000008

// TestHideProcessWindowSetsNoWindowFlags 防的回归：
// HideWindow 或 CREATE_NO_WINDOW 漏设一个，后台探测（java -version、taskkill、
// 整合包安装器）就会在用户屏幕上闪出黑色控制台窗口。
func TestHideProcessWindowSetsNoWindowFlags(t *testing.T) {
	command := exec.Command("cmd.exe", "/c", "exit", "0")
	HideProcessWindow(command)

	if command.SysProcAttr == nil {
		t.Fatal("SysProcAttr 未被创建")
	}
	if !command.SysProcAttr.HideWindow {
		t.Fatal("HideWindow 未设置，STARTUPINFO 仍会显示窗口")
	}
	if command.SysProcAttr.CreationFlags&createNoWindowFlag == 0 {
		t.Fatalf("CreationFlags = %#x，缺少 CREATE_NO_WINDOW(%#x)",
			command.SysProcAttr.CreationFlags, createNoWindowFlag)
	}
	if createNoWindowFlag != 0x08000000 {
		t.Fatalf("CREATE_NO_WINDOW 常量 = %#x，Win32 定义为 0x08000000", createNoWindowFlag)
	}
}

// TestHideProcessWindowPreservesExistingCreationFlags 防的回归：
// 用 `=` 而不是 `|=` 写 CreationFlags，把调用方已经设好的标志
// （DETACHED_PROCESS、CREATE_UNICODE_ENVIRONMENT 等）抹掉，
// 表现为子进程行为诡异或环境变量丢失。
func TestHideProcessWindowPreservesExistingCreationFlags(t *testing.T) {
	command := exec.Command("cmd.exe", "/c", "exit", "0")
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcessFlag}

	HideProcessWindow(command)

	if command.SysProcAttr.CreationFlags&detachedProcessFlag == 0 {
		t.Fatalf("原有 CreationFlags(%#x) 被覆盖，实际 %#x",
			detachedProcessFlag, command.SysProcAttr.CreationFlags)
	}
	if command.SysProcAttr.CreationFlags&createNoWindowFlag == 0 {
		t.Fatalf("CREATE_NO_WINDOW 未或入，实际 %#x", command.SysProcAttr.CreationFlags)
	}

	// 二次调用必须幂等（重复或运算不应产生副作用）
	HideProcessWindow(command)
	if command.SysProcAttr.CreationFlags != createNoWindowFlag|detachedProcessFlag {
		t.Fatalf("重复调用后 CreationFlags = %#x，期望 %#x",
			command.SysProcAttr.CreationFlags, createNoWindowFlag|detachedProcessFlag)
	}
}

// TestHideProcessWindowKeepsStdoutCapture 防的回归：
// 隐藏窗口的实现顺手改了 stdio 或 StartProcess 参数，导致 `java -version`
// 这类探测拿不到输出（Java 版本判断失败 → 启动参数用错）。
func TestHideProcessWindowKeepsStdoutCapture(t *testing.T) {
	if _, err := exec.LookPath("cmd.exe"); err != nil {
		t.Skip("找不到 cmd.exe，跳过真实拉起验证")
	}

	command := exec.Command("cmd.exe", "/c", "echo", "nyalauncher-hide-window-ok")
	HideProcessWindow(command)

	output, err := command.Output()
	if err != nil {
		t.Fatalf("隐藏窗口后命令应照常执行：%v", err)
	}
	if !strings.Contains(string(output), "nyalauncher-hide-window-ok") {
		t.Fatalf("stdout 捕获被破坏，输出 = %q", output)
	}
}
