// content_update_check.go
//
// X-4：实例内容（Mod / 资源包 / 光影包）的可更新检测 + 整合包清单比对。
//
// 两个核心事实决定了本文件的实现方式：
//
//  1. **没有 projectID，只有哈希**。实例目录里躺着的就是一个 .jar/.zip 文件，
//     想知道它是不是 Modrinth 上的东西，只能用 SHA-1 去 `version_files` 反查。
//     因此所有查询都是批量的（一次请求带多个哈希，见 modrinth.VersionFileLookupBatchSize）。
//
//  2. **「查不到」必须和「没有更新」分开**。Modrinth 上查不到这个哈希，只说明
//     「这个文件不是从 Modrinth 装的」，完全不能推出「它是最新版」——
//     CurseForge 独占的 Mod、自建包、手动改过的包全都查不到。把它们报成
//     「已是最新」就是撒谎。所以状态是四态：可更新 / 已是最新 / 未知 / 检查失败，
//     未知一律带可读的原因。
package download

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"nekolauncher/internal/config"
	"nekolauncher/internal/download/modrinth"
	"nekolauncher/internal/tools"
)

// ---------------------------------------------------------------------------
// 结果类型
// ---------------------------------------------------------------------------

// 内容检测状态。前端按这些字符串分支，不要改动字面量。
const (
	// ContentUpdateStatusUpdatable 查到了更新的版本。
	ContentUpdateStatusUpdatable = "updatable"
	// ContentUpdateStatusLatest 查到了归属，且当前就是最新版本。
	ContentUpdateStatusLatest = "latest"
	// ContentUpdateStatusUnknown 查不到归属（自建/手动放入/CurseForge 独占/未收录）。
	// **不是**「没有更新」。
	ContentUpdateStatusUnknown = "unknown"
	// ContentUpdateStatusCheckFailed 网络或服务端失败，本次没能得出结论。
	ContentUpdateStatusCheckFailed = "checkFailed"
	// ContentUpdateStatusSkipped 主动跳过（禁用态文件、目录形态的内容）。
	ContentUpdateStatusSkipped = "skipped"
)

// 内容类别（决定用哪个子目录与哪种加载器过滤）。
const (
	ContentUpdateKindMod          = "mod"
	ContentUpdateKindResourcePack = "resourcepack"
	ContentUpdateKindShaderPack   = "shaderpack"
)

// contentUpdateHashBatchSize 单次批量查询提交的哈希数上限，
// 必须与 modrinth 客户端的实现保持一致（用于向 UI 说明「几次请求」）。
const contentUpdateHashBatchSize = 100

// ContentUpdateFile 单个文件的可更新检测结果。
type ContentUpdateFile struct {
	// FilePath 磁盘上的绝对路径。
	FilePath string
	// FileName 文件名（含扩展名，不含 .disabled 后缀）。
	FileName string
	// Kind 内容类别：mod / resourcepack / shaderpack。
	Kind string
	// KindLabel 类别中文名（前端直接展示）。
	KindLabel string
	// Status 见 ContentUpdateStatus* 常量。
	Status string
	// StatusText 状态的中文说明（未知/失败时含具体原因）。
	StatusText string
	// SHA1 文件的 SHA-1（小写十六进制）；未计算时为空。
	SHA1 string
	// SizeBytes 文件大小（字节）。
	SizeBytes int64
	// SizeText 文件大小展示文本。
	SizeText string

	// ProjectID 匹配到的 Modrinth 项目 ID（未知时为空）。
	ProjectID string
	// ProjectName 匹配到的 Modrinth 项目名（未知时为空 = 不要编造）。
	ProjectName string
	// CurrentVersion 当前文件对应的版本号（未知时为空）。
	CurrentVersion string
	// CurrentVersionID 当前文件对应的版本 ID。
	CurrentVersionID string

	// LatestVersion 可更新时的最新版本号。
	LatestVersion string
	// LatestVersionID 可更新时的最新版本 ID。
	LatestVersionID string
	// LatestFileName 最新版本的文件名。
	LatestFileName string
	// DownloadURL 最新版本的下载地址。
	DownloadURL string
	// DownloadSizeBytes 最新版本文件大小（字节）。
	DownloadSizeBytes int64
	// DownloadSizeText 最新版本文件大小展示文本。
	DownloadSizeText string
	// ReleaseDate 最新版本发布日期（yyyy-MM-dd）。
	ReleaseDate string
	// CompatibleWithInstance 最新版本是否声明兼容当前实例的 MC 版本。
	CompatibleWithInstance bool
	// GameVersionsText 最新版本支持的 MC 版本（最多 3 个，逗号分隔）。
	GameVersionsText string
	// LoadersText 最新版本支持的加载器（逗号分隔）。
	LoadersText string
}

// ContentUpdateCheckResult 一个实例的内容更新检测结果。
type ContentUpdateCheckResult struct {
	// VersionID 实例 ID。
	VersionID string
	// ContentDirectory 实际检查的内容目录。
	ContentDirectory string
	// ModsDirectory / ResourcePacksDirectory / ShaderPacksDirectory 子目录路径。
	ModsDirectory          string
	ResourcePacksDirectory string
	ShaderPacksDirectory   string
	// GameVersion 实例的基础 MC 版本（用于兼容性判定，可能为空 = 未识别）。
	GameVersion string
	// LoaderName 实例加载器显示名（原版/Fabric/Forge/NeoForge/Quilt）。
	LoaderName string

	// Files 每个被检查文件的结果（可更新优先排前面）。
	Files []ContentUpdateFile

	// UpdatableCount 可更新数量。
	UpdatableCount int
	// LatestCount 已是最新数量。
	LatestCount int
	// UnknownCount 未知数量（Modrinth 未收录 / 非 Modrinth 来源）。
	UnknownCount int
	// FailedCount 检查失败数量（网络或服务端错误）。
	FailedCount int
	// SkippedCount 跳过数量（禁用态、目录形态）。
	SkippedCount int
	// CheckedFileCount 实际参与检测的文件数（= Files 长度）。
	CheckedFileCount int
	// DuplicateFileCount 与其它文件内容重复、未重复联网查询的文件数。
	DuplicateFileCount int
	// HashRequestCount 实际发出的 Modrinth 请求数（批量口径）。
	HashRequestCount int
	// HashBatchSize 单次批量查询的哈希数上限。
	HashBatchSize int

	// Modpack 整合包清单比对结果（没有清单时 Present = false）。
	Modpack ModpackVerifyResult
	// Notices 汇总性的中文提示（部分失败、整合包不可校验等）。
	Notices []string
}

// ---------------------------------------------------------------------------
// 整合包清单比对
// ---------------------------------------------------------------------------

// 整合包清单比对的判定结果。
const (
	// ModpackVerifyOK 所有声明的文件都存在且哈希一致。
	ModpackVerifyOK = "ok"
	// ModpackVerifyIssues 存在缺失或被改动的文件。
	ModpackVerifyIssues = "issues"
	// ModpackVerifyNoIndex 没找到清单，无法比对。
	ModpackVerifyNoIndex = "noIndex"
	// ModpackVerifyUnsupported CurseForge 格式：只有 projectID/fileID，离线无法校验。
	ModpackVerifyUnsupported = "unsupported"
	// ModpackVerifyFailed 清单存在但读取/解析失败。
	ModpackVerifyFailed = "failed"
)

// 清单里单个文件的比对状态。
const (
	// ModpackFileCurrent 哈希一致。
	ModpackFileCurrent = "current"
	// ModpackFileMissing 磁盘上找不到。
	ModpackFileMissing = "missing"
	// ModpackFileModified 存在但哈希不一致。
	ModpackFileModified = "modified"
	// ModpackFileUnverifiable 清单没给哈希（CurseForge 引用），无法比对。
	ModpackFileUnverifiable = "unverifiable"
)

// ModpackVerifyFile 清单里一个文件的比对结果。
type ModpackVerifyFile struct {
	// Path 清单声明的相对路径（正斜杠）。
	Path string
	// FileName 文件名。
	FileName string
	// Status 见 ModpackFile* 常量。
	Status string
	// StatusText 状态中文说明。
	StatusText string
	// DeclaredSHA1 清单声明的 SHA-1（可能为空）。
	DeclaredSHA1 string
	// ActualSHA1 磁盘上实际的 SHA-1（文件缺失时为空）。
	ActualSHA1 string
	// Required 清单是否标为必需。
	Required bool
}

// ModpackVerifyResult 整合包清单比对结果。
type ModpackVerifyResult struct {
	// Present 是否找到了清单（含 CurseForge 的 manifest.json）。
	Present bool
	// Format 清单格式：modrinth / curseforge / 空串。
	Format string
	// IndexPath 清单文件的实际路径（来自实例目录或上次安装时的快照）。
	IndexPath string
	// Source 清单来源说明（中文，如「实例内容目录」）。
	Source string
	// Status 见 ModpackVerify* 常量。
	Status string
	// StatusText 状态中文说明。
	StatusText string
	// PackName 整合包名（清单里有则填）。
	PackName string
	// PackVersion 整合包版本。
	PackVersion string
	// TotalFiles 清单声明的文件总数。
	TotalFiles int
	// CurrentCount 哈希一致的数量。
	CurrentCount int
	// MissingCount 缺失数量。
	MissingCount int
	// ModifiedCount 被改动的数量。
	ModifiedCount int
	// UnverifiableCount 清单没给哈希、无法比对的数量。
	UnverifiableCount int
	// Files 明细（缺失/改动优先排在前面，便于前端直接展示）。
	Files []ModpackVerifyFile
}

// ---------------------------------------------------------------------------
// 钩子与常量
// ---------------------------------------------------------------------------

// ContentUpdateProgressFunc 进度回调：(阶段 key, 中文提示)。可为 nil。
type ContentUpdateProgressFunc func(phase, message string)

// ContentHashFunc 计算文件 SHA-1（小写十六进制）。测试可注入假实现。
type ContentHashFunc func(path string) (string, error)

// InstanceGameInfoHook 解析实例的基础 MC 版本与加载器显示名。
// 由 internal/bindings 在启动时注入 internal/instance 的实现（download 包不能
// 直接 import instance —— instance 依赖本包，会成环）。未注入时返回空串，
// 兼容性判定退化为「不做过滤」，不影响检测本身。
var InstanceGameInfoHook func(minecraftDirectory, sourcePath, versionID string) (gameVersion, loaderName string)

// SaveModpackIndexSnapshotHook 把整合包清单原文交给宿主保存（仅测试注入；
// 生产路径直接用本文件的 SaveModpackIndexSnapshot 落到启动器数据目录）。
var SaveModpackIndexSnapshotHook func(contentDirectory, format, indexRaw string) error

// maxModpackIndexReadBytes 清单文件读取上限（8 MB，与安装侧一致）。
const maxModpackIndexReadBytes = 8 * 1024 * 1024

// maxIndexCaseInsensitiveLookups 大小写不一致时逐级探测目录的次数上限，
// 防止在畸形清单（大量不同大小写路径）上做 O(n) 次目录遍历。
const maxIndexCaseInsensitiveLookups = 64

// ---------------------------------------------------------------------------
// 入口
// ---------------------------------------------------------------------------

// CheckInstanceContentUpdates 检查一个实例的内容是否有更新。
//
// contentDirectory 由调用方通过 ResolveContentDirectoryForInstance 解析
// （隔离实例是 versions/<id>，共享实例是游戏根目录）。
// gameVersion / loaderName 用于把「最新版本」限定在当前实例可用的范围内；
// 为空则不做该过滤（未知一律不编造）。
// progress 可为 nil。
func CheckInstanceContentUpdates(
	ctx context.Context,
	contentDirectory, versionID, gameVersion, loaderName string,
	progress ContentUpdateProgressFunc,
) (*ContentUpdateCheckResult, error) {
	return checkInstanceContentUpdates(ctx, contentDirectory, versionID, gameVersion, loaderName, progress, computeFileSHA1)
}

// checkInstanceContentUpdates 与上面同源，多一个哈希函数注入点（测试用）。
func checkInstanceContentUpdates(
	ctx context.Context,
	contentDirectory, versionID, gameVersion, loaderName string,
	progress ContentUpdateProgressFunc,
	hashFile ContentHashFunc,
) (*ContentUpdateCheckResult, error) {
	if strings.TrimSpace(contentDirectory) == "" {
		return nil, errors.New("内容目录不能为空")
	}
	if hashFile == nil {
		hashFile = computeFileSHA1
	}

	result := &ContentUpdateCheckResult{
		VersionID:              versionID,
		ContentDirectory:       contentDirectory,
		ModsDirectory:          filepath.Join(contentDirectory, "mods"),
		ResourcePacksDirectory: filepath.Join(contentDirectory, "resourcepacks"),
		ShaderPacksDirectory:   filepath.Join(contentDirectory, "shaderpacks"),
		GameVersion:            gameVersion,
		LoaderName:             loaderName,
		Files:                  []ContentUpdateFile{},
		Notices:                []string{},
		HashBatchSize:          contentUpdateHashBatchSize,
	}

	files := collectContentUpdateCandidates(result)

	// ---- 1) 本地哈希（同一路径只算一次，重复内容不重复联网） ----
	reportUpdateProgress(progress, "hashing", fmt.Sprintf("正在计算 %d 个文件的 SHA-1…", len(files)))
	hashCache := map[string]string{}
	fileHashes := make([]string, len(files))
	uniqueHashes := make([]string, 0, len(files))
	seenHash := map[string]bool{}
	for i := range files {
		if err := ctx.Err(); err != nil {
			return nil, translateCheckError(err)
		}
		if files[i].Status == ContentUpdateStatusSkipped {
			continue
		}
		key := strings.ToLower(files[i].FilePath)
		hash, ok := hashCache[key]
		if !ok {
			computed, err := hashFile(files[i].FilePath)
			if err != nil {
				files[i].Status = ContentUpdateStatusCheckFailed
				files[i].StatusText = fmt.Sprintf("读取文件失败：%v", err)
				result.FailedCount++
				continue
			}
			hash = computed
			hashCache[key] = hash
		}
		fileHashes[i] = hash
		files[i].SHA1 = hash
		if !seenHash[hash] {
			seenHash[hash] = true
			uniqueHashes = append(uniqueHashes, hash)
		} else {
			result.DuplicateFileCount++
		}
	}

	// ---- 2) 批量反查归属（一批哈希一个请求） ----
	matches := map[string]modrinth.VersionFileMatch{}
	lookupFailed := false
	if len(uniqueHashes) > 0 {
		reportUpdateProgress(progress, "lookup", fmt.Sprintf("正在向 Modrinth 查询 %d 个文件哈希…", len(uniqueHashes)))
		looked, lookupErr := modrinth.GetVersionFilesByHashes(ctx, uniqueHashes)
		result.HashRequestCount = batchRequestCount(len(uniqueHashes))
		if lookupErr != nil {
			lookupFailed = true
			// 反查失败时不再问「最新版本」：没有归属的更新结果没有意义，
			// 而且第二次请求大概率同样失败，白等一轮。
			result.Notices = append(result.Notices,
				fmt.Sprintf("无法向 Modrinth 查询文件哈希：%s", readableCheckError(lookupErr)))
		} else {
			matches = looked
		}
	}

	// ---- 3) 批量问最新版本（同一批哈希，一次请求，按实例兼容性过滤） ----
	updates := map[string]modrinth.VersionFileUpdate{}
	updateFailed := false
	if !lookupFailed {
		compatible := hashesWithMatch(uniqueHashes, matches)
		if len(compatible) > 0 {
			reportUpdateProgress(progress, "latest", fmt.Sprintf("正在比较 %d 个已识别文件的最新版本…", len(compatible)))
			latest, updateErr := modrinth.GetLatestVersionFilesByHashes(
				ctx, compatible, gameVersionsFor(gameVersion), loadersFor(loaderName))
			result.HashRequestCount += batchRequestCount(len(compatible))
			if updateErr != nil {
				updateFailed = true
				result.Notices = append(result.Notices,
					fmt.Sprintf("查询最新版本失败，这些文件本次无法判断：%s", readableCheckError(updateErr)))
			} else {
				updates = latest
			}
		}
	}

	// ---- 4) 逐个文件落结论 ----
	for i := range files {
		entry := &files[i]
		if entry.Status == ContentUpdateStatusSkipped {
			continue
		}
		if entry.Status == ContentUpdateStatusCheckFailed {
			continue
		}
		entry.SizeBytes = fileSize(entry.FilePath)
		entry.SizeText = formatByteSize(entry.SizeBytes)

		hash := fileHashes[i]
		if hash == "" {
			entry.Status = ContentUpdateStatusCheckFailed
			entry.StatusText = "未能计算文件哈希。"
			result.FailedCount++
			continue
		}

		match, found := matches[hash]
		if !found {
			// 关键语义：查不到 ≠ 没有更新。
			if lookupFailed {
				entry.Status = ContentUpdateStatusCheckFailed
				entry.StatusText = "本次未能向 Modrinth 查询（网络或服务端失败）。"
				result.FailedCount++
			} else {
				entry.Status = ContentUpdateStatusUnknown
				entry.StatusText = "Modrinth 未收录这个文件（自建包、手动放入或 CurseForge 独占），无法判断是否可更新；可在版本管理里查看（CurseForge 文件需配置 API Key）。"
				result.UnknownCount++
			}
			continue
		}

		entry.ProjectID = match.ProjectID
		entry.ProjectName = match.ProjectID
		entry.CurrentVersion = match.VersionNumber
		entry.CurrentVersionID = match.VersionID

		latest, hasLatest := updates[hash]
		switch {
		case updateFailed:
			entry.Status = ContentUpdateStatusCheckFailed
			entry.StatusText = "已识别文件来源，但本次未能确认最新版本。"
			result.FailedCount++
		case !hasLatest:
			// 反查命中、但「最新版本」接口没有回答这个哈希：可能是已是最新，
			// 也可能是当前实例的 MC 版本/加载器下没有任何兼容版本。
			// 后者报「已是最新」是错的，因此按「未知」处理并说明原因。
			if hasCompatibilityFilter(gameVersion, loaderName) {
				entry.Status = ContentUpdateStatusUnknown
				entry.StatusText = fmt.Sprintf(
					"Modrinth 上没有匹配当前实例（%s）的可用版本，无法判断是否可更新。",
					compatibilityLabel(gameVersion, loaderName))
				result.UnknownCount++
			} else {
				entry.Status = ContentUpdateStatusLatest
				entry.StatusText = fmt.Sprintf("已是最新版本（%s）。", orPlaceholder(match.VersionNumber))
				result.LatestCount++
			}
		case latest.VersionID == match.VersionID:
			entry.Status = ContentUpdateStatusLatest
			entry.StatusText = fmt.Sprintf("已是最新版本（%s）。", orPlaceholder(match.VersionNumber))
			result.LatestCount++
		default:
			entry.Status = ContentUpdateStatusUpdatable
			entry.LatestVersion = latest.VersionNumber
			entry.LatestVersionID = latest.VersionID
			entry.LatestFileName = latest.File.Filename
			entry.DownloadURL = latest.File.URL
			entry.DownloadSizeBytes = latest.File.Size
			entry.DownloadSizeText = formatByteSize(latest.File.Size)
			entry.ReleaseDate = formatReleaseDate(latest.DatePublished)
			entry.GameVersionsText = joinLimited(latest.GameVersions, 3)
			entry.LoadersText = joinLimited(latest.Loaders, 4)
			entry.CompatibleWithInstance = compatibleWithInstance(latest, gameVersion, loaderName)
			entry.StatusText = fmt.Sprintf("有新版本：%s → %s。",
				orPlaceholder(match.VersionNumber), orPlaceholder(latest.VersionNumber))
			result.UpdatableCount++
		}
	}

	// 项目名回填（Modrinth 的 version_files 只给 project_id）
	fillProjectNames(ctx, files, progress)

	sortUpdateFiles(files)
	result.Files = files
	result.CheckedFileCount = len(files)
	if result.UpdatableCount == 0 && result.UnknownCount > 0 {
		result.Notices = append(result.Notices, fmt.Sprintf(
			"有 %d 个文件在 Modrinth 上查不到归属，它们是否可更新无法判断（这不是「已是最新」）。",
			result.UnknownCount))
	}

	// ---- 5) 整合包清单比对（纯本地，不联网） ----
	reportUpdateProgress(progress, "modpack", "正在比对整合包清单…")
	result.Modpack = VerifyInstanceModpack(contentDirectory, hashFile)
	if result.Modpack.Status == ModpackVerifyUnsupported {
		result.Notices = append(result.Notices, result.Modpack.StatusText)
	}
	if result.Modpack.Status == ModpackVerifyIssues {
		result.Notices = append(result.Notices, fmt.Sprintf(
			"整合包清单比对：缺失 %d 个、被改动 %d 个。",
			result.Modpack.MissingCount, result.Modpack.ModifiedCount))
	}

	reportUpdateProgress(progress, "done", result.SummaryText())
	return result, nil
}

// SummaryText 结果的一句话摘要（中文，前端可直接展示）。
func (r *ContentUpdateCheckResult) SummaryText() string {
	parts := []string{
		fmt.Sprintf("可更新 %d 个", r.UpdatableCount),
		fmt.Sprintf("已是最新 %d 个", r.LatestCount),
	}
	if r.UnknownCount > 0 {
		parts = append(parts, fmt.Sprintf("未知 %d 个", r.UnknownCount))
	}
	if r.FailedCount > 0 {
		parts = append(parts, fmt.Sprintf("检查失败 %d 个", r.FailedCount))
	}
	if r.SkippedCount > 0 {
		parts = append(parts, fmt.Sprintf("跳过 %d 个", r.SkippedCount))
	}
	return strings.Join(parts, "，") + "。"
}

// ---------------------------------------------------------------------------
// 候选文件收集
// ---------------------------------------------------------------------------

// collectContentUpdateCandidates 枚举实例里可检测的内容文件。
// mods 取 *.jar / *.jar.disabled；资源包与光影取 *.zip / *.zip.disabled。
// 目录形态的资源包/光影没有单一文件哈希，直接标记为跳过（如实说明原因）。
func collectContentUpdateCandidates(result *ContentUpdateCheckResult) []ContentUpdateFile {
	files := make([]ContentUpdateFile, 0)

	appendEntries := func(directory, kind, label string, suffixes []string) {
		for _, path := range enumerateContentFiles(directory, suffixes) {
			entry := ContentUpdateFile{
				FilePath:  path,
				FileName:  contentDisplayName(path),
				Kind:      kind,
				KindLabel: label,
				Status:    ContentUpdateStatusUnknown,
			}
			if isDisabledContentFile(path) {
				entry.Status = ContentUpdateStatusSkipped
				entry.StatusText = "文件处于禁用状态（.disabled），跳过更新检测。"
				result.SkippedCount++
			}
			files = append(files, entry)
		}
	}

	appendEntries(result.ModsDirectory, ContentUpdateKindMod, "Mod", []string{".jar", ".jar.disabled"})
	appendEntries(result.ResourcePacksDirectory, ContentUpdateKindResourcePack, "资源包", []string{".zip", ".zip.disabled"})
	appendEntries(result.ShaderPacksDirectory, ContentUpdateKindShaderPack, "光影包", []string{".zip", ".zip.disabled"})

	// 目录形态的资源包/光影（未打包）也要如实报「跳过」，否则用户会以为它们被检查过
	for _, item := range []struct {
		directory string
		kind      string
		label     string
	}{
		{result.ResourcePacksDirectory, ContentUpdateKindResourcePack, "资源包"},
		{result.ShaderPacksDirectory, ContentUpdateKindShaderPack, "光影包"},
	} {
		entries, err := os.ReadDir(item.directory)
		if err != nil {
			continue
		}
		for _, dirEntry := range entries {
			if !dirEntry.IsDir() || strings.HasPrefix(dirEntry.Name(), ".") {
				continue
			}
			result.SkippedCount++
			files = append(files, ContentUpdateFile{
				FilePath:   filepath.Join(item.directory, dirEntry.Name()),
				FileName:   dirEntry.Name(),
				Kind:       item.kind,
				KindLabel:  item.label,
				Status:     ContentUpdateStatusSkipped,
				StatusText: "目录形态的内容没有单一文件哈希，无法按哈希检测更新。",
			})
		}
	}

	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Kind != files[j].Kind {
			return files[i].Kind < files[j].Kind
		}
		return strings.ToLower(files[i].FileName) < strings.ToLower(files[j].FileName)
	})
	return files
}

// enumerateContentFiles 列出目录下匹配后缀的文件（不递归、跳过隐藏文件）。
func enumerateContentFiles(directory string, suffixes []string) []string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		lower := strings.ToLower(name)
		for _, suffix := range suffixes {
			if strings.HasSuffix(lower, suffix) {
				result = append(result, filepath.Join(directory, name))
				break
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

// contentDisplayName 展示用文件名（剥掉 .disabled 后缀）。
func contentDisplayName(path string) string {
	name := filepath.Base(path)
	if isDisabledContentFile(name) {
		name = name[:len(name)-len(".disabled")]
	}
	return name
}

func isDisabledContentFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".disabled")
}

// ---------------------------------------------------------------------------
// 哈希与批量口径
// ---------------------------------------------------------------------------

// computeFileSHA1 计算文件 SHA-1（小写十六进制）。
// 与 Modrinth 的 `algorithm=sha1` 口径一致；整合包导出侧用的是同一算法。
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

// batchRequestCount 与 modrinth 客户端一致的分批数：每个批次一个请求。
func batchRequestCount(hashCount int) int {
	if hashCount <= 0 {
		return 0
	}
	size := contentUpdateHashBatchSize
	if size <= 0 {
		return 1
	}
	return (hashCount + size - 1) / size
}

// hashesWithMatch 只保留反查命中的哈希（未命中的问「最新版本」没有意义）。
func hashesWithMatch(hashes []string, matches map[string]modrinth.VersionFileMatch) []string {
	result := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		if _, ok := matches[hash]; ok {
			result = append(result, hash)
		}
	}
	return result
}

// gameVersionsFor 兼容性过滤用的 MC 版本列表：只有解析出具体版本号才过滤。
func gameVersionsFor(gameVersion string) []string {
	trimmed := strings.TrimSpace(gameVersion)
	switch trimmed {
	case "", "未识别", "未知", "未提供":
		return nil
	}
	return []string{trimmed}
}

// loadersFor 兼容性过滤用的加载器列表：只对 Mod 有意义（资源包/光影包没有加载器）。
// 「原版」没有对应的 Modrinth 加载器标识，返回 nil（不过滤）。
func loadersFor(loaderName string) []string {
	switch strings.ToLower(strings.TrimSpace(loaderName)) {
	case "fabric":
		return []string{"fabric"}
	case "forge":
		return []string{"forge"}
	case "neoforge":
		return []string{"neoforge"}
	case "quilt":
		return []string{"quilt"}
	default:
		return nil
	}
}

func hasCompatibilityFilter(gameVersion, loaderName string) bool {
	return len(gameVersionsFor(gameVersion)) > 0 || len(loadersFor(loaderName)) > 0
}

func compatibilityLabel(gameVersion, loaderName string) string {
	parts := []string{}
	if version := strings.TrimSpace(gameVersion); version != "" {
		parts = append(parts, "Minecraft "+version)
	}
	if loader := strings.TrimSpace(loaderName); loader != "" && !strings.EqualFold(loader, "原版") {
		parts = append(parts, loader)
	}
	if len(parts) == 0 {
		return "当前实例"
	}
	return strings.Join(parts, " + ")
}

// compatibleWithInstance 最新版本是否声明兼容当前实例。
// 实例信息缺失时返回 true（不做无依据的否定判断）。
func compatibleWithInstance(update modrinth.VersionFileUpdate, gameVersion, loaderName string) bool {
	versions := gameVersionsFor(gameVersion)
	loaders := loadersFor(loaderName)
	gameOK := len(versions) == 0 || containsStringFold(update.GameVersions, versions[0])
	loaderOK := len(loaders) == 0 || containsStringFold(update.Loaders, loaders[0])
	return gameOK && loaderOK
}

// containsStringFold 不区分大小写的列表包含判断（资源包/加载器标识大小写不保证）。
func containsStringFold(haystack []string, needle string) bool {
	for _, item := range haystack {
		if strings.EqualFold(strings.TrimSpace(item), needle) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 项目名回填
// ---------------------------------------------------------------------------

// fillProjectNames 用 projectID 查项目名（同一项目只查一次，失败保持 projectID 占位）。
// 项目名只是展示信息，任何失败都不应让整个检测变成失败。
func fillProjectNames(ctx context.Context, files []ContentUpdateFile, progress ContentUpdateProgressFunc) {
	missing := map[string]bool{}
	for i := range files {
		if files[i].ProjectID != "" && files[i].ProjectName == files[i].ProjectID {
			missing[files[i].ProjectID] = true
		}
	}
	if len(missing) == 0 {
		return
	}

	reportUpdateProgress(progress, "projects", fmt.Sprintf("正在获取 %d 个项目的名称…", len(missing)))
	names := map[string]string{}
	for projectID := range missing {
		if err := ctx.Err(); err != nil {
			break
		}
		name, err := modrinth.GetProjectName(ctx, projectID)
		if err != nil || strings.TrimSpace(name) == "" {
			continue
		}
		names[projectID] = name
	}
	if len(names) == 0 {
		return
	}
	for i := range files {
		if name, ok := names[files[i].ProjectID]; ok {
			files[i].ProjectName = name
		}
	}
}

// ---------------------------------------------------------------------------
// 排序与格式化
// ---------------------------------------------------------------------------

// sortUpdateFiles 排序：可更新 → 失败 → 未知 → 已是最新 → 跳过，同状态按类别 + 名称。
func sortUpdateFiles(files []ContentUpdateFile) {
	rank := func(status string) int {
		switch status {
		case ContentUpdateStatusUpdatable:
			return 0
		case ContentUpdateStatusCheckFailed:
			return 1
		case ContentUpdateStatusUnknown:
			return 2
		case ContentUpdateStatusLatest:
			return 3
		default:
			return 4
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		left, right := rank(files[i].Status), rank(files[j].Status)
		if left != right {
			return left < right
		}
		if files[i].Kind != files[j].Kind {
			return files[i].Kind < files[j].Kind
		}
		return strings.ToLower(files[i].FileName) < strings.ToLower(files[j].FileName)
	})
}

// formatByteSize 字节数格式化（与 models.ModrinthVersionFile.SizeDisplay 同口径）。
func formatByteSize(size int64) string {
	switch {
	case size >= 1048576:
		return fmt.Sprintf("%.1f MB", float64(size)/1048576.0)
	case size >= 1024:
		return fmt.Sprintf("%.0f KB", float64(size)/1024.0)
	case size > 0:
		return fmt.Sprintf("%d B", size)
	default:
		return "未知大小"
	}
}

// formatReleaseDate 把 RFC3339 转成 yyyy-MM-dd；解析失败原样返回。
func formatReleaseDate(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) >= 10 && trimmed[4] == '-' && trimmed[7] == '-' {
		return trimmed[:10]
	}
	return trimmed
}

// joinLimited 取前 limit 个元素拼接；超出的部分用「…」提示。
func joinLimited(values []string, limit int) string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}
	if limit > 0 && len(cleaned) > limit {
		return strings.Join(cleaned[:limit], ", ") + "…"
	}
	return strings.Join(cleaned, ", ")
}

func orPlaceholder(value string) string {
	if strings.TrimSpace(value) == "" {
		return "未知版本"
	}
	return value
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func reportUpdateProgress(progress ContentUpdateProgressFunc, phase, message string) {
	if progress != nil {
		progress(phase, message)
	}
}

// readableCheckError 把底层错误转成面向用户的一句话。
func readableCheckError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "已取消。"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "请求超时。"
	}
	return err.Error()
}

func translateCheckError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("检查更新已取消：%w", err)
}

// ---------------------------------------------------------------------------
// 整合包清单快照（安装时落盘，供后续离线比对）
// ---------------------------------------------------------------------------

// modpackIndexSnapshot 整合包清单快照。
type modpackIndexSnapshot struct {
	// SavedAt 记录时间（RFC3339，仅便于排查）。
	SavedAt string `json:"savedAt"`
	// Format modrinth / curseforge。
	Format string `json:"format"`
	// Raw 清单原始 JSON 文本（安装时解析用的那一段）。
	Raw string `json:"raw"`
}

// SaveModpackIndexSnapshot 把整合包清单存到启动器数据目录，供后续离线比对。
// 清单本身不会留在实例目录里（安装器按规范把它当启动器元数据排除），
// 不留一份快照，「整合包有没有被改动」这个问题在安装完成后就再也答不上来。
//
// indexRaw 为空、或格式无法识别时为空操作（不产生垃圾文件）。
func SaveModpackIndexSnapshot(contentDirectory, format, indexRaw string) error {
	if strings.TrimSpace(contentDirectory) == "" || strings.TrimSpace(indexRaw) == "" {
		return nil
	}
	normalizedFormat := normalizeModpackFormat(format)
	if normalizedFormat == "" {
		return nil
	}
	directory, err := modpackSnapshotDirectory()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(modpackIndexSnapshot{
		SavedAt: time.Now().Format(time.RFC3339),
		Format:  normalizedFormat,
		Raw:     indexRaw,
	}, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(directory, modpackSnapshotKey(contentDirectory)+".json")
	return writeFileAtomically(target, payload)
}

// ModpackSnapshotPath 清单快照路径（测试与排查用）。
func ModpackSnapshotPath(contentDirectory string) (string, error) {
	directory, err := modpackSnapshotDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, modpackSnapshotKey(contentDirectory)+".json"), nil
}

func modpackSnapshotDirectory() (string, error) {
	storage := strings.TrimSpace(config.StorageDirectory())
	if storage == "" {
		return "", errors.New("启动器存储目录不可用，无法保存整合包清单快照")
	}
	return filepath.Join(storage, "modpack-index"), nil
}

// modpackSnapshotKey 快照文件名：内容目录全路径的 SHA-1 前 16 位 + 目录名。
// 必须在「安装时写」与「检查时读」两侧算出完全相同的值，因此只依赖内容目录
// 这一个输入（实例名/版本号在共享布局下与内容目录并不一一对应）。
func modpackSnapshotKey(contentDirectory string) string {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(contentDirectory)))
	sum := sha1.Sum([]byte(normalized))
	readable := sanitizeSegment(filepath.Base(filepath.Clean(contentDirectory)))
	if readable == "" || readable == "." {
		readable = "instance"
	}
	if utf8.RuneCountInString(readable) > 40 {
		readable = string([]rune(readable)[:40])
	}
	return hex.EncodeToString(sum[:8]) + "-" + readable
}

// persistModpackIndexSnapshot 安装流程结束后保存清单快照（供后续离线比对）。
// 失败不影响安装结果，只由调用方记一条错误（用户仍能用整合包，只是清单比对会报无清单）。
func persistModpackIndexSnapshot(contentDirectory, format, indexRaw string) error {
	if strings.TrimSpace(indexRaw) == "" {
		return nil
	}
	if SaveModpackIndexSnapshotHook != nil {
		return SaveModpackIndexSnapshotHook(contentDirectory, format, indexRaw)
	}
	return SaveModpackIndexSnapshot(contentDirectory, format, indexRaw)
}

// writeFileAtomically 先写临时文件再改名，避免半截文件被当成有效快照。
func writeFileAtomically(path string, payload []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, payload, 0o644); err != nil {
		return err
	}
	tools.RemoveFileIfExists(path)
	if err := os.Rename(temporary, path); err != nil {
		tools.RemoveFileIfExists(temporary)
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// 整合包清单比对
// ---------------------------------------------------------------------------

// VerifyInstanceModpack 比对整合包清单（modrinth.index.json / manifest.json）与磁盘文件。
// 不联网：只做本地哈希比对；CurseForge 格式只有 projectID/fileID，如实报「需要 API key」。
func VerifyInstanceModpack(contentDirectory string, hashFile ContentHashFunc) ModpackVerifyResult {
	result := ModpackVerifyResult{Files: []ModpackVerifyFile{}}
	if hashFile == nil {
		hashFile = computeFileSHA1
	}

	raw, path, source, format := resolveModpackIndex(contentDirectory)
	if raw == "" {
		result.Status = ModpackVerifyNoIndex
		result.StatusText = "没有找到整合包清单（modrinth.index.json / manifest.json），无法比对已装文件是否被改动；" +
			"清单按规范不会留在实例目录里，可重新导入一次整合包以生成清单快照。"
		return result
	}
	result.Present = true
	result.Format = format
	result.IndexPath = path
	result.Source = source

	if format == "curseforge" {
		// CurseForge 的 manifest.json 只有 projectID/fileID 引用，没有哈希也没有直链；
		// 要拿文件哈希必须走 CurseForge 官方 API（需要 API key）。
		result.Status = ModpackVerifyUnsupported
		result.StatusText = "这是 CurseForge 格式的整合包清单，只记录了 projectID/fileID 引用，" +
			"没有文件哈希，离线无法校验；需要 CurseForge API key 才能查询文件详情。"
		var manifest struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if json.Unmarshal([]byte(raw), &manifest) == nil {
			result.PackName = manifest.Name
			result.PackVersion = manifest.Version
		}
		return result
	}

	var index modpackIndex
	if err := json.Unmarshal([]byte(raw), &index); err != nil {
		result.Status = ModpackVerifyFailed
		result.StatusText = fmt.Sprintf("整合包清单解析失败：%v", err)
		return result
	}
	result.PackName, result.PackVersion = readIndexPackIdentity(raw)

	caseInsensitiveBudget := maxIndexCaseInsensitiveLookups
	total := 0
	for _, file := range index.Files {
		declared := normalizeIndexPath(file.Path)
		if declared == "" {
			// 没有 path 的条目（CurseForge 引用混进 Modrinth 清单）无法定位文件
			if file.ProjectID != nil && file.FileID != nil {
				result.UnverifiableCount++
				result.Files = append(result.Files, ModpackVerifyFile{
					Status:     ModpackFileUnverifiable,
					StatusText: fmt.Sprintf("条目只给了 CurseForge 引用（%d/%d），无法离线校验。", *file.ProjectID, *file.FileID),
				})
			}
			continue
		}
		total++
		entry := ModpackVerifyFile{
			Path:         declared,
			FileName:     filepath.Base(filepath.FromSlash(declared)),
			DeclaredSHA1: declaredSHA1(file.Hashes),
			Required:     file.Required == nil || *file.Required,
		}

		absolute := filepath.Join(contentDirectory, filepath.FromSlash(declared))
		if !tools.FileExists(absolute) {
			// 解压阶段会剥掉 overrides/ 前缀，清单路径与包内路径可能对不上
			overridden := filepath.Join(contentDirectory, filepath.FromSlash("overrides/"+declared))
			if tools.FileExists(overridden) {
				absolute = overridden
			} else if caseInsensitiveBudget > 0 {
				if found, ok := findContentPath(contentDirectory, declared); ok {
					absolute = found
					caseInsensitiveBudget--
				}
			}
		}
		if !tools.FileExists(absolute) {
			entry.Status = ModpackFileMissing
			entry.StatusText = "清单声明的文件在磁盘上不存在。"
			result.MissingCount++
			result.Files = append(result.Files, entry)
			continue
		}

		if entry.DeclaredSHA1 == "" {
			entry.Status = ModpackFileUnverifiable
			entry.StatusText = "清单没有给出该文件的哈希，无法比对。"
			result.UnverifiableCount++
			result.Files = append(result.Files, entry)
			continue
		}

		actual, err := hashFile(absolute)
		if err != nil {
			entry.Status = ModpackFileUnverifiable
			entry.StatusText = fmt.Sprintf("读取文件失败，无法比对：%v", err)
			result.UnverifiableCount++
			result.Files = append(result.Files, entry)
			continue
		}
		entry.ActualSHA1 = strings.ToLower(actual)
		if strings.EqualFold(entry.ActualSHA1, entry.DeclaredSHA1) {
			entry.Status = ModpackFileCurrent
			entry.StatusText = "与清单一致。"
			result.CurrentCount++
		} else {
			entry.Status = ModpackFileModified
			entry.StatusText = "文件哈希与清单不一致（被替换或改过）。"
			result.ModifiedCount++
		}
		result.Files = append(result.Files, entry)
	}

	result.TotalFiles = total
	switch {
	case result.MissingCount > 0 || result.ModifiedCount > 0:
		result.Status = ModpackVerifyIssues
		result.StatusText = fmt.Sprintf("清单比对：%d 个文件缺失、%d 个被改动、%d 个一致。",
			result.MissingCount, result.ModifiedCount, result.CurrentCount)
	case total == 0:
		result.Status = ModpackVerifyOK
		result.StatusText = "清单没有声明任何可校验的文件。"
	default:
		result.Status = ModpackVerifyOK
		result.StatusText = fmt.Sprintf("清单比对：%d 个文件全部与清单一致。", result.CurrentCount)
	}
	sortModpackFiles(result.Files)
	return result
}

// sortModpackFiles 排序：缺失 → 改动 → 无法校验 → 一致，便于前端直接展示。
func sortModpackFiles(files []ModpackVerifyFile) {
	rank := func(status string) int {
		switch status {
		case ModpackFileMissing:
			return 0
		case ModpackFileModified:
			return 1
		case ModpackFileUnverifiable:
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		left, right := rank(files[i].Status), rank(files[j].Status)
		if left != right {
			return left < right
		}
		return strings.ToLower(files[i].Path) < strings.ToLower(files[j].Path)
	})
}

// resolveModpackIndex 按优先级找整合包清单：
//  1. 实例内容目录里的 modrinth.index.json / index.json / manifest.json；
//  2. 上次安装时由本启动器保存的清单快照（安装器会把清单从实例目录里剔除，
//     没有快照时这个问题只能回答「不知道」）。
func resolveModpackIndex(contentDirectory string) (raw, path, source, format string) {
	candidates := []struct {
		name   string
		format string
	}{
		{"modrinth.index.json", "modrinth"},
		{"manifest.json", "curseforge"},
		{"index.json", "modrinth"},
	}
	for _, candidate := range candidates {
		full := filepath.Join(contentDirectory, candidate.name)
		if !tools.FileExists(full) {
			continue
		}
		data, err := readIndexFileLimited(full)
		if err != nil {
			continue
		}
		if detected := detectModpackFormat(data, candidate.format); detected != "" {
			return string(data), full, "实例内容目录", detected
		}
	}

	snapshotPath, err := ModpackSnapshotPath(contentDirectory)
	if err != nil || !tools.FileExists(snapshotPath) {
		return "", "", "", ""
	}
	data, err := readIndexFileLimited(snapshotPath)
	if err != nil {
		return "", "", "", ""
	}
	var snapshot modpackIndexSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil || strings.TrimSpace(snapshot.Raw) == "" {
		return "", "", "", ""
	}
	detected := detectModpackFormat([]byte(snapshot.Raw), snapshot.Format)
	if detected == "" {
		return "", "", "", ""
	}
	return snapshot.Raw, snapshotPath, "上次安装时缓存的清单快照", detected
}

// readIndexFileLimited 限长读取清单文件（防止异常大文件把内存吃满）。
func readIndexFileLimited(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err == nil && info.Size() > maxModpackIndexReadBytes {
		return nil, fmt.Errorf("整合包清单超过体积上限（%d MB）", maxModpackIndexReadBytes>>20)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxModpackIndexReadBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxModpackIndexReadBytes {
		return nil, fmt.Errorf("整合包清单超过体积上限（%d MB）", maxModpackIndexReadBytes>>20)
	}
	return data, nil
}

// detectModpackFormat 识别清单格式：
//   - 有 files[].path 或 files[].hashes → Modrinth（mrpack）；
//   - 有 files[].projectID/fileID 或 minecraft 段 → CurseForge；
//   - files 是空数组时按文件名兜底（modrinth.index.json 一定是 mrpack）；
//   - 其余视为无法识别（返回空串，调用方按「没有清单」处理，不要瞎猜）。
func detectModpackFormat(data []byte, fallback string) string {
	var probe struct {
		Files []struct {
			Path      string            `json:"path"`
			Hashes    map[string]string `json:"hashes"`
			ProjectID *int              `json:"projectID"`
			FileID    *int              `json:"fileID"`
		} `json:"files"`
		Minecraft *json.RawMessage `json:"minecraft"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return ""
	}
	for _, file := range probe.Files {
		if strings.TrimSpace(file.Path) != "" || len(file.Hashes) > 0 {
			return "modrinth"
		}
		if file.ProjectID != nil && file.FileID != nil {
			return "curseforge"
		}
	}
	if probe.Minecraft != nil {
		return "curseforge"
	}
	// files 为空数组（mrpack 合法：内容全在 overrides/ 里）或字段缺失时按文件名兜底
	if probe.Files != nil {
		return normalizeModpackFormat(fallback)
	}
	return ""
}

func normalizeModpackFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "modrinth", "mrpack":
		return "modrinth"
	case "curseforge":
		return "curseforge"
	default:
		return ""
	}
}

// declaredSHA1 读取 mrpack 条目声明的 SHA-1（小写）。
// 只取 SHA-1：Modrinth 规范里 sha1 与 sha512 都可能出现，但更新检测与
// Modrinth 的 version_files 接口都按 SHA-1 口径工作，混用两种算法只会让
// 「哈希不一致」的判定失去可比性。
func declaredSHA1(hashes modpackFileHashes) string {
	return strings.ToLower(strings.TrimSpace(hashes.SHA1))
}

// readIndexPackIdentity 读整合包名与版本（两种格式字段名不同）。
func readIndexPackIdentity(raw string) (name, version string) {
	var probe struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		VersionID string `json:"versionId"`
	}
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return "", ""
	}
	version = probe.Version
	if strings.TrimSpace(version) == "" {
		version = probe.VersionID
	}
	return probe.Name, version
}

// normalizeIndexPath 规范化清单里的相对路径（统一正斜杠、去首尾空白与前导斜杠）。
func normalizeIndexPath(path string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	normalized = strings.TrimPrefix(normalized, "./")
	normalized = strings.TrimPrefix(normalized, "/")
	return normalized
}

// findContentPath 大小写不一致时的兜底查找：逐级用不区分大小写的目录项名匹配。
// 只在磁盘上真找不到精确路径时调用（有次数上限）。
func findContentPath(root, relative string) (string, bool) {
	current := root
	segments := strings.Split(relative, "/")
	for index, segment := range segments {
		entries, err := os.ReadDir(current)
		if err != nil {
			return "", false
		}
		matched := ""
		for _, entry := range entries {
			if strings.EqualFold(entry.Name(), segment) {
				matched = entry.Name()
				break
			}
		}
		if matched == "" {
			return "", false
		}
		current = filepath.Join(current, matched)
		if index == len(segments)-1 {
			return current, true
		}
	}
	return "", false
}
