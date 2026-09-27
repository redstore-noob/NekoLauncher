package bindings

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"nekolauncher/internal/config"
)

// TestAutoDetectJavaMergesAndDeduplicates Java 自动检索：找到的条目只新增一次，
// 再点一次全部进 Skipped，且不动用户已配置的条目。
//
// 这条用例防的是"点一次加一次、列表里堆满重复项"，以及"自动检索把用户手填的路径挤掉"。
func TestAutoDetectJavaMergesAndDeduplicates(t *testing.T) {
	// 造一个可被检索到的"Java"：把假可执行文件放进 JAVA_HOME/bin，
	// 检索逻辑会枚举 JAVA_HOME（不依赖本机真实安装，也不会碰用户的 Java）
	javaHome := t.TempDir()
	binDirectory := filepath.Join(javaHome, "bin")
	if err := os.MkdirAll(binDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(binDirectory, javaExecutableName())
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JAVA_HOME", javaHome)

	// 用户已配置的条目：自动检索必须保留它（哪怕它指向一个不存在的位置）
	configured := filepath.Join(t.TempDir(), "user-java")
	if !config.AddJava(configured, "自定义") {
		t.Fatal("准备用户条目失败")
	}

	api := &ConfigAPI{}
	first := api.AutoDetectJava()

	if first.Found == nil || first.Added == nil || first.Skipped == nil || first.Versions == nil {
		t.Fatalf("结果里的切片不能为 nil（前端会直接 .map）：%+v", first)
	}
	found := false
	for _, path := range first.Found {
		if strings.EqualFold(path, mustAbs(t, executable)) {
			found = true
		}
	}
	if !found {
		t.Skipf("本机检索不到 JAVA_HOME 下的 java（PATH/平台差异），跳过：%+v", first.Found)
	}

	// 第二次调用：所有找到的都应在 Skipped 里，Added 为空
	second := api.AutoDetectJava()
	if len(second.Added) != 0 {
		t.Fatalf("重复检索不该再新增：%+v", second.Added)
	}
	if len(second.Skipped) == 0 {
		t.Fatalf("重复检索应把已有条目算作跳过：%+v", second)
	}

	// 用户自己配置的条目必须还在列表里
	kept := false
	for _, item := range config.GetJavaPaths() {
		if strings.EqualFold(item.JavaPath, configured) {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("自动检索不该丢掉用户已配置的 Java：%+v", config.GetJavaPaths())
	}
}

// mustAbs 解析绝对路径（测试里只要求路径可比）。
func mustAbs(t *testing.T, path string) string {
	t.Helper()

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}

	return abs
}

// javaExecutableName 平台上的 Java 可执行文件名（Windows 带 .exe）。
func javaExecutableName() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}

	return "java"
}
