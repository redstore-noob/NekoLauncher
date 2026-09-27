//go:build windows

package launch

import (
	"strings"
	"syscall"
	"unsafe"
)

var (
	localeKernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetUserDefaultLocale = localeKernel32.NewProc("GetUserDefaultLocaleName")
)

// readSystemLocale 读取用户默认区域名（BCP-47，如 zh-CN / en-US）；失败返回空串。
func readSystemLocale() string {
	if err := procGetUserDefaultLocale.Find(); err != nil {
		return ""
	}
	buf := make([]uint16, 85) // LOCALE_NAME_MAX_LENGTH
	ret, _, _ := procGetUserDefaultLocale.Call(
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
	)
	if ret == 0 {
		return ""
	}

	return strings.TrimSpace(syscall.UTF16ToString(buf))
}
