package modrinth

// 测试隔离：把 HOME/USERPROFILE 指到一次性临时目录。
//
// 为什么需要：回退到镜像成功时本包会经 internal/logs 记一条 INFO 日志，
// 而日志目录是从 HOME/USERPROFILE 推出来的——不隔离就会往用户真实的
// %USERPROFILE%/NekoLauncher/Logs 里塞测试产生的垃圾日志。放在 TestMain 里
// 是因为共享日志文件路径在首次写入时就固定了，逐个用例 t.Setenv 已经来不及。

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "nekolauncher-modrinth-test-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("USERPROFILE", home)
	_ = os.Setenv("HOME", home)

	code := m.Run()

	_ = os.RemoveAll(home)
	os.Exit(code)
}
