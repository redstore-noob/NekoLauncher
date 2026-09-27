package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestStorage 建立隔离的临时存储目录。
func newTestStorage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	storageDirectory = dir
	sharedStore = nil
	resetDomainStores()
	t.Cleanup(func() {
		sharedStore = nil
		resetDomainStores()
	})
	return dir
}

// TestRoutingSplitsAccountsFromLauncherConfig 验证账户键写入 accounts.yaml，
// 其余键（含外观偏好与游戏设置）一律写入 launcher.yaml。
func TestRoutingSplitsAccountsFromLauncherConfig(t *testing.T) {
	dir := newTestStorage(t)

	// 账户域
	if !SetValue("accounts", "encrypted-blob") {
		t.Fatal("写入 accounts 失败")
	}
	// 启动器外观/行为
	if !SetValue("closeAction", "ask") {
		t.Fatal("写入 closeAction 失败")
	}
	if !SetValue("homeWidgetColumns", "2") {
		t.Fatal("写入 homeWidgetColumns 失败")
	}
	// 游戏侧设置同样进 launcher.yaml
	if !SetValue("minecraftPath", `E:\mc`) {
		t.Fatal("写入 minecraftPath 失败")
	}
	if !SetValue("downloadActiveSource", "BMCL") {
		t.Fatal("写入 downloadActiveSource 失败")
	}

	for _, name := range []string{"accounts.yaml", "launcher.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("期望存在 %s: %v", name, err)
		}
	}
	// 旧的 JSON 配置不应再被创建
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		t.Fatal("不应再创建 config.json")
	}

	if got := GetValue("accounts"); got != "encrypted-blob" {
		t.Fatalf("accounts 读取结果 = %q", got)
	}
	if got := GetValue("closeAction"); got != "ask" {
		t.Fatalf("closeAction 读取结果 = %q", got)
	}
	if got := GetValue("minecraftPath"); got != `E:\mc` {
		t.Fatalf("minecraftPath 读取结果 = %q", got)
	}
	if got := GetValue("downloadActiveSource"); got != "BMCL" {
		t.Fatalf("downloadActiveSource 读取结果 = %q", got)
	}

	// accounts 不应出现在 launcher.yaml 里
	launcherBody, err := os.ReadFile(filepath.Join(dir, "launcher.yaml"))
	if err != nil {
		t.Fatalf("读取 launcher.yaml 失败: %v", err)
	}
	if containsKeyLine(string(launcherBody), "accounts") {
		t.Fatalf("accounts 不应写入 launcher.yaml，实际内容:\n%s", launcherBody)
	}
}

// TestStringTypesSurviveYAMLRoundTrip 验证数字字符串与 C# 风格布尔字面量
// 在 YAML 往返后仍是字符串（否则 Atoi / parseBool 会失败）。
func TestStringTypesSurviveYAMLRoundTrip(t *testing.T) {
	newTestStorage(t)

	cases := map[string]string{
		"musicVolume":             "50",
		"launcherAcrylicEnabled":  "True",
		"verifyFilesBeforeLaunch": "False",
		"launcherWindowWidth":     "1280",
	}
	for k, v := range cases {
		if !SetValue(k, v) {
			t.Fatalf("写入 %s 失败", k)
		}
	}

	// 重新加载（清空内存态，强制从磁盘 YAML 读回）
	sharedStore = nil
	resetDomainStores()

	for k, want := range cases {
		if got := GetValue(k); got != want {
			t.Fatalf("%s 往返后 = %q，期望 %q", k, got, want)
		}
	}
	// 数值语义仍然可用
	if got := GetValue("musicVolume"); got != "50" {
		t.Fatalf("musicVolume = %q", got)
	}
}

// TestJavaPathsSurviveYAMLRoundTrip 验证 javaPath 数组（对象数组）在 YAML 往返后
// 仍是 JavaPathGet 能解析的 []any / map[string]any 结构。
func TestJavaPathsSurviveYAMLRoundTrip(t *testing.T) {
	newTestStorage(t)

	if !AddJava(`C:\Java\bin\java.exe`, "21") {
		t.Fatal("AddJava 失败")
	}
	if !AddJava(`C:\Java8\bin\java.exe`, "8") {
		t.Fatal("AddJava 失败")
	}

	sharedStore = nil
	resetDomainStores()

	items := GetJavaPaths()
	if len(items) != 2 {
		t.Fatalf("Java 路径数量 = %d，期望 2：%+v", len(items), items)
	}
	if items[0].JavaPath != `C:\Java\bin\java.exe` || items[0].JavaVersion != "21" {
		t.Fatalf("首条 Java 路径 = %+v", items[0])
	}
	if items[1].JavaVersion != "8" {
		t.Fatalf("第二条 Java 版本 = %q", items[1].JavaVersion)
	}
}

// TestClearValueRemovesFromStore 验证 ClearValue 能删除键。
func TestClearValueRemovesFromStore(t *testing.T) {
	newTestStorage(t)

	SetValue("homeWidgetColumns", "2")
	if got := GetValue("homeWidgetColumns"); got != "2" {
		t.Fatalf("homeWidgetColumns = %q", got)
	}
	if !ClearValue("homeWidgetColumns") {
		t.Fatal("ClearValue 应返回 true")
	}
	if got := GetValue("homeWidgetColumns"); got != "" {
		t.Fatalf("删除后 homeWidgetColumns = %q", got)
	}
}

// TestTrickyPathValuesSurviveYAMLRoundTrip 覆盖 YAML 里有特殊含义的路径字符：
// `#`（注释起始）、引号、反斜杠、Unicode。配置里存了大量文件系统路径，
// 一旦被 YAML 误解析就会静默丢失设置。
func TestTrickyPathValuesSurviveYAMLRoundTrip(t *testing.T) {
	newTestStorage(t)

	cases := map[string]string{
		"pathHash":    `C:\games\my #1 pack\minecraft`,
		"pathQuote":   `C:\games\it's "quoted"\mc`,
		"pathUnicode": `D:\我的世界\整合包\mc`,
		"pathDash":    `C:\leading dash`,
		"pathPercent": `%APPDATA%\.minecraft`,
		"pathAmp":     `C:\a&b`,
		"pathColon":   `C:\a: b`,
	}
	for k, v := range cases {
		if !SetValue(k, v) {
			t.Fatalf("写入 %s 失败", k)
		}
	}

	sharedStore = nil
	resetDomainStores()

	for k, want := range cases {
		if got := GetValue(k); got != want {
			t.Errorf("%s 往返后 = %q，期望 %q", k, got, want)
		}
	}
}

// containsKeyLine 判断 YAML 文本中是否存在顶层 `key:` 行。
func containsKeyLine(body, key string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, key+":") {
			return true
		}
	}
	return false
}
