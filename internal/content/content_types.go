// content_types.go 内容元数据子系统的共享类型与常量。
//
// 本包按关注点拆分为多个文件（scan / archive / instance_icon / nbt / catalog），
// 这里集中放置它们共用的声明，避免在每个文件里重复。
package content

// GameContentEntry 内容页里一个条目（Mod/资源包/光影/存档）的展示信息。
type GameContentEntry struct {
	Name          string
	MetadataLine  string
	Description   string
	IconPath      string // 空串表示无图标文件
	FallbackGlyph string
	SourcePath    string
	IsDisabled    bool
}

// GameInstanceVisual 实例图标解析结果：图标路径（或内置图标符号）+ 兜底字形。
type GameInstanceVisual struct {
	IconPath      string
	FallbackGlyph string
}

// InstanceContext 内容扫描所需的实例上下文（C# 直接消费 GameInstanceSnapshot；
// 为避免 instance ↔ content 循环依赖，这里收敛为两个必需字段，见 PORTING_NOTES.md）。
type InstanceContext struct {
	// SourcePath 实例的目录来源（根目录或外部实例目录）。
	SourcePath string
	// MinecraftDirectory Minecraft 根目录。
	MinecraftDirectory string
}

// ExternalInstanceLayout 外部实例信息的最小投影（对应 ExternalGameInstanceLayout 子集）。
type ExternalInstanceLayout struct {
	InstanceId        string
	InstanceDirectory string
	LauncherRoot      string
}

// ExternalInstanceResolver 外部实例识别钩子：由 internal/instance 在包初始化时注入
// instance.TryResolveExternalInstance 的适配器；未注入时外部实例识别跳过。
var ExternalInstanceResolver func(sourcePath string) (ExternalInstanceLayout, bool)

const (
	maximumMetadataBytes = 2 * 1024 * 1024
	// maximumZipIconBytes 压缩包内图标条目大小上限（自定义图标上限见 customiconstore.go）。
	maximumZipIconBytes = 8 * 1024 * 1024
	// maximumIconCacheFiles 内容图标缓存文件数上限；触发时清理到一半。
	maximumIconCacheFiles = 600
	// maximumEntryCacheCount 缓存条数上限：超过后整体清空（轻量策略，避免长期驻留冷数据）。
	maximumEntryCacheCount = 4096
)
