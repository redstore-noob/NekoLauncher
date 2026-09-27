// Package update 启动器自身更新：查 GitHub Releases → 比对版本 → 下载新可执行文件 →
// （Windows）就地替换并重启。
//
// 与"更新游戏/模组"无关：这里只处理启动器自己的 exe。
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ReleasesRepo 启动器自己的 GitHub 仓库（owner/name）。
// 说明：仓库曾用名 NyaLauncher，GitHub 会重定向，但 API 规范名是 NekoLauncher。
const ReleasesRepo = "redstore-noob/NekoLauncher"

// releasesAPI 列出版本的接口。用列表而不是 /releases/latest：需要按需过滤预发布版，
// 而 latest 接口会自动跳过预发布（这个项目恰恰以 preview 版为主）。
var releasesAPI = "https://api.github.com/repos/" + ReleasesRepo + "/releases?per_page=30"

// ReleasePageURL 版本页地址（查不到可用资产时指引用户手动下载）。
const ReleasePageURL = "https://github.com/" + ReleasesRepo + "/releases"

// requestTimeout 单次查询超时。
const requestTimeout = 15 * time.Second

// releaseAsset GitHub 版本资产。
type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// release GitHub 版本。
type release struct {
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	PublishedAt time.Time      `json:"published_at"`
	Prerelease  bool           `json:"prerelease"`
	Draft       bool           `json:"draft"`
	HTMLURL     string         `json:"html_url"`
	Assets      []releaseAsset `json:"assets"`
}

// Asset 选中给当前平台的可下载资产。
type Asset struct {
	Name string `json:"Name"`
	URL  string `json:"URL"`
	// Size 字节数（0 表示未知）。
	Size int64 `json:"Size"`
}

// CheckResult 一次更新检查的结果（前端直接展示，字段不再二次加工）。
type CheckResult struct {
	// CurrentVersion 当前启动器版本（info.Version()）。
	CurrentVersion string `json:"CurrentVersion"`
	// LatestVersion 远端最新版本号（去掉 v 前缀的 tag）。
	LatestVersion string `json:"LatestVersion"`
	// UpdateAvailable 是否有更新的版本。
	UpdateAvailable bool `json:"UpdateAvailable"`
	// Prerelease 最新版本是否为预发布版。
	Prerelease bool `json:"Prerelease"`
	// Notes 版本说明（release body，可能为空）。
	Notes string `json:"Notes"`
	// PublishedAt 发布时间（Unix 秒；0 表示未知）。
	PublishedAt int64 `json:"PublishedAt"`
	// Asset 当前平台可用的下载资产；为 nil 时只能手动下载（见 ManualHint/PageURL）。
	Asset *Asset `json:"Asset"`
	// PageURL 版本页地址（手动下载入口）。
	PageURL string `json:"PageURL"`
	// ManualHint 非空表示"能查到新版本但没法自动装"的原因（例如本平台没有可直接替换的资产）。
	ManualHint string `json:"ManualHint"`
	// CanSelfUpdate 当前平台是否支持自动替换自身（Windows 支持；其它平台要用户手动换）。
	CanSelfUpdate bool `json:"CanSelfUpdate"`
}

// CanSelfUpdate 当前平台能否就地替换启动器本体。
func CanSelfUpdate() bool { return runtime.GOOS == "windows" }

// Check 查询最新版本并与 currentVersion 比较。
//
// includePrerelease 为 false 时忽略预发布版（用户只想要稳定版）；
// 项目当前以 preview 版为主，所以界面上默认勾选"包含预发布"。
func Check(ctx context.Context, currentVersion string, includePrerelease bool) (CheckResult, error) {
	result := CheckResult{
		CurrentVersion: strings.TrimSpace(currentVersion),
		PageURL:        ReleasePageURL,
		CanSelfUpdate:  CanSelfUpdate(),
	}

	releases, err := fetchReleases(ctx)
	if err != nil {
		return result, err
	}
	if len(releases) == 0 {
		return result, fmt.Errorf("仓库还没有发布任何版本")
	}

	latest := pickLatestRelease(releases, includePrerelease)
	if latest == nil {
		return result, fmt.Errorf("没有符合条件（%s）的版本，可在版本页手动查看", releaseFilterLabel(includePrerelease))
	}

	result.LatestVersion = normalizeVersion(latest.TagName)
	result.Prerelease = latest.Prerelease
	result.Notes = strings.TrimSpace(latest.Body)
	if !latest.PublishedAt.IsZero() {
		result.PublishedAt = latest.PublishedAt.Unix()
	}
	if strings.TrimSpace(latest.HTMLURL) != "" {
		result.PageURL = latest.HTMLURL
	}

	result.UpdateAvailable = CompareVersions(result.LatestVersion, result.CurrentVersion) > 0

	asset, ok := pickAsset(latest.Assets)
	if ok {
		result.Asset = &asset
	} else {
		result.ManualHint = fmt.Sprintf(
			"这个版本没有适用于 %s/%s 的直接下载文件，请到版本页手动下载。",
			runtime.GOOS, runtime.GOARCH)
	}
	if result.UpdateAvailable && result.Asset == nil {
		result.ManualHint = fmt.Sprintf(
			"新版本 %s 没有适用于 %s/%s 的直接下载文件，请到版本页手动下载。",
			result.LatestVersion, runtime.GOOS, runtime.GOARCH)
	}

	return result, nil
}

// fetchReleases 拉取版本列表（带 UA 与 GitHub 推荐的 Accept 头）。
func fetchReleases(ctx context.Context) ([]release, error) {
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, releasesAPI, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "NekoLauncher-Updater")
	request.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: requestTimeout}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("访问 GitHub 失败：%w", err)
	}
	defer func() { _ = response.Body.Close() }()

	payload, _ := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	switch {
	case response.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("版本页不存在（%s）", ReleasesRepo)
	case response.StatusCode == http.StatusForbidden, response.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("GitHub 接口限流，请稍后再试")
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return nil, fmt.Errorf("GitHub 返回 %d", response.StatusCode)
	}

	var releases []release
	if err := json.Unmarshal(payload, &releases); err != nil {
		return nil, fmt.Errorf("版本列表解析失败：%w", err)
	}

	return releases, nil
}

// pickLatestRelease 按版本号挑最新的一个（跳过草稿；includePrerelease=false 时跳过预发布）。
// 不依赖接口返回顺序：GitHub 按创建时间排，打补丁版时可能与版本号顺序不一致。
func pickLatestRelease(releases []release, includePrerelease bool) *release {
	var latest *release
	for index := range releases {
		candidate := &releases[index]
		if candidate.Draft {
			continue
		}
		if candidate.Prerelease && !includePrerelease {
			continue
		}
		if strings.TrimSpace(candidate.TagName) == "" {
			continue
		}
		if latest == nil ||
			CompareVersions(normalizeVersion(candidate.TagName), normalizeVersion(latest.TagName)) > 0 {
			latest = candidate
		}
	}

	return latest
}

// releaseFilterLabel 过滤条件的用户可读描述。
func releaseFilterLabel(includePrerelease bool) string {
	if includePrerelease {
		return "含预发布"
	}

	return "仅稳定版"
}

// pickAsset 选当前平台可直接使用的资产。
//
// 命名来自 CI（.github/workflows/ci.yml）：
//
//	windows: NekoLauncher.exe / NekoLauncher-windows-amd64-portable.zip
//	linux:   NekoLauncher-linux-amd64.AppImage / .tar.gz
//	darwin:  NekoLauncher-darwin-universal.zip
//
// 自动替换只用"能直接落到目标路径"的那一个：Windows 用裸 exe；其它平台（AppImage/zip）
// 留给用户手动处理（返回 false → ManualHint）。
func pickAsset(assets []releaseAsset) (Asset, bool) {
	if runtime.GOOS != "windows" {
		return Asset{}, false
	}

	want := fmt.Sprintf("NekoLauncher-%s-%s", runtime.GOOS, runtime.GOARCH)
	for _, asset := range assets {
		name := strings.TrimSpace(asset.Name)
		if name == "" || strings.TrimSpace(asset.URL) == "" {
			continue
		}
		lower := strings.ToLower(name)
		switch {
		case lower == "nekolauncher.exe":
			return Asset{Name: name, URL: asset.URL, Size: asset.Size}, true
		case strings.HasPrefix(lower, strings.ToLower(want)) && strings.HasSuffix(lower, ".exe"):
			return Asset{Name: name, URL: asset.URL, Size: asset.Size}, true
		}
	}

	return Asset{}, false
}

// normalizeVersion 去掉 tag 的 v 前缀与首尾空白。
func normalizeVersion(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "v")
	value = strings.TrimPrefix(value, "V")

	return value
}

// CompareVersions 比较两个版本号：a > b 返回正数，相等 0，a < b 返回负数。
//
// 支持的形态：`1.2.3`、`1.2.3-preview4`、`1.2.3-beta`、`Nya_Rebuild`（无法解析时按 0 处理）。
// 规则：数字段逐段比较；有预发布后缀的**小于**同核心的正式版；两个预发布按
// "字母前缀 + 数字后缀"比较（preview9 < preview10，beta < preview）。
func CompareVersions(a, b string) int {
	coreA, preA := splitVersion(a)
	coreB, preB := splitVersion(b)

	if diff := compareCore(coreA, coreB); diff != 0 {
		return diff
	}
	switch {
	case preA == "" && preB == "":
		return 0
	case preA == "":
		return 1 // 正式版 > 预发布
	case preB == "":
		return -1
	default:
		return comparePrerelease(preA, preB)
	}
}

// splitVersion 拆成数字核心与预发布后缀。
func splitVersion(version string) ([]int, string) {
	value := normalizeVersion(version)
	if value == "" {
		return nil, ""
	}

	core := value
	pre := ""
	if index := strings.IndexAny(value, "-+"); index >= 0 {
		core = value[:index]
		pre = strings.TrimSpace(value[index+1:])
	}

	parts := strings.Split(core, ".")
	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		// 非数字段（例如 Nya_Rebuild 整体）按 0 处理，避免把版本比较变成随机结果
		number, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			number = 0
		}
		numbers = append(numbers, number)
	}

	return numbers, pre
}

// compareCore 逐段比较数字核心（长度不同时缺位按 0）。
func compareCore(a, b []int) int {
	length := len(a)
	if len(b) > length {
		length = len(b)
	}
	for index := 0; index < length; index++ {
		left, right := 0, 0
		if index < len(a) {
			left = a[index]
		}
		if index < len(b) {
			right = b[index]
		}
		if left != right {
			if left > right {
				return 1
			}

			return -1
		}
	}

	return 0
}

// comparePrerelease 比较预发布后缀：先比字母前缀，再比数字（preview9 < preview10）。
func comparePrerelease(a, b string) int {
	prefixA, numberA := splitPrerelease(a)
	prefixB, numberB := splitPrerelease(b)

	if prefixA != prefixB {
		if prefixA < prefixB {
			return -1
		}

		return 1
	}
	switch {
	case numberA == numberB:
		return 0
	case numberA > numberB:
		return 1
	default:
		return -1
	}
}

// splitPrerelease 把预发布后缀拆成字母前缀与末尾数字（beta2 → "beta", 2）。
func splitPrerelease(value string) (string, int) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	index := len(trimmed)
	for index > 0 {
		char := trimmed[index-1]
		if char < '0' || char > '9' {
			break
		}
		index--
	}
	if index == len(trimmed) {
		return trimmed, 0
	}
	number, err := strconv.Atoi(trimmed[index:])
	if err != nil {
		return trimmed, 0
	}

	return trimmed[:index], number
}
