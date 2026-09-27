package online

import (
	"archive/tar"
	"compress/gzip"
	"strings"
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestPickTerracottaAsset 按平台/架构挑资产：不能选到别的平台、不能选校验文件。
func TestPickTerracottaAsset(t *testing.T) {
	assets := []terracottaReleaseAsset{
		{Name: "terracotta-1.2.0-x86_64-pc-windows-msvc.zip", URL: "https://example.com/win.zip", Size: 100},
		{Name: "terracotta-1.2.0-aarch64-pc-windows-msvc.zip", URL: "https://example.com/win-arm.zip"},
		{Name: "terracotta-1.2.0-x86_64-unknown-linux-gnu.zip", URL: "https://example.com/linux.zip"},
		{Name: "terracotta-1.2.0-aarch64-apple-darwin.zip", URL: "https://example.com/mac.zip"},
		{Name: "SHA256SUMS.txt", URL: "https://example.com/sums"},
		{Name: "terracotta-1.2.0-x86_64-pc-windows-msvc.zip.sha256", URL: "https://example.com/win.sha256"},
	}

	cases := []struct {
		goos   string
		goarch string
		want   string
	}{
		{goos: "windows", goarch: "amd64", want: "terracotta-1.2.0-x86_64-pc-windows-msvc.zip"},
		{goos: "windows", goarch: "arm64", want: "terracotta-1.2.0-aarch64-pc-windows-msvc.zip"},
		{goos: "linux", goarch: "amd64", want: "terracotta-1.2.0-x86_64-unknown-linux-gnu.zip"},
		{goos: "darwin", goarch: "arm64", want: "terracotta-1.2.0-aarch64-apple-darwin.zip"},
	}

	for _, testCase := range cases {
		asset, err := pickTerracottaAsset(assets, testCase.goos, testCase.goarch)
		if err != nil {
			t.Fatalf("%s/%s 挑选失败：%v", testCase.goos, testCase.goarch, err)
		}
		if asset.Name != testCase.want {
			t.Fatalf("%s/%s 选中 %q，期望 %q", testCase.goos, testCase.goarch, asset.Name, testCase.want)
		}
	}

	// 没有匹配平台时明确报错（而不是随便给一个）
	if _, err := pickTerracottaAsset([]terracottaReleaseAsset{
		{Name: "terracotta-1.2.0-x86_64-unknown-linux-gnu.zip"},
	}, "windows", "amd64"); err == nil {
		t.Fatal("没有匹配资产时应报错")
	}

	// 架构不匹配也不能退而求其次（x86_64 包不能给 arm64 用）
	if _, err := pickTerracottaAsset([]terracottaReleaseAsset{
		{Name: "terracotta-x86_64-pc-windows-msvc.zip"},
	}, "windows", "arm64"); err == nil {
		t.Fatal("架构不匹配时应报错")
	}

	// 空列表报错
	if _, err := pickTerracottaAsset(nil, "windows", "amd64"); err == nil {
		t.Fatal("空资产列表应报错")
	}
}

// TestPickTerracottaAssetRealNames 用 v0.4.2 的真实资产名校验三端挑选：
// 发行包命名是 terracotta-<版本>-<os>-<arch>-pkg.tar.gz，另有 Android 的 .so 干扰项。
func TestPickTerracottaAssetRealNames(t *testing.T) {
	assets := []terracottaReleaseAsset{
		{Name: "terracotta-0.4.2-android-arm64v8a.so"},
		{Name: "terracotta-0.4.2-android-armv7.so"},
		{Name: "terracotta-0.4.2-android-x86.so"},
		{Name: "terracotta-0.4.2-android-x86_64.so"},
		{Name: "terracotta-0.4.2-freebsd-x86_64-pkg.tar.gz"},
		{Name: "terracotta-0.4.2-linux-arm64-pkg.tar.gz"},
		{Name: "terracotta-0.4.2-linux-loongarch64-pkg.tar.gz"},
		{Name: "terracotta-0.4.2-linux-riscv64-pkg.tar.gz"},
		{Name: "terracotta-0.4.2-linux-x86_64-pkg.tar.gz"},
		{Name: "terracotta-0.4.2-macos-arm64-pkg.tar.gz"},
		{Name: "terracotta-0.4.2-macos-x86_64-pkg.tar.gz"},
		{Name: "terracotta-0.4.2-windows-arm64-pkg.tar.gz"},
		{Name: "terracotta-0.4.2-windows-x86_64-pkg.tar.gz"},
	}

	cases := []struct {
		goos   string
		goarch string
		want   string
	}{
		{goos: "windows", goarch: "amd64", want: "terracotta-0.4.2-windows-x86_64-pkg.tar.gz"},
		{goos: "windows", goarch: "arm64", want: "terracotta-0.4.2-windows-arm64-pkg.tar.gz"},
		{goos: "linux", goarch: "amd64", want: "terracotta-0.4.2-linux-x86_64-pkg.tar.gz"},
		{goos: "linux", goarch: "arm64", want: "terracotta-0.4.2-linux-arm64-pkg.tar.gz"},
		{goos: "darwin", goarch: "amd64", want: "terracotta-0.4.2-macos-x86_64-pkg.tar.gz"},
		{goos: "darwin", goarch: "arm64", want: "terracotta-0.4.2-macos-arm64-pkg.tar.gz"},
	}

	for _, testCase := range cases {
		asset, err := pickTerracottaAsset(assets, testCase.goos, testCase.goarch)
		if err != nil {
			t.Fatalf("%s/%s 挑选失败：%v", testCase.goos, testCase.goarch, err)
		}
		if asset.Name != testCase.want {
			t.Fatalf("%s/%s 选中 %q，期望 %q", testCase.goos, testCase.goarch, asset.Name, testCase.want)
		}
	}
}

// TestAssetPlatformScore 打分规则：校验文件/源码包一律排除，zip 优先于 tar.gz。
func TestAssetPlatformScore(t *testing.T) {
	if score := assetPlatformScore("terracotta.zip.sha256", "windows", "amd64"); score != -1 {
		t.Fatalf("校验文件应被排除，得分 %d", score)
	}
	if score := assetPlatformScore("source.zip", "windows", "amd64"); score != -1 {
		t.Fatalf("源码包应被排除，得分 %d", score)
	}

	zipScore := assetPlatformScore("terracotta-x86_64-pc-windows-msvc.zip", "windows", "amd64")
	tarScore := assetPlatformScore("terracotta-x86_64-apple-darwin.tar.gz", "darwin", "amd64")
	if zipScore <= 0 || tarScore <= 0 {
		t.Fatalf("正常资产应为正分：zip=%d tar=%d", zipScore, tarScore)
	}
}

// TestExtractTerracottaArchive 解压：正常解出、越界路径被拒绝、保留可执行权限。
func TestExtractTerracottaArchive(t *testing.T) {
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "terracotta.zip")
	createTestZip(t, archivePath, map[string]string{
		"terracotta/terracotta.exe": "binary",
		"terracotta/README.md":      "docs",
	})

	target := filepath.Join(directory, "out")
	if err := extractTerracottaArchive(archivePath, target); err != nil {
		t.Fatalf("解压失败：%v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(target, "terracotta", "terracotta.exe"))
	if readErr != nil || string(data) != "binary" {
		t.Fatalf("解压内容不符：%v / %q", readErr, data)
	}

	// 越界路径
	evil := filepath.Join(directory, "evil.zip")
	createTestZip(t, evil, map[string]string{"../escaped.txt": "boom"})
	if err := extractTerracottaArchive(evil, filepath.Join(directory, "out2")); err == nil {
		t.Fatal("越界路径应被拒绝")
	}
}

// TestLocateTerracottaBinary 可执行文件查找（深浅不限、名字不区分大小写）。
func TestLocateTerracottaBinary(t *testing.T) {
	directory := t.TempDir()
	expected := terracottaBinaryNames()[0]

	nested := filepath.Join(directory, "terracotta-1.2.0", "bin")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, expected), []byte("bin"), 0o755); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}

	found, err := locateTerracottaBinary(directory)
	if err != nil {
		t.Fatalf("查找失败：%v", err)
	}
	if filepath.Base(found) != expected {
		t.Fatalf("找到 %q，期望 %q", found, expected)
	}

	// 找不到时给出期望名字的提示
	if _, err := locateTerracottaBinary(t.TempDir()); err == nil {
		t.Fatal("空目录应报错")
	}
}

// TestLocateTerracottaBinaryVersioned 实测 v0.4.2 三平台包内布局：带版本号文件名，
// Windows 带 VCRUNTIME DLL，macOS 带同名 .pkg 安装包分发物（绝不能选中它）。
func TestLocateTerracottaBinaryVersioned(t *testing.T) {
	directory := t.TempDir()

	files := []string{
		"terracotta-0.4.2-windows-x86_64.exe",
		"terracotta-0.4.2-windows-arm64.exe",
		"VCRUNTIME140.DLL",
		"terracotta-0.4.2-macos-arm64",
		"terracotta-0.4.2-macos-arm64.pkg",
		"terracotta-0.4.2-macos-x86_64",
		"terracotta-0.4.2-linux-x86_64",
		"terracotta-0.4.2-linux-arm64",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("bin"), 0o755); err != nil {
			t.Fatalf("写文件失败：%v", err)
		}
	}

	// 期望按 GOOS/GOARCH 组合给出对应平台的文件
	expected := map[string]string{
		"windows/amd64": "terracotta-0.4.2-windows-x86_64.exe",
		"windows/arm64": "terracotta-0.4.2-windows-arm64.exe",
		"linux/amd64":   "terracotta-0.4.2-linux-x86_64",
		"linux/arm64":   "terracotta-0.4.2-linux-arm64",
		"darwin/amd64":  "terracotta-0.4.2-macos-x86_64",
		"darwin/arm64":  "terracotta-0.4.2-macos-arm64",
	}[runtime.GOOS + "/" + runtime.GOARCH]
	if expected == "" {
		t.Skipf("未覆盖的平台组合 %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	found, err := locateTerracottaBinary(directory)
	if err != nil {
		t.Fatalf("查找失败：%v", err)
	}
	if filepath.Base(found) != expected {
		t.Fatalf("选中 %q，期望 %q", filepath.Base(found), expected)
	}
}

// createTestZip 造一个测试用 zip。
func createTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("建压缩包失败：%v", err)
	}
	writer := zip.NewWriter(file)
	for name, content := range files {
		entry, createErr := writer.Create(name)
		if createErr != nil {
			t.Fatalf("写条目失败：%v", createErr)
		}
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatalf("写内容失败：%v", writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭失败：%v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("关闭失败：%v", err)
	}
}

// TestExtractTerracottaTarGz 解压 tar.gz（实测发行包用的就是这个格式）。
func TestExtractTerracottaTarGz(t *testing.T) {
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "terracotta-0.4.2-windows-x86_64-pkg.tar.gz")
	createTestTarGz(t, archivePath, map[string]string{
		"terracotta/terracotta.exe": "binary",
		"terracotta/assets/note.txt": "hello",
	})

	target := filepath.Join(directory, "out")
	if err := extractTerracottaArchive(archivePath, target); err != nil {
		t.Fatalf("解压 tar.gz 失败：%v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(target, "terracotta", "terracotta.exe"))
	if readErr != nil || string(data) != "binary" {
		t.Fatalf("解压内容不符：%v / %q", readErr, data)
	}
	// 解出后应当能直接定位到可执行文件
	found, locateErr := locateTerracottaBinary(target)
	if locateErr != nil {
		t.Fatalf("解压后未找到可执行文件：%v", locateErr)
	}
	if filepath.Base(found) != terracottaBinaryNames()[0] {
		t.Fatalf("定位到 %q", found)
	}
}

// TestExtractTerracottaUnsupported 不认识的格式要明确报错（而不是静默成功）。
func TestExtractTerracottaUnsupported(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "terracotta.dmg")
	if err := os.WriteFile(path, []byte("dmg"), 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	if err := extractTerracottaArchive(path, filepath.Join(directory, "out")); err == nil {
		t.Fatal("不支持的格式应报错")
	}
}

// createTestTarGz 造一个测试用 tar.gz。
func createTestTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("建压缩包失败：%v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)

	for name, content := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(content)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("写头失败：%v", err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatalf("写内容失败：%v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("关闭 tar 失败：%v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("关闭 gzip 失败：%v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("关闭文件失败：%v", err)
	}
}

// TestInstallTerracottaLive 真连 GitHub API 校验"能挑出当前平台的资产"。
// 默认跳过（需要网络与外网），设置 NEKO_LIVE_TERRACOTTA=1 才跑。
func TestInstallTerracottaLive(t *testing.T) {
	if os.Getenv("NEKO_LIVE_TERRACOTTA") == "" {
		t.Skip("未设置 NEKO_LIVE_TERRACOTTA，跳过在线校验")
	}

	release, err := fetchTerracottaRelease(t.Context())
	if err != nil {
		t.Fatalf("拉取 release 失败：%v", err)
	}
	if len(release.Assets) == 0 {
		t.Fatal("release 里没有资产")
	}
	for _, asset := range release.Assets {
		t.Logf("资产：%s（%d 字节）", asset.Name, asset.Size)
	}

	asset, pickErr := pickTerracottaAsset(release.Assets, runtime.GOOS, runtime.GOARCH)
	if pickErr != nil {
		// 上游确实可能没有本平台资产（例如 macOS 只有 dmg），这不算失败
		t.Skipf("%s/%s 没有可用资产：%v", runtime.GOOS, runtime.GOARCH, pickErr)
	}
	t.Logf("release %s → 选中 %s", release.TagName, asset.Name)

	// 选中的资产必须是启动器真能解开的格式，否则"自动安装"就是假的
	lower := strings.ToLower(asset.Name)
	if !strings.HasSuffix(lower, ".zip") && !strings.HasSuffix(lower, ".tar.gz") &&
		!strings.HasSuffix(lower, ".tgz") {
		t.Skipf("选中 %s：该平台需手动解压（前端会给出提示）", asset.Name)
	}
}
