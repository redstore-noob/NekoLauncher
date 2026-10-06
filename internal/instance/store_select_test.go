package instance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSelectRescansWhenSnapshotIsStale 覆盖"界面列得出版本、点选却没反应"的场景：
// 前端列版本是直接扫 versions/ 目录，而 Select 校验的是快照里的版本列表。
// 快照落后于磁盘时（在启动器外装了新版本 / 刚改了游戏目录 / 恰有刷新在跑），
// Select 必须重扫一次再判定，而不是直接拒绝。
func TestSelectRescansWhenSnapshotIsStale(t *testing.T) {
	root := t.TempDir()
	versionsDir := filepath.Join(root, "versions")
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeVersionDir(t, versionsDir, "1.20.1", vanillaJSON, true, nil)

	// 建立初始快照：此时磁盘上只有 1.20.1
	Refresh(context.Background(), root)
	if got := CurrentSnapshot(); len(got.VersionIds) != 1 {
		t.Fatalf("初始快照应有 1 个版本，实际：%+v", got.VersionIds)
	}

	// 模拟"启动器外"新装的版本：只落磁盘，不刷新快照
	makeVersionDir(t, versionsDir, "1.21.1", `{"id":"1.21.1","type":"release"}`, true, nil)
	if got := CurrentSnapshot(); len(got.VersionIds) != 1 {
		t.Fatalf("写入磁盘后快照不应自行变化（前置条件），实际：%+v", got.VersionIds)
	}

	if !Select("1.21.1") {
		t.Fatal("磁盘上存在的版本应能选中（需要先重扫快照再判定）")
	}
	after := CurrentSnapshot()
	if !containsFold(after.VersionIds, "1.21.1") {
		t.Fatalf("重扫后快照应包含新版本，实际：%+v", after.VersionIds)
	}
	if after.SelectedVersionId != "1.21.1" {
		t.Fatalf("选中项应为 1.21.1，实际：%q", after.SelectedVersionId)
	}
}

// TestSelectRejectsMissingVersion 磁盘与快照里都没有的版本仍要拒绝（重扫不是万能放行）。
func TestSelectRejectsMissingVersion(t *testing.T) {
	root := t.TempDir()
	versionsDir := filepath.Join(root, "versions")
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeVersionDir(t, versionsDir, "1.20.1", vanillaJSON, true, nil)
	Refresh(context.Background(), root)

	if Select("2.0-does-not-exist") {
		t.Fatal("不存在的版本必须返回 false")
	}
	if Select("   ") {
		t.Fatal("空版本号必须返回 false")
	}
	if got := CurrentSnapshot().SelectedVersionId; got != "1.20.1" {
		t.Fatalf("拒绝后选中项不应被改动，实际：%q", got)
	}
}

func containsFold(list []string, want string) bool {
	for _, item := range list {
		if strings.EqualFold(item, want) {
			return true
		}
	}

	return false
}
