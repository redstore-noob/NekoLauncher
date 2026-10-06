package download

// resource_search.go 资源搜索服务：把 Modrinth / CurseForge 两个资源站统一成
// 「搜索 → 看版本 → 装进当前实例内容目录」一条链路，供 DownloadAPI 绑定调用。
//
// 为什么放在 download 包：下载必须复用本包已有的通道（断点续传 / 限速 / 暂停门 /
// 原子落地 / SourceProvider 源回退），另起一套下载实现只会制造第二份语义。
//
// 前端只传"哪个站、哪个项目、哪个版本、装到哪个目录"；文件地址与文件名一律由
// 后端按版本 ID 向资源站反查——WebView 传来的 URL 不可信，也不该由前端决定落点。

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nekolauncher/internal/download/curseforge"
	"nekolauncher/internal/download/modrinth"
	"nekolauncher/internal/logs"
	"nekolauncher/internal/models"
)

// 资源搜索的条数口径：默认 20 条，与界面上"一屏能看完"的量级一致；
// 上限 100 条（Modrinth 单页上限）：下载页的标签页一次拉 100 条后本地分页，
// 上限压到 40 会让它的分页凭空少掉一半内容。CurseForge 侧另有 50 条的硬限制，
// 由 curseforge 客户端自行收敛。
const (
	resourceSearchDefaultLimit = 20
	resourceSearchMaxLimit     = 100
	// resourceVersionListLimit CurseForge 文件列表单页上限（官方硬限制 50）。
	resourceVersionListLimit = 50
)

// resolveResourceURL 解析资源文件地址：返回主用地址与回退地址（回退可能为空串）。
//
// 默认实现走 DownloadSourceProvider：用户选择的下载源（Official / BMCL）
// 决定主用地址，设置里的"自动回退源"决定回退地址——资源的 CDN 目前没有国内镜像，
// 因此这一步对 cdn.modrinth.com / edge.forgecdn.net 通常是恒等映射，但接口
// 必须保留：一旦某个源补上内容镜像，这里不需要改任何调用点。
//
// 抽成变量是为了让测试能注入 httptest 地址：真实 CDN 在单测里不可达，而
// "主源失败后确实尝试了回退源"这件事必须有测试盯着。
var resolveResourceURL = func(rawURL string) (primary string, fallback string) {
	primary = SourceProvider.Resolve(rawURL)
	if candidate := SourceProvider.ResolveFallback(rawURL); candidate != nil {
		fallback = *candidate
	}
	return primary, fallback
}

// ListResourceSources 资源站清单（前端数据源切换 + "内置 Key 未生效"提示的唯一依据）。
// 域名、镜像地址都从各客户端的常量取，前端不再抄一份。
func ListResourceSources(apiKey string) []models.ResourceSourceInfo {
	configured := strings.TrimSpace(apiKey) != ""

	return []models.ResourceSourceInfo{
		{
			ID:               models.ResourceSourceModrinth,
			Name:             "Modrinth",
			SiteURL:          modrinth.SiteURL,
			APIHost:          modrinth.APIHost(),
			MirrorHost:       modrinth.MirrorHost(),
			ProjectTypes:     []string{models.ProjectTypeMod, models.ProjectTypeModpack, models.ProjectTypeShader, models.ProjectTypeResourcePack},
			RequiresAPIKey:   false,
			APIKeyConfigured: true,
			Available:        true,
			Hint:             "无需 API Key；官方接口不通时会自动改用国内镜像。",
		},
		{
			ID:               models.ResourceSourceCurseForge,
			Name:             "CurseForge",
			SiteURL:          curseforge.SiteURL,
			APIHost:          curseforge.APIHost(),
			MirrorHost:       curseforge.MirrorHost(),
			ProjectTypes:     []string{models.ProjectTypeMod, models.ProjectTypeModpack, models.ProjectTypeShader, models.ProjectTypeResourcePack},
			RequiresAPIKey:   true,
			APIKeyConfigured: configured,
			Available:        configured,
			Hint:             curseforge.APIKeyHint,
		},
	}
}

// SearchResources 在指定资源站搜索资源。
//
// CurseForge 未配置 API Key 时**不返回错误**：返回 NeedsAPIKey + 中文引导，
// 让界面能正常渲染引导卡片——把"没填 Key"做成异常会让下载页看起来像坏了。
func SearchResources(
	ctx context.Context,
	request models.ResourceSearchRequest,
	apiKey string,
) (models.ResourceSearchResult, error) {
	source := normalizeResourceSource(request.Source)
	projectType := models.NormalizeProjectType(request.ProjectType)
	limit := clampResourceLimit(request.Limit)
	result := models.ResourceSearchResult{Source: source, Hits: []models.ResourceHit{}}

	switch source {
	case models.ResourceSourceCurseForge:
		if strings.TrimSpace(apiKey) == "" {
			result.Message = curseforge.APIKeyHint
			result.NeedsAPIKey = true
			return result, nil
		}

		options := curseforge.SearchOptions{
			Query:       strings.TrimSpace(request.Query),
			ClassID:     models.CurseForgeClassFromProjectType(projectType),
			GameVersion: strings.TrimSpace(request.GameVersion),
			Limit:       limit,
		}
		// 加载器过滤只对 Mod / 整合包有意义：给材质包/光影包带上 modLoaderType
		// 会被服务端当成"没有匹配"，直接搜出空列表。
		if projectType == models.ProjectTypeMod || projectType == models.ProjectTypeModpack {
			options.LoaderType = models.CurseForgeLoaderFromName(request.Loader)
		}

		response, err := curseforge.Search(ctx, apiKey, options)
		if err != nil {
			return result, err
		}
		hits := make([]models.ResourceHit, 0, len(response.Data))
		for _, project := range response.Data {
			hits = append(hits, resourceHitFromCurseForge(project))
		}
		result.Hits = hits
		result.Total = response.Pagination.TotalCount
		result.UsedMirror = curseforge.UsedMirror()
		return result, nil

	default:
		// Modrinth 的加载器 facet 用小写名（fabric / forge / neoforge / quilt），
		// 空串 = 不过滤。
		var (
			projects []models.ModrinthProject
			err      error
		)
		if loaders := normalizeLoaderNames(request.Loaders); len(loaders) > 0 {
			// 加载器 OR 组（"只要是 Mod 加载器就行"）：与单加载器的 AND 过滤是
			// 两种不同的 facets 形状，不能混用。
			projects, err = modrinth.SearchWithLoaderGroup(ctx, projectType, strings.TrimSpace(request.Query),
				strings.TrimSpace(request.GameVersion), loaders, limit)
		} else {
			projects, err = modrinth.SearchWithLoader(ctx, projectType, strings.TrimSpace(request.Query),
				strings.TrimSpace(request.GameVersion), normalizeLoaderName(request.Loader), limit)
		}
		if err != nil {
			return result, err
		}
		hits := make([]models.ResourceHit, 0, len(projects))
		for _, project := range projects {
			hits = append(hits, resourceHitFromModrinth(project))
		}
		result.Hits = hits
		result.Total = len(hits)
		result.UsedMirror = modrinth.UsedMirror()
		return result, nil
	}
}

// ListResourceVersions 列出项目的版本，并标出哪些匹配当前实例的 MC 版本 / 加载器。
//
// 故意不做服务端过滤：用户经常需要"换一个旧版本试试"，把不匹配的版本藏起来
// 反而没法选；这里只在本地标记（MatchesInstance / MatchNote）并把匹配项排到前面。
func ListResourceVersions(
	ctx context.Context,
	request models.ResourceVersionRequest,
	apiKey string,
) (models.ResourceVersionList, error) {
	source := normalizeResourceSource(request.Source)
	projectID := strings.TrimSpace(request.ProjectID)
	result := models.ResourceVersionList{
		Source:    source,
		ProjectID: projectID,
		Versions:  []models.ResourceVersion{},
	}
	if projectID == "" {
		return result, fmt.Errorf("项目 ID 不能为空")
	}

	var (
		versions []models.ResourceVersion
		err      error
	)

	switch source {
	case models.ResourceSourceCurseForge:
		if strings.TrimSpace(apiKey) == "" {
			result.Message = curseforge.APIKeyHint
			result.NeedsAPIKey = true
			return result, nil
		}
		versions, err = curseForgeResourceVersions(ctx, apiKey, projectID)
		result.UsedMirror = curseforge.UsedMirror()
	default:
		versions, err = modrinthResourceVersions(ctx, projectID)
		result.UsedMirror = modrinth.UsedMirror()
	}
	if err != nil {
		return result, err
	}

	versions = MatchResourceVersions(versions, request.GameVersion, request.Loader)
	for _, version := range versions {
		if version.MatchesInstance {
			result.MatchedCount++
		}
	}
	result.Versions = versions
	return result, nil
}

// MatchResourceVersions 标记版本与实例的匹配情况，并把匹配项稳定排到前面
// （同组内保持资源站返回的新→旧顺序）。
//
// 判定口径：
//   - gameVersion 为空 = 不判定 MC 版本；
//   - loader 为空 / "minecraft" / "any" = 不判定加载器（材质包、光影包的
//     loaders 通常就是 ["minecraft"]，对它们做加载器判定会全员不匹配）；
//   - 版本侧 loaders 为空或含 "minecraft" 时视为与任意加载器兼容。
func MatchResourceVersions(versions []models.ResourceVersion, gameVersion, loader string) []models.ResourceVersion {
	wantedGame := strings.TrimSpace(gameVersion)
	wantedLoader := normalizeLoaderName(loader)

	for index := range versions {
		version := &versions[index]
		reasons := make([]string, 0, 2)

		if wantedGame != "" && !containsIgnoreCase(version.GameVersions, wantedGame) {
			reasons = append(reasons, fmt.Sprintf("不支持 MC %s（该版本支持 %s）",
				wantedGame, versionsSummary(version.GameVersions)))
		}
		if wantedLoader != "" && !loaderCompatible(version.Loaders, wantedLoader) {
			reasons = append(reasons, fmt.Sprintf("不支持 %s（该版本支持 %s）",
				models.CurseForgeLoaderDisplayName(wantedLoader), versionsSummary(version.Loaders)))
		}

		version.MatchesInstance = len(reasons) == 0
		version.MatchNote = strings.Join(reasons, "；")
	}

	sort.SliceStable(versions, func(i, j int) bool {
		return versions[i].MatchesInstance && !versions[j].MatchesInstance
	})
	return versions
}

// DownloadResourceVersion 下载指定资源版本的主文件到实例内容目录，返回保存路径。
//
// 走的是本包已有的下载通道（断点续传 / 限速 / 暂停门 / 原子落地），并且
// 主用地址与回退地址都来自 SourceProvider——主源失败时自动换回退源重下一次，
// 两次都失败才向用户报错（错误里写清两个地址都试过）。
func DownloadResourceVersion(
	ctx context.Context,
	request models.ResourceDownloadRequest,
	apiKey string,
	progress ProgressBytes,
) (models.ResourceDownloadResult, error) {
	result := models.ResourceDownloadResult{}
	source := normalizeResourceSource(request.Source)
	projectID := strings.TrimSpace(request.ProjectID)
	versionID := strings.TrimSpace(request.VersionID)

	if versionID == "" {
		return result, fmt.Errorf("版本 ID 不能为空")
	}
	if strings.TrimSpace(request.ContentDirectory) == "" {
		return result, fmt.Errorf("目标内容目录不能为空")
	}

	var (
		projectType string
		fileName    string
		fileURL     string
		fileSize    int64
	)

	switch source {
	case models.ResourceSourceCurseForge:
		if strings.TrimSpace(apiKey) == "" {
			return result, fmt.Errorf("%s", curseforge.APIKeyHint)
		}
		file, err := curseforge.GetFile(ctx, apiKey, projectID, versionID)
		if err != nil {
			return result, err
		}
		fileURL = derefString(file.DownloadURL)
		if fileURL == "" {
			// 作者禁止第三方分发时 /files 不给地址，官方提供单独端点；
			// 该端点同样可能 403，此时只能引导用户去网页手动下载。
			resolved, resolveErr := curseforge.ResolveDownloadURL(ctx, apiKey, projectID, versionID)
			if resolveErr != nil || resolved == "" {
				return result, fmt.Errorf(
					"该文件不允许第三方下载（作者关闭了 API 分发），请到 CurseForge 网页手动下载后导入")
			}
			fileURL = resolved
		}
		fileName = file.FileName
		fileSize = file.FileLength
		// 文件接口不返回 classId，项目类型只能靠调用方给的子目录体现；
		// 这里保持空串，让子目录回退到 mods（最常见的一类）。

	default:
		version, err := modrinth.GetVersion(ctx, versionID)
		if err != nil {
			return result, err
		}
		if projectID != "" && version.ProjectID != "" && !strings.EqualFold(version.ProjectID, projectID) {
			return result, fmt.Errorf("版本 %s 不属于项目 %s，已拒绝下载", versionID, projectID)
		}
		file := version.PrimaryFile()
		if file == nil || strings.TrimSpace(file.URL) == "" {
			return result, fmt.Errorf("该版本没有可下载的主文件")
		}
		fileURL = file.URL
		fileName = file.Filename
		fileSize = file.Size
	}

	subDirectory := strings.TrimSpace(request.SubDirectory)
	if subDirectory == "" {
		subDirectory = models.SubDirectoryForProjectType(projectType)
	}
	if subDirectory == "" {
		// 类型未知（例如 CurseForge 的文件接口不返回分类）时按 Mod 处理：
		// 这是最常见的一类，且 mods 目录一定存在。
		subDirectory = "mods"
	}

	primary, fallback := resolveResourceURL(fileURL)
	if strings.TrimSpace(primary) == "" {
		return result, fmt.Errorf("资源文件地址为空，无法下载")
	}

	savedPath, err := DownloadFileToInstance(ctx, primary, fileName, request.ContentDirectory, subDirectory, progress)
	if err == nil {
		return models.ResourceDownloadResult{
			SavedPath: savedPath,
			FileName:  filepath.Base(savedPath),
			FileSize:  fileSize,
			SourceURL: primary,
		}, nil
	}

	// 主源失败：仅在"确实配置了另一个地址"且用户没有取消时回退，
	// 否则回退只是把同样的失败重放一遍。
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if strings.TrimSpace(fallback) == "" || strings.EqualFold(fallback, primary) {
		return result, fmt.Errorf("下载失败（%s）：%w", hostOfURL(primary), err)
	}

	logs.Write("INFO", fmt.Sprintf("资源下载主源失败，改用回退源重试：%s -> %s", hostOfURL(primary), hostOfURL(fallback)))
	retryPath, retryErr := DownloadFileToInstance(ctx, fallback, fileName, request.ContentDirectory, subDirectory, progress)
	if retryErr != nil {
		return result, fmt.Errorf("下载失败：主源（%s）与回退源（%s）均未成功；主源错误：%w；回退源错误：%v",
			hostOfURL(primary), hostOfURL(fallback), err, retryErr)
	}
	return models.ResourceDownloadResult{
		SavedPath:    retryPath,
		FileName:     filepath.Base(retryPath),
		FileSize:     fileSize,
		SourceURL:    fallback,
		UsedFallback: true,
	}, nil
}

// ---- 各资源站的版本列表 ----

func modrinthResourceVersions(ctx context.Context, projectID string) ([]models.ResourceVersion, error) {
	versions, err := modrinth.GetVersions(ctx, projectID, nil, nil)
	if err != nil {
		return nil, err
	}
	result := make([]models.ResourceVersion, 0, len(versions))
	for index := range versions {
		result = append(result, resourceVersionFromModrinth(&versions[index]))
	}
	return result, nil
}

func curseForgeResourceVersions(ctx context.Context, apiKey, projectID string) ([]models.ResourceVersion, error) {
	files, err := curseforge.GetFiles(ctx, apiKey, projectID, "", 0, resourceVersionListLimit)
	if err != nil {
		return nil, err
	}
	result := make([]models.ResourceVersion, 0, len(files))
	for index := range files {
		result = append(result, resourceVersionFromCurseForge(&files[index], projectID))
	}
	return result, nil
}

// ---- 原始模型 → 统一模型 ----

func resourceHitFromModrinth(project models.ModrinthProject) models.ResourceHit {
	page := strings.TrimSpace(project.Slug)
	if page == "" {
		page = project.ProjectID
	}
	return models.ResourceHit{
		Source:           models.ResourceSourceModrinth,
		ProjectID:        project.ProjectID,
		Slug:             project.Slug,
		Title:            project.Title,
		Description:      project.Description,
		IconURL:          project.IconURL,
		ProjectType:      models.NormalizeProjectType(project.ProjectType),
		PageURL:          modrinth.SiteURL + "/project/" + page,
		Downloads:        project.Downloads,
		Follows:          project.Follows,
		DownloadsDisplay: project.DownloadsDisplay(),
		FollowsDisplay:   project.FollowsDisplay(),
		TypeDisplay:      project.TypeDisplay(),
		TypeIcon:         project.TypeIcon(),
		DateDisplay:      dateDisplayOrEmpty(project.DateCreated),
	}
}

func resourceHitFromCurseForge(project models.CurseForgeProject) models.ResourceHit {
	return models.ResourceHit{
		Source:           models.ResourceSourceCurseForge,
		ProjectID:        fmt.Sprintf("%d", project.ID),
		Slug:             project.Slug,
		Title:            project.Name,
		Description:      project.Summary,
		Author:           project.AuthorDisplay(),
		IconURL:          project.IconURL(),
		ProjectType:      project.ProjectType(),
		PageURL:          project.PageURL(),
		Downloads:        project.DownloadCount,
		DownloadsDisplay: project.DownloadsDisplay(),
		// CurseForge 没有"关注数"概念，用空串让界面不显示该列，
		// 而不是填一个假的 0（会让人以为没人关注）。
		FollowsDisplay: "",
		TypeDisplay:    project.TypeDisplay(),
		TypeIcon:       project.TypeIcon(),
		DateDisplay:    project.DateDisplay(),
	}
}

func resourceVersionFromModrinth(version *models.ModrinthVersion) models.ResourceVersion {
	result := models.ResourceVersion{
		Source:              models.ResourceSourceModrinth,
		ProjectID:           version.ProjectID,
		VersionID:           version.ID,
		Name:                version.Name,
		VersionNumber:       version.VersionNumber,
		Changelog:           derefString(version.Changelog),
		GameVersions:        version.GameVersions,
		Loaders:             version.Loaders,
		DatePublished:       version.DatePublished,
		ReleaseType:         normalizeReleaseType(version.VersionType),
		DownloadAllowed:     true,
		DisplayName:         version.DisplayName(),
		DateDisplay:         version.DateDisplay(),
		GameVersionsDisplay: version.GameVersionsDisplay(),
		LoaderDisplay:       version.LoaderDisplay(),
		Summary:             version.Summary(),
	}
	if file := version.PrimaryFile(); file != nil {
		result.FileName = file.Filename
		result.FileURL = file.URL
		result.FileSize = file.Size
		result.FileSizeDisplay = file.SizeDisplay()
		result.SHA1 = file.SHA1()
	}
	result.ReleaseTypeDisplay = releaseTypeDisplay(result.ReleaseType)
	result.Dependencies = resourceDependenciesFromModrinth(version.Dependencies)
	return result
}

func resourceVersionFromCurseForge(file *models.CurseForgeFile, projectID string) models.ResourceVersion {
	downloadURL := derefString(file.DownloadURL)
	result := models.ResourceVersion{
		Source:              models.ResourceSourceCurseForge,
		ProjectID:           projectID,
		VersionID:           fmt.Sprintf("%d", file.ID),
		Name:                file.DisplayName,
		VersionNumber:       file.DisplayName,
		GameVersions:        file.MinecraftVersions(),
		Loaders:             curseForgeLoaderNames(file),
		DatePublished:       file.FileDate,
		ReleaseType:         normalizeCurseForgeReleaseType(file.ReleaseType),
		FileName:            file.FileName,
		FileURL:             downloadURL,
		FileSize:            file.FileLength,
		SHA1:                file.SHA1(),
		DownloadAllowed:     downloadURL != "",
		DisplayName:         file.Label(),
		DateDisplay:         file.DateDisplay(),
		GameVersionsDisplay: file.GameVersionsDisplay(),
		LoaderDisplay:       file.LoaderDisplay(),
		Summary:             file.Summary(),
		FileSizeDisplay:     file.SizeDisplay(),
		ReleaseTypeDisplay:  file.ReleaseTypeDisplay(),
	}
	result.Dependencies = resourceDependenciesFromCurseForge(file.Dependencies)
	return result
}

func resourceDependenciesFromModrinth(dependencies []models.ModrinthDependency) []models.ResourceDependency {
	result := make([]models.ResourceDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		result = append(result, models.ResourceDependency{
			ProjectID:   derefString(dependency.ProjectID),
			VersionID:   derefString(dependency.VersionID),
			FileName:    derefString(dependency.FileName),
			Kind:        dependency.DependencyType,
			KindDisplay: dependency.DependencyTypeDisplay(),
			Required:    dependency.IsRequired(),
		})
	}
	return result
}

func resourceDependenciesFromCurseForge(dependencies []models.CurseForgeDependency) []models.ResourceDependency {
	result := make([]models.ResourceDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		result = append(result, models.ResourceDependency{
			ProjectID:   fmt.Sprintf("%d", dependency.ModID),
			Kind:        fmt.Sprintf("%d", dependency.RelationType),
			KindDisplay: dependency.DependencyTypeDisplay(),
			Required:    dependency.IsRequired(),
		})
	}
	return result
}

// ---- 小工具 ----

// normalizeResourceSource 归一化资源站标识；未知值一律按 Modrinth 处理
// （默认数据源，也是唯一不需要 Key 的那个）。
func normalizeResourceSource(source string) string {
	if strings.EqualFold(strings.TrimSpace(source), models.ResourceSourceCurseForge) {
		return models.ResourceSourceCurseForge
	}
	return models.ResourceSourceModrinth
}

// normalizeLoaderName 归一化加载器名；空 / minecraft / any / all / 未知 → 空串（不判定）。
func normalizeLoaderName(loader string) string {
	trimmed := strings.ToLower(strings.TrimSpace(loader))
	switch trimmed {
	case "", "minecraft", "any", "all", "vanilla", "原版":
		return ""
	case "neoforge":
		return "neoforge"
	case "forge":
		return "forge"
	case "fabric":
		return "fabric"
	case "quilt":
		return "quilt"
	default:
		return trimmed
	}
}

// normalizeLoaderNames 归一化加载器 OR 组（去空、去重、统一小写）。
func normalizeLoaderNames(loaders []string) []string {
	result := make([]string, 0, len(loaders))
	for _, loader := range loaders {
		if normalized := normalizeLoaderName(loader); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

// loaderCompatible 版本是否与该加载器兼容。
func loaderCompatible(versionLoaders []string, wanted string) bool {
	if len(versionLoaders) == 0 {
		return true // 没声明加载器（材质包/光影包常见）= 不参与判定
	}
	if containsIgnoreCase(versionLoaders, wanted) {
		return true
	}
	// 声明为 minecraft 的版本（材质包/光影包/数据包）对任何加载器都适用
	return containsIgnoreCase(versionLoaders, "minecraft")
}

// containsIgnoreCase 忽略大小写的包含判断（空串一律 false）。
// 名字与 X-4 的 content_update_check.go 里的 containsFold 区分开：同一个包里
// 两个同义小工具重名会直接编译不过。
func containsIgnoreCase(values []string, wanted string) bool {
	needle := strings.TrimSpace(wanted)
	if needle == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), needle) {
			return true
		}
	}
	return false
}

// versionsSummary 把版本/加载器列表拼成提示文案里的简短枚举（最多 3 个）。
// 加载器名统一成展示写法（forge → Forge）：版本侧的加载器名是小写的接口原文，
// 直接拼进提示会显得像另一个东西。
func versionsSummary(values []string) string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cleaned = append(cleaned, models.CurseForgeLoaderDisplayName(trimmed))
		}
	}
	if len(cleaned) == 0 {
		return "无"
	}
	if len(cleaned) > 3 {
		return strings.Join(cleaned[:3], ", ") + " 等"
	}
	return strings.Join(cleaned, ", ")
}

// clampResourceLimit 收敛搜索结果条数。
func clampResourceLimit(limit int) int {
	if limit <= 0 {
		return resourceSearchDefaultLimit
	}
	if limit > resourceSearchMaxLimit {
		return resourceSearchMaxLimit
	}
	return limit
}

// curseForgeLoaderNames 从 sortableGameVersions 里取加载器名（统一展示名，小写）。
func curseForgeLoaderNames(file *models.CurseForgeFile) []string {
	result := make([]string, 0, 2)
	for _, version := range file.SortableGameVersions {
		if version.GameVersionType != models.CurseForgeModLoaderVersionTypeID {
			continue
		}
		if name := strings.TrimSpace(version.GameVersionName); name != "" {
			result = append(result, strings.ToLower(name))
		}
	}
	return result
}

// projectTypeForModrinthVersion 已删除：Modrinth 的版本响应里没有 project_type，
// 硬猜只会猜错；子目录由调用方传（前端知道自己搜的是哪一类），未传时回退 mods。

// normalizeReleaseType 归一化发布类型（release / beta / alpha）。
func normalizeReleaseType(releaseType string) string {
	normalized := strings.ToLower(strings.TrimSpace(releaseType))
	switch normalized {
	case "release", "beta", "alpha":
		return normalized
	default:
		return ""
	}
}

// normalizeCurseForgeReleaseType CurseForge 数值发布类型 → 统一取值。
func normalizeCurseForgeReleaseType(releaseType int) string {
	switch releaseType {
	case models.CurseForgeReleaseTypeRelease:
		return "release"
	case models.CurseForgeReleaseTypeBeta:
		return "beta"
	case models.CurseForgeReleaseTypeAlpha:
		return "alpha"
	default:
		return ""
	}
}

// releaseTypeDisplay 统一发布类型 → 中文名。
func releaseTypeDisplay(releaseType string) string {
	switch releaseType {
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

// derefString 解引用可空字符串（nil → 空串）。
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// dateDisplayOrEmpty 时间 → yyyy-MM-dd；零值返回空串。
func dateDisplayOrEmpty(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}

// hostOfURL 取 URL 主机名用于提示文案（不解析失败就原样返回）。
func hostOfURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	if index := strings.Index(trimmed, "://"); index >= 0 {
		rest := trimmed[index+3:]
		if slash := strings.IndexAny(rest, "/?#"); slash >= 0 {
			return rest[:slash]
		}
		return rest
	}
	return trimmed
}
