package update

import (
	"context"
	"os"
	"testing"
)

// TestLiveLauncherReleases 真连本仓库的 GitHub Releases，验证"查更新"这条链路在真实
// 网络上成立（含 0 版本这种正常状态：仓库还没发过版本时必须给出可读提示而不是崩）。
//
//	NEKO_LIVE_UPDATE=1 go test ./internal/update/ -run TestLiveLauncherReleases -v
func TestLiveLauncherReleases(t *testing.T) {
	if os.Getenv("NEKO_LIVE_UPDATE") != "1" {
		t.Skip("设置 NEKO_LIVE_UPDATE=1 才真连 GitHub 查版本")
	}

	result, err := Check(context.Background(), "0.0.0-test", true)
	if err != nil {
		// "还没有发布任何版本"是合法状态，不该当成失败
		t.Logf("查询结果：%v（仓库 %s 目前没有可用版本）", err, ReleasesRepo)

		return
	}

	t.Logf("最新版本=%s（预发布=%v，可自动更新=%v，资产=%v），当前=%s，有更新=%v",
		result.LatestVersion, result.Prerelease, result.CanSelfUpdate, result.Asset != nil,
		result.CurrentVersion, result.UpdateAvailable)
	if result.PageURL == "" {
		t.Fatal("结果里必须带版本页地址，否则手动下载无路可走")
	}
}
