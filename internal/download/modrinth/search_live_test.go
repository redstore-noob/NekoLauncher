package modrinth

// 真实网络用例：默认 skip，只有显式设了 NEKO_LIVE_RESOURCES=1 才跑。
//
// 为什么需要它：离线用例全部指向 httptest 假服务器，能证明"我们的解析与回退逻辑对"，
// 但证明不了"真实 Modrinth（以及国内镜像）的响应形状还是我们以为的样子"。
//
// 运行：NEKO_LIVE_RESOURCES=1 go test ./internal/download/modrinth/ -run TestLive -v -count=1

import (
	"context"
	"os"
	"testing"
	"time"
)

// liveResourcesEnabled 是否允许打真实网络。
func liveResourcesEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("NEKO_LIVE_RESOURCES") != "1" {
		t.Skip("跳过真实网络用例：需要 NEKO_LIVE_RESOURCES=1")
	}
}

// TestLiveModrinthSearchAndVersions 真连 Modrinth：
//   - 官方根地址能搜到 sodium（关键词搜索的 facets 与响应解析都对得上）；
//   - 按项目查版本能拿到文件地址（下载链路的前提）。
func TestLiveModrinthSearchAndVersions(t *testing.T) {
	liveResourcesEnabled(t)
	ResetEndpoints() // 强制官方优先（清掉其它用例可能留下的端点覆盖）

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	hits, err := SearchWithLoader(ctx, "mod", "sodium", "1.21.1", "fabric", 5)
	if err != nil {
		t.Fatalf("真实 Modrinth 搜索失败：%v", err)
	}
	if len(hits) == 0 {
		t.Fatal("真实 Modrinth 搜索 sodium 返回 0 条结果")
	}
	t.Logf("搜索命中 %d 条，首条：%s（%s，%s）", len(hits), hits[0].Title, hits[0].ProjectID, hits[0].DownloadsDisplay())

	versions, err := GetVersions(ctx, hits[0].ProjectID, []string{"1.21.1"}, []string{"fabric"})
	if err != nil {
		t.Fatalf("真实 Modrinth 版本查询失败：%v", err)
	}
	if len(versions) == 0 {
		t.Fatalf("%s 在 1.21.1/fabric 下没有版本", hits[0].Title)
	}
	file := versions[0].PrimaryFile()
	if file == nil || file.URL == "" {
		t.Fatalf("版本 %s 没有可下载的主文件", versions[0].VersionNumber)
	}
	t.Logf("版本 %s（%s）主文件 %s（%s）", versions[0].DisplayName(), versions[0].VersionNumber,
		file.Filename, file.SizeDisplay())
}

// TestLiveModrinthMirrorOnly 真连国内镜像：把主地址设成镜像、禁用回退，
// 验证"官方不通时镜像这条路真的能走通"（这是 X-3 国内可用性的核心假设）。
//
// 注：原本经 SearchQuery 进入，而 SearchQuery 已作为无调用方的薄包装删除，
// 这里改为直接打 SearchFull（镜像回退逻辑所在），覆盖度不变。
func TestLiveModrinthMirrorOnly(t *testing.T) {
	liveResourcesEnabled(t)
	SetEndpoints(MirrorAPIRoot, "")
	t.Cleanup(ResetEndpoints)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	hits, err := SearchFull(ctx, "mod", "sodium", "", 3)
	if err != nil {
		t.Fatalf("镜像 %s 搜索失败：%v", MirrorAPIRoot, err)
	}
	if len(hits) == 0 {
		t.Fatalf("镜像 %s 搜索 sodium 返回 0 条结果", MirrorAPIRoot)
	}
	t.Logf("镜像搜索命中 %d 条，首条：%s", len(hits), hits[0].Title)
}
