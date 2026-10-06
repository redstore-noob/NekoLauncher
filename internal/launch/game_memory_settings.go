package launch

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/mem"

	"nekolauncher/internal/config"
)

// SystemMemorySnapshot 物理内存快照（单位 MB）。
type SystemMemorySnapshot struct {
	TotalMemoryMb     int
	AvailableMemoryMb int
}

// GameMemoryDecision 启动时的内存决策：是否自动、最终 -Xmx 以及系统/保留内存信息。
// IsMemoryTight 可用内存不足以满足预留策略时为 true：已按 4 GiB 保底分配，
// 但系统随时可能进入换页，启动器应向用户发出低内存警告。
type GameMemoryDecision struct {
	IsAutomatic       bool
	MaximumMemoryMb   int
	TotalMemoryMb     int
	AvailableMemoryMb int
	ReservedMemoryMb  int
	IsMemoryTight     bool
	// FromInstanceSettings 本次上限由实例独立内存设置进一步压低（供"为什么是这么大"溯源）。
	FromInstanceSettings bool
}

// GameMemorySettings 持久化全局内存策略，并在启动时解析最终生效的 JVM -Xmx。
// 自动模式每次启动都重新采样物理内存，保证决策反映当前机器状态。
var GameMemorySettings gameMemorySettingsNamespace

const (
	maximumMemoryKey          = "globalMaximumMemoryMb"
	automaticAdjustmentKey    = "automaticMemoryAdjustment"
	minimumSelectableMemoryMb = 512
	memoryStepMb              = 256

	// automaticMemoryFloorMb 自动模式下的保底堆上限：-Xmx 只是上限并不预先占用，
	// 现代 Minecraft（尤其百模级整合包）世界生成时的实际堆需求普遍超过 2 GiB——
	// 低于 4 GiB 分配大概率 OOM（可用内存不足时宁可承受系统换页，
	// 也不给一个注定崩的值）。
	automaticMemoryFloorMb = 4096
)

type gameMemorySettingsNamespace struct{}

// IsAutomaticAdjustmentEnabled 是否启用自动内存调整（持久化到全局配置）。
func (gameMemorySettingsNamespace) IsAutomaticAdjustmentEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(config.GetValue(automaticAdjustmentKey)), "true")
}

// SetAutomaticAdjustmentEnabled 设置是否启用自动内存调整。
func (gameMemorySettingsNamespace) SetAutomaticAdjustmentEnabled(enabled bool) {
	// 与 config 包其它布尔写入一致的 True/False 写法
	value := "False"
	if enabled {
		value = "True"
	}
	config.SetValue(automaticAdjustmentKey, value)
}

// SliderMaximumMemoryMb 滑块上限：物理内存按 256MB 向下取整（不低于最小可选值）。
func (gameMemorySettingsNamespace) SliderMaximumMemoryMb() int {
	snapshot := GetSystemMemory()
	return maxInt(minimumSelectableMemoryMb, roundDown(snapshot.TotalMemoryMb, memoryStepMb))
}

// ManualMaximumMemoryMb 手动模式生效值：配置值被钳制在 [512, 滑块上限] 并按步长取整。
func (gameMemorySettingsNamespace) ManualMaximumMemoryMb() int {
	sliderMaximum := GameMemorySettings.SliderMaximumMemoryMb()
	configured, err := strconv.Atoi(strings.TrimSpace(config.GetValue(maximumMemoryKey)))
	if err != nil {
		configured = minInt(recommendedDefaultMaximumMemoryMb(), sliderMaximum)
	}
	return clampAndRoundMemory(configured, sliderMaximum)
}

// recommendedDefaultMaximumMemoryMb 手动模式未配置时的缺省上限：按物理内存的一半
// 给出，钳制在 [2 GiB, 8 GiB]——8 GiB 足够绝大多数大型整合包，同时给系统/启动器
// 留出余量；采样失败时退回 4 GiB。
func recommendedDefaultMaximumMemoryMb() int {
	total := GetSystemMemory().TotalMemoryMb
	if total <= 0 {
		return 4096
	}
	return minInt(maxInt(roundDown(total/2, memoryStepMb), 2048), 8192)
}

// SaveManualMaximumMemoryMb 保存手动内存上限。
func (gameMemorySettingsNamespace) SaveManualMaximumMemoryMb(memoryMb int) bool {
	normalized := clampAndRoundMemory(memoryMb, GameMemorySettings.SliderMaximumMemoryMb())
	return config.SetValue(maximumMemoryKey, strconv.Itoa(normalized))
}

// ResolveForLaunch 解析启动时的 -Xmx：
// 手动模式取持久化的手动值（受实例独立设置进一步限制）；
// 自动模式为保证系统流畅，预留一块内存并按物理内存 75% 封顶。
// instanceMaximumMemoryMb 为实例独立内存上限（只允许往下调）；nil 表示未启用。
func ResolveForLaunch(instanceMaximumMemoryMb *int) GameMemoryDecision {
	memory := GetSystemMemory()
	systemMaximum := maxInt(minimumSelectableMemoryMb, roundDown(memory.TotalMemoryMb, memoryStepMb))
	if !GameMemorySettings.IsAutomaticAdjustmentEnabled() {
		policyMaximum := clampAndRoundMemory(GameMemorySettings.ManualMaximumMemoryMb(), systemMaximum)
		return GameMemoryDecision{
			IsAutomatic:          false,
			MaximumMemoryMb:      applyInstanceMemoryLimit(policyMaximum, instanceMaximumMemoryMb, systemMaximum),
			TotalMemoryMb:        memory.TotalMemoryMb,
			AvailableMemoryMb:    memory.AvailableMemoryMb,
			ReservedMemoryMb:     0,
			FromInstanceSettings: instanceMaximumMemoryMb != nil,
		}
	}

	// 预留内存：至少给系统/启动器留 2GB，且不超过物理内存的 15%（向上取整到步长）
	reserve := maxInt(2048, roundUp(memory.TotalMemoryMb*15/100, memoryStepMb))
	// 自动上限：可用内存扣除预留后，与物理内存 75% 封顶取较小者；
	// 可用内存不足以覆盖预留时按保底值分配（-Xmx 按需提交，不会瞬间吃满），
	// 但标记 IsMemoryTight 让启动器给出低内存警告。
	availableForGame := memory.AvailableMemoryMb - reserve
	isMemoryTight := availableForGame < automaticMemoryFloorMb
	// 自动上限同时受物理内存 75% 封顶
	totalMemoryCap := maxInt(
		minimumSelectableMemoryMb,
		roundDown(memory.TotalMemoryMb*75/100, memoryStepMb))
	candidate := availableForGame
	if isMemoryTight {
		candidate = automaticMemoryFloorMb
	} else {
		candidate = maxInt(minimumSelectableMemoryMb, availableForGame)
	}
	automaticMaximum := clampAndRoundMemory(minInt(candidate, totalMemoryCap), systemMaximum)

	return GameMemoryDecision{
		IsAutomatic:          true,
		MaximumMemoryMb:      applyInstanceMemoryLimit(automaticMaximum, instanceMaximumMemoryMb, systemMaximum),
		TotalMemoryMb:        memory.TotalMemoryMb,
		AvailableMemoryMb:    memory.AvailableMemoryMb,
		ReservedMemoryMb:     reserve,
		IsMemoryTight:        isMemoryTight,
		FromInstanceSettings: instanceMaximumMemoryMb != nil,
	}
}

// ---------------------------------------------------------------------------
// 数值钳制与取整
// ---------------------------------------------------------------------------

func clampAndRoundMemory(value, maximum int) int {
	clamped := minInt(maxInt(value, minimumSelectableMemoryMb), maxInt(maximum, minimumSelectableMemoryMb))
	return maxInt(minimumSelectableMemoryMb, roundDown(clamped, memoryStepMb))
}

// applyInstanceMemoryLimit 实例独立内存设置：只允许把上限往下调，不会突破全局策略。
func applyInstanceMemoryLimit(policyMaximum int, instanceMaximumMemoryMb *int, systemMaximum int) int {
	if instanceMaximumMemoryMb != nil {
		return minInt(policyMaximum, clampAndRoundMemory(*instanceMaximumMemoryMb, systemMaximum))
	}
	return policyMaximum
}

func roundDown(value, step int) int { return value / step * step }
func roundUp(value, step int) int   { return (value + step - 1) / step * step }

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

// ---------------------------------------------------------------------------
// 各平台内存采样
// ---------------------------------------------------------------------------

// GetSystemMemory 采样物理内存：Windows 用 GlobalMemoryStatusEx，
// Linux 读 /proc/meminfo，其余走 gopsutil 运行时兜底。
func GetSystemMemory() SystemMemorySnapshot {
	if isWindows() {
		if snapshot, ok := readWindowsMemory(); ok {
			return snapshot
		}
	}
	if runtime.GOOS == "linux" {
		if snapshot, ok := readLinuxMemory(); ok {
			return snapshot
		}
	}
	return readRuntimeMemoryFallback()
}

func readLinuxMemory() (SystemMemorySnapshot, bool) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return SystemMemorySnapshot{}, false
	}
	defer file.Close()

	var totalKb, availableKb int64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			totalKb = readLinuxKilobytes(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			availableKb = readLinuxKilobytes(line)
		}
	}
	if totalKb <= 0 {
		return SystemMemorySnapshot{}, false
	}
	// MemAvailable 缺失（很老的内核）时按总量一半估算可用内存
	available := totalKb / 2 / 1024
	if availableKb > 0 {
		available = availableKb / 1024
	}
	return SystemMemorySnapshot{
		TotalMemoryMb:     clampMegabytes(totalKb / 1024),
		AvailableMemoryMb: clampMegabytes(available),
	}, true
}

// readLinuxKilobytes 解析 /proc/meminfo 形如 "MemTotal: 16384 kB" 的行，取 kb 数值。
func readLinuxKilobytes(line string) int64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	value, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// readRuntimeMemoryFallback 非 Windows/Linux 平台的兜底：用 gopsutil 读取
// 运行时内存信息估算（C# 使用 GC.GetGCMemoryInfo，Go 侧 gopsutil 更精确）。
func readRuntimeMemoryFallback() SystemMemorySnapshot {
	info, err := mem.VirtualMemory()
	if err != nil {
		return SystemMemorySnapshot{
			TotalMemoryMb:     minimumSelectableMemoryMb,
			AvailableMemoryMb: minimumSelectableMemoryMb,
		}
	}
	return SystemMemorySnapshot{
		TotalMemoryMb:     clampMegabytes(int64(info.Total) / 1024 / 1024),
		AvailableMemoryMb: clampMegabytes(int64(info.Available) / 1024 / 1024),
	}
}

func clampMegabytes(megabytes int64) int {
	if megabytes < minimumSelectableMemoryMb {
		return minimumSelectableMemoryMb
	}
	return int(megabytes)
}
