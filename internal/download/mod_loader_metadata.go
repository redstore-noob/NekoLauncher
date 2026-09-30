package download

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"nekolauncher/internal/download/modrinth"
)

// ModLoaderType 支持的 Mod Loader 类型。
type ModLoaderType int

const (
	ModLoaderVanilla ModLoaderType = iota
	ModLoaderFabric
	ModLoaderQuilt
	ModLoaderNeoForge
	ModLoaderForge
	ModLoaderOptifine
)

// String 枚举名（与 C# 枚举名一致，用于持久化与显示）。
func (t ModLoaderType) String() string {
	switch t {
	case ModLoaderFabric:
		return "Fabric"
	case ModLoaderQuilt:
		return "Quilt"
	case ModLoaderNeoForge:
		return "NeoForge"
	case ModLoaderForge:
		return "Forge"
	case ModLoaderOptifine:
		return "OptiFine"
	default:
		return "Vanilla"
	}
}

// ParseModLoaderType 从名称解析 Loader 类型（大小写不敏感）。
func ParseModLoaderType(name string) (ModLoaderType, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "vanilla":
		return ModLoaderVanilla, true
	case "fabric":
		return ModLoaderFabric, true
	case "quilt":
		return ModLoaderQuilt, true
	case "neoforge":
		return ModLoaderNeoForge, true
	case "forge":
		return ModLoaderForge, true
	case "optifine":
		return ModLoaderOptifine, true
	default:
		return ModLoaderVanilla, false
	}
}

// ModLoaderVersion 单个 Mod Loader 版本条目（从 Loader API 获取）。
type ModLoaderVersion struct {
	// Type Loader 类型。
	Type ModLoaderType
	// LoaderVersion Loader 版本号，如 "0.16.14"（Fabric）或 "21.8.1"（NeoForge）。
	LoaderVersion string
	// IsStable 是否为推荐/稳定版本。
	IsStable bool
	// BuildNumber 构建号（部分 Loader 使用）。
	BuildNumber int
	// MetadataURL 安装此 Loader 所需的版本 JSON 元数据 URL。
	MetadataURL string
	// RequiresInstallerExtraction MetadataURL 指向的是安装器 JAR 而非版本 JSON。
	// 需要下载 JAR 后从中提取 version.json 或 install_profile.json。
	RequiresInstallerExtraction bool
}

// DisplayName 用于 UI 展示的格式化名称。
func (v ModLoaderVersion) DisplayName() string {
	switch v.Type {
	case ModLoaderFabric:
		return fmt.Sprintf("Fabric Loader %s", v.LoaderVersion)
	case ModLoaderQuilt:
		return fmt.Sprintf("Quilt Loader %s", v.LoaderVersion)
	case ModLoaderNeoForge:
		return fmt.Sprintf("NeoForge %s", v.LoaderVersion)
	case ModLoaderForge:
		return fmt.Sprintf("Forge %s", v.LoaderVersion)
	case ModLoaderOptifine:
		return fmt.Sprintf("OptiFine %s", v.LoaderVersion)
	default:
		return v.LoaderVersion
	}
}

// ModLoaderMetadata Mod Loader 元数据获取服务。
// 从各 Loader 官方 API 获取可用版本列表和安装元数据。对应 C# ModLoaderMetadata。

const fabricLoaderVersionsURL = "https://meta.fabricmc.net/v2/versions/loader/"
const quiltLoaderVersionsURL = "https://meta.quiltmc.org/v3/versions/loader/"

const neoForgeVersionsURL = "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"

// neoForgeBmclListURL BMCLAPI（bangbang93 镜像）按 Minecraft 版本查询 NeoForge 列表。
const neoForgeBmclListURL = "https://bmclapi2.bangbang93.com/neoforge/list/%s"

// neoForgeBmclInstallerURL BMCLAPI 下载 NeoForge 安装器 JAR（302 重定向到实际文件）。
const neoForgeBmclInstallerURL = "https://bmclapi2.bangbang93.com/neoforge/version/%s/download/installer.jar"

const forgePromotionsURL = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"

// optiFineDownloadsURL OptiFine 官网下载页（HTML）。OptiFine 没有 meta API，
// 版本列表靠解析页面上的 adloadx 链接获得（BMCLAPI 未提供 OptiFine 列表端点）。
const optiFineDownloadsURL = "https://optifine.net/downloads"

// optiFineAdLoadURLFormat OptiFine 安装器下载引导页（输入文件名，页面内含 downloadx 直链）。
const optiFineAdLoadURLFormat = "https://optifine.net/adloadx?f=%s"

// optiFineDownloadLinkRegex 从 adloadx 页面提取真实下载直链（downloadx?f=...&x=token）。
var optiFineDownloadLinkRegex = regexp.MustCompile(`downloadx\?f=[A-Za-z0-9._=&;-]+`)

// optiFineFileLinkRegex 从下载页提取安装器文件名（OptiFine_1.21.11_HD_U_J9.jar / preview_OptiFine_...jar）。
var optiFineFileLinkRegex = regexp.MustCompile(`(?:preview_)?OptiFine_[0-9][A-Za-z0-9._-]*?\.jar`)

// fabricLoaderEntry Fabric/Quilt meta API 响应条目。
type fabricLoaderEntry struct {
	Loader *fabricLoaderInfo `json:"loader"`
}

type fabricLoaderInfo struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

// neoForgeBmclEntry BMCLAPI /neoforge/list/:mcversion 返回条目。
type neoForgeBmclEntry struct {
	Version string `json:"version"`
}

// loaderMetadataTimeout Loader 元数据请求的 10 秒超时（与 C# 各调用点一致）。
const loaderMetadataTimeout = 10 * time.Second

func shortTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, loaderMetadataTimeout)
}

// GetFabricVersions 获取指定 Minecraft 版本可用的 Fabric Loader 版本列表。
func GetFabricVersions(ctx context.Context, minecraftVersion string) ([]ModLoaderVersion, error) {
	if strings.TrimSpace(minecraftVersion) == "" {
		return nil, fmt.Errorf("minecraftVersion 不能为空")
	}

	ctx, cancel := shortTimeout(ctx)
	defer cancel()
	rawJSON, err := SourceProvider.GetString(ctx, fabricLoaderVersionsURL+minecraftVersion, nil)
	if err != nil {
		return nil, err
	}
	var response []fabricLoaderEntry
	if err := json.Unmarshal([]byte(rawJSON), &response); err != nil {
		return nil, err
	}

	result := make([]ModLoaderVersion, 0, len(response))
	for _, entry := range response {
		if entry.Loader == nil {
			continue
		}
		result = append(result, ModLoaderVersion{
			Type:          ModLoaderFabric,
			LoaderVersion: entry.Loader.Version,
			IsStable:      entry.Loader.Stable,
			MetadataURL: fmt.Sprintf("%s%s/%s/profile/json",
				fabricLoaderVersionsURL, minecraftVersion, entry.Loader.Version),
		})
	}
	return result, nil
}

// GetQuiltVersions 获取指定 Minecraft 版本可用的 Quilt Loader 版本列表。
// Quilt 的 meta API（v3）响应形状与 Fabric v2 相同，可直接复用 Fabric 的反序列化模型；
// profile JSON 同样是标准版本 JSON（inheritsFrom + Knot 启动主类），
// 库文件来自 maven.quiltmc.org 与 maven.fabricmc.org。
func GetQuiltVersions(ctx context.Context, minecraftVersion string) ([]ModLoaderVersion, error) {
	if strings.TrimSpace(minecraftVersion) == "" {
		return nil, fmt.Errorf("minecraftVersion 不能为空")
	}

	ctx, cancel := shortTimeout(ctx)
	defer cancel()
	rawJSON, err := SourceProvider.GetString(ctx, quiltLoaderVersionsURL+minecraftVersion, nil)
	if err != nil {
		return nil, err
	}
	var response []fabricLoaderEntry
	if err := json.Unmarshal([]byte(rawJSON), &response); err != nil {
		return nil, err
	}

	result := make([]ModLoaderVersion, 0, len(response))
	for _, entry := range response {
		if entry.Loader == nil {
			continue
		}
		result = append(result, ModLoaderVersion{
			Type:          ModLoaderQuilt,
			LoaderVersion: entry.Loader.Version,
			IsStable:      entry.Loader.Stable,
			MetadataURL: fmt.Sprintf("%s%s/%s/profile/json",
				quiltLoaderVersionsURL, minecraftVersion, entry.Loader.Version),
		})
	}
	return result, nil
}

// GetNeoForgeVersions 获取指定 Minecraft 版本可用的 NeoForge 版本列表。
// 按用户选择的下载源决定优先顺序：BMCL 源（默认）优先走 BMCLAPI 镜像
// （国内网络可达性好），Official 源优先走官方 Maven，另一侧仅作兜底。
// NeoForge 版本格式为 "{mcMajor}.{mcMinor}.{patch}"，如 "21.8.1"。
func GetNeoForgeVersions(ctx context.Context, minecraftVersion string) ([]ModLoaderVersion, error) {
	if strings.TrimSpace(minecraftVersion) == "" {
		return nil, fmt.Errorf("minecraftVersion 不能为空")
	}

	if SourceProvider.Active().Name != DownloadSources.Official.Name {
		if fromBmcl := tryFetchNeoForgeFromBmcl(ctx, minecraftVersion); fromBmcl != nil {
			return fromBmcl, nil
		}
		return fetchNeoForgeFromOfficialMaven(ctx, minecraftVersion)
	}

	fromOfficial, err := fetchNeoForgeFromOfficialMaven(ctx, minecraftVersion)
	if err == nil && len(fromOfficial) > 0 {
		return fromOfficial, nil
	}
	// 官方源不可用（网络/格式异常）时兜底 BMCL，避免版本列表为空
	if fromBmcl := tryFetchNeoForgeFromBmcl(ctx, minecraftVersion); fromBmcl != nil {
		return fromBmcl, nil
	}
	return []ModLoaderVersion{}, nil
}

// tryFetchNeoForgeFromBmcl BMCLAPI（bangbang93 镜像）获取 NeoForge 版本列表：
// 直接返回该 MC 版本下的版本，无需前缀推导；不可用或无结果返回 nil。
func tryFetchNeoForgeFromBmcl(ctx context.Context, minecraftVersion string) []ModLoaderVersion {
	ctx, cancel := shortTimeout(ctx)
	defer cancel()
	bmclJSON, err := SourceProvider.GetString(ctx, fmt.Sprintf(neoForgeBmclListURL, minecraftVersion), nil)
	if err != nil {
		return nil
	}
	var entries []neoForgeBmclEntry
	if err := json.Unmarshal([]byte(bmclJSON), &entries); err != nil {
		return nil
	}

	seen := map[string]bool{}
	var versions []string
	for _, e := range entries {
		if strings.TrimSpace(e.Version) == "" {
			continue
		}
		v := NormalizeBmclNeoForgeVersion(e.Version)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		versions = append(versions, v)
	}
	sort.SliceStable(versions, func(i, j int) bool {
		return modrinth.CompareVersionStrings(versions[i], versions[j]) > 0
	})

	result := make([]ModLoaderVersion, 0, len(versions))
	for _, v := range versions {
		result = append(result, ModLoaderVersion{
			Type:                        ModLoaderNeoForge,
			LoaderVersion:               v,
			MetadataURL:                 fmt.Sprintf(neoForgeBmclInstallerURL, v),
			RequiresInstallerExtraction: true,
		})
	}
	return result
}

// fetchNeoForgeFromOfficialMaven 官方 Maven maven-metadata.xml 获取 NeoForge 版本列表（按前缀匹配）。
func fetchNeoForgeFromOfficialMaven(ctx context.Context, minecraftVersion string) ([]ModLoaderVersion, error) {
	ctx, cancel := shortTimeout(ctx)
	defer cancel()
	metadataXML, err := SourceProvider.GetString(ctx, neoForgeVersionsURL, nil)
	if err != nil {
		return nil, err
	}

	versions := parseMavenVersions(metadataXML)
	prefix := ToNeoForgePrefix(minecraftVersion)

	matched := make([]string, 0, len(versions))
	for _, v := range versions {
		// 带版本边界匹配：仅匹配 "21.8.x" 或 "21.0.x"（基础版 1.21 → 21.0.x），
		// 防止 "21.1" 误匹配 "21.11.x"、防止 "21" 误匹配全部 21.x 系列。
		if matchesPrefix(v, prefix) {
			matched = append(matched, v)
		}
	}
	// 按版本号数字比较降序（字典序会让 21.8.9 排在 21.8.54 前面）
	sort.SliceStable(matched, func(i, j int) bool {
		return modrinth.CompareVersionStrings(matched[i], matched[j]) > 0
	})

	result := make([]ModLoaderVersion, 0, len(matched))
	for _, v := range matched {
		result = append(result, ModLoaderVersion{
			Type:          ModLoaderNeoForge,
			LoaderVersion: v,
			MetadataURL: fmt.Sprintf(
				"https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", v, v),
			RequiresInstallerExtraction: true,
		})
	}
	return result, nil
}

// NormalizeBmclNeoForgeVersion 归一化 BMCL 返回的 NeoForge 版本号：
// 纯版本号（如 "21.8.54"）原样返回；带 "mc-版本" 前缀的（如 "1.20.1-47.1.12"）截取纯版本号。
func NormalizeBmclNeoForgeVersion(version string) string {
	v := strings.TrimSpace(version)
	if len(v) == 0 || v[0] < '0' || v[0] > '9' {
		return v
	}
	dash := strings.Index(v, "-")
	if dash > 0 && dash < len(v)-1 {
		tail := v[dash+1:]
		if tail[0] >= '0' && tail[0] <= '9' {
			return tail
		}
	}
	return v
}

// GetForgeVersions 获取指定 Minecraft 版本推荐的 Forge 版本。
// Forge 的版本分发方式不同于 Fabric/NeoForge，只提供推荐版本。
func GetForgeVersions(ctx context.Context, minecraftVersion string) ([]ModLoaderVersion, error) {
	if strings.TrimSpace(minecraftVersion) == "" {
		return nil, fmt.Errorf("minecraftVersion 不能为空")
	}

	ctx, cancel := shortTimeout(ctx)
	defer cancel()
	rawJSON, err := SourceProvider.GetString(ctx, forgePromotionsURL, nil)
	if err != nil {
		return nil, err
	}

	var doc struct {
		Promos map[string]string `json:"promos"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &doc); err != nil {
		return nil, err
	}

	var results []ModLoaderVersion
	for key, forgeVersion := range doc.Promos {
		// key 格式: "{mcVersion}-latest" 或 "{mcVersion}-recommended"
		// 必须与所选 MC 版本精确匹配：防止 "1.21" 误匹配 "1.21.1-latest"
		if !matchesForgePromoKey(key, minecraftVersion) {
			continue
		}
		if strings.TrimSpace(forgeVersion) == "" {
			continue
		}

		isRecommended := strings.HasSuffix(strings.ToLower(key), "-recommended")
		fullVersion := fmt.Sprintf("%s-%s", minecraftVersion, forgeVersion)

		results = append(results, ModLoaderVersion{
			Type:          ModLoaderForge,
			LoaderVersion: fullVersion,
			IsStable:      isRecommended,
			MetadataURL: fmt.Sprintf(
				"https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar",
				fullVersion, fullVersion),
			RequiresInstallerExtraction: true,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].IsStable != results[j].IsStable {
			return results[i].IsStable
		}
		return results[i].LoaderVersion > results[j].LoaderVersion
	})
	return results, nil
}

// GetOptifineVersions 获取指定 Minecraft 版本可用的 OptiFine 版本列表。
// OptiFine 没有 meta API：解析官网下载页上的 adloadx 链接
// （OptiFine_1.21.11_HD_U_J9.jar / preview_OptiFine_26.1.2_HD_U_K1_pre2.jar），
// 按文件名中的 Minecraft 版本号过滤；preview 前缀标记为非稳定。
func GetOptifineVersions(ctx context.Context, minecraftVersion string) ([]ModLoaderVersion, error) {
	if strings.TrimSpace(minecraftVersion) == "" {
		return nil, fmt.Errorf("minecraftVersion 不能为空")
	}

	ctx, cancel := shortTimeout(ctx)
	defer cancel()
	pageHTML, err := SourceProvider.GetString(ctx, optiFineDownloadsURL, nil)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	result := []ModLoaderVersion{}
	for _, fileName := range optiFineFileLinkRegex.FindAllString(pageHTML, -1) {
		if seen[fileName] {
			continue
		}
		seen[fileName] = true

		isPreview := strings.HasPrefix(fileName, "preview_")
		baseName := strings.TrimPrefix(fileName, "preview_") // OptiFine_<mc>_<type>.jar
		nameNoExt := strings.TrimSuffix(baseName, ".jar")
		// 文件名形状 OptiFine_<mcVersion>_<type>，type 自身可含下划线（HD_U_J9 / HD_U_K1_pre2）
		rest := strings.TrimPrefix(nameNoExt, "OptiFine_")
		underscore := strings.Index(rest, "_")
		if underscore <= 0 {
			continue
		}
		mcVersion := rest[:underscore]
		optiFineType := rest[underscore+1:]
		if !strings.EqualFold(mcVersion, minecraftVersion) {
			continue
		}
		result = append(result, ModLoaderVersion{
			Type:          ModLoaderOptifine,
			LoaderVersion: mcVersion + "_" + optiFineType,
			IsStable:      !isPreview,
			// MetadataURL 指向 adloadx 引导页；安装时需再解析出 downloadx 直链
			// （见 mod_loader_installer.go installOptifineFromInstallerJar）
			MetadataURL:                 fmt.Sprintf(optiFineAdLoadURLFormat, fileName),
			RequiresInstallerExtraction: true,
		})
	}
	return result, nil
}

// GetModLoaderVersions 获取指定 Loader 类型在指定 Minecraft 版本下的可用版本列表。
func GetModLoaderVersions(ctx context.Context, loaderType ModLoaderType, minecraftVersion string) ([]ModLoaderVersion, error) {
	switch loaderType {
	case ModLoaderFabric:
		return GetFabricVersions(ctx, minecraftVersion)
	case ModLoaderQuilt:
		return GetQuiltVersions(ctx, minecraftVersion)
	case ModLoaderNeoForge:
		return GetNeoForgeVersions(ctx, minecraftVersion)
	case ModLoaderForge:
		return GetForgeVersions(ctx, minecraftVersion)
	case ModLoaderOptifine:
		return GetOptifineVersions(ctx, minecraftVersion)
	default:
		return []ModLoaderVersion{}, nil
	}
}

// ToNeoForgePrefix 将 Minecraft 版本号转换为 NeoForge 的 Maven 版本前缀。
// 如 "1.21.8" → "21.8"，"1.21"（基础版）→ "21.0"。
func ToNeoForgePrefix(minecraftVersion string) string {
	// NeoForge 版本格式: {mcMinor}.{mcPatch}
	// MC 1.21.8 → NeoForge 21.8.x；MC 1.21（无补丁号）→ NeoForge 21.0.x
	parts := strings.Split(minecraftVersion, ".")
	if len(parts) >= 3 {
		return fmt.Sprintf("%s.%s", parts[1], parts[2])
	}
	if len(parts) == 2 {
		return fmt.Sprintf("%s.0", parts[1])
	}
	return minecraftVersion
}

// matchesPrefix 带版本边界的精确前缀匹配：仅当 value 等于 prefix 或以 "prefix." 开头时匹配，
// 防止 "21.1" 误匹配 "21.11.x" 这类错误。
func matchesPrefix(value, prefix string) bool {
	return strings.EqualFold(value, prefix) ||
		strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix)+".")
}

// matchesForgePromoKey Forge promos 键匹配：键格式为 "{mcVersion}-latest" / "{mcVersion}-recommended"，
// 要求去后缀后与所选 MC 版本精确相等，防止 "1.21" 误匹配 "1.21.1-latest"。
func matchesForgePromoKey(promoKey, minecraftVersion string) bool {
	if strings.TrimSpace(promoKey) == "" || strings.TrimSpace(minecraftVersion) == "" {
		return false
	}
	for _, suffix := range []string{"-latest", "-recommended"} {
		if !strings.HasSuffix(strings.ToLower(promoKey), suffix) {
			continue
		}
		return strings.EqualFold(promoKey[:len(promoKey)-len(suffix)], minecraftVersion)
	}
	return false
}

// parseMavenVersions 解析 Maven maven-metadata.xml 中的 version 列表。
func parseMavenVersions(metadataXML string) []string {
	var versions []string
	const startTag = "<version>"
	const endTag = "</version>"

	span := metadataXML
	for {
		startIndex := strings.Index(span, startTag)
		if startIndex < 0 {
			break
		}
		valueStart := startIndex + len(startTag)
		rest := span[valueStart:]
		endIndex := strings.Index(rest, endTag)
		if endIndex < 0 {
			break
		}
		versions = append(versions, rest[:endIndex])
		span = rest[endIndex+len(endTag):]
	}
	return versions
}
