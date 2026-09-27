package download

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nekolauncher/internal/tools"
)

// StageCount 安装总阶段数。
const StageCount = 7

// downloadBufferSize 下载缓冲区大小。
const downloadBufferSize = 128 * 1024

// MinecraftInstallProgress 安装进度快照：阶段、字节数与文件数，百分比由字节数推导。
type MinecraftInstallProgress struct {
	StageIndex     int
	StageName      string
	Detail         string
	CompletedBytes int64
	TotalBytes     int64
	CompletedFiles int
	TotalFiles     int
	BytesPerSecond float64
}

// Percentage 百分比（0-100）。
func (p MinecraftInstallProgress) Percentage() float64 {
	if p.TotalBytes <= 0 {
		return 0
	}
	pct := float64(p.CompletedBytes) * 100 / float64(p.TotalBytes)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

// InstallProgressFunc 安装进度回调。对应 C# IProgress<MinecraftInstallProgress>；
// 回调在下载 goroutine 上同步触发（C# Progress<T> 会封送到 UI 线程，
// Go/Wails 侧需自行调度或对接 EventsEmit，见 PORTING_NOTES.md）。
type InstallProgressFunc func(MinecraftInstallProgress)

// MinecraftVersionInstaller 从 Mojang 官方元数据安装原版 Minecraft 版本。
// SHA-1 匹配的已有文件直接复用；下载先写临时文件、校验通过后才移动到位。
type MinecraftVersionInstaller struct{}

// progressReportInterval 进度上报节流间隔。
const progressReportInterval = 120 * time.Millisecond

type downloadFile struct {
	url         string
	targetPath  string
	sha1        string
	size        int64
	displayName string
}

// installCounters 跨阶段共享的安装计数器：已完成字节、网络字节、已完成文件与清单总量。
// 各下载任务并发更新，读取方使用 atomic 保证可见性。
type installCounters struct {
	completedBytes atomic.Int64
	networkBytes   atomic.Int64
	completedFiles atomic.Int64
	totalBytes     atomic.Int64
	totalFiles     atomic.Int64
}

// addProgressBytes 下载进度推进（可传负数用于失败重试时回滚，网络字节同步回退）。
func (c *installCounters) addProgressBytes(value int64) {
	c.networkBytes.Add(value)
	c.completedBytes.Add(value)
}

func (c *installCounters) addCompleted(value int64)  { c.completedBytes.Add(value) }
func (c *installCounters) incrementCompletedFiles()  { c.completedFiles.Add(1) }
func (c *installCounters) addTotalBytes(value int64) { c.totalBytes.Add(value) }
func (c *installCounters) addTotalFiles(value int64) { c.totalFiles.Add(value) }

// Install 安装指定版本：先下载版本元数据，再执行完整安装流程。
func (i *MinecraftVersionInstaller) Install(
	ctx context.Context,
	versionID, metadataURL, minecraftDirectory string,
	progress InstallProgressFunc,
) error {
	if err := validateInstallArgs(versionID, metadataURL, minecraftDirectory); err != nil {
		return err
	}
	if err := ValidateVersionID(versionID); err != nil {
		return err
	}

	root := filepath.Clean(minecraftDirectory)
	if err := os.MkdirAll(filepath.Join(root, "versions"), 0o755); err != nil {
		return err
	}

	reportProgress(progress, 1, "获取版本元数据", fmt.Sprintf("正在读取 Minecraft %s 的版本描述", versionID), 0, 0, 0, 0, 0)
	metadataBytes, err := SourceProvider.GetBytes(ctx, metadataURL, nil)
	if err != nil {
		return err
	}

	return i.InstallFromMetadataBytes(ctx, versionID, root, metadataBytes, progress)
}

// InstallFromMetadataBytes 从已有的元数据字节流完成安装
// （供 ModLoaderInstaller 传入从 JAR 提取的版本 JSON）。
func (i *MinecraftVersionInstaller) InstallFromMetadataBytes(
	ctx context.Context,
	versionID, root string,
	metadataBytes []byte,
	progress InstallProgressFunc,
) error {
	if err := ValidateVersionID(versionID); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "versions"), 0o755); err != nil {
		return err
	}

	reportProgress(progress, 2, "分析下载清单", "正在整理客户端、依赖库与资源索引", 0, 0, 0, 0, 0)
	var metadata VersionJSON
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return fmt.Errorf("版本 JSON 解析失败：%w", err)
	}
	versionDirectory := filepath.Join(root, "versions", versionID)
	if err := os.MkdirAll(versionDirectory, 0o755); err != nil {
		return err
	}

	// 阶段 3-5 的清单在安装前即可确定；阶段 6 的资源文件清单要等资源索引下载完才能生成。
	clientFiles := createClientPlan(&metadata, versionID, versionDirectory)
	libraryFiles, err := createLibraryPlan(&metadata, root)
	if err != nil {
		return err
	}
	assetIndexFile, err := createAssetIndexPlan(&metadata, root)
	if err != nil {
		return err
	}
	// 日志配置（log4j2）与资源索引同级：都属于 assets 目录下的公共资源
	loggingFile := createLoggingPlan(&metadata, root)

	started := time.Now()
	counters := &installCounters{}
	for _, file := range clientFiles {
		counters.addTotalBytes(maxInt64(0, file.size))
	}
	for _, file := range libraryFiles {
		counters.addTotalBytes(maxInt64(0, file.size))
	}
	counters.addTotalFiles(int64(len(clientFiles) + len(libraryFiles)))
	if assetIndexFile != nil {
		counters.addTotalBytes(maxInt64(0, assetIndexFile.size))
		counters.addTotalFiles(1)
	}
	if loggingFile != nil {
		counters.addTotalBytes(maxInt64(0, loggingFile.size))
		counters.addTotalFiles(1)
	}

	if err := downloadStage(ctx, 3, "下载游戏客户端", clientFiles, progress, counters, started); err != nil {
		return err
	}
	if err := downloadStage(ctx, 4, "下载依赖库", libraryFiles, progress, counters, started); err != nil {
		return err
	}

	if assetIndexFile != nil {
		if err := downloadStage(ctx, 5, "下载资源索引", []downloadFile{*assetIndexFile}, progress, counters, started); err != nil {
			return err
		}
	}
	if loggingFile != nil {
		if err := downloadStage(ctx, 5, "下载日志配置", []downloadFile{*loggingFile}, progress, counters, started); err != nil {
			return err
		}
	}

	var assetFiles []downloadFile
	if assetIndexFile != nil {
		assetFiles, err = createAssetPlan(ctx, assetIndexFile.targetPath, root)
		if err != nil {
			return err
		}
	}
	for _, file := range assetFiles {
		counters.addTotalBytes(maxInt64(0, file.size))
	}
	counters.addTotalFiles(int64(len(assetFiles)))
	if err := downloadStage(ctx, 6, "下载游戏资源", assetFiles, progress, counters, started); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	reportProgress(progress, 7, "完成校验与安装", "正在写入版本描述并完成安装",
		counters.completedBytes.Load(), counters.totalBytes.Load(),
		int(counters.completedFiles.Load()), int(counters.totalFiles.Load()),
		calculateSpeed(counters.networkBytes.Load(), started))
	versionJSONPath := filepath.Join(versionDirectory, versionID+".json")
	if err := writeAllBytesAtomically(versionJSONPath, metadataBytes); err != nil {
		return err
	}
	reportProgress(progress, 7, "完成校验与安装", fmt.Sprintf("Minecraft %s 已安装完成", versionID),
		counters.totalBytes.Load(), counters.totalBytes.Load(),
		int(counters.totalFiles.Load()), int(counters.totalFiles.Load()),
		calculateSpeed(counters.networkBytes.Load(), started))
	return nil
}

// ---------------------------------------------------------------------------
// 下载清单生成
// ---------------------------------------------------------------------------

func createClientPlan(metadata *VersionJSON, versionID, versionDirectory string) []downloadFile {
	// Mod Loader 版本 JSON 不含 client 下载信息（通过 inheritsFrom 继承原版），正常跳过。
	if metadata.Downloads == nil || metadata.Downloads.Client == nil {
		return nil
	}
	client := metadata.Downloads.Client
	return []downloadFile{{
		url:         client.URL,
		targetPath:  filepath.Join(versionDirectory, versionID+".jar"),
		sha1:        client.SHA1,
		size:        client.Size,
		displayName: "客户端文件",
	}}
}

func createLibraryPlan(metadata *VersionJSON, minecraftDirectory string) ([]downloadFile, error) {
	// 同一目标路径只保留最后一条，去重 Maven 坐标重复声明的库。
	result := map[string]downloadFile{}
	order := []string{}
	features := DefaultFeatures()
	for _, library := range metadata.Libraries {
		if !ruleIsAllowed(library.Rules, features) {
			continue
		}

		handled, err := planDownloadsArtifact(library, result, &order, minecraftDirectory)
		if err != nil {
			return nil, err
		}
		if handled {
			continue
		}
		handled, err = planLegacyNatives(library, result, &order, minecraftDirectory)
		if err != nil {
			return nil, err
		}
		if handled {
			continue
		}
		if err := planCoordinateLibrary(library, result, &order, minecraftDirectory); err != nil {
			return nil, err
		}
	}

	files := make([]downloadFile, 0, len(result))
	for _, key := range order {
		files = append(files, result[key])
	}
	return files, nil
}

// planDownloadsArtifact 新版本格式：downloads.artifact / downloads.classifiers + natives。
func planDownloadsArtifact(library libraryJSON, result map[string]downloadFile, order *[]string, minecraftDirectory string) (bool, error) {
	if library.Downloads == nil {
		return false, nil
	}

	if library.Downloads.Artifact != nil {
		if err := addLibraryDownload(result, order, library.Downloads.Artifact, minecraftDirectory, "依赖库"); err != nil {
			return true, err
		}
	}

	if library.Downloads.Classifiers != nil && library.Natives != nil {
		if classifier, ok := library.Natives[RuleEvaluatorOSName()]; ok {
			if native, ok := library.Downloads.Classifiers[expandArchPlaceholder(classifier)]; ok {
				if err := addLibraryDownload(result, order, native, minecraftDirectory, "原生依赖库"); err != nil {
					return true, err
				}
			}
		}
	}

	return true, nil
}

// planLegacyNatives 旧版本（1.7.x 及更早）natives 回退：版本 JSON 无 downloads 字段，
// 用 name + classifier 拼出 natives JAR 并下载，否则这些版本永远缺原生库无法启动。
func planLegacyNatives(library libraryJSON, result map[string]downloadFile, order *[]string, minecraftDirectory string) (bool, error) {
	if library.Natives == nil {
		return false, nil
	}
	classifier, ok := library.Natives[RuleEvaluatorOSName()]
	if !ok {
		return false, nil
	}

	nativeRelPath := CreateNativeMavenPath(library.Name, classifier)
	if nativeRelPath == "" {
		return false, nil
	}

	nativeBaseURL := library.URL
	if strings.TrimSpace(nativeBaseURL) == "" {
		nativeBaseURL = "https://libraries.minecraft.net/"
	}
	nativeURL := strings.TrimRight(nativeBaseURL, "/") + "/" + filepath.ToSlash(nativeRelPath)
	nativeTarget, err := resolveRelativePath(filepath.Join(minecraftDirectory, "libraries"), nativeRelPath)
	if err != nil {
		return true, err
	}
	setPlan(result, order, nativeTarget, downloadFile{
		url: nativeURL, targetPath: nativeTarget, displayName: "原生依赖库",
	})
	return true, nil
}

// planCoordinateLibrary 更早的纯坐标格式：由 name（Maven 坐标）+ url 推导下载地址。
func planCoordinateLibrary(library libraryJSON, result map[string]downloadFile, order *[]string, minecraftDirectory string) error {
	if library.Name == "" {
		return nil
	}
	relativePath := CreateMavenPath(library.Name)
	if relativePath == "" {
		return nil
	}
	baseURL := library.URL
	if strings.TrimSpace(baseURL) == "" {
		baseURL = inferMavenRepository(library.Name)
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://libraries.minecraft.net/"
	}
	url := strings.TrimRight(baseURL, "/") + "/" + filepath.ToSlash(relativePath)
	target, err := resolveRelativePath(filepath.Join(minecraftDirectory, "libraries"), relativePath)
	if err != nil {
		return err
	}
	setPlan(result, order, target, downloadFile{
		url: url, targetPath: target, displayName: "依赖库",
	})
	return nil
}

// setPlan 以大小写不敏感的路径为键写入计划（保持插入顺序）。
func setPlan(result map[string]downloadFile, order *[]string, key string, file downloadFile) {
	mapKey := strings.ToLower(filepath.ToSlash(key))
	if _, exists := result[mapKey]; !exists {
		*order = append(*order, mapKey)
	}
	result[mapKey] = file
}

// expandArchPlaceholder natives classifier 中的 ${arch} 占位符按当前系统位数展开。
func expandArchPlaceholder(classifier string) string {
	arch := "32"
	if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
		arch = "64"
	}
	return strings.ReplaceAll(classifier, "${arch}", arch)
}

func createAssetIndexPlan(metadata *VersionJSON, minecraftDirectory string) (*downloadFile, error) {
	// Mod Loader 版本 JSON 不含资源索引（通过 inheritsFrom 继承）。
	if metadata.AssetIndex == nil {
		return nil, nil
	}
	id := metadata.AssetIndex.ID
	if id == "" {
		id = metadata.Assets
	}
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("资源索引缺少 ID。")
	}

	if metadata.AssetIndex.URL == "" {
		return nil, fmt.Errorf("资源索引缺少下载地址。")
	}
	// 索引 id 来自版本 JSON（可能由第三方 Loader 元数据/镜像提供），
	// 必须跟其它下载目标一样做目录包含校验，否则 "../.." 可写到 Minecraft 目录之外
	target, err := resolveRelativePath(
		filepath.Join(minecraftDirectory, "assets", "indexes"),
		id+".json",
	)
	if err != nil {
		return nil, err
	}

	return &downloadFile{
		url:         metadata.AssetIndex.URL,
		targetPath:  target,
		sha1:        metadata.AssetIndex.SHA1,
		size:        metadata.AssetIndex.Size,
		displayName: "资源索引",
	}, nil
}

type assetIndexEntry struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

func createAssetPlan(ctx context.Context, indexPath, minecraftDirectory string) ([]downloadFile, error) {
	stream, err := os.Open(indexPath)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	var index struct {
		Objects map[string]assetIndexEntry `json:"objects"`
	}
	if err := json.NewDecoder(stream).Decode(&index); err != nil {
		return nil, err
	}
	if len(index.Objects) == 0 {
		return nil, nil
	}

	result := map[string]downloadFile{}
	order := []string{}
	for name, asset := range index.Objects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hash := asset.Hash
		if !isSHA1(hash) {
			continue
		}
		relativePath := filepath.Join(hash[:2], hash)
		target, err := resolveRelativePath(filepath.Join(minecraftDirectory, "assets", "objects"), relativePath)
		if err != nil {
			return nil, err
		}
		setPlan(result, &order, target, downloadFile{
			url:         fmt.Sprintf("https://resources.download.minecraft.net/%s/%s", hash[:2], hash),
			targetPath:  target,
			sha1:        hash,
			size:        asset.Size,
			displayName: name,
		})
	}

	files := make([]downloadFile, 0, len(result))
	for _, key := range order {
		files = append(files, result[key])
	}
	return files, nil
}

// ---------------------------------------------------------------------------
// 分阶段并发下载
// ---------------------------------------------------------------------------

func downloadStage(
	ctx context.Context,
	stageIndex int,
	stageName string,
	files []downloadFile,
	progress InstallProgressFunc,
	counters *installCounters,
	started time.Time,
) error {
	if len(files) == 0 {
		reportProgress(progress, stageIndex, stageName, "此阶段没有需要下载的文件",
			counters.completedBytes.Load(), counters.totalBytes.Load(),
			int(counters.completedFiles.Load()), int(counters.totalFiles.Load()),
			calculateSpeed(counters.networkBytes.Load(), started))
		return nil
	}

	sem := make(chan struct{}, ParallelDownloads())
	var wg sync.WaitGroup
	errs := make([]error, len(files))
	var mu sync.Mutex
	currentFiles := map[string]bool{}

	for idx := range files {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			file := files[idx]
			sem <- struct{}{}
			defer func() { <-sem }()

			mu.Lock()
			currentFiles[file.displayName] = true
			activeCount := len(currentFiles)
			mu.Unlock()

			reusedBytes, err := downloadOneFile(ctx, file,
				counters.addProgressBytes,
				func() {
					reportThrottled(progress, stageIndex, stageName, file, activeCount, counters, started)
				})
			if err == nil && reusedBytes > 0 {
				counters.addCompleted(reusedBytes)
			}
			if err == nil {
				counters.incrementCompletedFiles()
			} else {
				errs[idx] = err
			}

			mu.Lock()
			delete(currentFiles, file.displayName)
			mu.Unlock()
		}(idx)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	reportProgress(progress, stageIndex, stageName, stageName+"完成",
		counters.completedBytes.Load(), counters.totalBytes.Load(),
		int(counters.completedFiles.Load()), int(counters.totalFiles.Load()),
		calculateSpeed(counters.networkBytes.Load(), started))
	return nil
}

// lastReportTicksMs 进度节流共用节拍器（unix 毫秒）。
var lastReportTicksMs atomic.Int64

// reportThrottled 按固定时间间隔节流的中途进度上报（每个阶段共用同一个节拍器）。
func reportThrottled(
	progress InstallProgressFunc,
	stageIndex int,
	stageName string,
	file downloadFile,
	activeFileCount int,
	counters *installCounters,
	started time.Time,
) {
	now := time.Now().UnixMilli()
	previous := lastReportTicksMs.Load()
	if now-previous < progressReportInterval.Milliseconds() ||
		!lastReportTicksMs.CompareAndSwap(previous, now) {
		return
	}

	reportProgress(progress, stageIndex, stageName,
		fmt.Sprintf("正在处理 %d 个文件 · %s", activeFileCount, file.displayName),
		counters.completedBytes.Load(), counters.totalBytes.Load(),
		int(counters.completedFiles.Load()), int(counters.totalFiles.Load()),
		calculateSpeed(counters.networkBytes.Load(), started))
}

// downloadOneFile 下载单个文件：已存在且校验通过则复用；否则经主源/回退源重试下载。
// 每个源最多尝试两次，容忍瞬时网络抖动；全部失败才视为该文件下载失败。
// 返回复用文件的字节数（0 表示本次实际下载）。
func downloadOneFile(
	ctx context.Context,
	file downloadFile,
	addProgressBytes func(int64),
	reportProgress func(),
) (int64, error) {
	if valid, err := isExistingFileValid(ctx, file); err == nil && valid {
		if file.size > 0 {
			return file.size, nil
		}
		if info, err := os.Stat(file.targetPath); err == nil {
			return info.Size(), nil
		}
		return 0, nil
	}

	directory := filepath.Dir(file.targetPath)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return 0, err
	}
	temporaryPath := file.targetPath + ".nya-download"

	resolvedURL := SourceProvider.Resolve(file.url)
	urls := []string{resolvedURL}
	if fb := SourceProvider.ResolveFallback(file.url); fb != nil && !strings.EqualFold(resolvedURL, *fb) {
		urls = append(urls, *fb)
	}

	var lastErr error
	for _, url := range urls {
		for attempt := 0; attempt < 2; attempt++ {
			// 停滞看门狗（进度感知）：每次读到数据都会重置空闲计时，
			// 慢速但仍在推进的连接不会被误杀；只有连接彻底停滞才触发重试。
			perFileCtx, cancel := context.WithCancel(ctx)
			// stalled 由看门狗（另一个 goroutine）置位，用它区分"停滞中断"与真实 IO 失败
			stalled := &atomic.Bool{}
			watchdog := newStallWatchdog(func() {
				stalled.Store(true)
				cancel()
			}, StallTimeout)

			attemptBytes := &atomic.Int64{}
			err := func() error {
				defer cancel()
				err := downloadToTemporary(perFileCtx, url, temporaryPath, attemptBytes, addProgressBytes, reportProgress, watchdog)
				if err == nil {
					watchdog.Touch()
					if validateErr := validateTemporaryFile(file, temporaryPath); validateErr != nil {
						return validateErr
					}
					if moveErr := os.Rename(temporaryPath, file.targetPath); moveErr != nil {
						// Windows 上 Rename 不能覆盖已存在文件：先删目标再移动
						tools.RemoveFileIfExists(file.targetPath)
						if moveErr := os.Rename(temporaryPath, file.targetPath); moveErr != nil {
							return moveErr
						}
					}
					return nil
				}
				return err
			}()
			watchdog.Dispose()

			if err == nil {
				return 0, nil
			}
			// 回滚本次尝试已累计的字节，避免重试导致进度双计
			if written := attemptBytes.Load(); written > 0 {
				addProgressBytes(-written)
			}
			tools.RemoveFileIfExists(temporaryPath)

			// 外部令牌取消：直接向上冒泡为"用户已取消"
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			// 不能用 perFileCtx.Err() 判断停滞：cancel 在上面的闭包里 defer，
			// 走到这里时它必然已是 Canceled，会把 404/校验失败/磁盘错误
			// 一律报成"连接停滞"。改用看门狗自己的标记。
			if stalled.Load() && ctx.Err() == nil {
				lastErr = fmt.Errorf("下载连接停滞（%.0f 秒无进度）：%s", StallTimeout.Seconds(), file.displayName)
			} else {
				lastErr = err
			}
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("下载失败：%s", file.displayName)
	}
	return 0, lastErr
}

// downloadToTemporary 单次尝试：流式下载到临时文件，每读到一块数据就喂看门狗并推进进度。
func downloadToTemporary(
	ctx context.Context,
	url, temporaryPath string,
	attemptBytes *atomic.Int64,
	addProgressBytes func(int64),
	reportProgress func(),
	watchdog *stallWatchdog,
) error {
	if _, err := validateHTTPSURL(url); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := longDownloadClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &httpStatusError{StatusCode: resp.StatusCode}
	}

	destination, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer destination.Close()

	buffer := make([]byte, downloadBufferSize)
	for {
		// 全局暂停门：暂停期间连接保持、速度归零，恢复后原连接继续
		if err := WaitPauseGate(ctx); err != nil {
			return err
		}
		read, err := resp.Body.Read(buffer)
		if read > 0 {
			watchdog.Touch()
			if _, writeErr := destination.Write(buffer[:read]); writeErr != nil {
				return writeErr
			}
			attemptBytes.Add(int64(read))
			addProgressBytes(int64(read))
			reportProgress()
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// validateTemporaryFile 校验临时文件：sha1 匹配 + 字节数与清单一致，失败返回错误。
func validateTemporaryFile(file downloadFile, temporaryPath string) error {
	if !matchesSHA1(temporaryPath, file.sha1) {
		return fmt.Errorf("下载文件校验失败：%s", file.displayName)
	}
	// 无 sha1 时至少校验字节数与清单声明一致，防止截断文件被当作有效
	if file.size > 0 {
		if info, err := os.Stat(temporaryPath); err != nil || info.Size() != file.size {
			return fmt.Errorf("下载文件大小不匹配：%s", file.displayName)
		}
	}
	return nil
}

func isExistingFileValid(ctx context.Context, file downloadFile) (bool, error) {
	if _, err := os.Stat(file.targetPath); err != nil {
		return false, nil
	}
	if file.size > 0 {
		if info, err := os.Stat(file.targetPath); err != nil || info.Size() != file.size {
			return false, nil
		}
	}
	done := make(chan bool, 1)
	go func() { done <- matchesSHA1(file.targetPath, file.sha1) }()
	select {
	case ok := <-done:
		return ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func matchesSHA1(path, expectedSHA1 string) bool {
	if strings.TrimSpace(expectedSHA1) == "" {
		return true
	}
	stream, err := os.Open(path)
	if err != nil {
		return false
	}
	defer stream.Close()
	hasher := sha1.New()
	if _, err := io.Copy(hasher, stream); err != nil {
		return false
	}
	return strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expectedSHA1)
}

func writeAllBytesAtomically(path string, data []byte) error {
	temporaryPath := path + ".nya-download"
	if err := os.WriteFile(temporaryPath, data, 0o644); err != nil {
		tools.RemoveFileIfExists(temporaryPath)
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		tools.RemoveFileIfExists(path)
		if err := os.Rename(temporaryPath, path); err != nil {
			tools.RemoveFileIfExists(temporaryPath)
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 清单项辅助
// ---------------------------------------------------------------------------

// CreateMavenPath 将 Maven 坐标转换为文件系统相对路径（支持 @extension 后缀）。
// 与校验端共用同一实现，避免双份逻辑漂移。
func CreateMavenPath(coordinate string) string {
	if strings.TrimSpace(coordinate) == "" {
		return ""
	}

	// 支持 @extension 后缀（如 org.lwjgl:lwjgl:3.2.3@jar），与启动端解析保持一致
	extension := "jar"
	name := coordinate
	if idx := strings.Index(coordinate, "@"); idx >= 0 {
		extension = coordinate[idx+1:]
		name = coordinate[:idx]
	}

	parts := strings.Split(name, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return ""
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return ""
		}
	}
	groupPath := strings.ReplaceAll(parts[0], ".", string(filepath.Separator))
	classifier := ""
	if len(parts) == 4 {
		classifier = "-" + parts[3]
	}
	return filepath.Join(groupPath, parts[1], parts[2],
		fmt.Sprintf("%s-%s%s.%s", parts[1], parts[2], classifier, extension))
}

// CreateNativeMavenPath 旧版本（1.7.x 及更早）natives 的 Maven 路径回退：
// 版本 JSON 无 downloads 字段时，用 name + classifier 拼出 natives JAR 的相对路径。
func CreateNativeMavenPath(coordinate, classifier string) string {
	if strings.TrimSpace(coordinate) == "" || strings.TrimSpace(classifier) == "" {
		return ""
	}
	parts := strings.Split(coordinate, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return ""
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return ""
		}
	}
	return filepath.Join(
		strings.ReplaceAll(parts[0], ".", string(filepath.Separator)),
		parts[1], parts[2],
		fmt.Sprintf("%s-%s-%s.jar", parts[1], parts[2], classifier))
}

// ValidateVersionID 校验版本 ID 只能作为 versions/ 下的单个文件夹名使用，
// 拒绝路径分隔符、"." 与 ".."（防止路径穿越导致目录外写入或级联删除）。
func ValidateVersionID(versionID string) error {
	if strings.TrimSpace(versionID) == "" {
		return fmt.Errorf("版本 ID 不能为空。")
	}
	if versionID == "." || versionID == ".." ||
		containsInvalidFileNameChars(versionID) {
		return fmt.Errorf("版本 ID 包含不安全字符：%s", versionID)
	}
	return nil
}

func validateInstallArgs(versionID, metadataURL, minecraftDirectory string) error {
	if strings.TrimSpace(versionID) == "" {
		return fmt.Errorf("versionID 不能为空")
	}
	if strings.TrimSpace(metadataURL) == "" {
		return fmt.Errorf("metadataURL 不能为空")
	}
	if strings.TrimSpace(minecraftDirectory) == "" {
		return fmt.Errorf("minecraftDirectory 不能为空")
	}
	return nil
}

// inferMavenRepository 根据 Maven 坐标的 group ID 推断最可能的仓库地址。
// 仅在库条目没有显式 url 且没有 downloads.artifact 时使用。
func inferMavenRepository(coordinate string) string {
	if strings.TrimSpace(coordinate) == "" {
		return "https://libraries.minecraft.net/"
	}
	lower := strings.ToLower(coordinate)

	// NeoForge 系列
	if strings.HasPrefix(lower, "net.neoforged") {
		return "https://maven.neoforged.net/releases/"
	}
	// NeoForge / Forge 重映射客户端（net.minecraft:client:<mc>-<build>:srg）：
	// 该 SRG 混淆版 client JAR 由 Loader 官方发布在自己的 Maven 上，Mojang 源不存在。
	// 若推断到 libraries.minecraft.net 会 404，导致 NeoForge 启动报 "installation is corrupted"。
	if strings.HasPrefix(lower, "net.minecraft") && strings.HasSuffix(lower, ":srg") {
		return "https://maven.neoforged.net/releases/"
	}
	// Forge 系列
	if strings.HasPrefix(lower, "net.minecraftforge") {
		return "https://maven.minecraftforge.net/"
	}
	// cpw.mods 系列（modlauncher / securejarhandler / bootstraplauncher 等）：
	// 旧版在 Forge Maven，新版本随 NeoForge 发布，统一回退 Forge Maven 保证可访问。
	if strings.HasPrefix(lower, "cpw.mods") {
		return "https://maven.minecraftforge.net/"
	}
	// Fabric 系列
	if strings.HasPrefix(lower, "net.fabricmc") {
		return "https://maven.fabricmc.net/"
	}
	// Quilt 系列
	if strings.HasPrefix(lower, "org.quiltmc") {
		return "https://maven.quiltmc.org/"
	}
	// MixinExtras (NeoForge 变体)
	if strings.HasPrefix(lower, "io.github.llamalad7") {
		return "https://maven.neoforged.net/releases/"
	}
	return "https://libraries.minecraft.net/"
}

// resolveRelativePath 将相对路径解析到 root 之下，越界时返回错误（防路径穿越）。
// 版本 JSON 可能来自第三方 Loader 元数据：越界必须中止安装而不是 panic。
func resolveRelativePath(root, relativePath string) (string, error) {
	normalizedRoot := filepath.Clean(root)
	target := filepath.Clean(filepath.Join(normalizedRoot, relativePath))
	if target != normalizedRoot && !strings.HasPrefix(target, normalizedRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("下载路径超出 Minecraft 目录：%s", relativePath)
	}
	return target, nil
}

func isSHA1(value string) bool {
	if len(value) != 40 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func calculateSpeed(completedBytes int64, started time.Time) float64 {
	elapsed := time.Since(started).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(completedBytes) / elapsed
}

// reportProgress 组装进度快照并回调（progress 为 nil 时不做任何事）。
func reportProgress(
	progress InstallProgressFunc,
	stageIndex int,
	stageName, detail string,
	completedBytes, totalBytes int64,
	completedFiles, totalFiles int,
	speed float64,
) {
	if progress == nil {
		return
	}
	progress(MinecraftInstallProgress{
		StageIndex:     stageIndex,
		StageName:      stageName,
		Detail:         detail,
		CompletedBytes: completedBytes,
		TotalBytes:     totalBytes,
		CompletedFiles: completedFiles,
		TotalFiles:     totalFiles,
		BytesPerSecond: speed,
	})
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// addLibraryDownload 按 downloads.artifact / classifiers.path 追加库下载计划。
func addLibraryDownload(
	result map[string]downloadFile,
	order *[]string,
	element *downloadInfoJSON,
	minecraftDirectory, displayName string,
) error {
	if element == nil || strings.TrimSpace(element.Path) == "" {
		return nil
	}
	target, err := resolveRelativePath(filepath.Join(minecraftDirectory, "libraries"), element.Path)
	if err != nil {
		return err
	}
	setPlan(result, order, target, downloadFile{
		url:         element.URL,
		targetPath:  target,
		sha1:        element.SHA1,
		size:        element.Size,
		displayName: filepath.Base(target),
	})
	return nil
}
