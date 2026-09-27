package bindings

// WorldAPI：扫描全部实例的存档目录，提供最近游玩的世界列表。

import (
	"nekolauncher/internal/instance"
	"nekolauncher/internal/world"
)

// GetRecentWorlds 最近游玩的世界（按 LastPlayed 降序取前 max 个）。
func (a *WorldAPI) GetRecentWorlds(max int) []world.WorldInfo {
	return world.GetRecentWorlds(instance.CurrentSnapshot(), max)
}
