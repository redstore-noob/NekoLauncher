package solo

// 导出全流程的端到端测试：伪造 stub / 启动器 exe / 版本链 / 实例内容 / Java，
// 跑通 ExportSolo 后逐项校验最终 exe 的尾标、载荷条目与 CRC。
// 标记激活（ApplyStartupDefaults）见 marker_test.go。

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekolauncher/internal/config"
	"nekolauncher/internal/modpack"
)

// writeFile 辅助：创建带父目录的文件并写入内容。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败 %s：%v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件失败 %s：%v", path, err)
	}
}

// buildFakeWorld 构造一份作者侧实例：
//
//	root/versions/1.20.1/…                          （父版本）
//	root/versions/1.20.1-forge-47.2.0/…             （Forge 版本，inheritsFrom 1.20.1，同时是内容目录）
//	root/java-home/…                                （待捆绑的假 JRE）
func buildFakeWorld(t *testing.T, root string) (versionsRoot, contentDirectory, javaHome string) {
	t.Helper()
	forgeID := "1.20.1-forge-47.2.0"
	forgeDir := filepath.Join(root, "versions", forgeID)
	writeFile(t, filepath.Join(forgeDir, forgeID+".json"),
		`{"inheritsFrom": "1.20.1", "id": "`+forgeID+`"}`)
	writeFile(t, filepath.Join(forgeDir, forgeID+".jar"), "forge-jar-bytes")
	writeFile(t, filepath.Join(forgeDir, "options.txt"), "options-bytes")
	writeFile(t, filepath.Join(forgeDir, "mods", "jei.jar"), "jei-mod-bytes")
	writeFile(t, filepath.Join(forgeDir, "config", "jei.cfg"), "config-bytes")
	writeFile(t, filepath.Join(forgeDir, "saves", "world", "level.dat"), "level-bytes")

	parentDir := filepath.Join(root, "versions", "1.20.1")
	writeFile(t, filepath.Join(parentDir, "1.20.1.json"), `{"id": "1.20.1"}`)
	writeFile(t, filepath.Join(parentDir, "1.20.1.jar"), "vanilla-jar-bytes")

	javaHome = filepath.Join(root, "java-home")
	writeFile(t, filepath.Join(javaHome, "bin", "java.exe"), "fake-java")
	writeFile(t, filepath.Join(javaHome, "lib", "rt.jar"), "rt-bytes")
	writeFile(t, filepath.Join(javaHome, "jmods", "java.base.jmod"), "should-be-skipped")
	writeFile(t, filepath.Join(javaHome, "src.zip"), "should-be-skipped")

	return filepath.Join(root, "versions"), forgeDir, javaHome
}

// readSoloPayload 解析最终 exe：校验尾标与 CRC，返回载荷 zip 读取器。
func readSoloPayload(t *testing.T, path string) (*zip.Reader, Trailer) {
	t.Helper()
	trailer, err := ReadTrailerFromFile(path)
	if err != nil {
		t.Fatalf("解析尾标失败：%v", err)
	}
	reader, err := OpenPayloadRange(path, trailer)
	if err != nil {
		t.Fatalf("定位载荷失败：%v", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("读取载荷失败：%v", err)
	}
	if int64(len(data)) != trailer.Length {
		t.Fatalf("载荷长度 %d 与尾标声明 %d 不符", len(data), trailer.Length)
	}
	if crc32.ChecksumIEEE(data) != trailer.CRC32 {
		t.Fatal("载荷 CRC 校验失败")
	}
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("载荷不是有效 zip：%v", err)
	}
	return zipReader, trailer
}

func zipEntryText(t *testing.T, reader *zip.Reader, name string) string {
	t.Helper()
	for _, file := range reader.File {
		if file.Name == name {
			opened, err := file.Open()
			if err != nil {
				t.Fatalf("打开条目 %s 失败：%v", name, err)
			}
			content, err := io.ReadAll(opened)
			if err != nil {
				t.Fatalf("读取条目 %s 失败：%v", name, err)
			}
			return string(content)
		}
	}
	t.Fatalf("载荷缺少条目 %s", name)
	return ""
}

func zipHasEntry(reader *zip.Reader, name string) bool {
	for _, file := range reader.File {
		if file.Name == name {
			return true
		}
	}
	return false
}

func TestExportSoloEndToEnd(t *testing.T) {
	storage := t.TempDir()
	if err := config.SetStorageDirectory(storage); err != nil {
		t.Fatalf("重定向存储目录失败：%v", err)
	}
	world := t.TempDir()
	versionsRoot, contentDirectory, javaHome := buildFakeWorld(t, world)

	stubBytes := "FAKE-STUB-PE-IMAGE"
	stubPath := filepath.Join(t.TempDir(), "NekoSolo.Installer.exe")
	writeFile(t, stubPath, stubBytes)
	t.Setenv(stubEnvKey, stubPath)

	launcherBytes := "FAKE-NEKOLAUNCHER-EXE"
	launcherPath := filepath.Join(t.TempDir(), "NekoLauncher.exe")
	writeFile(t, launcherPath, launcherBytes)
	previous := launcherExecutable
	launcherExecutable = func() (string, error) { return launcherPath, nil }
	defer func() { launcherExecutable = previous }()

	forgeID := "1.20.1-forge-47.2.0"
	if !config.AddJava(filepath.Join(javaHome, "bin", "java.exe"), "17") {
		t.Fatal("注册首选 Java 失败")
	}

	outputPath := filepath.Join(t.TempDir(), "Demo-Setup.exe")
	options := SoloExportOptions{
		PackName:         "我的究极生存包",
		PackVersion:      "2.4.0",
		Author:           "作者喵",
		UpdateLink:       "https://example.com/pack",
		Description:      "测试包",
		MinecraftVersion: "1.20.1",
		LoaderName:       "Forge",
		LoaderVersion:    "47.2.0",
		IncludedPaths:    []string{"mods/jei.jar", "config", "saves/world", "options.txt"},
		ContentDirectory: contentDirectory,
		VersionDirectory: filepath.Join(versionsRoot, forgeID),
		VersionID:        forgeID,
		BundleJava:       true,
		SimpleMode:       true,
	}

	var phases []string
	result, err := ExportSolo(t.Context(), options, outputPath, func(p modpack.ModpackExportProgress) {
		phases = append(phases, p.Phase)
	})
	if err != nil {
		t.Fatalf("ExportSolo 失败：%v", err)
	}
	if result.OutputPath != outputPath || result.OverrideFiles != 4 || result.DeclaredFiles != 4 {
		t.Fatalf("导出统计不符：%+v", result)
	}
	if phases[len(phases)-1] != "完成" {
		t.Fatalf("进度未以「完成」收尾：%v", phases)
	}

	reader, trailer := readSoloPayload(t, outputPath)
	if trailer.Offset <= 0 {
		t.Fatalf("载荷偏移异常：%d", trailer.Offset)
	}

	// manifest：元数据完整、图标缺省不写条目
	manifest, err := ParseManifest([]byte(zipEntryText(t, reader, "manifest.json")))
	if err != nil {
		t.Fatalf("载荷清单解析失败：%v", err)
	}
	if manifest.VersionID != forgeID || manifest.PackVersion != "2.4.0" ||
		!manifest.SimpleMode || !manifest.HasJava || manifest.UpdateLink != "https://example.com/pack" {
		t.Fatalf("清单字段不符：%+v", manifest)
	}

	// files/：启动器本体逐字节一致（固定命名，与安装器/标记的约定一致）+ 便携标记
	if got := zipEntryText(t, reader, "files/NekoLauncher.exe"); got != launcherBytes {
		t.Fatalf("载荷内的启动器本体不符：%q", got)
	}
	if !zipHasEntry(reader, "files/portable.flag") {
		t.Fatal("载荷缺少 portable.flag")
	}

	// minecraft/：版本链（json+jar）+ 内容（进版本目录）
	if got := zipEntryText(t, reader, "minecraft/versions/"+forgeID+"/"+forgeID+".json"); !strings.Contains(got, "inheritsFrom") {
		t.Fatalf("Forge 版本 json 内容不符：%q", got)
	}
	if !zipHasEntry(reader, "minecraft/versions/"+forgeID+"/"+forgeID+".jar") {
		t.Fatal("载荷缺少 Forge jar")
	}
	if !zipHasEntry(reader, "minecraft/versions/1.20.1/1.20.1.json") ||
		!zipHasEntry(reader, "minecraft/versions/1.20.1/1.20.1.jar") {
		t.Fatal("载荷缺少 inheritsFrom 父版本文件")
	}
	for _, required := range []string{
		"minecraft/versions/" + forgeID + "/mods/jei.jar",
		"minecraft/versions/" + forgeID + "/config/jei.cfg",
		"minecraft/versions/" + forgeID + "/saves/world/level.dat",
	} {
		if !zipHasEntry(reader, required) {
			t.Fatalf("载荷缺少内容条目 %s", required)
		}
	}

	// jre/：运行时带上了，排除项没带上
	if got := zipEntryText(t, reader, "jre/bin/java.exe"); got != "fake-java" {
		t.Fatalf("捆绑 Java 内容不符：%q", got)
	}
	if zipHasEntry(reader, "jre/jmods/java.base.jmod") || zipHasEntry(reader, "jre/src.zip") {
		t.Fatal("捆绑 Java 不应包含 jmods / src.zip")
	}
}

// v2 在线安装包：exe 只含 stub + 远程清单 + v2 尾标；载荷 zip 落在输出旁待上传。
func TestExportSoloRemoteEndToEnd(t *testing.T) {
	storage := t.TempDir()
	if err := config.SetStorageDirectory(storage); err != nil {
		t.Fatalf("重定向存储目录失败：%v", err)
	}
	world := t.TempDir()
	versionsRoot, contentDirectory, _ := buildFakeWorld(t, world)

	stubBytes := "FAKE-STUB-PE-IMAGE"
	stubPath := filepath.Join(t.TempDir(), "NekoSolo.Installer.exe")
	writeFile(t, stubPath, stubBytes)
	t.Setenv(stubEnvKey, stubPath)

	launcherBytes := "FAKE-NEKOLAUNCHER-EXE"
	launcherPath := filepath.Join(t.TempDir(), "NekoLauncher.exe")
	writeFile(t, launcherPath, launcherBytes)
	previous := launcherExecutable
	launcherExecutable = func() (string, error) { return launcherPath, nil }
	defer func() { launcherExecutable = previous }()

	outputPath := filepath.Join(t.TempDir(), "Demo-Setup.exe")
	options := SoloExportOptions{
		PackName:           "我的究极生存包",
		PackVersion:        "2.4.0",
		MinecraftVersion:   "1.20.1",
		IncludedPaths:      []string{"mods/jei.jar", "config"},
		ContentDirectory:   contentDirectory,
		VersionDirectory:   filepath.Join(versionsRoot, "1.20.1-forge-47.2.0"),
		VersionID:          "1.20.1-forge-47.2.0",
		RemoteDistribution: true,
		PayloadURL:         "https://github.com/acme/pack/releases/download/v1/payload.zip",
	}
	result, err := ExportSolo(t.Context(), options, outputPath, nil)
	if err != nil {
		t.Fatalf("ExportSolo 失败：%v", err)
	}
	if result.PayloadPath == "" || result.PayloadSizeBytes <= 0 {
		t.Fatalf("远程导出应返回载荷路径与大小：%+v", result)
	}
	if _, err := os.Stat(result.PayloadPath); err != nil {
		t.Fatalf("载荷 zip 不存在：%v", err)
	}

	// exe 体积应远小于载荷：尾标 v2，内嵌清单可解析且指向载荷 URL
	trailer, err := ReadTrailerFromFile(outputPath)
	if err != nil {
		t.Fatalf("解析尾标失败：%v", err)
	}
	if !trailer.V2 {
		t.Fatal("远程安装包应使用 v2 尾标")
	}
	manifestData, err := OpenPayloadRange(outputPath, trailer)
	if err != nil {
		t.Fatalf("定位内嵌清单失败：%v", err)
	}
	raw, err := io.ReadAll(manifestData)
	manifestData.Close()
	if err != nil {
		t.Fatalf("读取内嵌清单失败：%v", err)
	}
	if crc32.ChecksumIEEE(raw) != trailer.CRC32 {
		t.Fatal("内嵌清单 CRC 校验失败")
	}
	var remote RemoteManifest
	if err := json.Unmarshal(raw, &remote); err != nil {
		t.Fatalf("内嵌清单不是有效 JSON：%v", err)
	}
	if remote.PayloadURL != options.PayloadURL ||
		remote.PackVersion != "2.4.0" ||
		remote.VersionID != "1.20.1-forge-47.2.0" ||
		remote.PayloadSize != result.PayloadSizeBytes {
		t.Fatalf("远程清单字段不符：%+v", remote)
	}

	// 载荷 zip 本身可按清单校验（模拟安装器下载后的校验路径）
	payloadData, err := os.ReadFile(result.PayloadPath)
	if err != nil {
		t.Fatalf("读取载荷 zip 失败：%v", err)
	}
	if int64(len(payloadData)) != remote.PayloadSize || crc32.ChecksumIEEE(payloadData) != remote.PayloadCRC32 {
		t.Fatal("载荷 zip 大小或 CRC 与清单不符")
	}
	if _, err := zip.NewReader(bytes.NewReader(payloadData), int64(len(payloadData))); err != nil {
		t.Fatalf("载荷 zip 无效：%v", err)
	}
}

func TestExportSoloWithoutJavaAndIcon(t *testing.T) {
	if err := config.SetStorageDirectory(t.TempDir()); err != nil {
		t.Fatalf("重定向存储目录失败：%v", err)
	}
	world := t.TempDir()
	versionsRoot, contentDirectory, _ := buildFakeWorld(t, world)

	stubPath := filepath.Join(t.TempDir(), "stub.exe")
	writeFile(t, stubPath, "STUB")
	t.Setenv(stubEnvKey, stubPath)

	launcherPath := filepath.Join(t.TempDir(), "NekoLauncher.exe")
	writeFile(t, launcherPath, "LAUNCHER")
	previous := launcherExecutable
	launcherExecutable = func() (string, error) { return launcherPath, nil }
	defer func() { launcherExecutable = previous }()

	iconPath := filepath.Join(t.TempDir(), "icon.png")
	writeFile(t, iconPath, "fake-png")

	outputPath := filepath.Join(t.TempDir(), "Demo-Setup.exe")
	forgeID := "1.20.1-forge-47.2.0"
	// 勾选捆绑 Java 但从未配置过首选 Java：应跳过并给出警告
	result, err := ExportSolo(t.Context(), SoloExportOptions{
		PackName:         "NoJava Pack",
		PackVersion:      "1.0.0",
		MinecraftVersion: "1.20.1",
		IncludedPaths:    []string{"mods/jei.jar"},
		ContentDirectory: contentDirectory,
		VersionDirectory: filepath.Join(versionsRoot, forgeID),
		VersionID:        forgeID,
		IconPngPath:      iconPath,
		BundleJava:       true,
		SimpleMode:       false,
	}, outputPath, nil)
	if err != nil {
		t.Fatalf("ExportSolo 失败：%v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "Java") {
		t.Fatalf("未捆绑 Java 时应有警告：%v", result.Warnings)
	}

	reader, _ := readSoloPayload(t, outputPath)
	manifest, err := ParseManifest([]byte(zipEntryText(t, reader, "manifest.json")))
	if err != nil {
		t.Fatalf("清单解析失败：%v", err)
	}
	if manifest.HasJava || manifest.IconPath != "icon.png" || manifest.SimpleMode {
		t.Fatalf("清单字段不符：%+v", manifest)
	}
	if !zipHasEntry(reader, "icon.png") {
		t.Fatal("载荷缺少图标条目")
	}
	if zipHasEntry(reader, "jre/bin/java.exe") {
		t.Fatal("未勾选捆绑时不应有 jre 条目")
	}
	// VersionID 省略时回退为 VersionDirectory 的目录名
	if manifest.VersionID != forgeID {
		t.Fatalf("VersionID 回退失败：%q", manifest.VersionID)
	}
}

func TestExportSoloFailures(t *testing.T) {
	storage := t.TempDir()
	if err := config.SetStorageDirectory(storage); err != nil {
		t.Fatalf("重定向存储目录失败：%v", err)
	}
	world := t.TempDir()
	versionsRoot, contentDirectory, _ := buildFakeWorld(t, world)
	forgeID := "1.20.1-forge-47.2.0"

	// 输出扩展名必须是 .exe
	if _, err := ExportSolo(t.Context(), SoloExportOptions{PackName: "x", MinecraftVersion: "1.20.1",
		ContentDirectory: contentDirectory, VersionDirectory: filepath.Join(versionsRoot, forgeID)},
		filepath.Join(t.TempDir(), "out.zip"), nil); err == nil {
		t.Fatal("非 .exe 输出应被拒绝")
	}

	// 模板缺失：清掉环境变量后必须报错并提示构建方式
	t.Setenv(stubEnvKey, "")
	_, err := ExportSolo(t.Context(), SoloExportOptions{PackName: "x", MinecraftVersion: "1.20.1",
		ContentDirectory: contentDirectory, VersionDirectory: filepath.Join(versionsRoot, forgeID)},
		filepath.Join(t.TempDir(), "out.exe"), nil)
	if err == nil || !strings.Contains(err.Error(), "NEKOSOLO_STUB") {
		t.Fatalf("缺少模板时的报错应给出指引，实际：%v", err)
	}

	// 环境变量指向不存在的文件
	t.Setenv(stubEnvKey, filepath.Join(t.TempDir(), "missing.exe"))
	_, err = ExportSolo(t.Context(), SoloExportOptions{PackName: "x", MinecraftVersion: "1.20.1",
		ContentDirectory: contentDirectory, VersionDirectory: filepath.Join(versionsRoot, forgeID)},
		filepath.Join(t.TempDir(), "out.exe"), nil)
	if err == nil || !strings.Contains(err.Error(), "NEKOSOLO_STUB") {
		t.Fatalf("模板不存在时应报错，实际：%v", err)
	}

	// 版本 json 缺失
	t.Setenv(stubEnvKey, "")
	broken := t.TempDir()
	brokenID := "1.20.1-broken"
	brokenDir := filepath.Join(broken, "versions", brokenID)
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatalf("创建目录失败：%v", err)
	}
	_, err = ExportSolo(t.Context(), SoloExportOptions{PackName: "x", MinecraftVersion: "1.20.1",
		ContentDirectory: contentDirectory, VersionDirectory: brokenDir, VersionID: brokenID},
		filepath.Join(t.TempDir(), "out.exe"), nil)
	if err == nil || !errors.Is(err, os.ErrNotExist) && !strings.Contains(err.Error(), brokenID+".json") {
		t.Fatalf("版本 json 缺失应报错，实际：%v", err)
	}
}

func TestFindStubTemplateCandidates(t *testing.T) {
	directory := t.TempDir()
	t.Setenv(stubEnvKey, "")
	previous := stubSearchBases
	stubSearchBases = func() []string { return []string{filepath.Join(directory, "NekoSolo", "build")} }
	defer func() { stubSearchBases = previous }()

	// NekoSolo/build/NekoSolo.Installer.exe 是仓库推荐发布位置
	buildDir := filepath.Join(directory, "NekoSolo", "build")
	writeFile(t, filepath.Join(buildDir, "NekoSolo.Installer.exe"), "STUB")
	found, err := FindStubTemplate()
	if err != nil {
		t.Fatalf("应从 NekoSolo/build 找到模板：%v", err)
	}
	if filepath.Base(found) != "NekoSolo.Installer.exe" {
		t.Fatalf("找到的模板不符：%s", found)
	}

	// 环境变量优先级最高
	override := filepath.Join(t.TempDir(), "custom.exe")
	writeFile(t, override, "STUB2")
	t.Setenv(stubEnvKey, override)
	found, err = FindStubTemplate()
	if err != nil || found != override {
		t.Fatalf("环境变量指定的模板应优先，实际：%s, %v", found, err)
	}
}
