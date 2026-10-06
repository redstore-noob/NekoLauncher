package bindings

// ContentAPI：实例内容（Mod / 资源包 / 光影 / 存档）扫描、自定义图标与存档操作。

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"nekolauncher/internal/content"
	"nekolauncher/internal/instance"
	"nekolauncher/internal/logs"
	"nekolauncher/internal/modname"
	"nekolauncher/internal/tools"
)

// ---- 内容扫描 ----

// ReadSaves 读取存档目录列表（每个子目录一个条目）。
//
// 目录必须落在已知游戏根目录内：这个绑定同时被插件 API（getSaves）使用，不校验时
// 它就是一个"任意目录枚举器"——传任意可读目录即可列出其子目录名与路径。越界按
// "读不到"返回空列表（与该函数原有的失败语义一致）并记一条 WARN，让插件作者在
// 运行日志里能看出是被拒绝，而不是"这个实例没有存档"。
func (a *ContentAPI) ReadSaves(directory string) []content.GameContentEntry {
	if !insideKnownGameRoot(directory) {
		logs.Write("WARN", fmt.Sprintf(
			"拒绝读取已知游戏目录之外的存档目录：%s", strings.TrimSpace(directory)))

		return []content.GameContentEntry{}
	}
	return content.ReadSaves(callCtx(a.ctx), directory)
}

// AnalyzeModConflicts 检测当前选中实例的 mod 冲突：缺前置、重复 mod、
// 声明式不兼容、加载器 / MC 版本不匹配。
//
// 纯只读：只打开 mods 目录下的 jar 读元数据，不改动任何文件。
// 前端应在"装完新 mod"与"启动失败"两个时机调用它。
// 实例版本 / 加载器解析失败时相应检查自动跳过（宁可漏报，不误报）。
func (a *ContentAPI) AnalyzeModConflicts() content.ModConflictReport {
	snapshot := instance.CurrentSnapshot()
	contentDirectory := instance.GameVersionIsolationGetContentDirectory(
		snapshot, snapshot.SelectedVersionId)
	if contentDirectory == "" {
		contentDirectory = snapshot.MinecraftDirectory
	}

	// 版本 / 加载器尽力解析：拿不到就传空串，检测侧会跳过对应规则
	var gameVersion, loaderName string
	if snapshot.SelectedVersionId != "" {
		if details, err := instance.LoadDetails(callCtx(a.ctx), snapshot,
			snapshot.SelectedVersionId); err == nil {
			gameVersion = strings.TrimSpace(details.BaseGameVersion)
			loaderName = strings.TrimSpace(details.LoaderName)
			switch gameVersion {
			case "未识别", "未知", "未提供":
				gameVersion = ""
			}
		}
	}

	return content.AnalyzeModConflicts(
		filepath.Join(contentDirectory, "mods"),
		gameVersion,
		normalizeLoaderName(loaderName),
	)
}

// normalizeLoaderName 把实例详情里的加载器展示名收敛成检测侧认识的 id。
// 认不出来时返回空串（检测侧据此跳过加载器匹配，不误报）。
func normalizeLoaderName(name string) string {
	lowered := strings.ToLower(strings.TrimSpace(name))
	switch {
	case lowered == "" || lowered == "原版" || lowered == "vanilla":
		return "vanilla"
	case strings.Contains(lowered, "neoforge"):
		return "neoforge"
	case strings.Contains(lowered, "forge"):
		return "forge"
	case strings.Contains(lowered, "quilt"):
		return "quilt"
	case strings.Contains(lowered, "fabric"):
		return "fabric"
	}
	return ""
}

// GetInstanceVisual 解析实例图标（当前选中目录上下文）。
func (a *ContentAPI) GetInstanceVisual(versionID, loaderName string) content.GameInstanceVisual {
	snapshot := instance.CurrentSnapshot()
	return content.ResolveInstanceVisual(content.InstanceContext{
		SourcePath:         snapshot.SourcePath,
		MinecraftDirectory: snapshot.MinecraftDirectory,
	}, versionID, loaderName)
}

// ---- 自定义图标 ----

// SetCustomIcon 设置自定义图标（校验扩展名与 8MB 上限），返回存储路径。
func (a *ContentAPI) SetCustomIcon(minecraftDirectory, versionID, sourcePath string) (string, error) {
	return content.SetCustomIcon(minecraftDirectory, versionID, sourcePath)
}

// RemoveCustomIcon 移除自定义图标。
func (a *ContentAPI) RemoveCustomIcon(minecraftDirectory, versionID string) bool {
	return content.RemoveCustomIcon(minecraftDirectory, versionID)
}

// CopyFileIntoDirectory 把外部文件复制到指定目录（拖拽安装 .jar/.zip 用）。
func (a *ContentAPI) CopyFileIntoDirectory(sourcePath, destinationDir string) (string, error) {
	return content.CopyFileIntoDirectory(sourcePath, destinationDir)
}

// SniffZipKind 依据 zip 内部结构判断内容类别（拖拽安装路由用）：
// shaderpack / resourcepack / save / unknown。
func (a *ContentAPI) SniffZipKind(sourcePath string) string {
	return content.SniffZipKind(sourcePath)
}

// ---- 存档操作 ----

// ExportSave 把存档目录打包为 .zip。
func (a *ContentAPI) ExportSave(saveDirectory, destinationZipPath string) (string, error) {
	return content.ExportSave(callCtx(a.ctx), saveDirectory, tools.SanitizeSavePath(destinationZipPath))
}

// ImportSave 把存档压缩包解压到 savesDirectory，返回解压出的存档目录路径。
func (a *ContentAPI) ImportSave(archiveZipPath, savesDirectory string) (string, error) {
	return content.ImportSave(callCtx(a.ctx), archiveZipPath, savesDirectory)
}

// DeleteSave 删除存档目录。
func (a *ContentAPI) DeleteSave(saveDirectory string) error { return content.DeleteSave(saveDirectory) }

// ---- 模组中文名 ----
// 译名优先取 SCL 社区数据集（gitee 静态文件），未收录时检索
// MC百科（mcmod.cn）搜索匹配，本地持久缓存；关于页有声明。

// LookupModNameTranslations 立即返回已知译名（只查本地缓存，不发请求）。
// key 为传入的原始文件名，只包含有命中的条目。
func (a *ContentAPI) LookupModNameTranslations(fileNames []string) map[string]string {
	return modname.Lookup(fileNames)
}

// RefreshModNameTranslations 异步为未命中的模组补查（限流；结果落盘后发出
// "modname:updated" 事件，前端收到后重新调用 Lookup 即可拿到新译名）。
func (a *ContentAPI) RefreshModNameTranslations(fileNames []string) {
	modname.RefreshAsync(fileNames)
}

// ---- 存档快照（Rewind） ----

// RewindSummary 汇总整个回溯仓库：快照总数、数据块占用与预算、最近快照。
// 主页小组件用它展示一眼概览，不需要指定某个存档或实例。
func (a *ContentAPI) RewindSummary() content.RewindSummary {
	return content.ComputeRewindSummary()
}

// CreateLaunchSnapshotForWorld 在启动游戏前给某个存档创建"还原点"。
//
// 内容相对最近快照没有变化时**跳过**（连续启动不会堆出一串一模一样的还原点），
// 返回 created=false。返回的 snapshot 在跳过时为零值。
//
// 自动还原点计入 Rewind 的自动快照上限，会被正常淘汰，不会无限占盘。
func (a *ContentAPI) CreateLaunchSnapshotForWorld(
	worldDirectory string,
) (content.SaveSnapshot, bool, error) {
	return content.CreateLaunchSnapshot(callCtx(a.ctx), worldDirectory)
}

// ListSaveSnapshots 列出某个存档的全部快照（新的在前）。
func (a *ContentAPI) ListSaveSnapshots(saveDirectory string) []content.SaveSnapshot {
	return content.ListSaveSnapshots(saveDirectory)
}

// CreateSaveSnapshot 给存档当前状态创建一个快照（color 为可选标记颜色）。
func (a *ContentAPI) CreateSaveSnapshot(saveDirectory, label, color string) (content.SaveSnapshot, error) {
	return content.CreateSaveSnapshot(callCtx(a.ctx), saveDirectory, label, color)
}

// SetSaveSnapshotColor 更新快照的标记颜色（空串恢复默认色）。
func (a *ContentAPI) SetSaveSnapshotColor(saveDirectory, snapshotID, color string) error {
	return content.SetSaveSnapshotColor(saveDirectory, snapshotID, color)
}

// RollbackSaveSnapshot 把存档回滚到指定快照（回滚前自动创建安全快照）。
func (a *ContentAPI) RollbackSaveSnapshot(saveDirectory, snapshotID string) (content.SaveSnapshot, error) {
	return content.RollbackSaveSnapshot(callCtx(a.ctx), saveDirectory, snapshotID)
}

// DeleteSaveSnapshot 删除指定快照并回收数据块。
func (a *ContentAPI) DeleteSaveSnapshot(saveDirectory, snapshotID string) error {
	return content.DeleteSaveSnapshot(saveDirectory, snapshotID)
}

// ---- 实例快照（Rewind） ----

// ensureGameNotBusy 实例级快照操作的运行中守卫：游戏在准备或运行时，实例目录正被
// 进程活跃写入，快照会定格"写了一半"的状态，回滚则可能损坏运行中的游戏。
// 存档级操作不加此守卫（保持既有行为：游戏运行中也可以给单个存档拍快照）。
func (a *ContentAPI) ensureGameNotBusy() error {
	if a.launch == nil {
		return nil
	}
	if a.launch.Current().ShouldShowIndicator() {
		return errors.New("游戏正在启动或运行中，无法创建或回滚实例快照；请先退出游戏")
	}
	return nil
}

// ListInstanceSnapshots 列出某个实例游戏目录的全部快照（新的在前）。
func (a *ContentAPI) ListInstanceSnapshots(instanceDirectory string) []content.SaveSnapshot {
	return content.ListInstanceSnapshots(instanceDirectory)
}

// CreateInstanceSnapshot 给实例当前状态创建一个快照（游戏运行中会被拒绝）。
// 实例快照不包含 libraries、assets 等可重新下载的目录，回滚时它们原样保留。
func (a *ContentAPI) CreateInstanceSnapshot(instanceDirectory, label, color string) (content.SaveSnapshot, error) {
	if err := a.ensureGameNotBusy(); err != nil {
		return content.SaveSnapshot{}, err
	}
	return content.CreateInstanceSnapshot(callCtx(a.ctx), instanceDirectory, label, color)
}

// SetInstanceSnapshotColor 更新实例快照的标记颜色（空串恢复默认色）。
func (a *ContentAPI) SetInstanceSnapshotColor(instanceDirectory, snapshotID, color string) error {
	return content.SetInstanceSnapshotColor(instanceDirectory, snapshotID, color)
}

// RollbackInstanceSnapshot 把实例回滚到指定快照（回滚前自动创建安全快照；游戏运行中会被拒绝）。
func (a *ContentAPI) RollbackInstanceSnapshot(instanceDirectory, snapshotID string) (content.SaveSnapshot, error) {
	if err := a.ensureGameNotBusy(); err != nil {
		return content.SaveSnapshot{}, err
	}
	return content.RollbackInstanceSnapshot(callCtx(a.ctx), instanceDirectory, snapshotID)
}

// DeleteInstanceSnapshot 删除指定实例快照并回收数据块。
func (a *ContentAPI) DeleteInstanceSnapshot(instanceDirectory, snapshotID string) error {
	return content.DeleteInstanceSnapshot(instanceDirectory, snapshotID)
}
