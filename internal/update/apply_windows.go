//go:build windows

package update

// Windows 就地替换：
//
//	1. 新 exe 先完整写到 <exe>.update-new（此时本体未动）；
//	2. 把正在运行的 exe 改名为 <exe>.update-old（**改名允许**，覆盖正在运行的文件不允许）；
//	3. 把 .update-new 改名到原路径（同卷 rename，原子）；
//	4. 启动新 exe，旧 exe 留到下次启动时由 CleanupOldExecutable 删除。
//
// 与旧方案（直接 O_TRUNC 写最终路径）的区别：第 2、3 步之间进程被杀死
// （断电/崩溃/任务管理器）的窗口里，完好的旧 exe 仍在 .update-old 上，
// 下次启动 CleanupOldExecutable 会发现本体缺失并优先恢复它，而不是把旧版删掉。
// 写入中途被杀死也只会留下半截的 .update-new，本体不受影响。

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
	pending := current + updatePendingSuffix

	// 第 1 步：先在旁边写好完整的新 exe（失败时本体毫发无损）
	if err := copyExecutable(newExecutable, pending); err != nil {
		_ = os.Remove(pending)
		return false, fmt.Errorf("写入新版本失败：%w", err)
	}

	// 上一次的残留先清掉，避免改名撞车
	_ = os.Remove(old)
	// 第 2 步：本体让位（改名允许；覆盖正在运行的文件不允许）
	if err := os.Rename(current, old); err != nil {
		_ = os.Remove(pending)
		return false, fmt.Errorf("无法把当前程序改名（可能被杀毒软件占用）：%w", err)
	}

	restore := func() {
		_ = os.Remove(current)
		_ = os.Rename(old, current)
		_ = os.Remove(pending)
	}

	// 第 3 步：新 exe 就位（同卷 rename，不存在半截文件状态）
	if err := os.Rename(pending, current); err != nil {
		restore()
		return false, fmt.Errorf("新版本就位失败：%w", err)
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
