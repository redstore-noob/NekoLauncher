package monitoring

// 磁盘占用采样：主页「磁盘空间」小组件使用。
// 语义：采样 path 所在【物理硬盘】的整体占用——把这块盘上所有分区的容量与
// 剩余空间求和，而不是只看 game 目录所在的那个盘符分区；物理盘映射仅在
// Windows 提供（见 disk_windows.go），其他平台回落单分区语义。

import (
	"github.com/shirou/gopsutil/v4/disk"
)

// DiskUsage 一次磁盘占用采样。
type DiskUsage struct {
	// Path 发起采样的目录路径（盘符/挂载点定位依据）。
	Path string `json:"Path"`
	// TotalBytes 整块物理硬盘的总容量（回落单分区时为分区容量）。
	TotalBytes uint64 `json:"TotalBytes"`
	// FreeBytes 整块物理硬盘的剩余容量之和。
	FreeBytes uint64 `json:"FreeBytes"`
	// UsedPercent 已用百分比 0~100（按求和后的总量计算）。
	UsedPercent float64 `json:"UsedPercent"`
}

// UsageOf 采样 path 所在物理硬盘的整体占用；
// 物理盘映射不可用时回落为 path 所在分区的占用。
func UsageOf(path string) (DiskUsage, error) {
	if usage, err := physicalUsage(path); err == nil {
		return usage, nil
	}
	return partitionUsage(path)
}

// partitionUsage 单分区占用采样（gopsutil disk.Usage，向上定位挂载点）。
func partitionUsage(path string) (DiskUsage, error) {
	usage, err := disk.Usage(path)
	if err != nil {
		return DiskUsage{}, err
	}

	return DiskUsage{
		Path:        path,
		TotalBytes:  usage.Total,
		FreeBytes:   usage.Free,
		UsedPercent: clampPercent(usage.UsedPercent),
	}, nil
}
