//go:build windows

package singleinstance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// wailsWindowClass 是 Wails 主窗口注册的窗口类名（与
// internal/bindings/acrylic_windows.go 中的同名常量一致）。用它跨进程识别
// 「已有的启动器窗口」——第二个实例必须能看见第一个实例的窗口，
// 所以这里不能像 acrylic 那样按 PID 过滤。
const wailsWindowClass = "wailsWindow"

var (
	procCreateMutexW             = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateMutexW")
	procEnumWindows              = windows.NewLazySystemDLL("user32.dll").NewProc("EnumWindows")
	procGetClassNameW            = windows.NewLazySystemDLL("user32.dll").NewProc("GetClassNameW")
	procShowWindow               = windows.NewLazySystemDLL("user32.dll").NewProc("ShowWindow")
	procIsWindowVisible          = windows.NewLazySystemDLL("user32.dll").NewProc("IsWindowVisible")
	procIsIconic                 = windows.NewLazySystemDLL("user32.dll").NewProc("IsIconic")
	procSetForegroundWindow      = windows.NewLazySystemDLL("user32.dll").NewProc("SetForegroundWindow")
	procBringWindowToTop         = windows.NewLazySystemDLL("user32.dll").NewProc("BringWindowToTop")
	procAllowSetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("AllowSetForegroundWindow")
)

const (
	swRestore = 9

	// ASFW_ANY 传给 AllowSetForegroundWindow，表示允许任意进程抢占前台。
	// 第二个实例通常本来就是用户刚双击拉起来的（因此已有前台权限），
	// 这一步是兜底，避免被前台的其它窗口挡住导致「闪一下就没了」。
	asfwAny = ^uintptr(0)
)

// mutexName 生成绑定到 exe 绝对路径的互斥体名。
//
// 用 hash 而不是路径原文：互斥体名里出现反斜杠属于保留字符（会被解释成
// 命名空间分隔），hash 同时把名字长度压到可控范围。
//
// 不加 "Global\" 前缀 —— 默认就是本会话作用域，不同 Windows 用户各自
// 跑一份互斥体是正确行为，不会互相阻挡。
func mutexName() string {
	path := executablePath()
	sum := sha256.Sum256([]byte(strings.ToLower(path)))

	return "NekoLauncher_single_instance_" + hex.EncodeToString(sum[:])
}

// executablePath 返回 exe 的绝对路径；拿不到时回落成空串（所有异常情况
// 共用一个互斥体名，宁可误挡也不放任并发写配置）。
func executablePath() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}

	return path
}

func acquire() Result {
	name, err := windows.UTF16PtrFromString(mutexName())
	if err != nil {
		return Result{Cleanup: func() {}}
	}

	// 第二个参数 bInitialOwner=false：不要求成为持有者，只关心「是否已存在」。
	// 是否已有实例完全由 ERROR_ALREADY_EXISTS 判定，不依赖 WaitForSingleObject，
	// 因此不存在「第一个实例没释放导致死等」的问题。
	handle, _, callErr := procCreateMutexW.Call(
		0,
		0,
		uintptr(unsafe.Pointer(name)),
	)
	if handle == 0 {
		return Result{Cleanup: func() {}}
	}

	cleanup := func() {
		_ = windows.CloseHandle(windows.Handle(handle))
	}

	if errors.Is(callErr, windows.ERROR_ALREADY_EXISTS) {
		// 已是第二个实例：句柄不再需要，先关掉再激活已有窗口。
		cleanup()

		// 用户双击第二次的真实意图是「把那个窗口给我拿出来」，
		// 所以这里不提「已有实例在运行」，直接把窗口显到前台。
		activateExistingWindow()

		return Result{AlreadyRunning: true, Cleanup: func() {}}
	}

	return Result{Cleanup: cleanup}
}

// --- 把已有窗口提到前台 ---------------------------------------------------

var (
	enumCallbackOnce sync.Once
	enumCallbackProc uintptr

	enumMu        sync.Mutex
	enumFoundHWND uintptr
)

// activateExistingWindow 找到已有的启动器窗口并让它可见、置前。
// 找不到时静默返回——此时调用方仍然会退出，用户看到的现象是
// 「双击后没有新窗口」，配合窗口本该已经在前台，通常不会造成困惑。
func activateExistingWindow() {
	hwnd := findAnyLauncherWindow()
	if hwnd == 0 {
		return
	}

	// StartHidden 期间窗口是隐藏的（见 main.go 的 StartHidden 注释）：如果
	// 前一个实例还停在「等前端调用 Show」的阶段，这里必须显式显示，
	// 否则用户双击第二次依然什么都看不到。
	visible, _, _ := procIsWindowVisible.Call(hwnd)
	minimized, _, _ := procIsIconic.Call(hwnd)
	if visible == 0 || minimized != 0 {
		procShowWindow.Call(hwnd, swRestore)
	}

	procAllowSetForegroundWindow.Call(asfwAny)
	procBringWindowToTop.Call(hwnd)
	procSetForegroundWindow.Call(hwnd)
}

// launcherWindowEnumProc 枚举回调：按窗口类名匹配，**不做 PID 过滤**
// （第二个实例要找的正是别的进程的窗口）。
func launcherWindowEnumProc(hwnd uintptr, _ uintptr) uintptr {
	// 类名缓冲区给 256 字符足够容纳 "wailsWindow"
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return 1 // 继续枚举
	}
	if windows.UTF16ToString(buf[:n]) == wailsWindowClass {
		enumFoundHWND = hwnd

		return 0 // 已找到，停止枚举
	}

	return 1
}

func findAnyLauncherWindow() uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()

	// 回调只创建一次：syscall.NewCallback 每次调用都会占用 Go 运行时回调表里
	// 一个永不回收的槽位（上限 2000），反复新建最终会不可恢复地崩溃。
	// 这条约束与 internal/bindings/acrylic_windows.go 中的处理一致。
	enumCallbackOnce.Do(func() {
		enumCallbackProc = windows.NewCallback(launcherWindowEnumProc)
	})
	enumFoundHWND = 0
	procEnumWindows.Call(enumCallbackProc, 0)

	return enumFoundHWND
}
