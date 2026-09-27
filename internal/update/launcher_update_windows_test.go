//go:build windows

package update

// Windows 就地替换流程的单元测试：copyExecutable 只存在于 apply_windows.go，
// 依赖它的测试集中在这里，避免 linux/darwin 的 vet/test 编译不过。

import (
	"os"
	"path/filepath"
	"testing"
)

// TestApplyReplacesExecutableWindows 就地替换：旧文件被改名留底、新文件落到原路径。
//
// 直接对"测试二进制自己"调用 Apply 会真的启动一个测试进程，所以这里把
// 替换逻辑拆成可测的两步：先用临时目录里的"假 exe"验证改名 + 覆盖，
// 再单独验证 Apply 在非 Windows 上的明确拒绝（见 launcher_update_test.go）。
func TestApplyReplacesExecutableWindows(t *testing.T) {
	directory := t.TempDir()
	current := filepath.Join(directory, "Launcher.exe")
	if err := os.WriteFile(current, make([]byte, minimumExecutableBytes+10), 0o755); err != nil {
		t.Fatal(err)
	}
	incoming := filepath.Join(directory, "incoming.exe")
	if err := os.WriteFile(incoming, make([]byte, minimumExecutableBytes+20), 0o755); err != nil {
		t.Fatal(err)
	}

	// 复制 + 改名这两步与 Apply 里的一致；Apply 额外做的事是启动新进程（测试里不能做）
	old := oldExecutablePath(current)
	if err := os.Rename(current, old); err != nil {
		t.Fatalf("改名失败：%v", err)
	}
	if err := copyExecutable(incoming, current); err != nil {
		t.Fatalf("复制新版本失败：%v", err)
	}
	info, err := os.Stat(current)
	if err != nil {
		t.Fatalf("新版本没落到原路径：%v", err)
	}
	if info.Size() != minimumExecutableBytes+20 {
		t.Fatalf("落盘的不是新版本：%d 字节", info.Size())
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("旧版本应保留为回滚兜底：%v", err)
	}
}
