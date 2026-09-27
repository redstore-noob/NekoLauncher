package bindings

// SystemAPI 扩展：背景图源增强 —— 必应每日一图缓存与 Wallpaper Engine 联动。
// 与 api_wallpaper.go 分离存放，避免与其他改动冲突。

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/tools"
)

// ---- 必应每日一图 ----

// bingImageArchive 必应官方 HPImageArchive 接口的响应结构。
type bingImageArchive struct {
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
}

// GetBingDailyImagePath 返回今日必应背景图的本地缓存路径；当日已缓存直接复用，
// 否则从必应官方 HPImageArchive 拉取并落盘。失败返回错误，前端回落远程直连地址。
func (a *SystemAPI) GetBingDailyImagePath() (string, error) {
	cacheDir := filepath.Join(config.StorageDirectory(), "cache", "bing")
	today := time.Now().Format("2006-01-02")
	cached := filepath.Join(cacheDir, today+".jpg")
	if info, err := os.Stat(cached); err == nil && info.Size() > 0 {
		return cached, nil
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}

	imageURL, err := fetchBingDailyImageURL()
	if err != nil {
		return "", err
	}
	resp, err := tools.SharedHTTPClient.Get(imageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("必应每日图下载失败：HTTP %d", resp.StatusCode)
	}
	// 限长读取 + JPEG 特征校验：非 2xx 之外，网关的错误页也会是 200，
	// 不校验就会把一页 HTML 当成今天的壁纸缓存 24 小时
	const maximumBingImageBytes = 32 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maximumBingImageBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maximumBingImageBytes {
		return "", fmt.Errorf("必应每日图体积超过上限")
	}
	if len(data) < 1024 || data[0] != 0xFF || data[1] != 0xD8 || data[2] != 0xFF {
		return "", fmt.Errorf("必应每日图下载内容异常")
	}

	// 临时文件 + 重命名，中断残留的 .tmp 不会污染当日缓存
	tmp := cached + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, cached); err != nil {
		os.Remove(tmp)
		return "", err
	}
	pruneBingCache(cacheDir, today)
	return cached, nil
}

// fetchBingDailyImageURL 从必应 HPImageArchive 拉取今日背景图地址；
// 国内主用 cn.bing.com，失败回落 www.bing.com。
func fetchBingDailyImageURL() (string, error) {
	archives := []string{
		"https://cn.bing.com/HPImageArchive.aspx?format=js&idx=0&n=1",
		"https://www.bing.com/HPImageArchive.aspx?format=js&idx=0&n=1",
	}
	var archive bingImageArchive
	for _, archiveURL := range archives {
		resp, err := tools.SharedHTTPClient.Get(archiveURL)
		if err != nil {
			continue
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			continue
		}
		if jsonErr := json.Unmarshal(data, &archive); jsonErr == nil && len(archive.Images) > 0 && archive.Images[0].URL != "" {
			imageURL := archive.Images[0].URL
			if !strings.HasPrefix(imageURL, "http") {
				imageURL = "https://cn.bing.com" + imageURL
			}
			return imageURL, nil
		}
	}
	return "", fmt.Errorf("获取必应每日图清单失败")
}

// pruneBingCache 清理 7 天前的历史缓存，目录只保留最近一周的图。
func pruneBingCache(dir, today string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -7)
	for _, entry := range entries {
		name := entry.Name()
		if name == today+".jpg" || !strings.HasSuffix(name, ".jpg") {
			continue
		}
		day, parseErr := time.Parse("2006-01-02", strings.TrimSuffix(name, ".jpg"))
		if parseErr != nil || day.Before(cutoff) {
			os.Remove(filepath.Join(dir, name))
		}
	}
}

// ---- Wallpaper Engine 联动 ----

// weWallpaperAppID Wallpaper Engine 的 Steam App ID（workshop 内容目录名）。
const weWallpaperAppID = "431960"

// WallpaperEngineWallpaper WE 联动结果：Path 为静态回退图路径（场景类型优先取
// scene.pkg 内提取的原画贴图，取不到才用预览图），Source 为壁纸原文件路径（图片本体或
// 视频，可清晰展示/播放；场景/网页等无静/可播原文件时为空），Title 为壁纸标题，
// Type 为壁纸类型，Web 为网页类壁纸的入口 HTML（相对项目根目录，其余类型为空串，
// 前端据此拼 /wwwallpaper/<Web> 交给 iframe 播放）。
type WallpaperEngineWallpaper struct {
	Path   string `json:"Path"`
	Source string `json:"Source"`
	Title  string `json:"Title"`
	Type   string `json:"Type"`
	Web    string `json:"Web"`
	// Unsupported 当前平台不支持与 Wallpaper Engine 联动（WE 本身只有 Windows 版）。
	// 前端据此给出"换个图源"的提示，而不是让用户对着一个永远加载不出的图源发呆。
	Unsupported bool `json:"Unsupported"`
}

// GetWallpaperEngineWallpaper 读取 Wallpaper Engine 当前应用的壁纸，返回
// 可用于背景的静态图片。未安装、未应用壁纸或无可用静态图时 Path 为空串。
func (a *SystemAPI) GetWallpaperEngineWallpaper() (*WallpaperEngineWallpaper, error) {
	if runtime.GOOS != "windows" {
		return &WallpaperEngineWallpaper{Unsupported: true}, nil
	}
	projectDir, err := wallpaperEngineSelectedProject()
	if err != nil || projectDir == "" {
		if err != nil {
			return nil, err
		}
		// WE 在运行但未应用任何壁纸（纯色/空白）
		return &WallpaperEngineWallpaper{}, nil
	}
	return wallpaperEngineWallpaperFromProject(projectDir)
}

// wallpaperEngineSelectedProject 从 WE config.json 解析当前壁纸所在的项目目录。
// 未安装返回错误；WE 存在但未应用壁纸返回空串。
func wallpaperEngineSelectedProject() (string, error) {
	configPath := wallpaperEngineConfigPath()
	if configPath == "" {
		return "", fmt.Errorf("未找到 Wallpaper Engine 安装")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return "", err
	}
	file := findSelectedWallpaperFile(root)
	if file == "" {
		return "", nil
	}
	return filepath.Dir(file), nil
}

// wallpaperEngineConfigPath 定位 Wallpaper Engine 的 config.json：
// 注册表读 Steam 安装目录 → 解析 libraryfolders.vdf 找出全部资料库 →
// 逐个检查 common/wallpaper_engine/config.json；找不到返回空串。
func wallpaperEngineConfigPath() string {
	for _, library := range steamLibraries(steamPathFromRegistry()) {
		candidate := filepath.Join(library, "steamapps", "common", "wallpaper_engine", "config.json")
		if tools.FileExists(candidate) {
			return candidate
		}
	}
	return ""
}

// steamPathFromRegistry 注册表 HKCU\Software\Valve\Steam 的 SteamPath；
// 平台实现见 api_wallpaper_steam_windows.go / _other.go（非 Windows 恒返回空串）。

// vdfPathPattern libraryfolders.vdf 中资料库路径条目（"path" "C:\...\SteamLibrary"）。
var vdfPathPattern = regexp.MustCompile(`"path"\s+"([^"]+)"`)

// steamLibraries 返回全部 Steam 资料库根目录（主库在前），去重保序。
func steamLibraries(steamRoot string) []string {
	libraries := make([]string, 0, 4)
	if steamRoot != "" {
		libraries = append(libraries, steamRoot)
	}
	if data, err := os.ReadFile(filepath.Join(steamRoot, "steamapps", "libraryfolders.vdf")); err == nil {
		for _, match := range vdfPathPattern.FindAllStringSubmatch(string(data), -1) {
			libraries = append(libraries, match[1])
		}
	}
	seen := make(map[string]bool, len(libraries))
	unique := libraries[:0]
	for _, library := range libraries {
		normalized := strings.ToLower(filepath.Clean(library))
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		unique = append(unique, library)
	}
	return unique
}

// findSelectedWallpaperFile 在 config.json 顶层找到当前应用的壁纸文件路径。
// 顶层按 Windows 用户名分层，不猜用户名：逐个分支找
// general.wallpaperconfig.selectedwallpapers.<MonitorN>.file，取编号最小的显示器。
func findSelectedWallpaperFile(root map[string]any) string {
	monitors := make([]string, 0, 1)
	filesByMonitor := make(map[string]string, 1)
	for _, section := range root {
		sectionMap, ok := section.(map[string]any)
		if !ok {
			continue // 安装目录等字符串键直接跳过
		}
		general, _ := sectionMap["general"].(map[string]any)
		wallpaperConfig, _ := general["wallpaperconfig"].(map[string]any)
		selected, _ := wallpaperConfig["selectedwallpapers"].(map[string]any)
		for monitor, entry := range selected {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			file, _ := entryMap["file"].(string)
			if file == "" {
				continue
			}
			if _, exists := filesByMonitor[monitor]; !exists {
				monitors = append(monitors, monitor)
			}
			filesByMonitor[monitor] = file
		}
	}
	if len(monitors) == 0 {
		return ""
	}
	sort.Strings(monitors) // Monitor0 优先于 Monitor1
	return filesByMonitor[monitors[0]]
}

// weProjectMeta 壁纸 project.json 里我们关心的字段。
type weProjectMeta struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	File    string `json:"file"`
	Preview string `json:"preview"`
	// Dependency 指向真正负责渲染的"依赖项目"（创意工坊 id）。
	Dependency string `json:"dependency"`
}

// weDependencyProjectDirectory 解析 dependency 指向的项目目录与它的 project.json。
//
// WE 的"皮肤/预设"型壁纸自己不带 type/file，只有一个 dependency 指向模板项目
// （例：创意工坊 #3694164989「新约能天使」依赖 #884307090「Perfect Wallpaper」，
// 后者才是 type=web + file=index.html 的网页壁纸）。不跟这一层的话，
// 选中的项目里既没有 type 也没有 file，最后只能退回一张静态预览图——
// 表现就是"网页壁纸不能用"。
func weDependencyProjectDirectory(projectDir, dependency string) (string, weProjectMeta) {
	id := strings.TrimSpace(dependency)
	// 只接受同级目录下的纯 id：带分隔符的输入一律不认，避免跳目录
	if id == "" || strings.ContainsAny(id, `/\`) || id == "." || id == ".." {
		return "", weProjectMeta{}
	}
	candidate := filepath.Join(filepath.Dir(projectDir), id)
	if candidate == projectDir {
		return "", weProjectMeta{}
	}
	data, err := os.ReadFile(filepath.Join(candidate, "project.json"))
	if err != nil {
		return "", weProjectMeta{}
	}
	var meta weProjectMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", weProjectMeta{}
	}

	return candidate, meta
}

// wallpaperEngineWallpaperFromProject 从壁纸项目目录解析可用于背景的文件：
// 原文件（Source）优先取壁纸本体——图片直接展示、视频直接播放，避免低分辨率
// 预览图被拉伸发糊；场景类型再从 scene.pkg 里提取原画贴图，网页等确实没有可用
// 原文件时，才用 project.json 声明的预览图（声明缺失时按常用命名探测）兜底。
//
// 预设型壁纸（只有 dependency）会先跟到依赖项目去取 type/file，但标题与预览图
// 仍用用户实际选中的那个——那是他在 WE 里看到的样子。
func wallpaperEngineWallpaperFromProject(projectDir string) (*WallpaperEngineWallpaper, error) {
	data, err := os.ReadFile(filepath.Join(projectDir, "project.json"))
	if err != nil {
		return nil, err
	}
	var project weProjectMeta
	if err := json.Unmarshal(data, &project); err != nil {
		return nil, err
	}

	// 渲染来源：自身缺 type/file 时跟随 dependency（只跟一层，够用且不会成环）
	renderDir, renderType, renderFile := projectDir, project.Type, project.File
	if strings.TrimSpace(renderType) == "" || strings.TrimSpace(renderFile) == "" {
		if dependencyDir, dependency := weDependencyProjectDirectory(projectDir, project.Dependency); dependencyDir != "" {
			renderDir, renderType, renderFile = dependencyDir, dependency.Type, dependency.File
		}
	}

	result := &WallpaperEngineWallpaper{Title: project.Title, Type: renderType}
	if result.Title == "" {
		result.Title = project.Dependency
	}

	// 网页类壁纸：整个项目目录经 /wwwallpaper 路由交给背景层的 iframe 呈现，
	// Path 取预览图作为加载期间的兜底底图（web 壁纸没有可直出的图片/视频原文件）
	if strings.EqualFold(renderType, "web") {
		if entry := weWebEntryPath(renderDir, renderFile); entry != "" {
			result.Web = entry
			setWebWallpaperTarget(renderDir, entry)
		} else {
			setWebWallpaperTarget("", "")
		}
		result.Path, _ = weBestPreview(projectDir, project.Preview)

		return result, nil
	}
	setWebWallpaperTarget("", "")

	if renderFile != "" {
		original := filepath.Join(renderDir, filepath.FromSlash(renderFile))
		if tools.FileExists(original) && (isImagePath(original) || isVideoPath(original)) {
			result.Source = original
		}
	}
	// 图片本体即静态图，直接作为回退图（Source/Path 同为原图）
	if result.Source != "" && isImagePath(result.Source) {
		result.Path = result.Source
		return result, nil
	}
	// 场景壁纸：project.json 声明的多是 scene.json——它被打包在 scene.pkg 里，磁盘上并不
	// 单独存在，只找预览图会落到 192×192 级别的缩略图上，铺满屏就是"糊"。pkg 里通常存着
	// 原画贴图，取像素面积更大的那张作为静态背景。
	previewPath, previewArea := weBestPreview(projectDir, project.Preview)
	if pkgPath, pkgArea := weScenePackageImage(projectDir); pkgPath != "" && pkgArea > previewArea {
		result.Path = pkgPath
		return result, nil
	}
	result.Path = previewPath
	return result, nil
}

// weWebEntryPath 网页壁纸的入口 HTML：project.json 的 file 优先，
// 缺失或不是 .html 时探测 index.html。返回相对项目根目录的斜杠路径；找不到返回空串。
func weWebEntryPath(projectDir, declared string) string {
	candidates := make([]string, 0, 2)
	if strings.TrimSpace(declared) != "" {
		candidates = append(candidates, filepath.ToSlash(declared))
	}
	candidates = append(candidates, "index.html")
	for _, name := range candidates {
		// 归一化并挡掉越界：入口必须落在项目目录内
		cleaned := strings.TrimPrefix(path.Clean("/"+name), "/")
		if cleaned == "" || !strings.HasSuffix(strings.ToLower(cleaned), ".html") {
			continue
		}
		if tools.FileExists(filepath.Join(projectDir, filepath.FromSlash(cleaned))) {
			return cleaned
		}
	}

	return ""
}

// weBestPreview 在候选预览图里取像素面积最大的一张。project.json 声明的名字优先参与
// 比较，但不"命中即用"——同一壁纸的 preview.gif 与 preview.jpg 分辨率可能差很多。
func weBestPreview(projectDir, declared string) (string, int) {
	bestPath, bestArea := "", 0
	for _, name := range wePreviewCandidates(declared) {
		candidate := filepath.Join(projectDir, filepath.FromSlash(name))
		if area := weImageFileArea(candidate); area > bestArea {
			bestPath, bestArea = candidate, area
		}
	}
	return bestPath, bestArea
}

// wePreviewCandidates 预览图探测顺序：project.json 声明的名字优先，其余为常用命名。
func wePreviewCandidates(declared string) []string {
	candidates := make([]string, 0, 4)
	if declared != "" {
		candidates = append(candidates, filepath.FromSlash(declared))
	}
	return append(candidates, "preview.jpg", "preview.png", "preview.gif")
}

// isImagePath 路径扩展名是否为 /localfile 白名单内的图片格式。
func isImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
		return true
	default:
		return false
	}
}

// isVideoPath 路径扩展名是否为 WebView（Chromium）可直接播放的视频格式；
// 仅放行常见可播容器，避免把 mkv/avi 等无法解码的文件交给前端空等。
func isVideoPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".webm", ".ogv", ".m4v", ".mov":
		return true
	default:
		return false
	}
}
