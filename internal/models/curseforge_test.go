package models

// CurseForge 模型与展示语义用例。
//
// 防的是两类回归：
//  1. json tag 与官方返回不一致（字段全变零值，界面上一片空白但接口"成功"）；
//  2. 展示串（下载量/大小/日期/加载器）另起一套阈值，与 Modrinth 侧不一致。

import (
	"encoding/json"
	"strings"
	"testing"
)

// curseForgeSearchFixture 真实响应的裁剪版：包含可空 classId、可空 links、
// thumbnailUrl 兜底等情况。
const curseForgeSearchFixture = `{"data":[
 {"id":238222,"gameId":432,"name":"Just Enough Items","slug":"jei",
  "links":{"websiteUrl":"https://www.curseforge.com/minecraft/mc-mods/jei"},
  "summary":"查看物品配方","downloadCount":1500000,"classId":6,
  "authors":[{"id":1,"name":"mezz"}],
  "logo":{"thumbnailUrl":"https://media.forgecdn.net/t.png","url":"https://media.forgecdn.net/f.png"},
  "dateModified":"2024-06-13T08:00:00.000Z","allowModDistribution":true},
 {"id":42,"gameId":432,"name":"未分类项目","slug":"unclassified",
  "links":{"websiteUrl":null},"summary":"","downloadCount":500,"classId":null,
  "authors":[],"logo":{"thumbnailUrl":"https://media.forgecdn.net/t2.png","url":""},
  "dateModified":"不是日期","allowModDistribution":false}
],"pagination":{"index":0,"pageSize":20,"resultCount":2,"totalCount":377}}`

// curseForgeFilesFixture 真实文件响应的裁剪版（含加载器与哈希）。
const curseForgeFilesFixture = `{"data":[{
 "id":8965084,"modId":238222,"displayName":"30.38.0.228 for NeoForge 26.2","fileName":"jei.jar",
 "releaseType":2,"fileDate":"2026-09-24T16:00:36.760Z","fileLength":2302732,
 "downloadUrl":"https://edge.forgecdn.net/files/8965/84/jei.jar",
 "gameVersions":["Client","NeoForge","Server","1.21.1","1.21.2","1.21.3","1.21.4"],
 "sortableGameVersions":[
   {"gameVersionName":"Client","gameVersionTypeId":75208},
   {"gameVersionName":"NeoForge","gameVersionTypeId":68441},
   {"gameVersionName":"1.21.1","gameVersionTypeId":77784},
   {"gameVersionName":"Server","gameVersionTypeId":75208}],
 "dependencies":[{"modId":1700987,"relationType":2},{"modId":1689768,"relationType":3}],
 "hashes":[{"value":"44B5B014F16ED568682E4C2AAB8486484EC2128A","algo":1},{"value":"d8dd","algo":2}],
 "isAvailable":true},{"id":1,"modId":238222,"displayName":"","fileName":"legacy.jar",
 "releaseType":1,"fileDate":"","fileLength":512,"downloadUrl":null,"gameVersions":[],
 "sortableGameVersions":[],"dependencies":[],"hashes":[],"isAvailable":true}]}`

// TestCurseForgeSearchUnmarshal 防的回归：json tag 与官方字段名不符
// （例如写成 download_count / class_id），结果全变零值却看不出错。
func TestCurseForgeSearchUnmarshal(t *testing.T) {
	var result CurseForgeSearchResult
	if err := json.Unmarshal([]byte(curseForgeSearchFixture), &result); err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("项目数 = %d，期望 2", len(result.Data))
	}
	if result.Pagination.TotalCount != 377 {
		t.Fatalf("分页总数 = %d，期望 377", result.Pagination.TotalCount)
	}

	project := result.Data[0]
	if project.ID != 238222 || project.Name != "Just Enough Items" || project.DownloadCount != 1500000 {
		t.Fatalf("基本字段没解析出来：%+v", project)
	}
	if project.ProjectType() != ProjectTypeMod {
		t.Fatalf("classId=6 应映射为 mod，实际 %q", project.ProjectType())
	}
	if project.AllowModDistribution != true {
		t.Fatal("allowModDistribution 未解析")
	}
}

// TestCurseForgeProjectDisplayFields 防的回归：
// 下载量阈值/作者/图标兜底/网页地址兜底与 Modrinth 侧不一致。
func TestCurseForgeProjectDisplayFields(t *testing.T) {
	var result CurseForgeSearchResult
	if err := json.Unmarshal([]byte(curseForgeSearchFixture), &result); err != nil {
		t.Fatalf("解析失败：%v", err)
	}

	project := result.Data[0]
	if got := project.DownloadsDisplay(); got != "1.5M 下载" {
		t.Fatalf("DownloadsDisplay = %q，期望 1.5M 下载（与 Modrinth 阈值一致）", got)
	}
	if got := project.AuthorDisplay(); got != "mezz" {
		t.Fatalf("AuthorDisplay = %q", got)
	}
	if got := project.IconURL(); got != "https://media.forgecdn.net/f.png" {
		t.Fatalf("IconURL = %q，应优先原图", got)
	}
	if got := project.PageURL(); got != "https://www.curseforge.com/minecraft/mc-mods/jei" {
		t.Fatalf("PageURL = %q", got)
	}
	if got := project.DateDisplay(); got != "2024-06-13" {
		t.Fatalf("DateDisplay = %q，期望 2024-06-13", got)
	}

	// 第二条：classId 为 null、没有作者、links.websiteUrl 为 null、日期非法
	unclassified := result.Data[1]
	if got := unclassified.ProjectType(); got != "" {
		t.Fatalf("classId 为 null 时项目类型应为空串（未知），实际 %q", got)
	}
	if got := unclassified.TypeDisplay(); got != "" {
		t.Fatalf("未知类型不该伪装成 Mod，实际显示 %q", got)
	}
	if got := unclassified.AuthorDisplay(); got != "" {
		t.Fatalf("没有作者时应返回空串（界面据此隐藏该行），实际 %q", got)
	}
	if got := unclassified.IconURL(); got != "https://media.forgecdn.net/t2.png" {
		t.Fatalf("原图缺失应回退缩略图，实际 %q", got)
	}
	if got := unclassified.PageURL(); got != "https://www.curseforge.com/minecraft/mc-mods/unclassified" {
		t.Fatalf("链接缺失应按 slug 拼地址，实际 %q", got)
	}
	if got := unclassified.DownloadsDisplay(); got != "500 下载" {
		t.Fatalf("DownloadsDisplay = %q，期望 500 下载", got)
	}
	if got := unclassified.DateDisplay(); got != "不是日期" {
		t.Fatalf("无法解析的日期应原样返回，实际 %q", got)
	}
}

// TestCurseForgeFileDisplayFields 防的回归：
// 大小单位阈值、发布类型、游戏版本（混着 Client/Server/加载器）拆错。
func TestCurseForgeFileDisplayFields(t *testing.T) {
	var result CurseForgeFilesResult
	if err := json.Unmarshal([]byte(curseForgeFilesFixture), &result); err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("文件数 = %d，期望 2", len(result.Data))
	}

	file := result.Data[0]
	if got := file.SizeDisplay(); got != "2.2 MB" {
		t.Fatalf("SizeDisplay = %q，期望 2.2 MB", got)
	}
	if got := file.ReleaseTypeDisplay(); got != "测试版" {
		t.Fatalf("ReleaseTypeDisplay = %q，期望 测试版", got)
	}
	if got := file.LoaderDisplay(); got != "NeoForge" {
		t.Fatalf("LoaderDisplay = %q，期望 NeoForge（不能被 Client/Server 干扰）", got)
	}
	if got := file.GameVersionsDisplay(); got != "1.21.1, 1.21.2, 1.21.3" {
		t.Fatalf("GameVersionsDisplay = %q，期望 3 个 MC 版本（超出截断）", got)
	}
	if got := file.MinecraftVersions(); len(got) != 4 {
		t.Fatalf("MinecraftVersions = %v，期望 4 个（剔除 Client/Server/加载器）", got)
	}
	if got := file.SHA1(); got != "44b5b014f16ed568682e4c2aab8486484ec2128a" {
		t.Fatalf("SHA1 = %q，期望小写", got)
	}
	if file.DownloadURL == nil || *file.DownloadURL == "" {
		t.Fatal("downloadUrl 未解析")
	}

	summary := file.Summary()
	for _, want := range []string{"NeoForge", "2026-09-24", "2.2 MB", "测试版"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("Summary = %q，缺少 %q", summary, want)
		}
	}

	// 第二条：名称为空回退文件名、没有 downloadUrl（作者禁止分发）
	legacy := result.Data[1]
	if got := legacy.Label(); got != "legacy.jar" {
		t.Fatalf("Label = %q，空 displayName 应回退文件名", got)
	}
	if legacy.DownloadURL != nil {
		t.Fatal("downloadUrl 为 null 时应保持 nil（界面据此禁用下载）")
	}
	if got := legacy.SizeDisplay(); got != "512 B" {
		t.Fatalf("SizeDisplay = %q，期望 512 B", got)
	}
	if got := legacy.DateDisplay(); got != "" {
		t.Fatalf("空日期应返回空串，实际 %q", got)
	}
}

// TestCurseForgeDependencyDisplay 防的回归：依赖关系类型文案错位。
func TestCurseForgeDependencyDisplay(t *testing.T) {
	cases := []struct {
		relation int
		display  string
		required bool
	}{
		{1, "内嵌", false},
		{2, "可选", false},
		{3, "必需", true},
		{4, "不兼容", false},
		{5, "替代", false},
		{6, "包含", false},
		{99, "", false},
	}
	for _, tc := range cases {
		dependency := CurseForgeDependency{ModID: 1, RelationType: tc.relation}
		if got := dependency.DependencyTypeDisplay(); got != tc.display {
			t.Fatalf("relationType=%d 显示 %q，期望 %q", tc.relation, got, tc.display)
		}
		if got := dependency.IsRequired(); got != tc.required {
			t.Fatalf("relationType=%d IsRequired = %v，期望 %v", tc.relation, got, tc.required)
		}
	}
}

// TestCurseForgeDownloadsDisplayThresholds 防的回归：阈值写成 > 而不是 >=，
// 1000 下载会显示成 "1000 下载" 而不是 "1.0K 下载"。
func TestCurseForgeDownloadsDisplayThresholds(t *testing.T) {
	cases := []struct {
		downloads int64
		want      string
	}{
		{0, "0 下载"},
		{999, "999 下载"},
		{1000, "1.0K 下载"},
		{999_999, "1000.0K 下载"},
		{1_000_000, "1.0M 下载"},
		{2_500_000, "2.5M 下载"},
	}
	for _, tc := range cases {
		project := CurseForgeProject{DownloadCount: tc.downloads}
		if got := project.DownloadsDisplay(); got != tc.want {
			t.Fatalf("DownloadsDisplay(%d) = %q，期望 %q", tc.downloads, got, tc.want)
		}
	}
}
