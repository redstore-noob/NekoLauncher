package download_test

// 真实网络用例：默认 skip，只有显式设了 NEKO_LIVE_MODRINTH_UPDATE=1 才跑。
//
// 为什么需要它：整套离线用例都指向 httptest 假服务器，能证明"我们的解析逻辑对"，
// 但证明不了"真实 Modrinth 的 version_files 契约还是我们以为的样子"。
// 这个用例负责那一层：真连 api.modrinth.com，用当前运行环境的官方根地址
// （不走镜像覆写），确认 ①批量反查能打通 ②未收录的哈希返回"查不到"而不是报错。
//
// 运行：NEKO_LIVE_MODRINTH_UPDATE=1 go test ./internal/download/ -run TestLiveModrinth -v -count=1

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"nekolauncher/internal/download/modrinth"
)

// liveUpdateEnabled 是否允许打真实网络。
func liveUpdateEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("NEKO_LIVE_MODRINTH_UPDATE") != "1" {
		t.Skip("跳过真实网络用例：需要 NEKO_LIVE_MODRINTH_UPDATE=1")
	}
}

// TestLiveModrinthVersionFilesContract 真连 Modrinth：
//   - 官方根地址（把镜像覆写清掉，验证的是官方契约本身）；
//   - 一个几乎不可能被收录的哈希必须返回"空结果且不报错"（= 未知，不是失败）；
//   - 批量接口形状与解码路径必须与离线用例假设的一致。
func TestLiveModrinthVersionFilesContract(t *testing.T) {
	liveUpdateEnabled(t)

	// 用固定字符串算哈希：结果稳定可复现，且绝不可能是 Modrinth 上架文件
	sum := sha1.Sum([]byte("nekolauncher-x4-live-probe-not-a-real-mod-file"))
	probe := hex.EncodeToString(sum[:])

	modrinth.ResetEndpoints() // 强制官方地址 + 默认客户端（防止上一条用例留下的覆盖）

	t.Logf("官方根地址：%s", modrinth.OfficialAPIRoot)
	t.Logf("探测哈希（sha1）：%s", probe)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	matches, err := modrinth.GetVersionFilesByHashes(ctx, []string{probe})
	if err != nil {
		t.Fatalf("真实 Modrinth 批量反查失败（网络或契约变更）：%v", err)
	}
	t.Logf("反查返回 %d 条记录（期望 0：该哈希不是任何已发布文件）", len(matches))
	if len(matches) != 0 {
		t.Errorf("探测哈希不应命中任何文件，实际命中 %d 条", len(matches))
	}

	// 再走一次「最新版本」通道，确认该端点在生产参数下也能解码
	updates, err := modrinth.GetLatestVersionFilesByHashes(ctx, []string{probe}, []string{"1.21.1"}, []string{"fabric"})
	if err != nil {
		t.Fatalf("真实 Modrinth 最新版本接口失败：%v", err)
	}
	t.Logf("最新版本接口返回 %d 条记录（期望 0）", len(updates))

	// 项目信息端点也要通（项目名回填依赖它）
	name, err := modrinth.GetProjectName(ctx, "P7dR8mSH") // Fabric API 的固定 projectID
	if err != nil {
		t.Fatalf("真实 Modrinth 项目信息接口失败：%v", err)
	}
	t.Logf("P7dR8mSH 的项目名：%q", name)
	if strings.TrimSpace(name) == "" {
		t.Error("项目信息接口应返回非空项目名")
	}
}

// TestLiveModrinthResolvesRealFileHash 真连 Modrinth 验证「能识别的文件一定认得出」：
// 从真实项目的版本列表里取一个**真实存在的文件哈希**，再拿它走一遍批量反查，
// 必须能认回该版本。
//
// 为什么这条最重要：整套离线用例的哈希都是夹具造的，只能证明解析逻辑自洽；
// 一旦接口的哈希口径（sha1 大小写/字段名）或响应形状变了，离线用例照样全绿，
// 而线上会表现为「所有 Mod 都查不到 → 全部显示未知」。这条用例专门堵这个洞。
func TestLiveModrinthResolvesRealFileHash(t *testing.T) {
	liveUpdateEnabled(t)

	modrinth.ResetEndpoints()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	versions, err := modrinth.GetVersions(ctx, "P7dR8mSH", nil, nil) // Fabric API
	if err != nil {
		t.Fatalf("拉取 Fabric API 版本列表失败：%v", err)
	}
	if len(versions) == 0 {
		t.Fatal("Fabric API 不应一个版本都没有")
	}

	var realHash, expectVersionID string
	for _, version := range versions {
		for _, file := range version.Files {
			// models.ModrinthVersionFile 的哈希字段是 map（键 sha1/sha512），
			// 与 mrpack 声明条目的结构不同——这里按接口口径取。
			hash := strings.TrimSpace(file.Hashes["sha1"])
			if hash != "" && file.URL != "" {
				realHash = strings.ToLower(hash)
				expectVersionID = version.ID
				break
			}
		}
		if realHash != "" {
			break
		}
	}
	if realHash == "" {
		t.Fatal("未能从 Fabric API 的版本列表里取到带 sha1 的文件")
	}
	t.Logf("取到真实文件哈希：%s（版本 %s）", realHash, expectVersionID)

	matches, err := modrinth.GetVersionFilesByHashes(ctx, []string{realHash})
	if err != nil {
		t.Fatalf("批量反查真实哈希失败：%v", err)
	}
	match, ok := matches[realHash]
	if !ok {
		t.Fatalf("真实存在的文件哈希必须能反查命中，实际返回 %d 条", len(matches))
	}
	t.Logf("反查命中：project=%s version=%s versionNumber=%s file=%s size=%d",
		match.ProjectID, match.VersionID, match.VersionNumber, match.File.Filename, match.File.Size)
	if match.ProjectID != "P7dR8mSH" {
		t.Errorf("命中的项目 ID = %q，期望 P7dR8mSH", match.ProjectID)
	}
	if match.File.URL == "" || match.File.Size <= 0 {
		t.Errorf("命中的文件缺少下载地址或大小：%+v", match.File)
	}

	// 再问一次「最新版本」：真实哈希必须能得到回答（可能同版本，也可能更新），
	// 不能是"接口没回答"——那在 UI 上会退化成「未知」。
	updates, err := modrinth.GetLatestVersionFilesByHashes(ctx, []string{realHash}, nil, nil)
	if err != nil {
		t.Fatalf("真实哈希的最新版本查询失败：%v", err)
	}
	update, ok := updates[realHash]
	if !ok {
		t.Fatalf("真实哈希必须能在最新版本接口得到回答，实际返回 %d 条", len(updates))
	}
	t.Logf("最新版本：%s（%s）", update.VersionNumber, update.VersionID)
	if update.VersionID == "" || update.File.URL == "" {
		t.Errorf("最新版本结果不完整：%+v", update)
	}
}
