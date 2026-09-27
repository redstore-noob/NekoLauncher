//go:build windows

package monitoring

// GPU 占用（Windows）：读取 GPU 引擎性能计数器（任务管理器同源数据源），
// 不依赖厂商 SDK，NVIDIA / AMD / Intel 核显与独显通用。
// 计数器经 WMI 暴露为 Win32_PerfFormattedData_GPUPerformanceCounters_GPUEngine，
// 每个实例是「某进程 × 某引擎」的占用，求和即整机 GPU 占用（封顶 100%）。

import (
	"strings"

	"github.com/yusufpapurcu/wmi"
)

// gpuEngineCounter 映射 Win32_PerfFormattedData_GPUPerformanceCounters_GPUEngine 的
// 关心字段。Name 形如 pid_1234_luid_..._phys_0_eng_0_engtype_3D。
type gpuEngineCounter struct {
	Name                  string
	UtilizationPercentage uint32
}

// gpuPercent 返回整机 GPU 占用（0~100）；性能计数器不可用（Win10 1709 之前、
// 远程会话等）返回 GpuUnavailable。所有引擎实例求和后封顶 100%：
// 不同类型引擎（3D/Copy/VideoDecode…）可并行工作，求和略高于“忙碌时间占比”，
// 但与任务管理器展示的量级一致，对监控组件足够。
func gpuPercent() float64 {
	var counters []gpuEngineCounter
	query := `SELECT Name, UtilizationPercentage FROM Win32_PerfFormattedData_GPUPerformanceCounters_GPUEngine`
	if err := wmi.Query(query, &counters); err != nil {
		return GpuUnavailable
	}

	total := 0.0
	seen := make(map[string]struct{}, len(counters))
	for _, counter := range counters {
		// 同名实例只计一次（个别驱动会重复上报同一引擎）
		name := strings.ToLower(counter.Name)
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		total += float64(counter.UtilizationPercentage)
		if total >= 100 {
			return 100
		}
	}
	return total
}
