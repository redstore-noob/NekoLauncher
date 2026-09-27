package download

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"nekolauncher/internal/info"
	"nekolauncher/internal/tools"
)

// JavaRuntimeInstaller Java 运行时安装器。按平台自动选择安装包
// （Windows/macOS/Linux × x64/arm64），支持从 Azul Zulu、Oracle JDK、
// Eclipse Temurin 三个供应商实时查询并下载 JDK。

// JavaVendor Java JDK 供应商。
type JavaVendor int

const (
	// JavaVendorZulu Azul Zulu（默认，官方构建、免费商用）。
	JavaVendorZulu JavaVendor = iota
	// JavaVendorOracle Oracle JDK（官方直接下载仅提供 Java 21+）。
	JavaVendorOracle
	// JavaVendorTemurin Eclipse Temurin（Adoptium，带 SHA-256 校验）。
	JavaVendorTemurin
)

// String 供应商名（小写，用于目录命名与 API 参数）。
func (v JavaVendor) String() string {
	switch v {
	case JavaVendorZulu:
		return "Zulu"
	case JavaVendorOracle:
		return "Oracle"
	case JavaVendorTemurin:
		return "Temurin"
	default:
		return fmt.Sprintf("JavaVendor(%d)", int(v))
	}
}

// ParseJavaVendor 从名称解析供应商。
func ParseJavaVendor(name string) (JavaVendor, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "zulu":
		return JavaVendorZulu, true
	case "oracle":
		return JavaVendorOracle, true
	case "temurin":
		return JavaVendorTemurin, true
	default:
		return JavaVendorZulu, false
	}
}

// JavaRuntimeInstallProgress Java 运行时安装进度。
type JavaRuntimeInstallProgress struct {
	Phase          string
	CompletedBytes int64
	TotalBytes     int64
	BytesPerSecond float64
}

// JavaProgressFunc Java 安装进度回调（对应 C# IProgress<JavaRuntimeInstallProgress>）。
type JavaProgressFunc func(JavaRuntimeInstallProgress)

// InstalledJavaRuntime 已安装的 Java 运行时。
type InstalledJavaRuntime struct {
	DirectoryPath      string
	JavaExecutablePath string
	MajorVersion       *int
	Vendor             *JavaVendor
}

// DisplayName 展示名，如 "Zulu JDK 21"。
func (r InstalledJavaRuntime) DisplayName() string {
	version := "JDK"
	if r.MajorVersion != nil {
		version = fmt.Sprintf("JDK %d", *r.MajorVersion)
	}
	if r.Vendor != nil {
		return fmt.Sprintf("%s %s", JavaVendorDisplayName(*r.Vendor), version)
	}
	return "Java " + version
}

// JavaVendorDisplayName 供应商中文显示名。
func JavaVendorDisplayName(vendor JavaVendor) string {
	switch vendor {
	case JavaVendorZulu:
		return "Zulu"
	case JavaVendorOracle:
		return "Oracle"
	case JavaVendorTemurin:
		return "Temurin"
	default:
		return vendor.String()
	}
}

// JavaDownloadCandidate 从供应商 API 实时查询到的 JDK 下载候选条目
// （供版本列表展示与下载）。
type JavaDownloadCandidate struct {
	Vendor       JavaVendor
	MajorVersion int
	BuildVersion string
	DownloadURL  string
	SHA256       string
	SizeBytes    int64
}

// VendorName 供应商显示名。
func (c JavaDownloadCandidate) VendorName() string { return JavaVendorDisplayName(c.Vendor) }

// SizeDisplay 格式化大小。
func (c JavaDownloadCandidate) SizeDisplay() string {
	if c.SizeBytes > 0 {
		return fmt.Sprintf("%.1f MB", float64(c.SizeBytes)/1048576.0)
	}
	return ""
}

// DisplayName 列表主标题：如 "Zulu JDK 21"。
func (c JavaDownloadCandidate) DisplayName() string {
	return fmt.Sprintf("%s JDK %d", c.VendorName(), c.MajorVersion)
}

// DetailText 列表副标题：实际构建版本号 + 大小，如 "21.0.12.1 · 184.2 MB"。
func (c JavaDownloadCandidate) DetailText() string {
	build := c.BuildVersion
	if strings.TrimSpace(build) == "" {
		build = "latest"
	}
	size := c.SizeDisplay()
	if size != "" {
		return fmt.Sprintf("%s · %s", build, size)
	}
	return build
}

// SupportedJavaVersions 可自动下载的 Java 主版本（覆盖 Minecraft 全系版本需求）。
var SupportedJavaVersions = []int{8, 11, 17, 21, 25}

// OracleUnsupportedMessage Oracle JDK 官方直接下载仅支持 Java 21+
// （11/17 的 latest 链接返回 404，8 需登录许可）。
const OracleUnsupportedMessage = "Oracle JDK 官方直接下载仅提供 Java 21 及以上版本，请选择 21 / 25 或改用 Zulu / Temurin。"

const zuluMetadataAPI = "https://api.azul.com/metadata/v1/zulu/packages/"
const adoptiumAssetsAPI = "https://api.adoptium.net/v3/assets/latest/%d/hotspot"
const oracleDownloadBase = "https://download.oracle.com/java/%d/latest/jdk-%d_%s_bin.%s"

// javaClient 独立 HTTP 客户端：30 分钟整体超时（大文件下载），统一 UA。
// 包装器 base 留空（RoundTrip 时解析 DefaultTransport），代理设置替换
// 全局 Transport 后无需重建客户端即生效。
var javaClient = &http.Client{
	Timeout:   30 * time.Minute,
	Transport: &userAgentRoundTripper{base: nil, ua: "NekoLauncher/" + info.Version()},
}

// JavaRuntimeInstaller 安装器实例（C# 为实例类；当前无可变状态，保留结构以便扩展）。
type JavaRuntimeInstaller struct{}

// GetRuntimeDirectory 获取 Java 运行时根目录（与启动器启动时的搜索目录保持一致）。
func GetRuntimeDirectory() string {
	return filepath.Join(GetDefaultMinecraftDirectory(), "runtime")
}

// GetPlatformDisplayName 当前平台的说明文字（如 "Windows x64"、"macOS Apple Silicon"）。
func GetPlatformDisplayName() string {
	osName := "Linux"
	switch runtime.GOOS {
	case "windows":
		osName = "Windows"
	case "darwin":
		osName = "macOS"
	}
	arch := "x64"
	if isArm64() {
		if runtime.GOOS == "darwin" {
			arch = "Apple Silicon"
		} else {
			arch = "ARM64"
		}
	}
	return osName + " " + arch
}

// GetInstalledRuntimes 扫描已安装的 Java 运行时（支持 vendor 命名目录与历史遗留目录）。
func GetInstalledRuntimes() []InstalledJavaRuntime {
	runtimeDirectory := GetRuntimeDirectory()
	entries, err := os.ReadDir(runtimeDirectory)
	if err != nil {
		return nil
	}

	var result []InstalledJavaRuntime
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(runtimeDirectory, entry.Name())
		javaPath := findJavaExecutableIn(directory)
		if javaPath == "" {
			continue
		}
		version := tryGetJavaMajorVersion(javaPath)
		vendor := resolveVendorFromDirectory(directory)
		result = append(result, InstalledJavaRuntime{
			DirectoryPath:      directory,
			JavaExecutablePath: javaPath,
			MajorVersion:       version,
			Vendor:             vendor,
		})
	}
	// 按主版本降序
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			a, b := 0, 0
			if result[i].MajorVersion != nil {
				a = *result[i].MajorVersion
			}
			if result[j].MajorVersion != nil {
				b = *result[j].MajorVersion
			}
			if b > a {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

// DeleteRuntime 删除已安装的 Java 运行时目录。
func DeleteRuntime(directoryPath string) error {
	if strings.TrimSpace(directoryPath) == "" {
		return nil
	}
	fullPath, err := filepath.Abs(directoryPath)
	if err != nil {
		return err
	}
	runtimeRoot := filepath.Clean(GetRuntimeDirectory())
	if fullPath != runtimeRoot &&
		!strings.HasPrefix(strings.ToLower(fullPath), strings.ToLower(runtimeRoot+string(filepath.Separator))) {
		return fmt.Errorf("仅允许删除 runtime 目录内的 Java 运行时。")
	}
	return os.RemoveAll(fullPath)
}

// ---------------------------------------------------------------------------
// 实时查询可用版本
// ---------------------------------------------------------------------------

// QueryAvailableJavaVersions 实时查询指定供应商在当前平台下所有可用 JDK 版本
// （含实际构建号与大小）。Oracle 无公开动态 API，返回其官方直接下载支持的两个版本。
func QueryAvailableJavaVersions(ctx context.Context, vendor JavaVendor) ([]JavaDownloadCandidate, error) {
	osKey := operatingSystemKey()
	arch := "x64"
	if isArm64() {
		arch = "aarch64"
	}

	if vendor == JavaVendorOracle {
		var oracleResults []JavaDownloadCandidate
		for _, version := range SupportedJavaVersions {
			if version < 21 {
				continue
			}
			oracleResults = append(oracleResults, createOracleCandidate(version, osKey, arch))
		}
		return oracleResults, nil
	}

	// 各版本并发查询（对应 C# Task.WhenAll）
	type queryResult struct {
		index     int
		candidate *JavaDownloadCandidate
	}
	results := make([]*JavaDownloadCandidate, len(SupportedJavaVersions))
	ch := make(chan queryResult, len(SupportedJavaVersions))
	for i, version := range SupportedJavaVersions {
		go func(index, version int) {
			var candidate *JavaDownloadCandidate
			var err error
			if vendor == JavaVendorZulu {
				candidate, err = queryZuluCandidate(ctx, version, osKey, arch)
			} else {
				candidate, err = queryTemurinCandidate(ctx, version, osKey, arch)
			}
			if err != nil && ctx.Err() == nil {
				logsWrite(fmt.Sprintf("查询 Java %d (%s) 失败：%v", version, vendor, err))
			}
			ch <- queryResult{index: index, candidate: candidate}
		}(i, version)
	}
	for range SupportedJavaVersions {
		r := <-ch
		results[r.index] = r.candidate
	}

	var out []JavaDownloadCandidate
	for _, candidate := range results {
		if candidate != nil {
			out = append(out, *candidate)
		}
	}
	// 按主版本升序
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].MajorVersion < out[i].MajorVersion {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// queryZuluCandidate Zulu：Azul Metadata API，返回最新 GA 构建。
func queryZuluCandidate(ctx context.Context, majorVersion int, osKey, arch string) (*JavaDownloadCandidate, error) {
	archiveType := "tar.gz"
	if osKey == "windows" {
		archiveType = "zip"
	}
	endpoint := fmt.Sprintf("%s?java_version=%d&os=%s&arch=%s&archive_type=%s&java_package_type=jdk&latest=true&release_status=ga&page_size=1",
		zuluMetadataAPI, majorVersion, osKey, arch, archiveType)

	rawJSON, err := httpGetString(ctx, javaClient, endpoint)
	if err != nil {
		return nil, err
	}
	var entries []struct {
		DownloadURL string `json:"download_url"`
		Name        string `json:"name"`
	}
	if err := jsonUnmarshalStrict([]byte(rawJSON), &entries); err != nil {
		return nil, err
	}
	if len(entries) == 0 || strings.TrimSpace(entries[0].DownloadURL) == "" {
		return nil, nil
	}

	// 从文件名提取实际构建版本，如 "zulu21.52.203-ca-jdk21.0.12.1-win_x64.zip" → "21.0.12.1"
	buildVersion := extractJDKVersionFromName(entries[0].Name)
	// Zulu Metadata API 未提供 sha256，下载后靠 https + 解压验证兜底
	return &JavaDownloadCandidate{
		Vendor: JavaVendorZulu, MajorVersion: majorVersion,
		BuildVersion: buildVersion, DownloadURL: entries[0].DownloadURL,
	}, nil
}

// queryTemurinCandidate Temurin：Adoptium API，带 SHA-256 校验。
func queryTemurinCandidate(ctx context.Context, majorVersion int, osKey, arch string) (*JavaDownloadCandidate, error) {
	// Adoptium API 的 os 枚举是 linux/windows/mac（无 macos），需单独映射
	adoptiumOS := osKey
	if adoptiumOS == "macos" {
		adoptiumOS = "mac"
	}
	endpoint := fmt.Sprintf(adoptiumAssetsAPI, majorVersion) +
		fmt.Sprintf("?architecture=%s&image_type=jdk&os=%s&vendor=eclipse&page_size=1", arch, adoptiumOS)

	rawJSON, err := httpGetString(ctx, javaClient, endpoint)
	if err != nil {
		return nil, err
	}
	var entries []struct {
		Binary struct {
			Package struct {
				Link     string `json:"link"`
				Checksum string `json:"checksum"`
				Size     int64  `json:"size"`
			} `json:"package"`
			Version struct {
				Semver string `json:"semver"`
			} `json:"version"`
		} `json:"binary"`
	}
	if err := jsonUnmarshalStrict([]byte(rawJSON), &entries); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	pkg := entries[0].Binary.Package
	if strings.TrimSpace(pkg.Link) == "" || strings.TrimSpace(pkg.Checksum) == "" {
		return nil, nil
	}
	return &JavaDownloadCandidate{
		Vendor: JavaVendorTemurin, MajorVersion: majorVersion,
		BuildVersion: entries[0].Binary.Version.Semver,
		DownloadURL:  pkg.Link, SHA256: pkg.Checksum, SizeBytes: pkg.Size,
	}, nil
}

// createOracleCandidate Oracle：官方直接下载链接（无需登录，实测仅 Java 21/25 可用）。
func createOracleCandidate(majorVersion int, osKey, arch string) JavaDownloadCandidate {
	var platform string
	switch osKey {
	case "windows":
		if arch == "aarch64" {
			platform = "windows-aarch64"
		} else {
			platform = "windows-x64"
		}
	case "macos":
		if arch == "aarch64" {
			platform = "macos-aarch64"
		} else {
			platform = "macos-x64"
		}
	default:
		if arch == "aarch64" {
			platform = "linux-aarch64"
		} else {
			platform = "linux-x64"
		}
	}
	extension := "tar.gz"
	if osKey == "windows" {
		extension = "zip"
	}
	return JavaDownloadCandidate{
		Vendor: JavaVendorOracle, MajorVersion: majorVersion, BuildVersion: "latest",
		DownloadURL: fmt.Sprintf(oracleDownloadBase, majorVersion, majorVersion, platform, extension),
	}
}

// extractJDKVersionFromName 从 Zulu 文件名中提取实际 JDK 版本号
// （如 "zulu21.52.203-ca-jdk21.0.12.1-win_x64.zip" → "21.0.12.1"）。
func extractJDKVersionFromName(fileName string) string {
	if strings.TrimSpace(fileName) == "" {
		return ""
	}
	lower := strings.ToLower(fileName)
	marker := strings.Index(lower, "jdk")
	if marker < 0 {
		return ""
	}
	segment := fileName[marker+3:]
	end := strings.IndexAny(segment, "-_+")
	if end < 0 {
		end = len(segment)
	}
	return segment[:end]
}

// ---------------------------------------------------------------------------
// 安装
// ---------------------------------------------------------------------------

// InstallCandidate 下载并安装指定的 JDK 候选条目到 runtime 目录。
func (i *JavaRuntimeInstaller) InstallCandidate(
	ctx context.Context,
	candidate JavaDownloadCandidate,
	progress JavaProgressFunc,
) (*InstalledJavaRuntime, error) {
	runtimeDirectory := GetRuntimeDirectory()
	if err := os.MkdirAll(runtimeDirectory, 0o755); err != nil {
		return nil, err
	}
	vendorKey := strings.ToLower(candidate.Vendor.String())
	targetDirectory := filepath.Join(runtimeDirectory, fmt.Sprintf("java-%s-%d", vendorKey, candidate.MajorVersion))

	vendorDisplay := candidate.VendorName()
	extension := "tar.gz"
	if runtime.GOOS == "windows" {
		extension = "zip"
	}
	temporaryArchive := filepath.Join(os.TempDir(),
		fmt.Sprintf("nyalauncher-java-%s-%d-%d.%s", vendorKey, candidate.MajorVersion, time.Now().UnixNano(), extension))
	defer tools.RemoveFileIfExists(temporaryArchive)

	reportJavaProgress(progress, fmt.Sprintf("正在下载 %s JDK %d", vendorDisplay, candidate.MajorVersion), 0, candidate.SizeBytes, 0)
	if err := downloadJavaArchive(ctx, candidate.DownloadURL, temporaryArchive, candidate.SizeBytes, progress); err != nil {
		return nil, err
	}

	// SHA-256 完整性校验（有校验值才校验；Zulu/Oracle 无公开校验值时靠 https + 解压验证兜底）
	if strings.TrimSpace(candidate.SHA256) != "" {
		reportJavaProgress(progress, "校验文件完整性", candidate.SizeBytes, candidate.SizeBytes, 0)
		if err := verifyJavaSHA256(temporaryArchive, candidate.SHA256); err != nil {
			return nil, err
		}
	}

	// 解压到临时目录（防部分解压污染已有安装）。
	// 注意：临时目录必须与目标同卷——Directory.Move 不支持跨盘移动，
	// %TEMP% 在 C: 而游戏目录在其它盘时会在删掉旧 runtime 后移动失败。
	extractionDirectory := filepath.Join(runtimeDirectory, fmt.Sprintf("nya-java-extract-%d", time.Now().UnixNano()))
	defer tryDeleteDirectory(extractionDirectory)

	if err := os.MkdirAll(extractionDirectory, 0o755); err != nil {
		return nil, err
	}
	reportJavaProgress(progress, "正在解压", candidate.SizeBytes, candidate.SizeBytes, 0)
	if err := extractJavaArchive(temporaryArchive, extractionDirectory); err != nil {
		return nil, err
	}

	// 定位解压出的 JDK 根目录并移动到正式位置
	extractedRoot := findJDKRoot(extractionDirectory)
	if extractedRoot == "" {
		return nil, fmt.Errorf("下载的压缩包中没有找到 JDK 目录。")
	}
	if _, err := os.Stat(targetDirectory); err == nil {
		if err := os.RemoveAll(targetDirectory); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(extractedRoot, targetDirectory); err != nil {
		// 跨卷移动回退：复制后删除。两个错误都要报出来——旧运行时在上面
		// 已经被删掉了，只报 rename 的错误会掩盖真正的原因。
		if copyErr := copyDirectory(extractedRoot, targetDirectory); copyErr != nil {
			return nil, fmt.Errorf("安装 JDK 失败（rename: %v, copy: %w）", err, copyErr)
		}
		tryDeleteDirectory(extractedRoot)
	}

	javaPath := findJavaExecutableIn(targetDirectory)
	if javaPath == "" {
		return nil, fmt.Errorf("安装完成后未找到 java 可执行文件。")
	}
	installedVersion := tryGetJavaMajorVersion(javaPath)
	reportJavaProgress(progress, "安装完成", candidate.SizeBytes, candidate.SizeBytes, 0)
	return &InstalledJavaRuntime{
		DirectoryPath:      targetDirectory,
		JavaExecutablePath: javaPath,
		MajorVersion:       installedVersion,
		Vendor:             &candidate.Vendor,
	}, nil
}

// ---------------------------------------------------------------------------
// 平台判定
// ---------------------------------------------------------------------------

func isArm64() bool {
	return runtime.GOARCH == "arm64"
}

func operatingSystemKey() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	default:
		return "linux"
	}
}

// ---------------------------------------------------------------------------
// 下载与校验
// ---------------------------------------------------------------------------

func downloadJavaArchive(
	ctx context.Context,
	url, targetPath string,
	expectedSize int64,
	progress JavaProgressFunc,
) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := javaClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &httpStatusError{StatusCode: resp.StatusCode}
	}

	totalBytes := resp.ContentLength
	if totalBytes <= 0 {
		totalBytes = expectedSize
	}
	destination, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer destination.Close()

	buffer := make([]byte, 128*1024)
	var downloaded atomic.Int64
	started := time.Now()
	// 进度节流：与 MinecraftVersionInstaller 相同的固定间隔上报，
	// 避免每次回调都封送到 UI 线程造成报表风暴。
	var lastReport int64 = -progressReportInterval.Milliseconds()
	for {
		// 全局暂停门：暂停期间连接保持、速度归零，恢复后原连接继续
		if err := WaitPauseGate(ctx); err != nil {
			return err
		}
		read, err := resp.Body.Read(buffer)
		if read > 0 {
			if _, writeErr := destination.Write(buffer[:read]); writeErr != nil {
				return writeErr
			}
			downloaded.Add(int64(read))
			now := time.Since(started).Milliseconds()
			if now-lastReport >= progressReportInterval.Milliseconds() {
				lastReport = now
				elapsed := time.Since(started).Seconds()
				var speed float64
				if elapsed > 0 {
					speed = float64(downloaded.Load()) / elapsed
				}
				reportJavaProgress(progress, "正在下载 JDK", downloaded.Load(), totalBytes, speed)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	elapsed := time.Since(started).Seconds()
	var speed float64
	if elapsed > 0 {
		speed = float64(downloaded.Load()) / elapsed
	}
	reportJavaProgress(progress, "正在下载 JDK", downloaded.Load(), totalBytes, speed)
	return nil
}

func verifyJavaSHA256(path, expectedSHA256 string) error {
	stream, err := os.Open(path)
	if err != nil {
		return err
	}
	defer stream.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, stream); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expectedSHA256) {
		return fmt.Errorf("Java 安装包校验失败（SHA-256 不匹配，文件可能损坏或被篡改）。")
	}
	return nil
}

func extractJavaArchive(archivePath, destinationDirectory string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		reader, err := zip.OpenReader(archivePath)
		if err != nil {
			return err
		}
		defer reader.Close()
		for _, entry := range reader.File {
			if err := extractZipEntry(entry, destinationDirectory); err != nil {
				return err
			}
		}
		return nil
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		stream, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer stream.Close()
		gzipReader, err := gzip.NewReader(stream)
		if err != nil {
			return err
		}
		defer gzipReader.Close()
		tarReader := tar.NewReader(gzipReader)
		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			target := filepath.Join(destinationDirectory, filepath.FromSlash(header.Name))
			if !strings.HasPrefix(target, filepath.Clean(destinationDirectory)+string(filepath.Separator)) {
				return fmt.Errorf("非法的压缩包内路径：%s", header.Name)
			}
			switch header.Typeflag {
			case tar.TypeDir:
				if err := os.MkdirAll(target, 0o755); err != nil {
					return err
				}
			case tar.TypeReg:
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					return err
				}
				writer, err := os.OpenFile(target, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode)&0o777)
				if err != nil {
					return err
				}
				if _, err := io.Copy(writer, tarReader); err != nil {
					writer.Close()
					return err
				}
				writer.Close()
			default:
				// 符号链接/硬链接等条目一律跳过：运行时不需要，
				// 且避免解包出指向包外路径的链接
			}
		}
	default:
		return fmt.Errorf("不支持的安装包格式：%s", archivePath)
	}
}

func extractZipEntry(entry *zip.File, destinationDirectory string) error {
	name := strings.ReplaceAll(entry.Name, "\\", "/")
	target := filepath.Join(destinationDirectory, filepath.FromSlash(name))
	if !strings.HasPrefix(target, filepath.Clean(destinationDirectory)+string(filepath.Separator)) {
		return fmt.Errorf("非法的压缩包内路径：%s", entry.Name)
	}
	if entry.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	reader, err := entry.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	writer, err := os.OpenFile(target, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, entry.Mode())
	if err != nil {
		return err
	}
	defer writer.Close()
	_, err = io.Copy(writer, reader)
	return err
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

// findJDKRoot 在解压目录中定位 JDK 根目录（含 bin/java 的那一层）。
func findJDKRoot(baseDirectory string) string {
	javaPath := findJavaExecutableIn(baseDirectory)
	if javaPath == "" {
		return ""
	}
	// java 位于 <root>/bin/java.exe：向上两级即 JDK 根目录
	binDir := filepath.Dir(javaPath)
	return filepath.Dir(binDir)
}

// findJavaExecutableIn 在目录树中查找 java 可执行文件（要求父目录为 bin）。
func findJavaExecutableIn(directory string) string {
	executableName := "java"
	if runtime.GOOS == "windows" {
		executableName = "java.exe"
	}
	return findFileInTree(directory, executableName, "bin")
}

// resolveVendorFromDirectory 从目录名（java-zulu-21 / java-oracle-17 /
// java-temurin-8 或历史遗留 java-21）解析供应商。
func resolveVendorFromDirectory(directoryPath string) *JavaVendor {
	name := filepath.Base(directoryPath)
	if strings.TrimSpace(name) == "" {
		return nil
	}
	for _, vendor := range []JavaVendor{JavaVendorZulu, JavaVendorOracle, JavaVendorTemurin} {
		prefix := "java-" + strings.ToLower(vendor.String()) + "-"
		if strings.HasPrefix(strings.ToLower(name), prefix) {
			v := vendor
			return &v
		}
	}
	return nil
}

var javaVersionPattern = regexp.MustCompile(`(?i)version\s+"(\d+)(?:\.(\d+))?`)

// tryGetJavaMajorVersion 执行 java -version 探测主版本号；失败返回 nil。
// 先等待退出（带 3 秒超时）再读流，避免同步读取永久阻塞。
func tryGetJavaMajorVersion(javaExecutable string) *int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, javaExecutable, "-version")
	var stderr, stdout strings.Builder
	command.Stderr = &stderr
	command.Stdout = &stdout
	// java.exe 是控制台程序，探测时禁止闪现 cmd 窗口
	tools.HideProcessWindow(command)
	// 先等待退出（超时则强杀），再读取输出，杜绝挂起
	if err := command.Run(); err != nil && ctx.Err() == nil {
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}

	match := javaVersionPattern.FindStringSubmatch(stderr.String() + "\n" + stdout.String())
	if match == nil {
		return nil
	}
	major := parseJavaInt(match[1])
	if major == nil {
		return nil
	}
	// 旧版 JDK 输出 "1.8.0"：主版本取次段
	if *major == 1 && len(match) > 2 {
		if legacyMajor := parseJavaInt(match[2]); legacyMajor != nil {
			return legacyMajor
		}
	}
	return major
}

func parseJavaInt(s string) *int {
	n := 0
	ok := len(s) > 0
	for _, c := range s {
		if c < '0' || c > '9' {
			ok = false
			break
		}
		n = n*10 + int(c-'0')
	}
	if !ok {
		return nil
	}
	return &n
}

func reportJavaProgress(progress JavaProgressFunc, phase string, completedBytes, totalBytes int64, speed float64) {
	if progress == nil {
		return
	}
	progress(JavaRuntimeInstallProgress{
		Phase:          phase,
		CompletedBytes: completedBytes,
		TotalBytes:     totalBytes,
		BytesPerSecond: speed,
	})
}

func tryDeleteDirectory(path string) {
	_ = os.RemoveAll(path)
}

// copyDirectory 跨卷移动回退：递归复制目录。
func copyDirectory(source, target string) error {
	return filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if d.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		// 流式复制：JDK 单文件可达上百 MB，避免整文件读入内存。
		// 不用 defer：Walk 回调里 defer 会把句柄一路攒到遍历结束才释放。
		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		targetFile, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			sourceFile.Close()

			return err
		}
		_, err = io.Copy(targetFile, sourceFile)
		sourceCloseErr := sourceFile.Close()
		closeErr := targetFile.Close()
		if err != nil {
			return err
		}
		if sourceCloseErr != nil {
			return sourceCloseErr
		}

		return closeErr
	})
}
