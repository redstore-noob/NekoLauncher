package bindings

import (
	"time"

	"nekolauncher/internal/music"
)

// audioBridge music.AudioPlayer 的前端桥接实现：
// 所有播放动作转成 Wails 事件交给前端 Web Audio 执行；
// 进度由前端通过 Music.ReportPlaybackProgress 回传。
//
// P3-4 的结论（原 ROADMAP 里写作"未移植项 music.AudioPlayer"）：**音频输出就是前端实现，
// 不打算在 Go 侧再写一份**。理由：
//   - Go 标准库没有跨平台音频解码/输出，C# 版靠 NAudio（Windows）+ afplay/mpv（macOS/Linux），
//     移植进来要么拖一堆 cgo 依赖，要么在各个平台各写一套外部进程调用；
//   - Wails 的界面本来就是一个完整浏览器，Web Audio / <audio> 解码能力比任何外部进程都稳，
//     本地文件还能直接走应用内 /localfile 路由流式读取，不用先复制再喂给播放器；
//   - 状态机、播放列表、自动切歌仍在 Go 侧（music.Shared），跨页面/跨组件共享一份真相，
//     前端换掉只影响"出声"这一段。
//
// 因此 AudioPlayer 这个接口的**生产实现就是本文件**（前端对端见 frontend/src/lib/audioBridge.ts）。
type audioBridge struct {
	api *MusicAPI
}

// 编译期断言：桥接实现必须始终满足 music.AudioPlayer（接口改动会在这里立刻暴露）。
var _ music.AudioPlayer = (*audioBridge)(nil)

func (b *audioBridge) Play(filePath string) error {
	emit(b.api.ctx, "music:play", map[string]string{"filePath": filePath})
	return nil
}

func (b *audioBridge) Pause() { emit(b.api.ctx, "music:pause") }

func (b *audioBridge) Resume() error {
	emit(b.api.ctx, "music:resume")
	return nil
}

func (b *audioBridge) Stop() {
	emit(b.api.ctx, "music:stop")
	b.api.positionNs.Store(0)
}

func (b *audioBridge) Seek(position time.Duration) {
	emit(b.api.ctx, "music:seek", map[string]int64{"positionMs": position.Milliseconds()})
}

func (b *audioBridge) Position() time.Duration {
	return time.Duration(b.api.positionNs.Load())
}

func (b *audioBridge) Duration() time.Duration {
	return time.Duration(b.api.durationNs.Load())
}

func (b *audioBridge) SetVolume(percent int) {
	emit(b.api.ctx, "music:volume", map[string]int{"percent": percent})
}

// SetOnFinished 注册自然播完回调：由前端 Music.NotifyTrackFinished 触发。
func (b *audioBridge) SetOnFinished(callback func()) {
	b.api.finishedMu.Lock()
	b.api.onTrackFinished = callback
	b.api.finishedMu.Unlock()
}
