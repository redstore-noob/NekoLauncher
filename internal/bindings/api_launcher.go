package bindings

// LauncherAPI：游戏启动管线。对应 C# GameLaunchService / GameMemorySettings。

import (
	"path/filepath"

	"nekolauncher/internal/config"
	"nekolauncher/internal/instance"
	"nekolauncher/internal/launch"
)

// GetLaunchSnapshot 当前启动状态快照。
func (a *LauncherAPI) GetLaunchSnapshot() launch.GameLaunchSnapshot { return a.service.Current() }

// GetLogText 内存日志全文（启动 + 游戏输出；前端可配合 launch:changed 轮询刷新）。
func (a *LauncherAPI) GetLogText() string { return a.service.GetLogText() }

// Launch 启动当前选中的实例与账号；serverHost 非空时直接进服，worldName 非空时直接进存档。
// Wails 在后台 goroutine 调用命令，阻塞至启动流程结束（或失败）不会冻结 UI。
func (a *LauncherAPI) Launch(serverHost string, serverPort *int, worldName string) launch.LaunchResult {
	return a.service.LaunchSelected(callCtx(a.ctx), serverHost, serverPort, worldName)
}

// LaunchVersion 以显式版本启动（插件 API）：不改变"当前选中"的实例，
// 路径与用户手点启动完全一致（同一账号、同一校验管线）。
func (a *LauncherAPI) LaunchVersion(versionID, serverHost string, serverPort *int, worldName string) launch.LaunchResult {
	return a.service.LaunchExplicit(callCtx(a.ctx), versionID, serverHost, serverPort, worldName)
}

// StopGame 停止运行中的游戏进程树。
func (a *LauncherAPI) StopGame() launch.LaunchResult { return a.service.TryStopGame() }

// GetPlaytimeStats 全部实例的游玩时长统计，按累计时长降序（主页统计卡片数据源）。
// 前端应在 launch:changed 进入 Exited 阶段时重新拉取。
func (a *LauncherAPI) GetPlaytimeStats() []config.PlaytimeRecord {
	return config.GetPlaytimeStats()
}

// ---- 内存策略 ----

// GetSystemMemory 物理内存快照（MB）。
func (a *LauncherAPI) GetSystemMemory() launch.SystemMemorySnapshot { return launch.GetSystemMemory() }

// IsAutomaticMemoryAdjustmentEnabled 自动内存调整开关。
func (a *LauncherAPI) IsAutomaticMemoryAdjustmentEnabled() bool {
	return launch.GameMemorySettings.IsAutomaticAdjustmentEnabled()
}

// SetAutomaticMemoryAdjustmentEnabled 保存自动内存调整开关。
func (a *LauncherAPI) SetAutomaticMemoryAdjustmentEnabled(enabled bool) {
	launch.GameMemorySettings.SetAutomaticAdjustmentEnabled(enabled)
}

// GetMemorySliderMaximum 滑块上限（物理内存按 256MB 向下取整）。
func (a *LauncherAPI) GetMemorySliderMaximum() int {
	return launch.GameMemorySettings.SliderMaximumMemoryMb()
}

// GetManualMaximumMemoryMb 手动模式生效值。
func (a *LauncherAPI) GetManualMaximumMemoryMb() int {
	return launch.GameMemorySettings.ManualMaximumMemoryMb()
}

// SaveManualMaximumMemoryMb 保存手动内存上限。
func (a *LauncherAPI) SaveManualMaximumMemoryMb(memoryMb int) bool {
	return launch.GameMemorySettings.SaveManualMaximumMemoryMb(memoryMb)
}

// ---- 参数溯源（"为什么这样启动"） ----

// GetLaunchProvenance 返回最近一次成功启动的参数溯源报告：
// 最终 Java 命令行的每条参数来自哪里、有没有被后续参数覆盖。
//
// 只读；从未成功启动过时返回 nil（前端据此显示空状态）。
// 报告里的令牌类参数已在 Go 侧脱敏，不会下发到前端。
func (a *LauncherAPI) GetLaunchProvenance() *launch.LaunchProvenanceReport {
	return a.service.LastProvenance()
}

// GetLaunchProvenanceVersionId 上述报告对应的版本 id（无记录时为空串）。
func (a *LauncherAPI) GetLaunchProvenanceVersionId() string {
	return a.service.LastLaunchVersionId()
}

// ---- 崩溃诊断 ----

// DiagnoseCrash 诊断最近一次崩溃：读实例游戏目录下最新的 crash-reports/*.txt，
// 结合内存日志判定常见原因并给出处置建议。崩溃弹窗据此展示结论。
//
// 只读操作，不抛异常：没有报告、没有命中任何规则时也会返回可展示的结论文案。
func (a *LauncherAPI) DiagnoseCrash() launch.CrashDiagnosis {
	snapshot := instance.CurrentSnapshot()
	// 崩溃报告写在"游戏目录"（版本隔离时是 versions/<实例>），不是 .minecraft 根目录
	gameDirectory := instance.GameVersionIsolationGetGameDirectory(
		snapshot, snapshot.SelectedVersionId)
	if gameDirectory == "" {
		gameDirectory = snapshot.MinecraftDirectory
	}

	profile := config.Get(snapshot.MinecraftDirectory, snapshot.SelectedVersionId)
	memoryMb := profile.MaximumMemoryMb
	if memoryMb <= 0 {
		// 未开实例独立内存时，实际生效的是全局手动上限
		memoryMb = launch.GameMemorySettings.ManualMaximumMemoryMb()
	}

	return launch.DiagnoseCrash(launch.CrashDiagnosisInput{
		CrashReportsDirectory: filepath.Join(gameDirectory, "crash-reports"),
		LogText:               a.service.GetLogText(),
		MinecraftVersion:      snapshot.SelectedVersionId,
		JavaVersion:           config.JavaVersion(),
		MemoryMb:              memoryMb,
	})
}

// ---- 工具 ----

// DetectJavaMajorVersion 探测 Java 主版本号（失败返回 nil）。
func (a *LauncherAPI) DetectJavaMajorVersion(javaExecutable string) *int {
	return launch.TryDetectJavaMajorVersion(javaExecutable)
}

// DeleteDirectory 尝试删除目录树（用于实例目录清理）。
func (a *LauncherAPI) DeleteDirectory(directory string) { launch.TryDeleteDirectory(directory) }
