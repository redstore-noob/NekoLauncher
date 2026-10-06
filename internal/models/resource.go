// resource.go 资源站（Modrinth / CurseForge）的统一数据模型。
//
// 为什么要有这一层：两个站点的原始字段完全不同（Modrinth 的 project_id/downloads
// 与 CurseForge 的 id/downloadCount），但界面上是同一张列表、同一套操作。
// 统一模型在 Go 侧一次性把双方折算成同样的字段与展示串（复用 models 里
// 已被单测锁定的格式化语义），前端只按 `source` 显示来源徽标，不再写分支转换。
package models

// 资源站标识（稳定的前端取值，不要用显示名当 id）。
const (
	ResourceSourceModrinth   = "modrinth"
	ResourceSourceCurseForge = "curseforge"
)

// ResourceSourceInfo 资源站描述。
//
// 域名、镜像地址与 Key 申请地址都在这里由 Go 侧给出：前端文案与图标可以各写各的，
// 但"接口到底打哪个域名、Key 去哪里申请"只允许有一处定义。
type ResourceSourceInfo struct {
	// ID 资源站标识（ResourceSourceModrinth / ResourceSourceCurseForge）。
	ID string `json:"id"`
	// Name 展示名（Modrinth / CurseForge）。
	Name string `json:"name"`
	// SiteURL 站点主页。
	SiteURL string `json:"siteUrl"`
	// APIHost 官方接口主机名（用于错误提示与设置页说明）。
	APIHost string `json:"apiHost"`
	// MirrorHost 国内镜像主机名；空串 = 无镜像。
	MirrorHost string `json:"mirrorHost"`
	// ProjectTypes 该站支持检索的项目类型（统一取值）。
	ProjectTypes []string `json:"projectTypes"`
	// RequiresAPIKey 是否需要 API Key（CurseForge 为 true；Key 由编译期内置）。
	RequiresAPIKey bool `json:"requiresApiKey"`
	// APIKeyConfigured 内置 Key 是否已生效。
	APIKeyConfigured bool `json:"apiKeyConfigured"`
	// Available 现在能不能用（需要 Key 而未生效时为 false，但界面不报错、只提示）。
	Available bool `json:"available"`
	// Hint 不可用时的中文引导（可直接展示）。
	Hint string `json:"hint"`
}

// ResourceSearchRequest 资源搜索请求。
type ResourceSearchRequest struct {
	// Source 资源站标识；空串按 Modrinth 处理。
	Source string `json:"source"`
	// ProjectType 统一项目类型（mod / modpack / shader / resourcepack）。
	ProjectType string `json:"projectType"`
	// Query 关键词；空串 = 浏览该类型的热门内容。
	Query string `json:"query"`
	// GameVersion 按 MC 版本过滤；空串 = 不过滤。
	GameVersion string `json:"gameVersion"`
	// Loader 按加载器过滤（fabric / forge / neoforge / quilt）；空串 = 不过滤。
	Loader string `json:"loader"`
	// Loaders 加载器"任一匹配"过滤（OR 组），用于下载页 Mod 标签这种
	// "只要是 Mod 加载器就行"的场景；非空时优先于 Loader。
	Loaders []string `json:"loaders"`
	// Limit 返回条数上限（<=0 时由后端取默认值）。
	Limit int `json:"limit"`
}

// ResourceHit 统一的搜索结果条目。
type ResourceHit struct {
	Source      string `json:"source"`
	ProjectID   string `json:"projectId"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Author      string `json:"author"`
	IconURL     string `json:"iconUrl"`
	ProjectType string `json:"projectType"`
	PageURL     string `json:"pageUrl"`
	Downloads   int64  `json:"downloads"`
	Follows     int    `json:"follows"`
	// 以下展示串由 Go 侧统一生成（Modrinth 与 CurseForge 共用同一套阈值与单位）。
	DownloadsDisplay string `json:"downloadsDisplay"`
	FollowsDisplay   string `json:"followsDisplay"`
	TypeDisplay      string `json:"typeDisplay"`
	TypeIcon         string `json:"typeIcon"`
	DateDisplay      string `json:"dateDisplay"`
}

// ResourceSearchResult 搜索结果。
type ResourceSearchResult struct {
	Source string        `json:"source"`
	Hits   []ResourceHit `json:"hits"`
	// Total 服务端报告的命中总数（Modrinth 不返回时为 len(Hits)）。
	Total int `json:"total"`
	// Message 可读提示（例如未配置 CurseForge Key 时该怎么办）。
	Message string `json:"message"`
	// NeedsAPIKey 需要在设置里补 CurseForge API Key（界面据此显示引导入口）。
	NeedsAPIKey bool `json:"needsApiKey"`
	// UsedMirror 本次结果来自国内镜像（界面可提示"已自动切换镜像"）。
	UsedMirror bool `json:"usedMirror"`
}

// ResourceVersionRequest 版本列表请求。
type ResourceVersionRequest struct {
	Source    string `json:"source"`
	ProjectID string `json:"projectId"`
	// GameVersion 当前实例的 MC 版本（用于标记"匹配当前实例"）；空串 = 不判定。
	GameVersion string `json:"gameVersion"`
	// Loader 当前实例的加载器；空串 = 不判定。
	Loader string `json:"loader"`
}

// ResourceVersion 统一的版本条目（含下载所需的文件信息与展示串）。
type ResourceVersion struct {
	Source        string   `json:"source"`
	ProjectID     string   `json:"projectId"`
	VersionID     string   `json:"versionId"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"versionNumber"`
	Changelog     string   `json:"changelog"`
	GameVersions  []string `json:"gameVersions"`
	Loaders       []string `json:"loaders"`
	DatePublished string   `json:"datePublished"`
	// ReleaseType release / beta / alpha（两站统一折算）。
	ReleaseType string `json:"releaseType"`
	FileName    string `json:"fileName"`
	FileURL     string `json:"fileUrl"`
	FileSize    int64  `json:"fileSize"`
	SHA1        string `json:"sha1"`
	// DownloadAllowed 作者是否允许第三方分发（false 时界面禁用下载并说明原因）。
	DownloadAllowed bool                 `json:"downloadAllowed"`
	Dependencies    []ResourceDependency `json:"dependencies"`
	// 展示串。
	DisplayName         string `json:"displayName"`
	DateDisplay         string `json:"dateDisplay"`
	GameVersionsDisplay string `json:"gameVersionsDisplay"`
	LoaderDisplay       string `json:"loaderDisplay"`
	Summary             string `json:"summary"`
	FileSizeDisplay     string `json:"fileSizeDisplay"`
	ReleaseTypeDisplay  string `json:"releaseTypeDisplay"`
	// MatchesInstance 是否匹配请求里给出的实例 MC 版本 / 加载器。
	MatchesInstance bool `json:"matchesInstance"`
	// MatchNote 不匹配的原因（中文，可直接展示）；匹配时为空串。
	MatchNote string `json:"matchNote"`
}

// ResourceDependency 统一依赖条目（仅作展示与提示，不做自动递归安装）。
type ResourceDependency struct {
	ProjectID   string `json:"projectId"`
	VersionID   string `json:"versionId"`
	FileName    string `json:"fileName"`
	Kind        string `json:"kind"`
	KindDisplay string `json:"kindDisplay"`
	Required    bool   `json:"required"`
}

// ResourceVersionList 版本列表结果。
type ResourceVersionList struct {
	Source    string            `json:"source"`
	ProjectID string            `json:"projectId"`
	Versions  []ResourceVersion `json:"versions"`
	// MatchedCount 其中匹配当前实例的条数（列表已按"匹配优先"排序）。
	MatchedCount int    `json:"matchedCount"`
	Message      string `json:"message"`
	NeedsAPIKey  bool   `json:"needsApiKey"`
	UsedMirror   bool   `json:"usedMirror"`
}

// ResourceDownloadRequest 资源下载请求。
//
// 前端只传"哪个站的哪个版本装到哪个目录"，文件地址一律由后端按版本 ID 反查：
// WebView 传来的 URL 不可信，也不该由前端决定最终落到哪个文件名。
type ResourceDownloadRequest struct {
	Source    string `json:"source"`
	ProjectID string `json:"projectId"`
	VersionID string `json:"versionId"`
	// ContentDirectory 目标实例的内容目录（mods/resourcepacks 的父目录）。
	ContentDirectory string `json:"contentDirectory"`
	// SubDirectory 内容子目录；空串时由项目类型推导（mod → mods 等）。
	SubDirectory string `json:"subDirectory"`
}

// ResourceDownloadResult 下载结果。
type ResourceDownloadResult struct {
	SavedPath string `json:"savedPath"`
	FileName  string `json:"fileName"`
	FileSize  int64  `json:"fileSize"`
	SourceURL string `json:"sourceUrl"`
	// UsedFallback 主源失败、实际是回退源下载成功的。
	UsedFallback bool `json:"usedFallback"`
}
