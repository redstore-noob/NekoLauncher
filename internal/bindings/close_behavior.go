package bindings

import (
	"context"
	"sync/atomic"

	"nekolauncher/internal/config"
	"nekolauncher/internal/logs"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// exitConfirmed 用户已明确确认退出（关闭询问选了「退出」、或托盘菜单退出）。
// 关闭钩子据此直接放行，避免退出时再弹一次询问。
var exitConfirmed atomic.Bool

// HandleBeforeClose Wails OnBeforeClose 钩子：返回 true 阻止关闭。
// 会在点击X之前记录一次窗口尺寸，下次启动Launcher时将会自动同步尺寸。
func HandleBeforeClose(ctx context.Context) bool {
	width, height := wailsruntime.WindowGetSize(ctx)
	_ = config.SaveGlobalWindowSize(width, height)

	if exitConfirmed.Load() {
		return false
	}
	switch config.GetValue("closeAction") {
	case "exit":
		exitConfirmed.Store(true)

		return false
	case "tray":
		// 没有托盘的平台（macOS 等）不能隐藏窗口：托盘菜单是唯一能把窗口显示
		// 回来的入口，缺了它应用就再也找不回来了（只剩杀进程一条路）。
		// 这类平台按"每次询问"处理，让用户自己选退出。
		if !TraySupported() {
			logs.Write("WARN", "当前平台没有系统托盘，关闭行为已按\"询问\"处理")
			wailsruntime.EventsEmit(ctx, "launcher:close-requested")

			return true
		}
		wailsruntime.WindowHide(ctx)

		return true
	default: // 未设置 = 每次询问：交给前端弹窗，选择后再调 Exit/Hide
		wailsruntime.EventsEmit(ctx, "launcher:close-requested")

		return true
	}
}

// ExitLauncher 确认退出（前端关闭询问 / 设置页预览）。
func (a *SystemAPI) ExitLauncher() {
	exitConfirmed.Store(true)
	if a.ctx != nil {
		wailsruntime.Quit(a.ctx)
	}
}

// HideLauncher 最小化到托盘（隐藏主窗口）。
//
// 没有托盘的平台（macOS 等）上没有"显示回来"的入口，隐藏等于把应用变成
// 只能杀进程的幽灵进程。此时按用户的"收起来"意图直接退出——前端的
// 最小化选项已在这些平台隐藏（见 TraySupported），这里只是兜底。
func (a *SystemAPI) HideLauncher() {
	if a.ctx == nil {
		return
	}
	if !TraySupported() {
		logs.Write("WARN", "当前平台没有系统托盘，\"最小化到托盘\"按退出处理")
		a.ExitLauncher()

		return
	}
	wailsruntime.WindowHide(a.ctx)
}
