package update

// 自替换：把下载好的新可执行文件换到当前 exe 的位置并重启。
//
// 平台差异用构建标签分开（apply_windows.go / apply_other.go）：
// Windows 可以"改名正在运行的 exe"，于是能做到就地替换；其它平台的启动器
// 是 AppImage / .app 包，替换方式各不相同，交给用户手动处理更安全。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// oldExecutableSuffix 被替换下来的旧 exe 后缀；下次启动时清理。
const oldExecutableSuffix = ".update-old"

// ErrManualUpdateRequired 当前平台不支持自动替换。
var ErrManualUpdateRequired = errors.New("当前平台不支持自动更新，请到版本页手动下载")

// CurrentExecutable 当前启动器可执行文件的绝对路径。
func CurrentExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}

	return filepath.Abs(path)
}

// validateReplacement 检查待替换文件是否像一个可执行文件：存在、是普通文件、不是空文件。
// 真正的完整性由下载阶段的 SHA/大小把关，这里只挡住"明显不对的文件"，
// 避免把一个空文件或目录换到 exe 位置上（换上去就再也启动不了）。
func validateReplacement(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("待替换路径是目录")
	}
	if info.Size() < minimumExecutableBytes {
		return errors.New("待替换文件过小，可能不是完整的可执行文件")
	}

	return nil
}

// minimumExecutableBytes 可执行文件大小下限（Wails 应用实测 5 MB+；这里只做粗筛）。
const minimumExecutableBytes = 512 << 10

// CleanupOldExecutable 清理上次更新留下的旧 exe。启动时调用即可，失败不影响运行。
func CleanupOldExecutable() {
	current, err := CurrentExecutable()
	if err != nil {
		return
	}
	old := current + oldExecutableSuffix
	if _, err := os.Stat(old); err != nil {
		return
	}
	_ = os.Remove(old)
}

// oldExecutablePath 旧 exe 的落点（不含后缀时补上）。
func oldExecutablePath(current string) string {
	if strings.HasSuffix(current, oldExecutableSuffix) {
		return current + oldExecutableSuffix
	}

	return current + oldExecutableSuffix
}
