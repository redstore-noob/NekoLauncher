//go:build !windows

package bindings

// 非 Windows：没有注册表可读，Steam 目录探测直接返回空串。
// Wallpaper Engine 本身只有 Windows 版，调用方（GetWallpaperEngineWallpaper）
// 也会在非 Windows 上提前返回，这里只是保证包能编译。

// steamPathFromRegistry 非 Windows 平台恒返回空串。
func steamPathFromRegistry() string { return "" }
