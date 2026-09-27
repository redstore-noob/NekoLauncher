package config

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"nekolauncher/internal/logs"
)

// logsWriteError 写一条 ERROR 日志（失败不向上传播，与 C# Console.WriteLine 语义对齐）。
func logsWriteError(info string) {
	logs.Write("ERROR", info)
}

// pathsEqualFold 路径比较器语义：Windows 忽略大小写，其他系统区分大小写。
// 仅用于已规范化的路径字符串比较（对应 C# 的 PathComparer / StringComparer）。
func pathsEqualFold(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

// parseBool 解析配置项中的布尔值（对应 C# bool.TryParse：失败返回错误）。
func parseBool(value string) (bool, error) {
	result, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, err
	}
	return result, nil
}

// formatBool 布尔值序列化为配置字符串（对应 C# bool.ToString()）。
func formatBool(value bool) string {
	if value {
		return "True"
	}
	return "False"
}

// parseInt 解析配置项中的整数值（对应 C# int.TryParse）。
func parseInt(value string) (int, error) {
	result, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	return result, nil
}

// itoa 整数序列化为配置字符串（对应 C# int.ToString()）。
func itoa(value int) string { return fmt.Sprintf("%d", value) }

// trimTrailingSeparator 去掉路径尾部的目录分隔符（对应
// Path.TrimEndingDirectorySeparator；根目录如 "C:\" 保持原样）。
func trimTrailingSeparator(path string) string {
	if len(path) < 2 {
		return path
	}
	for len(path) > 0 && (path[len(path)-1] == '/' || path[len(path)-1] == '\\') {
		// 保留根目录本身的分隔符（如 "C:\" 或 "/"）
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = path[:len(path)-1]
	}
	return path
}
