package download

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/tools"
)

// ModLoaderInstaller Mod Loader 安装器。在原版 Minecraft 已安装的基础上，
// 叠加安装指定的 Mod Loader。核心原理：Loader 的版本 JSON 通过 inheritsFrom 继承原版，
// MinecraftVersionInstaller 可直接处理此类 JSON。
type ModLoaderInstaller struct {
	baseInstaller MinecraftVersionInstaller
}

// installerTimeout 安装器最长允许运行 10 分钟。
const installerTimeout = 10 * time.Minute

// Install 安装指定 Mod Loader 到 Minecraft 目录。
//   - loader: 要安装的 Loader 版本信息（含元数据 URL）；
//   - instanceName: 实例名称，用作 versions/ 下的文件夹名，
//     如 "fabric-loader-0.16.14-1.21.8" 或用户自定义名称；
//   - minecraftVersion: 原版 Minecraft 版本号，用于确保原版已安装。
func (m *ModLoaderInstaller) Install(
	ctx context.Context,
	loader ModLoaderVersion,
	instanceName, minecraftDirectory, minecraftVersion string,
	progress InstallProgressFunc,
) error {
	if strings.TrimSpace(instanceName) == "" {
		return fmt.Errorf("instanceName 不能为空")
	}
	if strings.TrimSpace(minecraftDirectory) == "" {
		return fmt.Errorf("minecraftDirectory 不能为空")
	}
	if strings.TrimSpace(minecraftVersion) == "" {
		return fmt.Errorf("minecraftVersion 不能为空")
	}
	if strings.EqualFold(instanceName, minecraftVersion) {
		// 实例名与原版版本同名会把 Loader JSON 写进原版版本目录，
		// 覆盖原版元数据且形成自引用继承环——必须在安装前拒绝
		return fmt.Errorf("实例名称不能与 Minecraft 版本号相同（%s），请换一个名称。", minecraftVersion)
	}

	root := filepath.Clean(minecraftDirectory)

	// 1. 确保原版 Minecraft 已安装（Loader 的 inheritsFrom 需要原版文件）。
	//    若原版是本次作为依赖临时装的，Loader 装好后会被扁平化掉这个依赖。
	vanillaInstalledAsDependency, err := m.ensureVanillaInstalled(ctx, root, minecraftVersion)
	if err != nil {
		return err
	}

	if loader.RequiresInstallerExtraction {
		// NeoForge / Forge：需要从安装器 JAR 中提取版本 JSON
		if err := m.installFromInstallerJar(ctx, loader, instanceName, root, minecraftVersion, progress); err != nil {
			return err
		}
	} else {
		// Fabric：版本 JSON 可直接从 API 获取
		if err := m.baseInstaller.Install(ctx, instanceName, loader.MetadataURL, root, progress); err != nil {
			return err
		}
	}

	// 2. 扁平化：把 inheritsFrom 继承链合并为实例自包含的版本 JSON，
	//    并复制客户端 JAR。每个实例从此完全独立——不依赖原版版本目录，
	//    同一 Minecraft 主版本的多个实例也互不影响。
	//    实现经宿主钩子注入 launch.VersionJsonFlattener（host_hooks.go）。
	if err := FlattenVersionJSON(ctx, root, instanceName); err != nil {
		return err
	}

	// 3. 依赖用的原版目录：没有其它实例引用时移除，避免实例列表里
	//    出现一个"多出来的"原版条目。
	if vanillaInstalledAsDependency {
		m.tryRemoveUnreferencedDependency(ctx, root, minecraftVersion, instanceName)
	}
	return nil
}

// tryRemoveUnreferencedDependency 删除不再被任何版本引用的依赖版本目录（扁平化后调用）。
// 仍有引用（其它实例也继承它）或删除失败时不删除——失败无害，实例照常可用。
func (m *ModLoaderInstaller) tryRemoveUnreferencedDependency(
	ctx context.Context, root, dependencyVersionID, instanceName string,
) {
	if strings.TrimSpace(dependencyVersionID) == "" ||
		strings.EqualFold(dependencyVersionID, instanceName) {
		return
	}

	// 让出文件句柄窗口，避免与安装器/杀毒软件的尾部写入竞争
	select {
	case <-time.After(200 * time.Millisecond):
	case <-ctx.Done():
		return
	}

	if IsVersionReferenced(root, dependencyVersionID) {
		return
	}
	// 依赖目录删除失败不影响实例：它已不再被引用，仅是残留
	_ = os.RemoveAll(filepath.Join(root, "versions", dependencyVersionID))
}

// CreateDefaultInstanceName 生成默认的实例名称。
func CreateDefaultInstanceName(loaderType ModLoaderType, loaderVersion, minecraftVersion string) string {
	switch loaderType {
	case ModLoaderFabric:
		return fmt.Sprintf("fabric-loader-%s-%s", loaderVersion, minecraftVersion)
	case ModLoaderQuilt:
		return fmt.Sprintf("quilt-loader-%s-%s", loaderVersion, minecraftVersion)
	case ModLoaderNeoForge:
		return fmt.Sprintf("neoforge-%s-%s", loaderVersion, minecraftVersion)
	case ModLoaderForge:
		return fmt.Sprintf("forge-%s", loaderVersion)
	case ModLoaderOptifine:
		return fmt.Sprintf("OptiFine_%s", loaderVersion)
	default:
		return minecraftVersion
	}
}

// ensureVanillaInstalled 检查原版 Minecraft 是否已安装；未安装时使用 Mojang 官方源下载。
// 作为依赖安装时静默执行，不报告进度。
// 返回原版是否为本次作为依赖临时安装的（用于安装后清理依赖目录）。
func (m *ModLoaderInstaller) ensureVanillaInstalled(
	ctx context.Context, root, minecraftVersion string,
) (bool, error) {
	versionJSONPath := filepath.Join(root, "versions", minecraftVersion, minecraftVersion+".json")
	if _, err := os.Stat(versionJSONPath); err == nil {
		return false, nil
	}

	// 原版未安装，从 Mojang 版本清单获取元数据 URL
	versions, err := GetVersions(ctx)
	if err != nil {
		return false, err
	}
	var vanilla *struct {
		ID  string
		URL string
	}
	for i := range versions {
		if strings.EqualFold(versions[i].ID, minecraftVersion) {
			vanilla = &struct {
				ID  string
				URL string
			}{versions[i].ID, versions[i].URL}
			break
		}
	}
	if vanilla == nil || strings.TrimSpace(vanilla.URL) == "" {
		return false, fmt.Errorf("无法从 Mojang 版本清单中找到 Minecraft %s。", minecraftVersion)
	}

	// 作为依赖静默下载，不向用户报告进度
	if err := m.baseInstaller.Install(ctx, minecraftVersion, vanilla.URL, root, nil); err != nil {
		return false, err
	}
	return true, nil
}

// installFromInstallerJar 从安装器 JAR 安装 NeoForge / Forge / OptiFine。
//
// NeoForge / Forge：优先直接运行安装器（java -jar installer.jar --installClient <目录>；
// 旧版安装器用 --install-client，由 runInstaller 自动探测切换）：
// NeoForge / Forge 的 SRG 重映射客户端（libraries/net/minecraft/client/...-srg.jar）
// 只由安装器生成，任何 Maven 源都没有该文件；只提取 version.json 会导致
// 启动时报 "NeoForge installation is corrupted"。
//
// OptiFine：安装器是自解包程序（1.21+ 为 xdelta 补丁 + modlauncher 架构，
// 安装器 JAR 内没有可提取的版本 JSON），同样必须运行安装器
// （java -cp installer.jar optifine.Installer --installGame <目录>，
// HMCL 同款调用方式；旧版安装器用 --install）。官网下载页 adloadx 是引导页，
// 需先解析出 downloadx 直链再下载安装器 JAR。
//
// 运行安装器需要本机 Java（走 FindJavaExecutable 全链查找）；失败时直接报错——
// 提取式安装永远无法生成 SRG 客户端等核心产物，装出来的版本启动必报
// "installation corrupted"（与 C# 行为一致：不再回退提取式安装）。
func (m *ModLoaderInstaller) installFromInstallerJar(
	ctx context.Context,
	loader ModLoaderVersion,
	instanceName, root, minecraftVersion string,
	progress InstallProgressFunc,
) error {
	// 1. 下载安装器 JAR 到临时文件（OptiFine 需先从引导页解析直链）
	tempJar := filepath.Join(os.TempDir(), fmt.Sprintf("nyalauncher-installer-%d.jar", time.Now().UnixNano()))
	defer tools.RemoveFileIfExists(tempJar)

	twoMinutes := 2 * time.Minute
	var jarBytes []byte
	var err error
	if loader.Type == ModLoaderOptifine {
		jarBytes, err = downloadOptifineInstaller(ctx, loader.MetadataURL)
	} else {
		jarBytes, err = SourceProvider.GetBytes(ctx, loader.MetadataURL, &twoMinutes)
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(tempJar, jarBytes, 0o644); err != nil {
		return err
	}

	// 快照 versions 目录已有目录（安装器运行前的基线）。
	// 安装器成功运行后，只处理基线之外新增的目录，绝不触碰任何已有实例目录。
	versionsDir := filepath.Join(root, "versions")
	preExistingDirs := map[string]bool{}
	if entries, err := os.ReadDir(versionsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				preExistingDirs[strings.ToLower(entry.Name())] = true
			}
		}
	}

	// 2. 优先运行安装器：生成 srg 客户端等核心产物
	installerError, runErr := m.tryRunInstaller(ctx, tempJar, root, progress, loader.Type)
	if runErr == nil {
		if loader.Type == ModLoaderOptifine {
			// OptiFine 安装器产物：versions/OptiFine_<mc>_<type>/ 下的版本 JSON
			if !hasOptifineVersionArtifact(root, preExistingDirs) {
				return fmt.Errorf(
					"OptiFine 安装器运行结束，但未生成版本目录（versions/OptiFine_*）。"+
						"请重试安装，或检查安装器输出确认 Java 版本与网络。%s",
					installerError)
			}
		} else if !hasRuntimeClientArtifact(root, loader.Type, loader.LoaderVersion) {
			// 校验安装器确实生成了运行时客户端产物；缺则说明安装不完整。
			// NeoForge 26.x（NeoForgeV1）产出 minecraft-client-patched.jar；
			// Forge 老架构（MCP）产出 client-*-srg.jar。
			return fmt.Errorf(
				"%s 安装器运行结束，但未生成必需的运行时客户端产物"+
					"（NeoForge: libraries/net/neoforged/minecraft-client-patched/*.jar；"+
					"Forge: libraries/net/minecraft/client/*-srg.jar）。"+
					"请重试安装，或检查安装器输出确认 Java 版本与网络。%s",
				loader.DisplayName(), installerError)
		}

		// 安装器成功后会生成 versions/{id}/ 与所需 libraries；
		// 将安装器默认版本名对齐到用户指定的实例名（仅处理新增目录，绝不碰已有实例）
		alignInstanceDirectory(root, minecraftVersion, instanceName, preExistingDirs)
		return nil
	}

	// 3. 安装器运行失败：提取式安装永远无法生成 SRG 客户端等核心产物，
	//    装出来的版本启动必报 "installation corrupted"，因此直接报错并带上真实原因。
	detail := installerError
	if detail == "" {
		detail = runErr.Error()
	}
	if loader.Type == ModLoaderOptifine {
		return fmt.Errorf(
			"OptiFine 安装器运行失败：%s。OptiFine 必须由安装器完成安装（需要 Java），请检查 Java 与网络后重试。",
			detail)
	}
	return fmt.Errorf(
		"Loader 安装器运行失败：%s。NeoForge/Forge 必须由安装器完成安装（需要生成 SRG 客户端库），请检查 Java 与网络后重试。",
		detail)
}

// downloadOptifineInstaller 下载 OptiFine 安装器 JAR。
// adLoadURL 是官网引导页（adloadx?f=<file>），页面内含 downloadx 直链（带一次性 token）。
func downloadOptifineInstaller(ctx context.Context, adLoadURL string) ([]byte, error) {
	twoMinutes := 2 * time.Minute
	pageHTML, err := SourceProvider.GetString(ctx, adLoadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("打开 OptiFine 下载页失败：%w", err)
	}
	link := optiFineDownloadLinkRegex.FindString(pageHTML)
	if link == "" {
		return nil, fmt.Errorf("无法从 OptiFine 下载页解析出安装器直链（页面结构可能已变化）")
	}
	link = strings.ReplaceAll(link, "&amp;", "&")
	jarBytes, err := SourceProvider.GetBytes(ctx, "https://optifine.net/"+link, &twoMinutes)
	if err != nil {
		return nil, fmt.Errorf("下载 OptiFine 安装器失败：%w", err)
	}
	return jarBytes, nil
}

// hasOptifineVersionArtifact 检查安装器是否生成了 OptiFine 版本目录
// （versions/OptiFine_<mc>_<type>/ 下存在版本 JSON；只认安装器本次新增的目录）。
func hasOptifineVersionArtifact(minecraftRoot string, preExistingDirs map[string]bool) bool {
	entries, err := os.ReadDir(filepath.Join(minecraftRoot, "versions"))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		lower := strings.ToLower(entry.Name())
		if !strings.Contains(lower, "optifine") || preExistingDirs[lower] {
			continue
		}
		dirEntries, err := os.ReadDir(filepath.Join(minecraftRoot, "versions", entry.Name()))
		if err != nil {
			continue
		}
		for _, file := range dirEntries {
			if !file.IsDir() && strings.HasSuffix(strings.ToLower(file.Name()), ".json") {
				return true
			}
		}
	}
	return false
}

// hasRuntimeClientArtifact 检查安装器是否生成了该 Loader 架构对应的运行时客户端产物：
// NeoForge 26.x（NeoForgeV1）→ minecraft-client-patched-{version}.jar；
// Forge 老架构（MCP）→ libraries/net/minecraft/client/*-srg.jar。
func hasRuntimeClientArtifact(minecraftRoot string, loaderType ModLoaderType, loaderVersion string) bool {
	if loaderType == ModLoaderNeoForge {
		if strings.TrimSpace(loaderVersion) == "" {
			return false
		}
		patchedJar := filepath.Join(minecraftRoot, "libraries", "net", "neoforged",
			"minecraft-client-patched", loaderVersion,
			fmt.Sprintf("minecraft-client-patched-%s.jar", loaderVersion))
		_, err := os.Stat(patchedJar)
		return err == nil
	}
	// Forge（MCP 架构）需要 SRG 重映射客户端
	return hasSrgClientJar(minecraftRoot)
}

// hasSrgClientJar 检查 SRG 客户端库（*-srg.jar）是否已生成到 libraries/net/minecraft/client。
func hasSrgClientJar(minecraftRoot string) bool {
	clientLibraries := filepath.Join(minecraftRoot, "libraries", "net", "minecraft", "client")
	return findAnySuffixInTree(clientLibraries, "-srg.jar") != ""
}

// findAnySuffixInTree 查找文件名以 suffix 结尾的任意文件。
func findAnySuffixInTree(root, suffix string) string {
	if _, err := os.Stat(root); err != nil {
		return ""
	}
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if found != "" {
			return filepath.SkipAll
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), suffix) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// alignInstanceDirectory 把安装器生成的版本目录对齐到用户指定的实例名：
// 安装器固定使用 version.json 的 id（如 "neoforge-26.2.0.66"）作为目录名，
// 与启动器的自定义实例名（如 "neoforge-26.2.0.66-26.2"）不一致。
// 这里重命名目录并同步更新版本 JSON 的 id 字段。
func alignInstanceDirectory(root, minecraftVersion, instanceName string, preExistingDirs map[string]bool) {
	if strings.TrimSpace(instanceName) == "" {
		return
	}

	versionsDir := filepath.Join(root, "versions")
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.TrimSpace(name) == "" {
			continue
		}
		if strings.EqualFold(name, minecraftVersion) {
			continue // 原版目录，跳过
		}
		if strings.EqualFold(name, instanceName) {
			continue // 实例目录本身（重装场景），跳过，不打断遍历
		}
		// 关键防线：只处理安装器本次新增的目录，绝不触碰任何安装器运行前已存在的目录
		// （已有实例目录可能含用户 mods/saves/config 等数据）。
		if preExistingDirs[strings.ToLower(name)] {
			continue
		}
		lower := strings.ToLower(name)
		if !strings.Contains(lower, "neoforge") && !strings.Contains(lower, "forge") &&
			!strings.Contains(lower, "optifine") {
			continue
		}

		directory := filepath.Join(versionsDir, name)
		target := filepath.Join(versionsDir, instanceName)
		if _, err := os.Stat(target); err == nil {
			// 重装场景：实例目录已存在（内含用户 mods/saves/config，绝不能移动或删除），
			// 只把安装器新生成的版本 JSON（及可能的新 client jar）合并进现有实例目录。
			tryMergeInstallerJSON(target, directory, instanceName)
			return
		}

		if err := os.Rename(directory, target); err != nil {
			// 改名失败则保留安装器默认目录名（实例仍可识别）
			continue
		}
		// 版本 JSON 的 id 字段同步更新，并重命名 json 文件
		oldJSON := filepath.Join(target, name+".json")
		if _, err := os.Stat(oldJSON); err == nil {
			updateJSONID(oldJSON, filepath.Join(target, instanceName+".json"), instanceName)
		}
		return
	}
}

// tryMergeInstallerJSON 重装场景：把安装器新生成目录中的版本 JSON 合并进已存在的实例目录，
// 不移动用户数据；随后清理安装器生成的空壳目录。
func tryMergeInstallerJSON(targetDir, sourceDir, instanceName string) {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return
	}
	var sourceJSON string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			sourceJSON = filepath.Join(sourceDir, entry.Name())
			break
		}
	}
	if sourceJSON == "" {
		return
	}

	// 新 JSON 写入实例目录并更新 id
	targetJSON := filepath.Join(targetDir, instanceName+".json")
	updateJSONID(sourceJSON, targetJSON, instanceName)

	// 新 JSON 可能引用了版本目录内同名 client jar，一并补齐
	baseName := strings.TrimSuffix(filepath.Base(sourceJSON), filepath.Ext(sourceJSON))
	sourceJar := filepath.Join(sourceDir, baseName+".jar")
	targetJar := filepath.Join(targetDir, instanceName+".jar")
	if _, err := os.Stat(sourceJar); err == nil {
		if _, err := os.Stat(targetJar); err != nil {
			if data, err := os.ReadFile(sourceJar); err == nil {
				_ = os.WriteFile(targetJar, data, 0o644)
			}
		}
	}

	// 清理安装器生成的目录（仅删除其中的 json/jar 产物，不动其它内容；失败无害）
	entries, err = os.ReadDir(sourceDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			lower := strings.ToLower(entry.Name())
			if strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".jar") {
				tools.RemoveFileIfExists(filepath.Join(sourceDir, entry.Name()))
			}
		}
		if remaining, err := os.ReadDir(sourceDir); err == nil && len(remaining) == 0 {
			_ = os.Remove(sourceDir) // 清理失败则保留，无害
		}
	}
}

// ensureLauncherProfiles NeoForge / Forge 安装器强制要求 .minecraft 下存在
// launcher_profiles.json（官方启动器才会生成该文件）；启动器不写此文件时安装器会直接拒绝：
// "There is no minecraft launcher profile ... you need to run the launcher first!"
// 这里补一个最小合法模板。
func ensureLauncherProfiles(minecraftRoot string) {
	profilePath := filepath.Join(minecraftRoot, "launcher_profiles.json")
	if _, err := os.Stat(profilePath); err == nil {
		return
	}
	const template = `{
  "profiles": {},
  "selectedProfile": "(Default)",
  "clientToken": "nyalauncher",
  "authenticationDatabase": {}
}
`
	// 写失败不阻塞安装流程（部分安装器版本可能不强制检查）
	_ = os.WriteFile(profilePath, []byte(template), 0o644)
}

// updateJSONID 读取版本 JSON，更新 id 字段并写入新文件。
func updateJSONID(sourceJSON, targetJSON, newID string) {
	data, err := os.ReadFile(sourceJSON)
	if err != nil {
		return
	}
	var node map[string]json.RawMessage
	if err := json.Unmarshal(data, &node); err != nil {
		return // id 更新失败：保留原 json，目录名仍可用于识别
	}
	node["id"], _ = json.Marshal(newID)
	merged, err := json.Marshal(node)
	if err != nil {
		return
	}
	if err := os.WriteFile(targetJSON, merged, 0o644); err != nil {
		return
	}
	if targetJSON != sourceJSON {
		tools.RemoveFileIfExists(sourceJSON) // 旧 json 文件名清理失败可忽略
	}
}

// tryRunInstaller 运行 Loader 安装器。
// NeoForge / Forge：java -jar installer.jar --installClient <目录>（旧版 --install-client）。
// OptiFine：java -cp installer.jar optifine.Installer --installGame <目录>（旧版 --install）。
// 成功返回 ("", nil)；java 缺失或安装器失败返回 (原因, err)。
func (m *ModLoaderInstaller) tryRunInstaller(
	ctx context.Context,
	installerJarPath, minecraftRoot string,
	progress InstallProgressFunc,
	loaderType ModLoaderType,
) (string, error) {
	// 查找本机可用的 Java：
	// 1) 启动器托管的运行时目录（<mcDir>/runtime，递归扫描已下载的 JRE）
	// 2) 无托管时回退全链查找（JAVA_HOME / PATH 等）
	javaExecutable := FindJavaExecutable(GetRuntimeDirectory())
	if javaExecutable == "" {
		javaExecutable = FindJavaExecutable("")
	}
	if javaExecutable == "" {
		return "未找到可用的 Java 运行时，无法运行安装器。", fmt.Errorf("java not found")
	}

	ensureLauncherProfiles(minecraftRoot)

	if progress != nil {
		progress(MinecraftInstallProgress{
			StageIndex: 1,
			StageName:  "运行 Loader 安装器",
			Detail:     "正在运行安装器（首次需要下载依赖，请耐心等待）…",
		})
	}

	if loaderType == ModLoaderOptifine {
		// OptiFine 安装器不用 joptsimple，参数不被识别时输出格式不同，
		// 这里不做 unrecognized 门控，两种参数形态顺序各试一次
		var lastError string
		for _, installArg := range []string{"--installGame", "--install"} {
			args := []string{"-cp", installerJarPath, "optifine.Installer", installArg, minecraftRoot}
			onceError, _, runErr := runInstallerOnce(ctx, javaExecutable, args)
			if runErr == nil {
				return "", nil
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			lastError = onceError
		}
		if lastError == "" {
			lastError = "安装器运行失败。"
		}
		return lastError, fmt.Errorf("installer failed")
	}

	// 现代 NeoForge/Forge 安装器（joptsimple）改用 camelCase 的 --installClient；
	// 旧版安装器用 --install-client。先试新语法，遇到 "is not a recognized option"
	// 自动换组合重试（joptsimple 解析失败在下载前发生，重试代价低）。
	// 同时对 --mirror 做开关降级：个别新安装器移除了 --mirror 选项。
	installArgForms := []string{"--installClient", "--install-client"}
	var lastError string
	for _, installArg := range installArgForms {
		for _, useMirror := range []bool{true, false} {
			args := []string{"-jar", installerJarPath, installArg, minecraftRoot}
			if useMirror {
				if mavenBaseURL := SourceProvider.Active().Maven; mavenBaseURL != "" {
					addMirrorArgument(&args, mavenBaseURL)
				}
				if fb := SourceProvider.Fallback(); fb != nil &&
					!strings.EqualFold(fb.Maven, SourceProvider.Active().Maven) {
					addMirrorArgument(&args, fb.Maven)
				}
			}
			onceError, unrecognized, runErr := runInstallerOnce(ctx, javaExecutable, args)
			if runErr == nil {
				return "", nil
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			lastError = onceError
			// 仅当是 joptsimple 不可识别选项时才换组合；
			// 其它失败（下载失败 / 退出码非 0 / 超时）直接结束，避免无谓重跑。
			if !unrecognized {
				return lastError, runErr
			}
		}
	}
	if lastError == "" {
		lastError = "安装器运行失败。"
	}
	return lastError, fmt.Errorf("installer failed")
}

// runInstallerOnce 按给定参数单次运行安装器；
// unrecognized 标记是否因 joptsimple 不可识别选项而失败（仅 NeoForge/Forge 使用）。
func runInstallerOnce(
	ctx context.Context,
	javaExecutable string,
	args []string,
) (errMessage string, unrecognized bool, err error) {
	command := exec.Command(javaExecutable, args...)
	// java.exe 是控制台程序，安装期间禁止其弹出控制台窗口
	tools.HideProcessWindow(command)
	command.Cancel = func() error {
		// 尽力终止进程树；Windows 上 Go 1.20+ 的 Cancel 只杀主进程，
		// 安装器子进程残留由 WaitDelay + 超时兜底（见 PORTING_NOTES.md）。
		return command.Process.Kill()
	}
	command.WaitDelay = 5 * time.Second

	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Sprintf("无法启动安装器进程：%v", err), false, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return fmt.Sprintf("无法启动安装器进程：%v", err), false, err
	}

	if err := command.Start(); err != nil {
		return fmt.Sprintf("无法启动安装器进程：%v", err), false, err
	}

	// 异步读取输出，避免管道阻塞；仅保留尾部用于报错
	var outputTail []string
	var tailMu sync.Mutex
	appendTail := func(line string) {
		tailMu.Lock()
		defer tailMu.Unlock()
		const maxLines = 12
		outputTail = append(outputTail, line)
		if len(outputTail) > maxLines {
			outputTail = outputTail[len(outputTail)-maxLines:]
		}
	}
	drainDone := make(chan struct{}, 2)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				appendTail(line)
			}
		}
		drainDone <- struct{}{}
	}()
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				appendTail(line)
			}
		}
		drainDone <- struct{}{}
	}()

	// 轮询等待 + 即时取消响应：安装器最长允许运行 10 分钟，
	// 期间用户取消会立刻杀掉进程树，而不是等满整个超时窗口。
	started := time.Now()
	waitCh := make(chan error, 1)
	go func() { waitCh <- command.Wait() }()
	// 用一个 ticker 而不是循环里的 time.After：最长 10 分钟、500ms 一跳，
	// 反复 time.After 会白白新建约 1200 个定时器
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case waitErr := <-waitCh:
			<-drainDone
			<-drainDone
			if ctx.Err() != nil {
				return "", false, ctx.Err()
			}
			if exitErr, ok := waitErr.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
				tailMu.Lock()
				tail := strings.TrimSpace(strings.Join(outputTail, "\n"))
				tailMu.Unlock()
				message := fmt.Sprintf("安装器退出码 %d：%s", exitErr.ExitCode(), tail)
				// joptsimple 不可识别选项的特征串（如 "install-client is not a recognized option"）
				unrecognized = strings.Contains(strings.ToLower(tail), "is not a recognized option")
				return message, unrecognized, fmt.Errorf("exit code %d", exitErr.ExitCode())
			}
			if waitErr != nil {
				return fmt.Sprintf("运行安装器失败：%v", waitErr), false, waitErr
			}
			return "", false, nil
		case <-ctx.Done():
			_ = command.Process.Kill()
			<-waitCh
			return "", false, ctx.Err()
		case <-ticker.C:
			if time.Since(started) >= installerTimeout {
				_ = command.Process.Kill()
				<-waitCh
				return "安装器运行超时（10 分钟）。", false, fmt.Errorf("installer timeout")
			}
		}
	}
}

// addMirrorArgument 为安装器追加 --mirror 参数（跳过官方默认 maven，避免重复）。
func addMirrorArgument(args *[]string, mavenBaseURL string) {
	if strings.TrimSpace(mavenBaseURL) == "" {
		return
	}
	// 官方源无需镜像
	if strings.EqualFold(mavenBaseURL, DownloadSources.Official.Maven) {
		return
	}
	if strings.EqualFold(mavenBaseURL, "https://maven.neoforged.net/releases/") {
		return
	}
	*args = append(*args, "--mirror", strings.TrimRight(mavenBaseURL, "/")+"/")
}
