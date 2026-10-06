// Minecraft 安装目录定位：移植自 NyaLauncher.Core/Launch/LaunchTool.cs 内的
// MinecraftInstallationLocation / MinecraftDirectoryLocator。
// 原本属于 launch 侧的公共依赖，因 instance/world/content 均需消费且不能依赖
// 尚在移植中的 internal/launch，先落在 instance 包内（见 PORTING_NOTES.md）。
package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"nekolauncher/internal/config"
	"nekolauncher/internal/logs"
	"nekolauncher/internal/tools"
)

// MinecraftInstallationLocation 描述一次 Minecraft 安装路径解析的结果。
// 根据传入路径是"根目录"还是"versions/版本号 独立实例目录"，字段会有不同的取值。
type MinecraftInstallationLocation struct {
	// MinecraftDirectory Minecraft 根目录（如 ~/.minecraft），始终非空。
	MinecraftDirectory string
	// PreferredVersionId 传入的是独立实例目录时为对应的版本号；传入根目录时为空串。
	PreferredVersionId string
	// GameDirectory 独立实例目录（用于隔离 mods、config、saves 等）；传入根目录时为空串。
	GameDirectory string
}

// standardSubDirectories Minecraft 标准目录结构中应当存在的子文件夹列表。
var standardSubDirectories = []string{
	"versions", "assets", "libraries", "saves", "resourcepacks", "mods",
	"config", "crash-reports", "logs", "screenshots", "shaderpacks",
}

// GetDefaultDirectory 获取当前操作系统下 Minecraft 官方启动器使用的默认 .minecraft 目录。
func GetDefaultDirectory() string {
	home := toolsHome()
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" && home != "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, ".minecraft")
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "minecraft")
	}
	// Linux 及其他未识别系统：~/.minecraft
	return filepath.Join(home, ".minecraft")
}

func toolsHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// EnsureDefaultDirectory 检测默认 Minecraft 目录是否存在；不存在时在平台默认路径下
// 创建符合 Minecraft 目录结构的空文件夹骨架。返回保证存在的默认目录路径。
func EnsureDefaultDirectory() string {
	defaultDir := GetDefaultDirectory()
	if info, err := os.Stat(defaultDir); err != nil || !info.IsDir() {
		_ = os.MkdirAll(defaultDir, 0o755)
		for _, sub := range standardSubDirectories {
			_ = os.MkdirAll(filepath.Join(defaultDir, sub), 0o755)
		}
	}
	return defaultDir
}

func init() {
	// 供 internal/config 的目录目录保护逻辑使用（C# 侧由 MinecraftDirectoryLocator 提供）。
	config.DefaultMinecraftDirectoryLocator = EnsureDefaultDirectory
	// 纯查询版（无建目录副作用），config 侧做排除名单比较时用
	config.DefaultMinecraftDirectoryPath = GetDefaultDirectory
}

// ResolveInstallationPath 接受 Minecraft 根目录，或 versions/<版本号> 形式的独立实例目录。
// 路径为空、不存在或不是有效的 Minecraft 目录时返回 error（对应 C# MinecraftLaunchException）。
func ResolveInstallationPath(path string) (MinecraftInstallationLocation, error) {
	var location MinecraftInstallationLocation
	// 空路径直接拒绝
	if strings.TrimSpace(path) == "" {
		return location, fmt.Errorf("Minecraft 路径不能为空")
	}

	// 规范化：去掉首尾空白与引号、展开环境变量（%VAR%/$VAR）、转换为绝对路径
	trimmed := strings.TrimSpace(path)
	trimmed = strings.Trim(trimmed, `"`)
	expanded := os.ExpandEnv(trimmed)
	if runtime.GOOS == "windows" {
		expanded = expandWindowsEnv(expanded)
	}
	fullPath, err := filepath.Abs(expanded)
	if err != nil {
		return location, fmt.Errorf("Minecraft 路径不存在：%s", fullPath)
	}
	if info, err := os.Stat(fullPath); err != nil || !info.IsDir() {
		return location, fmt.Errorf("Minecraft 路径不存在：%s", fullPath)
	}

	// 判断是否为 "versions/<版本号>" 形式的独立实例目录：
	// 父目录名为 versions，且目录内存在与目录同名的 <版本号>.json 版本描述文件
	directoryName := filepath.Base(fullPath)
	parent := filepath.Dir(fullPath)
	if equalFoldOnWindows(filepath.Base(parent), "versions") &&
		tools.FileExists(filepath.Join(fullPath, directoryName+".json")) {
		// 版本目录的上一级（versions 的父级）即为 Minecraft 根目录
		root := filepath.Dir(parent)
		if root == parent {
			return location, fmt.Errorf("无法确定版本目录对应的 Minecraft 根目录")
		}
		// 根目录仍需通过校验（必须包含 versions 文件夹）
		if err := validateRootDirectory(root); err != nil {
			return location, err
		}
		return MinecraftInstallationLocation{MinecraftDirectory: root, PreferredVersionId: directoryName, GameDirectory: fullPath}, nil
	}

	// 不是实例目录，则按普通根目录处理并校验
	if err := validateRootDirectory(fullPath); err != nil {
		return location, err
	}
	return MinecraftInstallationLocation{MinecraftDirectory: fullPath}, nil
}

// expandWindowsEnv 展开 %VAR% 形式的环境变量（os.ExpandEnv 只处理 $VAR）。
func expandWindowsEnv(path string) string {
	for {
		start := strings.Index(path, "%")
		if start < 0 {
			return path
		}
		end := strings.Index(path[start+1:], "%")
		if end < 0 {
			return path
		}
		name := path[start+1 : start+1+end]
		value, ok := os.LookupEnv(name)
		if !ok {
			return path
		}
		path = path[:start] + value + path[start+end+2:]
	}
}

// maxVersionJSONSize 版本描述文件的大小上限：超过视为异常文件（官方 json 通常 < 1 MiB）。
const maxVersionJSONSize = 1 << 20

// maxInheritsFromDepth inheritsFrom 父链校验的最大回溯深度（同时防环）。
const maxInheritsFromDepth = 3

// versionJSONMeta 扫描时关心的版本描述文件最小字段集合。
type versionJSONMeta struct {
	InheritsFrom string `json:"inheritsFrom"`
	// Jar 显式指定客户端 jar 的来源版本（官方规范字段，Forge/OptiFine 与
	// 第三方启动器造的"变体版本"常用：自己的目录里只有 json，jar 借用别的版本）。
	Jar string `json:"jar"`
}

// GetInstalledVersionIds 扫描指定 Minecraft 根目录下已安装的版本列表。
//
// 目录约定：versions/<id>/<id>.json —— 整条启动 / 下载 / 改名管线都依赖该布局，
// 扫描不得产出违反约定的版本 ID。
//
// 有效性判定（C# 时代"有 json 即有效"的重写版）：
//  1. versions/<id>/<id>.json 存在、非空且可解析为 JSON；
//  2. 满足任一：
//     - 存在 versions/<id>/<id>.jar（自含客户端的完整版本）；
//     - json 的 jar 字段指向一个真实存在的客户端 jar；
//     - json 声明 inheritsFrom 且父版本本身有效（jar 由父版本沿链提供），
//     父链最多回溯 maxInheritsFromDepth 层并防环。
//
// 借此过滤下载中断的残缺目录（只有 json 没有 jar）、损坏的 json、
// 以及父版本已被删除的孤儿 Loader 实例——这些此前会进入列表但启动必然失败。
// 被过滤的目录写入 DEBUG 日志便于排查。结果按名称忽略大小写降序排列。
func GetInstalledVersionIds(minecraftDirectory string) []string {
	ids, _ := GetInstalledVersionIdsChecked(minecraftDirectory)

	return ids
}

// GetInstalledVersionIdsChecked 同 GetInstalledVersionIds，但用 ok 区分
// "versions 目录里确实没有可用版本"（ok=true）与"目录存在却读不出来"
// （ok=false，目录被占用 / 杀软扫描 / 权限不足）。
// 调用方必须据此决定是否清理实例配置：把读取失败当成空集会把该目录下
// 所有实例的隔离、内存、Java 路径等设置一次性抹掉。
func GetInstalledVersionIdsChecked(minecraftDirectory string) ([]string, bool) {
	versionsDirectory := filepath.Join(minecraftDirectory, "versions")
	// 尚未下载任何版本时直接返回空列表
	if info, err := os.Stat(versionsDirectory); err != nil || !info.IsDir() {
		return []string{}, true
	}

	entries, err := os.ReadDir(versionsDirectory)
	if err != nil {
		logs.Write("WARN", "读取 versions 目录失败，本次跳过实例配置清理："+err.Error())

		return []string{}, false
	}

	var ids []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "" {
			continue
		}
		id := entry.Name()
		reason := validateVersionInstance(filepath.Join(versionsDirectory, id), id, 0)
		if reason == "" {
			ids = append(ids, id)
			continue
		}
		logsWriteScanSkip(id, reason)
	}

	sort.Slice(ids, func(i, j int) bool {
		return strings.ToLower(ids[i]) > strings.ToLower(ids[j])
	})
	if ids == nil {
		ids = []string{}
	}
	return ids, true
}

// validateVersionInstance 校验 versions/<id>/ 是否构成一个可启动的实例。
// 返回空串表示有效；否则返回被过滤的原因（用于日志）。
func validateVersionInstance(versionDir, id string, depth int) string {
	jsonPath := filepath.Join(versionDir, id+".json")
	info, err := os.Stat(jsonPath)
	if err != nil || info.IsDir() {
		return "缺少版本描述文件 " + id + ".json"
	}
	if info.Size() == 0 {
		return "版本描述文件为空"
	}
	if info.Size() > maxVersionJSONSize {
		return "版本描述文件异常过大"
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		return "版本描述文件不可读"
	}
	var meta versionJSONMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return "版本描述文件不是有效 JSON"
	}

	// 自含客户端 jar 即为完整版本
	if tools.FileExists(filepath.Join(versionDir, id+".jar")) {
		return ""
	}

	// 没有自己的 jar：json 里的 jar 字段指向另一个版本时，客户端 jar 由那个版本提供。
	// 启动侧（version_profile_loader）认这个字段，扫描侧也必须认——否则这些版本的
	// 实例会从列表里消失，而扫描末尾的 PruneMissingVersions 还会顺手删掉它们的
	// 内存/Java/窗口/隔离等全部实例配置。
	if jar := strings.TrimSpace(meta.Jar); jar != "" && !strings.EqualFold(jar, id) {
		jarPath := filepath.Join(filepath.Dir(versionDir), jar, jar+".jar")
		if tools.FileExists(jarPath) {
			return ""
		}

		return "json 声明的客户端 jar 不存在：" + jar
	}

	// 无 jar：声明 inheritsFrom 时客户端由父版本沿链提供
	parent := strings.TrimSpace(meta.InheritsFrom)
	if parent == "" {
		return "缺少客户端 jar 且未声明 inheritsFrom"
	}
	if depth >= maxInheritsFromDepth {
		return "inheritsFrom 链过深"
	}
	if strings.EqualFold(parent, id) {
		return "inheritsFrom 指向自身"
	}
	parentDir := filepath.Join(filepath.Dir(versionDir), parent)
	if dirExistsIn(parentDir) != nil {
		return "inheritsFrom 父版本目录不存在：" + parent
	}
	if reason := validateVersionInstance(parentDir, parent, depth+1); reason != "" {
		return "inheritsFrom 父版本无效（" + parent + "）：" + reason
	}
	return ""
}

// dirExistsIn path 存在且为目录时返回 nil。
func dirExistsIn(path string) error {
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return fmt.Errorf("目录不存在：%s", path)
	}
	return nil
}

// logsWriteScanSkip 记录被扫描过滤的版本目录（失败不影响扫描本身）。
func logsWriteScanSkip(id, reason string) {
	logs.Write("DEBUG", fmt.Sprintf("实例扫描跳过 %s：%s", id, reason))
}

// ErrNotMinecraftRoot 目录缺少 versions 文件夹、不像 Minecraft 根目录的错误。
// 用哨兵错误区分"只是选了个普通文件夹"与权限 / IO 等真实故障：
// 前者由实例扫描降级为空快照（见 store.go scan），后者才作为扫描失败上报。
var ErrNotMinecraftRoot = errors.New("该路径不是有效的 Minecraft 根目录，缺少 versions 文件夹")

// IsInvalidRootDirectory 错误是否属于"路径存在但不像 Minecraft 根目录"这一类。
func IsInvalidRootDirectory(err error) bool {
	return errors.Is(err, ErrNotMinecraftRoot)
}

// validateRootDirectory 校验指定路径是否为有效的 Minecraft 根目录（必须包含 versions 文件夹）。
func validateRootDirectory(root string) error {
	if info, err := os.Stat(filepath.Join(root, "versions")); err != nil || !info.IsDir() {
		return fmt.Errorf("%w：%s", ErrNotMinecraftRoot, root)
	}
	return nil
}
