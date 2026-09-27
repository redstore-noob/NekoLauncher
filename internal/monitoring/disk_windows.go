//go:build windows

package monitoring

// Windows 物理盘映射：经 WMI 的 Win32_LogicalDiskToPartition 把盘符关联回
// 物理磁盘（分区 DeviceID 形如 "Disk #N, Partition #M"，N 即物理盘序号），
// 再把同一块物理盘上全部逻辑分区的占用求和。对应「磁盘空间」小组件的
// 「按整块硬盘统计」语义（系统盘常带恢复分区，单看 C: 会低估容量）。

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/yusufpapurcu/wmi"
)

// errPhysicalDiskUnavailable 物理盘映射不可用（调用方回落单分区语义）。
var errPhysicalDiskUnavailable = errors.New("physical disk mapping unavailable")

// volumeLetter 从路径提取盘符（形如 "E:"）；无法识别返回空串。
// 兼容 "E:"、"E:\"、"E:\Games\mc" 与 UNC 路径（UNC 无盘符，返回空串回落）。
func volumeLetter(path string) string {
	volume := filepath.VolumeName(path)

	return strings.ToUpper(volume)
}

// logicalDiskToPartition 映射 Win32_LogicalDiskToPartition 的关心字段：
// Antecedent = 分区引用（含 "Disk #N, Partition #M"），
// Dependent  = 逻辑盘引用（含 "E:"）。
type logicalDiskToPartition struct {
	Antecedent string
	Dependent  string
}

// refDeviceID 从 WMI 对象引用串里提取 DeviceID 的引号值；
// 引用形如 \\HOST\root\cimv2:Win32_LogicalDisk.DeviceID="E:"。
func refDeviceID(ref string) string {
	start := strings.LastIndex(ref, `="`)

	if start < 0 {
		return ""
	}
	value := ref[start+2:]
	value = strings.TrimSuffix(value, `"`)

	return value
}

// physicalUsage 把 path 归属的物理盘上全部分区求和；无法解析盘符或 WMI
// 查询失败时返回错误（调用方回落单分区语义）。
func physicalUsage(path string) (DiskUsage, error) {
	var rows []logicalDiskToPartition

	query := `SELECT Antecedent, Dependent FROM Win32_LogicalDiskToPartition`
	if err := wmi.Query(query, &rows); err != nil {
		return DiskUsage{}, err
	}
	if len(rows) == 0 {
		return DiskUsage{}, errPhysicalDiskUnavailable
	}

	// 第一遍：盘符 → 物理盘序号
	volumeToDisk := make(map[string]int, len(rows))
	for _, row := range rows {
		letter := refDeviceID(row.Dependent)
		partition := refDeviceID(row.Antecedent)

		if letter == "" || partition == "" {
			continue
		}
		if index := strings.LastIndex(partition, "Disk #"); index >= 0 {
			if number, err := strconv.Atoi(
				strings.TrimPrefix(partition[index:], "Disk #"),
			); err == nil {
				volumeToDisk[strings.ToUpper(letter)] = number
			}
		}
	}

	diskNumber, ok := volumeToDisk[volumeLetter(path)]
	if !ok {
		return DiskUsage{}, errPhysicalDiskUnavailable
	}	// 第二遍：同一物理盘上的全部盘符，逐分区求和
	var (
		totalBytes uint64
		freeBytes  uint64
	)
	for letter := range volumeToDisk {
		if volumeToDisk[letter] != diskNumber {
			continue
		}
		usage, err := disk.Usage(letter + `\`)
		if err != nil {
			continue
		}
		totalBytes += usage.Total
		freeBytes += usage.Free
	}
	if totalBytes == 0 {
		return DiskUsage{}, errPhysicalDiskUnavailable
	}

	usedPercent := float64(totalBytes-freeBytes) / float64(totalBytes) * 100

	return DiskUsage{
		Path:        path,
		TotalBytes:  totalBytes,
		FreeBytes:   freeBytes,
		UsedPercent: clampPercent(usedPercent),
	}, nil
}
