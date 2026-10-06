//go:build !linux

package bindings

// SystemAPI 扩展：Linux 动态桌面壁纸（swww / mpvpaper）的非 Linux 桩实现。
// Windows 有自己的桌面壁纸 API（SystemParametersInfoW），macOS 有 osascript，
// 均不需要这两个 Wayland 工具，这里只保证包能编译、接口形状一致。

import "fmt"

// LinuxWallpaperTools 与 Linux 实现保持同形（字段恒 false）。
type LinuxWallpaperTools struct {
	Swww     bool `json:"Swww"`
	Mpvpaper bool `json:"Mpvpaper"`
}

// GetLinuxWallpaperTools 非 Linux 平台恒返回两者都不可用。
func (a *SystemAPI) GetLinuxWallpaperTools() (LinuxWallpaperTools, error) {
	return LinuxWallpaperTools{}, nil
}

// ApplyDesktopWallpaper 非 Linux 平台不支持，返回明确错误而不是静默失败，
// 前端按平台隐藏入口，走到这里说明调用方出了问题。
func (a *SystemAPI) ApplyDesktopWallpaper(path string) error {
	return fmt.Errorf("当前平台不支持该方式设置桌面壁纸（仅 Linux 的 swww / mpvpaper）")
}
