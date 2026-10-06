//go:build !windows && !linux

package network

// macOS 等其余平台的"跟随系统"：标准库没有跨平台的系统代理读取接口，
// 直接使用环境变量（HTTP_PROXY / HTTPS_PROXY / NO_PROXY），与 Go 默认行为一致。
// Windows / Linux 有独立实现，见 proxy_windows.go / proxy_linux.go。

// cachedSystemProxy 非 Windows / Linux 平台恒返回空串（回落环境变量）。
func cachedSystemProxy() string { return "" }
