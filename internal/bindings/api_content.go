package bindings

// ContentAPI：实例内容（Mod / 资源包 / 光影 / 存档）扫描、自定义图标与存档操作。

import (
	"errors"

	"nekolauncher/internal/content"
	"nekolauncher/internal/instance"
)

// ---- 内容扫描 ----

// ReadMods 读取目录下全部 Mod。
func (a *ContentAPI) ReadMods(directory string) []content.GameContentEntry {
	return content.ReadMods(callCtx(a.ctx), directory)
}

// ReadResourcePacks 读取资源包。
func (a *ContentAPI) ReadResourcePacks(directory string) []content.GameContentEntry {
	return content.ReadResourcePacks(callCtx(a.ctx), directory)
}

// ReadShaders 读取光影包。
func (a *ContentAPI) ReadShaders(directory string) []content.GameContentEntry {
	return content.ReadShaders(callCtx(a.ctx), directory)
}

// ReadSaves 读取存档。
func (a *ContentAPI) ReadSaves(directory string) []content.GameContentEntry {
	return content.ReadSaves(callCtx(a.ctx), directory)
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

// ---- 存档操作 ----

// ExportSave 把存档目录打包为 .zip。
func (a *ContentAPI) ExportSave(saveDirectory, destinationZipPath string) (string, error) {
	return content.ExportSave(callCtx(a.ctx), saveDirectory, destinationZipPath)
}

// ImportSave 把存档压缩包解压到 savesDirectory，返回解压出的存档目录路径。
func (a *ContentAPI) ImportSave(archiveZipPath, savesDirectory string) (string, error) {
	return content.ImportSave(callCtx(a.ctx), archiveZipPath, savesDirectory)
}

// DeleteSave 删除存档目录。
func (a *ContentAPI) DeleteSave(saveDirectory string) error { return content.DeleteSave(saveDirectory) }

// ---- 存档快照（Rewind） ----

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
