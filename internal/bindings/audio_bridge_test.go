package bindings

import (
	"testing"
	"time"

	"nekolauncher/internal/music"
)

// TestAudioBridgeDrivesStateMachine 音频输出"由前端实现"这条链路的 Go 侧契约：
// 桥接把动作变成事件，前端把进度/播完回传，状态机据此自动切歌。
//
// 这条用例覆盖 P3-4 的结论：AudioPlayer 的生产实现就是 audioBridge
// （前端对端 frontend/src/lib/audioBridge.ts），Go 侧不再另写一份音频输出。
func TestAudioBridgeDrivesStateMachine(t *testing.T) {
	api := &MusicAPI{}
	bridge := &audioBridge{api: api}

	service := music.NewMusicPlayerService(bridge)
	service.SetPlaylist([]music.MusicTrack{
		{FilePath: "/music/a.mp3"},
		{FilePath: "/music/b.mp3"},
	})

	if err := service.Play(music.MusicTrack{FilePath: "/music/a.mp3"}); err != nil {
		t.Fatalf("播放失败：%v", err)
	}
	if state := service.State(); state != music.StatePlaying {
		t.Fatalf("状态 = %q，期望 Playing", state)
	}

	// 前端回传进度 → 桥接的 Position/Duration 立刻可见（Go 侧不再自己算时间）
	api.ReportPlaybackProgress(1000, 3000)
	if position := bridge.Position(); position != time.Second {
		t.Fatalf("Position = %v，期望 1s", position)
	}
	if duration := bridge.Duration(); duration != 3*time.Second {
		t.Fatalf("Duration = %v，期望 3s", duration)
	}

	// 自然播完 → 状态机自动切下一首（说明 SetOnFinished 真的接到了状态机）
	api.NotifyTrackFinished()
	if current := service.CurrentTrack(); current == nil || current.FilePath != "/music/b.mp3" {
		t.Fatalf("播完后应切到下一首，当前 = %+v", current)
	}

	// 暂停 / 恢复：桥接不阻塞状态机
	service.Pause()
	if state := service.State(); state != music.StatePaused {
		t.Fatalf("暂停后状态 = %q", state)
	}
	if err := service.Resume(); err != nil {
		t.Fatalf("恢复失败：%v", err)
	}
	if state := service.State(); state != music.StatePlaying {
		t.Fatalf("恢复后状态 = %q", state)
	}

	// 停止要顺手把进度归零，否则界面会停在"上次播放到 1 秒"
	service.Stop()
	if position := bridge.Position(); position != 0 {
		t.Fatalf("停止后 Position = %v，期望 0", position)
	}
	if state := service.State(); state != music.StateStopped {
		t.Fatalf("停止后状态 = %q", state)
	}
}

// TestAudioBridgeSeekAndVolume 跳转 / 音量只发事件，不改 Go 侧状态（前端才是执行者）。
func TestAudioBridgeSeekAndVolume(t *testing.T) {
	api := &MusicAPI{}
	bridge := &audioBridge{api: api}

	// ctx 未注入时 emit 是空操作：这里验证的是"不 panic 且不改动 Go 侧数值"
	bridge.Seek(5 * time.Second)
	bridge.SetVolume(42)

	api.ReportPlaybackProgress(2000, 4000)
	bridge.Seek(1500)
	if position := bridge.Position(); position != 2*time.Second {
		t.Fatalf("Seek 不该直接改 Go 侧进度（应等前端回传），得到 %v", position)
	}
}
