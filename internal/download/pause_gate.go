// Package download 下载子系统：原版/Loader 安装器、Mod 下载、Java 运行时、
// 下载源选择与全局暂停控制。移植自 C# NyaLauncher.Core.Download。
package download

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// OnPauseStateChanged 状态变化回调（进入/退出暂停时各触发一次，任意 goroutine 触发）。
// 对应 C# DownloadPauseGate.StateChanged 事件；Wails 侧可在此转发 EventsEmit。
var OnPauseStateChanged func()

// pauseGate 全局下载暂停门。所有下载读取循环（原版/Loader 安装器、Mod 下载、
// Java 运行时）在每轮缓冲读取前调用 Wait：暂停期间速度自然归零、连接保持，
// 恢复后从原连接继续，不需要断点重试。
//
// 暂停状态是进程级全局开关（不区分具体任务），供任务详情窗口的
// 「暂停下载 / 继续下载」按钮使用；ctx 取消时 Wait 立即返回错误，
// 不会阻塞取消路径。
type pauseGate struct {
	paused atomic.Bool

	mu     sync.Mutex
	signal chan struct{}
}

var pauseGateInstance = &pauseGate{signal: make(chan struct{})}

// DownloadPauseGate 全局暂停门单例（对应 C# 静态类 DownloadPauseGate）。
var DownloadPauseGate = pauseGateInstance

// newResumeSignal 对应 C# TaskCompletionSource(RunContinuationsAsynchronously)：
// 唤醒信号是一个可关闭的 channel，close 即唤醒所有等待者。
func newResumeSignal() chan struct{} { return make(chan struct{}) }

// IsPaused 当前是否处于全局暂停状态。
func (g *pauseGate) IsPaused() bool { return g.paused.Load() }

// Pause 进入全局暂停（重复调用无副作用）。
func (g *pauseGate) Pause() {
	if !g.paused.CompareAndSwap(false, true) {
		return
	}
	if OnPauseStateChanged != nil {
		OnPauseStateChanged()
	}
}

// Resume 退出全局暂停并唤醒所有等待的读取循环（未暂停时调用无副作用）。
func (g *pauseGate) Resume() {
	if !g.paused.CompareAndSwap(true, false) {
		return
	}
	g.mu.Lock()
	signal := g.signal
	g.signal = newResumeSignal()
	g.mu.Unlock()
	close(signal)
	if OnPauseStateChanged != nil {
		OnPauseStateChanged()
	}
}

// Wait 暂停期间阻塞调用方，直到恢复或 ctx 取消；未暂停时立即返回。
// 应在下载读取循环的每轮读取前调用。
func (g *pauseGate) Wait(ctx context.Context) error {
	if !g.IsPaused() {
		return nil
	}
	// 兜底轮询用同一个 ticker：暂停可能持续几十分钟，循环里反复 time.After
	// 会一直新建定时器
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for g.IsPaused() {
		if err := ctx.Err(); err != nil {
			return err
		}
		g.mu.Lock()
		signal := g.signal
		g.mu.Unlock()
		select {
		case <-signal:
			// 已恢复或信号被刷新，回到循环顶部重新检查暂停状态
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// 与 C# 的 1s 兜底轮询一致，防止信号竞态导致漏唤醒
			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}
	return nil
}

// WaitPauseGate 包级便捷入口：下载读取循环统一调用。
func WaitPauseGate(ctx context.Context) error { return pauseGateInstance.Wait(ctx) }

// IsDownloadPaused 当前是否处于全局下载暂停（供 UI 轮询显示按钮状态）。
func IsDownloadPaused() bool { return pauseGateInstance.IsPaused() }
