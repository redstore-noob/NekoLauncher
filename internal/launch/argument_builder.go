package launch

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"nekolauncher/internal/auth"
	"nekolauncher/internal/tools"
)

// placeholderPattern 版本参数占位符：${name}。
var placeholderPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// nullablePlaceholders 允许解析为空串的占位符。
//
// 它们表示"当前账号不具备该字段"：离线账号与皮肤站账号没有 clientId / xuid，
// 代码里就是显式填空串。而 1.18+ 的版本档案把这两个参数写成 "--clientId" 与
// "${clientid}" 两个**独立元素**（不是同一个 value 数组），于是空值会留下一个空参数——
// 既可能被游戏当成位置参数，也会被最终命令的参数校验直接拒绝启动
// （离线账号在 1.18+ 上就是这样被挡下的）。
//
// 所以这类占位符解析为空时，连同前面的标志一起省略，保证"标志 + 值"成对出现或成对消失。
// user_type 一并列入：游戏对它的默认行为与空串一致，省略不改变语义。
var nullablePlaceholders = map[string]bool{
	"clientid":  true,
	"auth_xuid": true,
	"user_type": true,
}

// isNullablePlaceholder 判断元素是否恰好就是一个可空占位符（整段即占位符本身）。
func isNullablePlaceholder(source string) bool {
	matches := placeholderPattern.FindAllStringSubmatch(source, -1)
	if len(matches) != 1 || strings.TrimSpace(source) != matches[0][0] {
		return false
	}
	return nullablePlaceholders[matches[0][1]]
}

// isFlagArgument 形如 "--clientId" 的标志参数（单个 "-" 不算）。
func isFlagArgument(argument string) bool {
	return len(argument) > 1 && argument[0] == '-'
}

// appendResolvedArgument 追加一个已解析的参数；可空占位符解析为空时，连同刚追加的标志
// 一起省略，避免留下一个悬空的空参数，或一个失去取值的标志。
func appendResolvedArgument(target *[]string, source, resolved string) {
	if resolved != "" || !isNullablePlaceholder(source) {
		*target = append(*target, resolved)
		return
	}
	if count := len(*target); count > 0 && isFlagArgument((*target)[count-1]) {
		*target = (*target)[:count-1]
	}
}

// MinecraftArgumentBuilder 启动参数装配器（对应 C# Internal/MinecraftArgumentBuilder）。
type MinecraftArgumentBuilder struct{}

// Build 生成最终 Java 命令行参数。
func (MinecraftArgumentBuilder) Build(
	profile *MinecraftVersionProfile,
	options MinecraftLaunchOptions,
	nativeDirectory string,
	classpath []string,
	mainClass string,
	prependJvmArguments, appendJvmArguments []string,
	prependGameArguments, appendGameArguments []string,
) ([]string, error) {
	if err := validateMemory(options); err != nil {
		return nil, err
	}

	minecraftDirectory, err := filepath.Abs(options.MinecraftDirectory)
	if err != nil {
		return nil, err
	}
	gameDirectory := options.GameDirectory
	if strings.TrimSpace(gameDirectory) == "" {
		gameDirectory = minecraftDirectory
	}
	if gameDirectory, err = filepath.Abs(gameDirectory); err != nil {
		return nil, err
	}
	assetsDirectory := filepath.Join(minecraftDirectory, "assets")
	librariesDirectory := filepath.Join(minecraftDirectory, "libraries")
	gameAssetsDirectory := getLegacyGameAssetsDirectory(assetsDirectory, profile.AssetsId)
	// 日志配置（log4j2）路径：版本 JSON 的 logging.client.file.id 落在
	// assets/log_configs/<id>（与官方启动器一致），用于解析 ${path} 占位符
	logConfigPath := ""
	if id := strings.TrimSpace(profile.LoggingFileId); id != "" {
		logConfigPath = filepath.Join(assetsDirectory, "log_configs", id)
	}
	classpathValue := strings.Join(classpath, string(filepath.ListSeparator))
	var authPlayerName, authUuid, authAccessToken, authSession, clientId, authXuid, userType string
	switch kind := options.Account.AccountKind(); kind {
	case "offline":
		offline, ok := options.Account.(*OfflineAccount)
		if !ok {
			return nil, newLaunchError("离线账号实现异常。")
		}
		authPlayerName = offline.AccountUsername()
		authUuid = toCompactUuid(offline.AccountUuid())
		authAccessToken = "0"
		authSession = "token:0:" + authUuid
		clientId = ""
		authXuid = ""
		userType = "legacy"
	case "microsoft":
		microsoft, ok := options.Account.(auth.MicrosoftAccount)
		if !ok {
			if pointer, pointerOk := options.Account.(*auth.MicrosoftAccount); pointerOk {
				microsoft, ok = *pointer, true
			}
		}
		if !ok {
			return nil, newLaunchError("正版账号实现异常。")
		}
		authPlayerName = microsoft.Username
		authUuid = toCompactUuid(microsoft.Uuid)
		authAccessToken = microsoft.AccessToken
		authSession = fmt.Sprintf("token:%s:%s", microsoft.AccessToken, authUuid)
		clientId = microsoft.ClientId
		authXuid = microsoft.XboxUserId
		userType = "msa"
	case "authlib":
		// 皮肤站账号凭据由 -javaagent 注入的 authlib-injector 在游戏内接管会话服务，
		// 此处照常下发角色名/UUID/令牌，作为注入前的原始会话参数
		authPlayerName = options.Account.AccountUsername()
		authUuid = toCompactUuid(options.Account.AccountUuid())
		authAccessToken = options.Account.AccountAccessToken()
		authSession = fmt.Sprintf("token:%s:%s", authAccessToken, authUuid)
		clientId = ""
		authXuid = ""
		userType = options.Account.AccountUserType()
	default:
		return nil, newLaunchError(fmt.Sprintf("不支持的账号类型：%s", kind))
	}

	// ${version_name} 用于 Forge 的 -DignoreList=...,${version_name}.jar，
	// 必须匹配 client jar 的实际文件名（继承式版本 = 原版 id，如 1.20.1.jar），
	// 否则原版 client 未被忽略、会被 SecureJarHandler 模块化，
	// 与 srg jar（minecraft 模块）同时含 blaze3d.systems 导致模块冲突崩溃。
	versionName := profile.ClientJarVersionId
	if strings.TrimSpace(versionName) == "" {
		versionName = profile.SourceId
	}
	if strings.TrimSpace(versionName) == "" {
		versionName = profile.Id
	}
	placeholders := map[string]string{
		"auth_player_name":    authPlayerName,
		"version_name":        versionName,
		"game_directory":      gameDirectory,
		"assets_root":         assetsDirectory,
		"assets_index_name":   profile.AssetsId,
		"auth_uuid":           authUuid,
		"auth_access_token":   authAccessToken,
		"auth_session":        authSession,
		"clientid":            clientId,
		"auth_xuid":           authXuid,
		"user_type":           userType,
		"version_type":        profile.VersionType,
		"user_properties":     "{}",
		"profile_properties":  "{}",
		"game_assets":         gameAssetsDirectory,
		"natives_directory":   nativeDirectory,
		"launcher_name":       options.LauncherName,
		"launcher_version":    options.LauncherVersion,
		"classpath":           classpathValue,
		"classpath_separator": string(filepath.ListSeparator),
		"library_directory":   librariesDirectory,
		"resolution_width":    fmt.Sprintf("%d", options.WindowWidth),
		"resolution_height":   fmt.Sprintf("%d", options.WindowHeight),
		"path":                logConfigPath,
	}

	features := MinecraftRuleEvaluator.CreateDefaultFeatures(
		options.WindowWidth > 0 && options.WindowHeight > 0)

	// 用户 JVM 参数自带 -Xms/-Xmx 时不重复下发内置值：
	// JVM 按出现顺序取后者，重复参数只是无效冗余且部分 JVM 会告警
	userSpecifiesXms := containsMemoryArgument(options.AdditionalJvmArguments, "-Xms")
	userSpecifiesXmx := containsMemoryArgument(options.AdditionalJvmArguments, "-Xmx")

	var result []string
	// 插件前置 JVM 参数：置于所有 JVM 参数之前（注入/代理类参数需最先生效）
	for _, argument := range prependJvmArguments {
		replaced, err := replacePlaceholders(argument, placeholders)
		if err != nil {
			return nil, err
		}
		appendResolvedArgument(&result, argument, replaced)
	}

	// 日志配置：版本 JSON 的 logging.client.argument（原版为
	// "-Dlog4j.configurationFile=${path}"）要由启动器补进 JVM 参数——
	// 它不在 arguments.jvm 里，官方启动器同样是自己加的。放在 JVM 段最前，
	// 与官方一致。配置没下到本地时跳过：指着一个不存在的文件会让 log4j 报错，
	// 而用默认配置照样能进游戏（文件由安装/校验流程负责补全）。
	if argument := strings.TrimSpace(profile.LoggingArgument); argument != "" {
		if logConfigPath == "" || tools.FileExists(logConfigPath) {
			if resolved, err := replacePlaceholders(argument, placeholders); err == nil && resolved != "" {
				result = append(result, resolved)
			}
		}
	}

	if !userSpecifiesXms {
		result = append(result, fmt.Sprintf("-Xms%dM", options.MinimumMemoryMb))
	}
	if !userSpecifiesXmx {
		result = append(result, fmt.Sprintf("-Xmx%dM", options.MaximumMemoryMb))
	}

	// 通用 JVM 性能优化参数（仅在内存充足时启用预触页，低配机避免启动失败/变慢）
	result = append(result,
		"-XX:+UnlockExperimentalVMOptions",
		"-XX:+UseG1GC",
		"-XX:G1NewSizePercent=20",
		"-XX:G1ReservePercent=20",
		"-XX:MaxGCPauseMillis=50",
		// 并行处理引用（软/弱引用清理），显著压低 Full GC 停顿；MC 大量使用缓存引用，收益明显
		"-XX:+ParallelRefProcEnabled",
	)
	if options.MaximumMemoryMb >= 4096 {
		result = append(result,
			"-XX:+AlwaysPreTouch",
			"-XX:G1HeapRegionSize=32M",
			// G1 字符串去重：MC 日志/资源路径等重复字符串极多，可省 10-20% 堆
			"-XX:+UseStringDeduplication",
		)
	}

	result = append(result, options.AdditionalJvmArguments...)

	// 记录启动器自身 JVM 参数的起始位置：classpath 探测只看启动器与版本档案
	// 生成的参数，避免用户附加的 -cp 误抑制真正的 classpath 下发
	launcherJvmArgsStart := len(result)

	if len(profile.JvmArguments) > 0 {
		if err := appendModernArguments(&result, profile.JvmArguments, features, placeholders); err != nil {
			return nil, err
		}
	} else {
		result = append(result,
			"-Djava.library.path="+nativeDirectory,
			"-cp",
			classpathValue)
	}

	// 插件追加 JVM 参数：位于 JVM 段末尾、主类之前
	for _, argument := range appendJvmArguments {
		replaced, err := replacePlaceholders(argument, placeholders)
		if err != nil {
			return nil, err
		}
		appendResolvedArgument(&result, argument, replaced)
	}

	// NeoForge / Forge 的 FML 在 production 模式下要求 system property "libraryDirectory"
	// 指向 libraries 目录，用于定位 minecraft-client-patched / srg 等运行时产物；
	// 缺少该参数时 FML 无法找到 Minecraft 类并报 "installation corrupted"。
	hasLibraryDirectory := false
	for _, argument := range result {
		if strings.HasPrefix(argument, "-DlibraryDirectory=") {
			hasLibraryDirectory = true
			break
		}
	}
	if !hasLibraryDirectory {
		result = append(result, "-DlibraryDirectory="+librariesDirectory)
	}

	// classpath 探测只看启动器与版本档案生成的参数段
	if !containsClasspathArgument(result[launcherJvmArgsStart:]) {
		result = append(result, "-cp", classpathValue)
	}

	result = append(result, mainClass)

	// 插件前置游戏参数：紧贴主类之后、版本档案参数之前
	for _, argument := range prependGameArguments {
		replaced, err := replacePlaceholders(argument, placeholders)
		if err != nil {
			return nil, err
		}
		appendResolvedArgument(&result, argument, replaced)
	}

	if len(profile.GameArguments) > 0 {
		if err := appendModernArguments(&result, profile.GameArguments, features, placeholders); err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(profile.LegacyGameArguments) != "" {
		tokenized, err := tokenizeLegacyArguments(profile.LegacyGameArguments)
		if err != nil {
			return nil, err
		}
		for _, argument := range tokenized {
			replaced, err := replacePlaceholders(argument, placeholders)
			if err != nil {
				return nil, err
			}
			appendResolvedArgument(&result, argument, replaced)
		}
	} else {
		return nil, newLaunchError("版本配置没有可用的游戏启动参数。")
	}

	for _, argument := range options.AdditionalGameArguments {
		replaced, err := replacePlaceholders(argument, placeholders)
		if err != nil {
			return nil, err
		}
		appendResolvedArgument(&result, argument, replaced)
	}
	// 插件追加游戏参数：与其余插件参数列表一样做占位符替换后追加
	for _, argument := range appendGameArguments {
		replaced, err := replacePlaceholders(argument, placeholders)
		if err != nil {
			return nil, err
		}
		appendResolvedArgument(&result, argument, replaced)
	}
	return result, nil
}

// containsMemoryArgument 判断用户 JVM 参数是否自带指定内存参数（-Xms/-Xmx）。
func containsMemoryArgument(arguments []string, prefix string) bool {
	for _, argument := range arguments {
		if len(argument) >= len(prefix) &&
			strings.EqualFold(argument[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}

// toCompactUuid 将 UUID 归一化为 32 位无连字符格式。
// 官方启动器与主流启动器（HMCL 等）对 --uuid / --session 中的 UUID 均使用
// 无连字符格式；此处归一化可兼容历史版本存储的带连字符 UUID。
func toCompactUuid(uuid string) string {
	return strings.ReplaceAll(uuid, "-", "")
}

// appendModernArguments 展开 argument 数组：字符串直接替换占位符；
// 对象按 rules 过滤后展开 value（字符串或字符串数组）。
func appendModernArguments(
	target *[]string,
	argumentElements []json.RawMessage,
	features map[string]bool,
	placeholders map[string]string,
) error {
	for _, element := range argumentElements {
		var text string
		if json.Unmarshal(element, &text) == nil {
			replaced, err := replacePlaceholders(text, placeholders)
			if err != nil {
				return err
			}
			appendResolvedArgument(target, text, replaced)
			continue
		}

		var complexArgument struct {
			Rules []ruleJSON      `json:"rules"`
			Value json.RawMessage `json:"value"`
		}
		if json.Unmarshal(element, &complexArgument) != nil {
			continue
		}
		if !rulesAllow(complexArgument.Rules, features) {
			continue
		}

		var singleValue string
		if json.Unmarshal(complexArgument.Value, &singleValue) == nil {
			replaced, err := replacePlaceholders(singleValue, placeholders)
			if err != nil {
				return err
			}
			appendResolvedArgument(target, singleValue, replaced)
			continue
		}
		var multipleValues []string
		if json.Unmarshal(complexArgument.Value, &multipleValues) == nil {
			for _, value := range multipleValues {
				replaced, err := replacePlaceholders(value, placeholders)
				if err != nil {
					return err
				}
				appendResolvedArgument(target, value, replaced)
			}
		}
	}
	return nil
}

// rulesAllow 规则评估（供本文件内部使用；对应 C# MinecraftRuleEvaluator.IsAllowed）。
func rulesAllow(rules []ruleJSON, features map[string]bool) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, rule := range rules {
		if !ruleMatches(rule, features) {
			continue
		}
		allowed = rule.Action == "allow"
	}
	return allowed
}

func replacePlaceholders(value string, placeholders map[string]string) (string, error) {
	var failure error
	result := placeholderPattern.ReplaceAllStringFunc(value, func(matched string) string {
		name := matched[2 : len(matched)-1]
		replacement, ok := placeholders[name]
		if !ok {
			if failure == nil {
				failure = newLaunchError(fmt.Sprintf("版本参数包含暂不支持的占位符：%s", name))
			}
			return matched
		}
		return replacement
	})
	if failure != nil {
		return "", failure
	}
	return result, nil
}

// tokenizeLegacyArguments 切分旧版 minecraftArguments（支持双引号与 \" 转义）。
func tokenizeLegacyArguments(commandLine string) ([]string, error) {
	var current strings.Builder
	quoted := false
	var result []string

	runes := []rune(commandLine)
	for index := 0; index < len(runes); index++ {
		character := runes[index]
		if character == '"' {
			quoted = !quoted
			continue
		}

		if character == '\\' && index+1 < len(runes) && runes[index+1] == '"' {
			current.WriteRune('"')
			index++
			continue
		}

		if isWhiteSpace(character) && !quoted {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(character)
	}

	if quoted {
		return nil, newLaunchError("旧版启动参数包含未闭合的引号。")
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result, nil
}

func isWhiteSpace(character rune) bool {
	switch character {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

func containsClasspathArgument(arguments []string) bool {
	for index := 0; index < len(arguments)-1; index++ {
		if arguments[index] == "-cp" || arguments[index] == "-classpath" {
			return true
		}
	}
	return false
}

func getLegacyGameAssetsDirectory(assetsDirectory, assetsId string) string {
	virtualDirectory := filepath.Join(assetsDirectory, "virtual", assetsId)
	if tools.DirectoryExists(virtualDirectory) {
		return virtualDirectory
	}
	return assetsDirectory
}

func validateMemory(options MinecraftLaunchOptions) error {
	if options.MinimumMemoryMb <= 0 || options.MaximumMemoryMb < options.MinimumMemoryMb {
		return newLaunchError("内存设置无效：最大内存必须大于等于最小内存。")
	}
	return nil
}
