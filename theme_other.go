//go:build !windows

package main

// startupBackgroundColour 非 Windows 平台读不到系统主题，返回白色由前端样式接管。
func startupBackgroundColour() (r, g, b uint8) { return 0xFF, 0xFF, 0xFF }
