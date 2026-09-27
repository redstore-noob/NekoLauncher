package download_test

// 真实网络用例：默认 skip，只有显式设了 NEKO_LIVE_RESOURCES=1 才跑。
//
// 与包内的 httptest 用例互补：那些证明"逻辑对"，这条证明"真资源站 + 真下载
// 通道能端到端跑通"——搜索 → 版本 → 把主文件下载到临时目录（不碰用户数据）。
//
// 运行：NEKO_LIVE_RESOURCES=1 go test ./internal/download/ -run TestLiveResource -v -count=1

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nekolauncher/internal/download"
	"nekolauncher/internal/download/curseforge"
	"nekolauncher/internal/download/modrinth"
	"nekolauncher/internal/models"
)

// liveResourceSearchEnabled 是否允许打真实网络。
func liveResourceSearchEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("NEKO_LIVE_RESOURCES") != "1" {
		t.Skip("跳过真实网络用例：需要 NEKO_LIVE_RESOURCES=1")
	}
}

// TestLiveResourceSearchVersionsAndDownload 真连 Modrinth 走完整链路：
// 搜索 → 挑匹配当前实例的版本 → 下载主文件到 t.TempDir()。
//
// 只下小文件（<= 8 MB）：这条用例的目的是验证链路，不是压测 CDN。
func TestLiveResourceSearchVersionsAndDownload(t *testing.T) {
	liveResourceSearchEnabled(t)
	modrinth.ResetEndpoints()
	curseforge.ResetEndpoints()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	search, err := download.SearchResources(ctx, models.ResourceSearchRequest{
		Source:      models.ResourceSourceModrinth,
		ProjectType: models.ProjectTypeMod,
		Query:       "sodium",
		GameVersion: "1.21.1",
		Loader:      "fabric",
		Limit:       5,
	}, "")
	if err != nil {
		t.Fatalf("真实搜索失败：%v", err)
	}
	if len(search.Hits) == 0 {
		t.Fatal("真实搜索返回 0 条结果")
	}
	hit := search.Hits[0]
	t.Logf("搜索首条：%s（%s，%s，%s）", hit.Title, hit.ProjectID, hit.DownloadsDisplay, hit.PageURL)

	versions, err := download.ListResourceVersions(ctx, models.ResourceVersionRequest{
		Source:      models.ResourceSourceModrinth,
		ProjectID:   hit.ProjectID,
		GameVersion: "1.21.1",
		Loader:      "fabric",
	}, "")
	if err != nil {
		t.Fatalf("真实版本查询失败：%v", err)
	}
	if len(versions.Versions) == 0 {
		t.Fatal("真实版本查询返回 0 条")
	}
	if versions.MatchedCount == 0 {
		t.Fatalf("%s 在 1.21.1/fabric 下没有匹配版本（首条说明：%s）",
			hit.Title, versions.Versions[0].MatchNote)
	}
	target := versions.Versions[0]
	t.Logf("匹配版本：%s（%s，%s，%s）", target.DisplayName, target.VersionNumber, target.Summary, target.FileName)
	if target.FileSize > 8*1024*1024 {
		t.Skipf("主文件 %d 字节过大，跳过真实下载", target.FileSize)
	}

	contentDirectory := t.TempDir()
	result, err := download.DownloadResourceVersion(ctx, models.ResourceDownloadRequest{
		Source:           models.ResourceSourceModrinth,
		ProjectID:        hit.ProjectID,
		VersionID:        target.VersionID,
		ContentDirectory: contentDirectory,
		SubDirectory:     "mods",
	}, "", nil)
	if err != nil {
		t.Fatalf("真实下载失败：%v", err)
	}
	if filepath.Dir(result.SavedPath) != filepath.Join(contentDirectory, "mods") {
		t.Fatalf("落点不对：%s", result.SavedPath)
	}
	info, err := os.Stat(result.SavedPath)
	if err != nil {
		t.Fatalf("文件没有落盘：%v", err)
	}
	if info.Size() == 0 {
		t.Fatal("落盘文件为空")
	}
	t.Logf("已下载到 %s（%d 字节，回退源=%v）", result.SavedPath, info.Size(), result.UsedFallback)
}

// TestLiveCurseForgeMirrorSearchThroughService 真连 CurseForge 镜像走服务层：
// 证明"没配 Key 时给引导、配了 Key（或走镜像）能搜到结果"这条分支在真实环境下成立。
func TestLiveCurseForgeMirrorSearchThroughService(t *testing.T) {
	liveResourceSearchEnabled(t)
	// 服务层不接受"无 Key 走镜像"的隐式行为，这里用占位 Key 触发真实请求，
	// 并把端点指向镜像（镜像自带 Key，占位值不会被校验）——验证的是可用性，
	// 不是官方鉴权。
	curseforge.SetEndpoints(curseforge.MirrorAPIRoot, "")
	t.Cleanup(curseforge.ResetEndpoints)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := download.SearchResources(ctx, models.ResourceSearchRequest{
		Source:      models.ResourceSourceCurseForge,
		ProjectType: models.ProjectTypeMod,
		Query:       "jei",
		GameVersion: "1.21.1",
		Loader:      "neoforge",
		Limit:       5,
	}, "live-placeholder-key")
	if err != nil {
		t.Fatalf("镜像搜索失败：%v", err)
	}
	if len(result.Hits) == 0 {
		t.Fatal("镜像搜索返回 0 条结果")
	}
	first := result.Hits[0]
	t.Logf("镜像首条：%s（%s，%s，%s）", first.Title, first.ProjectID, first.DownloadsDisplay, first.TypeDisplay)
}
