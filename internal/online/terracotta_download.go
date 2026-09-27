package online

import (
	"archive/tar"
	"compress/gzip"
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/download"
	"nekolauncher/internal/logs"
)

// 陶瓦联机自动安装：查 GitHub Releases → 按当前平台挑资产 → 下载 → 解压 → 写回设置。
//
// 为什么之前没做：其发行包按 target triple 命名（x86_64-pc-windows-msvc 之类），
// macOS 侧还可能是 .dmg，自动挑资产容易选错平台。现在改成**打分挑选 + 只自动处理
// 能解开的压缩包**：选不中或格式不支持时，把发行页链接交给用户手动处理，
// 绝不在"下了一个装不上的东西"之后假装成功。
const (
	terracottaReleasesAPI = "https://api.github.com/repos/burningtnt/Terracotta/releases/latest"
	// terracottaMirrorAPI Gitee 镜像的 releases API（国内回退）。
	terracottaMirrorPage = "https://gitee.com/burningtnt/Terracotta/releases"
	// terracottaInstallTimeout 单次安装的整体超时。
	terracottaInstallTimeout = 10 * time.Minute
)

// terracottaReleaseAsset GitHub release 里的一个资产。
type terracottaReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// terracottaRelease GitHub release 的最小投影。
type terracottaRelease struct {
	TagName string                   `json:"tag_name"`
	Name    string                   `json:"name"`
	Assets  []terracottaReleaseAsset `json:"assets"`
}

// TerracottaInstallResult 自动安装的结果（前端展示用）。
type TerracottaInstallResult struct {
	// Version release 的 tag
	Version string `json:"Version"`
	// AssetName 选中的资产文件名
	AssetName string `json:"AssetName"`
	// Path 安装后的可执行文件路径（已写回设置）
	Path string `json:"Path"`
	// ManualHint 非空表示"需要用户手动完成"：内容是原因与发行页地址
	ManualHint string `json:"ManualHint"`
}

// fetchTerracottaRelease 拉取最新 release。
func fetchTerracottaRelease(ctx context.Context) (terracottaRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, terracottaReleasesAPI, nil)
	if err != nil {
		return terracottaRelease{}, err
	}
	// GitHub API 要求 UA；不给会 403
	request.Header.Set("User-Agent", "NekoLauncher/"+terracottaUserAgentVersion())
	request.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return terracottaRelease{}, fmt.Errorf("访问 GitHub Releases 失败：%w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
		return terracottaRelease{}, errors.New("GitHub API 限流了，请稍后再试，或到发行页手动下载")
	}
	if response.StatusCode != http.StatusOK {
		return terracottaRelease{}, fmt.Errorf("GitHub Releases 返回 %d", response.StatusCode)
	}

	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if readErr != nil {
		return terracottaRelease{}, readErr
	}
	var release terracottaRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return terracottaRelease{}, fmt.Errorf("解析 release 失败：%w", err)
	}

	return release, nil
}

// terracottaUserAgentVersion 版本号（沿用 info 包的版本串；这里避免多引一个包）。
func terracottaUserAgentVersion() string {
	if value := strings.TrimSpace(config.GetValue("launcherVersion")); value != "" {
		return value
	}

	return "Nya_Rebuild"
}

// assetPlatformScore 给资产名打分：平台 + 架构都对上才给正分。
//
// 兼容各家命名习惯：windows/win、macos/darwin/osx/apple、linux/gnu；
// 架构上 amd64/x86_64/x64/64bit 视为同一类，arm64/aarch64 同一类。
// 明确排除的可执行文件后缀与校验文件（sha256/sig/asc）单独降权。
func assetPlatformScore(name, goos, goarch string) int {
	lower := strings.ToLower(name)

	// 校验文件、源码包一律不选
	for _, excluded := range []string{".sha256", ".sha512", ".sig", ".asc", "checksums", "source"} {
		if strings.Contains(lower, excluded) {
			return -1
		}
	}

	score := 0
	platformMatches := func(candidates []string) bool {
		for _, candidate := range candidates {
			if strings.Contains(lower, candidate) {
				return true
			}
		}

		return false
	}

	// 平台
	switch goos {
	case "windows":
		if !platformMatches([]string{"windows", "win32", "win64", "-win"}) {
			return -1
		}
		score += 4
	case "darwin":
		if !platformMatches([]string{"macos", "darwin", "osx", "apple"}) {
			return -1
		}
		score += 4
	case "linux":
		if !platformMatches([]string{"linux", "gnu"}) {
			return -1
		}
		score += 4
	default:
		return -1
	}

	// 架构
	switch goarch {
	case "amd64":
		if platformMatches([]string{"x86_64", "amd64", "x64", "64bit"}) {
			score += 2
		} else if platformMatches([]string{"aarch64", "arm64"}) {
			return -1
		}
	case "arm64":
		if platformMatches([]string{"aarch64", "arm64"}) {
			score += 2
		} else if platformMatches([]string{"x86_64", "amd64", "x64"}) {
			return -1
		}
	case "386":
		if platformMatches([]string{"i686", "i386", "x86", "32bit"}) {
			score += 2
		}
	}

	// 可解压格式优先（zip 比 tar.gz/.dmg 更容易在 Go 里处理）
	if strings.HasSuffix(lower, ".zip") {
		score += 3
	} else if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		score += 2
	} else if strings.HasSuffix(lower, ".dmg") || strings.HasSuffix(lower, ".pkg") {
		score += 0
	}

	return score
}

// pickTerracottaAsset 在当前平台可用的资产里挑一个（分最高者胜）。
func pickTerracottaAsset(assets []terracottaReleaseAsset, goos, goarch string) (terracottaReleaseAsset, error) {
	best := terracottaReleaseAsset{}
	bestScore := -1

	for _, asset := range assets {
		score := assetPlatformScore(asset.Name, goos, goarch)
		if score > bestScore {
			best, bestScore = asset, score
		}
	}
	if bestScore < 0 {
		return terracottaReleaseAsset{}, fmt.Errorf(
			"发行包里没有找到适配 %s/%s 的资产", goos, goarch)
	}

	return best, nil
}

// InstallTerracotta 自动下载并安装陶瓦联机，成功后把可执行文件路径写回设置。
func (p *terracottaProvider) InstallTerracotta(ctx context.Context) (TerracottaInstallResult, error) {
	installCtx, cancel := context.WithTimeout(ctx, terracottaInstallTimeout)
	defer cancel()

	release, err := fetchTerracottaRelease(installCtx)
	if err != nil {
		return TerracottaInstallResult{}, err
	}
	result := TerracottaInstallResult{Version: release.TagName}

	asset, err := pickTerracottaAsset(release.Assets, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		result.ManualHint = fmt.Sprintf(
			"%v。请到 %s 手动下载（国内可试 %s），再用右侧「选择可执行文件」指定。",
			err, terracottaReleases, terracottaMirrorPage)

		return result, nil
	}
	result.AssetName = asset.Name

	// 只自动处理 zip / tar.gz；dmg / pkg 交给用户
	// （实测发行包命名：terracotta-0.4.2-windows-x86_64-pkg.tar.gz）
	lowerAsset := strings.ToLower(asset.Name)
	if !strings.HasSuffix(lowerAsset, ".zip") && !strings.HasSuffix(lowerAsset, ".tar.gz") &&
		!strings.HasSuffix(lowerAsset, ".tgz") {
		result.ManualHint = fmt.Sprintf(
			"该平台的发行包是 %s，启动器暂不自动解压。请手动解压后到 %s 获取，"+
				"再用右侧「选择可执行文件」指定（国内可试 %s）。",
			asset.Name, terracottaReleases, terracottaMirrorPage)

		return result, nil
	}

	root := filepath.Join(config.StorageDirectory(), "terracotta")
	// 按版本号分子目录：升级后旧版本文件留在原地，定位只在新目录里做，
	// 避免新旧两套带版本号的二进制混在一起挑错
	directory := filepath.Join(root, strings.TrimPrefix(release.TagName, "v"))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return result, err
	}
	archivePath := filepath.Join(root, asset.Name)

	if err := download.DownloadFileToPath(installCtx, asset.URL, archivePath, nil); err != nil {
		return result, fmt.Errorf("下载 %s 失败：%w", asset.Name, err)
	}

	if err := extractTerracottaArchive(archivePath, directory); err != nil {
		return result, err
	}
	_ = os.Remove(archivePath)

	binary, err := locateTerracottaBinary(directory)
	if err != nil {
		return result, err
	}
	// Unix 侧保险：发行包打包/解压链路丢执行位时，会"装好了却起不来"
	if runtime.GOOS != "windows" {
		_ = os.Chmod(binary, 0o755)
	}
	result.Path = binary

	if p.store != nil {
		saveSetting(p.store, keyTerracottaPath, binary)
	}
	logs.Write("LAUNCH", fmt.Sprintf("陶瓦联机已自动安装：%s（%s）", binary, release.TagName))

	return result, nil
}

// extractTerracottaArchive 按扩展名分派解压（zip / tar.gz），两种都做越界路径校验。
func extractTerracottaArchive(archivePath, targetDirectory string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractTerracottaZip(archivePath, targetDirectory)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTerracottaTarGz(archivePath, targetDirectory)
	default:
		return fmt.Errorf("不支持的压缩格式：%s", filepath.Base(archivePath))
	}
}

// safeArchiveTarget 把压缩包内的相对路径解析到目标目录内（挡掉 ../ 与绝对路径）。
func safeArchiveTarget(targetDirectory, name string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("压缩包内含越界路径：%s", name)
	}
	target := filepath.Join(targetDirectory, cleaned)
	if !strings.HasPrefix(target, filepath.Clean(targetDirectory)+string(filepath.Separator)) {
		return "", fmt.Errorf("压缩包内含越界路径：%s", name)
	}

	return target, nil
}

// writeArchiveFile 落盘一个压缩包条目。
func writeArchiveFile(target string, mode os.FileMode, content io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o755
	}
	destination, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, content)
	closeErr := destination.Close()
	if copyErr != nil {
		return copyErr
	}

	return closeErr
}

// extractTerracottaZip 解压 zip。
func extractTerracottaZip(archivePath, targetDirectory string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开压缩包失败：%w", err)
	}
	defer func() { _ = reader.Close() }()

	for _, entry := range reader.File {
		target, joinErr := safeArchiveTarget(targetDirectory, entry.Name)
		if joinErr != nil {
			return joinErr
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}

			continue
		}
		source, openErr := entry.Open()
		if openErr != nil {
			return openErr
		}
		writeErr := writeArchiveFile(target, entry.Mode(), source)
		_ = source.Close()
		if writeErr != nil {
			return writeErr
		}
	}

	return nil
}

// extractTerracottaTarGz 解压 tar.gz（实测 Windows/Linux 发行包用的是这个格式）。
func extractTerracottaTarGz(archivePath, targetDirectory string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("打开 gzip 失败：%w", err)
	}
	defer func() { _ = gzipReader.Close() }()

	reader := tar.NewReader(gzipReader)
	for {
		header, nextErr := reader.Next()
		if nextErr == io.EOF {
			return nil
		}
		if nextErr != nil {
			return fmt.Errorf("读取 tar 失败：%w", nextErr)
		}
		target, joinErr := safeArchiveTarget(targetDirectory, header.Name)
		if joinErr != nil {
			return joinErr
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := writeArchiveFile(target, os.FileMode(header.Mode), reader); err != nil {
				return err
			}
		default:
			// 符号链接等一律跳过：不需要，也避免解出指向目录外的链接
		}
	}
}

// terracottaPlatformToken 当前平台在发行包命名里的写法（GOOS → 资产命名）。
func terracottaPlatformToken() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}

	return runtime.GOOS
}

// terracottaArchToken 当前架构在发行包命名里的写法。
func terracottaArchToken() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "386":
		return "i686"
	default:
		return runtime.GOARCH
	}
}

// isVersionedTerracottaBinary 判断是否是新版发行包里的带版本号可执行文件
// （terracotta-<版本>-<平台>[-<架构>][.exe]）。实测 v0.4.2 起的包内布局：
//   - windows：terracotta-0.4.2-windows-x86_64.exe + VCRUNTIME140.DLL
//   - macos：terracotta-0.4.2-macos-arm64 + 同名 .pkg（xar 安装包分发物，不是程序本体）
//   - linux：terracotta-0.4.2-linux-x86_64
//
// 因此排除 .pkg 与运行库，Windows 要求 .exe 后缀，其它平台要求没有 .exe。
func isVersionedTerracottaBinary(name string) bool {
	lower := strings.ToLower(name)
	if !strings.HasPrefix(lower, "terracotta-") ||
		strings.HasSuffix(lower, ".pkg") ||
		strings.Contains(lower, "vcruntime") {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.HasSuffix(lower, ".exe")
	}

	return !strings.HasSuffix(lower, ".exe")
}

// versionedTerracottaScore 带版本号候选的择优打分：平台名 > 架构名。
// 目录里同时留有多个版本/平台的解压产物时（升级未清理），挑当前平台最匹配的。
func versionedTerracottaScore(name string) int {
	lower := strings.ToLower(name)
	score := 0
	if strings.Contains(lower, terracottaPlatformToken()) {
		score += 2
	}
	if strings.Contains(lower, terracottaArchToken()) {
		score++
	}

	return score
}

// locateTerracottaBinary 在安装目录里找可执行文件（深浅不限，发行包目录结构不固定）。
// 固定名（terracotta / terracotta.exe 等，旧布局与手动解压）优先，
// 其次按新版带版本号文件名挑当前平台最匹配的；同分取字典序更大的（版本号更高的居多）。
func locateTerracottaBinary(directory string) (string, error) {
	legacy := terracottaBinaryNames()
	found := ""
	versioned := ""
	versionedScore := -1

	walkErr := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		name := entry.Name()
		for _, candidate := range legacy {
			if strings.EqualFold(name, candidate) {
				found = path

				return nil
			}
		}
		if isVersionedTerracottaBinary(name) {
			score := versionedTerracottaScore(name)

			if score > versionedScore ||
				(score == versionedScore &&
					strings.ToLower(name) > strings.ToLower(filepath.Base(versioned))) {
				versioned, versionedScore = path, score
			}
		}

		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if found != "" {
		return found, nil
	}
	if versioned != "" {
		return versioned, nil
	}

	return "", fmt.Errorf(
		"压缩包里没有找到可执行文件（期望 terracotta / terracotta.exe 或 terracotta-<版本>-<平台> 形式的文件）")
}
