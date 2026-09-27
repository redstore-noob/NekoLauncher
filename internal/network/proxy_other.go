//go:build !windows

package network

// 非 Windows 平台的"跟随系统"：标准库没有跨平台的系统代理读取接口，
// 直接使用环境变量（HTTP_PROXY / HTTPS_PROXY / NO_PROXY），与 Go 默认行为一致。
// cachedWindowsSystemProxy 由 proxy.go 的 systemProxyFunc 引用，这里提供空实现。

// cachedWindowsSystemProxy 非 Windows 平台恒返回空串（回落环境变量）。
func cachedWindowsSystemProxy() string { return "" }
