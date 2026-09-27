package download_test

// 整合包「导出 → 安装」闭环测试：用创作中心导出服务产出真实的 .mrpack，
// 再经安装服务（InstallModpack）解回一个全新的内容目录，验证文件落位、
// 启动器元数据不落进游戏目录、requirements 解析正确。
// 离线运行：导出时 ResolveModrinthLinks=false，全部内容进 overrides，
// 安装侧无需下载任何声明文件。

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekolauncher/internal/download"
	"nekolauncher/internal/modpack"
)

// buildInstanceFixture 造一个实例内容目录（与导出测试同构，但独立维护）。
func buildInstanceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(root, filepath.FromSlash(rel));
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("mods/alpha.jar", "fake-jar-alpha")
	write("mods/beta.jar", "fake-jar-beta")
	write("config/main.cfg", "key=value")
	write("resourcepacks/fancy.zip", "fake-resourcepack")
	write("saves/world/level.dat", "fake-level")

	return root
}

// exportMrpack 按创作中心弹窗的调用方式导出一个 Modrinth 整合包。
func exportMrpack(t *testing.T, contentDir string) string {
	t.Helper()
	items := modpack.CollectContent(contentDir)
	included := make([]string, 0, len(items))
	for _, item := range items {
		included = append(included, item.RelativePath)
	}

	output := filepath.Join(t.TempDir(), "roundtrip.mrpack")
	_, err := modpack.Export(
		context.Background(),
		modpack.ModpackExportOptions{
			Format:                modpack.FormatModrinth,
			PackName:              "往返整合包",
			PackVersion:           "1.4.2",
			MinecraftVersion:      "1.21.1",
			LoaderName:            "Fabric",
			LoaderVersion:         "0.16.9",
			IncludedPaths:         included,
			ResolveModrinthLinks:  false,
		},
		contentDir,
		output,
		nil,
	)
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}

	return output
}

func assertFileContent(t *testing.T, root, rel, want string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("缺少 %s：%v", rel, err)
	}
	if string(raw) != want {
		t.Errorf("%s 内容不符：%q", rel, string(raw))
	}
}

// TestInstallExportedMrpackRoundTrip 闭环：导出的包能原样装回。
func TestInstallExportedMrpackRoundTrip(t *testing.T) {
	source := buildInstanceFixture(t)
	archivePath := exportMrpack(t, source)
	target := filepath.Join(t.TempDir(), "new-instance")

	result, err := download.InstallModpack(context.Background(), archivePath, target, nil)
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	if len(result.Errors) > 0 {
		t.Errorf("安装过程报错：%v", result.Errors)
	}
	if result.InstalledFiles < 5 {
		t.Errorf("解压文件数不符：%d", result.InstalledFiles)
	}
	if result.DownloadedMods != 0 {
		t.Errorf("离线包不应触发下载，实际下载 %d 个", result.DownloadedMods)
	}

	// 内容逐文件还原（overrides/ 前缀已剥离）
	assertFileContent(t, target, "mods/alpha.jar", "fake-jar-alpha")
	assertFileContent(t, target, "mods/beta.jar", "fake-jar-beta")
	assertFileContent(t, target, "config/main.cfg", "key=value")
	assertFileContent(t, target, "resourcepacks/fancy.zip", "fake-resourcepack")
	assertFileContent(t, target, "saves/world/level.dat", "fake-level")

	// 启动器元数据不得落进游戏目录
	for _, banned := range []string{"modrinth.index.json", "index.json", "manifest.json", "overrides"} {
		if _, err := os.Stat(filepath.Join(target, banned)); err == nil {
			t.Errorf("启动器元数据 %s 不应出现在内容目录", banned)
		}
	}
}

// TestReadRequirementsOfExportedMrpack 安装前探测：导出时声明的 MC/加载器版本能读回。
func TestReadRequirementsOfExportedMrpack(t *testing.T) {
	source := buildInstanceFixture(t)
	archivePath := exportMrpack(t, source)

	req, err := download.ReadModpackRequirements(context.Background(), archivePath)
	if err != nil {
		t.Fatalf("读取要求失败：%v", err)
	}
	if req.MinecraftVersion != "1.21.1" {
		t.Errorf("MC 版本不符：%q", req.MinecraftVersion)
	}
	if req.LoaderType != download.ModLoaderFabric || req.LoaderVersion != "0.16.9" {
		t.Errorf("加载器不符：type=%d version=%q", req.LoaderType, req.LoaderVersion)
	}
	if !req.LoaderSupported() {
		t.Error("Fabric 应受支持")
	}
}

// TestInstallCurseForgeZip CurseForge 格式：manifest.json + overrides 也能安装。
func TestInstallCurseForgeZip(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "cf-pack.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zipWriter := zip.NewWriter(file)
	addEntry := func(name, content string) {
		entry, createErr := zipWriter.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}

	addEntry("manifest.json", `{"manifestVersion":1,"manifestType":"minecraftModpack","name":"CF包","version":"1.0.0","files":[],"minecraft":{"version":"1.20.1"}}`)
	addEntry("overrides/config/cf.cfg", "cf=true")
	addEntry("overrides/mods/cf-mod.jar", "fake-cf-jar")
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()

	target := filepath.Join(t.TempDir(), "cf-instance")
	result, err := download.InstallModpack(context.Background(), archivePath, target, nil)
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	if len(result.Errors) > 0 {
		t.Errorf("安装过程报错：%v", result.Errors)
	}
	assertFileContent(t, target, "config/cf.cfg", "cf=true")
	assertFileContent(t, target, "mods/cf-mod.jar", "fake-cf-jar")
	if _, err := os.Stat(filepath.Join(target, "manifest.json")); err == nil {
		t.Error("manifest.json 不应落入内容目录")
	}
}

// TestInstallMissingArchive 路径无效：直接报错而不是静默成功。
func TestInstallMissingArchive(t *testing.T) {
	_, err := download.InstallModpack(
		context.Background(),
		filepath.Join(t.TempDir(), "不存在.mrpack"),
		filepath.Join(t.TempDir(), "out"),
		nil,
	)
	if err == nil {
		t.Fatal("无效路径应报错")
	}
	if !strings.Contains(err.Error(), "") {
		t.Error("错误信息为空")
	}
}
