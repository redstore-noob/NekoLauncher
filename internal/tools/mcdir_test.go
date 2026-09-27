package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// defaultMinecraftDirectoryFor 按各平台规则算出期望值，测试自身不写死某一种布局。
func defaultMinecraftDirectoryFor(home, appData string) string {
	switch runtime.GOOS {
	case "windows":
		if appData != "" {
			return filepath.Join(appData, ".minecraft")
		}
		return filepath.Join(home, "AppData", "Roaming", ".minecraft")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "minecraft")
	default:
		return filepath.Join(home, ".minecraft")
	}
}

// guardInsideTemp 落盘前的最后一道闸：目标必须位于临时目录内。
// 防的回归是"测试写进真实 .minecraft"——一旦环境变量覆盖失效，
// 这条断言会先失败，而不是去改用户的游戏目录。
func guardInsideTemp(t *testing.T, root, target string) {
	t.Helper()
	if target == "" {
		t.Fatal("默认 Minecraft 目录不应为空（主目录可用时）")
	}
	if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(root)) {
		t.Fatalf("默认目录 %q 不在临时目录 %q 内，拒绝继续以免污染真实 .minecraft", target, root)
	}
}

// TestDefaultMinecraftDirectoryFollowsPlatformLayout 防的回归：
// 各平台默认 .minecraft 路径推导错位（macOS 少了 Application Support、
// Windows 少了 APPDATA 层级），启动器找不到官方启动器已有的存档与实例。
func TestDefaultMinecraftDirectoryFollowsPlatformLayout(t *testing.T) {
	home := t.TempDir()
	appData := filepath.Join(home, "AppData", "Roaming")
	if err := os.MkdirAll(appData, 0o755); err != nil {
		t.Fatalf("建临时 APPDATA 失败：%v", err)
	}
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", appData)

	got := DefaultMinecraftDirectory()
	want := defaultMinecraftDirectoryFor(home, appData)
	if got != want {
		t.Fatalf("DefaultMinecraftDirectory() = %q，期望 %q（GOOS=%s）", got, want, runtime.GOOS)
	}
	guardInsideTemp(t, home, got)
}

// TestDefaultMinecraftDirectoryFallsBackWithoutAppData 防的回归：
// APPDATA 为空时直接返回空串或相对路径，使默认目录变成当前工作目录下的
// "AppData/Roaming/.minecraft"（游戏目录跑到安装目录里）。
func TestDefaultMinecraftDirectoryFallsBackWithoutAppData(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("APPDATA 回落分支只在 Windows 上存在")
	}
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", "")

	got := DefaultMinecraftDirectory()
	want := filepath.Join(home, "AppData", "Roaming", ".minecraft")
	if got != want {
		t.Fatalf("APPDATA 缺失时 = %q，期望 %q", got, want)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("APPDATA 缺失时也必须返回绝对路径，得到 %q", got)
	}
	guardInsideTemp(t, home, got)
}

// TestEnsureDefaultMinecraftDirectoryCreatesSkeleton 防的回归：
// 目录骨架缺项（历史实现只建了 versions/assets/libraries 三项，
// 导致 saves/resourcepacks 等目录在首次启动时才被游戏自己创建，
// 而启动器的"目录检查"却报告一切正常）。
func TestEnsureDefaultMinecraftDirectoryCreatesSkeleton(t *testing.T) {
	home := t.TempDir()
	appData := filepath.Join(home, "AppData", "Roaming")
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", appData)

	want := defaultMinecraftDirectoryFor(home, appData)
	guardInsideTemp(t, home, want)
	if DirectoryExists(want) {
		t.Fatalf("前置条件不成立：%q 不该已存在", want)
	}

	got := EnsureDefaultMinecraftDirectory()
	if got != want {
		t.Fatalf("EnsureDefaultMinecraftDirectory() = %q，期望 %q", got, want)
	}
	if !DirectoryExists(got) {
		t.Fatalf("根目录未被创建：%s", got)
	}
	if len(standardSubDirectories) == 0 {
		t.Fatal("标准子目录列表为空，骨架创建将退化为只建根目录")
	}
	for _, sub := range standardSubDirectories {
		if !DirectoryExists(filepath.Join(got, sub)) {
			t.Fatalf("标准子目录缺失：%s", sub)
		}
	}

	// 只建目录骨架，不落任何文件（历史实现里塞过一个占位文件，会被游戏当成损坏内容）
	entries, err := os.ReadDir(got)
	if err != nil {
		t.Fatalf("读目录失败：%v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			t.Fatalf("骨架里不应创建文件：%s", entry.Name())
		}
	}
	if len(entries) != len(standardSubDirectories) {
		t.Fatalf("骨架子目录数量 = %d，期望 %d", len(entries), len(standardSubDirectories))
	}

	// 幂等：重复调用不报错、不改变结果
	if again := EnsureDefaultMinecraftDirectory(); again != got {
		t.Fatalf("重复调用返回 %q，期望 %q", again, got)
	}
}

// TestEnsureDefaultMinecraftDirectoryKeepsExistingContent 防的回归：
// 目录已存在时被重建/清空，抹掉用户已有的 versions、saves 等真实数据。
func TestEnsureDefaultMinecraftDirectoryKeepsExistingContent(t *testing.T) {
	home := t.TempDir()
	appData := filepath.Join(home, "AppData", "Roaming")
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", appData)

	root := defaultMinecraftDirectoryFor(home, appData)
	guardInsideTemp(t, home, root)
	if err := os.MkdirAll(filepath.Join(root, "saves", "MyWorld"), 0o755); err != nil {
		t.Fatalf("准备已有存档失败：%v", err)
	}
	marker := filepath.Join(root, "saves", "MyWorld", "level.dat")
	if err := os.WriteFile(marker, []byte("world"), 0o644); err != nil {
		t.Fatalf("写标记文件失败：%v", err)
	}

	if got := EnsureDefaultMinecraftDirectory(); got != root {
		t.Fatalf("EnsureDefaultMinecraftDirectory() = %q，期望 %q", got, root)
	}
	if !FileExists(marker) {
		t.Fatal("已存在的存档内容被破坏：EnsureDefaultMinecraftDirectory 应是只读判断 + 按需补建")
	}
}
