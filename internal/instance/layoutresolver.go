// 版本隔离布局解析：决定某个已安装版本的游戏/内容目录。
// 移植自 NyaLauncher.Core/Launch/GameInstanceLayoutResolver.cs。
package instance

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"nekolauncher/internal/tools"
)

// GameVersionLayout 版本隔离布局解析结果：内容目录与判定来源。
type GameVersionLayout struct {
	IsIsolated       bool
	ContentDirectory string
	Provider         string
	Evidence         string
}

// ExternalGameInstanceLayout 其它启动器管理的外部实例描述。
type ExternalGameInstanceLayout struct {
	InstanceId        string
	InstanceDirectory string
	ContentDirectory  string
	LauncherRoot      string
	Provider          string
	Evidence          string
}

// contentDirectories 视为 Minecraft 内容的子目录（必须非空才算证据）。
var contentDirectories = []string{"mods", "resourcepacks", "shaderpacks", "saves", "config"}

// contentFiles 视为 Minecraft 内容的特征文件。
var contentFiles = []string{"options.txt", "servers.dat"}

// minecraftFolderNames 版本目录内被视为独立内容目录的子目录名。
var minecraftFolderNames = []string{".minecraft", "minecraft"}

// knownPackLayouts 第三方启动器实例元数据文件与对应的启动器名称。
var knownPackLayouts = []struct{ Marker, Provider string }{
	{"minecraftinstance.json", "CurseForge"},
	{"profile.json", "Modrinth App"},
	{"instance.json", "ATLauncher"},
}

// ResolveLayout 解析实例的隔离布局（对应 C# GameInstanceLayoutResolver.Resolve）。
// 判定优先级：显式设置 > 自动检测（PCL/MultiMC/HMCL/整合包元数据/内容证据）> 全局默认兜底 > 共享目录。
//
// explicitIsolation：实例自身的显式隔离设置；非 nil 时直接生效。
// fallbackIsolation：全局默认隔离设置，仅在自动检测未发现任何隔离证据时兜底生效；
// 传 nil 表示未配置（视为关闭，即共享目录）。
func ResolveLayout(minecraftDirectory, sourcePath, versionID string, explicitIsolation, fallbackIsolation *bool) GameVersionLayout {
	mcRoot := trimEndingSeparator(mustAbs(minecraftDirectory))
	versionDirectory := filepath.Join(mcRoot, "versions", versionID)
	isolatedDirectory := resolveIsolatedContentDirectory(versionDirectory)

	// 1. 用户显式设置最优先
	if explicitIsolation != nil {
		if *explicitIsolation {
			return isolatedLayout(isolatedDirectory, "NekoLauncher", "用户已明确开启版本隔离")
		}
		return sharedLayout(mcRoot, "NekoLauncher", "用户已明确关闭版本隔离")
	}

	// 2. 自动检测：按第三方启动器特征与内容证据依次尝试
	if detected := detectAutoIsolation(mcRoot, versionDirectory, isolatedDirectory, sourcePath); detected != nil {
		return *detected
	}

	// 3. 自动检测未发现任何隔离证据：按全局默认兜底，未配置（nil）则保持共享目录
	if fallbackIsolation != nil && *fallbackIsolation {
		return isolatedLayout(
			isolatedDirectory,
			"NekoLauncher",
			"全局默认开启版本隔离（未检测到已有实例内容）")
	}
	return sharedLayout(mcRoot, "官方 / 共享布局", "未检测到版本独立内容或隔离设置")
}

// detectAutoIsolation 自动检测链：PCL > MultiMC > HMCL > 整合包元数据 > 独立内容目录 > 版本内内容 > 路径结构。
func detectAutoIsolation(mcRoot, versionDirectory, isolatedDirectory, sourcePath string) *GameVersionLayout {
	if pclSetting := tryReadPclIsolation(versionDirectory); pclSetting != nil {
		if *pclSetting {
			layout := isolatedLayout(isolatedDirectory, "PCL", "PCL Setup.ini 已开启版本隔离")
			return &layout
		}
		layout := sharedLayout(mcRoot, "PCL", "PCL Setup.ini 已关闭版本隔离")
		return &layout
	}

	if contentDirectory, ok := tryResolveMultiMcFamily(versionDirectory); ok {
		layout := isolatedLayout(
			contentDirectory,
			"MultiMC / Prism Launcher",
			"检测到 instance.cfg 与独立 minecraft/.minecraft 内容目录")
		return &layout
	}

	hmclMarker := filepath.Join(versionDirectory, ".hmclversion.cfg")
	if tools.FileExists(hmclMarker) && hasMinecraftContent(isolatedDirectory) {
		layout := isolatedLayout(
			isolatedDirectory,
			"HMCL",
			"检测到 .hmclversion.cfg 与版本独立内容")
		return &layout
	}

	if packDirectory, provider, ok := tryResolveKnownPackLayout(versionDirectory); ok {
		layout := isolatedLayout(
			packDirectory,
			provider,
			"检测到第三方启动器实例元数据与独立内容目录")
		return &layout
	}

	hasSeparateContent := !tools.PathsEqual(isolatedDirectory, versionDirectory) &&
		hasMinecraftContent(isolatedDirectory)
	if hasSeparateContent {
		layout := isolatedLayout(
			isolatedDirectory,
			"通用实例布局",
			"检测到独立 minecraft/.minecraft 内容目录")
		return &layout
	}

	if hasMinecraftContent(versionDirectory) {
		layout := isolatedLayout(
			versionDirectory,
			"通用版本隔离",
			"版本目录内存在模组、资源包、光影、配置或存档内容")
		return &layout
	}

	if IsVersionDirectorySource(sourcePath) {
		layout := isolatedLayout(
			isolatedDirectory,
			"路径结构",
			"当前添加路径是 versions/<版本> 实例目录")
		return &layout
	}

	return nil
}

// IsVersionDirectorySource 判断给定路径是否是 versions 目录下的真实版本子目录（versions/<版本>）。
func IsVersionDirectorySource(sourcePath string) bool {
	if strings.TrimSpace(sourcePath) == "" {
		return false
	}

	full, err := filepath.Abs(sourcePath)
	if err != nil {
		return false
	}
	info, err := os.Stat(full)
	if err != nil || !info.IsDir() {
		return false
	}
	parent := filepath.Dir(full)
	// 必须是真实存在的目录：versions/backup.zip 这类文件不应被当作实例目录
	// 触发隔离布局判定。父目录名比较与 PathsEqual 的平台大小写语义保持一致。
	parentIsVersions := equalFoldOnWindows(filepath.Base(parent), "versions")
	grandParent := filepath.Dir(parent)
	return parentIsVersions && grandParent != parent
}

// TryResolveExternalInstance 尝试把给定路径识别为其它启动器
// （MultiMC/CurseForge/Modrinth/ATLauncher）管理的外部实例，
// 并给出实例 ID、内容目录与所属启动器根目录。
func TryResolveExternalInstance(sourcePath string) (ExternalGameInstanceLayout, bool) {
	var layout ExternalGameInstanceLayout
	if strings.TrimSpace(sourcePath) == "" {
		return layout, false
	}

	selected := trimEndingSeparator(mustAbs(sourcePath))
	if info, err := os.Stat(selected); err != nil || !info.IsDir() {
		return layout, false
	}

	// 选中 .minecraft/minecraft 目录本身时，向上找一层带实例元数据的实例目录
	instanceDirectory := selected
	selectedName := filepath.Base(selected)
	parent := filepath.Dir(selected)
	if isMinecraftFolderName(selectedName) && hasExternalMarker(parent) {
		instanceDirectory = parent
	}

	provider, evidence, ok := getExternalMarker(instanceDirectory)
	if !ok {
		return layout, false
	}

	contentDirectory, ok := resolveExternalContentDirectory(instanceDirectory)
	if !ok {
		return layout, false
	}

	launcherRoot := resolveLauncherRoot(instanceDirectory)
	instanceID := filepath.Base(instanceDirectory)
	if strings.TrimSpace(instanceID) == "" {
		return layout, false
	}

	layout = ExternalGameInstanceLayout{
		InstanceId:        instanceID,
		InstanceDirectory: instanceDirectory,
		ContentDirectory:  contentDirectory,
		LauncherRoot:      launcherRoot,
		Provider:          provider,
		Evidence:          evidence,
	}
	return layout, true
}

// resolveLauncherRoot 实例目录的父目录若名为 instances，则再上一层是启动器根目录。
func resolveLauncherRoot(instanceDirectory string) string {
	instanceParent := filepath.Dir(instanceDirectory)
	insideInstancesFolder := instanceParent != instanceDirectory &&
		equalFoldOnWindows(filepath.Base(instanceParent), "instances")
	if !insideInstancesFolder {
		return instanceDirectory
	}
	grandParent := filepath.Dir(instanceParent)
	if grandParent == instanceParent {
		return instanceDirectory
	}
	return grandParent
}

// isMinecraftFolderName 目录名是否是 .minecraft 或 minecraft（不区分大小写）。
func isMinecraftFolderName(name string) bool {
	if name == "" {
		return false
	}
	for _, candidate := range minecraftFolderNames {
		if strings.EqualFold(candidate, name) {
			return true
		}
	}
	return false
}

// resolveIsolatedContentDirectory 版本隔离布局下的内容目录：版本目录内若有非空的
// .minecraft/minecraft 子目录则用它，否则就是版本目录本身。
func resolveIsolatedContentDirectory(versionDirectory string) string {
	for _, folderName := range minecraftFolderNames {
		candidate := filepath.Join(versionDirectory, folderName)
		if hasMinecraftContent(candidate) {
			return candidate
		}
	}
	return versionDirectory
}

// tryResolveMultiMcFamily 识别 MultiMC / Prism 系实例：instance.cfg 标记加独立 minecraft 目录。
func tryResolveMultiMcFamily(versionDirectory string) (string, bool) {
	cfgPath := filepath.Join(versionDirectory, "instance.cfg")
	if !tools.FileExists(cfgPath) {
		return "", false
	}

	for _, folderName := range minecraftFolderNames {
		candidate := filepath.Join(versionDirectory, folderName)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, true
		}
	}

	// 没有 minecraft 子目录但有内容时，版本目录本身就是内容目录
	if hasMinecraftContent(versionDirectory) {
		return versionDirectory, true
	}
	return "", false
}

// tryResolveKnownPackLayout 识别带实例元数据的整合包布局（CurseForge / Modrinth / ATLauncher）。
func tryResolveKnownPackLayout(versionDirectory string) (string, string, bool) {
	for _, known := range knownPackLayouts {
		markerPath := filepath.Join(versionDirectory, known.Marker)
		if !tools.FileExists(markerPath) {
			continue
		}
		contentDirectory := resolveIsolatedContentDirectory(versionDirectory)
		if info, err := os.Stat(contentDirectory); err == nil && info.IsDir() {
			return contentDirectory, known.Provider, true
		}
		return "", "", false
	}
	return "", "", false
}

// hasExternalMarker 实例目录是否存在外部实例元数据文件。
func hasExternalMarker(directory string) bool {
	_, _, ok := getExternalMarker(directory)
	return ok
}

// getExternalMarker 按优先级探测外部实例元数据文件，返回启动器名称与判定依据。
func getExternalMarker(directory string) (string, string, bool) {
	if tools.FileExists(filepath.Join(directory, "instance.cfg")) {
		return "MultiMC / Prism Launcher",
			"检测到外部 instance.cfg 与独立 minecraft/.minecraft 内容目录", true
	}
	if tools.FileExists(filepath.Join(directory, "minecraftinstance.json")) {
		return "CurseForge", "检测到外部 minecraftinstance.json 实例元数据", true
	}
	if tools.FileExists(filepath.Join(directory, "profile.json")) {
		return "Modrinth App", "检测到外部 profile.json 实例元数据", true
	}
	if tools.FileExists(filepath.Join(directory, "instance.json")) {
		return "ATLauncher", "检测到外部 instance.json 实例元数据", true
	}
	return "", "", false
}

// resolveExternalContentDirectory 外部实例的内容目录：优先 .minecraft/minecraft 子目录，退回实例目录本身。
func resolveExternalContentDirectory(instanceDirectory string) (string, bool) {
	for _, folderName := range minecraftFolderNames {
		candidate := filepath.Join(instanceDirectory, folderName)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, true
		}
	}
	if info, err := os.Stat(instanceDirectory); err == nil && info.IsDir() {
		return instanceDirectory, true
	}
	return "", false
}

// tryReadPclIsolation 读取 PCL 的 Setup.ini 隔离设置：VersionArgumentIndieV2 优先，
// 旧键 VersionArgumentIndie 兜底。文件缺失或不可读返回 nil。
func tryReadPclIsolation(versionDirectory string) *bool {
	setupPath := filepath.Join(versionDirectory, "PCL", "Setup.ini")
	if !tools.FileExists(setupPath) {
		return nil
	}

	data, err := os.ReadFile(setupPath)
	if err != nil {
		return nil
	}

	var legacySetting *bool
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		// Setup.ini 行格式为 "Key:Value"
		separator := strings.Index(line, ":")
		if separator <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:separator])
		parsed := parseIniBoolean(strings.TrimSpace(line[separator+1:]))
		if parsed == nil {
			continue
		}

		if strings.EqualFold(key, "VersionArgumentIndieV2") {
			return parsed
		}
		if strings.EqualFold(key, "VersionArgumentIndie") {
			legacySetting = parsed
		}
	}
	return legacySetting
}

// hasMinecraftContent 判断目录内是否存在真实的 Minecraft 内容。
// 注意：下载器预建的空骨架目录（mods/saves 等空文件夹）不算内容，
// 否则关闭全局隔离后，这些实例仍会被「检测到内容」分支误判为隔离。
func hasMinecraftContent(directory string) bool {
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return false
	}

	// 内容子目录必须至少包含一个文件/子项，空骨架目录不算
	for _, folderName := range contentDirectories {
		path := filepath.Join(directory, folderName)
		if entries, err := os.ReadDir(path); err == nil && len(entries) > 0 {
			return true
		}
	}

	for _, fileName := range contentFiles {
		if tools.FileExists(filepath.Join(directory, fileName)) {
			return true
		}
	}
	return false
}

// parseIniBoolean PCL Setup.ini 的布尔值支持 true/false 与 1/0 两种写法。
func parseIniBoolean(value string) *bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		result := true
		return &result
	case "false", "0":
		result := false
		return &result
	}
	return nil
}

func isolatedLayout(directory, provider, evidence string) GameVersionLayout {
	return GameVersionLayout{IsIsolated: true, ContentDirectory: directory, Provider: provider, Evidence: evidence}
}

func sharedLayout(directory, provider, evidence string) GameVersionLayout {
	return GameVersionLayout{IsIsolated: false, ContentDirectory: directory, Provider: provider, Evidence: evidence}
}

// equalFoldOnWindows Windows 文件系统大小写不敏感，其余平台区分大小写。
func equalFoldOnWindows(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// trimEndingSeparator 去掉路径尾部的目录分隔符，但保留盘符根
//（"C:\" 保持原样，与 config 包 trimTrailingSeparator 语义一致；
// 直接 strings.TrimRight 会把 "C:\" 剪成 "C:"）。
func trimEndingSeparator(path string) string {
	for len(path) > 0 {
		last := path[len(path)-1]
		if last != '/' && last != '\\' {
			break
		}
		// 已到根（父目录等于自身）时停止
		if filepath.Dir(path) == path {
			break
		}
		path = path[:len(path)-1]
	}
	return path
}
