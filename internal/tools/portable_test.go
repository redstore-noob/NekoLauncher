package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPortableStorageDirectoryDetection 便携模式判定：
// 有 portable.flag（文件）或 NekoLauncher-data（目录）才算，其它一律回落用户目录。
func TestPortableStorageDirectoryDetection(t *testing.T) {
	base := t.TempDir()

	// 什么都没有：非便携
	if directory, ok := PortableDataDirectoryIn(base); ok || directory != "" {
		t.Fatalf("空目录不该判为便携模式：%q / %v", directory, ok)
	}

	// 同名的**文件**（而不是目录）不算数据目录
	if err := os.WriteFile(filepath.Join(base, PortableDataDirectoryName), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if directory, ok := PortableDataDirectoryIn(base); ok {
		t.Fatalf("同名文件不该判为便携数据目录：%q", directory)
	}
	_ = os.Remove(filepath.Join(base, PortableDataDirectoryName))

	// 标记文件 → 便携
	if err := os.WriteFile(filepath.Join(base, PortableFlagName), []byte("portable"), 0o644); err != nil {
		t.Fatal(err)
	}
	directory, ok := PortableDataDirectoryIn(base)
	if !ok || directory != filepath.Join(base, PortableDataDirectoryName) {
		t.Fatalf("有 portable.flag 时应进入便携模式：%q / %v", directory, ok)
	}

	// 去掉标记、只留数据目录 → 依然便携（绿色版解压即用，不需要额外放标记）
	if err := os.Remove(filepath.Join(base, PortableFlagName)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, PortableDataDirectoryName), 0o755); err != nil {
		t.Fatal(err)
	}
	directory, ok = PortableDataDirectoryIn(base)
	if !ok || directory != filepath.Join(base, PortableDataDirectoryName) {
		t.Fatalf("有数据目录时应进入便携模式：%q / %v", directory, ok)
	}

	// 空路径不 panic、不算便携
	if directory, ok := PortableDataDirectoryIn(""); ok || directory != "" {
		t.Fatalf("空路径不该判为便携模式：%q / %v", directory, ok)
	}
}
