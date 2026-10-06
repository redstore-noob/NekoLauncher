package content

// mod_conflict.go 跨实例 / 实例内 mod 冲突检测。
//
// 与 content_scan.go 的分工：扫描负责"这个 jar 是什么"（名称、图标、版本），
// 本文件负责"这些 jar 放在一起会怎样"——把每个 mod 的**声明式元数据**
// （mod id、依赖、提供的能力、不兼容声明、目标 MC 版本、加载器）提出来，
// 在**启动之前**回答三个用户真正会问的问题：
//
//   1. 缺前置：A 需要 B，但 mods 目录里没有 B；
//   2. 重复：同一个 mod id 出现了两个 jar（最常见的"游戏起不来"原因）；
//   3. 版本不匹配：mod 声明支持的 MC 版本 / 加载器与当前实例不一致。
//
// 设计取向：
//   - **只报确定的事实，不猜**。拿不到元数据的 jar 归入"未识别"，绝不当成冲突；
//     解析不出来时宁可漏报（误报会让用户删掉本来正常的 mod，代价高得多）。
//   - 结果按严重度排序：Error（一定起不来）→ Warning（可能有问题）→ Info。
//   - 纯只读，不修改任何文件；"一键处理"由调用方决定（前端引导用户操作）。

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ConflictSeverity 冲突严重度。
type ConflictSeverity string

const (
	// SeverityError 几乎必然导致启动失败。
	SeverityError ConflictSeverity = "error"
	// SeverityWarning 可能出问题，取决于运行时条件。
	SeverityWarning ConflictSeverity = "warning"
	// SeverityInfo 仅供参考。
	SeverityInfo ConflictSeverity = "info"
)

// ModConflict 一条冲突/诊断结论。
type ModConflict struct {
	// Kind 稳定标识，前端查词典：duplicate-mod / missing-dependency /
	// incompatible-declared / mc-version-mismatch / loader-mismatch / unreadable。
	Kind string
	// Severity 严重度。
	Severity ConflictSeverity
	// Subject mod id 或文件名（哪一条内容出了问题）。
	Subject string
	// DisplayName 人类可读名称（拿不到时回落文件名）。
	DisplayName string
	// Files 涉及的文件名（重复 mod 会有多个；缺失依赖为空）。
	Files []string
	// Detail 运行期数值：缺失依赖名、声明的版本范围、实际 MC 版本等。
	// 与 Kind 配对，前端按 Kind 找模板后插值。
	Detail string
	// Related 相关方的 mod id（如"谁需要这个前置"），便于展示因果。
	Related []string
}

// ModDependency 一条依赖声明。
type ModDependency struct {
	// ModID 被依赖的 mod id（Fabric 的 depends 键 / Forge 的 modId）。
	ModID string
	// VersionRange 声明的版本范围原文（可能为空）。
	VersionRange string
	// Mandatory 是否强制依赖（Forge 的 mandatory=true；Fabric depends 恒为强制）。
	Mandatory bool
	// ProvidedBy 谁提出这条依赖（发起方的 mod id），便于展示因果。
	ProvidedBy string
}

// ModMetadata 从一个 mod jar 里提取出的声明式元数据。
type ModMetadata struct {
	// ModID 主 id（拿不到时为空串，此时该 jar 不参与依赖分析）。
	ModID string
	// Name 展示名。
	Name string
	// Version 版本。
	Version string
	// Loader "fabric" / "quilt" / "forge" / "neoforge" / "legacy" / ""（未识别）。
	Loader string
	// FileName jar 文件名（含扩展名）。
	FileName string
	// Depends 本 mod 依赖的其它 mod。
	Depends []ModDependency
	// Breaks 本 mod 声明**不兼容**的 mod id（Fabric 的 breaks / Forge 的 incompatible）。
	Breaks []string
	// Provides 本 mod 提供的额外能力 id（Fabric 的 provides）。
	Provides []string
	// DeclaredMCVersions mod 声明支持的 MC 版本（Fabric depends.minecraft）。
	DeclaredMCVersions string
}

// ModConflictReport 一次冲突检测的完整结论。
type ModConflictReport struct {
	Conflicts []ModConflict
	// Mods 参与分析的 mod（含未识别的，便于前端展示"我们看懂了几个"）。
	Mods []ModMetadata
	// AnalyzedMods / UnreadableMods 统计口径：前端用它说明"检测覆盖率"，
	// 避免用户以为"没报错"等于"绝对没问题"。
	AnalyzedMods   int
	UnreadableMods int
}

// HasBlockingConflict 是否存在 Error 级结论。
func (r ModConflictReport) HasBlockingConflict() bool {
	for _, conflict := range r.Conflicts {
		if conflict.Severity == SeverityError {
			return true
		}
	}
	return false
}

// modJarPattern 参与分析的 mod 文件：.jar 与其"已禁用"变体（.jar.disabled）。
var modJarExtensions = []string{".jar"}

// disabledFileSuffix mod 被禁用后的后缀（见 content_scan.go 的 isDisabledFile）。
const disabledFileSuffix = ".disabled"

// AnalyzeModConflicts 分析 mods 目录下的冲突。
//
// instanceMCVersion 当前实例的 MC 版本（可为空：为空时跳过版本匹配检查，
// 而不是把所有 mod 报成不匹配）；instanceLoader 当前加载器（"fabric"/"forge"/
// "neoforge"/"quilt"/"vanilla"，空串表示未知，同样跳过匹配检查）。
//
// 纯只读：只打开 zip 读元数据，不写任何文件。
func AnalyzeModConflicts(
	modsDirectory string,
	instanceMCVersion, instanceLoader string,
) ModConflictReport {
	report := ModConflictReport{
		Conflicts: []ModConflict{},
		Mods:      []ModMetadata{},
	}

	// 已禁用的 mod 不参与冲突分析：用户禁用某个 mod 正是为了消除冲突，
	// 把它们算进去会让"我明明禁用了它"变成永远消不掉的告警。
	metadatas := readEnabledModMetadata(modsDirectory)
	for _, metadata := range metadatas {
		if strings.TrimSpace(metadata.ModID) == "" {
			report.UnreadableMods++

			continue
		}
		report.AnalyzedMods++
	}
	report.Mods = metadatas

	report.Conflicts = append(report.Conflicts, detectDuplicateMods(metadatas)...)
	report.Conflicts = append(report.Conflicts, detectMissingDependencies(metadatas)...)
	report.Conflicts = append(report.Conflicts, detectDeclaredIncompatibilities(metadatas)...)
	report.Conflicts = append(report.Conflicts, detectVersionMismatches(
		metadatas, instanceMCVersion, instanceLoader)...)

	sortConflicts(report.Conflicts)
	return report
}

// sortConflicts 按严重度排序（error → warning → info），同severity 内按 subject 稳定排序，
// 保证同样的 mods 目录每次产出同样的顺序（前端列表不抖动）。
func sortConflicts(conflicts []ModConflict) {
	rank := func(severity ConflictSeverity) int {
		switch severity {
		case SeverityError:
			return 0
		case SeverityWarning:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(conflicts, func(left, right int) bool {
		leftRank, rightRank := rank(conflicts[left].Severity), rank(conflicts[right].Severity)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if conflicts[left].Kind != conflicts[right].Kind {
			return conflicts[left].Kind < conflicts[right].Kind
		}
		return conflicts[left].Subject < conflicts[right].Subject
	})
}

// ---------------------------------------------------------------------------
// 元数据读取
// ---------------------------------------------------------------------------

// readEnabledModMetadata 读取目录下所有**启用中**的 mod jar 元数据。
func readEnabledModMetadata(modsDirectory string) []ModMetadata {
	entries, err := os.ReadDir(modsDirectory)
	if err != nil {
		return []ModMetadata{}
	}

	result := make([]ModMetadata, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.EqualFold(filepath.Ext(name), ".jar") {
			continue
		}
		// .jar.disabled：已被用户禁用，不参与冲突检测
		if strings.HasSuffix(strings.ToLower(name), disabledFileSuffix) {
			continue
		}
		result = append(result, ReadModMetadata(filepath.Join(modsDirectory, name)))
	}
	return result
}

// ReadModMetadata 从一个 mod jar 提取声明式元数据。
// 任何解析失败都返回"只有文件名"的空元数据，绝不 panic、绝不返回 error
// （一个坏 jar 不能让整次体检失败）。
func ReadModMetadata(path string) ModMetadata {
	fileName := filepath.Base(path)
	fallback := ModMetadata{
		Name:     strings.TrimSuffix(fileName, filepath.Ext(fileName)),
		FileName: fileName,
	}

	archive, err := zip.OpenReader(path)
	if err != nil {
		return fallback
	}
	defer archive.Close()

	// 与扫描一致的优先级：fabric → quilt → (neo)forge → legacy
	if entry := findEntry(&archive.Reader, "fabric.mod.json"); entry != nil {
		return readFabricMetadata(path, &archive.Reader, entry, fallback)
	}
	if entry := findEntry(&archive.Reader, "quilt.mod.json"); entry != nil {
		return readQuiltMetadata(path, &archive.Reader, entry, fallback)
	}
	toml := findEntry(&archive.Reader, "META-INF/neoforge.mods.toml")
	loader := "neoforge"
	if toml == nil {
		toml = findEntry(&archive.Reader, "META-INF/mods.toml")
		loader = "forge"
	}
	if toml != nil {
		return readTomlMetadata(path, &archive.Reader, toml, loader, fallback)
	}
	if entry := findEntry(&archive.Reader, "mcmod.info"); entry != nil {
		metadata := readLegacyMetadata(path, &archive.Reader, entry, fallback)
		metadata.Loader = "legacy"

		return metadata
	}
	return fallback
}

// readFabricMetadata 解析 fabric.mod.json 的依赖信息。
func readFabricMetadata(
	sourcePath string,
	archive *zip.Reader,
	metadata *zip.File,
	fallback ModMetadata,
) ModMetadata {
	result := fallback
	root, err := readEntryObject(metadata)
	if err != nil {
		return result
	}
	result.Loader = "fabric"
	result.ModID = readJSONString(root, "id")
	if result.ModID == "" {
		// 没有 id 的 fabric mod 无法参与依赖分析，按未识别处理
		return ModMetadata{Name: result.Name, FileName: result.FileName}
	}
	result.Name = firstNonEmpty(readJSONString(root, "name"), result.ModID)
	result.Version = readJSONString(root, "version")

	// depends：{ "modid": "version-range-or-*" }，值可以是字符串或数组
	if depends, ok := root["depends"].(map[string]any); ok {
		for _, modID := range sortedKeys(depends) {
			// minecraft / java / fabricloader 不是 mod 依赖，单独处理
			if isEnvironmentDependency(modID) {
				if modID == "minecraft" {
					result.DeclaredMCVersions = describeDependencyValue(depends[modID])
				}

				continue
			}
			result.Depends = append(result.Depends, ModDependency{
				ModID:        modID,
				VersionRange: describeDependencyValue(depends[modID]),
				Mandatory:    true,
				ProvidedBy:   result.ModID,
			})
		}
	}
	if breaks, ok := root["breaks"].(map[string]any); ok {
		result.Breaks = append(result.Breaks, sortedKeys(breaks)...)
	}
	if provides, ok := root["provides"].([]any); ok {
		for _, item := range provides {
			if id, ok := item.(string); ok && strings.TrimSpace(id) != "" {
				result.Provides = append(result.Provides, id)
			}
		}
	}
	return result
}

// readTomlMetadata 解析 (neo)forge 的 mods.toml 依赖。
//
// Forge 的 TOML 依赖长这样：
//
//	[[dependencies.<modid>]]
//	    modId="fabric-api"
//	    mandatory=true
//	    versionRange="[1.0,)"
//
// 这里只用行级的轻量解析（与 content_scan.go 的 readTomlValue 同风格）：
// 逐行跟踪当前是否处在 [[dependencies.*]] 段内，再读该段里的 modId /
// mandatory / versionRange。刻意不引入 TOML 库——多一个依赖只为读三个字段
// 不划算，而且 Forge 的这部分结构非常固定。
func readTomlMetadata(
	sourcePath string,
	archive *zip.Reader,
	metadata *zip.File,
	loader string,
	fallback ModMetadata,
) ModMetadata {
	result := fallback
	result.Loader = loader
	text, err := readEntryText(metadata)
	if err != nil {
		return result
	}

	result.ModID = readTomlValue(text, "modId")
	if isTemplateValue(result.ModID) {
		result.ModID = ""
	}
	if result.ModID == "" {
		return ModMetadata{Name: result.Name, FileName: result.FileName}
	}
	name := readTomlValue(text, "displayName")
	if isTemplateValue(name) || strings.TrimSpace(name) == "" {
		name = result.ModID
	}
	result.Name = name
	version := readTomlValue(text, "version")
	if isTemplateValue(version) {
		version = firstNonEmpty(
			readManifestValue(archive, "Implementation-Version"),
			readManifestValue(archive, "Specification-Version"))
	}
	result.Version = version

	result.Depends = parseTomlDependencies(text, result.ModID)
	return result
}

// parseTomlDependencies 从 mods.toml 文本里解析 [[dependencies.<modid>]] 段。
func parseTomlDependencies(text, ownerModID string) []ModDependency {
	var dependencies []ModDependency
	inDependencySection := false

	// 解析一个段内累积的字段
	var currentID, currentRange string
	mandatory := true
	flush := func() {
		if !inDependencySection || strings.TrimSpace(currentID) == "" {
			currentID, currentRange, mandatory = "", "", true

			return
		}
		if !isEnvironmentDependency(currentID) && mandatory {
			dependencies = append(dependencies, ModDependency{
				ModID:        currentID,
				VersionRange: currentRange,
				Mandatory:    true,
				ProvidedBy:   ownerModID,
			})
		}
		currentID, currentRange, mandatory = "", "", true
	}

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[[") {
			// 新的段开始：先结算上一段
			flush()
			inDependencySection = strings.HasPrefix(trimmed, "[[dependencies.")
			continue
		}
		if !inDependencySection {
			continue
		}
		key, value, ok := splitTomlAssignment(trimmed)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "modid":
			currentID = value
		case "versionrange":
			currentRange = value
		case "mandatory":
			mandatory = !strings.EqualFold(value, "false")
		}
	}
	flush()
	return dependencies
}

// splitTomlAssignment 拆 "key = value" 并去掉两侧引号；空行/无等号返回 false。
func splitTomlAssignment(line string) (string, string, bool) {
	equals := strings.Index(line, "=")
	if equals <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:equals])
	value := strings.TrimSpace(line[equals+1:])
	value = strings.Trim(value, `"'`)
	if strings.TrimSpace(key) == "" {
		return "", "", false
	}
	return key, value, true
}

// readLegacyMetadata 解析旧版 mcmod.info（只有 id/name/version，没有依赖信息）。
func readLegacyMetadata(
	sourcePath string,
	archive *zip.Reader,
	metadata *zip.File,
	fallback ModMetadata,
) ModMetadata {
	result := fallback
	document, err := readEntryJSON(metadata)
	if err != nil {
		return result
	}
	var root map[string]any
	switch typed := document.(type) {
	case []any:
		if len(typed) > 0 {
			root, _ = typed[0].(map[string]any)
		}
	case map[string]any:
		root = typed
	}
	if root == nil {
		return result
	}
	modID := firstNonEmpty(readJSONString(root, "modid"), readJSONString(root, "modId"))
	if strings.TrimSpace(modID) == "" {
		return result
	}
	result.ModID = modID
	result.Name = firstNonEmpty(readJSONString(root, "name"), modID)
	result.Version = readJSONString(root, "version")
	return result
}

// readQuiltMetadata 解析 quilt.mod.json 的依赖信息（多两层包装）。
func readQuiltMetadata(
	sourcePath string,
	archive *zip.Reader,
	metadata *zip.File,
	fallback ModMetadata,
) ModMetadata {
	result := fallback
	root, err := readEntryObject(metadata)
	if err != nil {
		return result
	}
	loader, ok := root["quilt_loader"].(map[string]any)
	if !ok {
		loader = root
	}
	result.Loader = "quilt"
	result.ModID = readJSONString(loader, "id")
	if result.ModID == "" {
		return ModMetadata{Name: result.Name, FileName: result.FileName}
	}
	metadataRoot, ok := loader["metadata"].(map[string]any)
	if !ok {
		metadataRoot = loader
	}
	result.Name = firstNonEmpty(readJSONString(metadataRoot, "name"), result.ModID)
	result.Version = readJSONString(loader, "version")

	if depends, ok := loader["depends"].([]any); ok {
		for _, item := range depends {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			modID := readJSONString(entry, "id")
			if modID == "" || isEnvironmentDependency(modID) {
				if modID == "minecraft" {
					result.DeclaredMCVersions = readJSONString(entry, "versions")
				}

				continue
			}
			result.Depends = append(result.Depends, ModDependency{
				ModID:        modID,
				VersionRange: readJSONString(entry, "versions"),
				Mandatory:    !readJSONBool(entry, "optional"),
				ProvidedBy:   result.ModID,
			})
		}
	}
	return result
}

// isEnvironmentDependency 判断是否"不是 mod"的依赖项。
// minecraft / java / fabricloader / forge 由加载器自身满足，不该报"缺前置"。
func isEnvironmentDependency(modID string) bool {
	switch strings.ToLower(modID) {
	case "minecraft", "java", "fabricloader", "fabric-loader", "forge", "neoforge", "quilt_loader":
		return true
	}
	return false
}

// describeDependencyValue 把依赖值（字符串 / 数组 / 数字）渲染成可读文本。
func describeDependencyValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	case float64:
		// JSON 数字在 Go 里统一是 float64；"1.20" 这类版本号在 YAML/JSON 里
		// 可能被写成数字，去掉无意义的小数尾巴即可。
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	}
	return ""
}

// ---------------------------------------------------------------------------
// 检测规则
// ---------------------------------------------------------------------------

// detectDuplicateMods 同一个 mod id 出现多个 jar：加载器会拒绝启动。
func detectDuplicateMods(metadatas []ModMetadata) []ModConflict {
	byID := map[string][]ModMetadata{}
	for _, metadata := range metadatas {
		if metadata.ModID == "" {
			continue
		}
		byID[metadata.ModID] = append(byID[metadata.ModID], metadata)
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var conflicts []ModConflict
	for _, id := range ids {
		group := byID[id]
		if len(group) < 2 {
			continue
		}
		files := make([]string, 0, len(group))
		versions := make([]string, 0, len(group))
		for _, metadata := range group {
			files = append(files, metadata.FileName)
			if strings.TrimSpace(metadata.Version) != "" {
				versions = append(versions, metadata.Version)
			}
		}
		sort.Strings(files)
		conflicts = append(conflicts, ModConflict{
			Kind:        "duplicate-mod",
			Severity:    SeverityError,
			Subject:     id,
			DisplayName: group[0].Name,
			Files:       files,
			Detail:      strings.Join(versions, " / "),
		})
	}
	return conflicts
}

// detectMissingDependencies 声明了强制依赖但目录里没有任何 mod 提供它。
func detectMissingDependencies(metadatas []ModMetadata) []ModConflict {
	// 可用能力集合：mod id + provides 声明的额外 id
	available := map[string]bool{}
	for _, metadata := range metadatas {
		if metadata.ModID != "" {
			available[strings.ToLower(metadata.ModID)] = true
		}
		for _, provided := range metadata.Provides {
			available[strings.ToLower(provided)] = true
		}
	}

	// 按"缺失的依赖"聚合：同一个前置被多个 mod 需要时只报一条，列出全部需求方
	missing := map[string]*ModConflict{}
	for _, metadata := range metadatas {
		for _, dependency := range metadata.Depends {
			if !dependency.Mandatory {
				continue
			}
			key := strings.ToLower(dependency.ModID)
			if available[key] {
				continue
			}
			conflict, ok := missing[key]
			if !ok {
				conflict = &ModConflict{
					Kind:        "missing-dependency",
					Severity:    SeverityError,
					Subject:     dependency.ModID,
					DisplayName: dependency.ModID,
					Files:       []string{},
					Detail:      dependency.VersionRange,
					Related:     []string{},
				}
				missing[key] = conflict
			}
			conflict.Related = append(conflict.Related, dependency.ProvidedBy)
			conflict.Files = append(conflict.Files, metadata.FileName)
		}
	}

	keys := make([]string, 0, len(missing))
	for key := range missing {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	conflicts := make([]ModConflict, 0, len(keys))
	for _, key := range keys {
		conflict := missing[key]
		sort.Strings(conflict.Related)
		sort.Strings(conflict.Files)
		conflicts = append(conflicts, *conflict)
	}
	return conflicts
}

// detectDeclaredIncompatibilities 一方声明 breaks 另一方，但两者都在。
func detectDeclaredIncompatibilities(metadatas []ModMetadata) []ModConflict {
	present := map[string]bool{}
	for _, metadata := range metadatas {
		if metadata.ModID != "" {
			present[strings.ToLower(metadata.ModID)] = true
		}
	}

	var conflicts []ModConflict
	for _, metadata := range metadatas {
		for _, broken := range metadata.Breaks {
			key := strings.ToLower(broken)
			if !present[key] {
				continue
			}
			conflicts = append(conflicts, ModConflict{
				Kind:        "incompatible-declared",
				Severity:    SeverityError,
				Subject:     metadata.ModID,
				DisplayName: metadata.Name,
				Files:       []string{metadata.FileName},
				Detail:      broken,
				Related:     []string{broken},
			})
		}
	}
	return conflicts
}

// detectVersionMismatches mod 声明的 MC 版本 / 加载器与实例不一致。
func detectVersionMismatches(
	metadatas []ModMetadata,
	instanceMCVersion, instanceLoader string,
) []ModConflict {
	instanceMCVersion = strings.TrimSpace(instanceMCVersion)
	instanceLoader = strings.ToLower(strings.TrimSpace(instanceLoader))
	// 任一未知就跳过：宁可漏报，也不要把正常 mod 报成不匹配
	if instanceMCVersion == "" && instanceLoader == "" {
		return nil
	}

	var conflicts []ModConflict
	for _, metadata := range metadatas {
		if metadata.ModID == "" {
			continue
		}
		if instanceLoader != "" && metadata.Loader != "" && metadata.Loader != "legacy" {
			if !loaderMatches(metadata.Loader, instanceLoader) {
				conflicts = append(conflicts, ModConflict{
					Kind:        "loader-mismatch",
					Severity:    SeverityError,
					Subject:     metadata.ModID,
					DisplayName: metadata.Name,
					Files:       []string{metadata.FileName},
					Detail:      metadata.Loader + "|" + instanceLoader,
				})
			}
		}
		if instanceMCVersion != "" &&
			strings.TrimSpace(metadata.DeclaredMCVersions) != "" &&
			!versionRangeAllows(metadata.DeclaredMCVersions, instanceMCVersion) {
			conflicts = append(conflicts, ModConflict{
				Kind:        "mc-version-mismatch",
				Severity:    SeverityWarning,
				Subject:     metadata.ModID,
				DisplayName: metadata.Name,
				Files:       []string{metadata.FileName},
				Detail:      metadata.DeclaredMCVersions + "|" + instanceMCVersion,
			})
		}
	}
	return conflicts
}

// loaderMatches 加载器兼容判定。
// Quilt 兼容 Fabric mod（Quilt Loader 主动支持）；反向不成立。
func loaderMatches(modLoader, instanceLoader string) bool {
	if modLoader == instanceLoader {
		return true
	}
	if instanceLoader == "quilt" && modLoader == "fabric" {
		return true
	}
	return false
}

// versionRangeAllows 判断 MC 版本是否落在声明范围内。
//
// 只处理真实世界里最常见的几种写法，**刻意不做通用语义化版本求解**：
//   - "*" / 空 → 全部允许
//   - 精确版本  "1.20.1" → 相等
//   - 比较式    ">=1.20"  ">=1.20 <1.21"  "~1.20"
//   - Fabric 的 "1.20.x" 通配
//
// 无法理解的写法一律返回 true（不误报），这是本函数的核心约定。
func versionRangeAllows(declared, actual string) bool {
	declared = strings.TrimSpace(declared)
	if declared == "" || declared == "*" {
		return true
	}
	if declared == actual {
		return true
	}

	// 通配前缀："1.20.x" / "1.20.*"
	if strings.HasSuffix(declared, ".x") || strings.HasSuffix(declared, ".*") {
		prefix := declared[:len(declared)-2]
		return strings.HasPrefix(actual, prefix+".")
	}

	allowed := true
	recognized := false
	// 以空格分隔的多个条件取交集
	for _, clause := range strings.Fields(declared) {
		matched, ok := versionClauseAllows(clause, actual)
		if !ok {
			// 有无法理解的子句：整个声明视为"读不懂"，不误报
			return true
		}
		recognized = true
		allowed = allowed && matched
	}
	if !recognized {
		return true
	}
	return allowed
}

// versionClauseAllows 单个比较子句。
// 返回 (是否满足, 是否理解该写法)；不理解时第二个返回值为 false。
func versionClauseAllows(clause, actual string) (bool, bool) {
	for _, operator := range []string{">=", "<=", ">", "<", "~", "^"} {
		if !strings.HasPrefix(clause, operator) {
			continue
		}
		target := strings.TrimSpace(strings.TrimPrefix(clause, operator))
		if !looksLikeVersion(target) {
			// ">=" 后面不是版本号（例如 Maven 区间 "[1.19,1.21)"）：读不懂
			return false, false
		}
		comparison := compareVersions(actual, target)
		switch operator {
		case ">=":
			return comparison >= 0, true
		case "<=":
			return comparison <= 0, true
		case ">":
			return comparison > 0, true
		case "<":
			return comparison < 0, true
		case "~", "^":
			// ~1.20 → 1.20 系列；只比较主次版本
			return sameMajorMinor(actual, target), true
		}
	}
	// 无运算符：必须整体是个版本号，才按"精确版本"处理
	if !looksLikeVersion(clause) {
		// Maven 区间、纯文字等读不懂的写法：交给调用方按"不误报"放行
		return false, false
	}
	// "1.20" 与 "1.20.4" 视为同系列（作者常只写 major.minor）
	if strings.Count(clause, ".") == 1 {
		return strings.HasPrefix(actual, clause+".") || actual == clause, true
	}
	return clause == actual, true
}

// looksLikeVersion 判断文本是否像版本号：必须以数字开头，
// 且只含数字、点，以及常见的版本后缀字符（如 "1.20.1-pre1" 的连字符与字母）。
// 用它把 "[1.19,1.21)"、"garbage" 这类"读不懂"的写法挡在精确匹配之外。
func looksLikeVersion(text string) bool {
	if text == "" {
		return false
	}
	if text[0] < '0' || text[0] > '9' {
		return false
	}
	for _, character := range text {
		switch {
		case character >= '0' && character <= '9':
		case character == '.' || character == '-' || character == '_' || character == '+':
		case (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z'):
		default:
			// 逗号、方括号、空格等：不是版本号
			return false
		}
	}
	return true
}

// sameMajorMinor 主次版本是否一致（"1.20.1" 与 "1.20.4" → true）。
func sameMajorMinor(left, right string) bool {
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	// 只取前两段（major.minor）比较
	if len(leftParts) < 2 || len(rightParts) < 2 {
		return left == right
	}
	return leftParts[0] == rightParts[0] && leftParts[1] == rightParts[1]
}

// compareVersions 逐段比较版本号（"-1" 小于 / "0" 相等 / "1" 大于）。
// 非数字段按字符串比较；段数不同时短的补 0（"1.20" == "1.20.0"）。
func compareVersions(left, right string) int {
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	count := len(leftParts)
	if len(rightParts) > count {
		count = len(rightParts)
	}
	for index := 0; index < count; index++ {
		leftPart, rightPart := "0", "0"
		if index < len(leftParts) {
			leftPart = leftParts[index]
		}
		if index < len(rightParts) {
			rightPart = rightParts[index]
		}
		if leftPart == rightPart {
			continue
		}
		leftNumber, leftOk := parsePositiveInt(leftPart)
		rightNumber, rightOk := parsePositiveInt(rightPart)
		if leftOk && rightOk {
			if leftNumber < rightNumber {
				return -1
			}
			return 1
		}
		if leftPart < rightPart {
			return -1
		}
		return 1
	}
	return 0
}

// parsePositiveInt 解析非负整数；失败时第二个返回值为 false。
func parsePositiveInt(text string) (int, bool) {
	if text == "" {
		return 0, false
	}
	value := 0
	for _, character := range text {
		if character < '0' || character > '9' {
			return 0, false
		}
		value = value*10 + int(character-'0')
	}
	return value, true
}

// ResolveDuplicateMods 计算"要禁用哪些重复 mod 的 jar"。
//
// keepNewest=true 时每个 mod id 保留版本号最高的那个（用户最常见的场景是
// "装了新版忘了删旧版"）；否则保留文件名排序最后的那个（稳定且可预期）。
//
// **只计算、不执行**：返回应被禁用的绝对路径列表，由调用方决定何时改名。
// 这样"预览要动哪些文件"与"真的动"可以分开，用户能先看清楚再确认。
func ResolveDuplicateMods(modsDirectory string, keepNewest bool) ([]string, error) {
	metadatas := readEnabledModMetadata(modsDirectory)

	byID := map[string][]ModMetadata{}
	for _, metadata := range metadatas {
		if metadata.ModID == "" {
			continue
		}
		byID[metadata.ModID] = append(byID[metadata.ModID], metadata)
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var result []string
	for _, id := range ids {
		group := byID[id]
		if len(group) < 2 {
			continue
		}
		keep := pickModToKeep(group, keepNewest)
		for _, metadata := range group {
			if metadata.FileName == keep.FileName {
				continue
			}
			result = append(result, filepath.Join(modsDirectory, metadata.FileName))
		}
	}
	sort.Strings(result)
	return result, nil
}

// pickModToKeep 从一组同 id 的 mod 里选出保留哪个。
func pickModToKeep(group []ModMetadata, keepNewest bool) ModMetadata {
	keep := group[0]
	for _, candidate := range group[1:] {
		if keepNewest {
			// 版本号高的胜出；版本号相同或都读不出时退化为文件名比较
			comparison := compareVersions(candidate.Version, keep.Version)
			if comparison > 0 ||
				(comparison == 0 && candidate.FileName > keep.FileName) {
				keep = candidate
			}

			continue
		}
		if candidate.FileName > keep.FileName {
			keep = candidate
		}
	}
	return keep
}

// ---------------------------------------------------------------------------
// 小块工具
// ---------------------------------------------------------------------------

// sortedKeys 取 map 的键并排序（保证输出稳定）。
func sortedKeys(source map[string]any) []string {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
