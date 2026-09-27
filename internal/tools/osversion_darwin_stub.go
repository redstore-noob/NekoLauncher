//go:build !darwin

package tools

// darwinOSVersion 非 macOS 平台的占位实现（同 windowsOSVersion 的理由）。
func darwinOSVersion() string { return "" }
