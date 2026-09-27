// curseforge.go CurseForge API v1 的数据模型与展示语义。
//
// 与 modrinth.go 对称：接口字段原样映射（json tag 与 CurseForge 返回一致），
// 展示串（下载量/大小/日期/加载器/类型）在 Go 侧统一实现并被单测锁定，
// 前端只负责渲染——避免"Go 与 TS 各写一份单位换算"慢慢漂移。
package models

import (
	"fmt"
	"strings"
	"time"
)

// CurseForge Minecraft 游戏 ID（CurseForge 全站游戏编号，Minecraft 固定为 432）。
const CurseForgeMinecraftGameID = 432

// CurseForge 排序字段（sortField 参数）。
const (
	// CurseForgeSortPopularity 按热度（默认，最接近"大家都在用"的排序）
	CurseForgeSortPopularity = 2
	// CurseForgeSortDownloads 按总下载量
	CurseForgeSortDownloads = 6
	// CurseForgeSortLastUpdated 按最后更新时间
	CurseForgeSortLastUpdated = 3
)

// CurseForge ModLoaderType 取值（官方枚举，数值不能自造）。
const (
	CurseForgeLoaderAny        = 0
	CurseForgeLoaderForge      = 1
	CurseForgeLoaderCauldron   = 2
	CurseForgeLoaderLiteLoader = 3
	CurseForgeLoaderFabric     = 4
	CurseForgeLoaderQuilt      = 5
	CurseForgeLoaderNeoForge   = 6
)

// CurseForge 文件发布类型（releaseType）。
const (
	CurseForgeReleaseTypeRelease = 1
	CurseForgeReleaseTypeBeta    = 2
	CurseForgeReleaseTypeAlpha   = 3
)

// CurseForgeModLoaderVersionTypeID sortableGameVersions 里"加载器"条目的类型 ID：
// 该数组把 Minecraft 版本、加载器、Client/Server 混在一起，只有靠类型 ID
// 才能把加载器挑出来（否则界面会把 "Fabric" 当成一个游戏版本显示）。
const CurseForgeModLoaderVersionTypeID = 68441

// CurseForgeSearchResult /mods/search 的响应。
type CurseForgeSearchResult struct {
	Data       []CurseForgeProject  `json:"data"`
	Pagination CurseForgePagination `json:"pagination"`
}

// CurseForgePagination 分页信息（totalCount 用于界面显示"共 N 个结果"）。
type CurseForgePagination struct {
	Index       int `json:"index"`
	PageSize    int `json:"pageSize"`
	ResultCount int `json:"resultCount"`
	TotalCount  int `json:"totalCount"`
}

// CurseForgeProject /mods/search 与 /mods/{id} 的项目条目。
type CurseForgeProject struct {
	ID            int                `json:"id"`
	GameID        int                `json:"gameId"`
	Name          string             `json:"name"`
	Slug          string             `json:"slug"`
	Links         CurseForgeLinks    `json:"links"`
	Summary       string             `json:"summary"`
	DownloadCount int64              `json:"downloadCount"`
	ClassID       *int               `json:"classId"`
	Authors       []CurseForgeAuthor `json:"authors"`
	Logo          CurseForgeLogo     `json:"logo"`
	LatestFiles   []CurseForgeFile   `json:"latestFiles"`
	DateModified  string             `json:"dateModified"`
	DateReleased  string             `json:"dateReleased"`
	// AllowModDistribution 作者是否允许第三方分发；为 false 时 CurseForge 不返回
	// downloadUrl（只能引导用户去网页下载），界面必须据此禁用下载按钮而不是报错。
	AllowModDistribution bool `json:"allowModDistribution"`
}

// CurseForgeLinks 项目相关链接。
type CurseForgeLinks struct {
	WebsiteURL string `json:"websiteUrl"`
	SourceURL  string `json:"sourceUrl"`
	IssuesURL  string `json:"issuesUrl"`
	WikiURL    string `json:"wikiUrl"`
}

// CurseForgeAuthor 作者（CurseForge 没有单独的"作者"字段，取首个作者展示）。
type CurseForgeAuthor struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// CurseForgeLogo 项目图标。
type CurseForgeLogo struct {
	Title        string `json:"title"`
	ThumbnailURL string `json:"thumbnailUrl"`
	URL          string `json:"url"`
}

// CurseForgeFilesResult /mods/{id}/files 的响应。
type CurseForgeFilesResult struct {
	Data       []CurseForgeFile     `json:"data"`
	Pagination CurseForgePagination `json:"pagination"`
}

// CurseForgeFile /mods/{id}/files 里的单个文件。
type CurseForgeFile struct {
	ID                   int                             `json:"id"`
	ModID                int                             `json:"modId"`
	DisplayName          string                          `json:"displayName"`
	FileName             string                          `json:"fileName"`
	ReleaseType          int                             `json:"releaseType"`
	FileDate             string                          `json:"fileDate"`
	FileLength           int64                           `json:"fileLength"`
	DownloadCount        int64                           `json:"downloadCount"`
	DownloadURL          *string                         `json:"downloadUrl"`
	GameVersions         []string                        `json:"gameVersions"`
	SortableGameVersions []CurseForgeSortableGameVersion `json:"sortableGameVersions"`
	Dependencies         []CurseForgeDependency          `json:"dependencies"`
	Hashes               []CurseForgeHash                `json:"hashes"`
	IsAvailable          bool                            `json:"isAvailable"`
}

// CurseForgeSortableGameVersion 结构化的版本信息（含加载器与 MC 版本）。
type CurseForgeSortableGameVersion struct {
	GameVersionName string `json:"gameVersionName"`
	GameVersion     string `json:"gameVersion"`
	GameVersionType int    `json:"gameVersionTypeId"`
}

// CurseForgeDependency 文件依赖。
type CurseForgeDependency struct {
	ModID        int `json:"modId"`
	RelationType int `json:"relationType"`
}

// CurseForgeHash 文件哈希（algo: 1=SHA-1，2=MD5）。
type CurseForgeHash struct {
	Value string `json:"value"`
	Algo  int    `json:"algo"`
}

// SHA1 返回 SHA-1 哈希（algo=1），没有则返回空串。
func (f CurseForgeFile) SHA1() string {
	for _, hash := range f.Hashes {
		if hash.Algo == 1 {
			return strings.ToLower(strings.TrimSpace(hash.Value))
		}
	}
	return ""
}

// ---- 展示语义 ----

// DownloadsDisplay 格式化下载量（阈值与 Modrinth 侧完全一致）。
func (p CurseForgeProject) DownloadsDisplay() string {
	switch {
	case p.DownloadCount >= 1_000_000:
		return fmt.Sprintf("%.1fM 下载", float64(p.DownloadCount)/1_000_000.0)
	case p.DownloadCount >= 1_000:
		return fmt.Sprintf("%.1fK 下载", float64(p.DownloadCount)/1_000.0)
	default:
		return fmt.Sprintf("%d 下载", p.DownloadCount)
	}
}

// TypeDisplay 项目类型的中文显示名。
func (p CurseForgeProject) TypeDisplay() string {
	return ProjectTypeDisplay(p.ProjectType())
}

// TypeIcon 项目类型对应图标。
func (p CurseForgeProject) TypeIcon() string {
	return ProjectTypeIcon(p.ProjectType())
}

// ProjectType 统一项目类型（未知分类返回空串）。
func (p CurseForgeProject) ProjectType() string {
	return ProjectTypeFromCurseForgeClass(p.ClassID)
}

// AuthorDisplay 作者展示名：取首个作者，没有作者时返回空串（界面据此隐藏该行）。
func (p CurseForgeProject) AuthorDisplay() string {
	for _, author := range p.Authors {
		if name := strings.TrimSpace(author.Name); name != "" {
			return name
		}
	}
	return ""
}

// IconURL 图标地址：优先正方形原图，其次缩略图。
func (p CurseForgeProject) IconURL() string {
	if url := strings.TrimSpace(p.Logo.URL); url != "" {
		return url
	}
	return strings.TrimSpace(p.Logo.ThumbnailURL)
}

// PageURL 项目网页地址；链接缺失时用 slug 拼官方地址。
func (p CurseForgeProject) PageURL() string {
	if url := strings.TrimSpace(p.Links.WebsiteURL); url != "" {
		return url
	}
	if strings.TrimSpace(p.Slug) != "" {
		return "https://www.curseforge.com/minecraft/mc-mods/" + p.Slug
	}
	return ""
}

// DateDisplay 最后更新日期（yyyy-MM-dd）；解析失败时返回原始字符串。
func (p CurseForgeProject) DateDisplay() string {
	return formatCurseForgeDate(p.DateModified)
}

// Label 文件展示名：DisplayName 为空时回退文件名。
// （不能叫 DisplayName：该名字已被同名的接口字段占用。）
func (f CurseForgeFile) Label() string {
	if name := strings.TrimSpace(f.DisplayName); name != "" {
		return name
	}
	return f.FileName
}

// DateDisplay 文件发布日期（yyyy-MM-dd）；解析失败时返回原始字符串。
func (f CurseForgeFile) DateDisplay() string {
	return formatCurseForgeDate(f.FileDate)
}

// SizeDisplay 文件大小（阈值与 Modrinth 侧一致）。
func (f CurseForgeFile) SizeDisplay() string {
	switch {
	case f.FileLength >= 1048576:
		return fmt.Sprintf("%.1f MB", float64(f.FileLength)/1048576.0)
	case f.FileLength >= 1024:
		return fmt.Sprintf("%.0f KB", float64(f.FileLength)/1024.0)
	default:
		return fmt.Sprintf("%d B", f.FileLength)
	}
}

// ReleaseTypeDisplay 发布类型中文名。
func (f CurseForgeFile) ReleaseTypeDisplay() string {
	switch f.ReleaseType {
	case CurseForgeReleaseTypeRelease:
		return "正式版"
	case CurseForgeReleaseTypeBeta:
		return "测试版"
	case CurseForgeReleaseTypeAlpha:
		return "内测版"
	default:
		return ""
	}
}

// LoaderDisplay 加载器展示名（从 sortableGameVersions 里按类型 ID 挑出）。
func (f CurseForgeFile) LoaderDisplay() string {
	parts := make([]string, 0, 2)
	for _, version := range f.SortableGameVersions {
		if version.GameVersionType != CurseForgeModLoaderVersionTypeID {
			continue
		}
		if name := strings.TrimSpace(version.GameVersionName); name != "" {
			parts = append(parts, CurseForgeLoaderDisplayName(name))
		}
	}
	return strings.Join(parts, ", ")
}

// GameVersionsDisplay MC 版本展示（最多 3 个，剔除 Client/Server 与加载器名）。
func (f CurseForgeFile) GameVersionsDisplay() string {
	minecraft := f.MinecraftVersions()
	if len(minecraft) == 0 {
		return ""
	}
	n := len(minecraft)
	if n > 3 {
		n = 3
	}
	return strings.Join(minecraft[:n], ", ")
}

// MinecraftVersions 从 gameVersions 里筛出真正的 MC 版本号
// （该数组同时混着 "Client" / "Server" / "Fabric" 这类标记）。
func (f CurseForgeFile) MinecraftVersions() []string {
	result := make([]string, 0, len(f.GameVersions))
	for _, raw := range f.GameVersions {
		version := strings.TrimSpace(raw)
		if version == "" || isCurseForgeNonMinecraftVersion(version) {
			continue
		}
		result = append(result, version)
	}
	return result
}

// Summary 摘要：加载器 · 日期 · 文件大小（与 Modrinth 版本摘要同格式）。
func (f CurseForgeFile) Summary() string {
	var parts []string
	if text := f.LoaderDisplay(); text != "" {
		parts = append(parts, text)
	}
	if text := f.DateDisplay(); text != "" && text != f.FileDate {
		parts = append(parts, text)
	}
	if f.FileLength > 0 {
		parts = append(parts, f.SizeDisplay())
	}
	if text := f.ReleaseTypeDisplay(); text != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, " · ")
}

// DependencyTypeDisplay 依赖关系中文名（CurseForge relationType：1=内嵌 2=可选 3=必需 …）。
func (d CurseForgeDependency) DependencyTypeDisplay() string {
	switch d.RelationType {
	case 1:
		return "内嵌"
	case 2:
		return "可选"
	case 3:
		return "必需"
	case 4:
		return "不兼容"
	case 5:
		return "替代"
	case 6:
		return "包含"
	default:
		return ""
	}
}

// IsRequired 是否必需依赖。
func (d CurseForgeDependency) IsRequired() bool { return d.RelationType == 3 }

// CurseForgeLoaderName CurseForge 加载器枚举 → 展示名。
func CurseForgeLoaderName(loaderType int) string {
	switch loaderType {
	case CurseForgeLoaderForge:
		return "Forge"
	case CurseForgeLoaderNeoForge:
		return "NeoForge"
	case CurseForgeLoaderFabric:
		return "Fabric"
	case CurseForgeLoaderQuilt:
		return "Quilt"
	case CurseForgeLoaderLiteLoader:
		return "LiteLoader"
	case CurseForgeLoaderCauldron:
		return "Cauldron"
	default:
		return ""
	}
}

// CurseForgeLoaderFromName 加载器名 → CurseForge 枚举；未知返回 Any。
func CurseForgeLoaderFromName(name string) int {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "forge":
		return CurseForgeLoaderForge
	case "neoforge":
		return CurseForgeLoaderNeoForge
	case "fabric":
		return CurseForgeLoaderFabric
	case "quilt":
		return CurseForgeLoaderQuilt
	case "liteloader":
		return CurseForgeLoaderLiteLoader
	case "cauldron":
		return CurseForgeLoaderCauldron
	default:
		return CurseForgeLoaderAny
	}
}

// CurseForgeLoaderDisplayName 加载器原始名 → 统一展示名（大小写不规范时纠正）。
func CurseForgeLoaderDisplayName(raw string) string {
	if name := CurseForgeLoaderName(CurseForgeLoaderFromName(raw)); name != "" {
		return name
	}
	return raw
}

// isCurseForgeNonMinecraftVersion gameVersions 里混入的非 MC 版本标记。
func isCurseForgeNonMinecraftVersion(value string) bool {
	lower := strings.ToLower(value)
	if lower == "client" || lower == "server" {
		return true
	}
	return CurseForgeLoaderFromName(lower) != CurseForgeLoaderAny ||
		lower == "rift" || lower == "java edition"
}

// formatCurseForgeDate 把 CurseForge 的 RFC3339（含毫秒）转成 yyyy-MM-dd。
func formatCurseForgeDate(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z0700"} {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return trimmed
}
