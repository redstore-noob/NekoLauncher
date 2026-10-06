package launch

import (
	"fmt"
	"strings"
	"time"
)

// 本文件定义 internal/launch 对 internal/instance（C# GameInstanceStore /
// GameInstanceLayoutResolver / GameVersionIsolation）的最小依赖接口。
// 这四类实例管理功能由另一个工作流移植到 internal/instance；移植完成后由宿主
// 在初始化时注入下列钩子即可接入启动管线（详见 PORTING_NOTES.md）。

// GameInstanceSnapshot 启动前置校验所需的最小实例视图
// （对应 C# GameInstanceSnapshot 的相关字段）。
type GameInstanceSnapshot struct {
	IsLoading          bool
	ErrorMessage       string
	SelectedVersionId  string
	MinecraftDirectory string
	// VersionIds 已安装版本列表：显式版本启动（插件 API）用它校验版本存在性。
	VersionIds []string
	// SourcePath 实例来源路径（用于外部启动器实例识别与版本隔离解析）。
	SourcePath string
}

// ExternalInstanceInfo 已识别的外部启动器实例信息。
type ExternalInstanceInfo struct {
	// InstanceId 外部实例的稳定标识。
	InstanceId string
	// Provider 提供方显示名（MultiMC / CurseForge 等）。
	Provider string
}

// InstanceSnapshotProvider 宿主注入：返回当前选中的实例快照
// （对应 C# GameInstanceStore.Current）。为 nil 时启动前置校验直接失败。
var InstanceSnapshotProvider func() GameInstanceSnapshot

// InstanceSnapshotWaiter 宿主注入：阻塞至快照离开加载态（或超时）后返回快照。
// 对应 C# 启动流程对首次扫描完成的等待；为 nil 时回退 InstanceSnapshotProvider。
var InstanceSnapshotWaiter func(timeout time.Duration) GameInstanceSnapshot

// launchReadyWaitTimeout 启动前等待首次实例扫描完成的最长时长：
// 扫描通常毫秒级完成，该上限只为极端慢盘兜底。
const launchReadyWaitTimeout = 10 * time.Second

// waitForInstanceSnapshot 供启动前置校验使用：优先等待存储就绪，
// 钩子缺省时回退 currentInstanceSnapshot（未接线的"尚未就绪"视图）。
func waitForInstanceSnapshot() GameInstanceSnapshot {
	if InstanceSnapshotWaiter != nil {
		return InstanceSnapshotWaiter(launchReadyWaitTimeout)
	}
	return currentInstanceSnapshot()
}

// ExternalInstanceResolver 宿主注入：解析实例来源路径是否为外部启动器实例
// （对应 C# GameInstanceLayoutResolver.TryResolveExternalInstance）。
// ok 为 false 表示不是外部实例。为 nil 时视为非外部实例。
var ExternalInstanceResolver func(sourcePath string) (info ExternalInstanceInfo, ok bool)

// IsolatedGameDirectoryResolver 宿主注入：按版本隔离设置解析实例的游戏目录
// （对应 C# GameVersionIsolation.GetGameDirectory）。
// 返回空串表示不隔离（使用 Minecraft 根目录）。为 nil 时不隔离。
var IsolatedGameDirectoryResolver func(minecraftDirectory, sourcePath, versionId string) string

// PreLaunchSnapshotHook 宿主注入：在游戏进程拉起**之前**为实例留一个"还原点"。
//
// 由 internal/content 的 Rewind 快照引擎实现（bindings 层接线）。设计要点：
//   - **尽力而为**：返回的 error 只记日志，绝不阻断启动。存档快照失败
//     （磁盘满、权限不足）不该让玩家玩不了游戏。
//   - 是否真的创建由注入方决定（受"启动前自动备份"开关与"内容是否变化"控制），
//     这里只负责在正确的时机调用。
//   - gameDirectory 是实例的**游戏目录**（隔离时是 versions/<实例>），
//     不是 .minecraft 根目录——还原点必须落在真正会被改动的那份存档上。
//
// 为 nil 时完全跳过（无钩子 = 无此功能，零开销）。
var PreLaunchSnapshotHook func(gameDirectory, versionId string) error

// runPreLaunchSnapshot 调用启动前快照钩子；返回给用户看的提示行（可为空）。
func runPreLaunchSnapshot(gameDirectory, versionId string) string {
	if PreLaunchSnapshotHook == nil || strings.TrimSpace(gameDirectory) == "" {
		return ""
	}
	if err := PreLaunchSnapshotHook(gameDirectory, versionId); err != nil {
		// 尽力而为：失败只提示，不阻断
		return fmt.Sprintf("启动前自动备份未完成（不影响游戏启动）：%v", err)
	}
	return ""
}

// currentInstanceSnapshot 读取当前实例快照（钩子缺省时返回"未就绪"视图）。
func currentInstanceSnapshot() GameInstanceSnapshot {
	if InstanceSnapshotProvider != nil {
		return InstanceSnapshotProvider()
	}
	return GameInstanceSnapshot{
		IsLoading:    true,
		ErrorMessage: "实例管理尚未就绪。",
	}
}

// resolveExternalInstance 解析外部实例（钩子缺省时返回 false）。
func resolveExternalInstance(sourcePath string) (ExternalInstanceInfo, bool) {
	if ExternalInstanceResolver == nil {
		return ExternalInstanceInfo{}, false
	}
	return ExternalInstanceResolver(sourcePath)
}

// resolveIsolatedGameDirectory 解析隔离游戏目录（钩子缺省时返回空串 = 不隔离）。
func resolveIsolatedGameDirectory(minecraftDirectory, sourcePath, versionId string) string {
	if IsolatedGameDirectoryResolver == nil {
		return ""
	}
	return IsolatedGameDirectoryResolver(minecraftDirectory, sourcePath, versionId)
}
