package content

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/tools"
)

// ---------------------------------------------------------------------------
// 公开入口
// ---------------------------------------------------------------------------

// ReadMods 读取目录下全部 mod（*.jar 与 *.jar.disabled），按名称排序。
func ReadMods(ctx context.Context, directory string) []GameContentEntry {
	paths := append(
		enumerateFiles(directory, ".jar"),
		enumerateFiles(directory, ".jar.disabled")...)
	entries := make([]GameContentEntry, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, readCached(path, func() GameContentEntry {
			return readMod(ctx, path)
		}))
	}
	sortEntries(entries)
	return entries
}

// ReadResourcePacks 读取资源包（zip 或目录），按名称排序。
func ReadResourcePacks(ctx context.Context, directory string) []GameContentEntry {
	entries := make([]GameContentEntry, 0)
	for _, path := range enumerateArchivesAndDirectories(directory) {
		entries = append(entries, readCached(path, func() GameContentEntry {
			return readPack(ctx, path, "▣")
		}))
	}
	sortEntries(entries)
	return entries
}

// ReadShaders 读取光影包（zip 或目录），按名称排序。
func ReadShaders(ctx context.Context, directory string) []GameContentEntry {
	entries := make([]GameContentEntry, 0)
	for _, path := range enumerateArchivesAndDirectories(directory) {
		entries = append(entries, readCached(path, func() GameContentEntry {
			return readPack(ctx, path, "✦")
		}))
	}
	sortEntries(entries)
	return entries
}

// ReadSaves 读取存档目录列表（每个子目录一个条目），按名称排序。
func ReadSaves(ctx context.Context, directory string) []GameContentEntry {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return []GameContentEntry{}
	}
	result := make([]GameContentEntry, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := ctx.Err(); err != nil {
			break
		}
		result = append(result, readSave(filepath.Join(directory, entry.Name())))
	}
	sortEntries(result)
	return result
}

// ResolveInstanceVisual 解析实例的展示图标。优先级：显式内置图标偏好 →
// profile 自定义图标偏好 → 实例目录自带图标 → 按加载器默认。
// 前置判断 gameicon: 直接返回，无需触碰磁盘。
func ResolveInstanceVisual(snapshot InstanceContext, versionID, loaderName string) GameInstanceVisual {
	instanceDirectory := filepath.Join(snapshot.MinecraftDirectory, "versions", versionID)
	launcherRoot := ""
	if ExternalInstanceResolver != nil {
		if external, ok := ExternalInstanceResolver(snapshot.SourcePath); ok &&
			strings.EqualFold(external.InstanceId, versionID) {
			instanceDirectory = external.InstanceDirectory
			launcherRoot = external.LauncherRoot
		}
	}

	profileOverride := config.GetInstanceIconOverride(snapshot.MinecraftDirectory, versionID)
	if profileOverride != nil && strings.HasPrefix(*profileOverride, "gameicon:") {
		return GameInstanceVisual{*profileOverride, loaderGlyph(loaderName)}
	}

	// 自定义图标偏好（"custom"）优先读取用户手动设置的图标文件，其次实例目录自带图标
	useCustom := *configGetInstanceIconOverrideText(snapshot.MinecraftDirectory, versionID) == "custom"
	icon := ""
	if useCustom {
		icon = GetCustomIconPath(snapshot.MinecraftDirectory, versionID)
	}
	if icon == "" {
		icon = findInstanceIcon(instanceDirectory, launcherRoot)
	}
	if loaderName == "" {
		loaderName = detectLoaderFromMetadata(snapshot.MinecraftDirectory, instanceDirectory, versionID)
	}
	if icon == "" {
		icon = defaultInstanceIconPath(loaderName)
	}
	return GameInstanceVisual{icon, loaderGlyph(loaderName)}
}

// configGetInstanceIconOverrideText 返回图标偏好文本（nil 时返回指向空串的指针）。
func configGetInstanceIconOverrideText(minecraftDirectory, versionID string) *string {
	if value := config.GetInstanceIconOverride(minecraftDirectory, versionID); value != nil {
		return value
	}
	empty := ""
	return &empty
}

// ---------------------------------------------------------------------------
// 结果缓存
// ---------------------------------------------------------------------------

var (
	// entryCache 条目解析结果缓存：键含文件大小与修改时间，文件更新后自动失效；
	// 目录包的键由调用方构造，不含时间戳（目录 mtime 不反映内容变化，宁可重新解析）。
	entryCacheMu sync.Mutex
	entryCache   = map[string]GameContentEntry{}
)

// readCached 按（路径, 大小, 修改时间）缓存条目解析结果。
func readCached(path string, read func() GameContentEntry) GameContentEntry {
	key := buildCacheKey(path)
	if key == "" {
		return read()
	}
	entryCacheMu.Lock()
	if cached, ok := entryCache[key]; ok {
		entryCacheMu.Unlock()
		return cached
	}
	entryCacheMu.Unlock()

	entry := read()
	entryCacheMu.Lock()
	if len(entryCache) >= maximumEntryCacheCount {
		entryCache = map[string]GameContentEntry{}
	}
	entryCache[key] = entry
	entryCacheMu.Unlock()
	return entry
}

// buildCacheKey 构造（路径, 大小, 修改时间）缓存键；文件不可读时返回空串（不缓存）。
func buildCacheKey(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano())
}

// ---------------------------------------------------------------------------
// Mod 解析（fabric / quilt / toml / 旧版 mcmod.info）
// ---------------------------------------------------------------------------

// readMod 解析单个 mod 压缩包；解析失败降级为未知条目（PCL/HMCL 对此类损坏同样宽容）。
func readMod(ctx context.Context, path string) GameContentEntry {
	if err := ctx.Err(); err != nil {
		return unknownEntry(filepath.Base(path), "◆", path)
	}
	fallbackName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	archive, err := zip.OpenReader(path)
	if err != nil {
		return unknownEntry(fallbackName, "◆", path)
	}
	defer archive.Close()

	// 按加载器元数据文件的优先级逐个探测，找到即解析
	if entry := findEntry(&archive.Reader, "fabric.mod.json"); entry != nil {
		return readFabricMod(path, &archive.Reader, entry, fallbackName)
	}
	if entry := findEntry(&archive.Reader, "quilt.mod.json"); entry != nil {
		return readQuiltMod(path, &archive.Reader, entry, fallbackName)
	}
	toml := findEntry(&archive.Reader, "META-INF/neoforge.mods.toml")
	if toml == nil {
		toml = findEntry(&archive.Reader, "META-INF/mods.toml")
	}
	if toml != nil {
		return readTomlMod(path, &archive.Reader, toml, fallbackName)
	}
	if entry := findEntry(&archive.Reader, "mcmod.info"); entry != nil {
		return readLegacyMod(path, &archive.Reader, entry, fallbackName)
	}
	return unknownEntry(fallbackName, "◆", path)
}

// readFabricMod 解析 fabric.mod.json。
func readFabricMod(sourcePath string, archive *zip.Reader, metadata *zip.File, fallbackName string) GameContentEntry {
	root, err := readEntryObject(metadata)
	if err != nil {
		return unknownEntry(fallbackName, "◆", sourcePath)
	}
	name := firstNonEmpty(readJSONString(root, "name"), readJSONString(root, "id"), fallbackName)
	version := firstNonEmpty(readJSONString(root, "version"), "未提供")
	authors := readPeople(root, "authors")
	description := readDescription(root, "description")
	iconEntry := readIconProperty(root, "icon")
	return createEntry(sourcePath, archive, name, authors, version, description, iconEntry, "◆")
}

// readQuiltMod 解析 quilt.mod.json（结构比 fabric 多两层包装：quilt_loader → metadata）。
func readQuiltMod(sourcePath string, archive *zip.Reader, metadata *zip.File, fallbackName string) GameContentEntry {
	root, err := readEntryObject(metadata)
	if err != nil {
		return unknownEntry(fallbackName, "◆", sourcePath)
	}
	loader, ok := root["quilt_loader"].(map[string]any)
	if !ok {
		loader = root
	}
	metadataRoot, ok := loader["metadata"].(map[string]any)
	if !ok {
		metadataRoot = loader
	}
	name := firstNonEmpty(readJSONString(metadataRoot, "name"), readJSONString(loader, "id"), fallbackName)
	version := firstNonEmpty(readJSONString(loader, "version"), "未提供")
	authors := readPeople(metadataRoot, "contributors")
	description := readDescription(metadataRoot, "description")
	iconEntry := readIconProperty(metadataRoot, "icon")
	return createEntry(sourcePath, archive, name, authors, version, description, iconEntry, "◆")
}

// readTomlMod 解析 META-INF/(neoforge.)mods.toml。
func readTomlMod(sourcePath string, archive *zip.Reader, metadata *zip.File, fallbackName string) GameContentEntry {
	text, err := readEntryText(metadata)
	if err != nil {
		return unknownEntry(fallbackName, "◆", sourcePath)
	}
	modID := readTomlValue(text, "modId")
	if isTemplateValue(modID) {
		modID = ""
	}
	name := readTomlValue(text, "displayName")
	if isTemplateValue(name) {
		name = modID
	}
	if strings.TrimSpace(name) == "" {
		name = fallbackName
	}
	// 版本是模板占位符（未构建时原样保留 ${version}）时回退 MANIFEST 里的实现版本
	version := readTomlValue(text, "version")
	if isTemplateValue(version) {
		version = firstNonEmpty(
			readManifestValue(archive, "Implementation-Version"),
			readManifestValue(archive, "Specification-Version"))
	}
	if strings.TrimSpace(version) == "" {
		version = "未提供"
	}
	authors := readTomlValue(text, "authors")
	if isTemplateValue(authors) {
		authors = ""
	}
	if strings.TrimSpace(authors) == "" {
		authors = "未提供"
	}
	description := readTomlValue(text, "description")
	if isTemplateValue(description) {
		description = ""
	}
	logo := readTomlValue(text, "logoFile")
	return createEntry(sourcePath, archive, name, authors, version, description, logo, "◆")
}

// readLegacyMod 解析旧版 mcmod.info。
func readLegacyMod(sourcePath string, archive *zip.Reader, metadata *zip.File, fallbackName string) GameContentEntry {
	document, err := readEntryJSON(metadata)
	if err != nil {
		return unknownEntry(fallbackName, "◆", sourcePath)
	}
	// mcmod.info 顶层是数组（单 Mod 也是数组包一个对象）
	var obj map[string]any
	switch root := document.(type) {
	case []any:
		if len(root) > 0 {
			obj, _ = root[0].(map[string]any)
		}
	case map[string]any:
		obj = root
	}
	if obj == nil {
		obj = map[string]any{}
	}
	name := firstNonEmpty(readJSONString(obj, "name"), readJSONString(obj, "modid"), fallbackName)
	version := firstNonEmpty(readJSONString(obj, "version"), "未提供")
	authors := readPeople(obj, "authorList")
	description := readDescription(obj, "description")
	logo := readJSONString(obj, "logoFile")
	return createEntry(sourcePath, archive, name, authors, version, description, logo, "◆")
}

// ---------------------------------------------------------------------------
// 资源包 / 光影解析（zip 或目录，均以 pack.mcmeta 为准）
// ---------------------------------------------------------------------------

// readPack 解析资源包/光影条目；解析失败降级为未知条目。
func readPack(ctx context.Context, path, fallbackGlyph string) GameContentEntry {
	if err := ctx.Err(); err != nil {
		return unknownEntry(trimDisabledSuffix(filepath.Base(path)), fallbackGlyph, path)
	}
	// 禁用态文件名形如 x.zip.disabled：先剥 .disabled 再剥扩展名，
	// 否则列表里会显示成 "x.zip"（看着像没关掉）。
	baseName := trimDisabledSuffix(filepath.Base(path))
	fallbackName := strings.TrimSuffix(baseName, filepath.Ext(baseName))

	info, err := os.Stat(path)
	if err != nil {
		return unknownEntry(fallbackName, fallbackGlyph, path)
	}
	if info.IsDir() {
		return readDirectoryPack(path, fallbackName, fallbackGlyph)
	}
	return readZipPack(path, fallbackName, fallbackGlyph)
}

// readDirectoryPack 目录形态的资源包/光影：读 pack.mcmeta，图标取包内第一个存在的图片文件。
func readDirectoryPack(path, fallbackName, fallbackGlyph string) GameContentEntry {
	metadataPath := filepath.Join(path, "pack.mcmeta")
	directoryIcon := findFirstExisting(path, "pack.png", "icon.png", "preview.png")
	if !tools.FileExists(metadataPath) {
		return GameContentEntry{
			Name:          fallbackName,
			MetadataLine:  "作者 未提供 · 版本 未提供",
			Description:   path,
			IconPath:      directoryIcon,
			FallbackGlyph: fallbackGlyph,
			SourcePath:    path,
		}
	}
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return unknownEntry(fallbackName, fallbackGlyph, path)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return unknownEntry(fallbackName, fallbackGlyph, path)
	}
	root, _ := document.(map[string]any)
	if root == nil {
		root = map[string]any{}
	}
	return readPackDocument(root, fallbackName, path, directoryIcon, fallbackGlyph, path)
}

// readZipPack zip 形态的资源包/光影：从压缩包内提取 pack.mcmeta 与图标。
func readZipPack(path, fallbackName, fallbackGlyph string) GameContentEntry {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return unknownEntry(fallbackName, fallbackGlyph, path)
	}
	defer archive.Close()

	metadata := findEntry(&archive.Reader, "pack.mcmeta")
	var iconEntry *zip.File
	if iconEntry = findEntry(&archive.Reader, "pack.png"); iconEntry == nil {
		if iconEntry = findEntry(&archive.Reader, "icon.png"); iconEntry == nil {
			iconEntry = findEntry(&archive.Reader, "preview.png")
		}
	}
	icon := ""
	if iconEntry != nil {
		icon = extractIcon(path, iconEntry)
	}
	if metadata == nil {
		return GameContentEntry{
			Name:          fallbackName,
			MetadataLine:  "作者 未提供 · 版本 未提供",
			Description:   filepath.Base(path),
			IconPath:      icon,
			FallbackGlyph: fallbackGlyph,
			SourcePath:    path,
		}
	}
	root, err := readEntryJSON(metadata)
	if err != nil {
		return unknownEntry(fallbackName, fallbackGlyph, path)
	}
	obj, _ := root.(map[string]any)
	if obj == nil {
		obj = map[string]any{}
	}
	return readPackDocument(obj, fallbackName, filepath.Base(path), icon, fallbackGlyph, path)
}

// readPackDocument 从 pack.mcmeta JSON 中提取展示信息。
func readPackDocument(root map[string]any, fallbackName, sourceLabel, icon, fallbackGlyph, sourcePath string) GameContentEntry {
	// pack.mcmeta 顶层是 { "pack": {...} }，但手写的可能直接展开
	pack, ok := root["pack"].(map[string]any)
	if !ok {
		pack = root
	}
	name := firstNonEmpty(readJSONString(root, "name"), readJSONString(pack, "name"), fallbackName)
	author := firstNonEmpty(readJSONString(root, "author"), readJSONString(pack, "author"), "未提供")
	version := firstNonEmpty(readJSONString(root, "version"), readJSONString(pack, "version"), "未提供")
	description := readDescription(pack, "description")
	if strings.TrimSpace(description) == "" {
		description = sourceLabel
	}
	return GameContentEntry{
		Name:          name,
		MetadataLine:  fmt.Sprintf("作者 %s · 版本 %s", author, version),
		Description:   description,
		IconPath:      icon,
		FallbackGlyph: fallbackGlyph,
		SourcePath:    sourcePath,
	}
}

// ---------------------------------------------------------------------------
// 存档解析（level.dat = gzip + NBT）
// ---------------------------------------------------------------------------

// readSave 解析单个存档目录的展示信息。
func readSave(path string) GameContentEntry {
	folderName := filepath.Base(path)
	name := folderName
	gameVersion := "未提供"
	var lastPlayed *time.Time

	levelDat := filepath.Join(path, "level.dat")
	if tools.FileExists(levelDat) {
		if values, err := readLevelDat(levelDat); err == nil {
			if strings.TrimSpace(values.LevelName) != "" {
				name = values.LevelName
			}
			if strings.TrimSpace(values.GameVersion) != "" {
				gameVersion = values.GameVersion
			}
			lastPlayed = values.LastPlayed
		}
	}

	created := time.Time{}
	if info, err := os.Stat(path); err == nil {
		// 目录被删除/权限不足时用占位时间，避免整个存档扫描中断
		created = info.ModTime()
	}
	icon := findFirstExisting(path, "icon.png")

	description := fmt.Sprintf("存档文件夹：%s", folderName)
	if lastPlayed != nil {
		description = fmt.Sprintf("最后游玩：%s · 存档文件夹：%s",
			lastPlayed.Format("2006-01-02 15:04"), folderName)
	}
	return GameContentEntry{
		Name:          name,
		MetadataLine:  fmt.Sprintf("创建日期 %s · Minecraft %s", created.Format("2006-01-02 15:04"), gameVersion),
		Description:   description,
		IconPath:      icon,
		FallbackGlyph: "material:Apps",
		SourcePath:    path,
	}
}

// ---------------------------------------------------------------------------
// 条目组装
// ---------------------------------------------------------------------------

// createEntry 组装 mod 条目并提取图标。
func createEntry(sourcePath string, archive *zip.Reader, name, authors, version, description, iconEntryName, fallbackGlyph string) GameContentEntry {
	icon := ""
	if strings.TrimSpace(iconEntryName) != "" {
		if entry := findEntry(archive, iconEntryName); entry != nil {
			icon = extractIcon(sourcePath, entry)
		}
	}
	descriptionText := strings.TrimSpace(description)
	if descriptionText == "" {
		descriptionText = filepath.Base(sourcePath)
	}
	return GameContentEntry{
		Name:          name,
		MetadataLine:  fmt.Sprintf("作者 %s · 版本 %s", normalizeDisplay(authors), normalizeDisplay(version)),
		Description:   descriptionText,
		IconPath:      icon,
		FallbackGlyph: fallbackGlyph,
		SourcePath:    sourcePath,
		IsDisabled:    isDisabledFile(sourcePath),
	}
}

// unknownEntry 解析失败时的降级条目。
func unknownEntry(name, glyph, sourcePath string) GameContentEntry {
	return GameContentEntry{
		Name:          name,
		MetadataLine:  "作者 未提供 · 版本 未提供",
		Description:   filepath.Base(sourcePath),
		FallbackGlyph: glyph,
		SourcePath:    sourcePath,
		IsDisabled:    isDisabledFile(sourcePath),
	}
}

func normalizeDisplay(value string) string {
	if strings.TrimSpace(value) == "" {
		return "未提供"
	}
	return strings.TrimSpace(value)
}

func isDisabledFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".disabled")
}

// trimDisabledSuffix 去掉禁用态后缀（展示用原始名字）。
func trimDisabledSuffix(name string) string {
	if !isDisabledFile(name) {
		return name
	}

	return name[:len(name)-len(".disabled")]
}

// SniffZipKind 通过压缩包内部结构判断拖入的 zip 是什么内容：
// "shaderpack"（含 shaders/ 目录）、"resourcepack"（pack.mcmeta / assets/）、
// "save"（level.dat 在根或第一层）、"unknown"（判不出来，交给用户选）。
// 打不开的压缩包按 unknown 处理，不报错——调用方随后会走兜底路径。
func SniffZipKind(path string) string {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return "unknown"
	}
	defer archive.Close()

	hasShaders := false
	hasPackMeta := false
	hasAssets := false
	hasLevelDat := false
	for _, entry := range archive.Reader.File {
		name := strings.TrimPrefix(filepath.ToSlash(entry.Name), "./")
		segments := strings.Split(strings.ToLower(name), "/")
		if len(segments) >= 2 && segments[0] == "shaders" {
			hasShaders = true
			continue
		}
		if len(segments) == 1 {
			if segments[0] == "pack.mcmeta" {
				hasPackMeta = true
			}
			if segments[0] == "level.dat" {
				hasLevelDat = true
			}
			continue
		}
		// 存档包常见结构是 <世界名>/level.dat
		if len(segments) == 2 && segments[1] == "level.dat" {
			hasLevelDat = true
			continue
		}
		if segments[0] == "assets" {
			hasAssets = true
		}
	}

	switch {
	case hasShaders:
		return "shaderpack"
	case hasPackMeta || hasAssets:
		return "resourcepack"
	case hasLevelDat:
		return "save"
	default:
		return "unknown"
	}
}
