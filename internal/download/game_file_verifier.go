package download

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// GameFileVerifier 游戏文件完整性校验器。启动前检查关键文件是否齐全，
// 缺失时自动补全。对应 C# GameFileVerifier。
type GameFileVerifier struct {
	installer MinecraftVersionInstaller
}

// StatusFunc 校验状态回调（对应 C# IProgress<string>）。
type StatusFunc func(string)

// VerifyAndRepair 校验并补全指定版本的游戏文件。沿 inheritsFrom 链逐级检查：
// 版本 JSON、客户端 JAR、库文件。缺失时重新下载。
// hasCustomResolution 必须与启动侧一致（窗口宽高是否有效），否则
// 被 has_custom_resolution feature 规则门控的库会误报缺失/漏检。
// 返回补全的文件数；0 表示无需补全。
func (v *GameFileVerifier) VerifyAndRepair(
	ctx context.Context,
	minecraftDirectory, versionID string,
	hasCustomResolution bool,
	status StatusFunc,
) (int, error) {
	root := filepath.Clean(minecraftDirectory)
	repaired := 0
	visited := map[string]bool{}
	// 待校验队列：inheritsFrom 的父版本与 jar 字段的客户端 jar 来源都入队。
	// 后者在载荷里只带描述文件（不下发 jar 本体，见 internal/solo/export.go），
	// 必须靠本函数从官方地址补下——只走 inheritsFrom 会漏掉 jar 指向旁支的版本。
	pending := []string{versionID}

	// 沿 inheritsFrom / jar 引用逐级校验
	for len(pending) > 0 {
		currentID := pending[0]
		pending = pending[1:]
		if strings.TrimSpace(currentID) == "" || visited[strings.ToLower(currentID)] {
			continue
		}
		visited[strings.ToLower(currentID)] = true
		if err := ctx.Err(); err != nil {
			return repaired, err
		}

		versionDir := filepath.Join(root, "versions", currentID)
		jsonPath := filepath.Join(versionDir, currentID+".json")

		// 1. 版本 JSON 不存在 → 需要重新下载整个版本
		if _, err := os.Stat(jsonPath); err != nil {
			reportStatus(status, fmt.Sprintf("版本描述 %s 缺失，正在重新下载…", currentID))
			metadataURL := v.getMetadataURL(ctx, currentID)
			if strings.TrimSpace(metadataURL) != "" {
				if err := v.installer.Install(ctx, currentID, metadataURL, root, nil); err != nil {
					return repaired, err
				}
				repaired++
			}
			break
		}

		// 2. 客户端 JAR 不存在（仅对有 downloads.client 的版本检查）
		jarPath := filepath.Join(versionDir, currentID+".jar")
		hasClientDownload := false
		parentID := ""
		jarProviderID := ""

		jsonBytes, err := os.ReadFile(jsonPath)
		if err != nil {
			// 文件被占用（并发安装/杀毒软件）都视为需要重新下载
			reportStatus(status, fmt.Sprintf("版本描述 %s 读取失败，正在重新下载…", currentID))
			metadataURL := v.getMetadataURL(ctx, currentID)
			if strings.TrimSpace(metadataURL) != "" {
				if err := v.installer.Install(ctx, currentID, metadataURL, root, nil); err != nil {
					return repaired, err
				}
				repaired++
			}
			break
		}

		var rootElement map[string]json.RawMessage
		parseErr := json.Unmarshal(jsonBytes, &rootElement)
		if parseErr == nil {
			if downloads, ok := rootElement["downloads"]; ok {
				var probe struct {
					Client *json.RawMessage `json:"client"`
				}
				if json.Unmarshal(downloads, &probe) == nil && probe.Client != nil {
					hasClientDownload = true
				}
			}
			if inherits, ok := rootElement["inheritsFrom"]; ok {
				_ = json.Unmarshal(inherits, &parentID)
			}
			// jar 字段：客户端 jar 借用了另一个版本，那个版本的 jar 同样要能补下
			if jar, ok := rootElement["jar"]; ok {
				_ = json.Unmarshal(jar, &jarProviderID)
			}

			// 3. 校验库文件（并识别是否为 NeoForge/Forge Loader 实例）
			loaderType, loaderVersion, loaderMcVersion, isLoaderInstance := tryParseLoaderInfo(jsonBytes)
			if librariesRaw, ok := rootElement["libraries"]; ok {
				var libraries []libraryJSON
				if json.Unmarshal(librariesRaw, &libraries) == nil {
					missingLibs := verifyLibraries(root, libraries, hasCustomResolution)

					// NeoForge / Forge 特判：
					// a) 缺失库中含 SRG 客户端（新流程安装器生成的 JSON 会声明该库）；
					// b) 或识别为 Loader 实例但 client 库目录下没有任何 srg jar
					//    （老流程提取式安装的 JSON 不声明 srg，必须显式检查）。
					//    srg 只能由安装器生成，任何 Maven 仓库都不存在，必须重跑安装器。
					needRerun := false
					for _, lib := range missingLibs {
						if isSrgClientJar(lib) {
							needRerun = true
							break
						}
					}
					if !needRerun && isLoaderInstance &&
						(loaderType == ModLoaderNeoForge || loaderType == ModLoaderForge) {
						// NeoForge 26.x（NeoForgeV1）运行时产物是 minecraft-client-patched.jar，
						// 不需要 srg；Forge 老架构（MCP）才需要 client-srg.jar。按类型检查对应产物，
						// 缺失说明安装器未完整运行，重跑安装器。
						if loaderType == ModLoaderNeoForge {
							needRerun = !hasNeoForgePatchedClient(root, loaderVersion)
						} else {
							needRerun = !hasAnySrgClientJar(root)
						}
					}

					if needRerun {
						reportStatus(status, fmt.Sprintf(
							"版本 %s 的 Loader 安装不完整（缺少 SRG 客户端库），正在重新运行安装器修复…", currentID))
						if rerunLoaderInstaller(ctx, root, currentID, loaderType, loaderVersion, loaderMcVersion, status) {
							repaired++
							// 安装器已重建该版本目录，本链修复完成
							break
						}
						reportStatus(status, "重新运行安装器失败，回退到直接补全缺失库…")
					}

					if len(missingLibs) > 0 {
						reportStatus(status, fmt.Sprintf("版本 %s 缺少 %d 个库文件，正在补全…", currentID, len(missingLibs)))
						if err := v.installer.InstallFromMetadataBytes(ctx, currentID, root, jsonBytes, nil); err != nil {
							reportStatus(status, fmt.Sprintf("补全失败：%v", err))
						} else {
							repaired++
						}
					}
				}
			}
		} else {
			// JSON 损坏视为需要重新下载
			reportStatus(status, fmt.Sprintf("版本描述 %s 读取失败，正在重新下载…", currentID))
			metadataURL := v.getMetadataURL(ctx, currentID)
			if strings.TrimSpace(metadataURL) != "" {
				if err := v.installer.Install(ctx, currentID, metadataURL, root, nil); err != nil {
					return repaired, err
				}
				repaired++
			}
			break
		}

		// 客户端 JAR 缺失且该版本应该有
		if hasClientDownload {
			if _, err := os.Stat(jarPath); err != nil {
				reportStatus(status, fmt.Sprintf("客户端文件 %s 缺失，正在重新下载…", currentID))
				metadataURL := v.getMetadataURL(ctx, currentID)
				if strings.TrimSpace(metadataURL) != "" {
					if err := v.installer.Install(ctx, currentID, metadataURL, root, nil); err != nil {
						return repaired, err
					}
					repaired++
				}
			}
		}

		// 4. 日志配置（log4j2）缺失或哈希不符 → 补全 assets/log_configs/<id>。
		// 旧版本实例（本功能之前安装的）走的正是这条路径，不补的话控制台日志
		// 会退回 log4j 默认配置。
		if parseErr == nil {
			var metadata VersionJSON
			if json.Unmarshal(jsonBytes, &metadata) == nil && createLoggingPlan(&metadata, root) != nil {
				downloaded, err := EnsureLoggingConfig(ctx, root, &metadata, status)
				if err != nil {
					reportStatus(status, fmt.Sprintf("补全日志配置失败：%v", err))
				} else if downloaded {
					repaired++
				}
			} else if clientVersion := ClientVersionOf(jsonBytes); clientVersion != "" {
				// 已被扁平化的实例：本地 JSON 里没有 logging 段（原版 JSON 也删了），
				// 但 clientVersion 指得出对应的原版版本，用它取回声明再补文件
				downloaded, err := EnsureLoggingConfigFromMetadataURL(
					ctx, root, v.getMetadataURL(ctx, clientVersion), status)
				if err != nil {
					reportStatus(status, fmt.Sprintf("补全日志配置失败：%v", err))
				} else if downloaded {
					repaired++
				}
			}
		}

		// 父版本与 jar 来源版本都继续入队（去重由 visited 负责）
		pending = append(pending, parentID, jarProviderID)
	}

	return repaired, nil
}

// tryParseLoaderInfo 从版本 JSON 字节解析 Loader 信息（类型、Loader 版本、继承的原版版本）。
// 解析失败或信息不全时 ok 为 false。对应 C# GameFileVerifier.TryParseLoaderInfo。
func tryParseLoaderInfo(jsonBytes []byte) (loaderType ModLoaderType, loaderVersion, minecraftVersion string, ok bool) {
	loaderType = ModLoaderNeoForge
	var root map[string]json.RawMessage
	if err := json.Unmarshal(jsonBytes, &root); err != nil {
		return loaderType, "", "", false
	}

	// MC 版本：继承结构用 inheritsFrom；扁平化后的实例（无 inheritsFrom）
	// 用扁平化时写入的 clientVersion 元字段回退识别
	if inherits, ok := root["inheritsFrom"]; ok {
		_ = json.Unmarshal(inherits, &minecraftVersion)
	}
	if strings.TrimSpace(minecraftVersion) == "" {
		if clientVersion, ok := root["clientVersion"]; ok {
			_ = json.Unmarshal(clientVersion, &minecraftVersion)
		}
	}
	if strings.TrimSpace(minecraftVersion) == "" {
		return loaderType, "", "", false
	}

	var versionID, mainClass string
	if idElement, ok := root["id"]; ok {
		_ = json.Unmarshal(idElement, &versionID)
	}
	if mcElement, ok := root["mainClass"]; ok {
		_ = json.Unmarshal(mcElement, &mainClass)
	}

	// 信号 1：libraries 坐标（新流程安装器生成的 JSON 含 neoforge/forge 主库）
	if libsRaw, ok := root["libraries"]; ok {
		var libs []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(libsRaw, &libs) == nil {
			const neoPrefix = "net.neoforged:neoforge:"
			const forgePrefix = "net.minecraftforge:forge:"
			for _, lib := range libs {
				name := lib.Name
				if strings.TrimSpace(name) == "" {
					continue
				}
				if strings.HasPrefix(name, neoPrefix) {
					return ModLoaderNeoForge, strings.TrimPrefix(name, neoPrefix), minecraftVersion, true
				}
				if strings.HasPrefix(name, forgePrefix) {
					return ModLoaderForge, strings.TrimPrefix(name, forgePrefix), minecraftVersion, true
				}
			}
		}
	}

	// 信号 2：版本 id 前缀（老流程提取式安装的 JSON 无主库，id 即 "neoforge-{loader}" / "forge-{loader}"）
	if strings.TrimSpace(versionID) != "" {
		lowerID := strings.ToLower(versionID)
		const neoIDPrefix = "neoforge-"
		const forgeIDPrefix = "forge-"
		if strings.HasPrefix(lowerID, neoIDPrefix) {
			loaderVersion = versionID[len(neoIDPrefix):]
			// 实例名对齐后 id 可能形如 "neoforge-{loader}-{mc}"，去掉 mc 后缀还原纯 loader 版本
			mcSuffix := "-" + minecraftVersion
			if strings.HasSuffix(loaderVersion, mcSuffix) {
				loaderVersion = loaderVersion[:len(loaderVersion)-len(mcSuffix)]
			}
			return ModLoaderNeoForge, loaderVersion, minecraftVersion, strings.TrimSpace(loaderVersion) != ""
		}
		if strings.HasPrefix(lowerID, forgeIDPrefix) {
			loaderVersion = versionID[len(forgeIDPrefix):]
			return ModLoaderForge, loaderVersion, minecraftVersion, strings.TrimSpace(loaderVersion) != ""
		}
	}

	// 信号 3：mainClass（id 不含前缀时的类型兜底，版本仍需靠其它途径）
	if strings.TrimSpace(mainClass) != "" {
		if strings.Contains(mainClass, "net.neoforged") {
			return ModLoaderNeoForge, "", minecraftVersion, false
		}
		if strings.Contains(mainClass, "net.minecraftforge") {
			return ModLoaderForge, "", minecraftVersion, false
		}
	}

	return loaderType, "", minecraftVersion, false
}

// hasAnySrgClientJar 检查 Minecraft client 库目录下是否存在任意 SRG 重映射客户端
// （*-srg.jar）。Forge 老架构（MCP）启动必需该文件，且只能由安装器生成。
func hasAnySrgClientJar(minecraftRoot string) bool {
	clientLibDir := filepath.Join(minecraftRoot, "libraries", "net", "minecraft", "client")
	return findAnySuffixInTree(clientLibDir, "-srg.jar") != ""
}

// hasNeoForgePatchedClient 检查 NeoForge 的 patched 客户端（minecraft-client-patched）是否存在。
// NeoForge 26.x（NeoForgeV1 架构）以它为运行时的 Minecraft 类来源，缺则安装不完整。
func hasNeoForgePatchedClient(minecraftRoot, loaderVersion string) bool {
	if strings.TrimSpace(loaderVersion) == "" {
		return false
	}
	patchedJar := filepath.Join(minecraftRoot, "libraries", "net", "neoforged",
		"minecraft-client-patched", loaderVersion,
		fmt.Sprintf("minecraft-client-patched-%s.jar", loaderVersion))
	_, err := os.Stat(patchedJar)
	return err == nil
}

// rerunLoaderInstaller 重新运行 Loader 安装器（NeoForge / Forge）以修复 SRG 客户端库缺失。
// 安装器地址候选：BMCL 镜像优先，官方 Maven 兜底。
func rerunLoaderInstaller(
	ctx context.Context,
	root, versionID string,
	loaderType ModLoaderType,
	loaderVersion, minecraftVersion string,
	status StatusFunc,
) bool {
	if strings.TrimSpace(loaderVersion) == "" || strings.TrimSpace(minecraftVersion) == "" {
		return false
	}

	// 候选安装器地址：按用户选择的下载源决定 BMCL 镜像与官方 Maven 的优先顺序
	bmclFirst := SourceProvider.Active().Name != DownloadSources.Official.Name
	neoForgeOfficial := fmt.Sprintf(
		"https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar",
		loaderVersion, loaderVersion)
	neoForgeBmcl := fmt.Sprintf(
		"https://bmclapi2.bangbang93.com/neoforge/version/%s/download/installer.jar", loaderVersion)

	var candidates []string
	switch loaderType {
	case ModLoaderNeoForge:
		if bmclFirst {
			candidates = []string{neoForgeBmcl, neoForgeOfficial}
		} else {
			candidates = []string{neoForgeOfficial, neoForgeBmcl}
		}
	case ModLoaderForge:
		candidates = []string{fmt.Sprintf(
			"https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar",
			loaderVersion, loaderVersion)}
	default:
		return false
	}

	var installer ModLoaderInstaller
	for _, url := range candidates {
		loader := ModLoaderVersion{
			Type:                        loaderType,
			LoaderVersion:               loaderVersion,
			MetadataURL:                 url,
			RequiresInstallerExtraction: true,
		}
		progress := func(p MinecraftInstallProgress) {
			reportStatus(status, fmt.Sprintf("[%s] %s", p.StageName, p.Detail))
		}
		if err := installer.Install(ctx, loader, versionID, root, minecraftVersion, progress); err != nil {
			if ctx.Err() != nil {
				return false
			}
			reportStatus(status, fmt.Sprintf("安装器尝试 %s 失败：%v", url, err))
			continue
		}
		return true
	}
	return false
}

// isSrgClientJar 判断缺失库是否为 NeoForge / Forge 的 SRG 重映射客户端（只能由安装器生成）。
func isSrgClientJar(path string) bool {
	clientDir := filepath.Join("net", "minecraft", "client")
	return strings.Contains(strings.ToLower(path), strings.ToLower(clientDir)) &&
		strings.HasSuffix(strings.ToLower(path), "-srg.jar")
}

// verifyLibraries 校验库文件列表，返回缺失的库文件路径。
// 按当前系统与 feature 规则过滤（避免把其他平台的库误判为缺失），
// 优先使用 downloads.artifact.path，并检查 natives classifier（含旧版本 name 回退）。
func verifyLibraries(root string, libraries []libraryJSON, hasCustomResolution bool) []string {
	var missing []string
	librariesDir := filepath.Join(root, "libraries")
	// 与启动侧 CreateDefaultFeatures 对齐：仅 has_custom_resolution 由窗口参数决定，
	// 其余 feature 恒为 false
	features := DefaultFeatures()
	features["has_custom_resolution"] = hasCustomResolution

	for _, library := range libraries {
		// 只检查当前系统适用的库（与安装器/启动器使用同一套规则）
		if !ruleIsAllowed(library.Rules, features) {
			continue
		}

		// 1. 主构件：优先 downloads.artifact.path，回退到 name 转 Maven 路径
		if artifactPath := artifactRelativePath(library); artifactPath != "" {
			fullPath := filepath.Join(librariesDir, filepath.FromSlash(artifactPath))
			if _, err := os.Stat(fullPath); err != nil {
				missing = append(missing, fullPath)
			}
		}

		// 2. natives classifier：downloads.classifiers 优先，旧版本用 name + classifier 回退
		if nativePath := nativeRelativePath(library); nativePath != "" {
			nativeFullPath := filepath.Join(librariesDir, filepath.FromSlash(nativePath))
			if _, err := os.Stat(nativeFullPath); err != nil {
				missing = append(missing, nativeFullPath)
			}
		}
	}
	return missing
}

// artifactRelativePath 解析库的主构件相对路径；无 downloads.artifact 时按 Maven 坐标回退。
// natives-only 库（旧版本如 1.7.x 的 lwjgl-platform）没有主 jar，
// 安装侧也从不下载该文件，返回空串跳过校验，避免"修复永远失败"。
func artifactRelativePath(library libraryJSON) string {
	if library.Downloads != nil && library.Downloads.Artifact != nil &&
		strings.TrimSpace(library.Downloads.Artifact.Path) != "" {
		return library.Downloads.Artifact.Path
	}
	if len(library.Natives) > 0 {
		return ""
	}
	return CreateMavenPath(library.Name)
}

// nativeRelativePath 解析 natives classifier 的相对路径；旧版本（无 downloads）用
// name + classifier 拼路径。
func nativeRelativePath(library libraryJSON) string {
	if library.Natives == nil {
		return ""
	}
	classifierRaw, ok := library.Natives[RuleEvaluatorOSName()]
	if !ok {
		return ""
	}
	architecture := "32"
	if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
		architecture = "64"
	}
	classifier := strings.ReplaceAll(classifierRaw, "${arch}", architecture)

	if library.Downloads != nil && library.Downloads.Classifiers != nil {
		if native, ok := library.Downloads.Classifiers[classifier]; ok &&
			strings.TrimSpace(native.Path) != "" {
			return native.Path
		}
	}

	// 旧版本（1.7.x 及更早）无 downloads 字段：org.lwjgl:lwjgl-platform:2.9.4 + natives-windows
	// → org/lwjgl/lwjgl-platform/2.9.4/lwjgl-platform-2.9.4-natives-windows.jar
	return CreateNativeMavenPath(library.Name, classifier)
}

// getMetadataURL 异步获取版本的元数据 URL（用于重新下载）。
// 对于已知版本类型，返回 Mojang 官方 URL。
func (v *GameFileVerifier) getMetadataURL(ctx context.Context, versionID string) string {
	// 从 Mojang 版本清单查找
	versions, err := GetVersions(ctx)
	if err != nil {
		// 版本清单获取失败，无法自动修复
		return ""
	}
	for _, version := range versions {
		if strings.EqualFold(version.ID, versionID) && strings.TrimSpace(version.URL) != "" {
			return version.URL
		}
	}
	return ""
}

func reportStatus(status StatusFunc, message string) {
	if status != nil {
		status(message)
	}
}
