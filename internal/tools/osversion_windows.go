//go:build windows

package tools

import (
	"strconv"
	"syscall"
	"unsafe"
)

// windowsOSVersion 通过 RtlGetVersion 取真实系统版本，返回形如 "10.0.26200"。
// 取不到返回空串（由调用方回退）。
//
// 注意 DwOSVersionInfoSize 必须填结构体大小：RtlGetVersion 会校验它，
// 填 0 会直接返回非零状态码（早先就是漏了这一行，导致整条探测永远失败）。
func windowsOSVersion() string {
	rtlGetVersion := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")
	if err := rtlGetVersion.Find(); err != nil {
		return ""
	}

	var version osVersionInfo
	version.DwOSVersionInfoSize = uint32(unsafe.Sizeof(version))

	ret, _, _ := rtlGetVersion.Call(uintptr(unsafe.Pointer(&version)))
	if ret != 0 {
		return ""
	}

	return strconv.FormatUint(uint64(version.MajorVersion), 10) + "." +
		strconv.FormatUint(uint64(version.MinorVersion), 10) + "." +
		strconv.FormatUint(uint64(version.BuildNumber), 10)
}

// osVersionInfo 对应 Win32 RTL_OSVERSIONINFOW。
type osVersionInfo struct {
	DwOSVersionInfoSize uint32
	MajorVersion        uint32
	MinorVersion        uint32
	BuildNumber         uint32
	PlatformID          uint32
	CSDVersionText      [128]uint16
}
