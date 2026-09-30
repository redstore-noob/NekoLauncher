// 实例隔离布局的解析入口。移植自 NyaLauncher.Core/Config/GameVersionProfileStore.cs
// 内的 GameVersionIsolation 静态类（C# 侧放在 Config 是历史原因，实际依赖 Launch 类型；
// Go 版收敛到 instance 包，见 PORTING_NOTES.md）。
package instance

import (
	"strings"

	"nekolauncher/internal/config"
	"nekolauncher/internal/tools"
)

// GameVersionIsolationResolve 解析指定实例的目录布局：外部实例（PCL/MultiMC 等目录）
// 直接采用其自带布局；否则按"实例显式设置 > 自动检测 > 全局默认兜底"的优先级解析。
// 全局默认只作兜底传入：若作为显式覆盖，其他启动器的隔离布局检测将永远不生效。
func GameVersionIsolationResolve(snapshot GameInstanceSnapshot, versionID string) GameVersionLayout {
	if external, ok := TryResolveExternalInstance(snapshot.SourcePath); ok &&
		strings.EqualFold(external.InstanceId, versionID) {
		return GameVersionLayout{
			IsIsolated:       true,
			ContentDirectory: external.ContentDirectory,
			Provider:         external.Provider,
			Evidence:         external.Evidence,
		}
	}

	profile := config.Get(snapshot.MinecraftDirectory, versionID)
	return ResolveLayout(
		snapshot.MinecraftDirectory,
		snapshot.SourcePath,
		versionID,
		profile.IsVersionIsolationEnabled,
		tools.BoolPtr(config.DefaultVersionIsolation()),
	)
}

// GameVersionIsolationGetGameDirectory 隔离布局下的游戏目录；共享目录布局返回空串（沿用根目录）。
func GameVersionIsolationGetGameDirectory(snapshot GameInstanceSnapshot, versionID string) string {
	layout := GameVersionIsolationResolve(snapshot, versionID)
	if layout.IsIsolated {
		return layout.ContentDirectory
	}
	return ""
}

// GameVersionIsolationGetContentDirectory 指定实例的内容目录（隔离或共享）。
func GameVersionIsolationGetContentDirectory(snapshot GameInstanceSnapshot, versionID string) string {
	return GameVersionIsolationResolve(snapshot, versionID).ContentDirectory
}
