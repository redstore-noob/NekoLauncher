//go:build windows

package launch

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

// memoryStatusEx 对应 Win32 MEMORYSTATUSEX（与 C# MemoryStatusEx 布局一致）。
type memoryStatusEx struct {
	Length                   uint32
	MemoryLoad               uint32
	TotalPhysical            uint64
	AvailablePhysical        uint64
	TotalPageFile            uint64
	AvailablePageFile        uint64
	TotalVirtual             uint64
	AvailableVirtual         uint64
	AvailableExtendedVirtual uint64
}

// readWindowsMemory 采样 Windows 物理内存；失败返回 false 交由运行时兜底。
func readWindowsMemory() (SystemMemorySnapshot, bool) {
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	ret, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		return SystemMemorySnapshot{}, false
	}
	snapshot := SystemMemorySnapshot{
		TotalMemoryMb:     bytesToMb(int64(status.TotalPhysical)),
		AvailableMemoryMb: bytesToMb(int64(status.AvailablePhysical)),
	}
	if snapshot.TotalMemoryMb <= 0 {
		return SystemMemorySnapshot{}, false
	}
	return snapshot, true
}

func bytesToMb(bytesCount int64) int {
	return clampMegabytes(bytesCount / 1024 / 1024)
}
