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
//
// 这个函数会**回溯删除**已追加的标志（离线账号的 ${clientid} 解析为空 → 前面那条
// "--clientId" 必须一并撤销）。因此溯源记账不能只看"要不要 append"，
// 必须用同样的规则同步撤销，否则记账条数会多于最终命令行。
func appendResolvedArgument(target *[]string, source, resolved string) {
	if resolved != "" || !isNullablePlaceholder(source) {
		*target = append(*target, resolved)
		return
	}
	if count := len(*target); count > 0 && isFlagArgument((*target)[count-1]) {
		*target = (*target)[:count-1]
	}
}

// appendResolvedArgumentRecorded 带记账的 append。
// 先算"这次 append 之后目标切片会变成什么"，再据此同步记账：
//   - 正常追加 → 记一条；
//   - 触发回溯删除 → 不仅要跳过后面的记，还要**撤销刚记的那条**。
//
// 返回被撤销的记账条数（0 或 1），调用方无需关心，传参即可。
func appendResolvedArgumentRecorded(
	target *[]string,
	source, resolved string,
	recorder *provenanceRecorder,
	sourceInfo LaunchArgumentSource,
) {
	before := len(*target)
	appendResolvedArgument(target, source, resolved)
	after := len(*target)

	switch {
	case after > before:
		// 净增：本次确实落进命令行
		recorder.recordOne(resolved, sourceInfo)
	case after < before:
		// 回溯删除：本次没落进去，且还吃掉了上一条
		// （上一条的记账由调用方在正常追加时已经记过，这里补一次撤销）
		recorder.undoLast()
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
	return MinecraftArgumentBuilder{}.buildWithProvenance(
		profile, options, nativeDirectory, classpath, mainClass,
		prependJvmArguments, appendJvmArguments,
		prependGameArguments, appendGameArguments, nil)
}

// buildWithProvenance 与 Build 相同，额外把溯源报告写回 provenanceOut。
// 单独开一个入口是为了不改动 Build 的公开签名（调用方众多）。
func (MinecraftArgumentBuilder) buildWithProvenance(
	profile *MinecraftVersionProfile,
	options MinecraftLaunchOptions,
	nativeDirectory string,
	classpath []string,
	mainClass string,
	prependJvmArguments, appendJvmArguments []string,
	prependGameArguments, appendGameArguments []string,
	provenanceOut **LaunchProvenanceReport,
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
	
	// primary_jar_name 占位符：客户端 JAR 文件名（通常是 {version}.jar）
	primaryJarName := versionName + ".jar"
	
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
		"primary_jar_name":    primaryJarName,
	}

	features := MinecraftRuleEvaluator.CreateDefaultFeatures(
		options.WindowWidth > 0 && options.WindowHeight > 0)

	// 用户 JVM 参数自带 -Xms/-Xmx 时不重复下发内置值：
	// JVM 按出现顺序取后者，重复参数只是无效冗余且部分 JVM 会告警
	userSpecifiesXms := containsMemoryArgument(options.AdditionalJvmArguments, "-Xms")
	userSpecifiesXmx := containsMemoryArgument(options.AdditionalJvmArguments, "-Xmx")

	// 溯源记账（纯旁路，见 launch_provenance.go）：选项未要求时 recorder 为 nil，
	// 所有 record* 方法空转，热路径开销为零。
	recorder := newProvenanceRecorder(options.CollectProvenance)
	recorder.section(sectionJVM)

	var result []string
	// 插件前置 JVM 参数：置于所有 JVM 参数之前（注入/代理类参数需最先生效）
	pluginJvmSource := LaunchArgumentSource{
		Kind: SourcePlugin, Key: "plugin.prepend-jvm", PluginID: options.TransformPluginID,
	}
	for _, argument := range prependJvmArguments {
		replaced, err := replacePlaceholders(argument, placeholders)
		if err != nil {
			return nil, err
		}
		appendResolvedArgumentRecorded(&result, argument, replaced, recorder, pluginJvmSource)
	}

	// 日志配置：版本 JSON 的 logging.client.argument（原版为
	// "-Dlog4j.configurationFile=${path}"）要由启动器补进 JVM 参数——
	// 它不在 arguments.jvm 里，官方启动器同样是自己加的。放在 JVM 段最前，
	// 与官方一致。配置没下到本地时跳过：指着一个不存在的文件会让 log4j 报错，
	// 而用默认配置照样能进游戏（文件由安装/校验流程负责补全）。
	if argument := strings.TrimSpace(profile.LoggingArgument); argument != "" {
		if logConfigPath == "" || tools.FileExists(logConfigPath) {
			if resolved, err := replacePlaceholders(argument, placeholders); err == nil && resolved != "" {
				recorder.recordOne(resolved, LaunchArgumentSource{
					Kind: SourceVersionJSON, Key: "version-json.logging",
				})
				result = append(result, resolved)
			}
		}
	}

	if !userSpecifiesXms {
		argument := fmt.Sprintf("-Xms%dM", options.MinimumMemoryMb)
		recorder.recordOne(argument, LaunchArgumentSource{
			Kind: SourceLauncherAuto, Key: "memory.min.auto",
			Detail: fmt.Sprintf("%d", options.MinimumMemoryMb),
		})
		result = append(result, argument)
	}
	if !userSpecifiesXmx {
		argument := fmt.Sprintf("-Xmx%dM", options.MaximumMemoryMb)
		// 来源要区分"实例独立设置 / 全局手动上限 / 自动计算"——这正是用户最常问的那个"为什么"
		memoryKey := "memory.max.global"
		memoryKind := SourceGlobalSettings
		switch {
		case options.MemoryFromInstanceSettings:
			memoryKey, memoryKind = "memory.max.instance", SourceInstanceSettings
		case options.MemoryIsAutomatic:
			memoryKey, memoryKind = "memory.max.automatic", SourceLauncherAuto
		}
		recorder.recordOne(argument, LaunchArgumentSource{
			Kind: memoryKind, Key: memoryKey,
			Detail: fmt.Sprintf("%d", options.MaximumMemoryMb),
		})
		result = append(result, argument)
	}

	// 通用 JVM 性能优化参数（针对 Minecraft 工作负载特性优化）
	//
	// 第一条是整段参数的安全网：IgnoreUnrecognizedVMOptions 让"虚拟机不认识的
	// 参数"退化成一条警告而不是致命错误。没有它时，命令行里只要有任意一条
	// 当前 Java 不支持的选项（下面的调优项、实例/全局的「自定义 JVM 参数」、
	// 版本 JSON 的 arguments.jvm、插件追加的参数都算），JVM 会在初始化阶段
	// 直接退出：玩家看到的就是"点了启动，窗口一闪就关"，而且完全不知道为什么。
	// 带上它之后这些参数会被忽略，游戏照常起来，日志里留一条警告可查。
	// （顺序无关：JVM 会先收齐全部 -XX 参数再判定，实测前后放都一样。）
	tuningArguments := []string{
		"-XX:+IgnoreUnrecognizedVMOptions",
		"-XX:+UnlockExperimentalVMOptions",
		"-XX:+UseG1GC",
		
		// G1GC 核心参数：针对 MC 的内存分配模式优化
		"-XX:G1NewSizePercent=20",          // 新生代最小占比 20%，适应 MC 高分配率
		"-XX:G1MaxNewSizePercent=60",       // 新生代最大占比 60%，为对象晋升留空间
		"-XX:G1ReservePercent=20",          // 预留 20% 堆防止晋升失败触发 Full GC
		"-XX:MaxGCPauseMillis=50",          // 目标停顿 50ms，平衡吞吐与响应（游戏帧率敏感）
		"-XX:G1HeapWastePercent=5",         // 堆浪费阈值 5%，减少内存碎片
		"-XX:G1MixedGCCountTarget=4",       // 混合 GC 4 轮完成，缩短单次停顿
		"-XX:G1MixedGCLiveThresholdPercent=90", // 90% 存活率的老年代区域才混合回收，避免无效工作
		
		// 线程与编译优化
		"-XX:+ParallelRefProcEnabled",      // 并行处理引用（软/弱引用清理），显著压低 Full GC 停顿；MC 大量使用缓存引用，收益明显
		"-XX:+PerfDisableSharedMem",        // 禁用 JVM 性能计数器共享内存，减少 /tmp 文件系统 I/O
		"-XX:+DisableExplicitGC",           // 禁用显式 GC 调用（部分模组/插件的错误 System.gc() 会严重卡顿）
		"-XX:+AlwaysActAsServerClassMachine", // 强制使用服务器模式 JVM 配置（C2 编译器 + 更大代码缓存）
		"-XX:-OmitStackTraceInFastThrow",   // 保留异常堆栈跟踪，方便调试 MC 模组问题
		
		// 编译器优化（MaxInlineLevel 在 Java 8+ 都可用，适应 MC 深层方法调用链）
		"-XX:MaxInlineLevel=15",            // 内联深度 15，适应 MC 深层方法调用链
	}
	
	// 中等内存（≥4 GiB）优化
	if options.MaximumMemoryMb >= 4096 {
		tuningArguments = append(tuningArguments,
			// 字符串去重：MC 日志/资源路径/NBT 等重复字符串极多，可省 10-20% 堆
			"-XX:+UseStringDeduplication",
			"-XX:StringDeduplicationAgeThreshold=1", // 新生代晋升 1 次就去重，MC 字符串生命周期长
		)
	}
	
	// 大内存（≥8 GiB）优化
	if options.MaximumMemoryMb >= 8192 {
		tuningArguments = append(tuningArguments,
			"-XX:+AlwaysPreTouch",              // 预触页：启动时提交所有物理页，消除运行时缺页中断（仅大内存机器）
			"-XX:G1HeapRegionSize=32M",         // 32M region 减少大堆的 region 管理开销
			"-XX:InitiatingHeapOccupancyPercent=15", // 堆占用 15% 就启动并发标记，为大堆争取更多 GC 时间
		)
	} else {
		// 小内存堆默认参数
		tuningArguments = append(tuningArguments,
			"-XX:InitiatingHeapOccupancyPercent=40", // 小堆 40% 触发并发标记（更积极回收）
		)
	}
	
	// 超大内存（≥16 GiB）专属优化
	if options.MaximumMemoryMb >= 16384 {
		tuningArguments = append(tuningArguments,
			"-XX:G1NewSizePercent=30",          // 超大堆提升新生代下限到 30%
			"-XX:G1MaxNewSizePercent=50",       // 但上限降到 50%，为老年代留更多空间
			"-XX:SurvivorRatio=32",             // Survivor 区更小（1:32），减少复制开销
			"-XX:MaxTenuringThreshold=1",       // 快速晋升到老年代（MC 模组世界对象生命周期长）
		)
	}
	recorder.record(tuningArguments, LaunchArgumentSource{
		Kind: SourceLauncherAuto, Key: "jvm.tuning.g1",
	})
	result = append(result, tuningArguments...)

	// 用户附加 JVM 参数：来源取决于是否跟随全局（buildLaunchOptions 已做过合并）
	userJvmSource := LaunchArgumentSource{Kind: SourceInstanceSettings, Key: "jvm.user.instance"}
	if options.UsingGlobalLaunchSettings {
		userJvmSource = LaunchArgumentSource{Kind: SourceGlobalSettings, Key: "jvm.user.global"}
	}
	recorder.record(options.AdditionalJvmArguments, userJvmSource)
	result = append(result, options.AdditionalJvmArguments...)

	// 记录启动器自身 JVM 参数的起始位置：classpath 探测只看启动器与版本档案
	// 生成的参数，避免用户附加的 -cp 误抑制真正的 classpath 下发
	launcherJvmArgsStart := len(result)

	if len(profile.JvmArguments) > 0 {
		if err := appendModernArguments(&result, profile.JvmArguments, features, placeholders,
			recorder, LaunchArgumentSource{Kind: SourceVersionJSON, Key: "version-json.jvm"}); err != nil {
			return nil, err
		}
	} else {
		recorder.record([]string{
			"-Djava.library.path=" + nativeDirectory, "-cp", classpathValue,
		}, LaunchArgumentSource{Kind: SourceVersionJSON, Key: "version-json.classpath-legacy"})
		result = append(result,
			"-Djava.library.path="+nativeDirectory,
			"-cp",
			classpathValue)
	}

	// 插件追加 JVM 参数：位于 JVM 段末尾、主类之前
	pluginAppendJvmSource := LaunchArgumentSource{
		Kind: SourcePlugin, Key: "plugin.append-jvm", PluginID: options.TransformPluginID,
	}
	for _, argument := range appendJvmArguments {
		replaced, err := replacePlaceholders(argument, placeholders)
		if err != nil {
			return nil, err
		}
		appendResolvedArgumentRecorded(&result, argument, replaced, recorder, pluginAppendJvmSource)
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
		argument := "-DlibraryDirectory=" + librariesDirectory
		recorder.recordOne(argument, LaunchArgumentSource{
			Kind: SourceLauncherAuto, Key: "jvm.library-directory",
		})
		result = append(result, argument)
	}

	// classpath 探测只看启动器与版本档案生成的参数段
	if !containsClasspathArgument(result[launcherJvmArgsStart:]) {
		recorder.record([]string{"-cp", classpathValue}, LaunchArgumentSource{
			Kind: SourceLauncherAuto, Key: "jvm.classpath",
		})
		result = append(result, "-cp", classpathValue)
	}

	recorder.section(sectionMainClass)
	recorder.recordOne(mainClass, LaunchArgumentSource{
		Kind: SourceVersionJSON, Key: "version-json.main-class",
	})
	result = append(result, mainClass)

	recorder.section(sectionGame)
	// 插件前置游戏参数：紧贴主类之后、版本档案参数之前
	pluginPrependGameSource := LaunchArgumentSource{
		Kind: SourcePlugin, Key: "plugin.prepend-game", PluginID: options.TransformPluginID,
	}
	for _, argument := range prependGameArguments {
		replaced, err := replacePlaceholders(argument, placeholders)
		if err != nil {
			return nil, err
		}
		appendResolvedArgumentRecorded(&result, argument, replaced, recorder, pluginPrependGameSource)
	}

	if len(profile.GameArguments) > 0 {
		if err := appendModernArguments(&result, profile.GameArguments, features, placeholders,
			recorder, LaunchArgumentSource{Kind: SourceVersionJSON, Key: "version-json.game"}); err != nil {
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
			appendResolvedArgumentRecorded(&result, argument, replaced, recorder,
				LaunchArgumentSource{Kind: SourceVersionJSON, Key: "version-json.game"})
		}
	} else {
		return nil, newLaunchError("版本配置没有可用的游戏启动参数。")
	}

	gameArgumentSource := LaunchArgumentSource{Kind: SourceInstanceSettings, Key: "game.user.instance"}
	if options.UsingGlobalLaunchSettings {
		gameArgumentSource = LaunchArgumentSource{Kind: SourceGlobalSettings, Key: "game.user.global"}
	}
	for _, argument := range options.AdditionalGameArguments {
		replaced, err := replacePlaceholders(argument, placeholders)
		if err != nil {
			return nil, err
		}
		appendResolvedArgumentRecorded(&result, argument, replaced, recorder, gameArgumentSource)
	}
	// 插件追加游戏参数：与其余插件参数列表一样做占位符替换后追加
	pluginAppendGameSource := LaunchArgumentSource{
		Kind: SourcePlugin, Key: "plugin.append-game", PluginID: options.TransformPluginID,
	}
	for _, argument := range appendGameArguments {
		replaced, err := replacePlaceholders(argument, placeholders)
		if err != nil {
			return nil, err
		}
		appendResolvedArgumentRecorded(&result, argument, replaced, recorder, pluginAppendGameSource)
	}

	// 溯源是纯旁路：记账条目与最终参数逐一对齐后再产出报告，
	// 任何对不齐（理论不可达，记账点与 append 点成对）都不影响返回值。
	if recorder != nil {
		report := recorder.collectProvenance(result)
		if report != nil {
			report.JavaExecutable = options.JavaExecutable
			report.MainClass = mainClass
			report.WorkingDirectory = gameDirectory
		}
		if provenanceOut != nil {
			*provenanceOut = report
		}
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
// recorder 可为 nil（不记账）：记账在占位符替换**之后**按最终值记，
// 与 Build 末尾的逐条对齐检查口径一致。
func appendModernArguments(
	target *[]string,
	argumentElements []json.RawMessage,
	features map[string]bool,
	placeholders map[string]string,
	recorder *provenanceRecorder,
	source LaunchArgumentSource,
) error {
	for _, element := range argumentElements {
		var text string
		if json.Unmarshal(element, &text) == nil {
			replaced, err := replacePlaceholders(text, placeholders)
			if err != nil {
				return err
			}
			appendResolvedArgumentRecorded(target, text, replaced, recorder, source)
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
			appendResolvedArgumentRecorded(target, singleValue, replaced, recorder, source)
			continue
		}
		var multipleValues []string
		if json.Unmarshal(complexArgument.Value, &multipleValues) == nil {
			for _, value := range multipleValues {
				replaced, err := replacePlaceholders(value, placeholders)
				if err != nil {
					return err
				}
				appendResolvedArgumentRecorded(target, value, replaced, recorder, source)
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
