//go:build windows

package bindings

// 窗口透明 / 亚克力背景（DWM）运行时控制：
//
// Wails 的 WindowIsTranslucent 在系统层面落地为 WS_EX_NOREDIRECTIONBITMAP +
// DwmSetWindowAttribute(DWMWA_SYSTEMBACKDROP_TYPE)。后者支持运行时修改，因此
// "背景透明度"（前端改窗口底色 alpha）与"亚克力模糊"开关都能即时生效、无需重启。
//
// 注意：必须配合 main.go 的 DisableFramelessWindowDecorations: true。否则 Wails
// 会在每次 WM_ACTIVATE 调用 DwmExtendFrameIntoClientArea 重置窗口框架，干扰
// 透明/亚克力材质的渲染（这也是之前"透明不生效"的原因之一）。
//
// 关闭亚克力时把 backdrop 设为 DWMSBT_NONE：窗口保持清晰透明，直接透出桌面。
// 开启时设为 DWMSBT_ACRYLIC：DWM 在整个窗口范围绘制亚克力模糊，无需手动扩展
// 玻璃框架（系统 backdrop 本就画在整个窗口边界内）。
//
// Windows < 22621 不认识 DWMWA_SYSTEMBACKDROP_TYPE（DwmSetWindowAttribute 返回
// E_INVALIDARG），调用方据此回落到"写配置 + 重启进程"的旧路径。

import (
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"nekolauncher/internal/config"
)

// DWMWA_SYSTEMBACKDROP_TYPE 属性编号，以及 DWMSBT_* 桌面窗口管理器背景类型值。
const (
	dwmwaSystemBackdropType = 38
	dwmsbtNone              = 1 // 无系统背景：保持透明，直接透出桌面
	dwmsbtAcrylic           = 3 // 亚克力模糊

	dwmwaWindowCornerPreference = 33 // 圆角偏好
	dwmwcpRound                 = 2  // 圆角
)

// DwmExtendFrameIntoClientArea 把 DWM 玻璃框架扩展进客户区。margins 全 -1 表示
// 覆盖整个窗口。关键点：DisableFramelessWindowDecorations: true 之后 Wails 不再
// 自行扩展玻璃框架，而 DWMWA_SYSTEMBACKDROP_TYPE 的系统材质（亚克力）只绘制在
// 玻璃框架覆盖的区域内——没有玻璃框架时 backdrop 会静默不渲染，表现为
// "亚克力开了却只有清晰透明"。因此开启亚克力前先把框架铺满窗口。
var procDwmExtendFrameIntoClientArea = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmExtendFrameIntoClientArea")

type dwmMargins struct {
	cxLeftWidth, cxRightWidth, cyTopHeight, cyBottomHeight int32
}

// wailsWindowClass Wails 主窗口注册的窗口类名（见 wails window.go 的 windowClassName）。
const wailsWindowClass = "wailsWindow"

var (
	procEnumWindows              = syscall.NewLazyDLL("user32.dll").NewProc("EnumWindows")
	procGetClassNameW            = syscall.NewLazyDLL("user32.dll").NewProc("GetClassNameW")
	procGetWindowThreadProcessID = syscall.NewLazyDLL("user32.dll").NewProc("GetWindowThreadProcessId")
	procDwmSetWindowAttribute    = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	// 激活看门狗用的三个只读查询（见 watchWindowActivation）
	procGetForegroundWindow = syscall.NewLazyDLL("user32.dll").NewProc("GetForegroundWindow")
	procIsIconic            = syscall.NewLazyDLL("user32.dll").NewProc("IsIconic")
	procIsWindow            = syscall.NewLazyDLL("user32.dll").NewProc("IsWindow")
)

// applyAcrylicToWindow 把亚克力/透明的 DWM 参数写到指定窗口：
// enabled=true 铺满玻璃框架并开亚克力，false 撤销框架、保持清晰透明。
//
// 参数必须与启动时 fixupAcrylicBackdrop 设下的完全一致——一致的参数在 DWM 侧
// 是幂等操作，不会引起重绘，这是"重复应用不闪"的前提。反例就是 Wails 自己的
// ExtendFrameIntoClientArea（见 wails 的 win32/window.go）：它在每次 WM_ACTIVATE
// 把 margins 设成 1 像素，与这里的铺满值互相打架，才导致"窗口操作就闪一下"。
func applyAcrylicToWindow(hwnd uintptr, enabled bool) bool {
	if hwnd == 0 {
		return false
	}
	// 先铺满/撤销玻璃框架，让系统 backdrop 有可绘制的区域（见上）
	var margins dwmMargins
	if enabled {
		margins = dwmMargins{-1, -1, -1, -1}
	}
	ret, _, _ := procDwmExtendFrameIntoClientArea.Call(
		hwnd,
		uintptr(unsafe.Pointer(&margins)),
	)
	if ret != 0 {
		return false
	}
	backdrop := int32(dwmsbtNone)
	if enabled {
		backdrop = int32(dwmsbtAcrylic)
	}
	ret, _, _ = procDwmSetWindowAttribute.Call(
		hwnd,
		dwmwaSystemBackdropType,
		uintptr(unsafe.Pointer(&backdrop)),
		unsafe.Sizeof(backdrop),
	)
	return ret == 0
}

// applyAcrylicRuntime 在运行时切换窗口背景：enabled=true 用亚克力模糊，
// false 保持清晰透明。失败返回 false，由调用方回落重启流程。
func applyAcrylicRuntime(enabled bool) bool {
	return applyAcrylicToWindow(findLauncherWindow(), enabled)
}

// activationWatchInterval 激活看门狗的轮询间隔。两个只读 API 调用，开销可忽略。
const activationWatchInterval = 300 * time.Millisecond

// watchWindowActivation 在窗口重新成为前台时重设 DWM 参数。
//
// 要解决的问题：DWM 会在窗口"失去 / 重新获得前台"时（最小化后还原、Alt-Tab
// 切走再切回、托盘隐藏后唤回）重置扩展玻璃框架与系统 backdrop 状态。上游为了
// 治"窗口操作就闪一下"禁用了 Wails 的每次 WM_ACTIVATE 重设（它用 1px margins，
// 会把铺满窗口的框架改小、与亚克力 backdrop 冲突），代价是这些状态在激活变化后
// 再没有任何人维护——于是最小化 / Alt-Tab 回来就会看到背景与面板透明异常。
//
// 这里把这件事补回来：检测到"非前台 → 前台"的上升沿时，用与启动时完全相同的
// 参数重设一次。参数一致 → 幂等、不闪；状态被 DWM 重置时 → 恰好修复。
//
// 为什么用轮询而不是事件钩子：SetWinEventHook 的回调需要自建消息循环与线程
// 锁定，子类化 WNDPROC 要接管 Wails 的窗口过程并保证转发，两者都比"每 300ms
// 查两个只读状态"风险高得多，而本文件的既有风格也是轮询。
func watchWindowActivation() {
	go func() {
		var cached uintptr
		wasActive := false

		for {
			time.Sleep(activationWatchInterval)

			// 句柄只解析一次，之后用 IsWindow 校验；窗口重建时重新解析
			if cached == 0 || !isWindowAlive(cached) {
				cached = findLauncherWindow()
				wasActive = false
				if cached == 0 {
					continue
				}
			}

			foreground, _, _ := procGetForegroundWindow.Call()
			iconic, _, _ := procIsIconic.Call(cached)
			active := foreground == cached && iconic == 0

			// 只在上升沿重设，避免每个 tick 都去打扰 DWM
			if active && !wasActive {
				applyAcrylicToWindow(cached, config.AcrylicBackdropEnabled())
			}
			wasActive = active
		}
	}()
}

// isWindowAlive 句柄是否仍然有效。
func isWindowAlive(hwnd uintptr) bool {
	ok, _, _ := procIsWindow.Call(hwnd)

	return ok != 0
}

// fixupAcrylicBackdrop 启动后等待主窗口出现，再按配置重设一次 backdrop。
// 让"随窗口创建启用"与"运行时切换"走同一条可靠路径；其后由激活看门狗接手
// 维持（DWM 会在最小化 / Alt-Tab 时重置这些状态，见 watchWindowActivation）。
func fixupAcrylicBackdrop() {
	// 看门狗无条件启动：它自己解析窗口句柄，窗口创建稍晚也不受影响
	watchWindowActivation()

	go func() {
		var hwnd uintptr
		for i := 0; i < 150; i++ { // 最多等 15s，窗口创建即返回
			if hwnd = findLauncherWindow(); hwnd != 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if hwnd == 0 {
			return
		}
		// 关掉 Wails 装饰后自行补回 Win11 圆角（DWM 默认也多为圆角，这里显式确认）
		corner := int32(dwmwcpRound)
		procDwmSetWindowAttribute.Call(
			hwnd,
			dwmwaWindowCornerPreference,
			uintptr(unsafe.Pointer(&corner)),
			unsafe.Sizeof(corner),
		)
		applyAcrylicToWindow(hwnd, config.AcrylicBackdropEnabled())
	}()
}

// findLauncherWindow 在当前进程里找 Wails 主窗口句柄（按窗口类名 + PID 双重匹配）。
// 找不到返回 0。
//
// 枚举回调只创建一次：syscall.NewCallback 每次调用都会占用 Go 运行时回调表里的
// 一个槽位，而槽位永不回收（上限 2000）。这个方法每次轮询都会被调用（启动阶段
// 最多 150 次）加上每次切换亚克力开关，反复新建迟早会耗尽槽位并不可恢复地崩溃。
var (
	enumCallbackOnce sync.Once
	enumCallbackProc uintptr

	// 回调只能读写包级变量，用互斥锁把并发调用串起来
	enumMu        sync.Mutex
	enumSelfPID   uint32
	enumFoundHWND uintptr
)

func launcherWindowEnumProc(hwnd uintptr, _ uintptr) uintptr {
	var pid uint32

	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid != enumSelfPID {
		return 1 // 继续枚举
	}
	// 类名缓冲区给 256 字符足够容纳 "wailsWindow"
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if syscall.UTF16ToString(buf[:n]) == wailsWindowClass {
		enumFoundHWND = hwnd

		return 0 // 已找到，停止枚举
	}

	return 1
}

func findLauncherWindow() uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()

	enumCallbackOnce.Do(func() {
		enumCallbackProc = syscall.NewCallback(launcherWindowEnumProc)
	})
	enumSelfPID = uint32(os.Getpid())
	enumFoundHWND = 0
	procEnumWindows.Call(enumCallbackProc, 0)

	return enumFoundHWND
}
