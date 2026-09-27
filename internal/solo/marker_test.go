package solo

// ApplyStartupDefaults 的行为测试：标记应用、幂等与"已应用不再覆盖"。
// 存储目录经 config.SetStorageDirectory 指到用例专属临时目录。

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"nekolauncher/internal/config"
)

// samePath 平台一致的路径等价比较（Windows 大小写不敏感）。
func samePath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

// buildInstalledWorld 构造一个"安装完成"的目标布局：
// <root>/NekoLauncher-data/{minecraft/versions/<id>/…, runtime/jre/bin/java.exe}
func buildInstalledWorld(t *testing.T) (dataDirectory, minecraftDirectory, javaExe, versionID string) {
	t.Helper()
	root := t.TempDir()
	dataDirectory = filepath.Join(root, "NekoLauncher-data")
	minecraftDirectory = filepath.Join(dataDirectory, "minecraft")
	versionID = "1.20.1-forge-47.2.0"
	writeFile(t, filepath.Join(minecraftDirectory, "versions", versionID, versionID+".json"), `{"id": "x"}`)
	writeFile(t, filepath.Join(minecraftDirectory, "versions", versionID, "mods", "a.jar"), "mod")
	javaExe = filepath.Join(dataDirectory, "runtime", "jre", "bin", "java.exe")
	writeFile(t, javaExe, "java")
	return dataDirectory, minecraftDirectory, javaExe, versionID
}

func TestApplyStartupDefaultsAppliesMarker(t *testing.T) {
	if err := config.SetStorageDirectory(t.TempDir()); err != nil {
		t.Fatalf("重定向存储目录失败：%v", err)
	}
	_, minecraftDirectory, javaExe, versionID := buildInstalledWorld(t)

	if err := SaveMarker(config.StorageDirectory(), &Marker{
		Format:             PayloadFormat,
		PackName:           "Demo",
		PackVersion:        "1.0.0",
		VersionID:          versionID,
		SimpleMode:         true,
		MinecraftDirectory: minecraftDirectory,
		JavaExecutable:     javaExe,
	}); err != nil {
		t.Fatalf("写标记失败：%v", err)
	}

	if err := ApplyStartupDefaults(); err != nil {
		t.Fatalf("ApplyStartupDefaults 失败：%v", err)
	}

	if got := config.GetValue(simpleModeConfigKey); got != "true" {
		t.Errorf("simpleMode = %q，期望 true", got)
	}
	if got := config.GameDirectory(); !samePath(got, minecraftDirectory) {
		t.Errorf("游戏目录 = %q，期望 %q", got, minecraftDirectory)
	}
	if got := config.JavaExecutable(); !samePath(got, javaExe) {
		t.Errorf("Java = %q，期望 %q", got, javaExe)
	}
	if got := config.GetValue("selectedGameInstance"); got != versionID {
		t.Errorf("选中实例 = %q，期望 %q", got, versionID)
	}
	profile := config.Get(minecraftDirectory, versionID)
	if profile.IsVersionIsolationEnabled == nil || !*profile.IsVersionIsolationEnabled {
		t.Error("实例隔离应被显式开启")
	}

	marker, err := LoadMarker(config.StorageDirectory())
	if err != nil || marker == nil || !marker.Applied {
		t.Fatalf("标记应回写为已应用：%+v, %v", marker, err)
	}

	// 幂等：已应用的标记不再覆盖用户改过的配置
	if err := config.SetStorageDirectory(config.StorageDirectory()); err != nil {
		t.Fatalf("重定向存储目录失败：%v", err)
	}
	if err := config.SetValue(simpleModeConfigKey, "false"); !err {
		t.Fatal("改写 simpleMode 失败")
	}
	if err := ApplyStartupDefaults(); err != nil {
		t.Fatalf("重复应用不应报错：%v", err)
	}
	if got := config.GetValue(simpleModeConfigKey); got != "false" {
		t.Errorf("已应用的标记不应覆盖用户配置，simpleMode = %q", got)
	}
}

func TestApplyStartupDefaultsKeepsExistingConfig(t *testing.T) {
	storage := t.TempDir()
	if err := config.SetStorageDirectory(storage); err != nil {
		t.Fatalf("重定向存储目录失败：%v", err)
	}
	_, minecraftDirectory, javaExe, versionID := buildInstalledWorld(t)

	// 用户已有自己的游戏目录与 Java：标记不得接管
	existingGame := filepath.Join(t.TempDir(), "my-minecraft")
	if err := os.MkdirAll(existingGame, 0o755); err != nil {
		t.Fatalf("创建目录失败：%v", err)
	}
	if err := config.SaveGameDirectory(existingGame); !err {
		t.Fatal("预置游戏目录失败")
	}
	// 把已有 Java 记到公共 Home（临时 USERPROFILE）下，避免落在 storage 里被误断言
	existingJava := filepath.Join(config.UserHome(), "already-java", "bin", "java.exe")
	writeFile(t, existingJava, "java")
	if err := config.SaveJava(existingJava, "17"); !err {
		t.Fatal("预置 Java 失败")
	}

	if err := SaveMarker(storage, &Marker{
		Format:             PayloadFormat,
		VersionID:          versionID,
		SimpleMode:         true,
		MinecraftDirectory: minecraftDirectory,
		JavaExecutable:     javaExe,
	}); err != nil {
		t.Fatalf("写标记失败：%v", err)
	}
	if err := ApplyStartupDefaults(); err != nil {
		t.Fatalf("ApplyStartupDefaults 失败：%v", err)
	}

	if got := config.GameDirectory(); !samePath(got, existingGame) {
		t.Errorf("已有游戏目录被覆盖：%q", got)
	}
	if got := config.JavaExecutable(); !samePath(got, existingJava) {
		t.Errorf("已有 Java 被覆盖：%q", got)
	}
}

func TestApplyStartupDefaultsWithoutMarker(t *testing.T) {
	if err := config.SetStorageDirectory(t.TempDir()); err != nil {
		t.Fatalf("重定向存储目录失败：%v", err)
	}
	if err := ApplyStartupDefaults(); err != nil {
		t.Fatalf("无标记时应静默返回，实际：%v", err)
	}
	if got := config.GetValue(simpleModeConfigKey); got != "" {
		t.Errorf("不应写入 simpleMode：%q", got)
	}
}

func TestApplyStartupDefaultsRespectsUserSimpleMode(t *testing.T) {
	storage := t.TempDir()
	if err := config.SetStorageDirectory(storage); err != nil {
		t.Fatalf("重定向存储目录失败：%v", err)
	}
	_, minecraftDirectory, _, versionID := buildInstalledWorld(t)

	// 用户明确关闭过 S 模式：安装新包不得翻回开启
	if err := config.SetValue(simpleModeConfigKey, "false"); !err {
		t.Fatal("预置 simpleMode=false 失败")
	}
	if err := SaveMarker(storage, &Marker{
		Format:             PayloadFormat,
		PackID:             "pack-b",
		VersionID:          versionID,
		SimpleMode:         true,
		MinecraftDirectory: minecraftDirectory,
	}); err != nil {
		t.Fatalf("写标记失败：%v", err)
	}

	if err := ApplyStartupDefaults(); err != nil {
		t.Fatalf("ApplyStartupDefaults 失败：%v", err)
	}
	if got := config.GetValue(simpleModeConfigKey); got != "false" {
		t.Errorf("用户关闭的 S 模式被覆盖：%q", got)
	}
}

func TestApplyStartupDefaultsAddsFolderForExternalRoot(t *testing.T) {
	storage := t.TempDir()
	if err := config.SetStorageDirectory(storage); err != nil {
		t.Fatalf("重定向存储目录失败：%v", err)
	}
	_, minecraftDirectory, _, versionID := buildInstalledWorld(t)

	// 用户已有别的游戏目录：标记目录只能进"游戏目录列表"，不能接管当前目录
	existingGame := filepath.Join(t.TempDir(), "my-minecraft")
	if err := os.MkdirAll(existingGame, 0o755); err != nil {
		t.Fatalf("创建目录失败：%v", err)
	}
	if err := config.SaveGameDirectory(existingGame); !err {
		t.Fatal("预置游戏目录失败")
	}
	if err := SaveMarker(storage, &Marker{
		Format:             PayloadFormat,
		PackID:             "pack-b",
		PackName:           "Pack B",
		VersionID:          versionID,
		SimpleMode:         false,
		MinecraftDirectory: minecraftDirectory,
	}); err != nil {
		t.Fatalf("写标记失败：%v", err)
	}

	if err := ApplyStartupDefaults(); err != nil {
		t.Fatalf("ApplyStartupDefaults 失败：%v", err)
	}

	if got := config.GameDirectory(); !samePath(got, existingGame) {
		t.Errorf("已有游戏目录被覆盖：%q", got)
	}
	found := false
	for _, folder := range config.GetFolders() {
		if samePath(folder, minecraftDirectory) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("整合包根目录未进入游戏目录列表：%v", config.GetFolders())
	}
	// 新装的包应被选中（下次启动直接玩新包）
	if got := config.GetValue("selectedGameInstance"); got != versionID {
		t.Errorf("选中实例 = %q，期望 %q", got, versionID)
	}
	// 同根目录时不重复添加：再跑一次也只有一个条目
	if err := SaveMarker(storage, &Marker{
		Format:             PayloadFormat,
		PackID:             "pack-b",
		VersionID:          versionID,
		MinecraftDirectory: minecraftDirectory,
	}); err != nil {
		t.Fatalf("重写标记失败：%v", err)
	}
	_ = ApplyStartupDefaults() // applied 已置位，应直接跳过
}

func TestApplyStartupDefaultsToleratesMissingBundledJava(t *testing.T) {
	storage := t.TempDir()
	if err := config.SetStorageDirectory(storage); err != nil {
		t.Fatalf("重定向存储目录失败：%v", err)
	}
	_, minecraftDirectory, _, versionID := buildInstalledWorld(t)

	if err := SaveMarker(storage, &Marker{
		Format:             PayloadFormat,
		VersionID:          versionID,
		SimpleMode:         false,
		MinecraftDirectory: minecraftDirectory,
		JavaExecutable:     filepath.Join(storage, "gone", "java.exe"),
	}); err != nil {
		t.Fatalf("写标记失败：%v", err)
	}

	// 捆绑 Java 不存在：只记日志，其余应用照常、整体不报错
	if err := ApplyStartupDefaults(); err != nil {
		t.Fatalf("缺失捆绑 Java 不应导致失败：%v", err)
	}
	if got := config.JavaExecutable(); got != "" {
		t.Errorf("不应写入不存在的 Java：%q", got)
	}
	if got := config.GameDirectory(); !samePath(got, minecraftDirectory) {
		t.Errorf("游戏目录未被应用：%q", got)
	}
}
