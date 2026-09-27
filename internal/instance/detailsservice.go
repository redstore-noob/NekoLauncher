// 单个版本的详情装载：加载器识别、继承链与内容扫描。
// 移植自 NyaLauncher.Core/Launch/GameVersionDetailsService.cs。
package instance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekolauncher/internal/content"
	"nekolauncher/internal/tools"
)

// GameVersionDetails 详情页展示用的版本快照；所有集合一经构造不再变化。
type GameVersionDetails struct {
	VersionId           string
	VersionDirectory    string
	ContentDirectory    string
	LayoutProvider      string // 隔离布局判定来源
	LayoutEvidence      string // 判定依据说明
	IsIsolated          bool
	IsExternallyManaged bool // 是否为其它启动器管理的外部实例
	VersionType         string
	BaseGameVersion     string
	LoaderName          string
	LoaderVersion       string
	InstanceIconPath    string // 空串表示无自定义图标文件
	InstanceIconGlyph   string
	ReleaseTime         string
	MainClass           string
	JavaRequirement     string
	Mods                []content.GameContentEntry
	ResourcePacks       []content.GameContentEntry
	Shaders             []content.GameContentEntry
	Saves               []content.GameContentEntry
	HasShaderDirectory  bool
}

// IsVanilla 未安装任何加载器即视为原版。
func (d GameVersionDetails) IsVanilla() bool { return d.LoaderName == "原版" }

const (
	// maxInheritanceDepth inheritsFrom 循环引用的保护上限
	maxInheritanceDepth = 16

	// 各字段尚未解析到时的占位文案
	typeUnknown            = "未知"
	timeMissing            = "未提供"
	mainClassMissing       = "未提供"
	javaAutoDetect         = "自动检测"
	vanillaLoader          = "原版"
	baseVersionUnrecognized = "未识别"
)

// LoadDetails 装载单个版本的详情（对应 C# GameVersionDetailsService.LoadAsync；
// CancellationToken → context.Context）。
func LoadDetails(ctx context.Context, snapshot GameInstanceSnapshot, versionID string) (GameVersionDetails, error) {
	if err := ctx.Err(); err != nil {
		return GameVersionDetails{}, err
	}
	return loadDetails(snapshot, versionID, ctx), nil
}

func loadDetails(snapshot GameInstanceSnapshot, versionID string, ctx context.Context) GameVersionDetails {
	// 目录解析与继承链读取相互独立，可分别降级
	versionDirectory, isExternal := resolveVersionDirectory(snapshot, versionID)
	layout := GameVersionIsolationResolve(snapshot, versionID)
	lineage := readVersionInheritanceChain(snapshot, versionID, ctx)

	// 外部实例（MultiMC/CurseForge 等）没有标准版本 JSON，改读 mmc-pack.json
	if isExternal {
		readExternalComponentMetadata(versionDirectory, &lineage.loaderSignals, &lineage.baseGameVersion)
	}

	loader := detectLoader(lineage.loaderSignals)
	loaderVersion := detectLoaderVersion(&lineage, loader)
	if lineage.baseGameVersion == "" {
		if loader == vanillaLoader {
			lineage.baseGameVersion = versionID
		} else {
			lineage.baseGameVersion = baseVersionUnrecognized
		}
	}

	return collectDetails(snapshot, versionID, versionDirectory, layout, isExternal, lineage, loader, loaderVersion, ctx)
}

// resolveVersionDirectory 确定版本目录：外部实例直接用其实例目录，否则落在 versions/<versionId> 下。
func resolveVersionDirectory(snapshot GameInstanceSnapshot, versionID string) (string, bool) {
	if external, ok := TryResolveExternalInstance(snapshot.SourcePath); ok &&
		strings.EqualFold(external.InstanceId, versionID) {
		return external.InstanceDirectory, true
	}
	return filepath.Join(snapshot.MinecraftDirectory, "versions", versionID), false
}

// versionChain 继承链遍历的累积结果。
type versionChain struct {
	typeText     string
	timeText     string
	mainClassText string
	javaText     string
	// baseGameVersion 为空串表示尚未解析到（对应 C# nullable）。
	baseGameVersion string
	// loaderSignals 加载器识别信号：版本 ID、mainClass、库名、inheritsFrom 父版本。
	loaderSignals []string
	// libraryNames 仅库坐标（group:artifact:version），用于精确提取加载器版本号。
	libraryNames []string
}

// readVersionInheritanceChain 沿 inheritsFrom 逐级读取版本 JSON，汇集类型、发布时间、
// mainClass、Java 要求与库列表。JSON 缺失或损坏时以已收集到的部分降级继续。
func readVersionInheritanceChain(snapshot GameInstanceSnapshot, versionID string, ctx context.Context) versionChain {
	lineage := versionChain{
		typeText:      typeUnknown,
		timeText:      timeMissing,
		mainClassText: mainClassMissing,
		javaText:      javaAutoDetect,
	}
	lineage.loaderSignals = append(lineage.loaderSignals, versionID)

	cursor := versionID
	visited := map[string]bool{}
	for len(visited) < maxInheritanceDepth {
		key := strings.ToLower(cursor)
		if visited[key] {
			break
		}
		visited[key] = true

		jsonPath := filepath.Join(snapshot.MinecraftDirectory, "versions", cursor, cursor+".json")
		if !tools.FileExists(jsonPath) {
			break
		}

		// 版本 JSON 损坏或被占用（杀毒/备份软件锁文件）时降级继续，不中断详情页
		data, err := os.ReadFile(jsonPath)
		if err != nil {
			break
		}
		var root map[string]any
		if err := json.Unmarshal(data, &root); err != nil {
			break
		}

		readVersionMetadata(root, &lineage)
		readLibraries(root, &lineage)

		// 没有 inheritsFrom 即到达继承链顶端
		parentID := readMapString(root, "inheritsFrom")
		if strings.TrimSpace(parentID) == "" {
			break
		}
		if lineage.baseGameVersion == "" {
			lineage.baseGameVersion = parentID
		}
		lineage.loaderSignals = append(lineage.loaderSignals, parentID)
		cursor = parentID
	}

	return lineage
}

// readVersionMetadata 从单个版本 JSON 提取基础元数据；继承链上层不覆盖下层已解析的值。
func readVersionMetadata(root map[string]any, lineage *versionChain) {
	if lineage.baseGameVersion == "" {
		lineage.baseGameVersion = readMapString(root, "clientVersion")
		if lineage.baseGameVersion == "" {
			lineage.baseGameVersion = readMapString(root, "minecraftVersion")
		}
	}

	if lineage.typeText == typeUnknown {
		if value := readMapString(root, "type"); value != "" {
			lineage.typeText = value
		}
	}

	if lineage.timeText == timeMissing {
		if value := readMapString(root, "releaseTime"); value != "" {
			lineage.timeText = value
		} else if value := readMapString(root, "time"); value != "" {
			lineage.timeText = value
		}
	}

	mainClass := readMapString(root, "mainClass")
	if lineage.mainClassText == mainClassMissing && mainClass != "" {
		lineage.mainClassText = mainClass
	}
	lineage.loaderSignals = append(lineage.loaderSignals, mainClass)

	if lineage.javaText == javaAutoDetect {
		if javaVersion, ok := root["javaVersion"].(map[string]any); ok {
			if major, ok := javaVersion["majorVersion"].(float64); ok {
				lineage.javaText = fmt.Sprintf("Java %d", int64(major))
			}
		}
	}
}

// readLibraries 收集 libraries 数组中全部坐标，同时作为加载器识别信号。
func readLibraries(root map[string]any, lineage *versionChain) {
	libraries, ok := root["libraries"].([]any)
	if !ok {
		return
	}
	for _, item := range libraries {
		library, ok := item.(map[string]any)
		if !ok {
			continue
		}
		coordinate := readMapString(library, "name")
		if coordinate == "" {
			continue
		}
		lineage.loaderSignals = append(lineage.loaderSignals, coordinate)
		lineage.libraryNames = append(lineage.libraryNames, coordinate)
	}
}

// collectDetails 扫描内容目录并组装最终的详情记录。
func collectDetails(
	snapshot GameInstanceSnapshot,
	versionID string,
	versionDirectory string,
	layout GameVersionLayout,
	isExternal bool,
	lineage versionChain,
	loader string,
	loaderVersion string,
	ctx context.Context,
) GameVersionDetails {
	contentDirectory := layout.ContentDirectory
	instanceVisual := content.ResolveInstanceVisual(
		content.InstanceContext{SourcePath: snapshot.SourcePath, MinecraftDirectory: snapshot.MinecraftDirectory},
		versionID, loader)
	mods := content.ReadMods(ctx, filepath.Join(contentDirectory, "mods"))
	resourcePacks := content.ReadResourcePacks(ctx, filepath.Join(contentDirectory, "resourcepacks"))

	// 光影目录按需读取：不存在的目录没有必要探测
	shaderDirectory := filepath.Join(contentDirectory, "shaderpacks")
	hasShaderDirectory := false
	if info, err := os.Stat(shaderDirectory); err == nil && info.IsDir() {
		hasShaderDirectory = true
	}
	shaders := []content.GameContentEntry{}
	if hasShaderDirectory {
		shaders = content.ReadShaders(ctx, shaderDirectory)
	}
	saves := content.ReadSaves(ctx, filepath.Join(contentDirectory, "saves"))

	return GameVersionDetails{
		VersionId:           versionID,
		VersionDirectory:    versionDirectory,
		ContentDirectory:    contentDirectory,
		LayoutProvider:      layout.Provider,
		LayoutEvidence:      layout.Evidence,
		IsIsolated:          layout.IsIsolated,
		IsExternallyManaged: isExternal,
		VersionType:         lineage.typeText,
		BaseGameVersion:     lineage.baseGameVersion,
		LoaderName:          loader,
		LoaderVersion:       loaderVersion,
		InstanceIconPath:    instanceVisual.IconPath,
		InstanceIconGlyph:   instanceVisual.FallbackGlyph,
		ReleaseTime:         lineage.timeText,
		MainClass:           lineage.mainClassText,
		JavaRequirement:     lineage.javaText,
		Mods:                mods,
		ResourcePacks:       resourcePacks,
		Shaders:             shaders,
		Saves:               saves,
		HasShaderDirectory:  hasShaderDirectory,
	}
}

// detectLoader 按优先级从信号文本中识别加载器：NeoForge > Forge > Fabric > Quilt。
// 一次小写化后做子串判定；forge 判定天然涵盖 minecraftforge/net.minecraftforge。
func detectLoader(signals []string) string {
	haystack := strings.ToLower(strings.Join(signals, "\n"))
	switch {
	case strings.Contains(haystack, "neoforge"):
		return "NeoForge"
	case strings.Contains(haystack, "forge"):
		return "Forge"
	case strings.Contains(haystack, "fabric"):
		return "Fabric"
	case strings.Contains(haystack, "quilt"):
		return "Quilt"
	default:
		return vanillaLoader
	}
}

// loaderUidMap 加载器识别名 → 两段式 uid 的精确匹配表（mmc-pack.json 信号风格）。
var loaderUidMap = map[string]string{
	"Forge":    "net.minecraftforge",
	"NeoForge": "net.neoforged",
	"Fabric":   "net.fabricmc.fabric-loader",
	"Quilt":    "org.quiltmc.quilt-loader",
}

// detectLoaderVersion 从库坐标中提取加载器版本号。Forge 的版本形如 "1.20.1-47.2.0"，
// 需要拆出基础游戏版本与加载器版本两部分。
func detectLoaderVersion(lineage *versionChain, loader string) string {
	if loader == vanillaLoader {
		return "不适用"
	}

	loaderUID := loaderUidMap[loader]
	seen := map[string]bool{}
	candidates := append(append([]string{}, lineage.libraryNames...), lineage.loaderSignals...)
	for _, candidate := range candidates {
		key := strings.ToLower(candidate)
		if seen[key] {
			continue
		}
		seen[key] = true

		extracted, forgeBaseVersion := extractLoaderVersion(candidate, loader, loaderUID)
		if forgeBaseVersion != "" && lineage.baseGameVersion == "" {
			lineage.baseGameVersion = forgeBaseVersion
		}
		if extracted != "" {
			return extracted
		}
	}
	return "未提供"
}

// extractLoaderVersion 从单个坐标中匹配加载器版本；Forge 命中时同时返回其基础游戏版本段。
func extractLoaderVersion(candidate, loader, loaderUID string) (version string, forgeBaseVersion string) {
	parts := strings.Split(candidate, ":")

	// 两段式坐标（uid:version）按 uid 精确匹配
	if len(parts) == 2 {
		if loaderUID != "" && strings.EqualFold(parts[0], loaderUID) {
			return parts[1], ""
		}
		return "", ""
	}
	if len(parts) < 3 {
		return "", ""
	}

	if !matchesLoaderLibrary(loader, parts[0], parts[1]) {
		return "", ""
	}

	versionText := parts[2]
	if loader != "Forge" {
		return versionText, ""
	}

	separator := strings.Index(versionText, "-")
	if separator <= 0 || separator >= len(versionText)-1 {
		return versionText, ""
	}
	return versionText[separator+1:], versionText[:separator]
}

// matchesLoaderLibrary 各加载器对应的库坐标（group 或 group:artifact 前缀）匹配规则。
func matchesLoaderLibrary(loader, group, artifact string) bool {
	artifactLower := strings.ToLower(artifact)
	switch loader {
	case "NeoForge":
		return strings.EqualFold(group, "net.neoforged") &&
			(strings.Contains(artifactLower, "neoforge") || strings.Contains(artifactLower, "loader"))
	case "Fabric":
		return strings.EqualFold(group, "net.fabricmc") &&
			strings.Contains(artifactLower, "fabric-loader")
	case "Quilt":
		return strings.EqualFold(group, "org.quiltmc") &&
			strings.Contains(artifactLower, "quilt-loader")
	case "Forge":
		return strings.EqualFold(group, "net.minecraftforge") &&
			(artifact == "fmlloader" || artifact == "forge" ||
				strings.EqualFold(artifact, "FMLLOADER") || strings.EqualFold(artifact, "FORGE"))
	default:
		return false
	}
}

// readExternalComponentMetadata 读取外部实例的 mmc-pack.json 组件清单：
// net.minecraft 组件补全基础版本，其余组件以 "uid:version" 形式加入加载器识别信号。
func readExternalComponentMetadata(instanceDirectory string, loaderSignals *[]string, baseGameVersion *string) {
	packPath := filepath.Join(instanceDirectory, "mmc-pack.json")
	if !tools.FileExists(packPath) {
		return
	}

	data, err := os.ReadFile(packPath)
	if err != nil {
		// 元数据读取失败时保持降级路径，不中断详情页
		return
	}
	var document struct {
		Components []map[string]any `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil || document.Components == nil {
		return
	}

	for _, component := range document.Components {
		uid := readMapString(component, "uid")
		version := readMapString(component, "version")
		if strings.TrimSpace(uid) == "" || strings.TrimSpace(version) == "" {
			continue
		}
		if strings.EqualFold(uid, "net.minecraft") && *baseGameVersion == "" {
			*baseGameVersion = version
		}
		*loaderSignals = append(*loaderSignals, uid+":"+version)
	}
}

// readMapString 读取对象中字符串类型的字段，缺失或类型不符返回空串。
func readMapString(element map[string]any, propertyName string) string {
	if element == nil {
		return ""
	}
	if value, ok := element[propertyName].(string); ok {
		return value
	}
	return ""
}
