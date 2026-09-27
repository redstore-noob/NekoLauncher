package monitoring

// 全系统占用采样：CPU / GPU / 内存（主页「性能监控」小组件使用）。
// 与 memory.go 的进程级快照不同，这里面向整台机器，语义对齐任务管理器。

import (
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

// GpuUnavailable 表示 GPU 占用无法获取（无 GPU 性能计数器 / 非 Windows 平台）。
const GpuUnavailable = -1.0

// SystemUsage 一次全系统占用采样，百分比均为 0~100。
// JSON 字段名保持 PascalCase，与包内其他快照结构一致。
type SystemUsage struct {
	// CpuPercent 全部核心的整体 CPU 占用率。
	CpuPercent float64 `json:"CpuPercent"`
	// GpuPercent 整机 GPU 占用率；无法获取时为 GpuUnavailable(-1)。
	GpuPercent float64 `json:"GpuPercent"`
	// MemoryPercent 物理内存占用率。
	MemoryPercent float64 `json:"MemoryPercent"`
	// MemoryUsedGb 已用物理内存（GB）。
	MemoryUsedGb float64 `json:"MemoryUsedGb"`
	// MemoryTotalGb 物理内存总量（GB）。
	MemoryTotalGb float64 `json:"MemoryTotalGb"`
}

// gpuTTL GPU 采样结果缓存时长：GPU 引擎计数器走 WMI，单次查询可达上百毫秒，
// 而前端按 1s 轮询，缓存 2s 足够平滑且把 WMI 开销摊薄一半。
const gpuTTL = 2 * time.Second

var (
	gpuMu      sync.Mutex
	gpuCache   = GpuUnavailable
	gpuCacheAt time.Time
)

// SystemUsageNow 采集当前全系统占用。
// CPU 使用与上次调用的差值（gopsutil 内部保持状态，首次调用返回 0）；
// GPU 走 gpuPercent（平台相关实现，见 gpu_windows.go / gpu_other.go）并带短缓存。
func SystemUsageNow() SystemUsage {
	var usage SystemUsage

	if percents, err := cpu.Percent(0, false); err == nil && len(percents) > 0 {
		usage.CpuPercent = clampPercent(percents[0])
	}

	if vm, err := mem.VirtualMemory(); err == nil && vm.Total > 0 {
		usage.MemoryPercent = clampPercent(vm.UsedPercent)
		usage.MemoryUsedGb = float64(vm.Used) / 1024 / 1024 / 1024
		usage.MemoryTotalGb = float64(vm.Total) / 1024 / 1024 / 1024
	}

	usage.GpuPercent = sampleGpu()
	return usage
}

// sampleGpu 串行化 GPU 采样并按 gpuTTL 缓存，避免轮询方叠加 WMI 查询。
func sampleGpu() float64 {
	gpuMu.Lock()
	defer gpuMu.Unlock()

	if time.Since(gpuCacheAt) < gpuTTL {
		return gpuCache
	}
	gpuCache = clampGpu(gpuPercent())
	gpuCacheAt = time.Now()
	return gpuCache
}

func clampPercent(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return v
	}
}

// clampGpu GPU 采样结果合法性检查：无效值统一归为 GpuUnavailable。
func clampGpu(v float64) float64 {
	if v < 0 || v > 100 {
		return GpuUnavailable
	}
	return v
}
