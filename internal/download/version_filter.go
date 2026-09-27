package download

import "nekolauncher/internal/models"

// VersionFilter Minecraft 版本筛选服务。对应 C# VersionFilter。

// ApplyVersionFilter 根据筛选类型过滤版本列表。
// filter: "all" 全部, "release" 正式版, "snapshot" 快照版, "old" 远古版本。
func ApplyVersionFilter(versions []models.MinecraftVersion, filter string) []models.MinecraftVersion {
	switch filter {
	case "release":
		return filterVersions(versions, func(v models.MinecraftVersion) bool { return v.Type == "release" })
	case "snapshot":
		return filterVersions(versions, func(v models.MinecraftVersion) bool { return v.Type == "snapshot" })
	case "old":
		return filterVersions(versions, func(v models.MinecraftVersion) bool {
			return v.Type == "old_beta" || v.Type == "old_alpha"
		})
	default:
		return versions
	}
}

func filterVersions(versions []models.MinecraftVersion, predicate func(models.MinecraftVersion) bool) []models.MinecraftVersion {
	result := make([]models.MinecraftVersion, 0, len(versions))
	for _, v := range versions {
		if predicate(v) {
			result = append(result, v)
		}
	}
	return result
}
