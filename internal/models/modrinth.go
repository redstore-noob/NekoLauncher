// modrinth.go Modrinth API 相关数据模型（json tag 与 C# JsonPropertyName 完全一致）。
package models

import (
	"fmt"
	"strings"
	"time"
)

// ModrinthSearchResult Modrinth API v2 search 返回结果。
type ModrinthSearchResult struct {
	Hits []ModrinthProject `json:"hits"`
}

// ModrinthProject Modrinth 项目数据（来自 Modrinth API）。
type ModrinthProject struct {
	ProjectID   string    `json:"project_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	IconURL     string    `json:"icon_url"`
	ProjectType string    `json:"project_type"`
	Downloads   int64     `json:"downloads"`
	Follows     int       `json:"follows"`
	Slug        string    `json:"slug"`
	Versions    []string  `json:"versions"`
	DateCreated time.Time `json:"date_created"`
}

// DownloadsDisplay 格式化下载量。
func (p ModrinthProject) DownloadsDisplay() string {
	switch {
	case p.Downloads >= 1_000_000:
		return fmt.Sprintf("%.1fM 下载", float64(p.Downloads)/1_000_000.0)
	case p.Downloads >= 1_000:
		return fmt.Sprintf("%.1fK 下载", float64(p.Downloads)/1_000.0)
	default:
		return fmt.Sprintf("%d 下载", p.Downloads)
	}
}

// FollowsDisplay 格式化关注数。
func (p ModrinthProject) FollowsDisplay() string {
	if p.Follows >= 1_000 {
		return fmt.Sprintf("%.1fK ⭐", float64(p.Follows)/1_000.0)
	}
	return fmt.Sprintf("%d ⭐", p.Follows)
}

// TypeDisplay 项目类型的中文显示名（语义见 projecttype.go，与 CurseForge 共用）。
func (p ModrinthProject) TypeDisplay() string {
	return ProjectTypeDisplay(p.ProjectType)
}

// TypeIcon 项目类型对应图标。
func (p ModrinthProject) TypeIcon() string {
	return ProjectTypeIcon(p.ProjectType)
}

// ModrinthVersion Modrinth API 返回的 Mod 版本条目。
type ModrinthVersion struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"version_number"`
	Changelog     *string  `json:"changelog"`
	GameVersions  []string `json:"game_versions"`
	Loaders       []string `json:"loaders"`
	DatePublished string   `json:"date_published"`
	// VersionType 发布类型：release / beta / alpha（接口字段 version_type）。
	VersionType  string                `json:"version_type"`
	Files        []ModrinthVersionFile `json:"files"`
	Dependencies []ModrinthDependency  `json:"dependencies"`
}

// VersionTypeDisplay 发布类型中文名（与 CurseForge 的 ReleaseTypeDisplay 同口径）。
func (v *ModrinthVersion) VersionTypeDisplay() string {
	switch v.VersionType {
	case "release":
		return "正式版"
	case "beta":
		return "测试版"
	case "alpha":
		return "内测版"
	default:
		return ""
	}
}

// PrimaryFile 主文件：优先 primary 标记，否则取第一个。
func (v *ModrinthVersion) PrimaryFile() *ModrinthVersionFile {
	for i := range v.Files {
		if v.Files[i].Primary {
			return &v.Files[i]
		}
	}
	if len(v.Files) > 0 {
		return &v.Files[0]
	}
	return nil
}

// DisplayName 展示名：Name 为空时回退版本号。
func (v *ModrinthVersion) DisplayName() string {
	if strings.TrimSpace(v.Name) == "" {
		return v.VersionNumber
	}
	return v.Name
}

// DateDisplay 发布日期（yyyy-MM-dd），解析失败时返回原始字符串。
func (v *ModrinthVersion) DateDisplay() string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z0700"} {
		if t, err := time.Parse(layout, v.DatePublished); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return v.DatePublished
}

// GameVersionsDisplay 游戏版本展示（最多 3 个）。
func (v *ModrinthVersion) GameVersionsDisplay() string {
	if len(v.GameVersions) == 0 {
		return ""
	}
	n := len(v.GameVersions)
	if n > 3 {
		n = 3
	}
	return strings.Join(v.GameVersions[:n], ", ")
}

// LoaderDisplay 加载器展示名。
func (v *ModrinthVersion) LoaderDisplay() string {
	parts := make([]string, 0, len(v.Loaders))
	for _, l := range v.Loaders {
		switch l {
		case "fabric":
			parts = append(parts, "Fabric")
		case "forge":
			parts = append(parts, "Forge")
		case "neoforge":
			parts = append(parts, "NeoForge")
		case "quilt":
			parts = append(parts, "Quilt")
		default:
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, ", ")
}

// Summary 摘要：加载器 · 日期 · 文件大小。
func (v *ModrinthVersion) Summary() string {
	var parts []string
	if s := v.LoaderDisplay(); strings.TrimSpace(s) != "" {
		parts = append(parts, s)
	}
	if s := v.DateDisplay(); strings.TrimSpace(s) != "" {
		parts = append(parts, s)
	}
	if f := v.PrimaryFile(); f != nil {
		parts = append(parts, f.SizeDisplay())
	}
	return strings.Join(parts, " · ")
}

// String 实现 fmt.Stringer，与 C# ToString 一致。
func (v *ModrinthVersion) String() string { return v.DisplayName() }

// ModrinthVersionFile Modrinth 版本中的单个文件。
type ModrinthVersionFile struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Primary  bool   `json:"primary"`
	// Hashes 哈希表（sha1 / sha512），下载后校验与将来的更新检测都用得上。
	Hashes map[string]string `json:"hashes"`
}

// SHA1 返回文件的 SHA-1（小写），没有则返回空串。
func (f ModrinthVersionFile) SHA1() string {
	return strings.ToLower(strings.TrimSpace(f.Hashes["sha1"]))
}

// SizeDisplay 格式化文件大小。
func (f ModrinthVersionFile) SizeDisplay() string {
	switch {
	case f.Size >= 1048576:
		return fmt.Sprintf("%.1f MB", float64(f.Size)/1048576.0)
	case f.Size >= 1024:
		return fmt.Sprintf("%.0f KB", float64(f.Size)/1024.0)
	default:
		return fmt.Sprintf("%d B", f.Size)
	}
}

// ModrinthDependency Modrinth 版本的依赖条目。
type ModrinthDependency struct {
	ProjectID      *string `json:"project_id"`
	VersionID      *string `json:"version_id"`
	FileName       *string `json:"file_name"`
	DependencyType string  `json:"dependency_type"`
}

// IsRequired 是否必需依赖。
func (d ModrinthDependency) IsRequired() bool {
	if d.DependencyType == "" {
		return true // C# 默认值 "required"
	}
	return d.DependencyType == "required"
}

// DependencyTypeDisplay 依赖类型中文显示名。
func (d ModrinthDependency) DependencyTypeDisplay() string {
	switch d.DependencyType {
	case "required":
		return "必需"
	case "optional":
		return "可选"
	case "incompatible":
		return "不兼容"
	case "embedded":
		return "内嵌"
	default:
		return d.DependencyType
	}
}
