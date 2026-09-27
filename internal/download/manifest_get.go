package download

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"nekolauncher/internal/models"
)

// ManifestGet 获取 Minecraft 版本清单的服务。通过 SourceProvider 自动选择下载源。
// 对应 C# ManifestGet。

// GetVersions 获取 Minecraft 版本清单（使用当前活跃下载源，失败自动回退）。
// 返回按发布时间降序的版本列表。
func GetVersions(ctx context.Context) ([]models.MinecraftVersion, error) {
	rawJSON, err := SourceProvider.GetString(ctx, DownloadSources.Official.LauncherMeta, nil)
	if err != nil {
		return nil, err
	}

	var manifest models.VersionManifest
	if err := json.Unmarshal([]byte(rawJSON), &manifest); err != nil {
		return nil, fmt.Errorf("版本清单响应为空或格式错误：%w", err)
	}
	versions := manifest.Versions
	if len(versions) == 0 {
		return []models.MinecraftVersion{}, nil
	}

	for i := range versions {
		if versions[i].ID == manifest.Latest.Release {
			versions[i].IsLatestRelease = true
		}
		if versions[i].ID == manifest.Latest.Snapshot {
			versions[i].IsLatestSnapshot = true
		}
	}

	sort.SliceStable(versions, func(i, j int) bool {
		return versions[i].ReleaseTime.After(versions[j].ReleaseTime)
	})
	return versions, nil
}
