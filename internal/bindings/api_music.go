package bindings

// MusicAPI：音乐播放器。状态机与播放列表在 Go 侧（music.Shared），
// 实际音频解码输出由前端 Web Audio 实现：
//   - Go → 前端事件：music:play {filePath} / music:pause / music:resume /
//     music:stop / music:seek {positionMs} / music:volume {percent}；
//   - 前端 → Go 命令：ReportPlaybackProgress（进度回传）、NotifyTrackFinished（自然播完）。
// 状态类事件：music:stateChanged / music:trackChanged /
// music:playbackModeChanged / music:trackFinished。

import (
	"time"

	"nekolauncher/internal/music"
)

// ---- 曲库 ----

// SetMusicFolder 设置音乐目录并立即扫描。设置守卫见 guardSettableRoot：
// 只认已注册游戏目录或用户经对话框选中的目录——音乐目录是读收口的
// 根之一，任意设置等于扩大读取范围。
func (a *MusicAPI) SetMusicFolder(path string) error {
	if err := guardSettableRoot(path); err != nil {
		return err
	}
	return a.library.SetFolder(path)
}

// GetMusicFolderPath 当前音乐目录。
func (a *MusicAPI) GetMusicFolderPath() string { return a.library.FolderPath() }

// ScanMusicLibrary 重新扫描曲库。
func (a *MusicAPI) ScanMusicLibrary() { a.library.Scan() }

// GetMusicTracks 全部曲目。
func (a *MusicAPI) GetMusicTracks() []music.MusicTrack { return a.library.Tracks() }

// SearchMusicTracks 按关键字过滤曲目。
func (a *MusicAPI) SearchMusicTracks(keyword string) []music.MusicTrack {
	return a.library.Search(keyword)
}

// GetSortedMusicTracks 按指定模式排序曲目。
func (a *MusicAPI) GetSortedMusicTracks(mode music.MusicSortMode) []music.MusicTrack {
	return a.library.GetSorted(mode)
}

// GetMusicSortMode 当前排序模式。
func (a *MusicAPI) GetMusicSortMode() music.MusicSortMode { return a.library.SortMode() }

// SetMusicSortMode 保存排序模式。
func (a *MusicAPI) SetMusicSortMode(mode music.MusicSortMode) { a.library.SetSortMode(mode) }

// ---- 音量 / 播放模式 ----

// GetMusicVolume 曲库持久化音量 (0-100)。
func (a *MusicAPI) GetMusicVolume() int { return a.library.Volume() }

// SetMusicVolume 设置音量：持久化 + 应用到播放器 + 通知前端。
func (a *MusicAPI) SetMusicVolume(value int) {
	a.library.SetVolume(value)
	music.Shared.SetVolume(a.library.Volume())
}

// GetMusicPlaybackMode 当前播放模式。
func (a *MusicAPI) GetMusicPlaybackMode() music.PlaybackMode { return music.Shared.PlaybackMode() }

// SetMusicPlaybackMode 设置播放模式（播放器 + 持久化）。
func (a *MusicAPI) SetMusicPlaybackMode(mode music.PlaybackMode) {
	music.Shared.SetPlaybackMode(mode)
	a.library.SetPlaybackMode(mode)
}

// ---- 播放控制 ----

// PlayTrack 播放指定曲目（触发 music:play 事件让前端出声）。
func (a *MusicAPI) PlayTrack(track music.MusicTrack) error { return music.Shared.Play(track) }

// PausePlayback 暂停。
func (a *MusicAPI) PausePlayback() { music.Shared.Pause() }

// ResumePlayback 从暂停位置恢复。
func (a *MusicAPI) ResumePlayback() error { return music.Shared.Resume() }

// StopPlayback 停止并归零进度。
func (a *MusicAPI) StopPlayback() { music.Shared.Stop() }

// NextTrack 下一首（按当前模式选曲）。
func (a *MusicAPI) NextTrack() bool { return music.Shared.Next() }

// PreviousTrack 上一首。
func (a *MusicAPI) PreviousTrack() bool { return music.Shared.Previous() }

// SeekPlayback 跳转（ms；经 music:seek 事件交给前端执行）。
func (a *MusicAPI) SeekPlayback(positionMs int64) {
	music.Shared.Seek(time.Duration(positionMs) * time.Millisecond)
}

// ---- 播放状态 ----

// GetCurrentTrack 当前曲目（无则 nil）。
func (a *MusicAPI) GetCurrentTrack() *music.MusicTrack { return music.Shared.CurrentTrack() }

// GetPlaylist 播放列表。
func (a *MusicAPI) GetPlaylist() []music.MusicTrack { return music.Shared.Playlist() }

// SetPlaylist 设置播放列表（自动切歌 / 上下曲基于该列表）。
func (a *MusicAPI) SetPlaylist(tracks []music.MusicTrack) { music.Shared.SetPlaylist(tracks) }

// ---- 前端音频回传命令 ----

// ReportPlaybackProgress 前端回传播放进度（毫秒）。
func (a *MusicAPI) ReportPlaybackProgress(positionMs, durationMs int64) {
	a.positionNs.Store(int64(time.Duration(positionMs) * time.Millisecond))
	a.durationNs.Store(int64(time.Duration(durationMs) * time.Millisecond))
}

// NotifyTrackFinished 前端通知当前曲目自然播完（触发自动切歌 / 列表播完事件）。
func (a *MusicAPI) NotifyTrackFinished() {
	a.finishedMu.Lock()
	callback := a.onTrackFinished
	a.finishedMu.Unlock()
	if callback != nil {
		callback()
	}
}
