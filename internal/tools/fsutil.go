package tools

// fsutil.go 全项目共享的文件系统小工具。
//
// 此前 fileExists 在 5 个包各写一遍（其中 4 份逐字节相同）、directoryExists 3 份，
// 属于纯复制粘贴。收敛到本文件后，各包应直接调用 tools.*，不要再自行实现。
//
// 命名说明：FileExists 判定"存在且是普通文件"，DirectoryExists 判定"存在且是目录"。

import (
	"io"
	"os"
	"path/filepath"
)

// FileExists path 存在且为普通文件时返回 true。
// 目录、符号链接指向的目录（Stat 解引用后）都返回 false。
func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// DirectoryExists path 存在且为目录时返回 true。
func DirectoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// CopyFileContents 按字节复制文件内容，目标存在时覆盖；保留源文件权限位。
// 调用方负责确保目标目录存在。
func CopyFileContents(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// RemoveFileIfExists 删除普通文件；目标是目录或不存在时什么都不做。
// 与直接 os.Remove 的区别：路径不存在不算错误，目录不会被误删。
func RemoveFileIfExists(path string) {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		_ = os.Remove(path)
	}
}

// BoolPtr 返回 value 的地址（配置结构体的可选布尔字段用）。
func BoolPtr(value bool) *bool { return &value }
