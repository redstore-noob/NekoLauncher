package bindings

// ModpackAPI：整合包导出（Modrinth .mrpack / MultiMC .zip / CurseForge .zip）
// 与 NekoSolo 安装包（.exe）导出、导出配置档案。
// 事件：modpack:exportProgress（ModpackExportProgress，NekoSolo 导出复用同一事件）。

import (
	"strings"

	"nekolauncher/internal/config"
	"nekolauncher/internal/modpack"
	"nekolauncher/internal/solo"
)

// CollectExportContent 收集实例内容目录中可打包的内容单元。
func (a *ModpackAPI) CollectExportContent(contentDirectory string) []modpack.ModpackContentItem {
	return modpack.CollectContent(contentDirectory)
}

// ExportModpack 打包整合包到指定输出路径；进度经 modpack:exportProgress 事件推送。
// CurseForge 格式自动带出用户已保存的 API Key（指纹反查 projectID/fileID 用）。
func (a *ModpackAPI) ExportModpack(
	options modpack.ModpackExportOptions,
	contentDirectory, outputPath string,
) (modpack.ModpackExportResult, error) {
	if options.Format == modpack.FormatCurseForge && strings.TrimSpace(options.CurseForgeAPIKey) == "" {
		options.CurseForgeAPIKey = strings.TrimSpace(config.GetValue(curseForgeAPIKeyConfigKey))
	}
	ctx, done := a.beginExport()
	defer done()
	return modpack.Export(ctx, options, contentDirectory, outputPath, func(progress modpack.ModpackExportProgress) {
		emit(a.ctx, "modpack:exportProgress", progress)
	})
}

// NewExportOptions 带默认值的导出参数（ResolveModrinthLinks = true 等）。
func (a *ModpackAPI) NewExportOptions() modpack.ModpackExportOptions {
	return modpack.NewExportOptions()
}

// ---- NekoSolo 安装包（.exe） ----

// GetSoloStubStatus 查询 NekoSolo 安装器模板是否就绪（未就绪时 exe 导出不可用）。
func (a *ModpackAPI) GetSoloStubStatus() solo.StubStatus {
	return solo.StubTemplateStatus()
}

// DownloadSoloStub 在线获取安装器模板（从启动器的 GitHub Releases 下载到
// 存储目录），成功后 GetSoloStubStatus 即为就绪。
func (a *ModpackAPI) DownloadSoloStub() (solo.StubStatus, error) {
	path, err := solo.DownloadStubTemplate(callCtx(a.ctx))
	if err != nil {
		return solo.StubStatus{}, err
	}
	return solo.StubStatus{Found: true, Path: path}, nil
}

// ExportSoloPack 导出 NekoSolo 安装包（启动器 + 可选捆绑 Java + 整合包三合一 exe）；
// 进度复用 modpack:exportProgress 事件推送。
func (a *ModpackAPI) ExportSoloPack(options solo.SoloExportOptions, outputPath string) (modpack.ModpackExportResult, error) {
	ctx, done := a.beginExport()
	defer done()
	return solo.ExportSolo(ctx, options, outputPath, func(progress modpack.ModpackExportProgress) {
		emit(a.ctx, "modpack:exportProgress", progress)
	})
}

// ---- 导出配置档案（按版本目录持久化） ----

// LoadExportProfile 读取导出配置（无则空档案）。
func (a *ModpackAPI) LoadExportProfile(versionDirectory string) modpack.ModpackExportProfile {
	return modpack.LoadProfile(versionDirectory)
}

// SaveExportProfile 保存导出配置。
func (a *ModpackAPI) SaveExportProfile(versionDirectory string, profile modpack.ModpackExportProfile) bool {
	return modpack.SaveProfile(versionDirectory, profile)
}
