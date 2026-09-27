//go:build windows

package monitoring

// 本文件只在 Windows 上编译：监控采样的实现分平台（disk_windows.go / gpu_windows.go），
// 断言口径也按 Windows 语义写（盘符、盘根采样）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/process"
)

// volumeRootOf 返回测试临时目录所在卷的盘根（形如 "E:\"），用于磁盘采样用例。
// 不挑固定盘符：CI 与开发机的盘符不一定相同。
func volumeRootOf(t *testing.T) string {
	t.Helper()
	root := filepath.VolumeName(t.TempDir())
	if root == "" {
		t.Skip("临时目录没有盘符（非常规 Windows 卷），跳过磁盘用例")
	}
	return root + `\`
}

// TestMemorySnapshotHasReasonableShape 进程内存快照的结构与取值范围。
// 防的回归：进程枚举/工作集读取整条链路挂掉后返回零值快照，
// 主页内存小组件永远显示 0MB —— 那种"看起来正常但没数据"的故障最难被发现。
func TestMemorySnapshotHasReasonableShape(t *testing.T) {
	if testing.Short() {
		t.Skip("枚举全部进程较慢，-short 模式跳过")
	}

	snapshot := Snapshot()

	if snapshot.JavaProcessCount < 0 {
		t.Fatalf("Java 进程数不应为负：%d", snapshot.JavaProcessCount)
	}
	if snapshot.JvmMemoryMb < 0 {
		t.Fatalf("JVM 内存不应为负：%f", snapshot.JvmMemoryMb)
	}
	if snapshot.LauncherMemoryMb < 0 {
		t.Fatalf("启动器内存不应为负：%f", snapshot.LauncherMemoryMb)
	}

	// 本测试进程自己有工作集：这条断言正是"采集真的读到了东西"的证据。
	// 上界 16GB 只是防御性天花板，避免把字节/比特单位算错（RSS 未除以 1024^2）。
	if snapshot.LauncherMemoryMb <= 0 || snapshot.LauncherMemoryMb > 16*1024 {
		t.Fatalf("启动器自身工作集异常：%f MB（期望 0 < x <= 16GB），可能单位换算写错或采集失败",
			snapshot.LauncherMemoryMb)
	}

	// 交叉验证：独立枚举一次进程，确认 JVM 统计口径与快照一致。
	// 允许 1 个进程的偏差：两次枚举之间可能有短命 java 进程生灭，
	// 但口径写错（比如把"所有进程"都算进 JVM）会带来数量级差异，仍会被抓到。
	selfPID := int32(os.Getpid())
	infos, err := process.Processes()
	if err != nil {
		t.Skipf("当前环境无法枚举进程（%v），跳过交叉验证", err)
	}
	expectedCount := 0
	for _, p := range infos {
		if p.Pid == selfPID {
			continue
		}
		name, err := p.Name()
		if err != nil {
			continue
		}
		if lower := strings.ToLower(name); lower == "java" || lower == "javaw" {
			expectedCount++
		}
	}
	// 上限校验：绝不能把全部进程都计入 Java 进程数
	if snapshot.JavaProcessCount > len(infos) {
		t.Fatalf("Java 进程数 %d 超过系统进程总数 %d", snapshot.JavaProcessCount, len(infos))
	}
	if difference := snapshot.JavaProcessCount - expectedCount; difference < -1 || difference > 1 {
		t.Fatalf("Java 进程数 = %d，独立统计为 %d（口径不一致）",
			snapshot.JavaProcessCount, expectedCount)
	}
	if expectedCount == 0 && snapshot.JvmMemoryMb != 0 {
		t.Fatalf("没有 Java 进程时 JVM 内存应为 0，得到 %f", snapshot.JvmMemoryMb)
	}
}

// TestSystemUsageSnapshotRanges 全系统占用快照的字段范围。
// 防的回归：CPU/内存百分比越界（负值或 >100）导致前端进度条溢出，
// 以及内存总量字段为 0 让"已用/总量"显示成 0/0。
func TestSystemUsageSnapshotRanges(t *testing.T) {
	usage := SystemUsageNow()

	if usage.CpuPercent < 0 || usage.CpuPercent > 100 {
		t.Fatalf("CPU 占用应在 0-100，得到 %f", usage.CpuPercent)
	}
	if usage.MemoryPercent < 0 || usage.MemoryPercent > 100 {
		t.Fatalf("内存占用百分比应在 0-100，得到 %f", usage.MemoryPercent)
	}
	if usage.MemoryTotalGb <= 0 {
		t.Fatalf("物理内存总量应大于 0，得到 %f GB", usage.MemoryTotalGb)
	}
	if usage.MemoryUsedGb < 0 {
		t.Fatalf("已用内存不应为负，得到 %f GB", usage.MemoryUsedGb)
	}
	if usage.MemoryUsedGb > usage.MemoryTotalGb {
		t.Fatalf("已用内存 %f GB 大于总量 %f GB（换算或取值口径写反）",
			usage.MemoryUsedGb, usage.MemoryTotalGb)
	}
	// 总量在合理量级：小于 100MB 说明单位换算错（字节当成 GB）
	if usage.MemoryTotalGb < 0.1 {
		t.Fatalf("物理内存总量 %f GB 明显不合理，疑似单位换算错误", usage.MemoryTotalGb)
	}

	if usage.GpuPercent != GpuUnavailable &&
		(usage.GpuPercent < 0 || usage.GpuPercent > 100) {
		t.Fatalf("GPU 占用应落在 0-100 或为不可用值 %v，得到 %f",
			GpuUnavailable, usage.GpuPercent)
	}

	// 连续采样不应 panic；第一次 CPU 采样常为 0（gopsutil 需要两次差分），
	// 第二次必须拿到有效值
	time.Sleep(120 * time.Millisecond)
	second := SystemUsageNow()
	if second.CpuPercent < 0 || second.CpuPercent > 100 {
		t.Fatalf("第二次 CPU 采样越界：%f", second.CpuPercent)
	}
	if second.CpuPercent == 0 && usage.CpuPercent == 0 {
		t.Log("两次 CPU 采样都是 0（空闲机器），按设计不视为失败")
	}
}

// TestGpuSampleIsCached GpuPercent 取值合法且短时间内复用缓存。
// 防的回归：去掉 gpuTTL 缓存后前端 1s 轮询每次都打 WMI（单次数百毫秒）拖慢主页；
// 以及缓存把非法值（-5 / 300 / NaN）留在结果里。
func TestGpuSampleIsCached(t *testing.T) {
	first := sampleGpu()
	assertValidGpu(t, first)

	if time.Since(gpuCacheAt) >= gpuTTL {
		t.Fatalf("采样后应刷新缓存时间戳，距现在 %v", time.Since(gpuCacheAt))
	}
	if second := sampleGpu(); second != first {
		t.Fatalf("TTL 内两次采样应命中同一缓存：%f → %f", first, second)
	}

	// 压入过期缓存后重新采样，验证 TTL 判定确实基于时间戳
	gpuMu.Lock()
	gpuCache = 42.5
	gpuCacheAt = time.Now().Add(-time.Hour)
	gpuMu.Unlock()
	if got := sampleGpu(); got == 42.5 {
		t.Fatal("缓存过期后应重新采样，而不是继续返回旧值")
	} else {
		assertValidGpu(t, got)
	}

	// 还原全局缓存，避免污染同包其他用例
	t.Cleanup(func() {
		gpuMu.Lock()
		gpuCache = GpuUnavailable
		gpuCacheAt = time.Time{}
		gpuMu.Unlock()
	})
}

func assertValidGpu(t *testing.T, value float64) {
	t.Helper()
	if value == GpuUnavailable {
		return
	}
	if value < 0 || value > 100 {
		t.Fatalf("GPU 占用应为 0-100 或 %v，得到 %f", GpuUnavailable, value)
	}
}

// TestClampHelpers clampPercent 把越界百分比夹回 0-100，clampGpu 把非法 GPU 值归为不可用。
// 防的回归：gopsutil 在部分环境返回负值/超 100（计数器异常）时直接透给前端，
// 进度条渲染成负数宽度。
func TestClampHelpers(t *testing.T) {
	percentCases := []struct {
		in   float64
		want float64
	}{
		{-1, 0},
		{-1e9, 0},
		{0, 0},
		{37.5, 37.5},
		{100, 100},
		{100.1, 100},
		{1e9, 100},
	}
	for _, testCase := range percentCases {
		if got := clampPercent(testCase.in); got != testCase.want {
			t.Errorf("clampPercent(%v) = %v，期望 %v", testCase.in, got, testCase.want)
		}
	}

	gpuCases := []struct {
		in   float64
		want float64
	}{
		{GpuUnavailable, GpuUnavailable},
		{-0.1, GpuUnavailable},
		{-100, GpuUnavailable},
		{100.5, GpuUnavailable},
		{0, 0},
		{63.5, 63.5},
		{100, 100},
	}
	for _, testCase := range gpuCases {
		if got := clampGpu(testCase.in); got != testCase.want {
			t.Errorf("clampGpu(%v) = %v，期望 %v", testCase.in, got, testCase.want)
		}
	}
}

// TestPartitionUsageOfTempDirectory 单分区采样（分区语义）的结构与范围。
// 防的回归：磁盘采样恒返回 0/0（磁盘组件显示"0GB / 0GB"），
// 以及总量与剩余量口径写反（把 free 当 total）。
func TestPartitionUsageOfTempDirectory(t *testing.T) {
	root := volumeRootOf(t)

	usage, err := partitionUsage(root)
	if err != nil {
		t.Skipf("当前环境无法采样磁盘 %s（%v），跳过", root, err)
	}

	if usage.Path != root {
		t.Fatalf("Path 应原样回填采样路径 %q，得到 %q", root, usage.Path)
	}
	if usage.TotalBytes == 0 {
		t.Fatal("分区总容量不应为 0")
	}
	if usage.FreeBytes > usage.TotalBytes {
		t.Fatalf("剩余空间 %d 大于总容量 %d（字段接反）", usage.FreeBytes, usage.TotalBytes)
	}
	if usage.UsedPercent < 0 || usage.UsedPercent > 100 {
		t.Fatalf("已用百分比应在 0-100，得到 %f", usage.UsedPercent)
	}
	// 失败路径返回零值结构：调用方靠 err 判断，不会把 0 当容量展示
	if failed, err := partitionUsage(`ZZ:\definitely-missing`); err == nil || failed != (DiskUsage{}) {
		t.Fatalf("分区采样失败时应返回零值 DiskUsage，得到 %+v / %v", failed, err)
	}

	// 与 gopsutil 原始采样交叉验证，确认没有二次换算引入偏差
	raw, err := disk.Usage(root)
	if err != nil {
		t.Skipf("gopsutil 原始采样失败（%v），跳过交叉验证", err)
	}
	if usage.TotalBytes != raw.Total || usage.FreeBytes != raw.Free {
		t.Fatalf("采样结果与 gopsutil 原始值不一致：total %d/%d，free %d/%d",
			usage.TotalBytes, raw.Total, usage.FreeBytes, raw.Free)
	}
}

// TestUsageOfRealVolume UsageOf 端到端：物理盘映射或分区回落都必须给出合法用量。
// 防的回归：物理盘路径失败时没有正确回落到单分区语义（直接返回 0 值或错误），
// 磁盘组件在 WMI 不可用的机器上整块空白。
func TestUsageOfRealVolume(t *testing.T) {
	if testing.Short() {
		t.Skip("物理盘映射要走 WMI，-short 模式跳过")
	}
	root := volumeRootOf(t)

	usage, err := UsageOf(root)
	if err != nil {
		t.Fatalf("采样磁盘 %s 失败：%v", root, err)
	}
	// 两条分支都回填调用方传入的 path
	if usage.Path != root {
		t.Fatalf("Path 应原样回填 %q，得到 %q", root, usage.Path)
	}
	if usage.TotalBytes == 0 {
		t.Fatal("总容量不应为 0")
	}
	if usage.FreeBytes > usage.TotalBytes {
		t.Fatalf("剩余空间 %d 大于总容量 %d（字段接反）", usage.FreeBytes, usage.TotalBytes)
	}
	if usage.UsedPercent < 0 || usage.UsedPercent > 100 {
		t.Fatalf("已用百分比应在 0-100，得到 %f", usage.UsedPercent)
	}
	// 盘根采样必须落在真实分区量级（>1GB），防"回落分支返回空结构"的假通过
	if usage.TotalBytes < 1<<30 {
		t.Fatalf("盘根总容量 %d 字节明显不合理，疑似回落分支返回了空结果", usage.TotalBytes)
	}

	// 不存在的盘符：物理盘映射解析不出盘符 → 回落分区采样 → gopsutil 报错
	if _, err := UsageOf(`ZZ:\definitely-missing`); err == nil {
		t.Fatal("不存在的盘符应返回错误，而不是伪造一份 0 用量")
	}

	// 失败路径必须返回零值结构：调用方（磁盘组件）一旦把"出错"的用量当成真实数据，
	// 界面就会显示 0GB / 0GB。这条断言把"失败即零值"的约定钉住。
	if failed, err := UsageOf(`ZZ:\definitely-missing`); err == nil || failed != (DiskUsage{}) {
		t.Fatalf("采样失败时应返回零值 DiskUsage，得到 %+v / %v", failed, err)
	}
}

// TestVolumeLetterExtraction 盘符提取：各种写法都能拿到盘符，无盘符的路径不会匹配到任何分区。
// 防的回归：把 "E:\Games\mc" 整串当盘符去查 WMI 导致永远回落，
// 或者相对路径被当成盘符让物理盘映射去查一个不存在的 map 键。
//
// 注意 UNC：filepath.VolumeName(`\\server\share\dir`) 在 Windows 上返回 `\\server\share`
// （filepath 的"卷"包含 UNC 共享名），所以这里不等于空串。
// 这不影响回落：WMI 映射的键是盘符，UNC 值查不到键，physicalUsage 仍会返回错误回落单分区。
func TestVolumeLetterExtraction(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"E:", "E:"},
		{`E:\`, "E:"},
		{`E:\Games\mc`, "E:"},
		{`e:\games`, "E:"},
		{`C:\Users\22907\AppData\Roaming\.minecraft`, "C:"},
		{`\\server\share\dir`, `\\SERVER\SHARE`},
		{"music", ""},
		{"", ""},
		{".", ""},
	}
	for _, testCase := range cases {
		if got := volumeLetter(testCase.path); got != testCase.want {
			t.Errorf("volumeLetter(%q) = %q，期望 %q", testCase.path, got, testCase.want)
		}
	}
}

// TestRefDeviceIDParsing 从 WMI 对象引用串里取 DeviceID。
// 防的回归：Win32_LogicalDiskToPartition 的引用格式解析失败（引号/反斜杠处理写偏），
// 物理盘映射整条链路静默退化成"只看单个分区"，系统盘的恢复分区容量被漏算。
func TestRefDeviceIDParsing(t *testing.T) {
	cases := []struct {
		name string
		ref  string
		want string
	}{
		{
			"逻辑盘引用取到盘符",
			`\\HOST\root\cimv2:Win32_LogicalDisk.DeviceID="E:"`,
			"E:",
		},
		{
			"分区引用取到 Disk #N, Partition #M",
			`\\HOST\root\cimv2:Win32_DiskPartition.DeviceID="Disk #0, Partition #1"`,
			"Disk #0, Partition #1",
		},
		{"空串", "", ""},
		{"没有引号赋值", `\\HOST\root\cimv2:Win32_LogicalDisk.DeviceID=E:`, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := refDeviceID(testCase.ref); got != testCase.want {
				t.Fatalf("refDeviceID(%q) = %q，期望 %q", testCase.ref, got, testCase.want)
			}
		})
	}
}

// TestPhysicalUsageNeverReturnsZeroUsage physicalUsage 要么给出合法的物理盘用量，
// 要么返回错误让调用方回落单分区语义 —— 不能返回"总量 0"的伪结果。
// 防的回归：求和循环一个分区都没算上时直接返回 0 用量，
// 上游把 0 当成真实容量（磁盘组件显示 0GB，用户以为硬盘掉了）。
func TestPhysicalUsageNeverReturnsZeroUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("物理盘映射要走 WMI，-short 模式跳过")
	}
	root := volumeRootOf(t)

	usage, err := physicalUsage(root)
	if err != nil {
		if !strings.Contains(err.Error(), "physical disk mapping unavailable") &&
			!strings.Contains(err.Error(), "WMI") &&
			!strings.Contains(err.Error(), "wmi") {
			t.Logf("physicalUsage(%s) 返回错误（按设计由 UsageOf 回落）：%v", root, err)
		}
		return
	}
	if usage.TotalBytes == 0 {
		t.Fatal("physicalUsage 成功返回时总容量不应为 0（应改为返回错误触发回落）")
	}
	if usage.FreeBytes > usage.TotalBytes {
		t.Fatalf("剩余空间 %d 大于总容量 %d", usage.FreeBytes, usage.TotalBytes)
	}
	if usage.UsedPercent < 0 || usage.UsedPercent > 100 {
		t.Fatalf("已用百分比应在 0-100，得到 %f", usage.UsedPercent)
	}
	if usage.Path != root {
		t.Fatalf("Path 应原样回填 %q，得到 %q", root, usage.Path)
	}

	// UNC 路径没有盘符：必须走错误分支回落，而不是把 map 查空后返回零值
	if _, err := physicalUsage(`\\server\share`); err == nil {
		t.Fatal("UNC 路径映射不出盘符，physicalUsage 应返回错误")
	}
}
