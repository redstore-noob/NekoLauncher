//go:build linux

// 常驻系统托盘（Linux：org.kde.StatusNotifierItem over D-Bus）。
//
// 与 Windows 实现的差异：
//   - 图标必须是 PNG：energye/systray 的 unix 分支只用 image/png 解码
//     （见 systray_unix.go 的 convertToPixels），.ico 解不出来，图标会变成空白。
//   - 不依赖 libappindicator / cgo：库直接讲 StatusNotifierItem 协议，
//     所以打包的依赖清单不需要增加任何包。可见性取决于托盘宿主是否实现 SNI
//     （KDE 原生支持；GNOME 需要 AppIndicator 扩展或 snixembed）。
//   - 库内部对"没有 D-Bus 会话总线"只在日志里报错，但随后的退出路径会在
//     nil 连接上取字段而 panic。托盘是可选功能，绝不能把主程序带崩，
//     因此整个消息循环跑在带 recover 的独立 goroutine 里。
package bindings

import (
	"context"
	_ "embed"
	"sync/atomic"

	"github.com/energye/systray"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"nekolauncher/internal/logs"
)

//go:embed tray_icon.png
var trayIconBytes []byte

// trayRunning 托盘消息循环是否已经起来（StopTray 据此决定要不要唤醒它）。
var trayRunning atomic.Bool

// TraySupported 当前平台是否提供系统托盘（Windows / Linux 为真）。
func TraySupported() bool { return true }

// StartTray 启动托盘图标（独立 goroutine 跑 D-Bus 消息循环，与 Wails 主循环并行）。
func StartTray(ctx context.Context) {
	trayRunning.Store(true)
	// systray.Run 会阻塞在内部循环直到 Quit：panic 全部留在本 goroutine 内，
	// 不能让它冒到进程级（托盘失败顶多没有图标，启动器必须照常可用）。
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logs.Write("WARN", "系统托盘不可用（可能没有 D-Bus 会话总线或托盘宿主）")
			}
		}()
		systray.Run(func() {
			systray.SetIcon(trayIconBytes)
			systray.SetTooltip("NekoLauncher")
			// 左键单击（SNI Activate）直接唤回主窗口
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
	}()
}

// StopTray 应用退出时清理托盘图标；未成功启动过时不做任何事，
// 避免在没有会话总线的环境里触发库的 nil 连接退出路径。
func StopTray() {
	if !trayRunning.CompareAndSwap(true, false) {
		return
	}
	systray.Quit()
}
