//go:build windows

package bindings

// Windows：经 user32!SystemParametersInfoW 读取当前桌面壁纸路径。
// 单独成文件是因为 syscall.NewLazyDLL / UTF16ToString 只在 Windows 上存在。

import (
	"strings"
	"syscall"
	"unsafe"
)

// getDesktopWallpaperAction SystemParametersInfoW 的 SPI_GETDESKWALLPAPER 动作码。
const getDesktopWallpaperAction = 0x0073

// desktopWallpaperBufferChars 壁纸路径缓冲长度（字符）。
//
// 原来是照 MAX_PATH(260) 分配的，但 SPI_GETDESKWALLPAPER 只是"最多写 uiParam 个字符"，
// 并不强制 260：壁纸放在深层目录（或路径里有长用户名）时 260 会**被静默截断**，
// 截断后的路径打不开，界面就只显示一张默认图。这里给足 4096 个字符，
// 并且下面用"没写满缓冲"来判断没有被截断。
const desktopWallpaperBufferChars = 4096

// systemParametersInfoW user32!SystemParametersInfoW（x/sys/windows 未导出，此处自行声明）。
var systemParametersInfoW = syscall.NewLazyDLL("user32.dll").NewProc("SystemParametersInfoW")

// desktopWallpaperPath 通过 SystemParametersInfoW(SPI_GETDESKWALLPAPER) 读取当前壁纸。
// 未设置壁纸（纯色背景）时返回空串且不报错，由前端回退默认图。
func desktopWallpaperPath() (string, error) {
	if err := systemParametersInfoW.Find(); err != nil {
		return "", err
	}

	buf := make([]uint16, desktopWallpaperBufferChars)
	ret, _, callErr := systemParametersInfoW.Call(
		getDesktopWallpaperAction,
		uintptr(len(buf)),
		uintptr(unsafe.Pointer(&buf[0])),
		0,
	)
	if ret == 0 {
		// BOOL=FALSE：callErr 为 GetLastError 结果，可能是"未设置壁纸"
		return "", callErr
	}

	path := strings.TrimSpace(syscall.UTF16ToString(buf))
	if path == "" {
		// 纯色背景：系统里没有壁纸路径，这不是错误
		return "", nil
	}
	if len([]rune(path)) >= desktopWallpaperBufferChars-1 {
		// 缓冲被写满：路径可能被截断，宁可不显示也不要显示一个打不开的路径
		return "", nil
	}

	return path, nil
}
