// Package music 音乐库扫描与播放器状态管理。
// 移植自 NyaLauncher.Core/Music（MusicTrack / MusicLibrary / MusicPlayerService）。
package music

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// SupportedExtensions 支持的音频文件扩展名。
// 只收 WebView 能解码的容器（播放走 /localfile + <audio>）：WMA 在
// Chromium/WebKit 上都解不了，收进来只会让用户看到"能点却永远播不出声"的曲目，
// 因此刻意排除。新增格式时记得同步 bindings 的 audioExtWhitelist。
var SupportedExtensions = []string{
	".mp3", ".wav", ".ogg", ".flac", ".aac", ".m4a", ".opus",
}

// MusicTrack 音乐文件元数据。
// JSON 字段名与 C# record 属性一致（PascalCase）。
type MusicTrack struct {
	// FilePath 文件完整路径。
	FilePath string `json:"FilePath"`
	// FileSize 文件大小（字节）。
	FileSize int64 `json:"FileSize"`
	// LastModified 最后修改时间。
	LastModified time.Time `json:"LastModified"`
}

// Title 文件名（不含扩展名）作为显示标题。
func (t MusicTrack) Title() string {
	base := filepath.Base(t.FilePath)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

// Extension 文件扩展名（如 .mp3，小写）。
func (t MusicTrack) Extension() string {
	return strings.ToLower(filepath.Ext(t.FilePath))
}

// ExtensionDisplay 扩展名显示（去掉点，如 "mp3"）。
func (t MusicTrack) ExtensionDisplay() string {
	return strings.TrimPrefix(t.Extension(), ".")
}

// MetaDisplay 列表副标题（如 "mp3 · 4.2 MB"）。
func (t MusicTrack) MetaDisplay() string {
	return fmt.Sprintf("%s · %s", t.ExtensionDisplay(), t.SizeDisplay())
}

// SizeDisplay 文件大小的格式化显示。
func (t MusicTrack) SizeDisplay() string {
	switch {
	case t.FileSize >= 1048576:
		return fmt.Sprintf("%.1f MB", float64(t.FileSize)/1048576.0)
	case t.FileSize >= 1024:
		return fmt.Sprintf("%.0f KB", float64(t.FileSize)/1024.0)
	default:
		return fmt.Sprintf("%d B", t.FileSize)
	}
}

// DateDisplay 最后修改时间的格式化显示（yyyy-MM-dd HH:mm）。
func (t MusicTrack) DateDisplay() string {
	return t.LastModified.Format("2006-01-02 15:04")
}

// IsSupported 判断文件是否为支持的音频格式。
func IsSupported(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	for _, supported := range SupportedExtensions {
		if ext == supported {
			return true
		}
	}
	return false
}

// String 实现 fmt.Stringer，与 C# ToString() 一致返回标题。
func (t MusicTrack) String() string { return t.Title() }
