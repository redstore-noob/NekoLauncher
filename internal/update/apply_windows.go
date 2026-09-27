//go:build windows

package update

// Windows 就地替换：
//
//	1. 把正在运行的 exe 改名为 <exe>.update-old（**改名允许**，覆盖正在运行的文件不允许）；
//	2. 新 exe 复制到原路径，启动它；
//	3. 旧 exe 留到下次启动时由 CleanupOldExecutable 删除（本次进程还在用它）。
//
// 任一步失败都会把旧 exe 改回原路径，保证"更新失败也能照常启动"。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"nekolauncher/internal/tools"
)

// Apply 用 newExecutable 替换当前启动器本体，并说明是否已拉起新进程。
//
// 返回 started=true 时调用方应立刻退出自身（新进程已经起来接替）。
func Apply(newExecutable string) (bool, error) {
	if err := validateReplacement(newExecutable); err != nil {
		return false, fmt.Errorf("更新文件不可用：%w", err)
	}

	current, err := CurrentExecutable()
	if err != nil {
		return false, err
	}
	old := oldExecutablePath(current)

	// 上一次的残留先清掉，避免改名撞车
	_ = os.Remove(old)
	if err := os.Rename(current, old); err != nil {
		return false, fmt.Errorf("无法把当前程序改名（可能被杀毒软件占用）：%w", err)
	}

	restore := func() {
		_ = os.Remove(current)
		_ = os.Rename(old, current)
	}

	if err := copyExecutable(newExecutable, current); err != nil {
		restore()

		return false, fmt.Errorf("写入新版本失败：%w", err)
	}

	command := exec.Command(current)
	command.Dir = filepath.Dir(current)
	tools.HideProcessWindow(command)
	if err := command.Start(); err != nil {
		restore()

		return false, fmt.Errorf("启动新版本失败：%w", err)
	}

	return true, nil
}

// copyExecutable 复制文件并保留可执行权限位。
func copyExecutable(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()

	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := output.ReadFrom(input); err != nil {
		_ = output.Close()

		return err
	}

	return output.Close()
}
