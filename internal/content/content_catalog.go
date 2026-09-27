package content

// ---------------------------------------------------------------------------
// 内置图标目录
// ---------------------------------------------------------------------------

// defaultInstanceIconPath 按加载器名称给出内置 GameIcons 资源符号（"gameicon:{key}"）。
// UI 层将符号解码为程序集内嵌的 GameIcons PNG；Fabric、Quilt 及未知加载器用 command_block 兜底。
func defaultInstanceIconPath(loaderName string) string {
	switch loaderName {
	case "NeoForge":
		return "gameicon:neoforge"
	case "Forge":
		return "gameicon:forge"
	case "Fabric":
		return "gameicon:fabric"
	case "LiteLoader":
		return "gameicon:liteloader"
	case "原版":
		return "gameicon:vanilla"
	default:
		return "gameicon:command_block"
	}
}
