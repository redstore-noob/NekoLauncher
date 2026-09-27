package logs

import (
	"os"
	"path/filepath"
	"testing"

	"nekolauncher/internal/tools"
)

// TestLogDirectoryFollowsPortableMode 便携模式下日志要跟着数据目录走。
//
// 防的是"绿色版"的割裂：存储目录已经跟着 U 盘/程序目录，日志却还写进用户目录
// ——既让用户找不到日志，也可能在受限环境里直接写不进去（等于没有日志）。
func TestLogDirectoryFollowsPortableMode(t *testing.T) {
	// 把"程序目录"伪造成当前测试二进制所在目录：exe 同级放便携标记
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("取不到当前可执行文件：%v", err)
	}
	executableDirectory := filepath.Dir(executable)

	// 没有便携标记时应落在用户目录下（沿用原行为）
	if portable, ok := tools.PortableDataDirectory(); ok {
		t.Skipf("当前运行目录已经是便携模式（%s），跳过非便携分支断言", portable)
	}
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	if got := logDirectory(); got != filepath.Join(home, "NekoLauncher", "Logs") {
		t.Fatalf("非便携模式日志目录 = %q，期望落在用户目录下", got)
	}

	// 造出便携数据目录（用目录形态，不写标记文件，避免污染测试二进制所在目录）
	portableDirectory := filepath.Join(executableDirectory, tools.PortableDataDirectoryName)
	if err := os.MkdirAll(portableDirectory, 0o755); err != nil {
		t.Skipf("无法在程序目录创建便携数据目录（%v），跳过便携分支断言", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(portableDirectory) })

	if got := logDirectory(); got != filepath.Join(portableDirectory, "Logs") {
		t.Fatalf("便携模式日志目录 = %q，期望 %q", got, filepath.Join(portableDirectory, "Logs"))
	}
}
