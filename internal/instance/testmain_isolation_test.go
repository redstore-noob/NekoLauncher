package instance

// 测试隔离：把 HOME/USERPROFILE 指到一次性临时目录。
//
// 为什么需要：本包的测试会经 internal/logs 写日志（init 或错误路径都会写），
// 而日志目录是从 HOME/USERPROFILE 推出来的——不隔离就会往用户真实的
// %USERPROFILE%/NekoLauncher/Logs 里塞测试产生的垃圾日志，还会顺带读到真实
// launcher.yaml / accounts.yaml。放在 TestMain 里是因为共享日志文件路径在
// 首次写入时就固定了，逐个用例 t.Setenv 已经来不及。

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "nekolauncher-instance-test-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("USERPROFILE", home)
	_ = os.Setenv("HOME", home)

	code := m.Run()

	_ = os.RemoveAll(home)
	os.Exit(code)
}
