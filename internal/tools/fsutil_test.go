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

// TestCopyFileContentsCopiesAndOverwrites 防的回归：目标已存在时追加而不是覆盖，
// 导致新下载的文件尾部残留旧内容（jar 校验失败但看不出原因）。
func TestCopyFileContentsCopiesAndOverwrites(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.bin")
	// 先写长内容、再覆盖成短内容，专门验证 O_TRUNC 生效
	writeTestFile(t, source, "second")

	destination := filepath.Join(root, "nested", "deep", "target.bin")
	writeTestFile(t, destination, "a-much-longer-old-content")

	if err := CopyFileContents(source, destination); err != nil {
		t.Fatalf("复制失败：%v", err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("读取目标失败：%v", err)
	}
	if string(data) != "second" {
		t.Fatalf("目标内容 = %q，期望 %q（应覆盖而非追加）", data, "second")
	}

	// 目标目录不存在时也应自动补建（调用方常常直接把目标交给本函数）
	newDestination := filepath.Join(root, "brand-new", "x.bin")
	if err := CopyFileContents(source, newDestination); err != nil {
		t.Fatalf("目标目录不存在时应自动创建：%v", err)
	}
	if !FileExists(newDestination) {
		t.Fatalf("复制后目标文件不存在：%s", newDestination)
	}
}

// TestCopyFileContentsPreservesSourcePermissions 防的回归：复制出来的文件权限位丢失，
// 例如可执行文件（整合包安装器）复制后失去执行位。
func TestCopyFileContentsPreservesSourcePermissions(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "run.sh")
	writeTestFile(t, source, "#!/bin/sh\n")
	if err := os.Chmod(source, 0o600); err != nil {
		t.Fatalf("改权限失败：%v", err)
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		t.Fatalf("Stat 源文件失败：%v", err)
	}

	destination := filepath.Join(root, "copy.sh")
	if err := CopyFileContents(source, destination); err != nil {
		t.Fatalf("复制失败：%v", err)
	}
	destinationInfo, err := os.Stat(destination)
	if err != nil {
		t.Fatalf("Stat 目标文件失败：%v", err)
	}
	if destinationInfo.Mode().Perm() != sourceInfo.Mode().Perm() {
		t.Fatalf("目标权限 = %v，源权限 = %v，应保持一致",
			destinationInfo.Mode().Perm(), sourceInfo.Mode().Perm())
	}
}

// TestCopyFileContentsMissingSource 防的回归：源文件不存在时静默成功，
// 调用方以为文件已经就位（进而启动游戏）而实际什么都没复制。
func TestCopyFileContentsMissingSource(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "target.bin")
	if err := CopyFileContents(filepath.Join(root, "no-such-source.bin"), destination); err == nil {
		t.Fatal("源文件不存在时应返回错误")
	}
	if FileExists(destination) {
		t.Fatal("源文件不存在时不应创建目标文件")
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
