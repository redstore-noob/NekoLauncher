package download_test

// 整合包「导出 → 安装」闭环测试：用创作中心导出服务产出真实的 .mrpack，
// 再经安装服务（InstallModpack）解回一个全新的内容目录，验证文件落位、
// 启动器元数据不落进游戏目录、requirements 解析正确。
// 离线运行：导出时 ResolveModrinthLinks=false，全部内容进 overrides，
// 安装侧无需下载任何声明文件。

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekolauncher/internal/download"
	"nekolauncher/internal/modpack"
)

// writeZip 把 map 里的条目写成一个 zip（键为包内路径）。map 迭代顺序随机，
// 但安装侧的层叠解压不依赖条目顺序，正好顺带验证了这一点。
func writeZip(t *testing.T, name string, entries map[string]string) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), name)
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for entryName, content := range entries {
		entry, createErr := writer.Create(entryName)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}

func sha1Hex(content string) string {
	sum := sha1.Sum([]byte(content))
	return hex.EncodeToString(sum[:])
}

func sha512Hex(content string) string {
	sum := sha512.Sum512([]byte(content))
	return hex.EncodeToString(sum[:])
}

// buildInstanceFixture 造一个实例内容目录（与导出测试同构，但独立维护）。
func buildInstanceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
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
			Format:               modpack.FormatModrinth,
			PackName:             "往返整合包",
			PackVersion:          "1.4.2",
			MinecraftVersion:     "1.21.1",
			LoaderName:           "Fabric",
			LoaderVersion:        "0.16.9",
			IncludedPaths:        included,
			ResolveModrinthLinks: false,
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

	result, err := download.InstallModpack(context.Background(), "", archivePath, target, nil)
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
	result, err := download.InstallModpack(context.Background(), "", archivePath, target, nil)
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
		context.Background(), "",
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

// ---------------------------------------------------------------------------
// MultiMC / Prism 链路
// ---------------------------------------------------------------------------

// buildMultiMcZip 造一个 MultiMC/Prism 包：mmc-pack.json + instance.cfg，
// 游戏内容放在 .minecraft/ 下（与导出侧 exportservice.go 的约定一致）。
func buildMultiMcZip(t *testing.T, componentsJSON string, content map[string]string) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "mmc-pack.zip")
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

	addEntry("mmc-pack.json", componentsJSON)
	addEntry("instance.cfg", "InstanceType=OneSix\nname=MultiMC包\n")
	for name, body := range content {
		addEntry(".minecraft/"+name, body)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()

	return archivePath
}

// TestInstallWarnsOnUnsupportedLoader 防的回归：清单声明了本启动器不支持的
// 加载器（如 rift-loader）时，此前会静默退化成"原版"照装——文件解开了、
// 依赖也下了，启动却因为少了加载器崩掉，而界面只报一个文件数。
func TestInstallWarnsOnUnsupportedLoader(t *testing.T) {
	archivePath := writeZip(t, "unsupported.mrpack", map[string]string{
		"modrinth.index.json": `{"formatVersion":1,"game":"minecraft","name":"未知加载器包",` +
			`"versionId":"1.0.0","dependencies":{"minecraft":"1.12.2","rift-loader":"1.0.4"},` +
			`"files":[]}`,
		"overrides/mods/riftmod.jar": "rift-mod",
	})

	target := filepath.Join(t.TempDir(), "unsupported-instance")
	result, err := download.InstallModpack(context.Background(), "", archivePath, target, nil)
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	if result.DeclaredLoaderName != "rift-loader" {
		t.Errorf("声明加载器名 = %q，期望 rift-loader（识别不出时退回原始键）",
			result.DeclaredLoaderName)
	}
	if result.DeclaredLoaderVersion != "1.0.4" {
		t.Errorf("声明加载器版本 = %q，期望 1.0.4", result.DeclaredLoaderVersion)
	}

	joined := strings.Join(result.Warnings, " ")
	if !strings.Contains(joined, "暂不受支持") {
		t.Errorf("不受支持的加载器必须给出告警，实际告警：%v", result.Warnings)
	}
	if !strings.Contains(joined, "rift-loader") {
		t.Errorf("告警应点明是哪个加载器，实际：%v", result.Warnings)
	}
}

// TestReadRequirementsOfMultiMcPack 防的回归：MultiMC 包此前完全读不出运行要求
// （只认 modrinth.index.json / manifest.json），于是前端跳过"准备 MC + 加载器"，
// 包被解到共享根目录、启动即缺加载器。mmc-pack.json 必须能解析出 MC 与加载器。
func TestReadRequirementsOfMultiMcPack(t *testing.T) {
	cases := []struct {
		name        string
		components  string
		wantMC      string
		wantLoader  download.ModLoaderType
		wantVersion string
		wantRawKey  string
	}{
		{
			name: "Forge",
			components: `{"formatVersion":1,"components":[` +
				`{"uid":"net.minecraft","version":"1.20.1"},` +
				`{"uid":"net.minecraftforge","version":"47.2.0"}]}`,
			wantMC:      "1.20.1",
			wantLoader:  download.ModLoaderForge,
			wantVersion: "47.2.0",
			wantRawKey:  "forge",
		},
		{
			name: "Fabric",
			components: `{"formatVersion":1,"components":[` +
				`{"uid":"net.minecraft","version":"1.21.1"},` +
				`{"uid":"net.fabricmc.fabric-loader","version":"0.16.9"}]}`,
			wantMC:      "1.21.1",
			wantLoader:  download.ModLoaderFabric,
			wantVersion: "0.16.9",
			wantRawKey:  "fabric-loader",
		},
		{
			name: "NeoForge",
			components: `{"formatVersion":1,"components":[` +
				`{"uid":"net.minecraft","version":"1.20.4"},` +
				`{"uid":"net.neoforged.neoforge","version":"20.4.237"}]}`,
			wantMC:      "1.20.4",
			wantLoader:  download.ModLoaderNeoForge,
			wantVersion: "20.4.237",
			wantRawKey:  "neoforge",
		},
		{
			name: "Quilt",
			components: `{"formatVersion":1,"components":[` +
				`{"uid":"net.minecraft","version":"1.20.1"},` +
				`{"uid":"org.quiltmc.quilt-loader","version":"0.23.1"}]}`,
			wantMC:      "1.20.1",
			wantLoader:  download.ModLoaderQuilt,
			wantVersion: "0.23.1",
			wantRawKey:  "quilt-loader",
		},
		{
			// 只有 net.minecraft：原版包，必须能被识别（而不是当作"无要求"）
			name: "原版",
			components: `{"formatVersion":1,"components":[` +
				`{"uid":"net.minecraft","version":"1.19.2"}]}`,
			wantMC:     "1.19.2",
			wantLoader: download.ModLoaderVanilla,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			archivePath := buildMultiMcZip(t, testCase.components, nil)

			req, err := download.ReadModpackRequirements(context.Background(), archivePath)
			if err != nil {
				t.Fatalf("读取要求失败：%v", err)
			}
			if req == nil {
				t.Fatal("MultiMC 包应能解析出运行要求，实际为 nil")
			}
			if req.MinecraftVersion != testCase.wantMC {
				t.Errorf("MC 版本 = %q，期望 %q", req.MinecraftVersion, testCase.wantMC)
			}
			if req.LoaderType != testCase.wantLoader {
				t.Errorf("加载器类型 = %d，期望 %d", req.LoaderType, testCase.wantLoader)
			}
			if req.LoaderVersion != testCase.wantVersion {
				t.Errorf("加载器版本 = %q，期望 %q", req.LoaderVersion, testCase.wantVersion)
			}
			if req.RawLoaderKey != testCase.wantRawKey {
				t.Errorf("原始加载器键 = %q，期望 %q", req.RawLoaderKey, testCase.wantRawKey)
			}
		})
	}
}

// TestInstallMultiMcStripsDotMinecraft 防的回归：MultiMC 包的游戏内容在
// .minecraft/ 下，此前只剥离 overrides/，导致内容被塞进实例里一个字面
// 的 .minecraft 子目录——"装完了但一个 mod 都没生效"。
func TestInstallMultiMcStripsDotMinecraft(t *testing.T) {
	archivePath := buildMultiMcZip(t,
		`{"formatVersion":1,"components":[{"uid":"net.minecraft","version":"1.20.1"},`+
			`{"uid":"net.minecraftforge","version":"47.2.0"}]}`,
		map[string]string{
			"mods/alpha.jar":        "fake-jar-alpha",
			"config/main.cfg":       "key=value",
			"saves/world/level.dat": "fake-level",
		})

	target := filepath.Join(t.TempDir(), "mmc-instance")
	result, err := download.InstallModpack(context.Background(), "", archivePath, target, nil)
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	if len(result.Errors) > 0 {
		t.Errorf("安装过程报错：%v", result.Errors)
	}

	// 内容必须落在实例根下（前缀已剥离）
	assertFileContent(t, target, "mods/alpha.jar", "fake-jar-alpha")
	assertFileContent(t, target, "config/main.cfg", "key=value")
	assertFileContent(t, target, "saves/world/level.dat", "fake-level")

	// 不得留下字面的 .minecraft 目录
	if _, err := os.Stat(filepath.Join(target, ".minecraft")); err == nil {
		t.Error(".minecraft 前缀应被剥离，不应在实例里留下该目录")
	}
	// 启动器元数据不得落入内容目录
	for _, banned := range []string{"mmc-pack.json", "instance.cfg"} {
		if _, err := os.Stat(filepath.Join(target, banned)); err == nil {
			t.Errorf("启动器元数据 %s 不应出现在内容目录", banned)
		}
	}

	// 声明的运行要求要能回填到安装结果（UI 据此展示目标版本）
	if result.DetectedFormat != "multimc" {
		t.Errorf("识别格式 = %q，期望 multimc", result.DetectedFormat)
	}
	if result.DeclaredMinecraftVersion != "1.20.1" {
		t.Errorf("声明 MC 版本 = %q，期望 1.20.1", result.DeclaredMinecraftVersion)
	}
	if result.DeclaredLoaderName != "Forge" || result.DeclaredLoaderVersion != "47.2.0" {
		t.Errorf("声明加载器 = %q %q，期望 Forge 47.2.0",
			result.DeclaredLoaderName, result.DeclaredLoaderVersion)
	}
}

// TestInstallMrpackReportsDeclaredRequirements 安装结果要带上声明的运行要求，
// 否则 UI 只能显示一个文件数，用户看不出装到了哪个版本上。
func TestInstallMrpackReportsDeclaredRequirements(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "declared.mrpack")
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

	addEntry("modrinth.index.json", `{"formatVersion":1,"game":"minecraft",`+
		`"versionId":"1.0.0","name":"声明包",`+
		`"dependencies":{"minecraft":"1.21.1","fabric-loader":"0.16.9"},"files":[]}`)
	addEntry("overrides/config/x.cfg", "y=1")
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()

	target := filepath.Join(t.TempDir(), "declared-instance")
	result, err := download.InstallModpack(context.Background(), "", archivePath, target, nil)
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	if result.DetectedFormat != "modrinth" {
		t.Errorf("识别格式 = %q，期望 modrinth", result.DetectedFormat)
	}
	if result.DeclaredMinecraftVersion != "1.21.1" {
		t.Errorf("声明 MC 版本 = %q，期望 1.21.1", result.DeclaredMinecraftVersion)
	}
	if result.DeclaredLoaderName != "Fabric" || result.DeclaredLoaderVersion != "0.16.9" {
		t.Errorf("声明加载器 = %q %q，期望 Fabric 0.16.9",
			result.DeclaredLoaderName, result.DeclaredLoaderVersion)
	}
	// 受支持的加载器不应产生"装不了"的告警（快照等无关提示不在此列）
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "暂不受支持") {
			t.Errorf("受支持的加载器不应有'不受支持'告警：%s", warning)
		}
	}
}

// TestReadRequirementsIgnoresNestedMmcPack 防的回归：overrides/ 里可能带着
// 实例自己的 mmc-pack.json（不是本包的声明），必须只认包根的那份。
func TestReadRequirementsIgnoresNestedMmcPack(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "nested.zip")
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

	// 只有嵌套的那份（来自某个实例目录的残留），包根本身没有任何清单
	addEntry("overrides/mmc-pack.json",
		`{"components":[{"uid":"net.minecraft","version":"1.7.10"}]}`)
	addEntry("overrides/mods/x.jar", "fake")
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()

	req, err := download.ReadModpackRequirements(context.Background(), archivePath)
	if err != nil {
		t.Fatalf("读取要求失败：%v", err)
	}
	if req != nil {
		t.Errorf("嵌套的 mmc-pack.json 不应被当作本包声明，实际解析出 %+v", req)
	}
}

// ---------------------------------------------------------------------------
// mrpack 规范：client-overrides / server-overrides / env / 哈希校验
// ---------------------------------------------------------------------------

// TestInstallAppliesClientOverrides 防的回归：mrpack 的 client-overrides/
// 此前完全不处理，其内容会落成字面的 "client-overrides/…" 目录（mod 装不上）；
// 且 overrides 与 client-overrides 是层叠关系（后者覆盖前者），必须按层解压。
func TestInstallAppliesClientOverrides(t *testing.T) {
	archivePath := writeZip(t, "layered.mrpack", map[string]string{
		"modrinth.index.json": `{"formatVersion":1,"game":"minecraft","name":"分层包",` +
			`"versionId":"1.0.0","dependencies":{"minecraft":"1.21.1"},"files":[]}`,
		// 同名文件：client-overrides 必须覆盖 overrides
		"overrides/config/app.cfg":        "base-value",
		"client-overrides/config/app.cfg": "client-wins",
		// 只在 client-overrides 里存在的文件
		"client-overrides/mods/clientonly.jar": "client-only",
		// 服务端专用：客户端必须跳过
		"server-overrides/mods/serveronly.jar": "server-only",
	})

	target := filepath.Join(t.TempDir(), "layered-instance")
	result, err := download.InstallModpack(context.Background(), "", archivePath, target, nil)
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	if len(result.Errors) > 0 {
		t.Errorf("安装过程报错：%v", result.Errors)
	}

	// client-overrides 覆盖 overrides 的同名文件
	assertFileContent(t, target, "config/app.cfg", "client-wins")
	// client-overrides 独有的文件要落到实例根下
	assertFileContent(t, target, "mods/clientonly.jar", "client-only")

	// 不得留下字面的前缀目录
	for _, banned := range []string{"overrides", "client-overrides", "server-overrides"} {
		if _, err := os.Stat(filepath.Join(target, banned)); err == nil {
			t.Errorf("前缀目录 %s 应被剥离，不应出现在实例里", banned)
		}
	}
	// server-overrides 的内容不得装进客户端
	if _, err := os.Stat(filepath.Join(target, "mods", "serveronly.jar")); err == nil {
		t.Error("server-overrides 是服务端专用，不应装进客户端")
	}
}

// TestInstallLayeringIndependentOfZipOrder 防的回归：层叠解压不能顺着 zip
// 条目顺序，否则同一文件在两处出现时谁最后写入取决于压缩包内部顺序。
// writeZip 用 map（随机顺序），这里多跑几轮确认结果稳定。
func TestInstallLayeringIndependentOfZipOrder(t *testing.T) {
	for round := 0; round < 8; round++ {
		archivePath := writeZip(t, "order.mrpack", map[string]string{
			"modrinth.index.json": `{"formatVersion":1,"game":"minecraft","name":"顺序包",` +
				`"versionId":"1.0.0","dependencies":{"minecraft":"1.21.1"},"files":[]}`,
			"overrides/options.txt":        "overrides-losses",
			"client-overrides/options.txt": "client-losses",
		})

		target := filepath.Join(t.TempDir(), "order-instance")
		if _, err := download.InstallModpack(context.Background(), "", archivePath, target, nil); err != nil {
			t.Fatalf("安装失败：%v", err)
		}
		assertFileContent(t, target, "options.txt", "client-losses")
	}
}

// TestInstallSkipsClientUnsupportedFiles 防的回归：mrpack 的 env 此前被完全
// 忽略，env.client == "unsupported" 的服务端专用文件仍会被下载安装到客户端。
func TestInstallSkipsClientUnsupportedFiles(t *testing.T) {
	clientContent := "client-mod-content"
	serverContent := "server-mod-content"

	clientHits := 0
	serverHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/clientmod.jar":
			clientHits++
			_, _ = w.Write([]byte(clientContent))
		case "/servermod.jar":
			serverHits++
			_, _ = w.Write([]byte(serverContent))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	archivePath := writeZip(t, "env.mrpack", map[string]string{
		"modrinth.index.json": `{"formatVersion":1,"game":"minecraft","name":"环境包",` +
			`"versionId":"1.0.0","dependencies":{"minecraft":"1.21.1"},"files":[` +
			`{"path":"mods/clientmod.jar","hashes":{"sha1":"` + sha1Hex(clientContent) + `"},` +
			`"env":{"client":"required","server":"unsupported"},` +
			`"downloads":["` + server.URL + `/clientmod.jar"]},` +
			`{"path":"mods/servermod.jar","hashes":{"sha1":"` + sha1Hex(serverContent) + `"},` +
			`"env":{"client":"unsupported","server":"required"},` +
			`"downloads":["` + server.URL + `/servermod.jar"]}]}`,
	})

	target := filepath.Join(t.TempDir(), "env-instance")
	result, err := download.InstallModpack(context.Background(), "", archivePath, target, nil)
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}

	// 客户端文件正常安装
	assertFileContent(t, target, "mods/clientmod.jar", clientContent)
	if result.DownloadedMods != 1 {
		t.Errorf("下载数 = %d，期望 1（只装客户端文件）", result.DownloadedMods)
	}
	// 服务端专用文件既不该被下载，也不该落盘
	if serverHits != 0 {
		t.Errorf("env.client=unsupported 的文件不应被请求，实际请求 %d 次", serverHits)
	}
	if _, err := os.Stat(filepath.Join(target, "mods", "servermod.jar")); err == nil {
		t.Error("env.client=unsupported 的文件不应落盘")
	}
	if clientHits != 1 {
		t.Errorf("客户端文件请求次数 = %d，期望 1", clientHits)
	}
}

// TestInstallVerifiesSHA512OnlyFile 防的回归：清单只写 sha512（不写 sha1）时，
// 此前因为只读 SHA-1 而完全不做校验；两者都写时也只比了 SHA-1。
func TestInstallVerifiesSHA512OnlyFile(t *testing.T) {
	t.Run("只声明 sha512 且不匹配则报错并删除", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("actual-content"))
		}))
		defer server.Close()

		archivePath := writeZip(t, "sha512only.mrpack", map[string]string{
			"modrinth.index.json": `{"formatVersion":1,"game":"minecraft","name":"sha512包",` +
				`"versionId":"1.0.0","dependencies":{"minecraft":"1.21.1"},"files":[` +
				`{"path":"mods/s.jar","hashes":{"sha512":"` + sha512Hex("expected-content") + `"},` +
				`"downloads":["` + server.URL + `/s.jar"]}]}`,
		})

		target := filepath.Join(t.TempDir(), "sha512only-instance")
		result, err := download.InstallModpack(context.Background(), "", archivePath, target, nil)
		if err != nil {
			t.Fatalf("安装失败：%v", err)
		}
		joined := strings.Join(result.Errors, " ")
		if !strings.Contains(joined, "SHA-512") {
			t.Errorf("只有 sha512 时也必须校验并报错，实际错误：%v", result.Errors)
		}
		if _, err := os.Stat(filepath.Join(target, "mods", "s.jar")); err == nil {
			t.Error("SHA-512 不匹配的文件应被删除")
		}
	})

	t.Run("只声明 sha512 且匹配则安装成功", func(t *testing.T) {
		content := "sha512-ok-content"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(content))
		}))
		defer server.Close()

		archivePath := writeZip(t, "sha512ok.mrpack", map[string]string{
			"modrinth.index.json": `{"formatVersion":1,"game":"minecraft","name":"sha512好包",` +
				`"versionId":"1.0.0","dependencies":{"minecraft":"1.21.1"},"files":[` +
				`{"path":"mods/s.jar","hashes":{"sha512":"` + sha512Hex(content) + `"},` +
				`"downloads":["` + server.URL + `/s.jar"]}]}`,
		})

		target := filepath.Join(t.TempDir(), "sha512ok-instance")
		result, err := download.InstallModpack(context.Background(), "", archivePath, target, nil)
		if err != nil {
			t.Fatalf("安装失败：%v", err)
		}
		if len(result.Errors) > 0 {
			t.Errorf("SHA-512 匹配不应报错：%v", result.Errors)
		}
		if result.DownloadedMods != 1 {
			t.Errorf("下载数 = %d，期望 1", result.DownloadedMods)
		}
		assertFileContent(t, target, "mods/s.jar", content)
	})
}

// TestInstallIgnoresMalformedDeclaredHash 防的回归：清单里的哈希可能是占位符
// 或写坏的值（长度不对、非十六进制）。把它当有效哈希去比对，会把一个本来
// 正确的下载判成"校验失败"并删掉——所以只校验格式正确的哈希。
func TestInstallIgnoresMalformedDeclaredHash(t *testing.T) {
	content := "good-content"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()

	archivePath := writeZip(t, "placeholder.mrpack", map[string]string{
		"modrinth.index.json": `{"formatVersion":1,"game":"minecraft","name":"占位哈希包",` +
			`"versionId":"1.0.0","dependencies":{"minecraft":"1.21.1"},"files":[` +
			`{"path":"mods/p.jar","hashes":{"sha1":"` + sha1Hex(content) + `","sha512":"ignored"},` +
			`"downloads":["` + server.URL + `/p.jar"]}]}`,
	})

	target := filepath.Join(t.TempDir(), "placeholder-instance")
	result, err := download.InstallModpack(context.Background(), "", archivePath, target, nil)
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	if len(result.Errors) > 0 {
		t.Errorf("无效的 sha512 占位符不应让正确的下载失败：%v", result.Errors)
	}
	if result.DownloadedMods != 1 {
		t.Errorf("下载数 = %d，期望 1", result.DownloadedMods)
	}
	// 有效的 sha1 仍然要比对通过，文件必须留下
	assertFileContent(t, target, "mods/p.jar", content)
}

// TestInstallVerifiesDeclaredHashes 防的回归：mrpack 为每个声明文件都带 sha1，
// 安装侧此前从不校验——下到残缺或被替换的内容也会被当作正常安装。
func TestInstallVerifiesDeclaredHashes(t *testing.T) {
	t.Run("哈希不匹配则报错且不留文件", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("actual-content"))
		}))
		defer server.Close()

		archivePath := writeZip(t, "badhash.mrpack", map[string]string{
			"modrinth.index.json": `{"formatVersion":1,"game":"minecraft","name":"坏包",` +
				`"versionId":"1.0.0","dependencies":{"minecraft":"1.21.1"},"files":[` +
				`{"path":"mods/good.jar","hashes":{"sha1":"` + sha1Hex("expected-content") + `"},` +
				`"downloads":["` + server.URL + `/good.jar"]}]}`,
		})

		target := filepath.Join(t.TempDir(), "badhash-instance")
		result, err := download.InstallModpack(context.Background(), "", archivePath, target, nil)
		if err != nil {
			t.Fatalf("安装失败：%v", err)
		}
		if len(result.Errors) == 0 {
			t.Fatal("哈希不匹配必须报错，实际无错误")
		}
		if !strings.Contains(strings.Join(result.Errors, " "), "SHA-1") {
			t.Errorf("错误信息应说明哈希校验失败：%v", result.Errors)
		}
		if result.DownloadedMods != 0 {
			t.Errorf("校验失败不应计入成功下载，实际 %d", result.DownloadedMods)
		}
		// 损坏的文件必须删掉：留着会被游戏当正常 mod 加载
		if _, err := os.Stat(filepath.Join(target, "mods", "good.jar")); err == nil {
			t.Error("哈希不匹配的文件应被删除，不应留在实例里")
		}
	})

	t.Run("哈希匹配则正常安装", func(t *testing.T) {
		content := "verified-content"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(content))
		}))
		defer server.Close()

		archivePath := writeZip(t, "goodhash.mrpack", map[string]string{
			"modrinth.index.json": `{"formatVersion":1,"game":"minecraft","name":"好包",` +
				`"versionId":"1.0.0","dependencies":{"minecraft":"1.21.1"},"files":[` +
				`{"path":"mods/good.jar","hashes":{"sha1":"` + sha1Hex(content) + `"},` +
				`"downloads":["` + server.URL + `/good.jar"]}]}`,
		})

		target := filepath.Join(t.TempDir(), "goodhash-instance")
		result, err := download.InstallModpack(context.Background(), "", archivePath, target, nil)
		if err != nil {
			t.Fatalf("安装失败：%v", err)
		}
		if len(result.Errors) > 0 {
			t.Errorf("哈希匹配不应报错：%v", result.Errors)
		}
		if result.DownloadedMods != 1 {
			t.Errorf("下载数 = %d，期望 1", result.DownloadedMods)
		}
		assertFileContent(t, target, "mods/good.jar", content)
	})
}

// TestInstallDoesNotRedownloadClientOverridesFile 防的回归：包内已带在
// client-overrides/ 下的文件，不应被误判为缺失而联网重下。
func TestInstallDoesNotRedownloadClientOverridesFile(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte("from-network"))
	}))
	defer server.Close()

	archivePath := writeZip(t, "bundled.mrpack", map[string]string{
		"modrinth.index.json": `{"formatVersion":1,"game":"minecraft","name":"自带包",` +
			`"versionId":"1.0.0","dependencies":{"minecraft":"1.21.1"},"files":[` +
			`{"path":"mods/bundled.jar","hashes":{"sha1":"` + sha1Hex("bundled-content") + `"},` +
			`"downloads":["` + server.URL + `/bundled.jar"]}]}`,
		"client-overrides/mods/bundled.jar": "bundled-content",
	})

	target := filepath.Join(t.TempDir(), "bundled-instance")
	result, err := download.InstallModpack(context.Background(), "", archivePath, target, nil)
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	if len(result.Errors) > 0 {
		t.Errorf("安装过程报错：%v", result.Errors)
	}
	if result.DownloadedMods != 0 {
		t.Errorf("包内已带的文件不应计入下载，实际 %d", result.DownloadedMods)
	}
	if hits != 0 {
		t.Errorf("包内已带的文件不应触发下载，实际请求 %d 次", hits)
	}
	assertFileContent(t, target, "mods/bundled.jar", "bundled-content")
}
