// Rewind 全局汇总：主页小组件需要的是"整个回溯仓库"的一眼概览，
// 而不是某个存档/实例的快照列表——这里跨仓库扫描清单并统计占用。
package content

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// rewindSummaryRecentColors 迷你时间线最多展示的最近快照数。
const rewindSummaryRecentColors = 12

// RewindSummary Rewind 仓库的全局汇总（跨全部存档/实例仓库）。
type RewindSummary struct {
	// SnapshotCount 全部仓库的快照总数
	SnapshotCount int `json:"snapshotCount"`
	// BlobBytes 数据块实际占用（全局共享，同一块被多个快照引用只算一次）
	BlobBytes int64 `json:"blobBytes"`
	// BudgetBytes 仓库数据块预算（超过后按"先自动、后手动"淘汰最旧快照）
	BudgetBytes int64 `json:"budgetBytes"`
	// LastSnapshotAt 最近一次快照时间；从未快照过为零值
	LastSnapshotAt time.Time `json:"lastSnapshotAt"`
	// RecentColors 最近若干次快照的标记颜色（新的在前；空串表示未标记），
	// 供主页小组件画迷你时间线，不含隐私内容
	RecentColors []string `json:"recentColors,omitempty"`
}

// ComputeRewindSummary 扫描 Rewind 仓库根并汇总统计。仓库根不存在（从未快照过）
// 时返回零值汇总；单个清单损坏时跳过该清单，不让整个汇总失败。
func ComputeRewindSummary() RewindSummary {
	summary := RewindSummary{BudgetBytes: snapshotMaxTotalBytes}

	root := snapshotRootDirectory()
	reposRoot := filepath.Join(root, snapshotReposDirName)
	entries, err := os.ReadDir(reposRoot)
	if err != nil {
		return summary
	}

	type recentEntry struct {
		at    time.Time
		color string
	}
	var recents []recentEntry

	for _, repo := range entries {
		if !repo.IsDir() {
			continue
		}
		manifestsDir := filepath.Join(reposRoot, repo.Name(), snapshotManifestDirName)
		files, err := os.ReadDir(manifestsDir)
		if err != nil {
			continue
		}
		for _, file := range files {
			if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(manifestsDir, file.Name()))
			if err != nil {
				continue
			}
			var manifest saveSnapshotManifest
			if err := json.Unmarshal(data, &manifest); err != nil || manifest.Id == "" {
				continue
			}
			summary.SnapshotCount++
			if manifest.CreatedAt.After(summary.LastSnapshotAt) {
				summary.LastSnapshotAt = manifest.CreatedAt
			}
			recents = append(recents, recentEntry{at: manifest.CreatedAt, color: manifest.Color})
		}
	}

	summary.BlobBytes = directorySize(filepath.Join(root, snapshotBlobsDirName))

	if len(recents) > 0 {
		sort.Slice(recents, func(i, j int) bool { return recents[i].at.After(recents[j].at) })
		if len(recents) > rewindSummaryRecentColors {
			recents = recents[:rewindSummaryRecentColors]
		}
		summary.RecentColors = make([]string, len(recents))
		for i, entry := range recents {
			summary.RecentColors[i] = entry.color
		}
	}

	return summary
}

// directorySize 递归统计目录字节数；目录不存在返回 0。
func directorySize(directory string) int64 {
	var total int64

	err := filepath.WalkDir(directory, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil // 读不到的项跳过，不让汇总失败
		}
		if entry.Type().IsRegular() {
			if info, infoErr := entry.Info(); infoErr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	if err != nil {
		return total
	}
	return total
}
