package tools

// portable.go 便携模式（X-6）的公共判定。
//
// 放在 tools 而不是 config：判定结果有两个消费者——config（存储目录）与
// logs（日志目录），而 config **依赖** logs（configfilemanager 里写日志），
// 所以 logs 不能反过来 import config（循环）。tools 是两者共同的下层，
// 判定逻辑放在这里只写一份，避免"存储目录跟着 U 盘走、日志却还写用户目录"这种割裂。

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// PortableFlagName 便携模式标记文件名（放在 exe 同级即可开启）。
	PortableFlagName = "portable.flag"
	// PortableDataDirectoryName 便携模式数据目录名（exe 同级）。
	PortableDataDirectoryName = "NekoLauncher-data"
)

// PortableDataDirectory 便携模式下的数据目录；非便携模式返回 ok=false。
//
// 判定只看"exe 同级有没有 portable.flag 文件或 NekoLauncher-data 目录"，
// 不创建任何东西（读取路径的调用方很多，创建目录会有副作用）。
// 判定失败（拿不到 exe 路径）一律当非便携，绝不影响正常启动。
func PortableDataDirectory() (string, bool) {
	executable, err := os.Executable()
	if err != nil {
		return "", false
	}
	directory, err := filepath.Abs(filepath.Dir(executable))
	if err != nil {
		return "", false
	}

	return PortableDataDirectoryIn(directory)
}

// PortableDataDirectoryIn 判断指定目录（通常是 exe 所在目录）是否处于便携模式。
// 抽出来是为了能对任意临时目录做测试。
func PortableDataDirectoryIn(baseDirectory string) (string, bool) {
	if strings.TrimSpace(baseDirectory) == "" {
		return "", false
	}
	dataDirectory := filepath.Join(baseDirectory, PortableDataDirectoryName)
	if info, err := os.Stat(filepath.Join(baseDirectory, PortableFlagName)); err == nil && !info.IsDir() {
		return dataDirectory, true
	}
	if info, err := os.Stat(dataDirectory); err == nil && info.IsDir() {
		return dataDirectory, true
	}

	return "", false
}
