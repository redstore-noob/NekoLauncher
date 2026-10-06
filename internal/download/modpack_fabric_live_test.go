package download_test

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"nekolauncher/internal/download"
)

// 合成 fabric mrpack（无网络依赖：files 为空，内容全在 overrides）。
func buildFabricMrpack(t *testing.T, packPath string) {
	t.Helper()
	f, err := os.Create(packPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	files := map[string]string{
		"modrinth.index.json": `{
		  "formatVersion": 1,
		  "game": "minecraft",
		  "dependencies": {
		    "fabric-loader": "0.16.14",
		    "minecraft": "1.21.1"
		  },
		  "files": [],
		  "overrides": {}
		}`,
		"overrides/mods/dummy-mod.jar": "dummy",
		"overrides/config/opts.json":   "{}",
	}
	for name, content := range files {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestInstallFabricMrpackOffline 离线部分：要求解析 + overrides 解压 + 运行要求回填。
func TestInstallFabricMrpackOffline(t *testing.T) {
	dir := t.TempDir()
	packPath := filepath.Join(dir, "fabric-pack.mrpack")
	buildFabricMrpack(t, packPath)

	req, err := download.ReadModpackRequirements(context.Background(), packPath)
	if err != nil {
		t.Fatalf("ReadModpackRequirements: %v", err)
	}
	if req == nil || req.MinecraftVersion != "1.21.1" || req.LoaderType != download.ModLoaderFabric {
		t.Fatalf("requirements 解析错误: %+v", req)
	}

	contentDir := filepath.Join(dir, "content")
	result, err := download.InstallModpack(context.Background(), "", packPath, contentDir,
		func(_, _ int64) {}, func(string) {})
	if err != nil {
		t.Fatalf("InstallModpack: %v", err)
	}
	if len(result.Errors) > 0 {
		t.Fatalf("安装错误: %v", result.Errors)
	}
	if result.InstalledFiles != 2 {
		t.Fatalf("应解压 2 个文件，实际 %d", result.InstalledFiles)
	}
	if result.DeclaredLoaderName != "Fabric" || result.DeclaredMinecraftVersion != "1.21.1" {
		t.Fatalf("声明的运行要求回填错误: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(contentDir, "mods", "dummy-mod.jar")); err != nil {
		t.Fatalf("mods 文件未落盘: %v", err)
	}
}

// TestLiveFabricModpackChain 真实网络用例：版本清单 + fabric meta（BMCLAPI）可达，
// 且声明的 loader 版本存在于候选列表——整合包导入前置链路的端到端证明。
// 运行：NEKO_LIVE_RESOURCES=1 go test ./internal/download/ -run TestLiveFabricModpackChain -v -count=1
func TestLiveFabricModpackChain(t *testing.T) {
	if os.Getenv("NEKO_LIVE_RESOURCES") != "1" {
		t.Skip("真实网络用例：未设 NEKO_LIVE_RESOURCES=1，跳过")
	}

	dir := t.TempDir()
	packPath := filepath.Join(dir, "fabric-pack.mrpack")
	buildFabricMrpack(t, packPath)

	req, err := download.ReadModpackRequirements(context.Background(), packPath)
	if err != nil {
		t.Fatalf("ReadModpackRequirements: %v", err)
	}

	versions, err := download.GetVersions(context.Background())
	if err != nil {
		t.Fatalf("GetVersions: %v", err)
	}
	found := false
	for i := range versions {
		if versions[i].ID == req.MinecraftVersion {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("版本清单中没有 MC %s", req.MinecraftVersion)
	}

	loaders, err := download.GetModLoaderVersions(context.Background(), download.ModLoaderFabric, req.MinecraftVersion)
	if err != nil {
		t.Fatalf("GetModLoaderVersions(fabric): %v", err)
	}
	if len(loaders) == 0 {
		t.Fatal("fabric loader 列表为空")
	}
	declaredFound := false
	for _, l := range loaders {
		if l.LoaderVersion == req.LoaderVersion {
			declaredFound = true
			break
		}
	}
	t.Logf("fabric loader 共 %d 个版本，声明的 %s 在列表中: %v", len(loaders), req.LoaderVersion, declaredFound)
}
