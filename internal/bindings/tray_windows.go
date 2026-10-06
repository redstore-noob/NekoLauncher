//go:build windows

// 常驻系统托盘：单击图标显示主窗口，菜单提供显示 / 退出。
package bindings

import (
	"context"
	_ "embed"

	"github.com/energye/systray"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed tray_icon.ico
var trayIconBytes []byte

// TraySupported 当前平台是否提供系统托盘（Windows / Linux 为真）。
func TraySupported() bool { return true }

// StartTray 启动托盘图标（独立 goroutine 跑消息循环，与 Wails 主循环并行）。
func StartTray(ctx context.Context) {
	go systray.Run(func() {
		systray.SetIcon(trayIconBytes)
		systray.SetTooltip("NekoLauncher")
		systray.SetOnClick(func(_ systray.IMenu) {
			wailsruntime.WindowShow(ctx)
		})
		mShow := systray.AddMenuItem("显示启动器", "显示主窗口")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出 NekoLauncher", "退出启动器")
		mShow.Click(func() { wailsruntime.WindowShow(ctx) })
		mQuit.Click(func() {
			exitConfirmed.Store(true)
			wailsruntime.Quit(ctx)
		})
	}, func() {})
}

// StopTray 应用退出时清理托盘图标（否则图标会残留到鼠标划过）。
func StopTray() { systray.Quit() }
