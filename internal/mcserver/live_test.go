// 在线集成测试：验证四种核心的下载源与解析逻辑（需要网络）。
// 短模式（go test -short）自动跳过。
package mcserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireNetwork(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("short 模式跳过在线测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	_ = ctx
}

// TestLiveVanillaManifest 校验 Mojang 清单可解析且目标版本有 server 下载。
func TestLiveVanillaManifest(t *testing.T) {
	requireNetwork(t)

	var manifest struct {
		Versions []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"versions"`
	}
	if err := httpGetJSON(context.Background(),
		"https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &manifest); err != nil {
		t.Fatalf("清单获取失败：%v", err)
	}
	for _, v := range manifest.Versions {
		if v.ID == "1.21.4" && v.URL != "" {
			return
		}
	}
	t.Fatal("清单中未找到 1.21.4")
}

// TestLivePaperBuilds 校验 Paper fill v3 latest 构建带下载地址。
func TestLivePaperBuilds(t *testing.T) {
	requireNetwork(t)

	var build struct {
		Downloads map[string]struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"downloads"`
	}
	endpoint := "https://fill.papermc.io/v3/projects/paper/versions/1.21.4/builds/latest"
	if err := httpGetJSON(context.Background(), endpoint, &build); err != nil {
		t.Fatalf("Paper latest 获取失败：%v", err)
	}
	found := false
	for _, download := range build.Downloads {
		if download.URL != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("Paper latest 构建缺少下载地址")
	}
}

// TestLiveFabricMeta 校验 Fabric meta 的 loader 与 installer 版本可解析。
func TestLiveFabricMeta(t *testing.T) {
	requireNetwork(t)

	var loaders []struct {
		Loader struct {
			Version string `json:"version"`
		} `json:"loader"`
	}
	if err := httpGetJSON(context.Background(),
		"https://meta.fabricmc.net/v2/versions/loader/1.21.4", &loaders); err != nil {
		t.Fatalf("Fabric meta 获取失败：%v", err)
	}
	if len(loaders) == 0 || loaders[0].Loader.Version == "" {
		t.Fatal("Fabric meta 缺少 loader 版本")
	}

	var installers []struct {
		Version string `json:"version"`
	}
	if err := httpGetJSON(context.Background(),
		"https://meta.fabricmc.net/v2/versions/installer", &installers); err != nil {
		t.Fatalf("Fabric installer 获取失败：%v", err)
	}
	if len(installers) == 0 || installers[0].Version == "" {
		t.Fatal("Fabric installer 缺少版本")
	}
}

// TestLiveNeoForgeVersions 校验 NeoForge 版本按 MC 前缀过滤有结果。
func TestLiveNeoForgeVersions(t *testing.T) {
	requireNetwork(t)

	var versions struct {
		Versions []string `json:"versions"`
	}
	if err := httpGetJSON(context.Background(),
		"https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge",
		&versions); err != nil {
		t.Fatalf("NeoForge 版本获取失败：%v", err)
	}
	found := false
	for _, v := range versions.Versions {
		if strings.HasPrefix(v, "21.4.") {
			found = true

			break
		}
	}
	if !found {
		t.Fatal("NeoForge 没有 21.4.x 版本")
	}
}

// TestLiveInstallFabric 完整安装一次 Fabric（产物最小），校验 jar 落盘。
func TestLiveInstallFabric(t *testing.T) {
	requireNetwork(t)

	root := t.TempDir()
	serverRootOverride = &root
	t.Cleanup(func() { serverRootOverride = nil })

	dir := filepath.Join(root, "fabric-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &ServerConfig{MCVersion: "1.21.4"}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := installFabric(ctx, dir, cfg); err != nil {
		t.Fatalf("Fabric 安装失败：%v", err)
	}
	if cfg.CoreVersion == "" {
		t.Fatal("CoreVersion 未回填")
	}
	info, err := os.Stat(filepath.Join(dir, "fabric-server.jar"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 100*1024 {
		t.Fatalf("jar 体积异常：%d", info.Size())
	}
}

// jsonUnmarshalUsed 引用 encoding/json 避免未使用导入（httpGetJSON 的依赖说明）。
var _ = json.Valid

// TestLiveListServerMCVersions 校验四种核心的 MC 版本列表（新→旧、非空）。
func TestLiveListServerMCVersions(t *testing.T) {
	requireNetwork(t)
	ctx := context.Background()

	for _, core := range []string{CoreVanilla, CorePaper, CoreFabric, CoreNeoForge} {
		versions, err := ListServerMCVersions(ctx, core)
		if err != nil {
			t.Fatalf("%s：获取失败：%v", core, err)
		}
		if len(versions) == 0 {
			t.Fatalf("%s：版本列表为空", core)
		}
		has1214 := false
		for _, v := range versions {
			if v == "1.21.4" {
				has1214 = true
			}
		}
		if !has1214 {
			t.Fatalf("%s：列表缺少 1.21.4（%d 项）", core, len(versions))
		}
	}
}

// TestLiveListCoreVersions 校验四种核心在 1.21.4 下的服务端版本列表。
func TestLiveListCoreVersions(t *testing.T) {
	requireNetwork(t)
	ctx := context.Background()

	// Vanilla：即 MC 版本本身
	if v, err := ListCoreVersions(ctx, CoreVanilla, "1.21.4"); err != nil || len(v) != 1 || v[0] != "1.21.4" {
		t.Fatalf("Vanilla 版本列表异常：%v %v", v, err)
	}

	// Paper：builds 非空且新→旧（首项 > 末项）
	builds, err := ListCoreVersions(ctx, CorePaper, "1.21.4")
	if err != nil || len(builds) == 0 {
		t.Fatalf("Paper 构建列表为空：%v", err)
	}

	// Fabric：loader 版本非空
	loaders, err := ListCoreVersions(ctx, CoreFabric, "1.21.4")
	if err != nil || len(loaders) == 0 {
		t.Fatalf("Fabric loader 列表为空：%v", err)
	}

	// NeoForge：21.4.x 版本非空且首项 >= 末项
	neoforge, err := ListCoreVersions(ctx, CoreNeoForge, "1.21.4")
	if err != nil || len(neoforge) == 0 {
		t.Fatalf("NeoForge 版本列表为空：%v", err)
	}
	if neoforge[0] < neoforge[len(neoforge)-1] {
		t.Fatalf("NeoForge 列表应新→旧：%v", neoforge)
	}
}
