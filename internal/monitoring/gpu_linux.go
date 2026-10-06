//go:build linux

package monitoring

// GPU 占用（Linux）：没有统一接口，按「装了什么就用什么」的顺序探测：
//
//	1. nvidia-smi —— NVIDIA 专有驱动装了就有，利用率最准
//	2. /sys/class/drm/card*/device/gpu_busy_percent —— amdgpu 与 i915 都暴露
//
// 两路都拿不到（ nouveau / 虚拟机 / 无显卡 ）返回 GpuUnavailable。
// 每张卡得到的是 0-100 的整机利用率，多卡取最大值——不是求和：
// sysfs 语义是「这张卡的忙碌占比」，与 Windows 版引擎求和的口径不同。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// gpuPercent 返回整机 GPU 占用（0~100）；无法获取返回 GpuUnavailable。
func gpuPercent() float64 {
	if value, ok := nvidiaGpuPercent(); ok {
		return value
	}
	if value, ok := sysfsGpuPercent(); ok {
		return value
	}
	return GpuUnavailable
}

// nvidiaGpuPercent 调 nvidia-smi 查所有 GPU 的利用率，多卡取最大值。
func nvidiaGpuPercent() (float64, bool) {
	output, err := exec.Command(
		"nvidia-smi",
		"--query-gpu=utilization.gpu",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return 0, false
	}
	return parseNvidiaSmuUtilization(string(output))
}

// parseNvidiaSmuUtilization 解析 nvidia-smi 的 CSV 输出（每行一张卡的百分数），
// 多卡取最大值；没有任何有效数值时 ok=false。
func parseNvidiaSmuUtilization(output string) (float64, bool) {
	best := -1.0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 去掉可能带上的 "[N/A]" 等备注再取首段数字
		value, err := strconv.ParseFloat(strings.TrimSpace(
			strings.Fields(line)[0],
		), 64)
		if err != nil || value < 0 || value > 100 {
			continue
		}
		if value > best {
			best = value
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

// sysfsGpuPercent 读所有 DRM 显卡的 gpu_busy_percent，取最大值。
func sysfsGpuPercent() (float64, bool) {
	matches, err := filepath.Glob("/sys/class/drm/card*/device/gpu_busy_percent")
	if err != nil || len(matches) == 0 {
		return 0, false
	}
	best := -1.0
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		if err != nil || value < 0 || value > 100 {
			continue
		}
		if value > best {
			best = value
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}
