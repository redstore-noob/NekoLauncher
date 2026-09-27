package instance

// 实例截图扫描：主页「截图墙」小组件使用。
//
// 截图目录随版本隔离布局变化：隔离实例在 <实例内容目录>/screenshots，
// 共享实例在 <游戏根目录>/screenshots。这里直接复用隔离解析，避免前端
// 自己拼接路径时漏判隔离。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ScreenshotInfo 一张截图文件的元信息。
type ScreenshotInfo struct {
	// Path 文件绝对路径（前端经 /localfile 展示）。
	Path string `json:"Path"`
	// Name 文件名。
	Name string `json:"Name"`
	// SizeBytes 文件大小。
	SizeBytes int64 `json:"SizeBytes"`
	// ModifiedAt 修改时间。
	ModifiedAt time.Time `json:"ModifiedAt"`
}

// screenshotExtensions 视为截图的扩展名（与 localfile_handler 图片白名单一致）。
var screenshotExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".bmp": true, ".gif": true,
}

// ListScreenshots 列出指定实例截图目录下的图片，按修改时间倒序（新的在前）。
// versionID 为空时回落到当前游戏根目录的 screenshots。
func ListScreenshots(versionID string) []ScreenshotInfo {
	snapshot := CurrentSnapshot()
	directory := screenshotDirectory(snapshot, versionID)

	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}

	result := make([]ScreenshotInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !screenshotExtensions[ext] {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, ScreenshotInfo{
			Path:       filepath.Join(directory, entry.Name()),
			Name:       entry.Name(),
			SizeBytes:  info.Size(),
			ModifiedAt: info.ModTime(),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ModifiedAt.After(result[j].ModifiedAt)
	})

	return result
}

// screenshotDirectory 解析实例的截图目录（版本隔离感知）。
func screenshotDirectory(snapshot GameInstanceSnapshot, versionID string) string {
	if strings.TrimSpace(versionID) == "" {
		return filepath.Join(snapshot.MinecraftDirectory, "screenshots")
	}

	layout := GameVersionIsolationResolve(snapshot, versionID)
	return filepath.Join(layout.ContentDirectory, "screenshots")
}
