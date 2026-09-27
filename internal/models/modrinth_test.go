package models

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestModrinthProjectDownloadsDisplay 防的回归：下载量格式化阈值写成 > 而不是 >=
// （恰好 1000 次下载显示成 "1000 下载"、恰好 100 万显示成 "1000.0K 下载"），
// 以及千分位/小数位写错导致数字量级看错。
func TestModrinthProjectDownloadsDisplay(t *testing.T) {
	cases := []struct {
		downloads int64
		want      string
	}{
		{downloads: 0, want: "0 下载"},
		{downloads: 1, want: "1 下载"},
		{downloads: 999, want: "999 下载"},
		{downloads: 1000, want: "1.0K 下载"},
		{downloads: 1500, want: "1.5K 下载"},
		// 边界：999999/1000 = 999.999，%.1f 进位成 1000.0K（已知取整行为，改阈值要同步改这里）
		{downloads: 999_999, want: "1000.0K 下载"},
		{downloads: 1_000_000, want: "1.0M 下载"},
		{downloads: 1_234_567, want: "1.2M 下载"},
		{downloads: 12_345_678, want: "12.3M 下载"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			project := ModrinthProject{Downloads: tc.downloads}
			if got := project.DownloadsDisplay(); got != tc.want {
				t.Fatalf("DownloadsDisplay(%d) = %q，期望 %q", tc.downloads, got, tc.want)
			}
		})
	}
}

// TestModrinthProjectFollowsDisplay 防的回归：关注数阈值与下载数不一致
// （一个用 >= 一个用 >），以及单位后缀丢失。
func TestModrinthProjectFollowsDisplay(t *testing.T) {
	cases := []struct {
		follows int
		want    string
	}{
		{follows: 0, want: "0 ⭐"},
		{follows: 999, want: "999 ⭐"},
		{follows: 1000, want: "1.0K ⭐"},
		{follows: 1500, want: "1.5K ⭐"},
		{follows: 25_000, want: "25.0K ⭐"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			project := ModrinthProject{Follows: tc.follows}
			if got := project.FollowsDisplay(); got != tc.want {
				t.Fatalf("FollowsDisplay(%d) = %q，期望 %q", tc.follows, got, tc.want)
			}
		})
	}
}

// TestModrinthProjectTypeDisplayAndIcon 防的回归：项目类型中文名/图标错位，
// 搜索列表里把光影包显示成材质包（两者图标很容易复制粘贴弄反）。
func TestModrinthProjectTypeDisplayAndIcon(t *testing.T) {
	cases := []struct {
		name        string
		projectType string
		wantDisplay string
		wantIcon    string
	}{
		{name: "mod", projectType: "mod", wantDisplay: "Mod", wantIcon: "⬜"},
		{name: "modpack", projectType: "modpack", wantDisplay: "整合包", wantIcon: "📦"},
		{name: "shader", projectType: "shader", wantDisplay: "光影包", wantIcon: "☀️"},
		{name: "resourcepack", projectType: "resourcepack", wantDisplay: "材质包", wantIcon: "🎨"},
		// 未知类型原样透出 + 兜底图标，不能让前端出现空字符串
		{name: "未知类型", projectType: "datapack", wantDisplay: "datapack", wantIcon: "📄"},
		{name: "空类型", projectType: "", wantDisplay: "", wantIcon: "📄"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project := ModrinthProject{ProjectType: tc.projectType}
			if got := project.TypeDisplay(); got != tc.wantDisplay {
				t.Fatalf("TypeDisplay(%q) = %q，期望 %q", tc.projectType, got, tc.wantDisplay)
			}
			if got := project.TypeIcon(); got != tc.wantIcon {
				t.Fatalf("TypeIcon(%q) = %q，期望 %q", tc.projectType, got, tc.wantIcon)
			}
		})
	}

	// 四种已知类型的图标必须互不相同
	seen := map[string]string{}
	for _, projectType := range []string{"mod", "modpack", "shader", "resourcepack"} {
		icon := ModrinthProject{ProjectType: projectType}.TypeIcon()
		if other, duplicated := seen[icon]; duplicated {
			t.Fatalf("%s 与 %s 的图标重复：%q", projectType, other, icon)
		}
		seen[icon] = projectType
	}
}

// TestModrinthSearchResultUnmarshal 防的回归：hits 字段名或大小写不符导致搜索永远为空，
// 以及可选字段缺失（icon_url、versions 为 null）时解析整体失败。
func TestModrinthSearchResultUnmarshal(t *testing.T) {
	payload := `{
	  "hits": [
	    {
	      "project_id": "AANobbMI",
	      "title": "Sodium",
	      "description": "现代化渲染优化",
	      "icon_url": "https://cdn.modrinth.com/data/AANobbMI/icon.png",
	      "project_type": "mod",
	      "downloads": 40000000,
	      "follows": 12000,
	      "slug": "sodium",
	      "versions": ["mc1.21.1-0.6.0"],
	      "date_created": "2021-01-01T00:00:00Z",
	      "gallery": ["https://example.com/extra.png"]
	    },
	    {
	      "project_id": "MINIMAL",
	      "title": "无图标项目",
	      "project_type": "modpack",
	      "versions": null
	    }
	  ],
	  "offset": 0,
	  "limit": 10,
	  "total_hits": 2
	}`

	var result ModrinthSearchResult
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		t.Fatalf("解析搜索结果失败：%v", err)
	}
	if len(result.Hits) != 2 {
		t.Fatalf("命中数量 = %d，期望 2", len(result.Hits))
	}

	first := result.Hits[0]
	if first.ProjectID != "AANobbMI" || first.Title != "Sodium" || first.Slug != "sodium" {
		t.Fatalf("基础字段解析错误：%+v", first)
	}
	if first.Downloads != 40_000_000 || first.Follows != 12_000 {
		t.Fatalf("数值字段解析错误：downloads=%d follows=%d", first.Downloads, first.Follows)
	}
	if first.DateCreated.IsZero() {
		t.Fatalf("date_created 未解析：%+v", first.DateCreated)
	}
	if len(first.Versions) != 1 || first.Versions[0] != "mc1.21.1-0.6.0" {
		t.Fatalf("versions 解析错误：%+v", first.Versions)
	}

	// 缺字段的项目：零值但可用，格式化方法不 panic
	second := result.Hits[1]
	if second.IconURL != "" || second.Downloads != 0 || len(second.Versions) != 0 {
		t.Fatalf("缺失字段应为零值：%+v", second)
	}
	if got := second.DownloadsDisplay(); got != "0 下载" {
		t.Fatalf("缺字段项目的下载量显示 = %q", got)
	}
	if got := second.TypeDisplay(); got != "整合包" {
		t.Fatalf("缺字段项目的类型显示 = %q", got)
	}
}

// TestModrinthSearchResultEmptyPayloads 防的回归：搜索接口返回空结果或 null 时，
// 前端拿到 nil 切片直接 .map 报错。
func TestModrinthSearchResultEmptyPayloads(t *testing.T) {
	for _, payload := range []string{`{}`, `{"hits":[]}`, `{"hits":null}`} {
		var result ModrinthSearchResult
		if err := json.Unmarshal([]byte(payload), &result); err != nil {
			t.Fatalf("payload %s 解析失败：%v", payload, err)
		}
		if len(result.Hits) != 0 {
			t.Fatalf("payload %s 应得到 0 条命中，实际 %d", payload, len(result.Hits))
		}
	}
}

// TestModrinthVersionPrimaryFile 防的回归：
// 空 files 数组触发下标越界 panic（Modrinth 上确有 files 为空的版本）；
// 以及有 primary 标记时误取第一个文件，下载到 sources.jar 之类的附属文件。
func TestModrinthVersionPrimaryFile(t *testing.T) {
	// 空文件列表：返回 nil，不 panic
	empty := &ModrinthVersion{}
	if file := empty.PrimaryFile(); file != nil {
		t.Fatalf("files 为空时应返回 nil，实际 %+v", file)
	}

	// 没有 primary 标记：退回第一个
	noPrimary := &ModrinthVersion{Files: []ModrinthVersionFile{
		{Filename: "a.jar", Size: 1},
		{Filename: "b.jar", Size: 2},
	}}
	if file := noPrimary.PrimaryFile(); file == nil || file.Filename != "a.jar" {
		t.Fatalf("无 primary 标记时应取第一个文件，实际 %+v", file)
	}

	// 有 primary 标记：即使排在后面也必须选中它
	withPrimary := &ModrinthVersion{Files: []ModrinthVersionFile{
		{Filename: "sources.jar", Size: 1},
		{Filename: "main.jar", Size: 2, Primary: true},
		{Filename: "javadoc.jar", Size: 3, Primary: true},
	}}
	file := withPrimary.PrimaryFile()
	if file == nil || file.Filename != "main.jar" {
		t.Fatalf("应取第一个带 primary 标记的文件，实际 %+v", file)
	}

	// 返回的必须是切片内的地址（调用方改它等于改原数据），不能是副本
	file.Size = 99
	if withPrimary.Files[1].Size != 99 {
		t.Fatal("PrimaryFile 应返回切片元素地址，不能返回副本")
	}
}

// TestModrinthVersionDisplayNameAndString 防的回归：
// Name 为空白时展示名变成空串（列表里出现一行空白），以及 fmt.Stringer 没生效。
func TestModrinthVersionDisplayNameAndString(t *testing.T) {
	version := &ModrinthVersion{Name: "Sodium 0.6.0", VersionNumber: "mc1.21.1-0.6.0"}
	if got := version.DisplayName(); got != "Sodium 0.6.0" {
		t.Fatalf("DisplayName() = %q", got)
	}
	if got := fmt.Sprintf("%s", version); got != "Sodium 0.6.0" {
		t.Fatalf("String() 未被 fmt 使用：%q", got)
	}

	for _, blank := range []string{"", "   ", "\t\n"} {
		version := &ModrinthVersion{Name: blank, VersionNumber: "mc1.21.1-0.6.0"}
		if got := version.DisplayName(); got != "mc1.21.1-0.6.0" {
			t.Fatalf("Name = %q 时应回退版本号，实际 %q", blank, got)
		}
	}

	// 两者都为空时返回空串而不是 panic
	empty := &ModrinthVersion{}
	if got := empty.DisplayName(); got != "" {
		t.Fatalf("空版本的 DisplayName() = %q", got)
	}
}

// TestModrinthVersionDateDisplay 防的回归：
// Modrinth 的 date_published 有多种写法（带毫秒、只到秒、+0000 无冒号），
// 少一种布局就退回原始串，前端日期列出现 "2024-06-13T08:00:00+00:00" 这种长串。
func TestModrinthVersionDateDisplay(t *testing.T) {
	cases := []struct {
		name      string
		published string
		want      string
	}{
		{name: "只到秒带Z", published: "2024-06-13T08:00:00Z", want: "2024-06-13"},
		{name: "带纳秒", published: "2024-06-13T08:00:00.123456789Z", want: "2024-06-13"},
		{name: "零时区带冒号", published: "2024-06-13T08:00:00+00:00", want: "2024-06-13"},
		{name: "零时区不带冒号", published: "2024-06-13T08:00:00+0800", want: "2024-06-13"},
		{name: "负时区保留当地日期", published: "2024-06-13T23:30:00-05:00", want: "2024-06-13"},
		// 解析失败时原样返回：宁可显示怪串，也不要显示空
		{name: "非法串原样返回", published: "not-a-date", want: "not-a-date"},
		{name: "只有日期", published: "2024-06-13", want: "2024-06-13"},
		{name: "空串", published: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			version := &ModrinthVersion{DatePublished: tc.published}
			if got := version.DateDisplay(); got != tc.want {
				t.Fatalf("DateDisplay(%q) = %q，期望 %q", tc.published, got, tc.want)
			}
		})
	}
}

// TestModrinthVersionGameVersionsDisplay 防的回归：
// 版本列表截断写错（切 3 个写成切 2 个，或对不足 3 个的切片越界）。
func TestModrinthVersionGameVersionsDisplay(t *testing.T) {
	cases := []struct {
		name     string
		versions []string
		want     string
	}{
		{name: "空列表", versions: nil, want: ""},
		{name: "单个", versions: []string{"1.21.1"}, want: "1.21.1"},
		{name: "恰好三个", versions: []string{"1.21.1", "1.21", "1.20.6"}, want: "1.21.1, 1.21, 1.20.6"},
		{name: "超过三个只取前三个", versions: []string{"a", "b", "c", "d", "e"}, want: "a, b, c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			version := &ModrinthVersion{GameVersions: tc.versions}
			if got := version.GameVersionsDisplay(); got != tc.want {
				t.Fatalf("GameVersionsDisplay() = %q，期望 %q", got, tc.want)
			}
		})
	}
}

// TestModrinthVersionLoaderDisplay 防的回归：
// 加载器显示名大小写错（显示成 "fabric" 而不是 "Fabric"），
// 以及未知加载器被吞掉，用户看不出这个版本到底是给谁用的。
func TestModrinthVersionLoaderDisplay(t *testing.T) {
	cases := []struct {
		name    string
		loaders []string
		want    string
	}{
		{name: "空列表", loaders: nil, want: ""},
		{name: "fabric", loaders: []string{"fabric"}, want: "Fabric"},
		{name: "forge", loaders: []string{"forge"}, want: "Forge"},
		{name: "neoforge", loaders: []string{"neoforge"}, want: "NeoForge"},
		{name: "quilt", loaders: []string{"quilt"}, want: "Quilt"},
		{name: "未知加载器原样保留", loaders: []string{"liteloader"}, want: "liteloader"},
		{
			name:    "多加载器按顺序拼接",
			loaders: []string{"fabric", "quilt", "unknown"},
			want:    "Fabric, Quilt, unknown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			version := &ModrinthVersion{Loaders: tc.loaders}
			if got := version.LoaderDisplay(); got != tc.want {
				t.Fatalf("LoaderDisplay() = %q，期望 %q", got, tc.want)
			}
		})
	}
}

// TestModrinthVersionSummary 防的回归：摘要用 " · " 拼接时空段落没被跳过，
// 出现 "Fabric ·  · " 这种尾巴；以及没有文件时 panic。
func TestModrinthVersionSummary(t *testing.T) {
	full := &ModrinthVersion{
		Loaders:       []string{"fabric"},
		DatePublished: "2024-06-13T08:00:00Z",
		Files: []ModrinthVersionFile{
			{Filename: "sources.jar", Size: 10},
			{Filename: "main.jar", Size: 1048576, Primary: true},
		},
	}
	if got := full.Summary(); got != "Fabric · 2024-06-13 · 1.0 MB" {
		t.Fatalf("Summary() = %q，期望 %q", got, "Fabric · 2024-06-13 · 1.0 MB")
	}

	// 只有日期：不该出现多余的分隔符
	onlyDate := &ModrinthVersion{DatePublished: "2024-06-13T08:00:00Z"}
	if got := onlyDate.Summary(); got != "2024-06-13" {
		t.Fatalf("只有日期时 Summary() = %q", got)
	}

	// 全空：空串且不 panic
	if got := (&ModrinthVersion{}).Summary(); got != "" {
		t.Fatalf("空版本 Summary() = %q，期望空串", got)
	}

	// 空加载器串不该占位（Loaders 里混入空串时）
	blankLoader := &ModrinthVersion{Loaders: []string{""}}
	if got := blankLoader.Summary(); got != "" {
		t.Fatalf("空加载器串应被跳过，Summary() = %q", got)
	}
}

// TestModrinthVersionFileSizeDisplay 防的回归：文件大小单位换算阈值错位
// （1024 字节显示成 1024 B / 1.0 MB 显示成 1024 KB），用户看不出下载体积。
func TestModrinthVersionFileSizeDisplay(t *testing.T) {
	cases := []struct {
		size int64
		want string
	}{
		{size: 0, want: "0 B"},
		{size: 1, want: "1 B"},
		{size: 1023, want: "1023 B"},
		{size: 1024, want: "1 KB"},
		{size: 10240, want: "10 KB"},
		// 边界：1048575/1024 = 1023.999，%.0f 进位成 1024 KB（已知取整行为）
		{size: 1_048_575, want: "1024 KB"},
		{size: 1_048_576, want: "1.0 MB"},
		{size: 1_500_000, want: "1.4 MB"},
		{size: 2_097_152, want: "2.0 MB"},
		// 负数（后端异常数据）走兜底分支，不 panic
		{size: -1, want: "-1 B"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			file := ModrinthVersionFile{Size: tc.size}
			if got := file.SizeDisplay(); got != tc.want {
				t.Fatalf("SizeDisplay(%d) = %q，期望 %q", tc.size, got, tc.want)
			}
		})
	}
}

// TestModrinthDependencyIsRequired 防的回归：
// dependency_type 缺省时被当成"非必需"，依赖没有被自动安装，
// 游戏启动即崩（C# 版默认值是 required，必须保持一致）。
func TestModrinthDependencyIsRequired(t *testing.T) {
	cases := []struct {
		name           string
		dependencyType string
		want           bool
	}{
		{name: "缺省按必需（C# 默认值）", dependencyType: "", want: true},
		{name: "required", dependencyType: "required", want: true},
		{name: "optional", dependencyType: "optional", want: false},
		{name: "incompatible", dependencyType: "incompatible", want: false},
		{name: "embedded", dependencyType: "embedded", want: false},
		// 大小写敏感：未知值不能当成必需，否则会去下不该下的东西
		{name: "大小写敏感", dependencyType: "Required", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dependency := ModrinthDependency{DependencyType: tc.dependencyType}
			if got := dependency.IsRequired(); got != tc.want {
				t.Fatalf("IsRequired(%q) = %v，期望 %v", tc.dependencyType, got, tc.want)
			}
		})
	}
}

// TestModrinthDependencyTypeDisplay 防的回归：依赖类型中文名缺失/错位，
// 依赖确认弹窗里出现英文枚举值。
func TestModrinthDependencyTypeDisplay(t *testing.T) {
	cases := []struct {
		name           string
		dependencyType string
		want           string
	}{
		{name: "required", dependencyType: "required", want: "必需"},
		{name: "optional", dependencyType: "optional", want: "可选"},
		{name: "incompatible", dependencyType: "incompatible", want: "不兼容"},
		{name: "embedded", dependencyType: "embedded", want: "内嵌"},
		// 空值原样返回（显示层自行决定怎么提示）；注意 IsRequired 对空值另有约定
		{name: "空值原样返回", dependencyType: "", want: ""},
		{name: "未知类型原样返回", dependencyType: "unknown_type", want: "unknown_type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dependency := ModrinthDependency{DependencyType: tc.dependencyType}
			if got := dependency.DependencyTypeDisplay(); got != tc.want {
				t.Fatalf("DependencyTypeDisplay(%q) = %q，期望 %q", tc.dependencyType, got, tc.want)
			}
		})
	}
}

// TestModrinthVersionUnmarshal 防的回归：changelog / dependencies 里的可空字段
// 用值类型接，遇到 null 直接解析失败，整个版本列表加载不出来。
func TestModrinthVersionUnmarshal(t *testing.T) {
	payload := `{
	  "id": "abc123",
	  "project_id": "AANobbMI",
	  "name": "",
	  "version_number": "mc1.21.1-0.6.0",
	  "changelog": null,
	  "game_versions": ["1.21.1"],
	  "loaders": ["fabric"],
	  "date_published": "2024-06-13T08:00:00Z",
	  "files": [
	    {"url": "https://cdn.modrinth.com/a.jar", "filename": "a.jar", "size": 512000, "primary": true}
	  ],
	  "dependencies": [
	    {"project_id": null, "version_id": "dep-1", "file_name": null, "dependency_type": "required"},
	    {"project_id": "P2", "version_id": null, "file_name": "b.jar", "dependency_type": "optional"}
	  ]
	}`

	var version ModrinthVersion
	if err := json.Unmarshal([]byte(payload), &version); err != nil {
		t.Fatalf("解析版本详情失败：%v", err)
	}
	if version.ID != "abc123" || version.ProjectID != "AANobbMI" {
		t.Fatalf("id/project_id 解析错误：%+v", version)
	}
	if version.Changelog != nil {
		t.Fatalf("changelog 为 null 时应是 nil 指针，实际 %q", *version.Changelog)
	}
	if version.Name != "" {
		t.Fatalf("name 为空串时不该被填充：%q", version.Name)
	}
	// Name 为空 → 展示名回退版本号（真实 API 里很常见）
	if got := version.DisplayName(); got != "mc1.21.1-0.6.0" {
		t.Fatalf("DisplayName() = %q", got)
	}
	if len(version.Files) != 1 || version.Files[0].URL == "" || !version.Files[0].Primary {
		t.Fatalf("files 解析错误：%+v", version.Files)
	}
	if len(version.Dependencies) != 2 {
		t.Fatalf("dependencies 数量 = %d，期望 2", len(version.Dependencies))
	}
	if version.Dependencies[0].ProjectID != nil || version.Dependencies[0].VersionID == nil {
		t.Fatalf("可空字段解析错误：%+v", version.Dependencies[0])
	}
	if !version.Dependencies[0].IsRequired() {
		t.Fatal("dependency_type=required 应判定为必需依赖")
	}
	if version.Dependencies[1].IsRequired() {
		t.Fatal("dependency_type=optional 不应判定为必需依赖")
	}
}
