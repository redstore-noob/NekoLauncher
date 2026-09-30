package download

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/download/curseforge"
	"nekolauncher/internal/models"
)

// ContentInstallService 内容安装服务：把 Mod / 资源包 / 光影包 / 整合包下载到
// 指定实例的内容目录，或按自定义路径保存。整合包支持解压 .mrpack 并解析依赖。

// 解压防护常量。
const (
	// maximumArchiveEntries 解压防护：条目数上限。
	maximumArchiveEntries = 20000
	// maximumEntryBytes 解压防护：单条目解压后大小上限（2 GB）。
	maximumEntryBytes = 2 * 1024 * 1024 * 1024
	// maximumExtractedBytes 解压防护：累计解压字节上限（8 GB，防止压缩炸弹撑爆磁盘）。
	maximumExtractedBytes = 8 * 1024 * 1024 * 1024
	// maximumIndexBytes 整合包清单（modrinth.index.json / manifest.json）体积上限（8 MB）：
	// 这几个文件在用 json.Unmarshal 之前读进内存，必须有独立的限长，
	// 不能只依赖后面解压阶段的体积防护。
	maximumIndexBytes = 8 * 1024 * 1024
)

// DownloadFileToInstance 下载文件到实例内容目录的指定子目录（如 mods / resourcepacks / shaderpacks）。
// 返回最终保存的文件路径。
func DownloadFileToInstance(
	ctx context.Context,
	downloadURL, fileName, contentDirectory, subDirectory string,
	progress ProgressBytes,
) (string, error) {
	if strings.TrimSpace(downloadURL) == "" {
		return "", fmt.Errorf("downloadURL 不能为空")
	}
	if strings.TrimSpace(contentDirectory) == "" {
		return "", fmt.Errorf("contentDirectory 不能为空")
	}

	targetPath := filepath.Join(contentDirectory, subDirectory, sanitizeFileName(fileName))
	if err := DownloadFileToPath(ctx, downloadURL, targetPath, progress); err != nil {
		return "", err
	}
	return targetPath, nil
}

// ResolveContentDirectoryForInstance 解析已安装实例的内容目录（mods / resourcepacks
// 等的父目录）。与启动时的隔离判定完全一致（全局默认隔离 + 版本自身设置），
// 避免安装内容落点与游戏运行时目录不一致（例如默认隔离下 mods 被装进共享根目录）。
func ResolveContentDirectoryForInstance(minecraftDirectory, sourcePath, versionID string) string {
	if strings.TrimSpace(minecraftDirectory) == "" {
		return ""
	}
	// GameVersionIsolation.Resolve 只依赖 SourcePath 与 MinecraftDirectory，
	// 其余快照字段对本判定无影响（见 host_hooks.go 的钩子说明）。
	if ResolveContentDirectoryHook != nil {
		if dir := ResolveContentDirectoryHook(minecraftDirectory, sourcePath, versionID); dir != "" {
			return dir
		}
	}
	return minecraftDirectory
}

// ---------------------------------------------------------------------------
// 整合包安装
// ---------------------------------------------------------------------------

// ModpackInstallResult 安装统计：解压文件数、下载的依赖 mod 数、错误列表。
type ModpackInstallResult struct {
	InstalledFiles int
	DownloadedMods int
	Errors         []string
	// Warnings 非致命提示：安装成功但用户需要知道的事（如加载器不受支持、
	// 声明的运行要求与目标实例不一致）。与 Errors 分开，避免"装完了但一堆红字"。
	Warnings []string
	// DeclaredMinecraftVersion 清单里声明的 MC 版本（读不到时为空）。
	DeclaredMinecraftVersion string
	// DeclaredLoaderName / DeclaredLoaderVersion 清单里声明的加载器（无则为空/原版）。
	DeclaredLoaderName    string
	DeclaredLoaderVersion string
	// DeclaredLoaderSupported 声明的加载器能否由本启动器自动安装；原版包为 true，
	// 声明了但识别不出的加载器（如 rift-loader）为 false。
	DeclaredLoaderSupported bool
	// DetectedFormat 识别出的清单格式：modrinth / curseforge / multimc；无清单时为空。
	DetectedFormat string
}

// modpackIndex mrpack index.json / CurseForge manifest.json 的公共解析模型。
type modpackIndex struct {
	Files        []modpackFileEntry `json:"files"`
	Dependencies map[string]string  `json:"dependencies"`
}

type modpackFileEntry struct {
	Path      string   `json:"path"`
	Downloads []string `json:"downloads"`
	// Hashes mrpack 规范里每个声明文件都带哈希（sha1 / sha512），
	// 整合包更新检测（X-4）用它做本地比对，安装时用它校验下载结果。
	Hashes modpackFileHashes `json:"hashes"`
	// Env 声明该文件适用的运行面（mrpack 规范）：client / server 各为
	// required / optional / unsupported。客户端只关心 client。
	Env *modpackFileEnv `json:"env"`
	// CurseForge manifest 引用（无 downloads 直链、无 path）
	ProjectID *int  `json:"projectID"`
	FileID    *int  `json:"fileID"`
	Required  *bool `json:"required"`
}

// modpackFileEnv mrpack 条目的环境声明。
// client == "unsupported" 表示该文件是服务端专用：装到客户端上会导致
// 客户端启动崩溃或加载不存在的服务端 API，必须跳过。
type modpackFileEnv struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

// clientUnsupported 该条目在客户端是否被明确排除。
func (e *modpackFileEnv) clientUnsupported() bool {
	if e == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(e.Client), "unsupported")
}

// modpackFileHashes mrpack 声明文件的哈希集合。
type modpackFileHashes struct {
	SHA1   string `json:"sha1"`
	SHA512 string `json:"sha512"`
}

// InstallModpack 安装整合包到实例内容目录：
//  1. 解压包内所有文件（mods / config / saves / options.txt 等）到内容目录；
//  2. 解析 index，下载未包含在包内但声明了下载地址的 mods。
//
// 支持两种格式：
//   - Modrinth 的 .mrpack（modrinth.index.json / index.json，声明文件带下载地址）；
//   - CurseForge 的 .zip（manifest.json，mods 为 projectID/fileID 引用，
//     经官方 API（apiKey）换 CDN 直链下载；apiKey 为空时退回公开下载端点，
//     该端点现被 Cloudflare 人机验证拦截，通常不可用，错误信息会引导配 Key）。
//
// onStage 可选的阶段回调（可传 nil）：整合包安装分「解压」与「下载依赖」两段，
// 用于右下角下载中心显示当前下载阶段。
func InstallModpack(
	ctx context.Context,
	apiKey, mrpackPath, contentDirectory string,
	progress ProgressBytes,
	onStage ...func(string),
) (*ModpackInstallResult, error) {
	if strings.TrimSpace(mrpackPath) == "" {
		return nil, fmt.Errorf("mrpackPath 不能为空")
	}
	if strings.TrimSpace(contentDirectory) == "" {
		return nil, fmt.Errorf("contentDirectory 不能为空")
	}

	result := &ModpackInstallResult{Errors: []string{}, Warnings: []string{}}
	if err := os.MkdirAll(contentDirectory, 0o755); err != nil {
		return nil, err
	}
	reportStage := func(stage string) {
		if len(onStage) > 0 && onStage[0] != nil {
			onStage[0](stage)
		}
	}
	reportStage("解压整合包文件…")

	// 1) 解析索引：mrpack 用 modrinth.index.json（v1 标准）/ 旧版 index.json；
	//    CurseForge .zip 用 manifest.json（其 files 为 projectID/fileID 引用，
	//    在下方下载循环中经 CurseForge 公开端点解析真实文件）。
	archive, err := zip.OpenReader(mrpackPath)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	var index *modpackIndex
	inArchive := map[string]bool{}
	// 包根的启动器元数据文件不属于游戏内容：解压时跳过，
	// 否则导入 MultiMC zip 会把 mmc-pack.json / instance.cfg 等落进游戏目录。
	// 仅跳过包根（无子路径）的同名文件；overrides/ 内的同名文件不受影响。
	launcherMetadataNames := map[string]bool{
		"modrinth.index.json": true, "index.json": true, "manifest.json": true,
		"mmc-pack.json": true, "instance.cfg": true, ".packignore": true, "icon.png": true,
	}
	var totalArchiveBytes int64
	for _, entry := range archive.File {
		inArchive[strings.ReplaceAll(entry.Name, "\\", "/")] = true
		if !strings.HasSuffix(entry.Name, "/") {
			totalArchiveBytes += int64(entry.UncompressedSize64)
		}
	}

	var indexEntry *zip.File
	var mmcPackEntry *zip.File
	for i := range archive.File {
		name := archive.File[i].Name
		// 只认包根：overrides/ 里的同名文件是实例自身的元数据，不是本包的声明。
		if strings.Contains(strings.ReplaceAll(name, "\\", "/"), "/") {
			continue
		}
		lower := strings.ToLower(name)
		if lower == "modrinth.index.json" || lower == "index.json" || lower == "manifest.json" {
			indexEntry = archive.File[i]
			break
		}
		if lower == "mmc-pack.json" && mmcPackEntry == nil {
			mmcPackEntry = archive.File[i]
		}
	}

	// MultiMC / Prism 没有 files 声明（内容全在 overrides，运行要求写在
	// mmc-pack.json）。这里读它的声明元数据，让安装结果能与上层"声明的
	// 版本/加载器"核对——缺了这一步，MultiMC 包装完就只剩一堆无出处的文件。
	if indexEntry == nil && mmcPackEntry != nil {
		if reader, openErr := mmcPackEntry.Open(); openErr == nil {
			data, readErr := readAllLimited(reader, maximumIndexBytes)
			reader.Close()
			if readErr == nil {
				if req := requirementsFromMmcPack(data); req != nil {
					result.DetectedFormat = "multimc"
					applyDeclaredRequirements(result, req)
				}
			}
		}
	}
	if indexEntry != nil {
		reader, openErr := indexEntry.Open()
		if openErr == nil {
			// 限长读取：解压期的体积防护在这个阶段之后才生效，
			// 一个几 MB 的 deflate 全零条目能膨胀成几个 GB 撑爆内存
			data, readErr := readAllLimited(reader, maximumIndexBytes)
			reader.Close()
			if readErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("解析 index.json 失败：%v", readErr))
			} else {
				var parsed modpackIndex
				if err := json.Unmarshal(data, &parsed); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("解析 index.json 失败：%v", err))
				} else {
					index = &parsed
					// 清单本身不会留在实例目录里（上面按规范跳过启动器元数据），
					// 这里存一份快照，后续"整合包文件有没有被改动"才答得上来（X-4）。
					snapshotFormat := "modrinth"
					if strings.EqualFold(indexEntry.Name, "manifest.json") {
						snapshotFormat = "curseforge"
					}
					if err := persistModpackIndexSnapshot(
						contentDirectory, snapshotFormat, string(data)); err != nil {
						// 归类为提示而不是错误：文案自己就写着"不影响安装"，
						// 放进 Errors 会让"安装是否有失败"这个判断被一条无害的
						// 提示污染（调用方与用户都会把它当成安装失败）。
						result.Warnings = append(result.Warnings,
							fmt.Sprintf("保存整合包清单快照失败（不影响安装，仅影响后续改动检测）：%v", err))
					}
					// 回填清单声明的运行要求：安装结果要能回答"这包装在哪、
					// 声明的版本是什么"，否则用户只能看到一个文件数。
					result.DetectedFormat = snapshotFormat
					applyDeclaredRequirements(result, requirementsFromIndex(snapshotFormat, data, &parsed))
				}
			}
		}
	}

	// 2) 解压包内文件（按 mrpack 规范的层叠顺序，见 resolveModpackEntry）。
	//    防解压炸弹：条目数、单文件与累计解压字节超限时中止。
	totalFiles := 0
	for _, entry := range archive.File {
		if !strings.HasSuffix(entry.Name, "/") {
			totalFiles++
		}
	}
	if totalFiles > maximumArchiveEntries {
		return nil, fmt.Errorf("整合包含 %d 个文件条目，超过安全上限（%d）。", totalFiles, maximumArchiveEntries)
	}

	// mrpack 的 overrides 是层叠的：overrides 先，client-overrides 后（覆盖前者）。
	// 必须按固定轮次解压，不能顺着 zip 条目顺序——同一文件在两处都出现时，
	// 谁最后写入会取决于压缩包内部顺序，结果不可重现。
	// server-overrides 是服务端专用，客户端一律跳过。
	var extractedBytes int64
	for layer := 0; layer <= maximumModpackOverrideLayer; layer++ {
		for _, entry := range archive.File {
			if strings.HasSuffix(entry.Name, "/") {
				continue
			}
			if indexEntry != nil && strings.EqualFold(entry.Name, indexEntry.Name) {
				continue
			}
			normalizedEntryName := strings.ReplaceAll(entry.Name, "\\", "/")
			if !strings.Contains(normalizedEntryName, "/") && launcherMetadataNames[strings.ToLower(normalizedEntryName)] {
				continue
			}

			relative, entryLayer, install := resolveModpackEntry(normalizedEntryName)
			if !install || entryLayer != layer {
				continue
			}

			if err := extractEntry(ctx, entry, relative, contentDirectory, &extractedBytes, totalArchiveBytes, progress, result); err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				result.Errors = append(result.Errors, fmt.Sprintf("解压 %s 失败：%v", entry.Name, err))
				continue
			}
			result.InstalledFiles++
		}
	}

	// 3) 下载 index 中声明但不在包内的文件（mods / resourcepacks / shaderpacks 等）。
	//    解析与下载分离：依赖地址解析（CurseForge API 换直链，有频控）保持串行，
	//    下载阶段交给高速小文件下载器并行跑（并发数 = 全局并行下载设置），
	//    进度按文件聚合，与解压字节拼成整体进度。
	if index != nil && len(index.Files) > 0 {
		type depJob struct {
			label      string
			targetPath string
			url        string
			// expectedSHA1 / expectedSHA512 清单声明的哈希，用于下载后校验
			// 完整性（mrpack 规范要求每个声明文件都带 sha1 与 sha512）。
			expectedSHA1   string
			expectedSHA512 string
		}
		var deps []depJob
		for i := range index.Files {
			if ctx.Err() != nil {
				break
			}
			file := &index.Files[i]
			// CurseForge manifest 条目没有 path，只有 projectID/fileID + required
			isCurseForgeRef := file.Path == "" && file.ProjectID != nil && *file.ProjectID > 0 &&
				file.FileID != nil && *file.FileID > 0
			if strings.TrimSpace(file.Path) == "" && !isCurseForgeRef {
				continue
			}
			if file.Required != nil && !*file.Required {
				continue // 可选依赖不自动下载
			}
			// mrpack 的 env 声明该文件适用的运行面：client 明确为 unsupported
			// 表示这是服务端专用文件（即使 required 也不该装到客户端——
			// 装上去轻则报错重则直接崩），必须跳过。
			if file.Env.clientUnsupported() {
				continue
			}
			// 解压阶段会按层叠规则剥离包内根前缀，因此依赖路径匹配必须能
			// 识别带前缀的写法，否则包内已附带的依赖会被误判为缺失而联网重下。
			if file.Path != "" {
				normalized := strings.ReplaceAll(file.Path, "\\", "/")
				if modpackPathInArchive(inArchive, normalized) {
					continue // 文件已包含在包内，解压步骤已处理
				}
			}

			// CurseForge 引用走官方 API 换直链（含文件名）；其它按声明直链下载
			var downloadURL, cfFileName string
			if isCurseForgeRef {
				downloadURL, cfFileName = resolveCurseForgeDependency(ctx, apiKey, file)
				if strings.TrimSpace(downloadURL) == "" {
					result.Errors = append(result.Errors,
						fmt.Sprintf("无法解析 CurseForge 依赖 %d/%d 的下载地址，已跳过。", *file.ProjectID, *file.FileID))
					continue
				}
			} else {
				downloadURL = resolveDeclaredFileURL(file, result)
				if strings.TrimSpace(downloadURL) == "" {
					continue
				}
			}

			// mrpack：file.Path 是相对实例根的路径（如 mods/foo.jar），直接拼到内容目录；
			// CurseForge 引用：解析 CDN 文件名后落到 mods/ 目录
			var targetPath string
			if file.Path != "" {
				combined, err := safeCombine(contentDirectory, strings.ReplaceAll(file.Path, "\\", "/"))
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("跳过依赖 %s：%v", file.Path, err))
					continue
				}
				targetPath = combined
			} else {
				fileName := cfFileName
				if strings.TrimSpace(fileName) == "" {
					fileName = resolveCurseForgeFileName(ctx, downloadURL)
				}
				if strings.TrimSpace(fileName) == "" {
					result.Errors = append(result.Errors,
						fmt.Sprintf("无法解析 CurseForge 依赖 %d/%d 的文件名，已跳过。若持续出现，请在设置页配置 CurseForge API Key 后重试。",
							*file.ProjectID, *file.FileID))
					continue
				}
				combined, err := safeCombine(contentDirectory, "mods/"+fileName)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("跳过 CurseForge 依赖 %s：%v", fileName, err))
					continue
				}
				targetPath = combined
			}

			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("下载依赖 %s 失败：%v", file.Path, err))
				continue
			}
			label := file.Path
			if label == "" {
				label = cfFileName
			}
			if label == "" && file.ProjectID != nil && file.FileID != nil {
				label = fmt.Sprintf("%d/%d", *file.ProjectID, *file.FileID)
			}
			deps = append(deps, depJob{
				label:          label,
				targetPath:     targetPath,
				url:            downloadURL,
				expectedSHA1:   declaredSHA1(file.Hashes),
				expectedSHA512: declaredSHA512(file.Hashes),
			})
		}

		if len(deps) > 0 {
			jobs := make([]SmallFileJob, len(deps))
			for i, dep := range deps {
				jobs[i] = SmallFileJob{
					Name:           dep.label,
					URL:            dep.url,
					TargetPath:     dep.targetPath,
					ExpectedSHA1:   dep.expectedSHA1,
					ExpectedSHA512: dep.expectedSHA512,
				}
			}
			// 进度聚合：每个文件回调更新自己的计数，汇总成整体字节数；
			// 总量 = 已解压字节 + 依赖声明字节（依赖总量未知时保持 0，UI 走不定进度）
			var progressMu sync.Mutex
			fileDownloaded := make([]int64, len(jobs))
			fileTotal := make([]int64, len(jobs))
			reportAggregate := func(index int, downloaded, total int64) {
				var sumDownloaded, sumTotal int64

				progressMu.Lock()
				fileDownloaded[index] = downloaded
				fileTotal[index] = total
				for j := range fileDownloaded {
					sumDownloaded += fileDownloaded[j]
					sumTotal += fileTotal[j]
				}
				progressMu.Unlock()
				if progress != nil {
					progress(totalArchiveBytes+sumDownloaded, totalArchiveBytes+sumTotal)
				}
			}
			reportStage("下载依赖文件…")
			errs := DownloadSmallFiles(ctx, jobs, ParallelDownloads(), reportAggregate)
			for _, err := range errs {
				if err == nil {
					result.DownloadedMods++

					continue
				}
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				result.Errors = append(result.Errors, fmt.Sprintf("下载依赖失败：%v", err))
			}
		}
	}

	// 4) 一致性提示：装了但装不完整是最糟的结果——包解开了、依赖也下了，
	//    启动却因为"少了加载器"崩掉，而界面只报一个文件数。这里把"声明的
	//    加载器本启动器装不了"显式说出来，让用户知道要自己补哪一步。
	if result.DeclaredLoaderName != "" && !result.DeclaredLoaderSupported {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"整合包声明的加载器 %s %s 暂不受支持：文件已解压，但启动器不会自动安装该加载器，"+
				"需要手动装好后再启动。",
			result.DeclaredLoaderName, result.DeclaredLoaderVersion))
	}

	return result, nil
}

// applyDeclaredRequirements 把清单声明的运行要求回填到安装结果。
// 加载器名优先用识别出的展示名；识别不出但清单确实写了加载器键时退回原始键
// （如 "rift-loader"）——否则"声明了不受支持的加载器"永远没有机会被告警出来
// （识别出的类型只可能是受支持的四种，告警分支就成了死代码）。
func applyDeclaredRequirements(result *ModpackInstallResult, req *ModpackRequirements) {
	if result == nil || req == nil {
		return
	}
	result.DeclaredMinecraftVersion = req.MinecraftVersion
	result.DeclaredLoaderVersion = req.LoaderVersion
	result.DeclaredLoaderSupported = loaderInstallable(req)
	if req.LoaderType != ModLoaderVanilla {
		result.DeclaredLoaderName = loaderDisplayName(req.LoaderType)

		return
	}
	result.DeclaredLoaderName = strings.TrimSpace(req.RawLoaderKey)
}

// verifyDeclaredHashes 按清单声明的哈希校验下载结果：声明了哪个就校验哪个，
// 两者都声明就都校验（一次读取同时算两种摘要）。任何一项不一致都删除文件并报错。
//
// 为什么不能只认 SHA-1：mrpack 规范要求 sha1 与 sha512 都提供，但现实里存在
// 只写 sha512 的包——那些文件此前完全得不到校验；两者都给时，只比 SHA-1 也
// 白白浪费了更强的那个摘要。
//
// 只校验"格式正确"的哈希：清单里可能写着占位符或被写坏的值（长度不对、
// 非十六进制），拿那种值去比对会把一个本来正确的下载判成校验失败并删掉。
func verifyDeclaredHashes(path, expectedSHA1, expectedSHA512 string) error {
	wantSHA1 := isHexDigest(strings.TrimSpace(expectedSHA1), sha1.Size*2)
	wantSHA512 := isHexDigest(strings.TrimSpace(expectedSHA512), sha512.Size*2)
	if !wantSHA1 && !wantSHA512 {
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("下载结果不可读：%w", err)
	}

	var sha1Hasher, sha512Hasher hash.Hash
	var writers []io.Writer
	if wantSHA1 {
		sha1Hasher = sha1.New()
		writers = append(writers, sha1Hasher)
	}
	if wantSHA512 {
		sha512Hasher = sha512.New()
		writers = append(writers, sha512Hasher)
	}
	_, copyErr := io.Copy(io.MultiWriter(writers...), file)
	// 显式关闭而不是 defer：下面校验失败时要在本函数内删文件，
	// 而 Windows 不允许删除仍被打开的文件（defer 到返回才关就来不及了）。
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("校验下载文件失败：%w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("校验下载文件失败：%w", closeErr)
	}

	checks := []struct {
		name    string
		want    string
		hasher  hash.Hash
		enabled bool
	}{
		{"SHA-1", strings.ToLower(strings.TrimSpace(expectedSHA1)), sha1Hasher, wantSHA1},
		{"SHA-512", strings.ToLower(strings.TrimSpace(expectedSHA512)), sha512Hasher, wantSHA512},
	}
	for _, check := range checks {
		if !check.enabled {
			continue
		}
		actual := hex.EncodeToString(check.hasher.Sum(nil))
		if actual != check.want {
			// 残缺/被篡改的内容留着会被游戏当正常 mod 加载，必须删掉
			if removeErr := os.Remove(path); removeErr != nil {
				return fmt.Errorf("下载文件校验失败（%s 不一致）：期望 %s，实际 %s；且删除失败：%v",
					check.name, check.want, actual, removeErr)
			}
			return fmt.Errorf("下载文件校验失败（%s 不一致，已删除下载物）：期望 %s，实际 %s",
				check.name, check.want, actual)
		}
	}
	return nil
}

// loaderInstallable 该整合包声明的加载器能否由本启动器自动安装。
// 判据统一走 ModpackRequirements.LoaderSupported（而不是在别处再写一份
// 加载器名单）：原版包没有加载器要装，视为可安装；声明了但识别不出的
// 加载器（LoaderType 落到 Vanilla）不可安装。
func loaderInstallable(req *ModpackRequirements) bool {
	if req == nil {
		return true
	}
	if strings.TrimSpace(req.RawLoaderKey) == "" {
		return true
	}
	return req.LoaderSupported()
}

// maximumModpackOverrideLayer 包内内容的最高层叠层号（见 resolveModpackEntry）。
const maximumModpackOverrideLayer = 2

// modpackContentRoots 包内游戏内容的根前缀与层叠层号（层号大的覆盖小的）。
//
// 安装（解压）与校验（清单比对）必须共用这一份定义：任何一边漏掉某种前缀，
// 都会造成"装得上但校验报缺失"（或反过来）这种自相矛盾的结果 ——
// client-overrides / .minecraft 就曾在两边各漏过一次。
var modpackContentRoots = []struct {
	Prefix string
	Layer  int
}{
	{"overrides/", 1},
	{"client-overrides/", 2},
	{".minecraft/", 1},
}

// serverOverridesPrefix 服务端专用内容的根前缀：客户端一律不安装。
const serverOverridesPrefix = "server-overrides/"

// resolveModpackEntry 解析包内条目：返回相对实例根的路径、层叠层号，
// 以及该条目是否应安装到客户端。
//
// 各格式的内容根前缀与层叠关系：
//
//	overrides/         → 层 1（mrpack / CurseForge：客户端与服务端共用）
//	client-overrides/  → 层 2（mrpack：客户端专用，覆盖 overrides 的同名文件）
//	server-overrides/  → 不安装（mrpack：服务端专用，客户端装上去会崩）
//	.minecraft/        → 层 1（MultiMC / Prism：其导入器按此查找游戏文件）
//	（无前缀）          → 层 0（包根直接内容）
//
// client-overrides 与 server-overrides 是 mrpack 规范的正式组成部分，
// 不处理会导致前者的文件落成字面的 "client-overrides/…" 目录（内容不生效）、
// 后者的服务端专用文件被错误装进客户端。
func resolveModpackEntry(name string) (relative string, layer int, install bool) {
	normalized := strings.ReplaceAll(name, "\\", "/")
	if hasPathPrefix(normalized, serverOverridesPrefix) {
		return "", 0, false
	}
	for _, root := range modpackContentRoots {
		if hasPathPrefix(normalized, root.Prefix) {
			return normalized[len(root.Prefix):], root.Layer, true
		}
	}
	return normalized, 0, true
}

// contentRootCandidates 清单声明的相对路径在磁盘/压缩包上可能的写法：
// 先按原样，再依次补上各内容根前缀。校验时用它容忍"前缀未被剥离"的布局
// （外部工具解压出来的实例），安装时用它判断文件是否已随包附带。
func contentRootCandidates(declared string) []string {
	candidates := []string{declared}
	for _, root := range modpackContentRoots {
		candidates = append(candidates, root.Prefix+declared)
	}
	return candidates
}

// modpackPathInArchive 清单声明的路径是否已包含在包内
// （含 overrides/、client-overrides/、.minecraft/ 等带前缀的写法）。
func modpackPathInArchive(inArchive map[string]bool, declared string) bool {
	for _, candidate := range contentRootCandidates(declared) {
		if inArchive[candidate] {
			return true
		}
	}
	return false
}

// hasPathPrefix 大小写不敏感的前缀比对（zip 条目名在不同打包器下大小写不一）。
func hasPathPrefix(path, prefix string) bool {
	return len(path) >= len(prefix) && strings.EqualFold(path[:len(prefix)], prefix)
}

// extractEntry 解压单个 zip 条目（含大小与累计字节防护）。
// relative 是已由 resolveModpackEntry 解析好的实例内相对路径。
func extractEntry(
	ctx context.Context,
	entry *zip.File,
	relative string,
	contentDirectory string,
	extractedBytes *int64,
	totalArchiveBytes int64,
	progress ProgressBytes,
	result *ModpackInstallResult,
) error {
	if int64(entry.UncompressedSize64) > maximumEntryBytes {
		return fmt.Errorf("条目 %s 大小 %d MB 超过单文件上限。", entry.Name, entry.UncompressedSize64/1024/1024)
	}
	*extractedBytes += int64(entry.UncompressedSize64)
	if *extractedBytes > maximumExtractedBytes {
		return fmt.Errorf("累计解压字节超过安全上限（%d MB），疑似压缩炸弹。", maximumExtractedBytes/1024/1024)
	}

	destination, err := safeCombine(contentDirectory, relative)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	reader, err := entry.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	writer, err := os.OpenFile(destination, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer writer.Close()
	// 头部声明的 UncompressedSize64 可被恶意 zip 伪造，实际解压字节也要受限，
	// 防止压缩炸弹绕过开头的声明大小检查（LimitReader 兜底最大写入量）。
	written, err := io.Copy(writer, io.LimitReader(reader, maximumEntryBytes+1))
	if err != nil {
		return err
	}
	if written > maximumEntryBytes {
		return fmt.Errorf("条目 %s 实际解压大小超过单文件上限，疑似压缩炸弹。", entry.Name)
	}
	// 累计口径按实际字节修正（声明值可能失真）
	*extractedBytes += written - int64(entry.UncompressedSize64)
	if *extractedBytes > maximumExtractedBytes {
		return fmt.Errorf("累计解压字节超过安全上限（%d MB），疑似压缩炸弹。", maximumExtractedBytes/1024/1024)
	}
	// 字节口径与依赖下载一致（UI 按 MB 展示）
	if progress != nil {
		progress(*extractedBytes, totalArchiveBytes)
	}
	return nil
}

// curseForgeMetadataClient CurseForge 依赖解析专用客户端：禁用自动重定向以读取
// 302 Location 中的文件名（默认会自动跟随 302 到 CDN，最终 200 无文件名信息）。
var curseForgeMetadataClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse // 不跟随重定向
	},
	Timeout: 30 * time30Seconds,
}

const time30Seconds = 30 * time.Second

// resolveCurseForgeFileName 解析 CurseForge 公开下载端点的真实文件名：
// 端点返回 302，Location 指向 edge.forgecdn.net 的 CDN 地址（尾段即文件名）。
func resolveCurseForgeFileName(ctx context.Context, downloadURL string) string {
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return ""
	}
	// 这一步也可能直接打 edge.forgecdn.net（公开端点 302 之后），同样要带 Key。
	applyCurseForgeCDNAuth(req)
	resp, err := curseForgeMetadataClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusFound, http.StatusMovedPermanently,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		location := resp.Header.Get("Location")
		if strings.TrimSpace(location) == "" {
			return ""
		}
		if decoded, err := url.QueryUnescape(location); err == nil {
			location = decoded
		}
		segments := strings.Split(location, "/")
		for i := len(segments) - 1; i >= 0; i-- {
			if segments[i] != "" {
				return segments[i]
			}
		}
		return ""
	}

	// 未重定向（直接 200）时尝试 Content-Disposition
	disposition := resp.Header.Get("Content-Disposition")
	if filename := parseContentDispositionFileName(disposition); filename != "" {
		return filename
	}
	return ""
}

// resolveDeclaredFileURL 解析声明文件的下载地址：优先直链（Modrinth mrpack）；
// 无直链但带 projectID/fileID 时（CurseForge manifest）构造公开下载端点兜底，
// 该端点现被 Cloudflare 人机验证拦截（403），仅在没配 API Key 时作为最后尝试。
func resolveDeclaredFileURL(file *modpackFileEntry, result *ModpackInstallResult) string {
	for _, candidate := range file.Downloads {
		if isWellFormedHTTPURL(candidate) {
			return candidate
		}
	}

	if file.ProjectID != nil && *file.ProjectID > 0 && file.FileID != nil && *file.FileID > 0 {
		return fmt.Sprintf("https://www.curseforge.com/api/v1/mods/%d/files/%d/download", *file.ProjectID, *file.FileID)
	}

	detail := "（且非 CurseForge 引用）"
	if file.ProjectID != nil && *file.ProjectID > 0 {
		detail = "（fileID 缺失）"
	}
	result.Errors = append(result.Errors,
		fmt.Sprintf("依赖 %s 无可用下载地址%s，已跳过。", file.Path, detail))
	return ""
}

// resolveCurseForgeDependency 解析 CurseForge 依赖（projectID/fileID）的下载地址与文件名。
//
// 首选官方 API（x-api-key）：一次同时拿到文件名与 CDN 直链。Cloudflare 已给
// www.curseforge.com 的公开下载端点上了人机验证（403 "Just a moment…"），旧注释
// "该端点 302 到 CDN、无需 API key" 已失效；没配 Key 时仍返回公开端点作最后兜底
// （大概率失败，上层错误信息会引导用户去设置页配 Key）。
func resolveCurseForgeDependency(
	ctx context.Context,
	apiKey string,
	file *modpackFileEntry,
) (downloadURL, fileName string) {
	const fallbackURL = "https://www.curseforge.com/api/v1/mods/%d/files/%d/download"
	if strings.TrimSpace(apiKey) == "" {
		return fmt.Sprintf(fallbackURL, *file.ProjectID, *file.FileID), ""
	}

	modID := strconv.Itoa(*file.ProjectID)
	fileID := strconv.Itoa(*file.FileID)
	info, err := curseforge.GetFile(ctx, apiKey, modID, fileID)
	if err == nil && info != nil {
		if url := strings.TrimSpace(resolveCFDownloadLink(ctx, apiKey, modID, fileID, info)); url != "" {
			return url, strings.TrimSpace(info.FileName)
		}
	}
	// API 失败（网络/限流）时回退公开端点，文件名交给调用方用 302 解析再试
	return fmt.Sprintf(fallbackURL, *file.ProjectID, *file.FileID), ""
}

// resolveCFDownloadLink 由文件信息算出可下载直链：downloadUrl 非空用之；
// 作者禁止第三方分发时官方的 download-url 端点会返回 403，这里如实交空。
func resolveCFDownloadLink(
	ctx context.Context,
	apiKey, modID, fileID string,
	info *models.CurseForgeFile,
) string {
	if info != nil && info.DownloadURL != nil {
		if direct := strings.TrimSpace(*info.DownloadURL); direct != "" {
			return direct
		}
	}
	if resolved, err := curseforge.ResolveDownloadURL(ctx, apiKey, modID, fileID); err == nil {
		return strings.TrimSpace(resolved)
	}
	return ""
}

// DownloadModpackFile 下载整合包文件（.mrpack）到自定义路径或实例目录。
// 用于"自定义保存路径"场景：仅保存文件，不做解压。
func DownloadModpackFile(ctx context.Context, downloadURL, fileName, targetPath string, progress ProgressBytes) error {
	return DownloadFileToPath(ctx, downloadURL, targetPath, progress)
}

// safeCombine 安全拼接：确保解压目标位于内容目录内，阻止路径穿越。
// 越界时返回错误而不是 panic：条目名来自用户导入的整合包，一个畸形条目
// 只该被跳过并记录，不该让整个导入崩掉。
func safeCombine(root, relativePath string) (string, error) {
	normalized := strings.ReplaceAll(relativePath, "\\", "/")
	if strings.HasPrefix(normalized, "/") || hasParentSegment(normalized) {
		return "", fmt.Errorf("非法的整合包内路径：%s", relativePath)
	}

	rootFull := filepath.Clean(root)
	combined := filepath.Clean(filepath.Join(rootFull, filepath.FromSlash(normalized)))
	if combined != rootFull && !strings.HasPrefix(strings.ToLower(combined), strings.ToLower(rootFull+string(filepath.Separator))) {
		return "", fmt.Errorf("整合包路径越界：%s", relativePath)
	}
	return combined, nil
}

// hasParentSegment 按路径段判断是否含 ".."，避免误伤 "foo..bar.toml" 这类合法文件名。
func hasParentSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func sanitizeFileName(fileName string) string {
	name := strings.Trim(strings.TrimSpace(fileName), "\"")
	name = filepath.Base(name)
	name = sanitizeSegment(name)
	if strings.TrimSpace(name) == "" {
		return "download"
	}
	return name
}

// ---------------------------------------------------------------------------
// 整合包依赖（index.json -> dependencies）
// ---------------------------------------------------------------------------

// ModpackRequirements 整合包声明的运行要求，解析自 mrpack 的 index.json dependencies。
// 例如 Fabulously Optimized：{ "fabric-loader": "0.19.3", "minecraft": "26.2" }。
type ModpackRequirements struct {
	// MinecraftVersion 要求的 Minecraft 版本，如 "1.21.8"。
	MinecraftVersion string
	// LoaderType 加载器类型。无加载器键时为 Vanilla；遇到不支持的加载器键时保持
	// Vanilla，并通过 RawLoaderKey 暴露原始键以便上层告警。
	LoaderType ModLoaderType
	// LoaderVersion 加载器版本，如 "0.19.3"。
	LoaderVersion string
	// RawLoaderKey 原始加载器依赖键（如 "fabric-loader" / "quilt-loader"），用于不支持时告警。
	RawLoaderKey string
}

// LoaderSupported 本启动器是否能安装该加载器（Fabric / Quilt / NeoForge / Forge 支持）。
func (r *ModpackRequirements) LoaderSupported() bool {
	switch r.LoaderType {
	case ModLoaderFabric, ModLoaderQuilt, ModLoaderNeoForge, ModLoaderForge:
		return true
	default:
		return false
	}
}

// ReadModpackRequirements 解析整合包的版本要求。
// 支持：mrpack 的 modrinth.index.json / index.json（dependencies 字典），
// CurseForge 的 manifest.json（minecraft.version + minecraft.modLoaders[].id），
// 以及 MultiMC / Prism 的 mmc-pack.json（components[].uid + version）。
// 解析失败（无索引 / 无 minecraft 依赖 / JSON 损坏）时返回 nil。
func ReadModpackRequirements(ctx context.Context, mrpackPath string) (*ModpackRequirements, error) {
	if strings.TrimSpace(mrpackPath) == "" {
		return nil, fmt.Errorf("mrpackPath 不能为空")
	}

	archive, err := zip.OpenReader(mrpackPath)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	var mrpackEntry, manifestEntry, mmcPackEntry *zip.File
	for i := range archive.File {
		// 只认包根的同名文件：MultiMC zip 的 overrides/ 里可能带着实例自己的
		// mmc-pack.json，那不是本包声明的运行要求。
		if strings.Contains(strings.ReplaceAll(archive.File[i].Name, "\\", "/"), "/") {
			continue
		}
		lower := strings.ToLower(archive.File[i].Name)
		switch lower {
		case "modrinth.index.json", "index.json":
			if mrpackEntry == nil {
				mrpackEntry = archive.File[i]
			}
		case "manifest.json":
			if manifestEntry == nil {
				manifestEntry = archive.File[i]
			}
		case "mmc-pack.json":
			if mmcPackEntry == nil {
				mmcPackEntry = archive.File[i]
			}
		}
	}

	if mrpackEntry != nil {
		reader, openErr := mrpackEntry.Open()
		if openErr != nil {
			return nil, openErr
		}
		data, readErr := readAllLimited(reader, maximumIndexBytes)
		reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		var index modpackIndex
		if err := json.Unmarshal(data, &index); err != nil || index.Dependencies == nil {
			return nil, nil
		}
		return requirementsFromDependencies(index.Dependencies), nil
	}

	// CurseForge manifest.json：minecraft.version + modLoaders[].id（如 "forge-47.2.0"）
	if manifestEntry != nil {
		reader, openErr := manifestEntry.Open()
		if openErr != nil {
			return nil, openErr
		}
		data, readErr := readAllLimited(reader, maximumIndexBytes)
		reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		return requirementsFromCurseForgeManifest(data), nil
	}

	// MultiMC / Prism mmc-pack.json：components[].uid + version。
	// net.minecraft 给 MC 版本，加载器组件的 uid 形如 "net.fabricmc.fabric-loader" /
	// "net.minecraftforge" / "net.neoforged.neoforge" / "org.quiltmc.quilt-loader"。
	if mmcPackEntry != nil {
		reader, openErr := mmcPackEntry.Open()
		if openErr != nil {
			return nil, openErr
		}
		data, readErr := readAllLimited(reader, maximumIndexBytes)
		reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		return requirementsFromMmcPack(data), nil
	}

	return nil, nil
}

// requirementsFromMmcPack 解析 MultiMC / Prism 的 mmc-pack.json。
// 由 buildMultiMcComponents 的导出格式定义：
//
//	{"formatVersion":1,"components":[
//	   {"uid":"net.minecraft","version":"1.20.1"},
//	   {"uid":"net.minecraftforge","version":"47.2.0"}]}
//
// 缺少 net.minecraft 组件（或解析失败）时返回 nil——与其它格式口径一致，
// 让上层走"未识别到版本要求"的既有分支，而不是给出半个要求。
func requirementsFromMmcPack(data []byte) *ModpackRequirements {
	var root struct {
		Components []struct {
			Uid     string `json:"uid"`
			Version string `json:"version"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil
	}

	req := &ModpackRequirements{}
	for _, component := range root.Components {
		uid := strings.ToLower(strings.TrimSpace(component.Uid))
		version := strings.TrimSpace(component.Version)
		if version == "" {
			continue
		}
		// 基础游戏：net.minecraft（MultiMC 的官方 uid）
		if uid == "net.minecraft" {
			if req.MinecraftVersion == "" {
				req.MinecraftVersion = version
			}

			continue
		}
		// 加载器组件：取第一个能识别的，命中即定（与 mrpack 的"最多一个加载器"口径一致）
		if req.RawLoaderKey != "" {
			continue
		}
		if rawKey, loaderType, ok := loaderFromMmcUid(uid); ok {
			req.RawLoaderKey = rawKey
			req.LoaderType = loaderType
			req.LoaderVersion = version
		}
	}

	if strings.TrimSpace(req.MinecraftVersion) == "" {
		return nil
	}
	return req
}

// loaderFromMmcUid 把 MultiMC 组件 uid 映射到启动器的加载器类型。
// 使用前缀匹配：MultiMC 的 uid 有历史变体（如 net.minecraftforge 与
// net.minecraftforge.legacy），前缀比对能一并接住，且不会误判 net.minecraft。
func loaderFromMmcUid(uid string) (rawKey string, loaderType ModLoaderType, ok bool) {
	switch {
	case strings.HasPrefix(uid, "net.neoforged"):
		return "neoforge", ModLoaderNeoForge, true
	case strings.HasPrefix(uid, "net.fabricmc.fabric-loader"):
		return "fabric-loader", ModLoaderFabric, true
	case strings.HasPrefix(uid, "org.quiltmc.quilt-loader"):
		return "quilt-loader", ModLoaderQuilt, true
	// net.minecraftforge 必须排在 net.minecraft 之后判定：先落到这里的
	// 一定是带 forge 后缀的 uid，不会被基础游戏组件抢走。
	case strings.HasPrefix(uid, "net.minecraftforge"):
		return "forge", ModLoaderForge, true
	default:
		return "", ModLoaderVanilla, false
	}
}

// requirementsFromIndex 从已解析的清单原文里取运行要求。
// 与 ReadModpackRequirements 同一口径，但复用安装阶段已经读进内存的字节，
// 避免安装时为了回填元数据把清单再解一遍。
func requirementsFromIndex(format string, data []byte, parsed *modpackIndex) *ModpackRequirements {
	if format == "curseforge" {
		return requirementsFromCurseForgeManifest(data)
	}
	if parsed == nil || parsed.Dependencies == nil {
		return nil
	}
	return requirementsFromDependencies(parsed.Dependencies)
}

// requirementsFromCurseForgeManifest 解析 CurseForge manifest.json 的
// minecraft.version 与 modLoaders[].id（如 "forge-47.2.0"）。
func requirementsFromCurseForgeManifest(data []byte) *ModpackRequirements {
	var root struct {
		Minecraft *struct {
			Version    string `json:"version"`
			ModLoaders []struct {
				ID      string `json:"id"`
				Primary bool   `json:"primary"`
			} `json:"modLoaders"`
		} `json:"minecraft"`
	}
	if err := json.Unmarshal(data, &root); err != nil || root.Minecraft == nil || root.Minecraft.Version == "" {
		return nil
	}
	req := &ModpackRequirements{MinecraftVersion: root.Minecraft.Version}
	// primary 优先，缺省取第一个；id 形如 "forge-47.2.0" / "fabric-0.15.11"
	for _, loader := range root.Minecraft.ModLoaders {
		if loader.ID == "" {
			continue
		}
		req.RawLoaderKey, req.LoaderType, req.LoaderVersion = parseLoaderID(loader.ID)
		if req.LoaderType != ModLoaderVanilla || loader.Primary {
			break
		}
	}
	return req
}

// loaderDisplayName 加载器类型 → 展示名（与前端 LOADER_NAMES 一致）。
func loaderDisplayName(loaderType ModLoaderType) string {
	switch loaderType {
	case ModLoaderFabric:
		return "Fabric"
	case ModLoaderQuilt:
		return "Quilt"
	case ModLoaderForge:
		return "Forge"
	case ModLoaderNeoForge:
		return "NeoForge"
	default:
		return "原版"
	}
}

// parseLoaderID 解析 "forge-47.2.0" 形式的加载器 id。
func parseLoaderID(loaderID string) (rawKey string, loaderType ModLoaderType, loaderVersion string) {
	separator := strings.Index(loaderID, "-")
	if separator <= 0 {
		rawKey = loaderID
	} else {
		rawKey = loaderID[:separator]
		loaderVersion = loaderID[separator+1:]
	}
	if t, ok := mapLoaderKey(rawKey); ok {
		loaderType = t
		if strings.TrimSpace(loaderVersion) == "" {
			loaderVersion = ""
		}
	} else {
		loaderType = ModLoaderVanilla
	}
	return rawKey, loaderType, loaderVersion
}

func requirementsFromDependencies(deps map[string]string) *ModpackRequirements {
	mc := deps["minecraft"]
	if strings.TrimSpace(mc) == "" {
		return nil
	}

	req := &ModpackRequirements{MinecraftVersion: mc}

	// 取加载器依赖（mrpack 规范最多一个）。按固定顺序扫，不要直接 range map：
	// 万一包里同时写了多个加载器键，map 的随机遍历顺序会让同一份整合包
	// 每次解析出不同的加载器。
	for _, key := range []string{"fabric-loader", "quilt-loader", "neoforge", "forge"} {
		value, ok := deps[key]
		if !ok {
			continue
		}
		req.RawLoaderKey = key
		req.LoaderType, _ = mapLoaderKey(key)
		req.LoaderVersion = value

		return req
	}
	// 兜底：大小写变体或未知键。未知加载器（如 "rift-loader"）保持 Vanilla，
	// 但原始键与版本号都要留下，上层据此提示"声明了但本启动器装不了"。
	for key, value := range deps {
		if strings.EqualFold(key, "minecraft") {
			continue
		}
		req.RawLoaderKey = key
		// 版本号无论是否识别出加载器都要记：识别不出时报"声明的是这种加载器
		// 的哪个版本"才完整，缺了版本号提示等于没说清楚。
		req.LoaderVersion = value
		if t, ok := mapLoaderKey(key); ok {
			req.LoaderType = t
		}

		break
	}
	return req
}

func mapLoaderKey(key string) (ModLoaderType, bool) {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	// mrpack 用 "fabric-loader"；CurseForge manifest 的 modLoaders id 前缀是 "fabric"
	case "fabric-loader", "fabric":
		return ModLoaderFabric, true
	case "quilt-loader", "quilt":
		return ModLoaderQuilt, true
	case "neoforge":
		return ModLoaderNeoForge, true
	case "forge":
		return ModLoaderForge, true
	default:
		return ModLoaderVanilla, false
	}
}

// ---- 小工具 ----

func isWellFormedHTTPURL(raw string) bool {
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}

func parseContentDispositionFileName(disposition string) string {
	if disposition == "" {
		return ""
	}
	lower := strings.ToLower(disposition)
	idx := strings.Index(lower, "filename")
	if idx < 0 {
		return ""
	}
	rest := disposition[idx+len("filename"):]
	rest = strings.TrimLeft(rest, " ")
	if strings.HasPrefix(rest, "*=") {
		rest = rest[2:]
	} else if strings.HasPrefix(rest, "=") {
		rest = rest[1:]
	} else {
		return ""
	}
	rest = strings.Trim(strings.TrimSpace(rest), "\"")
	return rest
}
