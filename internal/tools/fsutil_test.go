package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestFile 在临时目录里落一个文件，测试绝不触碰真实用户数据。
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建临时目录失败：%v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写临时文件失败：%v", err)
	}
}

// TestFileExists 防的回归：把"目录"或"不存在的路径"当成文件。
// 早前各包各写一份 fileExists，其中若干份漏了 IsDir 判断，
// 于是目录被当成"已下载好的文件"直接跳过下载。
func TestFileExists(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.jar")
	writeTestFile(t, file, "jar")
	dir := filepath.Join(root, "versions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}

	if !FileExists(file) {
		t.Fatalf("存在的普通文件应判定为存在：%s", file)
	}
	if FileExists(dir) {
		t.Fatalf("目录不应被判定为文件：%s", dir)
	}
	if FileExists(filepath.Join(root, "missing.jar")) {
		t.Fatal("不存在的文件应判定为不存在")
	}
	if FileExists("") {
		t.Fatal("空路径应判定为不存在")
	}
}

// TestDirectoryExists 防的回归：把文件当成目录（后续 os.ReadDir / MkdirAll 会失败），
// 以及目录存在时误判为不存在导致重复创建骨架。
func TestDirectoryExists(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.jar")
	writeTestFile(t, file, "jar")

	if !DirectoryExists(root) {
		t.Fatalf("存在的目录应判定为存在：%s", root)
	}
	if DirectoryExists(file) {
		t.Fatalf("文件不应被判定为目录：%s", file)
	}
	if DirectoryExists(filepath.Join(root, "missing")) {
		t.Fatal("不存在的目录应判定为不存在")
	}
	if DirectoryExists("") {
		t.Fatal("空路径应判定为不存在")
	}
}

// TestRemoveFileIfExists 防的回归：用 os.Remove 直接删路径，把空目录误删；
// 以及路径不存在时把"删除失败"当成错误上报。
func TestRemoveFileIfExists(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "old.jar")
	writeTestFile(t, file, "old")

	RemoveFileIfExists(file)
	if FileExists(file) {
		t.Fatalf("文件应被删除：%s", file)
	}

	// 不存在的路径必须是空操作（不 panic、不报错）
	RemoveFileIfExists(filepath.Join(root, "already-gone.jar"))

	// 目录绝不能被删掉：isDir 判断缺失时 os.Remove 会删掉空目录
	dir := filepath.Join(root, "saves")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}
	RemoveFileIfExists(dir)
	if !DirectoryExists(dir) {
		t.Fatal("RemoveFileIfExists 只应删除普通文件，不能删除目录")
	}
}

// TestBoolPtr 防的回归：BoolPtr 返回同一个变量的地址导致配置项互相串值
// （三态布尔字段 *bool 需要各自独立的存储）。
func TestBoolPtr(t *testing.T) {
	enabled := BoolPtr(true)
	disabled := BoolPtr(false)
	if enabled == nil || !*enabled {
		t.Fatalf("BoolPtr(true) = %v，期望指向 true", enabled)
	}
	if disabled == nil || *disabled {
		t.Fatalf("BoolPtr(false) = %v，期望指向 false", disabled)
	}

	*enabled = false
	if *disabled {
		t.Fatal("两个指针应各自独立，修改一个不应影响另一个")
	}
}

// TestFileHelpersTreatPathsAsAbsoluteOrRelative 防的回归：把相对路径当成"不存在"，
// 使以工作目录为基准的调用方（CLI 参数、整合包脚本）全部失效。
func TestFileHelpersTreatPathsAsAbsoluteOrRelative(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "relative.txt"), "x")

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录失败：%v", err)
	}
	// 切到临时目录工作，构造一个真实存在的相对路径
	if err := os.Chdir(root); err != nil {
		t.Fatalf("切目录失败：%v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(workingDirectory) })

	if !FileExists("relative.txt") {
		t.Fatal("相对路径指向的普通文件应判定为存在")
	}
	if FileExists(filepath.Join("..", strings.Repeat("no", 3))) {
		t.Fatal("相对路径指向的不存在文件应判定为不存在")
	}
}
