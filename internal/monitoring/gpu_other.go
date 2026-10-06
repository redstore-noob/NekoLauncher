//go:build !windows && !linux

package monitoring

// macOS 等其余平台暂无统一的 GPU 占用读取方式（各厂商 SDK 各不相同），
// 直接上报 GpuUnavailable，前端以「不可用」展示。
// Linux 有独立实现，见 gpu_linux.go。

// gpuPercent 返回 GpuUnavailable 占位值。
func gpuPercent() float64 { return GpuUnavailable }
