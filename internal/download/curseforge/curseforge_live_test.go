package curseforge

// 真实网络用例：默认 skip，只有显式设了 NEKO_LIVE_RESOURCES=1 才跑。
//
// 官方接口需要用户自备 API Key，CI/本机都没有，因此这里验证的是**镜像**这条
// 国内可用性路径：镜像自带 Key，未配置 Key 的用户也能搜到内容（这也是
// "没配 Key 不崩界面"之外的第二层价值）。
//
// 运行：NEKO_LIVE_RESOURCES=1 go test ./internal/download/curseforge/ -run TestLive -v -count=1

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"nekolauncher/internal/models"
)

// TestLiveCurseForgeMirrorSearchAndFiles 真连 CurseForge 镜像：
// 搜索必须返回 Minecraft 的 Mod 分类结果，项目 ID 必须是数字，
// 文件列表必须能拿到文件名与地址（下载链路的前提）。
func TestLiveCurseForgeMirrorSearchAndFiles(t *testing.T) {
	if os.Getenv("NEKO_LIVE_RESOURCES") != "1" {
		t.Skip("跳过真实网络用例：需要 NEKO_LIVE_RESOURCES=1")
	}
	SetEndpoints(MirrorAPIRoot, "")
	t.Cleanup(ResetEndpoints)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := Search(ctx, "", SearchOptions{
		Query:       "jei",
		ClassID:     models.CurseForgeClassMods,
		GameVersion: "1.21.1",
		LoaderType:  models.CurseForgeLoaderNeoForge,
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("镜像 %s 搜索失败：%v", MirrorAPIRoot, err)
	}
	if len(result.Data) == 0 {
		t.Fatalf("镜像 %s 搜索 jei 返回 0 条结果", MirrorAPIRoot)
	}
	project := result.Data[0]
	if project.ID <= 0 || project.Name == "" {
		t.Fatalf("镜像返回的项目字段不完整：%+v", project)
	}
	t.Logf("镜像搜索命中 %d 条（总数 %d），首条：%s（id=%d，%s）",
		len(result.Data), result.Pagination.TotalCount, project.Name, project.ID, project.DownloadsDisplay())

	files, err := GetFiles(ctx, "", strconv.Itoa(project.ID), "1.21.1", models.CurseForgeLoaderNeoForge, 5)
	if err != nil {
		t.Fatalf("镜像文件列表查询失败：%v", err)
	}
	if len(files) == 0 {
		t.Fatalf("%s 在 1.21.1/NeoForge 下没有文件", project.Name)
	}
	file := files[0]
	if file.FileName == "" {
		t.Fatalf("文件字段不完整：%+v", file)
	}
	t.Logf("版本 %d：%s（%s，加载器 %s）", file.ID, file.FileName, file.SizeDisplay(), file.LoaderDisplay())
}
