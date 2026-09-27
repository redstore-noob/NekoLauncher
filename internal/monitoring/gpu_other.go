//go:build !windows

package monitoring

// 非 Windows 平台暂无统一的 GPU 占用读取方式（各厂商 SDK / DRM 各不相同），
// 直接上报 GpuUnavailable，前端以「不可用」展示。

// gpuPercent 返回 GpuUnavailable 占位值。
func gpuPercent() float64 { return GpuUnavailable }
