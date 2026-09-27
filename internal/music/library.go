package music

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// 配置持久化键名（与 C# 一致）。
const (
	folderKey      = "musicFolder"
	sortKey        = "musicSortMode"
	volumeKey      = "musicVolume"
	playbackModeKey = "musicPlaybackMode"
)

// MusicSortMode 音乐列表排序模式。
type MusicSortMode string

const (
	// SortFileName 按文件名升序。
	SortFileName MusicSortMode = "FileName"
	// SortFileNameDesc 按文件名降序。
	SortFileNameDesc MusicSortMode = "FileNameDesc"
	// SortDateModified 按修改时间升序。
	SortDateModified MusicSortMode = "DateModified"
	// SortDateModifiedDesc 按修改时间降序。
	SortDateModifiedDesc MusicSortMode = "DateModifiedDesc"
	// SortFileSize 按文件大小升序。
	SortFileSize MusicSortMode = "FileSize"
	// SortFileSizeDesc 按文件大小降序。
	SortFileSizeDesc MusicSortMode = "FileSizeDesc"
)

// MusicLibrary 音乐库管理：扫描文件夹、管理播放列表、持久化设置。
type MusicLibrary struct {
	mu        sync.Mutex
	tracks    []MusicTrack
	sorted    []MusicTrack
	sortMode  MusicSortMode
	config    ConfigStore
}

// NewMusicLibrary 创建音乐库。config 为持久化存储，传 nil 时使用内存实现。
func NewMusicLibrary(config ConfigStore) *MusicLibrary {
	if config == nil {
		config = NewMemoryConfigStore()
	}
	return &MusicLibrary{
		sortMode: SortFileName,
		config:   config,
	}
}

// Tracks 当前播放列表（已排序的副本）。
func (l *MusicLibrary) Tracks() []MusicTrack {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]MusicTrack, len(l.sorted))
	copy(out, l.sorted)
	return out
}

// SortMode 当前排序模式；设置后重新排序并持久化。
func (l *MusicLibrary) SortMode() MusicSortMode {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sortMode
}

// SetSortMode 设置排序模式。
func (l *MusicLibrary) SetSortMode(mode MusicSortMode) {
	l.mu.Lock()
	l.sortMode = mode
	l.sorted = SortTracks(l.tracks, mode)
	l.mu.Unlock()
	l.config.SetValue(sortKey, string(mode))
}

// FolderPath 当前音乐文件夹路径。
func (l *MusicLibrary) FolderPath() string {
	return l.config.GetValue(folderKey)
}

// Volume 音量 (0-100)。
func (l *MusicLibrary) Volume() int {
	val := l.config.GetValue(volumeKey)
	v, err := strconv.Atoi(val)
	if err != nil {
		return 80
	}
	return clamp(v, 0, 100)
}

// SetVolume 设置音量。
func (l *MusicLibrary) SetVolume(value int) {
	l.config.SetValue(volumeKey, strconv.Itoa(clamp(value, 0, 100)))
}

// PlaybackMode 播放模式（顺序 / 列表循环 / 单曲循环 / 随机）。
func (l *MusicLibrary) PlaybackMode() PlaybackMode {
	val := l.config.GetValue(playbackModeKey)
	mode := PlaybackMode(val)
	switch mode {
	case ModeSequential, ModeRepeatAll, ModeRepeatOne, ModeShuffle:
		return mode
	default:
		return ModeSequential
	}
}

// SetPlaybackMode 设置播放模式。
func (l *MusicLibrary) SetPlaybackMode(mode PlaybackMode) {
	l.config.SetValue(playbackModeKey, string(mode))
}

// SetFolder 设置音乐文件夹并扫描。
func (l *MusicLibrary) SetFolder(path string) error {
	if strings.TrimSpace(path) == "" {
		return errInvalid("音乐文件夹路径不能为空")
	}
	l.config.SetValue(folderKey, strings.TrimSpace(path))
	l.Scan()
	return nil
}

type errInvalid string

func (e errInvalid) Error() string { return string(e) }

// Scan 扫描当前设置的音乐文件夹。
func (l *MusicLibrary) Scan() {
	folder := l.FolderPath()
	if strings.TrimSpace(folder) == "" {
		l.mu.Lock()
		l.tracks = nil
		l.sorted = nil
		l.mu.Unlock()
		return
	}
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		l.mu.Lock()
		l.tracks = nil
		l.sorted = nil
		l.mu.Unlock()
		return
	}

	var tracks []MusicTrack
	for _, file := range enumerateAudioFilesSafe(folder) {
		if !IsSupported(file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil {
			continue // 跳过无法读取的文件
		}
		tracks = append(tracks, MusicTrack{
			FilePath:     file,
			FileSize:     info.Size(),
			LastModified: info.ModTime(),
		})
	}

	// 加载保存的排序模式（与 SortMode/GetSorted 同锁，避免撕裂读）
	var parsedSort MusicSortMode
	if saved := l.config.GetValue(sortKey); saved != "" {
		mode := MusicSortMode(saved)
		switch mode {
		case SortFileName, SortFileNameDesc, SortDateModified,
			SortDateModifiedDesc, SortFileSize, SortFileSizeDesc:
			parsedSort = mode
		}
	}

	l.mu.Lock()
	if parsedSort != "" {
		l.sortMode = parsedSort
	}
	l.tracks = tracks
	l.sorted = SortTracks(tracks, l.sortMode)
	l.mu.Unlock()
}

// enumerateAudioFilesSafe 递归枚举音频文件，单个子目录不可读时跳过。
// 对应 C# 的 Directory.EnumerateFiles(AllDirectories) 安全替代：
// 延迟枚举中途抛异常会丢弃整份列表，这里是逐层读取、失败仅跳过该层。
func enumerateAudioFilesSafe(directory string) []string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil // 目录不可读时跳过（对应 IOException / UnauthorizedAccessException）
	}
	var files []string
	for _, entry := range entries {
		name := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			files = append(files, enumerateAudioFilesSafe(name)...)
		} else if isRegularFile(name) {
			files = append(files, name)
		}
	}
	return files
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Search 根据文件名模糊搜索（不区分大小写）。keyword 为空时返回全部。
func (l *MusicLibrary) Search(keyword string) []MusicTrack {
	tracks := l.Tracks()
	if strings.TrimSpace(keyword) == "" {
		return tracks
	}
	keyword = strings.ToLower(keyword)
	var result []MusicTrack
	for _, t := range tracks {
		if strings.Contains(strings.ToLower(t.Title()), keyword) {
			result = append(result, t)
		}
	}
	return result
}

// GetSorted 获取指定模式排序后的列表。
func (l *MusicLibrary) GetSorted(mode MusicSortMode) []MusicTrack {
	l.mu.Lock()
	defer l.mu.Unlock()
	return SortTracks(l.tracks, mode)
}

// SortTracks 根据模式排序（返回副本，不修改原切片）。
func SortTracks(tracks []MusicTrack, mode MusicSortMode) []MusicTrack {
	out := make([]MusicTrack, len(tracks))
	copy(out, tracks)
	switch mode {
	case SortFileName:
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(out[i].Title()) < strings.ToLower(out[j].Title())
		})
	case SortFileNameDesc:
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(out[i].Title()) > strings.ToLower(out[j].Title())
		})
	case SortDateModified:
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].LastModified.Before(out[j].LastModified)
		})
	case SortDateModifiedDesc:
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].LastModified.After(out[j].LastModified)
		})
	case SortFileSize:
		sort.SliceStable(out, func(i, j int) bool { return out[i].FileSize < out[j].FileSize })
	case SortFileSizeDesc:
		sort.SliceStable(out, func(i, j int) bool { return out[i].FileSize > out[j].FileSize })
	}
	return out
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
