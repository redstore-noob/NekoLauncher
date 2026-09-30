package music

import (
	"errors"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// fakeAudioPlayer 假音频输出：只记录调用与伪造状态，不产生任何声音。
// 生产环境这台"播放器"由前端实现（见 bindings.audioBridge），
// 这里用来驱动 Go 侧状态机并断言回调次数。
type fakeAudioPlayer struct {
	mu            sync.Mutex
	playCalls     []string
	playErr       error
	resumeErr     error
	pauseCalls    int
	stopCalls     int
	seekCalls     []time.Duration
	volumeCalls   []int
	position      time.Duration
	duration      time.Duration
	onFinished    func()
	lastPlayError error
}

func (fake *fakeAudioPlayer) Play(filePath string) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.playCalls = append(fake.playCalls, filePath)
	if fake.playErr != nil {
		fake.lastPlayError = fake.playErr
		return fake.playErr
	}
	return nil
}

func (fake *fakeAudioPlayer) Pause() {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.pauseCalls++
}

func (fake *fakeAudioPlayer) Resume() error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.resumeErr
}

func (fake *fakeAudioPlayer) Stop() {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.stopCalls++
	fake.position = 0
}

func (fake *fakeAudioPlayer) Seek(position time.Duration) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.seekCalls = append(fake.seekCalls, position)
	fake.position = position
}

func (fake *fakeAudioPlayer) Position() time.Duration {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.position
}

func (fake *fakeAudioPlayer) Duration() time.Duration {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.duration
}

func (fake *fakeAudioPlayer) SetVolume(percent int) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.volumeCalls = append(fake.volumeCalls, percent)
}

func (fake *fakeAudioPlayer) SetOnFinished(callback func()) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.onFinished = callback
}

// finish 模拟一次"自然播完"（前端回报播放结束）。
// 没注册回调时什么都不做，避免测试误以为状态机会自己往前走。
func (fake *fakeAudioPlayer) finish() {
	fake.mu.Lock()
	callback := fake.onFinished
	fake.mu.Unlock()
	if callback != nil {
		callback()
	}
}

func (fake *fakeAudioPlayer) playCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return len(fake.playCalls)
}

func (fake *fakeAudioPlayer) lastPlayed() string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.playCalls) == 0 {
		return ""
	}
	return fake.playCalls[len(fake.playCalls)-1]
}

func (fake *fakeAudioPlayer) stopCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.stopCalls
}

func (fake *fakeAudioPlayer) pauseCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.pauseCalls
}

func (fake *fakeAudioPlayer) seekCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return len(fake.seekCalls)
}

func (fake *fakeAudioPlayer) volumes() []int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	out := make([]int, len(fake.volumeCalls))
	copy(out, fake.volumeCalls)
	return out
}

// newPlayerWithTracks 造一个带假音频输出与固定播放列表的播放器。
func newPlayerWithTracks(t *testing.T, paths ...string) (*MusicPlayerService, *fakeAudioPlayer) {
	t.Helper()
	fake := &fakeAudioPlayer{duration: 3 * time.Minute}
	service := NewMusicPlayerService(fake)
	tracks := make([]MusicTrack, len(paths))
	for i, path := range paths {
		tracks[i] = MusicTrack{FilePath: path}
	}
	service.SetPlaylist(tracks)
	return service, fake
}

// countCallbacks 统计回调被触发的次数。
func countCallbacks(register func(func())) func() int {
	var mu sync.Mutex
	count := 0
	register(func() {
		mu.Lock()
		count++
		mu.Unlock()
	})
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return count
	}
}

// TestPlayerStateMachineWithoutAudioOutput 没有音频输出实现时（audio = nil），状态机照常走。
// 防的回归：把"必须有音频后端"写死进状态机 —— 那样前端接管播放后，
// Play/Pause/Stop 会直接 panic 或卡在 Stopped，界面永远显示未播放。
func TestPlayerStateMachineWithoutAudioOutput(t *testing.T) {
	service := NewMusicPlayerService(nil)
	if state := service.State(); state != StateStopped {
		t.Fatalf("新建播放器应为 Stopped，得到 %q", state)
	}
	if track := service.CurrentTrack(); track != nil {
		t.Fatalf("新建播放器不应有当前曲目，得到 %+v", track)
	}

	track := MusicTrack{FilePath: `C:\m\a.mp3`}
	if err := service.Play(track); err != nil {
		t.Fatalf("无音频后端时播放不该报错：%v", err)
	}
	if state := service.State(); state != StatePlaying {
		t.Fatalf("播放后状态应为 Playing，得到 %q", state)
	}
	current := service.CurrentTrack()
	if current == nil || current.FilePath != track.FilePath {
		t.Fatalf("当前曲目不符：%+v", current)
	}
	if position := service.Position(); position != 0 {
		t.Fatalf("无音频后端时进度应为 0，得到 %v", position)
	}
	if duration := service.Duration(); duration != 0 {
		t.Fatalf("无音频后端时时长应为 0，得到 %v", duration)
	}

	// Seek 只转发给音频实现，没有实现时应静默忽略（不能 panic）
	service.Seek(5 * time.Second)
	service.Seek(-time.Second)

	service.Pause()
	if state := service.State(); state != StatePaused {
		t.Fatalf("暂停后状态应为 Paused，得到 %q", state)
	}
	if err := service.Resume(); err != nil {
		t.Fatalf("恢复失败：%v", err)
	}
	if state := service.State(); state != StatePlaying {
		t.Fatalf("恢复后状态应为 Playing，得到 %q", state)
	}
	if current := service.CurrentTrack(); current == nil || current.FilePath != track.FilePath {
		t.Fatalf("恢复后当前曲目应保持不变，得到 %+v", current)
	}

	service.Stop()
	if state := service.State(); state != StateStopped {
		t.Fatalf("停止后状态应为 Stopped，得到 %q", state)
	}
	if err := service.Resume(); err != nil {
		t.Fatalf("停止后恢复不该报错：%v", err)
	}
	if state := service.State(); state != StateStopped {
		t.Fatalf("停止后 Resume 不应把状态改回播放，得到 %q", state)
	}
}

// TestPlayerVolumeClampingPropagatesToAudio 音量夹紧 0-100 并同步给音频实现。
// 防的回归：前端把音量滑到范围外（-10 / 200）时直接把非法值透给音频层，
// 造成爆音或整段静音且状态显示与实际不符；初始化时也要把默认音量下发一次。
func TestPlayerVolumeClampingPropagatesToAudio(t *testing.T) {
	service, fake := newPlayerWithTracks(t, `C:\m\a.mp3`)

	if volume := service.Volume(); volume != 80 {
		t.Fatalf("默认音量应为 80，得到 %d", volume)
	}
	if volumes := fake.volumes(); len(volumes) != 1 || volumes[0] != 80 {
		t.Fatalf("构造时应把默认音量下发给音频实现，得到 %v", volumes)
	}

	for _, testCase := range []struct {
		set  int
		want int
	}{{-10, 0}, {0, 0}, {55, 55}, {100, 100}, {200, 100}} {
		service.SetVolume(testCase.set)
		if got := service.Volume(); got != testCase.want {
			t.Fatalf("设置音量 %d 后应为 %d，得到 %d", testCase.set, testCase.want, got)
		}
	}
	volumes := fake.volumes()
	if len(volumes) != 6 {
		t.Fatalf("每次 SetVolume 都应同步给音频实现，调用记录 = %v", volumes)
	}
	if volumes[len(volumes)-1] != 100 {
		t.Fatalf("最后一次下发的音量应为夹紧后的 100，得到 %d", volumes[len(volumes)-1])
	}
}

// TestPlayerPauseResumeStopTransitions 暂停 / 恢复 / 停止的状态迁移与非法调用。
// 防的回归：停止状态下按暂停仍走到音频层、暂停状态被 Resume 之外的路径改回播放，
// 以及 Resume 失败后状态卡在"暂停"（界面按钮显示成"继续"，实际再点没反应）。
func TestPlayerPauseResumeStopTransitions(t *testing.T) {
	service, fake := newPlayerWithTracks(t, `C:\m\a.mp3`)

	onState := countCallbacks(func(callback func()) { service.OnStateChanged = callback })

	// 停止状态下暂停：不应触碰音频层，也不应发状态事件
	service.Pause()
	if calls := fake.pauseCount(); calls != 0 {
		t.Fatalf("未播放时暂停不该下发到底层，得到 %d 次调用", calls)
	}
	if got := onState(); got != 0 {
		t.Fatalf("未播放时暂停不该触发状态事件，得到 %d 次", got)
	}

	if err := service.Play(MusicTrack{FilePath: `C:\m\a.mp3`}); err != nil {
		t.Fatalf("播放失败：%v", err)
	}
	if job := fake.lastPlayed(); job != `C:\m\a.mp3` {
		t.Fatalf("播放路径未下发给音频实现：%q", job)
	}

	service.Pause()
	if state := service.State(); state != StatePaused {
		t.Fatalf("暂停后状态应为 Paused，得到 %q", state)
	}
	if calls := fake.pauseCount(); calls != 1 {
		t.Fatalf("暂停应下发 1 次到底层，得到 %d 次", calls)
	}

	// 连按两次暂停：第二次是空操作（底层不该被重复暂停）
	service.Pause()
	if calls := fake.pauseCount(); calls != 1 {
		t.Fatalf("重复暂停不该重复下发，得到 %d 次调用", calls)
	}

	if err := service.Resume(); err != nil {
		t.Fatalf("恢复失败：%v", err)
	}
	if state := service.State(); state != StatePlaying {
		t.Fatalf("恢复后状态应为 Playing，得到 %q", state)
	}

	service.Stop()
	if state := service.State(); state != StateStopped {
		t.Fatalf("停止后状态应为 Stopped，得到 %q", state)
	}
	if calls := fake.stopCount(); calls == 0 {
		t.Fatal("停止应下发到底层以释放资源")
	}
	if got := onState(); got < 4 {
		t.Fatalf("播放/暂停/恢复/停止都应触发状态事件，得到 %d 次", got)
	}

	// 停止后暂停不应把状态改成暂停
	service.Pause()
	if state := service.State(); state != StateStopped {
		t.Fatalf("停止后暂停应保持 Stopped，得到 %q", state)
	}
}

// TestPlayerResumeReopensWhenAudioLost 暂停期间底层被回收（Resume 报错）时重新打开文件。
// 防的回归：系统休眠/音频设备切换后恢复播放直接失败，用户看到"继续"按钮点了没反应。
func TestPlayerResumeReopensWhenAudioLost(t *testing.T) {
	service, fake := newPlayerWithTracks(t, `C:\m\a.mp3`)
	track := MusicTrack{FilePath: `C:\m\a.mp3`}
	if err := service.Play(track); err != nil {
		t.Fatalf("播放失败：%v", err)
	}
	service.Pause()

	fake.mu.Lock()
	fake.resumeErr = errors.New("音频设备已被回收")
	fake.mu.Unlock()

	if err := service.Resume(); err != nil {
		t.Fatalf("Resume 报错时应回退到重新打开文件，而不是返回错误：%v", err)
	}
	if state := service.State(); state != StatePlaying {
		t.Fatalf("重新打开后状态应为 Playing，得到 %q", state)
	}
	if calls := fake.playCount(); calls != 2 {
		t.Fatalf("应重新 Play 一次（首次 + 重开），得到 %d 次", calls)
	}
}

// TestPlayerResumeFailureStopsAndRecordsError Resume 与重开都失败时返回错误并停止。
// 防的回归：错误被吞掉后界面显示"播放中"但没有任何声音，
// 用户完全不知道发生了什么（返回的 error 就是这条链路的可观测出口）。
func TestPlayerResumeFailureStopsAndRecordsError(t *testing.T) {
	service, fake := newPlayerWithTracks(t, `C:\m\a.mp3`)
	track := MusicTrack{FilePath: `C:\m\a.mp3`}
	if err := service.Play(track); err != nil {
		t.Fatalf("播放失败：%v", err)
	}
	service.Pause()

	onState := countCallbacks(func(callback func()) { service.OnStateChanged = callback })
	fake.mu.Lock()
	fake.resumeErr = errors.New("设备已失效")
	fake.playErr = errors.New("文件已被删除")
	fake.mu.Unlock()

	err := service.Resume()
	if err == nil {
		t.Fatal("恢复与重开都失败时应返回错误")
	}
	if state := service.State(); state != StateStopped {
		t.Fatalf("恢复失败后应回到 Stopped，得到 %q", state)
	}
	if current := service.CurrentTrack(); current != nil {
		t.Fatalf("恢复失败后不应保留当前曲目，得到 %+v", current)
	}
	if got := onState(); got == 0 {
		t.Fatal("恢复失败也应触发状态事件，否则界面停留在'暂停'")
	}
}

// TestPlayerPlayFailureRecordsErrorAndKeepsStopped 播放入口失败时记录原因、清空当前曲目、不发事件。
// 防的回归：坏文件被当成"正在播放"，界面显示曲目名却永远没声音；
// 以及失败路径误发 OnTrackChanged 让列表高亮到一首根本没播起来的歌。
func TestPlayerPlayFailureRecordsErrorAndKeepsStopped(t *testing.T) {
	service, fake := newPlayerWithTracks(t, `C:\m\missing.mp3`)
	fake.playErr = errors.New("文件不存在")

	onTrackChanged := countCallbacks(func(callback func()) { service.OnTrackChanged = callback })
	onState := countCallbacks(func(callback func()) { service.OnStateChanged = callback })

	if err := service.Play(MusicTrack{FilePath: `C:\m\missing.mp3`}); err == nil {
		t.Fatal("底层播放失败时 Play 应返回错误")
	}
	if state := service.State(); state != StateStopped {
		t.Fatalf("播放失败后状态应为 Stopped，得到 %q", state)
	}
	if current := service.CurrentTrack(); current != nil {
		t.Fatalf("播放失败后当前曲目应清空，得到 %+v", current)
	}
	if got := onTrackChanged(); got != 0 {
		t.Fatalf("播放失败不该触发曲目切换事件，得到 %d 次", got)
	}
	if got := onState(); got != 0 {
		t.Fatalf("播放失败不该触发状态事件（状态仍是 Stopped），得到 %d 次", got)
	}
}

// TestPlayerAutoAdvanceNotifiesOnlyAtSequentialEnd 顺序播放自动切歌，只有走完整个列表才通知"播完"。
// 防的回归（本包最关键的一条业务规则）：顺序模式中途切歌就触发 OnTrackFinished，
// 前端会误以为整张列表播完（自动停播/弹提示）；反过来漏掉末尾那次通知，
// 界面会永远停在最后一首的"播放中"。
func TestPlayerAutoAdvanceNotifiesOnlyAtSequentialEnd(t *testing.T) {
	service, fake := newPlayerWithTracks(t, `C:\m\a.mp3`, `C:\m\b.mp3`)

	onFinished := countCallbacks(func(callback func()) { service.OnTrackFinished = callback })
	onTrackChanged := countCallbacks(func(callback func()) { service.OnTrackChanged = callback })

	if err := service.Play(MusicTrack{FilePath: `C:\m\a.mp3`}); err != nil {
		t.Fatalf("播放失败：%v", err)
	}
	if got := onTrackChanged(); got != 1 {
		t.Fatalf("手动播放应触发一次曲目切换事件，得到 %d 次", got)
	}

	// 第一首自然播完：还有下一首 → 自动切歌，不通知"播完"
	fake.finish()
	if current := service.CurrentTrack(); current == nil || current.FilePath != `C:\m\b.mp3` {
		t.Fatalf("播完第一首应自动切到 b.mp3，得到 %+v", current)
	}
	if state := service.State(); state != StatePlaying {
		t.Fatalf("自动切歌后应为 Playing，得到 %q", state)
	}
	if got := onFinished(); got != 0 {
		t.Fatalf("顺序播放中途不该触发 OnTrackFinished，得到 %d 次", got)
	}
	if got := onTrackChanged(); got != 2 {
		t.Fatalf("自动切歌应触发一次曲目切换事件，得到 %d 次", got)
	}

	// 最后一首自然播完：列表到头 → 停下 + 通知"播完"
	fake.finish()
	if got := onFinished(); got != 1 {
		t.Fatalf("顺序模式播完整个列表应恰好通知一次，得到 %d 次", got)
	}
	if state := service.State(); state != StateStopped {
		t.Fatalf("播完列表后应为 Stopped，得到 %q", state)
	}
	if position := service.Position(); position != 0 {
		t.Fatalf("停止状态下进度应为 0，得到 %v", position)
	}
	if got := onTrackChanged(); got != 2 {
		t.Fatalf("播完列表不该再触发曲目切换，得到 %d 次", got)
	}

	// 再补一次"播完"回调（前端可能重复回报）：不应重复通知
	fake.finish()
	if got := onFinished(); got != 1 {
		t.Fatalf("Stopped 状态下的回调应被忽略，得到 %d 次通知", got)
	}
}

// TestPlayerManualStopNeverAutoAdvances 手动停止后底层回报"播完"不得自动切歌。
// 防的回归：Stop 时底层停止动作会顺带触发一次 finished 回调，
// 状态机把它误判成自然播完，于是"点了停止又自己唱起来"。
func TestPlayerManualStopNeverAutoAdvances(t *testing.T) {
	service, fake := newPlayerWithTracks(t, `C:\m\a.mp3`, `C:\m\b.mp3`)
	track := MusicTrack{FilePath: `C:\m\a.mp3`}
	if err := service.Play(track); err != nil {
		t.Fatalf("播放失败：%v", err)
	}

	onFinished := countCallbacks(func(callback func()) { service.OnTrackFinished = callback })
	service.Stop()

	playsBefore := fake.playCount()
	fake.finish()
	if got := fake.playCount(); got != playsBefore {
		t.Fatalf("手动停止后底层回报播完不该重新播放，播放次数 %d → %d", playsBefore, got)
	}
	if state := service.State(); state != StateStopped {
		t.Fatalf("手动停止后状态应保持 Stopped，得到 %q", state)
	}
	if current := service.CurrentTrack(); current == nil || current.FilePath != track.FilePath {
		t.Fatalf("手动停止后仍应能显示刚播的曲目，得到 %+v", current)
	}
	if got := onFinished(); got != 0 {
		t.Fatalf("手动停止不该算作播完列表，得到 %d 次通知", got)
	}

	// 暂停期间回报播完同样应被忽略（state != Playing）
	if err := service.Play(track); err != nil {
		t.Fatalf("重新播放失败：%v", err)
	}
	service.Pause()
	playsBefore = fake.playCount()
	fake.finish()
	if got := fake.playCount(); got != playsBefore {
		t.Fatalf("暂停期间回报播完不该自动切歌，播放次数 %d → %d", playsBefore, got)
	}
}

// TestPlayerRepeatModesAdvanceOnFinish 列表循环 / 单曲循环 / 随机模式下播完自动续播且永不报"播完"。
// 防的回归：循环模式在列表末尾被当成顺序模式停住（用户开着单曲循环却只放了一遍），
// 以及循环模式误触发 OnTrackFinished 让前端弹出"播放结束"。
func TestPlayerRepeatModesAdvanceOnFinish(t *testing.T) {
	t.Run("列表循环绕回第一首", func(t *testing.T) {
		service, fake := newPlayerWithTracks(t, `C:\m\a.mp3`, `C:\m\b.mp3`)

		onFinished := countCallbacks(func(callback func()) { service.OnTrackFinished = callback })
		onMode := countCallbacks(func(callback func()) { service.OnPlaybackModeChanged = callback })
		// 先挂回调再切模式：这次切换必须通知一次（前端要更新模式按钮）
		service.SetPlaybackMode(ModeRepeatAll)
		if got := onMode(); got != 1 {
			t.Fatalf("切换播放模式应通知一次，得到 %d 次", got)
		}

		if err := service.Play(MusicTrack{FilePath: `C:\m\b.mp3`}); err != nil {
			t.Fatalf("播放失败：%v", err)
		}
		fake.finish()
		if current := service.CurrentTrack(); current == nil || current.FilePath != `C:\m\a.mp3` {
			t.Fatalf("列表末尾播完应回到第一首，得到 %+v", current)
		}
		if state := service.State(); state != StatePlaying {
			t.Fatalf("列表循环应持续播放，状态 = %q", state)
		}
		if got := onFinished(); got != 0 {
			t.Fatalf("列表循环不该触发 OnTrackFinished，得到 %d 次", got)
		}

		// 模式未变化时重复设置不应重复发事件（前端会重复刷新）
		service.SetPlaybackMode(ModeRepeatAll)
		if got := onMode(); got != 1 {
			t.Fatalf("只有模式真的改变才发一次事件，得到 %d 次", got)
		}
	})

	t.Run("单曲循环重播同一首", func(t *testing.T) {
		service, fake := newPlayerWithTracks(t, `C:\m\a.mp3`, `C:\m\b.mp3`)
		service.SetPlaybackMode(ModeRepeatOne)

		onFinished := countCallbacks(func(callback func()) { service.OnTrackFinished = callback })
		if err := service.Play(MusicTrack{FilePath: `C:\m\a.mp3`}); err != nil {
			t.Fatalf("播放失败：%v", err)
		}
		fake.finish()
		if current := service.CurrentTrack(); current == nil || current.FilePath != `C:\m\a.mp3` {
			t.Fatalf("单曲循环应重播同一首，得到 %+v", current)
		}
		if state := service.State(); state != StatePlaying {
			t.Fatalf("单曲循环应持续播放，状态 = %q", state)
		}
		if got := fake.playCount(); got != 2 {
			t.Fatalf("单曲循环应重新下发一次播放，得到 %d 次", got)
		}
		if got := onFinished(); got != 0 {
			t.Fatalf("单曲循环不该触发 OnTrackFinished，得到 %d 次", got)
		}
	})

	t.Run("随机播放一直续播", func(t *testing.T) {
		service, fake := newPlayerWithTracks(t, `C:\m\a.mp3`, `C:\m\b.mp3`)
		service.SetPlaybackMode(ModeShuffle)
		// 固定种子，让这条用例可复现
		service.random = rand.New(rand.NewSource(1))

		onFinished := countCallbacks(func(callback func()) { service.OnTrackFinished = callback })
		if err := service.Play(MusicTrack{FilePath: `C:\m\a.mp3`}); err != nil {
			t.Fatalf("播放失败：%v", err)
		}
		for i := 0; i < 10; i++ {
			fake.finish()
		}
		if state := service.State(); state != StatePlaying {
			t.Fatalf("随机播放应持续续播，状态 = %q", state)
		}
		if got := onFinished(); got != 0 {
			t.Fatalf("随机播放不该触发 OnTrackFinished，得到 %d 次", got)
		}
		if got := fake.playCount(); got <= 1 {
			t.Fatalf("随机播放应在播完后重新选曲，播放次数 = %d", got)
		}
	})
}

// TestPlayerNextAndPreviousWrapping 下一首 / 上一首的边界（含空列表与单曲列表）。
// 防的回归：空播放列表按"下一首"下标越界 panic；
// 手动切换在顺序模式末尾停住（用户按下一首却什么都没发生）。
func TestPlayerNextAndPreviousWrapping(t *testing.T) {
	t.Run("手动下一首在末尾绕回第一首", func(t *testing.T) {
		service, fake := newPlayerWithTracks(t, `C:\m\a.mp3`, `C:\m\b.mp3`)
		if err := service.Play(MusicTrack{FilePath: `C:\m\b.mp3`}); err != nil {
			t.Fatalf("播放失败：%v", err)
		}
		if !service.Next() {
			t.Fatal("顺序模式手动切下一首应成功")
		}
		if current := service.CurrentTrack(); current == nil || current.FilePath != `C:\m\a.mp3` {
			t.Fatalf("列表末尾点下一首应回到第一首，得到 %+v", current)
		}
		if got := fake.playCount(); got != 2 {
			t.Fatalf("切歌应重新下发播放，得到 %d 次", got)
		}
	})

	t.Run("上一首在开头绕到末尾", func(t *testing.T) {
		service, _ := newPlayerWithTracks(t, `C:\m\a.mp3`, `C:\m\b.mp3`, `C:\m\c.mp3`)
		if err := service.Play(MusicTrack{FilePath: `C:\m\a.mp3`}); err != nil {
			t.Fatalf("播放失败：%v", err)
		}
		if !service.Previous() {
			t.Fatal("上一首应成功")
		}
		if current := service.CurrentTrack(); current == nil || current.FilePath != `C:\m\c.mp3` {
			t.Fatalf("列表开头点上一首应绕到末尾，得到 %+v", current)
		}
		if !service.Previous() {
			t.Fatal("上一首应成功")
		}
		if current := service.CurrentTrack(); current == nil || current.FilePath != `C:\m\b.mp3` {
			t.Fatalf("上一首应回到 b.mp3，得到 %+v", current)
		}
	})

	t.Run("空列表切歌安全返回 false", func(t *testing.T) {
		service := NewMusicPlayerService(nil)
		if service.Next() {
			t.Fatal("空播放列表按下一首应返回 false")
		}
		if service.Previous() {
			t.Fatal("空播放列表按上一首应返回 false")
		}
		if state := service.State(); state != StateStopped {
			t.Fatalf("空列表切歌后状态应保持 Stopped，得到 %q", state)
		}
	})

	t.Run("单曲列表切歌回到自己", func(t *testing.T) {
		service, _ := newPlayerWithTracks(t, `C:\m\only.mp3`)
		if !service.Next() {
			t.Fatal("单曲列表按下一首应重播该曲")
		}
		if current := service.CurrentTrack(); current == nil || current.FilePath != `C:\m\only.mp3` {
			t.Fatalf("单曲列表切歌应仍在同一首，得到 %+v", current)
		}
		if !service.Previous() {
			t.Fatal("单曲列表按上一首应成功")
		}
	})

	t.Run("当前曲目不在列表时下一首选第一首", func(t *testing.T) {
		service, _ := newPlayerWithTracks(t, `C:\m\a.mp3`, `C:\m\b.mp3`)
		if err := service.Play(MusicTrack{FilePath: `C:\m\outside.mp3`}); err != nil {
			t.Fatalf("播放失败：%v", err)
		}
		if !service.Next() {
			t.Fatal("应能切换到列表内曲目")
		}
		if current := service.CurrentTrack(); current == nil || current.FilePath != `C:\m\a.mp3` {
			t.Fatalf("当前曲目不在列表时应从第一首开始，得到 %+v", current)
		}
	})

	t.Run("切歌失败时保留当前曲目", func(t *testing.T) {
		service, fake := newPlayerWithTracks(t, `C:\m\a.mp3`, `C:\m\b.mp3`)
		if err := service.Play(MusicTrack{FilePath: `C:\m\a.mp3`}); err != nil {
			t.Fatalf("播放失败：%v", err)
		}
		fake.playErr = errors.New("下一首文件损坏")
		if service.Next() {
			t.Fatal("底层播放失败时 Next 应返回 false")
		}
	})
}

// TestPlayerSeekForwardsOnlyWhenPlaying 跳转只在有活动播放时下发给音频实现。
// 防的回归：未播放 / 已停止时拖动进度条仍把 Seek 发给底层，
// 前端 AudioPlayer 因此对着空音源乱跳，恢复播放时进度错位。
func TestPlayerSeekForwardsOnlyWhenPlaying(t *testing.T) {
	service, fake := newPlayerWithTracks(t, `C:\m\a.mp3`)

	service.Seek(10 * time.Second)
	if got := fake.seekCount(); got != 0 {
		t.Fatalf("未播放时不该下发 Seek，得到 %d 次", got)
	}

	if err := service.Play(MusicTrack{FilePath: `C:\m\a.mp3`}); err != nil {
		t.Fatalf("播放失败：%v", err)
	}
	service.Seek(10 * time.Second)
	if got := fake.seekCount(); got != 1 {
		t.Fatalf("播放中 Seek 应下发到底层，得到 %d 次", got)
	}

	// 负进度是非法输入，直接忽略
	service.Seek(-time.Second)
	if got := fake.seekCount(); got != 1 {
		t.Fatalf("负进度不该下发，得到 %d 次", got)
	}

	service.Pause()
	service.Seek(20 * time.Second)
	if got := fake.seekCount(); got != 2 {
		t.Fatalf("暂停时（仍算活动播放）应允许跳转，得到 %d 次", got)
	}

	service.Stop()
	service.Seek(30 * time.Second)
	if got := fake.seekCount(); got != 2 {
		t.Fatalf("停止后不该下发 Seek，得到 %d 次", got)
	}
}

// TestPlayerPlaylistIsACopy 播放列表读写都走副本，外部改动切片不影响服务内部状态。
// 防的回归：调用方拿到内部切片后 append/改元素，
// 让自动切歌读到被外部篡改的列表（曲目突然变成空路径）。
func TestPlayerPlaylistIsACopy(t *testing.T) {
	service := NewMusicPlayerService(nil)
	service.SetPlaylist([]MusicTrack{{FilePath: `C:\m\a.mp3`}})

	snapshot := service.Playlist()
	if len(snapshot) != 1 {
		t.Fatalf("播放列表长度应为 1，得到 %d", len(snapshot))
	}
	snapshot[0] = MusicTrack{FilePath: `C:\m\tampered.mp3`}

	if got := service.Playlist()[0].FilePath; got != `C:\m\a.mp3` {
		t.Fatalf("外部修改返回值污染了内部播放列表：%q", got)
	}

	// nil 列表要归一成空切片（前端 JSON 化时 nil 会变成 null）
	service.SetPlaylist(nil)
	if playlist := service.Playlist(); playlist == nil || len(playlist) != 0 {
		t.Fatalf("nil 播放列表应归一成空切片，得到 %#v", playlist)
	}
}

// TestSelectNextIndexPureLogic 纯逻辑选曲：所有模式 / 手动与自动 / 边界下标。
// 防的回归：这是自动切歌的唯一决策点，顺序模式末尾返回 0（无限循环）
// 或单曲循环手动切歌返回自己（按下一首没反应）都会在这里暴露。
func TestSelectNextIndexPureLogic(t *testing.T) {
	random := rand.New(rand.NewSource(7))

	cases := []struct {
		name          string
		mode          PlaybackMode
		playlistCount int
		currentIndex  int
		manual        bool
		want          int
	}{
		{"空列表", ModeSequential, 0, 0, false, -1},
		{"负数长度", ModeSequential, -3, 0, false, -1},
		{"无当前曲目从第一首开始", ModeSequential, 5, -1, false, 0},
		{"顺序自动前进", ModeSequential, 3, 0, false, 1},
		{"顺序自动到末尾停止", ModeSequential, 3, 2, false, -1},
		{"顺序手动在末尾绕回", ModeSequential, 3, 2, true, 0},
		{"列表循环自动绕回", ModeRepeatAll, 3, 2, false, 0},
		{"列表循环手动前进", ModeRepeatAll, 3, 1, true, 2},
		{"单曲循环自动停在原地", ModeRepeatOne, 3, 1, false, 1},
		{"单曲循环手动切下一首", ModeRepeatOne, 3, 1, true, 2},
		{"单曲循环手动在末尾绕回", ModeRepeatOne, 3, 2, true, 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := SelectNextIndex(
				testCase.mode, testCase.playlistCount, testCase.currentIndex, testCase.manual, random)
			if got != testCase.want {
				t.Fatalf("SelectNextIndex(%q, %d, %d, manual=%v) = %d，期望 %d",
					testCase.mode, testCase.playlistCount, testCase.currentIndex, testCase.manual, got, testCase.want)
			}
		})
	}

	// 随机模式：结果必须始终落在列表范围内；固定种子下 20 次里至少出现 2 种取值，
	// 否则说明"随机"退化成固定下标
	seen := map[int]bool{}
	for i := 0; i < 20; i++ {
		got := SelectNextIndex(ModeShuffle, 4, 0, false, random)
		if got < 0 || got >= 4 {
			t.Fatalf("随机模式返回越界下标 %d", got)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Fatalf("随机模式始终返回同一下标（%v），随机播放已退化", seen)
	}

	// random 传 nil 时内部兜底创建，不能 panic
	if got := SelectNextIndex(ModeShuffle, 3, 0, false, nil); got < 0 || got >= 3 {
		t.Fatalf("random 为 nil 时应内部兜底，得到 %d", got)
	}
}

// TestIndexOfTrackMatchesByValue 按字段值查找曲目下标。
// 防的回归：改成按指针/路径字符串比较后，
// 界面新构造的等值 MusicTrack 在列表里找不到，自动切歌永远从头开始。
func TestIndexOfTrackMatchesByValue(t *testing.T) {
	playlist := []MusicTrack{
		{FilePath: `C:\m\a.mp3`},
		{FilePath: `C:\m\b.mp3`},
	}

	if got := indexOfTrack(playlist, nil); got != -1 {
		t.Fatalf("无当前曲目应返回 -1，得到 %d", got)
	}
	if got := indexOfTrack(nil, &playlist[0]); got != -1 {
		t.Fatalf("空列表应返回 -1，得到 %d", got)
	}
	match := playlist[1]
	if got := indexOfTrack(playlist, &match); got != 1 {
		t.Fatalf("等值曲目应命中下标 1，得到 %d", got)
	}
	missing := MusicTrack{FilePath: `C:\m\z.mp3`}
	if got := indexOfTrack(playlist, &missing); got != -1 {
		t.Fatalf("不在列表中的曲目应返回 -1，得到 %d", got)
	}
}
