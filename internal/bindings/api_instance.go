package bindings

// InstanceAPI：实例扫描、选中、详情、重命名与隔离布局。
// 事件：instance:changed（GameInstanceSnapshot）。

import (
	"strings"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/instance"
)

// GetCurrentInstanceSnapshot 当前已发布的实例快照。
func (a *InstanceAPI) GetCurrentInstanceSnapshot() instance.GameInstanceSnapshot {
	return instance.CurrentSnapshot()
}

// RefreshInstances 扫描并发布新快照（默认用配置的游戏目录；可显式传 path）。
// path 传空串时使用当前配置来源。
func (a *InstanceAPI) RefreshInstances(path string) instance.GameInstanceSnapshot {
	if path == "" {
		path = instance.ResolveConfiguredSourcePath()
	}
	return instance.Refresh(callCtx(a.ctx), path)
}

// SelectInstance 选中实例；扫描中 / 出错 / 不存在返回 false。
func (a *InstanceAPI) SelectInstance(versionID string) bool { return instance.Select(versionID) }

// ---- 目录定位（MinecraftDirectoryLocator） ----

// GetDefaultMinecraftDirectory 平台默认 .minecraft 目录。
func (a *InstanceAPI) GetDefaultMinecraftDirectory() string { return instance.GetDefaultDirectory() }

// EnsureDefaultMinecraftDirectory 确保默认目录存在并返回。
func (a *InstanceAPI) EnsureDefaultMinecraftDirectory() string {
	return instance.EnsureDefaultDirectory()
}

// GetInstalledVersionIds 枚举目录下已安装版本。
func (a *InstanceAPI) GetInstalledVersionIds(minecraftDirectory string) []string {
	return instance.GetInstalledVersionIds(minecraftDirectory)
}

// ---- 详情 / 重命名 ----

// readySnapshot 等待首次实例扫描完成（毫秒级）后返回当前快照。
// 详情/改名入口若在启动早期被调用，会在加载态快照上操作：
// 目录字段为空导致重命名报"原版本文件夹不存在"、详情为空。
func (a *InstanceAPI) readySnapshot() instance.GameInstanceSnapshot {
	return instance.WaitForReady(10 * time.Second)
}

// GetVersionDetails 装载版本详情（加载器识别 + 内容扫描）。
func (a *InstanceAPI) GetVersionDetails(versionID string) (instance.GameVersionDetails, error) {
	return instance.LoadDetails(callCtx(a.ctx), a.readySnapshot(), versionID)
}

// GetInstanceDisplayVersion 返回实例用于界面展示的「游戏版本」号（如 26.2 / 1.12.2）。
// 快速启动面板等处若直接展示版本目录 ID，加载器实例会显示成
// fabric-loader-0.19.5-26.2 这类加载器 ID；正确字段是版本详情的 BaseGameVersion
// （沿 inheritsFrom / clientVersion 解析的基础版本）。本方法对前端给出明确可用的入口：
// 解析失败或未识别时回落为版本目录 ID 本身。
func (a *InstanceAPI) GetInstanceDisplayVersion(versionID string) (string, error) {
	details, err := instance.LoadDetails(callCtx(a.ctx), a.readySnapshot(), versionID)
	if err != nil {
		return versionID, err
	}
	base := strings.TrimSpace(details.BaseGameVersion)
	if base == "" || base == "未识别" || strings.EqualFold(base, versionID) {
		return versionID, nil
	}
	return base, nil
}

// RenameInstance 重命名版本目录并修补 inheritsFrom / jar 引用，返回实际新版本 ID。
func (a *InstanceAPI) RenameInstance(oldVersionID, requestedVersionID string) (string, error) {
	return instance.RenameVersion(callCtx(a.ctx), a.readySnapshot().MinecraftDirectory, oldVersionID, requestedVersionID)
}

// CopyInstance 复制实例（目录整体拷贝 + 版本 JSON id 补丁 + 实例档案克隆），
// 返回新版本 ID。外部启动器实例走 CopyExternalVersion 复制为主目录标准版本。
func (a *InstanceAPI) CopyInstance(sourceVersionID, requestedVersionID string) (string, error) {
	snapshot := a.readySnapshot()
	// 当前选中的是外部启动器实例（快照为单版本且可被外部识别）时走外部复制路径
	if strings.EqualFold(snapshot.SelectedVersionId, sourceVersionID) {
		if layout, ok := instance.TryResolveExternalInstance(snapshot.SourcePath); ok {
			return instance.CopyExternalVersion(callCtx(a.ctx), layout, config.GameDirectory(), requestedVersionID)
		}
	}
	return instance.CopyVersion(callCtx(a.ctx), snapshot.MinecraftDirectory, sourceVersionID, requestedVersionID)
}

// ---- 隔离布局（GameVersionIsolation） ----

// ResolveInstanceIsolation 解析实例布局。
func (a *InstanceAPI) ResolveInstanceIsolation(snapshot instance.GameInstanceSnapshot, versionID string) instance.GameVersionLayout {
	return instance.GameVersionIsolationResolve(snapshot, versionID)
}

// TryResolveExternalInstance 识别 MultiMC / PCL 等外部启动器实例。
func (a *InstanceAPI) TryResolveExternalInstance(sourcePath string) (instance.ExternalGameInstanceLayout, bool) {
	return instance.TryResolveExternalInstance(sourcePath)
}
