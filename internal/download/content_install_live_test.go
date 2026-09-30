package download

// 真实网络用例：默认 skip，NEKO_LIVE_RESOURCES=1 且提供 NEKO_CF_API_KEY 才跑。
//
// 防的回归：CurseForge 整合包导入的依赖下载走 www.curseforge.com 公开端点，
// 该端点已被 Cloudflare 人机验证拦截（403 "Just a moment…"），必须经官方 API
// （x-api-key）换 edge.forgecdn.net 直链才能完成导入。
//
// 运行：NEKO_LIVE_RESOURCES=1 NEKO_CF_API_KEY=<key> go test ./internal/download/ -run TestLive -v -count=1

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestLiveInstallCurseForgeZipWithDependency 真实导入一个带 CF 依赖声明的整合包：
// manifest 声明 JEI（projectID 238222）的某个真实 fileID，overrides 带一个配置文件。
// 导入完成后 overrides 落位、JEI jar 经 API 换直链下载进 mods/ 且体积非零。
func TestLiveInstallCurseForgeZipWithDependency(t *testing.T) {
	if os.Getenv("NEKO_LIVE_RESOURCES") != "1" {
		t.Skip("跳过真实网络用例：需要 NEKO_LIVE_RESOURCES=1")
	}
	apiKey := os.Getenv("NEKO_CF_API_KEY")
	if apiKey == "" {
		t.Skip("跳过真实网络用例：需要 NEKO_CF_API_KEY")
	}

	// JEI 1.20.1-forge 的真实文件（fileID 由构建时 API 查得；失效时换一个即可）
	const jeiProjectID, jeiFileID = 238222, 9009996

	archivePath := filepath.Join(t.TempDir(), "cf-pack.zip")
	if err := writeCurseForgeTestPack(archivePath, jeiProjectID, jeiFileID); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "cf-instance")
	result, err := InstallModpack(context.Background(), apiKey, archivePath, target, nil)
	if err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	if len(result.Errors) > 0 {
		t.Fatalf("导入过程报错：%v", result.Errors)
	}
	if result.DownloadedMods != 1 {
		t.Errorf("下载依赖数 = %d，期望 1", result.DownloadedMods)
	}
	mods, err := filepath.Glob(filepath.Join(target, "mods", "*.jar"))
	if err != nil || len(mods) != 1 {
		t.Fatalf("mods/ 下应有 1 个 jar，实际 %d 个（%v）", len(mods), err)
	}
	info, err := os.Stat(mods[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 1024 {
		t.Errorf("%s 体积 %d 字节，像是错误页而不是 jar", mods[0], info.Size())
	}
}

// writeCurseForgeTestPack 构造最小 CurseForge 整合包：manifest 声明一个真实依赖 + 一个 override。
func writeCurseForgeTestPack(archivePath string, projectID, fileID int64) error {
	archive, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()

	writer := zip.NewWriter(archive)
	add := func(name, content string) error {
		entry, err := writer.Create(name)
		if err != nil {
			return err
		}
		_, err = entry.Write([]byte(content))

		return err
	}

	manifest := `{"manifestVersion":1,"manifestType":"minecraftModpack","name":"live-test","version":"1.0.0",` +
		`"minecraft":{"version":"1.20.1"},"files":[{"projectID":` +
		strconv.FormatInt(projectID, 10) + `,"fileID":` + strconv.FormatInt(fileID, 10) + `,"required":true}]}`
	if err := add("manifest.json", manifest); err != nil {
		return err
	}
	if err := add("overrides/config/live-test.cfg", "live=true"); err != nil {
		return err
	}

	return writer.Close()
}
