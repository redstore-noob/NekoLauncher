package solo

// 导出/提取全流程的端到端测试：伪造 stub / 启动器 exe / 实例内容，跑通
// ExportSolo 后逐项校验最终 exe 的尾标、载荷条目与 CRC；再验证
// ExtractSoloPack / ImportSoloExe 的往返。标记激活见 marker_test.go。

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
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

// buildFakeWorld 构造一份作者侧实例内容（v3 不再需要版本目录与 Java）。
func buildFakeWorld(t *testing.T, root string) (contentDirectory string) {
	t.Helper()
	forgeDir := filepath.Join(root, "versions", "1.20.1-forge-47.2.0")
	writeFile(t, filepath.Join(forgeDir, "options.txt"), "options-bytes")
	writeFile(t, filepath.Join(forgeDir, "mods", "jei.jar"), "jei-mod-bytes")
	writeFile(t, filepath.Join(forgeDir, "config", "jei.cfg"), "config-bytes")
	writeFile(t, filepath.Join(forgeDir, "saves", "world", "level.dat"), "level-bytes")
	return forgeDir
}

// stubModrinthResolver 替换 Modrinth 直链解析：匹配命中由 wantMatch 控制。
func stubModrinthResolver(t *testing.T, wantMatch bool) {
	t.Helper()
	previous := modpack.ModrinthResolver
	modpack.ModrinthResolver = func(ctx context.Context, sha1Hex string) (*modpack.ModrinthFileMatch, error) {
		if !wantMatch {
			return nil, nil
		}
		return &modpack.ModrinthFileMatch{
			FileName:    "jei.jar",
			DownloadUrl: "https://cdn.modrinth.com/jei.jar",
			Sha1:        sha1Hex,
			SizeBytes:   int64(len("jei-mod-bytes")),
		}, nil
	}
	t.Cleanup(func() { modpack.ModrinthResolver = previous })
}

// buildExportWorld 组装导出所需的 stub / 启动器注入，返回输出路径辅助。
func buildExportWorld(t *testing.T) {
	t.Helper()
	if err := config.SetStorageDirectory(t.TempDir()); err != nil {
		t.Fatalf("重定向存储目录失败：%v", err)
	}
	stubPath := filepath.Join(t.TempDir(), "NekoSolo.Installer.exe")
	writeFile(t, stubPath, "FAKE-STUB-PE-IMAGE")
	t.Setenv(stubEnvKey, stubPath)

	launcherPath := filepath.Join(t.TempDir(), "NekoLauncher.exe")
	writeFile(t, launcherPath, "FAKE-NEKOLAUNCHER-EXE")
	previous := launcherExecutable
	launcherExecutable = func() (string, error) { return launcherPath, nil }
	t.Cleanup(func() { launcherExecutable = previous })
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
	buildExportWorld(t)
	stubModrinthResolver(t, true)
	world := t.TempDir()
	contentDirectory := buildFakeWorld(t, world)

	forgeID := "1.20.1-forge-47.2.0"
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
		VersionID:        forgeID,
		SimpleMode:       true,
	}

	var phases []string
	result, err := ExportSolo(t.Context(), options, outputPath, func(p modpack.ModpackExportProgress) {
		phases = append(phases, p.Phase)
	})
	if err != nil {
		t.Fatalf("ExportSolo 失败：%v", err)
	}
	if result.OutputPath != outputPath {
		t.Fatalf("导出统计不符：%+v", result)
	}
	if result.DeclaredFiles != 1 || result.OverrideFiles != 3 {
		t.Fatalf("导出统计不符（声明/覆写应为 1/3）：%+v", result)
	}
	if phases[len(phases)-1] != "完成" {
		t.Fatalf("进度未以「完成」收尾：%v", phases)
	}

	reader, trailer := readSoloPayload(t, outputPath)
	if trailer.Offset <= 0 {
		t.Fatalf("载荷偏移异常：%d", trailer.Offset)
	}
	if !trailer.V3 {
		t.Fatal("v3 导出应写 NKSOLO3 尾标")
	}

	// manifest：元数据完整、不再声明捆绑 Java
	manifest, err := ParseManifest([]byte(zipEntryText(t, reader, "manifest.json")))
	if err != nil {
		t.Fatalf("载荷清单解析失败：%v", err)
	}
	if manifest.VersionID != forgeID || manifest.PackVersion != "2.4.0" ||
		!manifest.SimpleMode || manifest.HasJava || manifest.UpdateLink != "https://example.com/pack" {
		t.Fatalf("清单字段不符：%+v", manifest)
	}

	// files/：启动器本体逐字节一致（固定命名，与安装器/标记的约定一致）+ 便携标记
	if got := zipEntryText(t, reader, "files/NekoLauncher.exe"); got != "FAKE-NEKOLAUNCHER-EXE" {
		t.Fatalf("载荷内的启动器本体不符：%q", got)
	}
	if !zipHasEntry(reader, "files/portable.flag") {
		t.Fatal("载荷缺少 portable.flag")
	}

	// v3 载荷 = 标准 mrpack：modrinth.index.json 声明直链，overrides/ 带回退内容
	indexRaw := zipEntryText(t, reader, "modrinth.index.json")
	var index struct {
		Files []struct {
			Path      string   `json:"path"`
			Downloads []string `json:"downloads"`
		} `json:"files"`
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(indexRaw), &index); err != nil {
		t.Fatalf("modrinth.index.json 解析失败：%v", err)
	}
	if len(index.Files) != 1 || index.Files[0].Path != "mods/jei.jar" ||
		len(index.Files[0].Downloads) != 1 || !strings.HasPrefix(index.Files[0].Downloads[0], "https://") {
		t.Fatalf("index 声明不符：%+v", index.Files)
	}
	if index.Dependencies["minecraft"] != "1.20.1" || index.Dependencies["forge"] != "47.2.0" {
		t.Fatalf("index dependencies 不符：%+v", index.Dependencies)
	}
	for _, required := range []string{
		"overrides/mods/jei.jar",
		"overrides/config/jei.cfg",
		"overrides/saves/world/level.dat",
		"overrides/options.txt",
	} {
		if zipHasEntry(reader, required) && required == "overrides/mods/jei.jar" {
			t.Fatalf("已声明直链的 mod 不该再进 overrides：%s", required)
		}
		if required != "overrides/mods/jei.jar" && !zipHasEntry(reader, required) {
			t.Fatalf("载荷缺少 overrides 条目 %s", required)
		}
	}

	// v3 不再携带版本描述与 jre/
	for _, forbidden := range []string{
		"minecraft/versions/" + forgeID + "/" + forgeID + ".json",
		"jre/bin/java.exe",
	} {
		if zipHasEntry(reader, forbidden) {
			t.Fatalf("v3 载荷不该包含 %s（MC 本体与 Java 均由首启联网补全）", forbidden)
		}
	}
}

// ExtractSoloPack：从导出的 exe 里提出标准 .mrpack（不含 manifest/files）。
func TestExtractSoloPack(t *testing.T) {
	buildExportWorld(t)
	stubModrinthResolver(t, false)
	world := t.TempDir()
	contentDirectory := buildFakeWorld(t, world)

	outputPath := filepath.Join(t.TempDir(), "Demo-Setup.exe")
	forgeID := "1.20.1-forge-47.2.0"
	if _, err := ExportSolo(t.Context(), SoloExportOptions{
		PackName:         "提取测试包",
		PackVersion:      "1.0.0",
		MinecraftVersion: "1.20.1",
		LoaderName:       "Forge",
		LoaderVersion:    "47.2.0",
		IncludedPaths:    []string{"mods/jei.jar", "options.txt"},
		ContentDirectory: contentDirectory,
		VersionID:        forgeID,
	}, outputPath, nil); err != nil {
		t.Fatalf("导出失败：%v", err)
	}

	mrpackPath := filepath.Join(t.TempDir(), "extracted.mrpack")
	if err := ExtractSoloPack(outputPath, mrpackPath); err != nil {
		t.Fatalf("提取失败：%v", err)
	}
	reader, err := zip.OpenReader(mrpackPath)
	if err != nil {
		t.Fatalf("提取结果不是有效 zip：%v", err)
	}
	defer reader.Close()
	if !zipHasEntry(&reader.Reader, "modrinth.index.json") {
		t.Fatal("提取结果缺少 modrinth.index.json")
	}
	if !zipHasEntry(&reader.Reader, "overrides/mods/jei.jar") {
		t.Fatal("提取结果缺少 overrides 内容")
	}
	for _, forbidden := range []string{"manifest.json", "files/NekoLauncher.exe", "files/portable.flag"} {
		if zipHasEntry(&reader.Reader, forbidden) {
			t.Fatalf("提取结果不该包含启动器条目 %s", forbidden)
		}
	}

	// ImportSoloExe：转存出的临时 mrpack 同样是标准 Modrinth 结构
	imported, err := ImportSoloExe(outputPath)
	if err != nil {
		t.Fatalf("导入转存失败：%v", err)
	}
	defer os.Remove(imported)
	if filepath.Ext(imported) != ".mrpack" || !strings.Contains(imported, "solo-import-") {
		t.Fatalf("临时 mrpack 路径不符：%s", imported)
	}
	importedReader, err := zip.OpenReader(imported)
	if err != nil {
		t.Fatalf("转存结果不是有效 zip：%v", err)
	}
	defer importedReader.Close()
	if !zipHasEntry(&importedReader.Reader, "modrinth.index.json") {
		t.Fatal("转存结果缺少 modrinth.index.json")
	}
}

// 非 NekoSolo 的 exe（无有效尾标）不能被提取/导入。
func TestExtractSoloPackRejectsForeignExe(t *testing.T) {
	foreign := filepath.Join(t.TempDir(), "not-solo.exe")
	writeFile(t, foreign, "MZ... definitely not a NekoSolo installer")
	if err := ExtractSoloPack(foreign, filepath.Join(t.TempDir(), "out.mrpack")); err == nil {
		t.Fatal("非 NekoSolo exe 应被拒绝")
	}
	if _, err := ImportSoloExe(foreign); err == nil {
		t.Fatal("非 NekoSolo exe 应被拒绝导入")
	}
}

// 在线安装包（v2 尾标）：exe 只含 stub + 远程清单 + 尾标；载荷 zip（v3 布局）
// 落在输出旁待上传。
func TestExportSoloRemoteEndToEnd(t *testing.T) {
	buildExportWorld(t)
	stubModrinthResolver(t, false)
	world := t.TempDir()
	contentDirectory := buildFakeWorld(t, world)

	outputPath := filepath.Join(t.TempDir(), "Demo-Setup.exe")
	options := SoloExportOptions{
		PackName:           "我的究极生存包",
		PackVersion:        "2.4.0",
		MinecraftVersion:   "1.20.1",
		IncludedPaths:      []string{"mods/jei.jar", "config"},
		ContentDirectory:   contentDirectory,
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

	// 载荷 zip 本身可按清单校验（模拟安装器下载后的校验路径），
	// 且布局是 v3（modrinth.index.json + files/，无 minecraft/、jre/）
	payloadData, err := os.ReadFile(result.PayloadPath)
	if err != nil {
		t.Fatalf("读取载荷 zip 失败：%v", err)
	}
	if int64(len(payloadData)) != remote.PayloadSize || crc32.ChecksumIEEE(payloadData) != remote.PayloadCRC32 {
		t.Fatal("载荷 zip 大小或 CRC 与清单不符")
	}
	payloadReader, err := zip.NewReader(bytes.NewReader(payloadData), int64(len(payloadData)))
	if err != nil {
		t.Fatalf("载荷 zip 无效：%v", err)
	}
	if !zipHasEntry(payloadReader, "modrinth.index.json") || !zipHasEntry(payloadReader, "files/NekoLauncher.exe") {
		t.Fatal("远程载荷应为 v3 布局")
	}
}

func TestExportSoloWithIcon(t *testing.T) {
	buildExportWorld(t)
	stubModrinthResolver(t, false)
	world := t.TempDir()
	contentDirectory := buildFakeWorld(t, world)

	iconPath := filepath.Join(t.TempDir(), "icon.png")
	writeFile(t, iconPath, "fake-png")

	outputPath := filepath.Join(t.TempDir(), "Demo-Setup.exe")
	forgeID := "1.20.1-forge-47.2.0"
	_, err := ExportSolo(t.Context(), SoloExportOptions{
		PackName:         "Icon Pack",
		PackVersion:      "1.0.0",
		MinecraftVersion: "1.20.1",
		IncludedPaths:    []string{"mods/jei.jar"},
		ContentDirectory: contentDirectory,
		VersionID:        forgeID,
		IconPngPath:      iconPath,
		SimpleMode:       false,
	}, outputPath, nil)
	if err != nil {
		t.Fatalf("ExportSolo 失败：%v", err)
	}

	reader, _ := readSoloPayload(t, outputPath)
	manifest, err := ParseManifest([]byte(zipEntryText(t, reader, "manifest.json")))
	if err != nil {
		t.Fatalf("清单解析失败：%v", err)
	}
	if manifest.IconPath != "overrides/icon.png" {
		t.Fatalf("清单图标路径不符：%+v", manifest)
	}
	if !zipHasEntry(reader, "overrides/icon.png") {
		t.Fatal("载荷缺少 overrides/icon.png")
	}
	// VersionID 省略时回退为 VersionDirectory 的目录名
	options := SoloExportOptions{
		PackName:         "Icon Pack",
		MinecraftVersion: "1.20.1",
		IncludedPaths:    []string{"mods/jei.jar"},
		ContentDirectory: contentDirectory,
		VersionDirectory: filepath.Join(world, "versions", forgeID),
	}
	if _, err := ExportSolo(t.Context(), options, outputPath, nil); err != nil {
		t.Fatalf("重导出失败：%v", err)
	}
	reader2, _ := readSoloPayload(t, outputPath)
	manifest2, err := ParseManifest([]byte(zipEntryText(t, reader2, "manifest.json")))
	if err != nil {
		t.Fatalf("清单解析失败：%v", err)
	}
	if manifest2.VersionID != forgeID {
		t.Fatalf("VersionID 回退失败：%q", manifest2.VersionID)
	}
}

func TestExportSoloFailures(t *testing.T) {
	buildExportWorld(t)
	world := t.TempDir()
	contentDirectory := buildFakeWorld(t, world)
	forgeID := "1.20.1-forge-47.2.0"

	// 输出扩展名必须是 .exe
	if _, err := ExportSolo(t.Context(), SoloExportOptions{PackName: "x", MinecraftVersion: "1.20.1",
		ContentDirectory: contentDirectory, VersionID: forgeID},
		filepath.Join(t.TempDir(), "out.zip"), nil); err == nil {
		t.Fatal("非 .exe 输出应被拒绝")
	}

	// 模板缺失：清掉环境变量后必须报错并提示构建方式
	t.Setenv(stubEnvKey, "")
	_, err := ExportSolo(t.Context(), SoloExportOptions{PackName: "x", MinecraftVersion: "1.20.1",
		ContentDirectory: contentDirectory, VersionID: forgeID},
		filepath.Join(t.TempDir(), "out.exe"), nil)
	if err == nil || !strings.Contains(err.Error(), "NEKOSOLO_STUB") {
		t.Fatalf("缺少模板时的报错应给出指引，实际：%v", err)
	}

	// 环境变量指向不存在的文件
	t.Setenv(stubEnvKey, filepath.Join(t.TempDir(), "missing.exe"))
	_, err = ExportSolo(t.Context(), SoloExportOptions{PackName: "x", MinecraftVersion: "1.20.1",
		ContentDirectory: contentDirectory, VersionID: forgeID},
		filepath.Join(t.TempDir(), "out.exe"), nil)
	if err == nil || !strings.Contains(err.Error(), "NEKOSOLO_STUB") {
		t.Fatalf("模板不存在时应报错，实际：%v", err)
	}

	// 无法确定实例名
	_, err = ExportSolo(t.Context(), SoloExportOptions{PackName: "x", MinecraftVersion: "1.20.1",
		ContentDirectory: contentDirectory},
		filepath.Join(t.TempDir(), "out.exe"), nil)
	if err == nil || !strings.Contains(err.Error(), "版本目录") {
		t.Fatalf("实例名缺失应报错，实际：%v", err)
	}
}

// TestExportSoloReplacesQuestionMarkInOutputPath 输出路径（整合包名/版本号拼出来的
// 文件名）里的 '?' 换成 '0'：Windows 不允许文件名带 '?'，不换的话作者只会看到
// "点了保存什么都没生成"。
func TestExportSoloReplacesQuestionMarkInOutputPath(t *testing.T) {
	buildExportWorld(t)
	stubModrinthResolver(t, false)
	world := t.TempDir()
	contentDirectory := buildFakeWorld(t, world)

	forgeID := "1.20.1-forge-47.2.0"
	directory := t.TempDir()
	outputPath := filepath.Join(directory, "我的包?.exe")
	expected := filepath.Join(directory, "我的包0.exe")

	result, err := ExportSolo(t.Context(), SoloExportOptions{
		PackName:         "我的包?",
		PackVersion:      "1.0.0",
		MinecraftVersion: "1.20.1",
		LoaderName:       "Forge",
		LoaderVersion:    "47.2.0",
		IncludedPaths:    []string{"mods/jei.jar", "options.txt"},
		ContentDirectory: contentDirectory,
		VersionID:        forgeID,
	}, outputPath, nil)
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	if result.OutputPath != expected {
		t.Errorf("导出结果路径 = %q，期望 %q", result.OutputPath, expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("产物未落在替换后的路径：%v", err)
	}
	if _, err := os.Stat(outputPath); err == nil {
		t.Errorf("不该留下带 '?' 的产物：%s", outputPath)
	}
	// 临时文件与载荷都从输出路径派生，改名后不该有残留
	if leftovers, _ := filepath.Glob(filepath.Join(directory, "*.nekosolo-tmp*")); len(leftovers) != 0 {
		t.Errorf("残留临时文件：%v", leftovers)
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

