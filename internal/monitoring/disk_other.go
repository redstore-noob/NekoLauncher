//go:build !windows

package monitoring

// 非 Windows 平台的物理盘语义回落：gopsutil 的分区 → 物理盘映射需要
// 各平台不同的探测方式（Linux /proc/diskstats、macOS IOKit），暂不实现，
// 统一回落为单分区占用（语义不变，仅失去"整盘求和"能力）。

import "errors"

// errPhysicalDiskUnavailable 物理盘映射不可用（disk_windows.go 同名约定）。
var errPhysicalDiskUnavailable = errors.New("physical disk mapping unavailable")

// physicalUsage 非 Windows 平台直接返回错误，由 UsageOf 回落单分区采样。
func physicalUsage(path string) (DiskUsage, error) {
	return DiskUsage{}, errPhysicalDiskUnavailable
}
