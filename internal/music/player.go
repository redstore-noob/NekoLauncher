package music

import (
	"errors"
	"math/rand"
	"sync"
	"time"
)

// PlaybackState 播放状态。
type PlaybackState string

const (
	// StateStopped 已停止。
	StateStopped PlaybackState = "Stopped"
	// StatePlaying 播放中。
	StatePlaying PlaybackState = "Playing"
	// StatePaused 已暂停。
	StatePaused PlaybackState = "Paused"
)

// PlaybackMode 播放模式：顺序（播完停）、列表循环、单曲循环、随机播放。
type PlaybackMode string

const (
	// ModeSequential 顺序播放（播完停）。
	ModeSequential PlaybackMode = "Sequential"
	// ModeRepeatAll 列表循环。
	ModeRepeatAll PlaybackMode = "RepeatAll"
	// ModeRepeatOne 单曲循环。
	ModeRepeatOne PlaybackMode = "RepeatOne"
	// ModeShuffle 随机播放。
	ModeShuffle PlaybackMode = "Shuffle"
)

// AudioPlayer 实际音频输出接口。
//
// Go 标准库没有跨平台音频播放能力，C# 侧的 NAudio（Windows）与
// afplay / mpv（macOS / Linux）回退无法直接移植。这里只保留状态管理
// 与播放列表逻辑，实际解码与音频输出由该接口的实现方承担：
// 将来由前端 Web Audio（Wails 前端通过事件桥接）或 Wails 侧原生绑定实现。
type AudioPlayer interface {
	// Play 打开并播放指定文件；失败返回 error。
	Play(filePath string) error
	// Pause 暂停（保留进度）。
	Pause()
	// Resume 从暂停位置恢复。
	Resume() error
	// Stop 停止并释放资源。
	Stop()
	// Seek 跳转到指定位置（自文件起算）。
	Seek(position time.Duration)
	// Position 当前播放位置（未播放时返回 0）。
	Position() time.Duration
	// Duration 当前曲目总时长（未知时返回 0）。
	Duration() time.Duration
	// SetVolume 设置音量 (0-100)。
	SetVolume(percent int)
	// SetOnFinished 注册自然播完回调（手动停止/暂停不得触发）。
	SetOnFinished(callback func())
}

// MusicPlayerService 音乐播放器服务（全局共享单例，见 Shared）。
// 只移植 C# 版的状态管理、播放列表与自动切歌逻辑；音频输出委托给 AudioPlayer。
type MusicPlayerService struct {
	mu           sync.Mutex
	random       *rand.Rand
	state        PlaybackState
	currentTrack *MusicTrack
	volume       int
	manualStop   bool
	playbackMode PlaybackMode
	playlist     []MusicTrack

	// audio 实际音频输出实现（可为 nil：仅状态机，如用于测试）。
	audio AudioPlayer

	// 事件/回调：C# 的 event Action → 回调函数字段（导出直接赋值）。
	// OnStateChanged       播放/暂停/停止状态变化。
	// OnTrackChanged       当前曲目变化（手动选择或自动切歌）。
	// OnPlaybackModeChanged 播放模式变化。
	// OnTrackFinished      顺序模式播完全部曲目、且没有下一首时触发
	//                      （列表循环/随机模式不会触发）。
	OnStateChanged        func()
	OnTrackChanged        func()
	OnPlaybackModeChanged func()
	OnTrackFinished       func()
}

// Shared 全局共享实例。所有界面（页面 / 桌面组件）都应使用该实例。
var Shared = NewMusicPlayerService(nil)

// NewMusicPlayerService 创建播放器服务。audio 为实际音频输出实现，
// 传 nil 时仅维护状态机（不产生声音）。
func NewMusicPlayerService(audio AudioPlayer) *MusicPlayerService {
	s := &MusicPlayerService{
		random:       rand.New(rand.NewSource(time.Now().UnixNano())),
		state:        StateStopped,
		volume:       80,
		playbackMode: ModeSequential,
		audio:        audio,
	}
	if audio != nil {
		audio.SetVolume(s.volume)
		audio.SetOnFinished(s.onNaturallyFinished)
	}
	return s
}

// State 当前播放状态。
func (s *MusicPlayerService) State() PlaybackState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// CurrentTrack 当前曲目（无曲目时返回 nil）。
func (s *MusicPlayerService) CurrentTrack() *MusicTrack {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentTrack
}

// Playlist 播放列表。自动切歌、上一首/下一首都基于该列表；
// 页面应在扫描/排序后将其设置为完整（未过滤）的曲目列表。
func (s *MusicPlayerService) Playlist() []MusicTrack {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]MusicTrack, len(s.playlist))
	copy(out, s.playlist)
	return out
}

// SetPlaylist 设置播放列表。
func (s *MusicPlayerService) SetPlaylist(tracks []MusicTrack) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tracks == nil {
		tracks = []MusicTrack{}
	}
	s.playlist = tracks
}

// PlaybackMode 当前播放模式。
func (s *MusicPlayerService) PlaybackMode() PlaybackMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.playbackMode
}

// SetPlaybackMode 设置播放模式。
func (s *MusicPlayerService) SetPlaybackMode(mode PlaybackMode) {
	s.mu.Lock()
	if s.playbackMode == mode {
		s.mu.Unlock()
		return
	}
	s.playbackMode = mode
	s.mu.Unlock()
	if s.OnPlaybackModeChanged != nil {
		s.OnPlaybackModeChanged()
	}
}

// Volume 音量 (0-100)。
func (s *MusicPlayerService) Volume() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.volume
}

// SetVolume 设置音量。
func (s *MusicPlayerService) SetVolume(value int) {
	s.mu.Lock()
	clamped := clamp(value, 0, 100)
	s.volume = clamped
	audio := s.audio
	s.mu.Unlock()
	if audio != nil {
		audio.SetVolume(clamped)
	}
}

// Position 当前播放位置（未在播放时为 0）。
func (s *MusicPlayerService) Position() time.Duration {
	audio := s.currentAudio()
	if audio == nil || s.State() == StateStopped {
		return 0
	}
	return audio.Position()
}

// Duration 当前曲目总时长（未知时为 0）。
func (s *MusicPlayerService) Duration() time.Duration {
	audio := s.currentAudio()
	if audio == nil {
		return 0
	}
	return audio.Duration()
}

// currentAudio 返回音频实现（仅在有活动播放时非 nil 的场合由实现方自行管理）。
func (s *MusicPlayerService) currentAudio() AudioPlayer {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateStopped {
		return nil
	}
	return s.audio
}

// Play 播放指定曲目（手动选择）。
func (s *MusicPlayerService) Play(track MusicTrack) error {
	if err := s.playCore(track); err != nil {
		return err
	}
	if s.OnTrackChanged != nil {
		s.OnTrackChanged()
	}
	s.fireStateChanged()
	return nil
}

// Pause 暂停当前播放（保留进度，可继续）。
func (s *MusicPlayerService) Pause() {
	s.mu.Lock()
	if s.state != StatePlaying {
		s.mu.Unlock()
		return
	}
	s.state = StatePaused
	audio := s.audio
	s.mu.Unlock()

	if audio != nil {
		audio.Pause()
	}
	s.fireStateChanged()
}

// Resume 从暂停位置恢复播放。
func (s *MusicPlayerService) Resume() error {
	s.mu.Lock()
	if s.state != StatePaused || s.currentTrack == nil {
		s.mu.Unlock()
		return nil
	}
	s.state = StatePlaying
	s.manualStop = false
	track := *s.currentTrack
	audio := s.audio
	s.mu.Unlock()

	if audio != nil {
		if err := audio.Resume(); err != nil {
			// 暂停时被清理的异常情况：直接重新打开文件
			if err := audio.Play(track.FilePath); err != nil {
				s.mu.Lock()
				s.state = StateStopped
				s.currentTrack = nil
				s.mu.Unlock()
				s.fireStateChanged()
				return err
			}
		}
	}
	s.fireStateChanged()
	return nil
}

// Stop 停止播放并归零进度。
func (s *MusicPlayerService) Stop() {
	s.stopCore(true)
}

// Next 播放列表中的下一首（按当前模式选曲，手动切换时顺序模式也会循环回第一首）。
// 返回是否成功切换。
func (s *MusicPlayerService) Next() bool {
	s.mu.Lock()
	currentIndex := indexOfTrack(s.playlist, s.currentTrack)
	nextIndex := SelectNextIndex(s.playbackMode, len(s.playlist), currentIndex, true, s.random)
	// 锁内取值拷贝（不取 &s.playlist[i]）：SetPlaylist 并发替换切片时，
	// 锁外解引用旧数组指针会读到被替换前的曲目
	var next MusicTrack
	hasNext := false
	if nextIndex >= 0 {
		next = s.playlist[nextIndex]
		hasNext = true
	}
	s.mu.Unlock()

	if !hasNext {
		return false
	}
	if err := s.playCore(next); err != nil {
		return false
	}
	if s.OnTrackChanged != nil {
		s.OnTrackChanged()
	}
	s.fireStateChanged()
	return true
}

// Previous 播放列表中的上一首（循环到末尾）。返回是否成功切换。
func (s *MusicPlayerService) Previous() bool {
	s.mu.Lock()
	if len(s.playlist) == 0 {
		s.mu.Unlock()
		return false
	}
	var previous MusicTrack
	if s.currentTrack == nil {
		previous = s.playlist[0]
	} else {
		index := indexOfTrack(s.playlist, s.currentTrack)
		previous = s.playlist[(index-1+len(s.playlist))%len(s.playlist)]
	}
	s.mu.Unlock()

	if err := s.playCore(previous); err != nil {
		return false
	}
	if s.OnTrackChanged != nil {
		s.OnTrackChanged()
	}
	s.fireStateChanged()
	return true
}

// Seek 跳转到指定位置。
func (s *MusicPlayerService) Seek(position time.Duration) {
	if position < 0 {
		return
	}
	audio := s.currentAudio()
	if audio == nil {
		return
	}
	audio.Seek(position)
}

// onNaturallyFinished 音频实现的自然播完回调（对应 C# OnPlaybackStopped）。
func (s *MusicPlayerService) onNaturallyFinished() {
	s.mu.Lock()
	// 仅自然播放完毕（非手动停止/暂停）才视为一曲结束
	naturallyFinished := !s.manualStop && s.state == StatePlaying
	next := (*MusicTrack)(nil)
	if naturallyFinished {
		s.state = StateStopped
		currentIndex := indexOfTrack(s.playlist, s.currentTrack)
		nextIndex := SelectNextIndex(s.playbackMode, len(s.playlist), currentIndex, false, s.random)
		if nextIndex >= 0 {
			// 锁内取值拷贝，理由同 Next
			nextCopy := s.playlist[nextIndex]
			next = &nextCopy
		}
	}
	s.mu.Unlock()

	if !naturallyFinished {
		s.fireStateChanged()
		return
	}

	if next != nil {
		// 按模式自动切歌（重复/循环/随机）
		if err := s.playCore(*next); err == nil {
			if s.OnTrackChanged != nil {
				s.OnTrackChanged()
			}
			s.fireStateChanged()
			return
		}
		s.fireStateChanged()
		return
	}

	// 顺序模式播完整个列表：通知
	if s.OnTrackFinished != nil {
		s.OnTrackFinished()
	}
	s.fireStateChanged()
}

// playCore 核心播放逻辑：打开新曲目并更新状态，但不触发任何事件（由调用方统一触发）。
func (s *MusicPlayerService) playCore(track MusicTrack) error {
	s.mu.Lock()
	audio := s.audio
	manualStop := false
	s.mu.Unlock()

	if audio != nil {
		audio.Stop() // 清理旧播放
	}

	s.mu.Lock()
	trackCopy := track
	s.currentTrack = &trackCopy
	s.state = StatePlaying
	s.manualStop = false
	s.mu.Unlock()

	if audio == nil {
		return nil
	}
	if err := audio.Play(track.FilePath); err != nil {
		s.mu.Lock()
		s.state = StateStopped
		s.currentTrack = nil
		s.manualStop = manualStop
		s.mu.Unlock()
		return err
	}
	return nil
}

// stopCore 停止播放。notify 为 true 时触发状态变化回调。
// manualStop 必须与 onNaturallyFinished 的读取同步，
// 否则自然播完与手动停止并发时可能被误判为"非手动"而自动切歌。
func (s *MusicPlayerService) stopCore(notify bool) {
	s.mu.Lock()
	s.manualStop = true
	audio := s.audio
	s.mu.Unlock()

	if audio != nil {
		audio.Stop()
	}
	s.mu.Lock()
	s.state = StateStopped
	s.mu.Unlock()

	if notify {
		s.fireStateChanged()
	}
}

func (s *MusicPlayerService) fireStateChanged() {
	if s.OnStateChanged != nil {
		s.OnStateChanged()
	}
}

// SelectNextIndex 纯逻辑版"下一首索引"，便于单元测试。
// playlistCount <= 0 或顺序模式播完列表尾部时返回 -1；
// currentIndex < 0（无当前曲目）时返回 0。
func SelectNextIndex(mode PlaybackMode, playlistCount, currentIndex int, manual bool, random *rand.Rand) int {
	if playlistCount <= 0 {
		return -1
	}
	if currentIndex < 0 {
		return 0
	}

	switch mode {
	case ModeShuffle:
		if random == nil {
			random = rand.New(rand.NewSource(time.Now().UnixNano()))
		}
		return random.Intn(playlistCount)
	case ModeRepeatOne:
		if manual {
			return (currentIndex + 1) % playlistCount
		}
		return currentIndex
	case ModeRepeatAll:
		return (currentIndex + 1) % playlistCount
	default: // Sequential
		if manual {
			return (currentIndex + 1) % playlistCount
		}
		if currentIndex >= playlistCount-1 {
			return -1
		}
		return currentIndex + 1
	}
}

// indexOfTrack 按值查找曲目在播放列表中的索引（字段值相等即命中）。
func indexOfTrack(playlist []MusicTrack, track *MusicTrack) int {
	if track == nil {
		return -1
	}
	for i := range playlist {
		if playlist[i] == *track {
			return i
		}
	}
	return -1
}

// ErrNoTrack 播放列表为空或没有可播放曲目。
var ErrNoTrack = errors.New("没有可播放的曲目")
