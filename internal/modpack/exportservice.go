// 整合包导出服务：把实例内容目录打包为 Modrinth（.mrpack）或 MultiMC（.zip）整合包。
// 移植自 NyaLauncher.Core/Modpack/ModpackExportService.cs。
// 两者都以 overrides/ 承载实例文件；Modrinth 额外尝试把 mod 哈希匹配到 Modrinth
// 的 version_file 端点，命中时改为 index 声明（导入方从直链下载），未命中保持 overrides。
package modpack

import (
	"archive/zip"
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"nekolauncher/internal/tools"
)

// ModpackExportFormat 整合包导出格式。
type ModpackExportFormat int

const (
	// FormatModrinth Modrinth .mrpack：mod 优先声明直链由导入方下载，其余进入 overrides。
	FormatModrinth ModpackExportFormat = iota
	// FormatMultiMc MultiMC / PrismLauncher .zip：mmc-pack.json + instance.cfg + overrides。
	FormatMultiMc
	// FormatCurseForge CurseForge .zip：manifest.json（projectID/fileID 声明）+ overrides。
	FormatCurseForge
)

// 内容分类常量（同时用作包内相对路径的顶层目录名）。
const (
	CategoryMods          = "mods"
	CategoryConfig        = "config"
	CategoryRoot          = "root"
	CategoryResourcePacks = "resourcepacks"
	CategoryShaderPacks   = "shaderpacks"
	CategorySaves         = "saves"
)

// ModpackContentItem 可打包的实例内容单元（一个 mod 文件、一个存档目录、config 目录等）。
// RelativePath 相对实例内容目录、使用正斜杠分隔。
type ModpackContentItem struct {
	Category     string
	RelativePath string
	Name         string
	SizeBytes    int64
	IsDirectory  bool
}

// CategoryDisplay 分类展示名（模组 / 配置 / 根文件 / 资源包 / 光影包 / 存档）。
func (item ModpackContentItem) CategoryDisplay() string {
	switch item.Category {
	case CategoryMods:
		return "模组"
	case CategoryConfig:
		return "配置"
	case CategoryRoot:
		return "根文件"
	case CategoryResourcePacks:
		return "资源包"
	case CategoryShaderPacks:
		return "光影包"
	case CategorySaves:
		return "存档"
	default:
		return item.Category
	}
}

// ModpackExportProgress 导出进度：阶段名 + 当前/总数。
type ModpackExportProgress struct {
	Phase   string
	Current int
	Total   int
}

// ModrinthFileMatch Modrinth version_file 端点按哈希命中的结果（声明进 index 的文件）。
type ModrinthFileMatch struct {
	FileName   string
	DownloadUrl string
	Sha1       string
	Sha512     string // 空串表示未提供
	SizeBytes  int64
}

// ModpackExportResult 导出结果统计。
type ModpackExportResult struct {
	OutputPath    string
	DeclaredFiles int
	OverrideFiles int
	Warnings      []string
}

// ModpackExportOptions 导出参数。
type ModpackExportOptions struct {
	Format ModpackExportFormat
	PackName string
	// PackVersion 整合包版本号（mrpack 的 versionId；MultiMC 写入 instance.cfg 注释性字段）。
	// 空串时导出流程按 "1.0.0" 处理（C# 属性默认值，见 PORTING_NOTES.md）。
	PackVersion string
	Author      string
	UpdateLink  string
	Description string
	// IconPngPath 图标（必须为 png，MultiMC 写入包根 icon.png，Modrinth 写入 overrides/icon.png）。
	IconPngPath string
	// MinecraftVersion 无法省略：为空时 Export 返回错误。
	MinecraftVersion string
	// LoaderName 加载器名（原版 / Fabric / Forge / NeoForge / Quilt）；空或“原版”视为无加载器。
	LoaderName    string
	LoaderVersion string
	// IncludedPaths 勾选打包的内容条目（相对路径）。
	IncludedPaths []string
	// ResolveModrinthLinks 是否把 mod 哈希提交到 Modrinth 换取直链（离线/失败时回退进 overrides）。
	// Go 零值为 false；C# 默认 true——需要默认开启时使用 NewExportOptions 或显式置 true。
	ResolveModrinthLinks bool
	// CurseForgeAPIKey CurseForge 导出时的用户 API Key（指纹反查 projectID/fileID）；
	// 为空时全部 mod 直接进 overrides，整合包仍然可用。
	CurseForgeAPIKey string
}

// NewExportOptions 返回带 C# 默认值的导出参数。
func NewExportOptions() ModpackExportOptions {
	return ModpackExportOptions{
		PackVersion:          "1.0.0",
		ResolveModrinthLinks: true,
	}
}

// ModrinthResolver Modrinth 直链解析器（sha1 → 命中信息；未命中返回 nil, nil）。
// 测试与离线场景可替换；默认走 Modrinth version_file 端点。
// 对应 C# 的静态属性 ModrinthResolver。
var ModrinthResolver = func(ctx context.Context, sha1Hex string) (*ModrinthFileMatch, error) {
	return resolveModrinthFile(ctx, sha1Hex)
}

// CollectContent 收集实例内容目录中可打包的内容单元。
func CollectContent(contentDirectory string) []ModpackContentItem {
	if strings.TrimSpace(contentDirectory) == "" {
		panic("contentDirectory 不能为空")
	}
	if info, err := os.Stat(contentDirectory); err != nil || !info.IsDir() {
		return []ModpackContentItem{}
	}

	var items []ModpackContentItem
	added := map[string]bool{}
	add := func(category, relativePath, name string, isDirectory bool) {
		normalized := filepath.ToSlash(relativePath)
		key := normalized
		if runtime.GOOS == "windows" {
			key = strings.ToLower(normalized)
		}
		if added[key] {
			return
		}
		added[key] = true
		var size int64
		fullPath := filepath.Join(contentDirectory, filepath.FromSlash(normalized))
		if info, err := os.Stat(fullPath); err == nil {
			if isDirectory {
				size = directorySize(fullPath)
			} else {
				size = info.Size()
			}
		}
		// 读不到大小的条目仍然列出（打包时再报错）
		items = append(items, ModpackContentItem{
			Category:     category,
			RelativePath: normalized,
			Name:         name,
			SizeBytes:    size,
			IsDirectory:  isDirectory,
		})
	}

	// 模组：单个 jar / 禁用态 jar
	modsDirectory := filepath.Join(contentDirectory, "mods")
	if info, err := os.Stat(modsDirectory); err == nil && info.IsDir() {
		for _, file := range enumerateSafe(modsDirectory) {
			name := filepath.Base(file)
			lower := strings.ToLower(name)
			if strings.HasSuffix(lower, ".jar") || strings.HasSuffix(lower, ".jar.disabled") {
				relative, err := filepath.Rel(contentDirectory, file)
				if err != nil {
					continue
				}
				add(CategoryMods, relative, name, false)
			}
		}
	}

	// 根文件：options.txt
	if tools.FileExists(filepath.Join(contentDirectory, "options.txt")) {
		add(CategoryRoot, "options.txt", "options.txt", false)
	}

	// 配置目录整体一个条目
	if info, err := os.Stat(filepath.Join(contentDirectory, "config")); err == nil && info.IsDir() {
		add(CategoryConfig, "config", "config/", true)
	}

	for _, pair := range []struct{ category, folder string }{
		{CategoryResourcePacks, "resourcepacks"},
		{CategoryShaderPacks, "shaderpacks"},
	} {
		directory := filepath.Join(contentDirectory, pair.folder)
		if info, err := os.Stat(directory); err != nil || !info.IsDir() {
			continue
		}
		for _, path := range enumerateTopLevel(directory) {
			isDirectory := false
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				isDirectory = true
			}
			name := filepath.Base(path)
			if isDirectory {
				name += "/"
			}
			relative, err := filepath.Rel(contentDirectory, path)
			if err != nil {
				continue
			}
			add(pair.category, relative, name, isDirectory)
		}
	}

	// 存档目录整体一个条目（整个 saves 可能很大，按存档分目录勾选）
	savesDirectory := filepath.Join(contentDirectory, "saves")
	if info, err := os.Stat(savesDirectory); err == nil && info.IsDir() {
		entries, err := os.ReadDir(savesDirectory)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				// 启动器的临时目录（导入 .nya-import-*、回滚恢复 .nya-restore-*）
				// 与存档同级：它们不是世界的存档，别打进整合包
				if strings.HasPrefix(entry.Name(), ".") {
					continue
				}
				relative, err := filepath.Rel(contentDirectory, filepath.Join(savesDirectory, entry.Name()))
				if err != nil {
					continue
				}
				add(CategorySaves, relative, entry.Name()+"/", true)
			}
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items
}

// Export 打包整合包到指定输出路径（.mrpack / .zip）。
// 打包过程中发生致命错误时返回 error；单个文件读取失败会跳过并记入 Warnings。
// progress 为可选回调（对应 C# IProgress<ModpackExportProgress>）。
func Export(
	ctx context.Context,
	options ModpackExportOptions,
	contentDirectory string,
	outputPath string,
	progress func(ModpackExportProgress),
) (ModpackExportResult, error) {
	var result ModpackExportResult
	if strings.TrimSpace(contentDirectory) == "" || strings.TrimSpace(outputPath) == "" {
		return result, fmt.Errorf("参数无效")
	}
	if strings.TrimSpace(options.PackName) == "" {
		return result, fmt.Errorf("整合包名称不能为空")
	}
	if strings.TrimSpace(options.MinecraftVersion) == "" {
		return result, fmt.Errorf("无法确定 Minecraft 版本")
	}
	if info, err := os.Stat(contentDirectory); err != nil || !info.IsDir() {
		return result, fmt.Errorf("实例内容目录不存在：%s", contentDirectory)
	}

	packVersion := options.PackVersion
	if strings.TrimSpace(packVersion) == "" {
		packVersion = "1.0.0"
	}

	var warnings []string
	includedSet := map[string]bool{}
	for _, path := range options.IncludedPaths {
		key := filepath.ToSlash(path)
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		includedSet[key] = true
	}
	allItems := CollectContent(contentDirectory)
	var selected []ModpackContentItem
	for _, item := range allItems {
		key := item.RelativePath
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if includedSet[key] {
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 {
		return result, fmt.Errorf("没有勾选任何要打包的内容")
	}

	// 逐文件展开为 (包内路径, 绝对路径)；目录条目递归展开
	var payloadFiles []payloadEntry
	for _, item := range selected {
		source := filepath.Join(contentDirectory, filepath.FromSlash(item.RelativePath))
		if item.IsDirectory {
			for _, file := range enumerateSafe(source) {
				payloadFiles = append(payloadFiles, payloadEntry{
					archivePath: toArchivePath(item.RelativePath, file, contentDirectory),
					sourcePath:  file,
				})
			}
		} else {
			payloadFiles = append(payloadFiles, payloadEntry{item.RelativePath, source})
		}
	}

	// Modrinth：mods 尝试按 sha1 匹配直链；未命中的留在 overrides
	declaredByPath := map[string]ModrinthFileMatch{}
	declaredFiles := 0
	// CurseForge：mods 按指纹匹配 files 声明（projectID/fileID）；未命中的留在 overrides
	cfFilesByPath := map[string]curseForgeFileRef{}
	var cfMatched []curseForgeFingerprintMatch
	if options.Format == FormatCurseForge {
		var resolveErr error
		cfFilesByPath, cfMatched, resolveErr = resolveCurseForgeModRefs(ctx, options, contentDirectory, selected, &warnings, progress)
		if resolveErr != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, resolveErr
		}
		declaredFiles = len(cfFilesByPath)
	} else if options.Format == FormatModrinth && options.ResolveModrinthLinks {
		var modPaths []string
		for _, item := range selected {
			if item.Category == CategoryMods && !item.IsDirectory {
				modPaths = append(modPaths, item.RelativePath)
			}
		}
		for _, modPath := range modPaths {
			if progress != nil {
				progress(ModpackExportProgress{"正在解析 Modrinth 直链", len(declaredByPath), len(modPaths)})
			}
			sha1Hex, err := computeFileSHA1(filepath.Join(contentDirectory, filepath.FromSlash(modPath)))
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("计算哈希失败（%s）：%v", modPath, err))
				continue
			}
			match, err := ModrinthResolver(ctx, sha1Hex)
			if err != nil {
				if ctx.Err() != nil {
					return result, ctx.Err()
				}
				warnings = append(warnings, fmt.Sprintf("查询 Modrinth 失败（%s）：%v", modPath, err))
			}
			if match != nil {
				declaredByPath[filepath.ToSlash(modPath)] = *match
			}
		}
		declaredFiles = len(declaredByPath)
		if declaredFiles < len(modPaths) {
			warnings = append(warnings,
				fmt.Sprintf("%d 个 mod 未匹配到 Modrinth 直链，将直接打包进 overrides。", len(modPaths)-declaredFiles))
		}
	}

	// 写包
	phase := fmt.Sprintf("正在写入 %d 个文件", len(payloadFiles))
	totalOverrides := 0
	if directory := filepath.Dir(outputPath); strings.TrimSpace(directory) != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return result, err
		}
	}
	// GUID 后缀防止并发导出到同一路径时相互截断临时文件
	temporaryPath := fmt.Sprintf("%s.%s.nya-pack-tmp", outputPath, newModpackGUID())
	defer func() { tools.RemoveFileIfExists(temporaryPath) }()

	if err := writeArchive(ctx, temporaryPath, options, packVersion, declaredByPath, cfFilesByPath, cfMatched, payloadFiles, &totalOverrides, &warnings, phase, progress); err != nil {
		return result, err
	}

	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return result, err
	}

	if progress != nil {
		progress(ModpackExportProgress{"完成", len(payloadFiles), len(payloadFiles)})
	}
	return ModpackExportResult{
		OutputPath:    outputPath,
		DeclaredFiles: declaredFiles,
		OverrideFiles: totalOverrides,
		Warnings:      warnings,
	}, nil
}

// payloadEntry 待写入压缩包的单个文件：包内路径 + 源文件绝对路径。
type payloadEntry struct {
	archivePath string
	sourcePath  string
}

// writeArchive 构建压缩包主体：index / manifest → 图标 → overrides。
func writeArchive(
	ctx context.Context,
	temporaryPath string,
	options ModpackExportOptions,
	packVersion string,
	declaredByPath map[string]ModrinthFileMatch,
	cfFilesByPath map[string]curseForgeFileRef,
	cfMatched []curseForgeFingerprintMatch,
	payloadFiles []payloadEntry,
	totalOverrides *int,
	warnings *[]string,
	phase string,
	progress func(ModpackExportProgress),
) (err error) {
	file, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	archive := zip.NewWriter(file)
	// 中央目录在 Close 时才写：磁盘写满/配额不足只在这里暴露。错误必须带出去，
	// 否则调用方会把截断的整合包改名成目标文件、报成功。
	defer func() {
		if closeErr := archive.Close(); err == nil {
			err = closeErr
		}
	}()

	switch options.Format {
	case FormatModrinth:
		indexJSON, err := marshalModpackJSON(buildModrinthIndex(options, packVersion, declaredByPath))
		if err != nil {
			return err
		}
		if err := writeTextEntry(archive, "modrinth.index.json", indexJSON); err != nil {
			return err
		}
	case FormatCurseForge:
		manifestJSON, err := marshalModpackJSON(buildCurseForgeManifest(options, packVersion, cfFilesByPath))
		if err != nil {
			return err
		}
		if err := writeTextEntry(archive, "manifest.json", manifestJSON); err != nil {
			return err
		}
		if err := writeTextEntry(archive, "modlist.html", buildCurseForgeModlist(cfMatched)); err != nil {
			return err
		}
	default:
		componentsJSON, err := marshalModpackJSON(buildMultiMcComponents(options, packVersion))
		if err != nil {
			return err
		}
		if err := writeTextEntry(archive, "mmc-pack.json", componentsJSON); err != nil {
			return err
		}
		if err := writeTextEntry(archive, "instance.cfg", buildInstanceCfg(options, packVersion)); err != nil {
			return err
		}
	}

	if strings.TrimSpace(options.IconPngPath) != "" && tools.FileExists(options.IconPngPath) {
		// Modrinth/CurseForge 规范不含图标：写入 overrides/icon.png（不干扰游戏）；
		// MultiMC 的图标约定为包根 icon.png
		iconEntry := "icon.png"
		if options.Format != FormatMultiMc {
			iconEntry = "overrides/icon.png"
		}
		if err := copyFileEntry(ctx, archive, iconEntry, options.IconPngPath); err != nil {
			return err
		}
	}

	// 实例文件在包内的根目录：Modrinth 用 overrides/（mrpack 规范），
	// MultiMC/Prism 用 .minecraft/（其导入器与多实例布局都按此查找）。
	overrideRoot := "overrides"
	if options.Format == FormatMultiMc {
		overrideRoot = ".minecraft"
	}

	for index, payload := range payloadFiles {
		if err := ctx.Err(); err != nil {
			return err
		}
		if progress != nil {
			progress(ModpackExportProgress{phase, index, len(payloadFiles)})
		}

		normalized := filepath.ToSlash(payload.archivePath)
		if _, declared := declaredByPath[normalized]; declared {
			continue // 已声明为 Modrinth 直链文件，不再重复进 overrides
		}
		if _, declaredCF := cfFilesByPath[strings.ToLower(normalized)]; declaredCF {
			continue // 已声明为 CurseForge files 条目，不再重复进 overrides
		}

		// overrides/ 是 mrpack 规范的约定；MultiMC/Prism 的多实例包把游戏文件
		// 放在 .minecraft/ 下。两者不能混用：本仓库自己的外部实例解析
		// （instance.resolveExternalContentDirectory）认的是 minecraft/.minecraft，
		// 把内容塞进 overrides/ 只会让解压后的实例"看起来有 mods、实际一个都读不到"。
		entryPath := filepath.ToSlash(filepath.Join(overrideRoot, payload.archivePath))
		if err := copyFileEntry(ctx, archive, entryPath, payload.sourcePath); err != nil {
			if ctx.Err() != nil {
				return err
			}
			*warnings = append(*warnings, fmt.Sprintf("跳过 %s：%v", payload.archivePath, err))
			continue
		}
		*totalOverrides++
	}
	return nil
}

// ---------------------------------------------------------------------------
// Modrinth index 构建
// ---------------------------------------------------------------------------

// modrinthIndex modrinth.index.json 根对象（字段名与 C# 序列化输出一致）。
type modrinthIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionId     string            `json:"versionId"`
	Name          string            `json:"name"`
	Summary       *string           `json:"summary,omitempty"`
	Files         []modrinthFile    `json:"files"`
	Dependencies  map[string]string `json:"dependencies"`
	NyaLauncher   *modrinthNyaMeta  `json:"nyaLauncher,omitempty"`
}

type modrinthNyaMeta struct {
	Author     *string `json:"author,omitempty"`
	UpdateLink *string `json:"updateLink,omitempty"`
}

type modrinthFile struct {
	Path       string            `json:"path"`
	Hashes     modrinthHashes    `json:"hashes"`
	Env        modrinthEnv       `json:"env"`
	Downloads  []string          `json:"downloads"`
	FileSize   int64             `json:"fileSize"`
}

type modrinthHashes struct {
	Sha1   *string `json:"sha1,omitempty"`
	Sha512 *string `json:"sha512,omitempty"`
}

type modrinthEnv struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

// buildModrinthIndex 组装 modrinth.index.json。
func buildModrinthIndex(options ModpackExportOptions, packVersion string, declaredByPath map[string]ModrinthFileMatch) modrinthIndex {
	keys := make([]string, 0, len(declaredByPath))
	for key := range declaredByPath {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return strings.ToLower(keys[i]) < strings.ToLower(keys[j]) })

	files := make([]modrinthFile, 0, len(keys))
	for _, key := range keys {
		match := declaredByPath[key]
		sha512 := match.Sha512
		sha1Value := match.Sha1
		var sha512Ptr *string
		if sha512 != "" {
			sha512Ptr = &sha512
		}
		files = append(files, modrinthFile{
			Path: key,
			Hashes: modrinthHashes{
				Sha1:   &sha1Value,
				Sha512: sha512Ptr,
			},
			Env:       modrinthEnv{Client: "required", Server: "required"},
			Downloads: []string{match.DownloadUrl},
			FileSize:  match.SizeBytes,
		})
	}

	dependencies := map[string]string{"minecraft": options.MinecraftVersion}
	if loaderKey := mapModrinthLoaderKey(options.LoaderName); loaderKey != "" && strings.TrimSpace(options.LoaderVersion) != "" {
		dependencies[loaderKey] = options.LoaderVersion
	}

	summary := options.Description
	index := modrinthIndex{
		FormatVersion: 1,
		Game:          "minecraft",
		VersionId:     packVersion,
		Name:          options.PackName,
		Summary:       &summary,
		Files:         files,
		Dependencies:  dependencies,
	}
	if options.Author == "" && options.UpdateLink == "" {
		return index
	}
	// 非规范字段：Modrinth 格式本身不承载作者/链接，导入方会忽略未知键；
	// 这里命名空间化保存，供支持该字段的启动器（含本启动器）回读
	author := options.Author
	updateLink := options.UpdateLink
	meta := &modrinthNyaMeta{}
	if author != "" {
		meta.Author = &author
	}
	if updateLink != "" {
		meta.UpdateLink = &updateLink
	}
	index.NyaLauncher = meta
	return index
}

// mapModrinthLoaderKey 加载器名 → Modrinth dependencies 键。
func mapModrinthLoaderKey(loaderName string) string {
	switch strings.ToLower(strings.TrimSpace(loaderName)) {
	case "fabric":
		return "fabric-loader"
	case "quilt":
		return "quilt-loader"
	case "forge":
		return "forge"
	case "neoforge":
		return "neoforge"
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// MultiMC 构建
// ---------------------------------------------------------------------------

// multiMcPack mmc-pack.json 根对象。
type multiMcPack struct {
	FormatVersion int                `json:"formatVersion"`
	Components    []multiMcComponent `json:"components"`
}

type multiMcComponent struct {
	CachedName    *string `json:"cachedName,omitempty"`
	CachedVersion *string `json:"cachedVersion,omitempty"`
	Important     *bool   `json:"important,omitempty"`
	Uid           string  `json:"uid"`
	Version       string  `json:"version"`
}

// buildMultiMcComponents 组装 mmc-pack.json 组件清单。
func buildMultiMcComponents(options ModpackExportOptions, packVersion string) multiMcPack {
	minecraft := options.MinecraftVersion
	components := []multiMcComponent{{
		CachedName:    strPtr("Minecraft"),
		CachedVersion: &minecraft,
		Important:     tools.BoolPtr(true),
		Uid:           "net.minecraft",
		Version:       minecraft,
	}}
	if uid, cachedName := mapMultiMcLoader(options.LoaderName); uid != "" && strings.TrimSpace(options.LoaderVersion) != "" {
		loaderVersion := options.LoaderVersion
		components = append(components, multiMcComponent{
			CachedName:    &cachedName,
			CachedVersion: &loaderVersion,
			Uid:           uid,
			Version:       loaderVersion,
		})
	}
	return multiMcPack{FormatVersion: 1, Components: components}
}

// mapMultiMcLoader 加载器名 → (uid, cachedName)。
func mapMultiMcLoader(loaderName string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(loaderName)) {
	case "fabric":
		return "net.fabricmc.fabric-loader", "Fabric Loader"
	case "quilt":
		return "org.quiltmc.quilt-loader", "Quilt Loader"
	case "forge":
		return "net.minecraftforge", "Forge"
	case "neoforge":
		return "net.neoforged.neoforge", "NeoForge"
	default:
		return "", ""
	}
}

// buildInstanceCfg 组装 MultiMC instance.cfg（properties 文本）。
func buildInstanceCfg(options ModpackExportOptions, packVersion string) string {
	var builder strings.Builder
	set := func(key, value string) {
		// instance.cfg 是 properties 文本：换行/反斜杠转义，避免破坏结构
		escaped := strings.NewReplacer("\\", "\\\\", "\r", "\\r", "\n", "\\n").Replace(value)
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(escaped)
		builder.WriteString("\n")
	}
	set("InstanceType", "OneSix")
	set("name", options.PackName)
	set("iconKey", "icon")
	set("lastLaunchTime", "0")
	set("TotalTimePlayed", "0")
	if strings.TrimSpace(packVersion) != "" {
		set("NyaLauncherPackVersion", packVersion)
	}
	if strings.TrimSpace(options.Author) != "" {
		set("NyaLauncherAuthor", options.Author)
	}
	if strings.TrimSpace(options.UpdateLink) != "" {
		set("NyaLauncherUpdateLink", options.UpdateLink)
	}
	if strings.TrimSpace(options.Description) != "" {
		set("NyaLauncherDescription", options.Description)
	}
	return builder.String()
}

// ---------------------------------------------------------------------------
// Modrinth version_file 哈希查询
// ---------------------------------------------------------------------------

const modrinthVersionFileURL = "https://api.modrinth.com/v2/version_file/%s?algorithm=sha1"

// modrinthVersionFilePayload version_file 端点响应。
type modrinthVersionFilePayload struct {
	URL      string         `json:"url"`
	Filename string         `json:"filename"`
	Size     int64          `json:"size"`
	Hashes   modrinthHashes `json:"hashes"`
}

// resolveModrinthFile 按 sha1 查询 Modrinth 的版本文件（返回 nil = 未收录）。
func resolveModrinthFile(ctx context.Context, sha1Hex string) (*ModrinthFileMatch, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(modrinthVersionFileURL, sha1Hex), nil)
	if err != nil {
		return nil, err
	}
	response, err := tools.SharedHTTPClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil
	}
	var payload modrinthVersionFilePayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.URL == "" {
		return nil, nil
	}
	sha512 := payload.Hashes.Sha512
	match := &ModrinthFileMatch{
		FileName:    payload.Filename,
		DownloadUrl: payload.URL,
		Sha1:        sha1Hex,
		SizeBytes:   payload.Size,
	}
	if payload.Hashes.Sha1 != nil && *payload.Hashes.Sha1 != "" {
		match.Sha1 = *payload.Hashes.Sha1
	}
	if sha512 != nil {
		match.Sha512 = *sha512
	}
	return match, nil
}

// ---------------------------------------------------------------------------
// JSON 序列化辅助
// ---------------------------------------------------------------------------

// marshalModpackJSON 输出两空格缩进、不转义 HTML 字符的 JSON
// （对应 C# UnsafeRelaxedJsonEscaping + WriteIndented）。
func marshalModpackJSON(value any) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimRight(buffer.String(), "\n"), nil
}

func strPtr(value string) *string { return &value }

// ---------------------------------------------------------------------------
// 文件/哈希工具
// ---------------------------------------------------------------------------

// computeFileSHA1 计算文件 SHA-1（小写十六进制）。
func computeFileSHA1(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha1.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// toArchivePath 目录条目展开后的包内路径。
func toArchivePath(itemRelativePath, fileFullPath, contentDirectory string) string {
	relative, err := filepath.Rel(contentDirectory, fileFullPath)
	if err != nil {
		relative = filepath.Base(fileFullPath)
	}
	relative = filepath.ToSlash(relative)
	prefix := strings.TrimSuffix(filepath.ToSlash(itemRelativePath), "/")
	// 目录条目展开后的相对路径一定以条目前缀开头；防御性兜底，避免条目重名错位
	if len(relative) > len(prefix) && strings.EqualFold(relative[:len(prefix)], prefix) && relative[len(prefix)] == '/' {
		return relative
	}
	return prefix + "/" + filepath.ToSlash(filepath.Base(fileFullPath))
}

// writeTextEntry 写入文本条目（UTF-8 无 BOM，Deflate 压缩）。
func writeTextEntry(archive *zip.Writer, entryName, content string) error {
	header := &zip.FileHeader{Name: entryName, Method: zip.Deflate}
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.WriteString(writer, content)
	return err
}

// copyFileEntry 先打开源文件再创建条目：源不可读（被占用等）时不会在包里留下空条目。
func copyFileEntry(ctx context.Context, archive *zip.Writer, entryName, sourcePath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = entryName
	header.Method = zip.Deflate
	entryWriter, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = io.Copy(entryWriter, source)
	return err
}

// enumerateSafe 递归枚举，单个子目录不可读/被占用时跳过而不是中断整棵遍历。
// 注意不能用 filepath.WalkDir + 外层 try/catch 语义：Walk 遇错即终止整棵遍历。
func enumerateSafe(directory string) []string {
	var result []string
	safeWalk(directory, &result)
	return result
}

func safeWalk(directory string, result *[]string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			safeWalk(path, result)
			continue
		}
		*result = append(*result, path)
	}
}

// enumerateTopLevel 枚举目录的子目录与文件（读不到的忽略），按名称排序。
func enumerateTopLevel(directory string) []string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		result = append(result, filepath.Join(directory, entry.Name()))
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

// directorySize 递归统计目录大小；读不到的文件按 0 计。
func directorySize(directory string) int64 {
	var total int64
	for _, path := range enumerateSafe(directory) {
		if info, err := os.Stat(path); err == nil {
			total += info.Size()
		}
	}
	return total
}

// newModpackGUID 生成不带连字符的小写 GUID。
func newModpackGUID() string {
	b := make([]byte, 16)
	_, _ = crand.Read(b)
	return fmt.Sprintf("%x", b)
}
