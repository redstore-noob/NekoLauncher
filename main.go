package main

import (
	"context"
	"embed"
	"time"

	"nekolauncher/internal/bindings"
	"nekolauncher/internal/config"
	"nekolauncher/internal/logs"
	"nekolauncher/internal/solo"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	api := bindings.New()
	// 恢复上次保存的窗口尺寸（未保存过时用默认值 760×480）
	windowWidth, windowHeight := config.LoadLauncherWindowSize()
	backdrop := windows.None
	if config.AcrylicBackdropEnabled() {
		backdrop = windows.Acrylic // 该功能(亚克力背景)仅Windows有效。
	}
	bgR, bgG, bgB := startupBackgroundColour()

	err := wails.Run(&options.App{
		Title:     "NekoLauncher",
		Frameless: true,
		Width:     windowWidth,
		Height:    windowHeight,
		MinWidth:  720,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: bindings.NewAssetFallbackHandler(),
		},
		BackgroundColour: &options.RGBA{R: bgR, G: bgG, B: bgB, A: 0},
		// 隐藏启动（治启动闪屏的关键）：窗口先不显示，等前端完成主题应用、
		// 配置 hydrate（窗口透明度/背景模式等）并渲染出首屏后再调
		// window.runtime.Show() 显示（见 frontend/src/layouts/index.tsx 的
		// WindowReveal）。此前窗口是"可见但空白"——用户会先看到透明/亚克力
		// 空窗，再看到 UI 弹出、透明度突变，全程都在闪。
		StartHidden: true,
		// 文件拖放：WebView2 的 HTML5 拖放拿不到磁盘路径（File.path 恒为空），
		// 必须走 Wails 原生 OnFileDrop 通道；EnableFileDrop 打开该通道，
		// DisableWebViewDrop 阻止 WebView 默认"打开拖入文件"的导航行为。
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		OnStartup: func(ctx context.Context) {
			// NekoSolo 安装标记必须在实例扫描（api.Startup）之前消费：
			// 它会接管游戏目录 / 捆绑 Java / 选中实例，首启扫描要看到这些配置
			if err := solo.ApplyStartupDefaults(); err != nil {
				logs.Write("WARN", err.Error())
			}
			// 把 Wails runtime ctx 注入各 API（事件桥接依赖），并执行启动初始化
			api.Startup(ctx)
			// 常驻托盘：单击显示主窗口，菜单可显示/退出
			bindings.StartTray(ctx)
			// 兜底：前端若因异常始终没能调 Show（脚本崩溃/插件阻塞），到点强制
			// 显示窗口，避免应用"启动了但什么都看不到"。窗口已显示时再调一次
			// WindowShow 无副作用。
			time.AfterFunc(5*time.Second, func() {
				wailsruntime.WindowShow(ctx)
			})
		},
		OnBeforeClose: func(ctx context.Context) bool {
			// 按「关闭按钮行为」设置分发：询问（前端弹窗）/ 托盘 / 直接退出
			return bindings.HandleBeforeClose(ctx)
		},
		OnShutdown: func(context.Context) {
			// API关闭，同时关闭托盘
			api.Shutdown()
			bindings.StopTray()
		},
		Bind: []interface{}{
			api.Config, api.Launcher, api.Download, api.Account,
			api.Instance, api.World, api.Content, api.Modpack,
			api.Music, api.Monitor, api.Server, api.ServerHost,
			api.Online, api.System, api.Plugin, api.Update,
		},
		Windows: &windows.Options{
			WindowIsTranslucent:  true,
			WebviewIsTransparent: true,
			BackdropType:         backdrop,
			// 关闭 Wails 的默认窗口框架装饰：不关的话它会在每次 WM_ACTIVATE
			// （窗口获得/失去焦点的每次点击、Alt-Tab）调 DwmExtendFrameIntoClientArea
			// 重置 DWM 框架，与透明/亚克力 backdrop 材质冲突——正是"窗口操作就闪
			// 一下"的来源。圆角等外观由 bindings/acrylic_windows.go 的
			// fixupAcrylicBackdrop 自行补回（见该文件注释）。
			DisableFramelessWindowDecorations: true,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
