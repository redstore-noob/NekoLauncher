//go:build !windows

package tools

// windowsOSVersion 非 Windows 平台的占位实现（OSVersionDescription 只在
// GOOS == "windows" 时调用它；这里存在只是为了让共用文件在其它平台也能编译）。
func windowsOSVersion() string { return "" }
