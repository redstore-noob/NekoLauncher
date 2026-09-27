//go:build !windows

package update

// 非 Windows：启动器以 AppImage / .app 包分发，替换方式各不相同（AppImage 需要替换
// 自身文件并加回执行位，macOS 要处理包签名与 quarantine 属性），自动替换风险高于收益。
// 这里明确返回 ErrManualUpdateRequired，由前端引导用户去版本页手动下载。

// Apply 非 Windows 平台不支持自动替换。
func Apply(newExecutable string) (bool, error) {
	return false, ErrManualUpdateRequired
}
