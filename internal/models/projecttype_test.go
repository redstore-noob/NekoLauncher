package models

// 资源项目类型的展示语义用例。
//
// 防的是"两个资源站的同一类资源显示不一致"：Modrinth 用 project_type 字符串、
// CurseForge 用数字 classId，两边一旦各写一份映射，就会出现"CurseForge 的整合包
// 显示成 Mod、装进 mods 目录"这种既看不出错、又确实装错位置的回归。

import "testing"

// TestProjectTypeDisplayAndIcon 防的回归：类型 ↔ 中文名/图标的对应关系错位。
func TestProjectTypeDisplayAndIcon(t *testing.T) {
	cases := []struct {
		projectType string
		display     string
		icon        string
	}{
		{"mod", "Mod", "⬜"},
		{"modpack", "整合包", "📦"},
		{"shader", "光影包", "☀️"},
		{"resourcepack", "材质包", "🎨"},
		{"plugin", "服务端插件", "🔌"},
		{"datapack", "datapack", "📄"}, // 未收录类型：原样返回，不伪装成 Mod
	}

	for _, tc := range cases {
		if got := ProjectTypeDisplay(tc.projectType); got != tc.display {
			t.Fatalf("ProjectTypeDisplay(%q) = %q，期望 %q", tc.projectType, got, tc.display)
		}
		if got := ProjectTypeIcon(tc.projectType); got != tc.icon {
			t.Fatalf("ProjectTypeIcon(%q) = %q，期望 %q", tc.projectType, got, tc.icon)
		}
	}
}

// TestNormalizeProjectTypeAliases 防的回归：
// 别名（shaderpack / resourcepacks / 中文名）没被归一化，
// 于是界面上同一个类型出现两种取值、下载落点也跟着分叉。
func TestNormalizeProjectTypeAliases(t *testing.T) {
	cases := map[string]string{
		"mod":           ProjectTypeMod,
		"mods":          ProjectTypeMod,
		"modpack":       ProjectTypeModpack,
		"整合包":           ProjectTypeModpack,
		"shader":        ProjectTypeShader,
		"shaderpack":    ProjectTypeShader,
		"光影包":           ProjectTypeShader,
		"resourcepack":  ProjectTypeResourcePack,
		"resourcepacks": ProjectTypeResourcePack,
		"材质包":           ProjectTypeResourcePack,
		"plugins":       ProjectTypePlugin,
		"":              "",
	}

	for input, want := range cases {
		if got := NormalizeProjectType(input); got != want {
			t.Fatalf("NormalizeProjectType(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// TestSubDirectoryForProjectType 防的回归：
// 内容被装进错误的子目录（光影包丢进 resourcepacks 之类），游戏里看不见。
func TestSubDirectoryForProjectType(t *testing.T) {
	cases := map[string]string{
		"mod":          "mods",
		"shader":       "shaderpacks",
		"shaderpack":   "shaderpacks",
		"resourcepack": "resourcepacks",
		"材质包":          "resourcepacks",
		// 整合包要走安装流程，不能简单丢进某个目录；未知类型同样返回空串
		"modpack": "",
		"unknown": "",
	}

	for input, want := range cases {
		if got := SubDirectoryForProjectType(input); got != want {
			t.Fatalf("SubDirectoryForProjectType(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// TestCurseForgeClassMapping 防的回归：classId 与统一类型互相映射错位，
// 或者未知/为空的 classId 被硬塞成 Mod。
func TestCurseForgeClassMapping(t *testing.T) {
	classID := func(value int) *int { return &value }
	cases := []struct {
		classID *int
		want    string
	}{
		{classID(CurseForgeClassMods), ProjectTypeMod},
		{classID(CurseForgeClassModpacks), ProjectTypeModpack},
		{classID(CurseForgeClassShaders), ProjectTypeShader},
		{classID(CurseForgeClassResourcePacks), ProjectTypeResourcePack},
		{classID(CurseForgeClassBukkitPlugins), ProjectTypePlugin},
		{classID(9999), ""},
		{nil, ""},
	}
	for _, tc := range cases {
		if got := ProjectTypeFromCurseForgeClass(tc.classID); got != tc.want {
			t.Fatalf("ProjectTypeFromCurseForgeClass(%v) = %q，期望 %q", tc.classID, got, tc.want)
		}
	}

	if got := CurseForgeClassFromProjectType("光影包"); got != CurseForgeClassShaders {
		t.Fatalf("CurseForgeClassFromProjectType(光影包) = %d，期望 %d", got, CurseForgeClassShaders)
	}
	if got := CurseForgeClassFromProjectType("mod"); got != CurseForgeClassMods {
		t.Fatalf("CurseForgeClassFromProjectType(mod) = %d，期望 %d", got, CurseForgeClassMods)
	}
	if got := CurseForgeClassFromProjectType("unknown"); got != 0 {
		t.Fatalf("未知类型应返回 0（不过滤），实际 %d", got)
	}
}

// TestProjectTypeDisplayConsistentAcrossSources 防的回归：
// 同一类资源在 Modrinth 与 CurseForge 结果里显示成不同的中文名/图标。
func TestProjectTypeDisplayConsistentAcrossSources(t *testing.T) {
	classID := CurseForgeClassModpacks
	fromModrinth := ModrinthProject{ProjectType: "modpack"}
	fromCurseForge := CurseForgeProject{ClassID: &classID}

	if fromModrinth.TypeDisplay() != fromCurseForge.TypeDisplay() {
		t.Fatalf("整合包显示名不一致：Modrinth=%q CurseForge=%q",
			fromModrinth.TypeDisplay(), fromCurseForge.TypeDisplay())
	}
	if fromModrinth.TypeIcon() != fromCurseForge.TypeIcon() {
		t.Fatalf("整合包图标不一致：Modrinth=%q CurseForge=%q",
			fromModrinth.TypeIcon(), fromCurseForge.TypeIcon())
	}
	if fromModrinth.TypeDisplay() != ProjectTypeDisplay(ProjectTypeModpack) {
		t.Fatalf("Modrinth 展示方法与共享语义不一致：%q", fromModrinth.TypeDisplay())
	}
}

// TestCurseForgeLoaderEnum 防的回归：加载器枚举与名字互转错位
// （数值是官方固定的，写错会让服务端按别的加载器过滤）。
func TestCurseForgeLoaderEnum(t *testing.T) {
	if got := CurseForgeLoaderFromName("NeoForge"); got != CurseForgeLoaderNeoForge {
		t.Fatalf("NeoForge = %d", got)
	}
	if got := CurseForgeLoaderFromName("fabric"); got != CurseForgeLoaderFabric {
		t.Fatalf("fabric = %d", got)
	}
	if got := CurseForgeLoaderName(CurseForgeLoaderQuilt); got != "Quilt" {
		t.Fatalf("Quilt 名字 = %q", got)
	}
	if got := CurseForgeLoaderFromName("不存在的加载器"); got != CurseForgeLoaderAny {
		t.Fatalf("未知加载器应返回 Any(0)，实际 %d", got)
	}
	if got := CurseForgeLoaderDisplayName("neoforge"); got != "NeoForge" {
		t.Fatalf("加载器展示名 = %q，期望统一大小写 NeoForge", got)
	}
	if got := CurseForgeLoaderDisplayName("OptiFine"); got != "OptiFine" {
		t.Fatalf("未收录加载器应原样返回，实际 %q", got)
	}
}
