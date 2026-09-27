package launch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"nekolauncher/internal/tools"
)

// MinecraftVersionProfile 合并继承链后的最终启动档案
// （对应 C# Internal/MinecraftVersionProfile）。
type MinecraftVersionProfile struct {
	// Id 实例目录名（启动请求的版本 id）。
	Id string
	// SourceId version.json 中声明的原始 id（如 NeoForge 的 "neoforge-21.8.54"）。
	// 与实例目录名不同；部分启动参数（如 -DignoreList=${version_name}.jar）
	// 依赖原始 id 才能正确匹配。
	SourceId string
	// MainClass 游戏主类。
	MainClass string
	// ClientJarVersionId 提供客户端 JAR 的版本 id（显式 jar 字段优先于含
	// downloads.client 的链节点）。
	ClientJarVersionId string
	// AssetsId 资源索引 id。
	AssetsId string
	// LoggingArgument 版本 JSON 的 logging.client.argument 模板
	// （原版为 -Dlog4j.configurationFile=${path}）；空表示该版本没有 logging 段。
	LoggingArgument string
	// LoggingFileId logging.client.file.id（log4j 配置文件名，如 client-1.12.xml）。
	// 落盘位置固定为 assets/log_configs/<id>，与官方启动器一致。
	LoggingFileId string
	// VersionType 版本类型（release / snapshot / …）。
	VersionType string
	// RequiredJavaMajorVersion 版本要求的最低 Java 主版本；nil 表示未声明。
	RequiredJavaMajorVersion *int
	// LegacyGameArguments 旧版 minecraftArguments（新版格式之前）。
	LegacyGameArguments string
	// JvmArguments 新版 arguments.jvm 原始元素（字符串或带 rules 的对象）。
	JvmArguments []json.RawMessage
	// GameArguments 新版 arguments.game 原始元素。
	GameArguments []json.RawMessage
	// Libraries 依赖库原始条目（去重后，保持首次出现顺序）。
	Libraries []json.RawMessage
}

// maximumInheritanceDepth 继承链深度上限（防环）。
const maximumInheritanceDepth = 16

// versionChainEntry 继承链节点：版本 id + 原始 JSON 对象。
type versionChainEntry struct {
	id   string
	root map[string]json.RawMessage
}

// MinecraftVersionProfileLoader 加载并合并 Minecraft 版本 Profile 继承链：
// 沿 inheritsFrom 向上追溯（深度上限防环），把参数/库/资源等声明合并为最终启动档案。
type MinecraftVersionProfileLoader struct{}

// Load 加载版本 JSON 并合并继承链。
func (MinecraftVersionProfileLoader) Load(
	ctx context.Context,
	minecraftDirectory, versionId string,
) (*MinecraftVersionProfile, error) {
	if err := validateVersionId(versionId); err != nil {
		return nil, err
	}

	var chain []versionChainEntry
	visited := map[string]bool{}
	currentId := versionId

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if visited[strings.ToLower(currentId)] {
			return nil, newLaunchError("版本继承出现循环：" + currentId)
		}
		visited[strings.ToLower(currentId)] = true

		if len(chain) >= maximumInheritanceDepth {
			return nil, newLaunchError("版本继承层级过深，已停止解析。")
		}

		jsonPath := versionJsonPath(minecraftDirectory, currentId)
		if !tools.FileExists(jsonPath) {
			return nil, newLaunchError("找不到版本配置：" + jsonPath)
		}

		data, err := os.ReadFile(jsonPath)
		if err != nil {
			return nil, newLaunchErrorWrap("读取版本配置失败："+jsonPath, err)
		}
		var root map[string]json.RawMessage
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, newLaunchErrorWrap("版本配置不是有效 JSON："+jsonPath, err)
		}

		chain = append(chain, versionChainEntry{id: currentId, root: root})
		parentId, hasParent := tryGetString(root, "inheritsFrom")
		if !hasParent || strings.TrimSpace(parentId) == "" {
			break
		}
		if err := validateVersionId(parentId); err != nil {
			return nil, err
		}
		currentId = parentId
	}

	// 自最顶层依赖（原版）向实例方向合并
	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}
	return mergeProfiles(versionId, chain)
}

// mergeProfiles 沿继承链合并为最终档案（子级覆盖父级，库按坐标键去重保持首现顺序）。
func mergeProfiles(requestedVersionId string, chain []versionChainEntry) (*MinecraftVersionProfile, error) {
	var mainClass, clientJarVersionId, assetsId, versionType string
	var legacyArguments, sourceId string
	var loggingArgument, loggingFileId string
	var javaMajorVersion *int
	var jvmArguments, gameArguments, libraries []json.RawMessage
	libraryIndexes := map[string]int{}

	for _, entry := range chain {
		root := entry.root
		if value, ok := tryGetString(root, "mainClass"); ok {
			mainClass = value
		}
		if value, ok := tryGetString(root, "assets"); ok {
			assetsId = value
		}
		if value, ok := tryGetString(root, "type"); ok {
			versionType = value
		}
		if value, ok := tryGetString(root, "minecraftArguments"); ok {
			legacyArguments = value
		}
		// 记录 version.json 声明的原始 id（链末的 Loader 版本优先）
		if declaredId, ok := tryGetString(root, "id"); ok && strings.TrimSpace(declaredId) != "" {
			sourceId = declaredId
		}

		if assetIndexRaw, ok := root["assetIndex"]; ok {
			var assetIndex map[string]json.RawMessage
			if json.Unmarshal(assetIndexRaw, &assetIndex) == nil {
				if assetIndexId, ok := tryGetString(assetIndex, "id"); ok {
					assetsId = assetIndexId
				}
			}
		}

		if javaVersionRaw, ok := root["javaVersion"]; ok {
			var javaVersion struct {
				MajorVersion *int `json:"majorVersion"`
			}
			if json.Unmarshal(javaVersionRaw, &javaVersion) == nil && javaVersion.MajorVersion != nil {
				parsed := *javaVersion.MajorVersion
				javaMajorVersion = &parsed
			}
		}

		// logging.client：log4j2 配置（argument 模板 + 文件名），子级覆盖父级
		if loggingRaw, ok := root["logging"]; ok {
			var logging struct {
				Client *struct {
					Argument string `json:"argument"`
					File     *struct {
						ID string `json:"id"`
					} `json:"file"`
				} `json:"client"`
			}
			if json.Unmarshal(loggingRaw, &logging) == nil && logging.Client != nil {
				if value := strings.TrimSpace(logging.Client.Argument); value != "" {
					loggingArgument = value
				}
				if logging.Client.File != nil && strings.TrimSpace(logging.Client.File.ID) != "" {
					loggingFileId = strings.TrimSpace(logging.Client.File.ID)
				}
			}
		}

		if downloadsRaw, ok := root["downloads"]; ok {
			var downloads map[string]json.RawMessage
			if json.Unmarshal(downloadsRaw, &downloads) == nil {
				if _, ok := downloads["client"]; ok {
					clientJarVersionId = entry.id
				}
			}
		}

		if explicitJarVersion, ok := tryGetString(root, "jar"); ok {
			clientJarVersionId = explicitJarVersion
		}

		if argumentsRaw, ok := root["arguments"]; ok {
			var arguments map[string]json.RawMessage
			if json.Unmarshal(argumentsRaw, &arguments) == nil {
				jvmArguments = appendJsonArray(jvmArguments, arguments["jvm"])
				gameArguments = appendJsonArray(gameArguments, arguments["game"])
			}
		}

		librariesRaw, ok := root["libraries"]
		if !ok {
			continue
		}
		var chainLibraries []json.RawMessage
		if json.Unmarshal(librariesRaw, &chainLibraries) != nil {
			continue
		}
		for _, library := range chainLibraries {
			key := libraryCoordinateKey(library)
			if existingIndex, ok := libraryIndexes[key]; ok {
				libraries[existingIndex] = library
			} else {
				libraryIndexes[key] = len(libraries)
				libraries = append(libraries, library)
			}
		}
	}

	if strings.TrimSpace(mainClass) == "" {
		return nil, newLaunchError("版本配置缺少 mainClass。")
	}
	if strings.TrimSpace(clientJarVersionId) == "" {
		return nil, newLaunchError("版本配置未指定可用的客户端 JAR。")
	}

	profile := &MinecraftVersionProfile{
		Id:                       requestedVersionId,
		MainClass:                mainClass,
		ClientJarVersionId:       clientJarVersionId,
		AssetsId:                 assetsId,
		VersionType:              versionType,
		RequiredJavaMajorVersion: javaMajorVersion,
		LegacyGameArguments:      legacyArguments,
		JvmArguments:             jvmArguments,
		GameArguments:            gameArguments,
		Libraries:                libraries,
		LoggingArgument:          loggingArgument,
		LoggingFileId:            loggingFileId,
	}
	// loader JSON 未声明 id 时回退到请求的实例名，避免沿用父（原版）版本的 id
	if strings.TrimSpace(sourceId) == "" {
		profile.SourceId = requestedVersionId
	} else {
		profile.SourceId = sourceId
	}
	if profile.AssetsId == "" {
		profile.AssetsId = "legacy"
	}
	if profile.VersionType == "" {
		profile.VersionType = "release"
	}
	return profile, nil
}

// appendJsonArray 把原始 JSON 数组元素追加到目标。
func appendJsonArray(target []json.RawMessage, raw json.RawMessage) []json.RawMessage {
	if len(raw) == 0 {
		return target
	}
	var elements []json.RawMessage
	if json.Unmarshal(raw, &elements) != nil {
		return target
	}
	return append(target, elements...)
}

// libraryCoordinateKey 库坐标键：group:artifact:classifier@ext。
// 继承版本可覆盖同一 group/artifact/classifier 的旧版本，
// 但普通 JAR、natives-* 和 unsafe 等 classifier 必须同时保留。
func libraryCoordinateKey(library json.RawMessage) string {
	var descriptor struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(library, &descriptor) != nil || strings.TrimSpace(descriptor.Name) == "" {
		return randomHexIdentifier()
	}

	name := descriptor.Name
	extension := ""
	coordinate := name
	if index := strings.Index(name, "@"); index >= 0 {
		extension = name[index:]
		coordinate = name[:index]
	}
	parts := strings.Split(coordinate, ":")
	if len(parts) < 3 {
		return name
	}
	classifier := ""
	if len(parts) >= 4 {
		classifier = parts[3]
	}
	return parts[0] + ":" + parts[1] + ":" + classifier + extension
}

func versionJsonPath(minecraftDirectory, versionId string) string {
	return filepath.Join(minecraftDirectory, "versions", versionId, versionId+".json")
}

// validateVersionId 版本 ID 不能为空、不能包含路径分隔符或非法文件名字符。
func validateVersionId(versionId string) error {
	if strings.TrimSpace(versionId) == "" ||
		strings.ContainsAny(versionId, `/\`) ||
		versionId == "." || versionId == ".." ||
		strings.ContainsAny(versionId, `<>:"|?*`) {
		return newLaunchError("无效的版本 ID：" + versionId)
	}
	return nil
}

// tryGetString 从原始 JSON 对象读取字符串字段。
func tryGetString(object map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := object[key]
	if !ok {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func randomHexIdentifier() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(buffer)
}
