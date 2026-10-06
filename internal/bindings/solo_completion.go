package bindings

// NekoSolo v3 首启内容补全：实现 solo.PendingPayloadHook。
// 安装器只解压启动器并把待装 mrpack 落盘；这里在启动器首次启动时串起
// 既有链路补全一切——装 MC/Loader（MC 本体联网下载）→ 装 mod（联网下载 +
// overrides 落盘）→ 装匹配的 Java 运行时（联网下载）。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"nekolauncher/internal/config"
	"nekolauncher/internal/download"
	"nekolauncher/internal/instance"
	"nekolauncher/internal/logs"
	"nekolauncher/internal/models"
	"nekolauncher/internal/solo"
)

// wireSolo 把首启补全钩子接进 solo 包（solo 不依赖 download，经此反转）。
func (a *API) wireSolo() {
	solo.PendingPayloadHook = a.completeSoloPendingPayload
}

// completeSoloPendingPayload 按 marker 补全整合包内容。
func (a *API) completeSoloPendingPayload(marker solo.Marker) error {
	mrpackPath := strings.TrimSpace(marker.PendingPayload)
	if mrpackPath == "" {
		return nil
	}
	if _, err := os.Stat(mrpackPath); err != nil {
		// 待装包已被清理（重复补全/用户手动删除）：视为完成
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	ctx := callCtx(a.Download.ctx)

	requirements, err := download.ReadModpackRequirements(ctx, mrpackPath)
	if err != nil {
		return fmt.Errorf("读取待装整合包失败：%w", err)
	}
	if reqErr := a.ensureSoloInstance(ctx, marker, requirements); reqErr != nil {
		return reqErr
	}
	minecraftDirectory := config.GameDirectory()
	contentDirectory := download.ResolveContentDirectoryForInstance(minecraftDirectory, "", marker.VersionID)
	if contentDirectory == "" {
		contentDirectory = minecraftDirectory
	}
	var result *download.ModpackInstallResult
	// 更新重装保留玩家数据：overrides 里的 saves/options.txt 会覆盖实例，
	// 先搬出暂存、装完还原（与 v1/v2 安装器的保留语义一致）
	stash := stashPlayerData(contentDirectory)
	runErr := download.RunContentDownload(ctx, "modpack", marker.PackName,
		func(taskCtx context.Context, report download.ProgressBytes, setDetail func(string)) error {
			var runErr error
			result, runErr = download.InstallModpack(taskCtx, effectiveCurseForgeAPIKey(), mrpackPath, contentDirectory, report, setDetail)
			return runErr
		})
	restorePlayerData(stash, contentDirectory)
	if runErr != nil {
		return fmt.Errorf("整合包内容安装失败：%w", runErr)
	}
	if result != nil && len(result.Errors) > 0 {
		// 部分依赖安装失败不能当成功收场：逐项留 WARN 日志之外，还要
		//  1) 发事件把失败项浮到界面（否则 mod 没装全只有日志有痕迹，
		//     玩家只会看到游戏莫名缺 mod）；
		//  2) 返回错误，让 marker.go 保住 PendingPayload 标记、下次启动
		//     整体重试——重跑时已就绪的文件按哈希跳过，只补缺口。
		for _, problem := range result.Errors {
			logs.Write("WARN", "NekoSolo：整合包内容安装警告："+problem)
		}
		failures := result.Errors
		if len(failures) > 3 {
			failures = failures[:3]
		}
		emit(a.Download.ctx, "solo:completionIssue", map[string]interface{}{
			"pack":     marker.PackName,
			"total":    len(result.Errors),
			"failures": failures,
		})

		return fmt.Errorf("整合包有 %d 项内容安装失败（下次启动自动重试）：%s",
			len(result.Errors), strings.Join(failures, "；"))
	}

	if javaErr := a.ensureSoloJava(ctx, minecraftDirectory, marker.VersionID); javaErr != nil {
		// Java 缺失不该让补全整体回滚：启动时还会按实例需求引导安装
		logs.Write("WARN", "NekoSolo：Java 运行时自动安装失败："+javaErr.Error())
	}
	return nil
}

// ensureSoloInstance 确保整合包的 MC 版本与 Loader 已安装（MC 本体联网下载）。
// 带加载器时总是新建独立实例（与前端整合包导入流程一致）；原版时已装则复用。
func (a *API) ensureSoloInstance(ctx context.Context, marker solo.Marker, requirements *download.ModpackRequirements) error {
	if requirements == nil {
		return fmt.Errorf("待装整合包缺少版本要求")
	}
	minecraftDirectory := config.GameDirectory()
	mcVersion := requirements.MinecraftVersion
	if mcVersion == "" {
		mcVersion = marker.MCVersion
	}
	gameVersion, err := findMinecraftVersion(ctx, mcVersion)
	if err != nil {
		return err
	}

	if requirements.RawLoaderKey != "" && !requirements.LoaderSupported() {
		return fmt.Errorf("整合包要求的加载器 %s 无法自动安装", requirements.RawLoaderKey)
	}
	if requirements.LoaderType != download.ModLoaderVanilla {
		loaderVersions, err := download.GetModLoaderVersions(ctx, requirements.LoaderType, mcVersion)
		if err != nil {
			return fmt.Errorf("查询 %s 版本失败：%w", requirements.LoaderType, err)
		}
		loader := pickModLoaderVersion(loaderVersions, requirements.LoaderVersion)
		if loader == nil {
			return fmt.Errorf("没有找到 %s 的可用版本（MC %s）", requirements.LoaderType, mcVersion)
		}
		instanceName := strings.TrimSpace(marker.VersionID)
		if instanceName == "" {
			instanceName = mcVersion
		}
		if !a.Download.service.StartModLoader(ctx, *gameVersion, *loader, instanceName, true) {
			return fmt.Errorf("游戏安装任务未能启动（可能有正在进行的下载）")
		}
		return nil
	}

	// 原版：已装复用
	installed := instance.GetInstalledVersionIds(minecraftDirectory)
	for _, id := range installed {
		if strings.EqualFold(id, mcVersion) {
			return nil
		}
	}
	if gameVersion == nil {
		return fmt.Errorf("下载源中没有找到 MC %s，无法自动安装", mcVersion)
	}
	if !a.Download.service.Start(ctx, *gameVersion) {
		return fmt.Errorf("游戏安装任务未能启动（可能有正在进行的下载）")
	}
	return nil
}

// findMinecraftVersion 在官方清单里找指定版本；找不到返回 (nil, nil)（原版路径
// 自行判断，Loader 路径直接报错）。
func findMinecraftVersion(ctx context.Context, mcVersion string) (*models.MinecraftVersion, error) {
	if strings.TrimSpace(mcVersion) == "" {
		return nil, fmt.Errorf("整合包未声明 Minecraft 版本")
	}
	versions, err := download.GetVersions(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取 Minecraft 版本清单失败：%w", err)
	}
	for _, version := range versions {
		if strings.EqualFold(version.ID, mcVersion) {
			found := version
			return &found, nil
		}
	}
	return nil, nil
}

func pickModLoaderVersion(candidates []download.ModLoaderVersion, wanted string) *download.ModLoaderVersion {
	for i := range candidates {
		if wanted != "" && candidates[i].LoaderVersion == wanted {
			return &candidates[i]
		}
	}
	if len(candidates) > 0 {
		return &candidates[0]
	}
	return nil
}

// ensureSoloJava 确保 MC 版本要求的 Java 运行时已安装（联网下载），
// 并在用户未配置首选 Java 时注册为首选。
func (a *API) ensureSoloJava(ctx context.Context, minecraftDirectory, versionID string) error {
	required, err := requiredJavaMajor(minecraftDirectory, versionID)
	if err != nil {
		return err
	}
	if required == 0 {
		return nil
	}
	for _, runtime := range download.GetInstalledRuntimes() {
		if runtime.MajorVersion != nil && *runtime.MajorVersion == required {
			return registerJavaIfUnset(runtime)
		}
	}

	candidates, err := download.QueryAvailableJavaVersions(ctx, download.JavaVendorZulu)
	if err != nil {
		return fmt.Errorf("查询 Zulu 下载候选失败：%w", err)
	}
	var chosen *download.JavaDownloadCandidate
	for i := range candidates {
		if candidates[i].MajorVersion == required {
			chosen = &candidates[i]
			break
		}
	}
	if chosen == nil {
		return fmt.Errorf("下载源中没有 Java %d 的候选", required)
	}
	var installer download.JavaRuntimeInstaller
	var installed *download.InstalledJavaRuntime
	runErr := download.RunContentDownload(ctx, "java",
		fmt.Sprintf("Zulu Java %d", required),
		func(taskCtx context.Context, report download.ProgressBytes, setDetail func(string)) error {
			var runErr error
			installed, runErr = installer.InstallCandidate(taskCtx, *chosen, func(progress download.JavaRuntimeInstallProgress) {
				report(progress.CompletedBytes, progress.TotalBytes)
				setDetail(progress.Phase)
			})
			return runErr
		})
	if runErr != nil {
		return fmt.Errorf("Java %d 安装失败：%w", required, runErr)
	}
	if installed == nil {
		return fmt.Errorf("Java %d 安装结果为空", required)
	}
	return registerJavaIfUnset(*installed)
}

// registerJavaIfUnset 用户没有配置首选 Java 时把装好的运行时注册进去
// （与 v1/v2 捆绑 Java 的注册语义一致：只在未配置时接管）。
func registerJavaIfUnset(runtime download.InstalledJavaRuntime) error {
	if config.JavaExecutable() != "" || runtime.JavaExecutablePath == "" {
		return nil
	}
	vendor := "managed"
	if runtime.Vendor != nil {
		vendor = runtime.Vendor.String()
	}
	if !config.SaveJava(runtime.JavaExecutablePath, vendor) {
		return fmt.Errorf("首选 Java 写入失败：%s", runtime.JavaExecutablePath)
	}
	return nil
}

// requiredJavaMajor 从已安装版本的描述文件里读 javaVersion.majorVersion；
// 读不到时按 MC 版本号推算（1.20.5+ → 21，1.17+ → 17，其余 → 8）。
func requiredJavaMajor(minecraftDirectory, versionID string) (int, error) {
	jsonPath := filepath.Join(minecraftDirectory, "versions", versionID, versionID+".json")
	if raw, err := os.ReadFile(jsonPath); err == nil {
		var versionJSON struct {
			JavaVersion struct {
				MajorVersion *int `json:"majorVersion"`
			} `json:"javaVersion"`
		}
		if json.Unmarshal(raw, &versionJSON) == nil && versionJSON.JavaVersion.MajorVersion != nil {
			return *versionJSON.JavaVersion.MajorVersion, nil
		}
	}
	return fallbackJavaMajor(versionID), nil
}

// fallbackJavaMajor 按 MC 版本号推算最低 Java 主版本（读不到版本 json 时的兜底）。
// 实例名可能带后缀（如 "1.20.1-forge-47.2.0"），只取前两段数字。
func fallbackJavaMajor(mcVersion string) int {
	match := mcVersionNumbers.FindStringSubmatch(strings.TrimSpace(mcVersion))
	if match == nil {
		return 8
	}
	minor, _ := strconv.Atoi(match[1])
	patch, _ := strconv.Atoi(match[2])
	if minor > 20 || (minor == 20 && patch >= 5) {
		return 21
	}
	if minor >= 17 {
		return 17
	}
	return 8
}

// mcVersionNumbers 匹配 "1.x" 或 "1.x.y" 开头的版本号。
var mcVersionNumbers = regexp.MustCompile(`^1\.(\d+)(?:\.(\d+))?`)

// playerDataStash 更新重装前搬出的玩家数据（暂存目录 + 条目相对路径）。
type playerDataStash struct {
	tempDirectory string
	entries       []string // 相对内容目录的路径（saves 或 options.txt）
}

// stashPlayerData 把内容目录里已有的玩家数据（saves/ 与 options.txt）搬到
// 临时目录，避免整合包 overrides 覆盖玩家的存档与设置。无可保留数据时返回 nil。
func stashPlayerData(contentDirectory string) *playerDataStash {
	var entries []string
	for _, name := range []string{"saves", "options.txt"} {
		if _, err := os.Stat(filepath.Join(contentDirectory, name)); err != nil {
			continue
		}
		entries = append(entries, name)
	}
	if len(entries) == 0 {
		return nil
	}
	temp, err := os.MkdirTemp("", "nekosolo-keep-")
	if err != nil {
		return nil
	}
	stash := &playerDataStash{tempDirectory: temp}
	for _, name := range entries {
		if err := os.Rename(filepath.Join(contentDirectory, name), filepath.Join(temp, name)); err == nil {
			stash.entries = append(stash.entries, name)
		}
	}
	if len(stash.entries) == 0 {
		_ = os.Remove(temp)
		return nil
	}
	return stash
}

// restorePlayerData 把暂存的玩家数据搬回内容目录（覆盖整合包带来的同名副本；
// 搬运失败不阻塞补全，玩家数据仍在暂存目录里等待下次还原前清理）。
func restorePlayerData(stash *playerDataStash, contentDirectory string) {
	if stash == nil {
		return
	}
	defer func() { _ = os.RemoveAll(stash.tempDirectory) }()
	for _, name := range stash.entries {
		source := filepath.Join(stash.tempDirectory, name)
		target := filepath.Join(contentDirectory, name)
		if err := os.RemoveAll(target); err != nil {
			logs.Write("WARN", "NekoSolo：还原玩家数据失败（原数据在 "+source+"）："+err.Error())
			continue
		}
		if err := os.Rename(source, target); err != nil {
			logs.Write("WARN", "NekoSolo：还原玩家数据失败（原数据在 "+source+"）："+err.Error())
		}
	}
}
