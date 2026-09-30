package bindings

import (
	"context"
	"sync/atomic"

	"nekolauncher/internal/config"

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
func (a *SystemAPI) HideLauncher() {
	if a.ctx != nil {
		wailsruntime.WindowHide(a.ctx)
	}
}
