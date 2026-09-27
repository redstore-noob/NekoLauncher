package bindings

// DownloadAPI：游戏/Loader/Java 下载任务、版本清单、下载源设置与内容下载。
// 对应 C# GameDownloadService / ManifestGet / JavaRuntimeInstaller /
// DownloadSettings / DownloadPauseGate / ContentInstallService。

import (
	"nekolauncher/internal/download"
	"nekolauncher/internal/models"
)

// ---- 下载任务 ----

// GetCurrentDownloadSnapshot 当前下载任务快照。
func (a *DownloadAPI) GetCurrentDownloadSnapshot() download.GameDownloadSnapshot {
	return a.service.Current()
}

// StartDownload 下载并安装原版 Minecraft。
func (a *DownloadAPI) StartDownload(version models.MinecraftVersion) bool {
	return a.service.Start(callCtx(a.ctx), version)
}

// StartModLoaderDownload 以 Mod Loader 模式下载（先确保原版，再叠加 Loader）。
func (a *DownloadAPI) StartModLoaderDownload(
	version models.MinecraftVersion,
	loader download.ModLoaderVersion,
	instanceName string,
	skipFabricApi bool,
) bool {
	return a.service.StartModLoader(callCtx(a.ctx), version, loader, instanceName, skipFabricApi)
}

// CancelDownload 取消当前任务。
func (a *DownloadAPI) CancelDownload() bool { return a.service.CancelActive() }

// PauseDownload 暂停全部下载（全局暂停门）。
func (a *DownloadAPI) PauseDownload() bool { return a.service.PauseActive() }

// ResumeDownload 恢复全部下载。
func (a *DownloadAPI) ResumeDownload() bool { return a.service.ResumeActive() }

// IsDownloadPaused 是否处于全局暂停。
func (a *DownloadAPI) IsDownloadPaused() bool { return download.IsDownloadPaused() }

// ---- 版本清单与 Loader 元数据 ----

// GetVersions 获取 Minecraft 版本清单（按发布时间降序）。
func (a *DownloadAPI) GetVersions() ([]models.MinecraftVersion, error) {
	return download.GetVersions(callCtx(a.ctx))
}

// ApplyVersionFilter 按关键字过滤版本列表。
func (a *DownloadAPI) ApplyVersionFilter(versions []models.MinecraftVersion, filter string) []models.MinecraftVersion {
	return download.ApplyVersionFilter(versions, filter)
}

// GetModLoaderVersions 获取指定 Loader 类型的版本列表。
func (a *DownloadAPI) GetModLoaderVersions(loaderType download.ModLoaderType, minecraftVersion string) ([]download.ModLoaderVersion, error) {
	return download.GetModLoaderVersions(callCtx(a.ctx), loaderType, minecraftVersion)
}

// CreateDefaultInstanceName 生成 Loader 实例默认名称。
func (a *DownloadAPI) CreateDefaultInstanceName(loaderType download.ModLoaderType, loaderVersion, minecraftVersion string) string {
	return download.CreateDefaultInstanceName(loaderType, loaderVersion, minecraftVersion)
}

// ---- Java 运行时 ----

// GetInstalledJavaRuntimes 已安装的托管 Java 运行时。
func (a *DownloadAPI) GetInstalledJavaRuntimes() []download.InstalledJavaRuntime {
	return download.GetInstalledRuntimes()
}

// DeleteJavaRuntime 删除指定运行时目录。
func (a *DownloadAPI) DeleteJavaRuntime(directoryPath string) error {
	return download.DeleteRuntime(directoryPath)
}

// QueryAvailableJavaVersions 查询指定发行方可下载的 JDK 列表。
func (a *DownloadAPI) QueryAvailableJavaVersions(vendor download.JavaVendor) ([]download.JavaDownloadCandidate, error) {
	return download.QueryAvailableJavaVersions(callCtx(a.ctx), vendor)
}

// InstallJavaRuntime 下载并安装指定 JDK；进度经 download:javaProgress 事件推送。
// 前端订阅方期望 {Percentage, Detail} 字段，这里从 JavaRuntimeInstallProgress
// （Phase/CompletedBytes/TotalBytes）映射，避免进度恒为 0、状态文本为空。
func (a *DownloadAPI) InstallJavaRuntime(candidate download.JavaDownloadCandidate) (*download.InstalledJavaRuntime, error) {
	var installer download.JavaRuntimeInstaller
	return installer.InstallCandidate(callCtx(a.ctx), candidate, func(progress download.JavaRuntimeInstallProgress) {
		percentage := 0.0
		if progress.TotalBytes > 0 {
			percentage = float64(progress.CompletedBytes) / float64(progress.TotalBytes) * 100
		}
		emit(a.ctx, "download:javaProgress", map[string]any{
			"Percentage": percentage,
			"Detail":     progress.Phase,
		})
	})
}

// ---- 下载源与设置 ----

// GetAllDownloadSources 内置下载源。
func (a *DownloadAPI) GetAllDownloadSources() []download.DownloadSource {
	return download.AllDownloadSources
}

// GetActiveDownloadSourceName 活跃下载源名称。
func (a *DownloadAPI) GetActiveDownloadSourceName() string { return download.ActiveSourceName() }

// SaveActiveDownloadSource 保存活跃下载源。
func (a *DownloadAPI) SaveActiveDownloadSource(source download.DownloadSource) {
	download.SaveActiveSource(source)
}

// GetFallbackDownloadSourceName 自动回退源名称（空串 = 禁用）。
func (a *DownloadAPI) GetFallbackDownloadSourceName() string { return download.FallbackSourceName() }

// SaveFallbackDownloadSource 保存自动回退源；nil = 禁用。
func (a *DownloadAPI) SaveFallbackDownloadSource(source *download.DownloadSource) {
	download.SaveFallbackSource(source)
}

// GetParallelDownloads 并行下载线程数。
func (a *DownloadAPI) GetParallelDownloads() int { return download.ParallelDownloads() }

// SaveParallelDownloads 保存并行下载线程数。
func (a *DownloadAPI) SaveParallelDownloads(count int) { download.SaveParallelDownloads(count) }

// GetSpeedLimitKbps 全局下载限速（KB/s，0 = 不限速）。
func (a *DownloadAPI) GetSpeedLimitKbps() int { return download.SpeedLimitKbps() }

// SaveSpeedLimitKbps 保存全局下载限速并立即生效（0 = 不限速）。
func (a *DownloadAPI) SaveSpeedLimitKbps(kbps int) { download.SaveSpeedLimitKbps(kbps) }

// GetSourceLatencies 并发测量全部内置下载源延迟（HEAD 版本清单，5 秒超时）；
// 失败的源 LatencyMs=-1。网络状态小组件使用。
func (a *DownloadAPI) GetSourceLatencies() []download.SourceLatency {
	return download.MeasureSourceLatencies(callCtx(a.ctx))
}

// ---- 内容下载（Mod / 资源包 / 光影 / 整合包） ----

// DownloadFileToInstance 下载文件到实例内容目录的子目录（mods 等），返回保存路径。
// 进度经 download:contentProgress 事件推送 {downloaded, total}。
func (a *DownloadAPI) DownloadFileToInstance(
	downloadURL, fileName, contentDirectory, subDirectory string,
) (string, error) {
	return download.DownloadFileToInstance(callCtx(a.ctx), downloadURL, fileName, contentDirectory, subDirectory,
		func(downloaded, total int64) {
			emit(a.ctx, "download:contentProgress", map[string]int64{"downloaded": downloaded, "total": total})
		})
}

// DownloadFileToPath 下载文件到任意路径。
func (a *DownloadAPI) DownloadFileToPath(downloadURL, targetPath string) error {
	return download.DownloadFileToPath(callCtx(a.ctx), downloadURL, targetPath,
		func(downloaded, total int64) {
			emit(a.ctx, "download:contentProgress", map[string]int64{"downloaded": downloaded, "total": total})
		})
}

// InstallModpackToInstance 解压 .mrpack / CurseForge .zip 到实例内容目录并下载声明文件。
// 返回 ModpackInstallResult{InstalledFiles, DownloadedMods, Errors}。
func (a *DownloadAPI) InstallModpackToInstance(mrpackPath, contentDirectory string) (*download.ModpackInstallResult, error) {
	return download.InstallModpack(callCtx(a.ctx), mrpackPath, contentDirectory,
		func(downloaded, total int64) {
			emit(a.ctx, "download:contentProgress", map[string]int64{"downloaded": downloaded, "total": total})
		})
}

// ReadModpackRequirements 读取整合包要求的 Minecraft / Loader 版本。
func (a *DownloadAPI) ReadModpackRequirements(mrpackPath string) (*download.ModpackRequirements, error) {
	return download.ReadModpackRequirements(callCtx(a.ctx), mrpackPath)
}

// ResolveContentDirectoryForInstance 解析实例内容目录（与启动隔离判定一致）。
func (a *DownloadAPI) ResolveContentDirectoryForInstance(minecraftDirectory, sourcePath, versionID string) string {
	return download.ResolveContentDirectoryForInstance(minecraftDirectory, sourcePath, versionID)
}
