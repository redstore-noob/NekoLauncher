// Package logs 启动器共享日志：单文件追加 + 超限轮转，全部实例写同一文件。
package logs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/tools"
)

var (
	writeMu sync.Mutex
	// sharedFilePath 进程内共享的日志文件路径：首个写入者以当前时刻定死文件名，
	// 之后所有写入者追加到同一文件，避免日志分散到多个文件里找不到。
	sharedFilePath string
)

// maximumLogFileSizeBytes 单文件大小上限；超过后轮转为 .old.log（只保留一代）。
const maximumLogFileSizeBytes = 8 * 1024 * 1024

func timeGet() string {
	return time.Now().Format("2006-01-02_15-04-05")
}

// logDirectory 日志目录：用户目录下的 NekoLauncher/Logs；便携模式下跟着数据目录走
// （否则"绿色版"的日志仍写进用户目录，既割裂又可能在受限环境里写不进去）；
// 用户目录不可用时回落到程序目录。
func logDirectory() string {
	if portable, ok := tools.PortableDataDirectory(); ok {
		return filepath.Join(portable, "Logs")
	}
	home := os.Getenv("USERPROFILE")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	if home == "" {
		exe, err := os.Executable()
		if err == nil {
			home = filepath.Dir(exe)
		} else {
			home = "."
		}
	}
	return filepath.Join(home, "NekoLauncher", "Logs")
}

// ensureSharedFilePath 取得（并按需创建）本次运行的共享日志文件路径，需持锁调用。
func ensureSharedFilePath() (string, error) {
	if sharedFilePath != "" {
		return sharedFilePath, nil
	}
	dir := logDirectory()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	sharedFilePath = filepath.Join(dir, timeGet()+".log")
	return sharedFilePath, nil
}

// Init 初始化日志系统（对应 C# 版构造函数写一条 INIT 日志）。
func Init() {
	if !Write("INFO", "日志系统初始化成功") {
		fmt.Println("日志初始化系统失败")
	}
}

// AddLogs 创建一条新的日志。当写入失败或 type == "ERROR" 时调用 onError（可为 nil）。
func AddLogs(info string, onError func(), typ string) bool {
	result := Write(typ, info)
	if !result || typ == "ERROR" {
		if onError != nil {
			onError()
		}
	}
	return result
}

// Write 静态写入入口：无需持有实例即可写共享日志文件。
// 文件写入持共享锁；控制台输出在锁外（慢速终端不应阻塞其他日志写入者）。
func Write(typ, info string) bool {
	line := fmt.Sprintf("[%s][%s]%s", timeGet(), typ, info)
	writeMu.Lock()
	path, err := ensureSharedFilePath()
	var f *os.File
	if err == nil {
		rotateIfTooLarge(path)
		f, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	}
	if err == nil {
		_, err = f.WriteString(line + "\n")
		f.Close()
	}
	writeMu.Unlock()
	if err != nil {
		fmt.Println(err)
		return false
	}
	fmt.Println(line)
	return true
}

// rotateIfTooLarge 当前日志文件超过大小上限时轮转：归档为 .old.log（覆盖上一代）。
// 轮转失败只放弃本次轮转，不中断写入。需持锁调用。
func rotateIfTooLarge(path string) {
	stat, err := os.Stat(path)
	if err != nil || stat.Size() < maximumLogFileSizeBytes {
		return
	}
	archived := filepath.Join(
		filepath.Dir(path),
		filepath.Base(path)[:len(filepath.Base(path))-len(filepath.Ext(path))]+".old.log")
	_ = os.Remove(archived)
	_ = os.Rename(path, archived)
}

// ReadCurrent 读取本次运行共享日志文件的全部内容；尚未产生日志时返回空串。
func ReadCurrent() (string, error) {
	writeMu.Lock()
	path := sharedFilePath
	writeMu.Unlock()
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// ExportCurrent 把本次运行的日志导出到 dest；尚未产生日志时写出空文件。
func ExportCurrent(dest string) error {
	if strings.TrimSpace(dest) == "" {
		return errors.New("导出路径不能为空")
	}
	content, err := ReadCurrent()
	if err != nil {
		return err
	}
	return os.WriteFile(dest, []byte(content), 0o644)
}

// ClearLogs 清空日志目录：删除其中的全部 .log 文件（含 .old.log 归档）。
// 本此运行的共享日志文件一并删除，并重置共享路径：后续写入会按当前时刻
// 新建文件，而不是继续向已被删除的旧路径追加。
// 返回成功删除的文件数量；目录不存在返回 0，发生异常时返回 -1。
func ClearLogs() int {
	dir := logDirectory()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return 0
	}
	deleted := 0
	writeMu.Lock()
	files, err := filepath.Glob(filepath.Join(dir, "*.log"))
	sharedFilePath = ""
	writeMu.Unlock()
	if err != nil {
		return -1
	}
	for _, file := range files {
		if err := os.Remove(file); err == nil {
			deleted++
		}
	}
	return deleted

}
