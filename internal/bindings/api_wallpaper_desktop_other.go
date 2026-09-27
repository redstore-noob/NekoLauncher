//go:build !windows && !darwin && !linux

package bindings

// 其它平台（FreeBSD 等）：没有跨桌面环境的壁纸读取方式，恒返回空串，
// 由前端回退默认图。加这个文件是为了让 GOOS=freebsd 之类的构建也能通过。

// desktopWallpaperPath 未实现的平台恒返回空串（不返回错误，调用方据此回落默认图）。
func desktopWallpaperPath() (string, error) { return "", nil }
