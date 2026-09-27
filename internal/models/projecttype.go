// projecttype.go 资源项目类型的统一展示语义。
//
// 为什么单独抽出来：Modrinth 与 CurseForge 用两套完全不同的字段表示"这是什么资源"
// （前者是 project_type 字符串，后者是 CurseForge 的数字 classId），但界面上的
// 中文名、图标、以及"装进哪个子目录"必须完全一致——两处各写一份映射，
// 迟早出现"CurseForge 的整合包显示成 Mod"这类错位。
package models

// 资源项目类型（内部统一取值，与 Modrinth 的 project_type 一致）。
const (
	ProjectTypeMod          = "mod"
	ProjectTypeModpack      = "modpack"
	ProjectTypeShader       = "shader"
	ProjectTypeResourcePack = "resourcepack"
	ProjectTypePlugin       = "plugin"
)

// CurseForge Minecraft 分类 ID（classId）。取自 CurseForge 官方文档，
// 只有这四类能在界面上与 Modrinth 的类型对齐。
const (
	CurseForgeClassMods          = 6
	CurseForgeClassResourcePacks = 12
	CurseForgeClassModpacks      = 4471
	CurseForgeClassShaders       = 6552
	CurseForgeClassBukkitPlugins = 5
)

// ProjectTypeFromCurseForgeClass 把 CurseForge classId 映射为统一类型；
// 不认识（或为 nil）时返回空串，由调用方按"未知类型"处理。
func ProjectTypeFromCurseForgeClass(classID *int) string {
	if classID == nil {
		return ""
	}
	switch *classID {
	case CurseForgeClassMods:
		return ProjectTypeMod
	case CurseForgeClassModpacks:
		return ProjectTypeModpack
	case CurseForgeClassShaders:
		return ProjectTypeShader
	case CurseForgeClassResourcePacks:
		return ProjectTypeResourcePack
	case CurseForgeClassBukkitPlugins:
		return ProjectTypePlugin
	default:
		return ""
	}
}

// CurseForgeClassFromProjectType 统一类型 → CurseForge classId（0 = 无对应分类）。
func CurseForgeClassFromProjectType(projectType string) int {
	switch NormalizeProjectType(projectType) {
	case ProjectTypeMod:
		return CurseForgeClassMods
	case ProjectTypeModpack:
		return CurseForgeClassModpacks
	case ProjectTypeShader:
		return CurseForgeClassShaders
	case ProjectTypeResourcePack:
		return CurseForgeClassResourcePacks
	default:
		return 0
	}
}

// NormalizeProjectType 归一化项目类型：把界面上可能出现的别名（shaderpack /
// resource_pack / "光影包" 等）折算成统一取值，避免调用方各自判断。
//
// 资源包/光影包在 Modrinth 叫 resourcepack / shader，在下载页的实例目录叫
// resourcepacks / shaderpacks，历史上两套叫法混用——这里统一收口。
func NormalizeProjectType(projectType string) string {
	switch projectType {
	case ProjectTypeMod, "mods":
		return ProjectTypeMod
	case ProjectTypeModpack, "modpacks", "整合包":
		return ProjectTypeModpack
	case ProjectTypeShader, "shaderpack", "shaderpacks", "光影包":
		return ProjectTypeShader
	case ProjectTypeResourcePack, "resourcepacks", "材质包":
		return ProjectTypeResourcePack
	case ProjectTypePlugin, "plugins":
		return ProjectTypePlugin
	default:
		return projectType
	}
}

// ProjectTypeDisplay 项目类型的中文显示名（Modrinth / CurseForge 共用）。
func ProjectTypeDisplay(projectType string) string {
	switch NormalizeProjectType(projectType) {
	case ProjectTypeMod:
		return "Mod"
	case ProjectTypeModpack:
		return "整合包"
	case ProjectTypeShader:
		return "光影包"
	case ProjectTypeResourcePack:
		return "材质包"
	case ProjectTypePlugin:
		return "服务端插件"
	default:
		return projectType
	}
}

// ProjectTypeIcon 项目类型对应图标（终端字体中的近似图标，与 C# 版本保持一致）。
func ProjectTypeIcon(projectType string) string {
	switch NormalizeProjectType(projectType) {
	case ProjectTypeMod:
		return "⬜"
	case ProjectTypeModpack:
		return "📦"
	case ProjectTypeShader:
		return "☀️"
	case ProjectTypeResourcePack:
		return "🎨"
	case ProjectTypePlugin:
		return "🔌"
	default:
		return "📄"
	}
}

// SubDirectoryForProjectType 项目类型 → 实例内的内容子目录。
// 空串表示需要由调用方另行决定（例如整合包要走安装流程而不是丢进某个目录）。
func SubDirectoryForProjectType(projectType string) string {
	switch NormalizeProjectType(projectType) {
	case ProjectTypeMod:
		return "mods"
	case ProjectTypeShader:
		return "shaderpacks"
	case ProjectTypeResourcePack:
		return "resourcepacks"
	default:
		return ""
	}
}
