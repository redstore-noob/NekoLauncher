package download

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"nekolauncher/internal/models"
)

// ManifestGet 获取 Minecraft 版本清单的服务。通过 SourceProvider 自动选择下载源。
// 对应 C# ManifestGet。

// 版本清单内存缓存：清单全量约 700 项且短期内不会变化，下载页每次挂载、
// 整合包安装前的版本确认都会调 GetVersions；不缓存的话每次都完整联网
// 拉取（慢源 + 镜像重试时可达数十秒），界面长时间停在加载态。
var (
	versionsCacheMu   sync.Mutex
	versionsCache     []models.MinecraftVersion
	versionsCacheTime time.Time
)

const versionsCacheTTL = 10 * time.Minute

// GetVersions 获取 Minecraft 版本清单（使用当前活跃下载源，失败自动回退）。
// 返回按发布时间降序的版本列表。命中缓存时直接返回，不再发起网络请求。
// 注意：返回的是缓存切片，调用方不得原地修改。
func GetVersions(ctx context.Context) ([]models.MinecraftVersion, error) {
	versionsCacheMu.Lock()
	if versionsCache != nil && time.Since(versionsCacheTime) < versionsCacheTTL {
		cached := versionsCache
		versionsCacheMu.Unlock()

		return cached, nil
	}
	versionsCacheMu.Unlock()

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

	versionsCacheMu.Lock()
	versionsCache = versions
	versionsCacheTime = time.Now()
	versionsCacheMu.Unlock()

	return versions, nil
}
