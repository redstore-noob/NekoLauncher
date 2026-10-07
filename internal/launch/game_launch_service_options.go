package launch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekolauncher/internal/auth"
	"nekolauncher/internal/config"
	"nekolauncher/internal/download"
)

// downloadVerifier 文件校验器（薄包装，便于测试注入）。
type downloadVerifier struct{}

func (downloadVerifier) verifyAndRepair(
	ctx context.Context,
	minecraftDirectory, versionId string,
	hasCustomResolution bool,
	status func(string),
) (int, error) {
	var verifier download.GameFileVerifier
	return verifier.VerifyAndRepair(ctx, minecraftDirectory, versionId, hasCustomResolution, status)
}

// effectiveHasCustomResolution 计算与启动参数装配一致的"是否下发自定义分辨率"
// 判定：实例开启独立设置且未跟随全局时用实例窗口尺寸，否则用全局设置。
// 文件校验用它对齐 has_custom_resolution 库规则的评估口径。
func effectiveHasCustomResolution(minecraftDirectory, versionId string) bool {
	profile := config.Get(minecraftDirectory, versionId)
	width, height := profile.WindowWidth, profile.WindowHeight
	if profile.FollowGlobalAdvancedSettings {
		global := config.LoadGlobalLaunchSettings()
		width, height = global.WindowWidth, global.WindowHeight
	}
	return width > 0 && height > 0
}

// buildLaunchOptions 启动参数装配：实例独立设置与全局高级设置的合并、内存决策、
// 直接进服参数、快速进存档参数、插件启动贡献（对应 C# GameLaunchService.Options.cs）。
func (s *GameLaunchService) buildLaunchOptions(
	ctx context.Context,
	instance GameInstanceSnapshot,
	versionId string,
	launchAccount MinecraftAccount,
	serverHost string,
	serverPort *int,
	worldName string,
) (*MinecraftLaunchOptions, error) {
	versionProfile := config.Get(instance.MinecraftDirectory, versionId)
	isolatedGameDirectory := resolveIsolatedGameDirectory(
		instance.MinecraftDirectory, instance.SourcePath, versionId)

	var instanceMaximumMemoryMb *int
	if versionProfile.UseIndependentMemorySettings {
		maximum := versionProfile.MaximumMemoryMb
		instanceMaximumMemoryMb = &maximum
	}
	memoryDecision := ResolveForLaunch(instanceMaximumMemoryMb)
	effectiveMinimumMemory := 512
	if versionProfile.UseIndependentMemorySettings {
		effectiveMinimumMemory = versionProfile.MinimumMemoryMb
	}
	if effectiveMinimumMemory > memoryDecision.MaximumMemoryMb {
		effectiveMinimumMemory = memoryDecision.MaximumMemoryMb
	}
	
	// 加载实例级别的 Java 配置（优先级最高）
	instanceJavaConfig, _ := config.LoadInstanceJavaConfig(
		filepath.Join(instance.MinecraftDirectory, "versions", versionId))
	
	globalLaunchSettings := config.LoadGlobalLaunchSettings()
	javaExecutable := versionProfile.JavaExecutable
	windowWidth := versionProfile.WindowWidth
	windowHeight := versionProfile.WindowHeight
	additionalJvmArguments := versionProfile.AdditionalJvmArguments
	additionalGameArguments := versionProfile.AdditionalGameArguments
	processPriority := versionProfile.ProcessPriority
	wrapperCommand := versionProfile.WrapperCommand
	environmentVariables := versionProfile.AdditionalEnvironmentVariables
	launchFullscreen := versionProfile.LaunchFullscreen
	
	// 应用实例 Java 配置（优先级最高）
	if instanceJavaConfig != nil {
		if instanceJavaConfig.JavaExecutable != "" {
			javaExecutable = instanceJavaConfig.JavaExecutable
		}
		if instanceJavaConfig.MinMemoryMB > 0 {
			effectiveMinimumMemory = instanceJavaConfig.MinMemoryMB
		}
		if instanceJavaConfig.MaxMemoryMB > 0 {
			memoryDecision.MaximumMemoryMb = instanceJavaConfig.MaxMemoryMB
			memoryDecision.IsAutomatic = false
			memoryDecision.FromInstanceSettings = true
		}
		if len(instanceJavaConfig.AdditionalJvmArguments) > 0 {
			additionalJvmArguments = append(additionalJvmArguments, instanceJavaConfig.AdditionalJvmArguments...)
		}
		if len(instanceJavaConfig.AdditionalGameArguments) > 0 {
			additionalGameArguments = append(additionalGameArguments, instanceJavaConfig.AdditionalGameArguments...)
		}
	}
	
	if versionProfile.FollowGlobalAdvancedSettings {
		javaExecutable = globalLaunchSettings.JavaExecutable
		windowWidth = globalLaunchSettings.WindowWidth
		windowHeight = globalLaunchSettings.WindowHeight
		additionalJvmArguments = globalLaunchSettings.AdditionalJvmArguments
		additionalGameArguments = globalLaunchSettings.AdditionalGameArguments
		processPriority = globalLaunchSettings.ProcessPriority
		wrapperCommand = globalLaunchSettings.WrapperCommand
		environmentVariables = globalLaunchSettings.AdditionalEnvironmentVariables
		launchFullscreen = globalLaunchSettings.LaunchFullscreen
	}
	if additionalJvmArguments == nil {
		additionalJvmArguments = []string{}
	}
	if additionalGameArguments == nil {
		additionalGameArguments = []string{}
	}
	if environmentVariables == nil {
		environmentVariables = []string{}
	}
	if launchFullscreen {
		// 原版客户端内置参数：以全屏启动（窗口宽高参数仍下发，不冲突）
		additionalGameArguments = append(append([]string{}, additionalGameArguments...),
			"--fullscreen")
	}
	if serverHost != "" {
		// 直接进服：优先使用 Minecraft 1.20+ 的 --quickPlayMultiplayer 参数（自动进服），
		// 不支持时回退到传统 --server / --port 参数（进入多人游戏界面但不自动连接）
		effectivePort := 25565
		if serverPort != nil {
			effectivePort = *serverPort
		}
		serverAddress := serverHost
		if effectivePort != 25565 {
			serverAddress = fmt.Sprintf("%s:%d", serverHost, effectivePort)
		}
		
		// 优先使用 Quick Play 参数（Minecraft 1.20+）：主菜单跳过，直接进入服务器
		additionalGameArguments = append(append([]string{}, additionalGameArguments...),
			"--quickPlayMultiplayer", serverAddress)
		
		// 兼容参数：旧版本会忽略 --quickPlayMultiplayer 但识别 --server / --port
		additionalGameArguments = append(additionalGameArguments,
			"--server", serverHost,
			"--port", fmt.Sprintf("%d", effectivePort))
		
		s.appendLog(fmt.Sprintf("已指定快速进入服务器：%s（Quick Play 模式 + 兼容模式）。", serverAddress), "LAUNCH")
	}
	if worldName != "" {
		// 快速进存档：Minecraft 1.20+ 的 --quickPlaySingleplayer 参数（跳过主菜单，直接进入指定世界）
		// 旧版本会忽略该参数，正常进入主菜单
		additionalGameArguments = append(append([]string{}, additionalGameArguments...),
			"--quickPlaySingleplayer", worldName)
		s.appendLog(fmt.Sprintf("已指定快速进入存档：%s（Quick Play 模式，需 Minecraft 1.20+）。", worldName), "LAUNCH")
	}
	effectiveGameDirectory := isolatedGameDirectory
	if strings.TrimSpace(effectiveGameDirectory) == "" {
		effectiveGameDirectory = instance.MinecraftDirectory
	}

	if memoryDecision.IsAutomatic {
		s.appendLog(fmt.Sprintf(
			"已根据启动前可用内存自动设置：可用 %d MiB，保留 %d MiB，游戏最大 %d MiB。",
			memoryDecision.AvailableMemoryMb, memoryDecision.ReservedMemoryMb, memoryDecision.MaximumMemoryMb), "LAUNCH")
	} else if versionProfile.UseIndependentMemorySettings {
		s.appendLog(fmt.Sprintf(
			"已应用独立内存设置：实例上限 %d MiB，全局手动上限生效后游戏最大 %d MiB。",
			versionProfile.MaximumMemoryMb, memoryDecision.MaximumMemoryMb), "LAUNCH")
	} else {
		s.appendLog(fmt.Sprintf(
			"实例未开启独立调整，已使用全局手动内存：最大 %d MiB。",
			memoryDecision.MaximumMemoryMb), "LAUNCH")
	}
	if memoryDecision.IsMemoryTight {
		s.appendLog("警告：系统可用内存严重不足，已按 2 GiB 保底分配，"+
			"游戏可能卡顿甚至崩溃，建议关闭其他程序后再启动。", "LAUNCH")
	}

	if authlibAccount, ok := launchAccount.(auth.AuthlibAccount); ok {
		// 皮肤站账号：把游戏会话服务重定向到皮肤站，需要注入器 jar 在本地就绪
		s.appendLog("正在准备 authlib-injector 注入器。", "LAUNCH")
		injectorJarPath, err := auth.EnsureInjector(
			ctx,
			instance.MinecraftDirectory,
			func(line string) { s.appendLog(line, "LAUNCH") })
		if err != nil {
			return nil, err
		}
		additionalJvmArguments = append(append([]string{}, additionalJvmArguments...),
			fmt.Sprintf("-javaagent:%s=%s", injectorJarPath, authlibAccount.ApiRoot))
		s.appendLog(fmt.Sprintf("已启用外置登录：%s", authlibAccount.ApiRoot), "LAUNCH")
	}

	javaExecutableForLaunch := javaExecutable
	if strings.TrimSpace(javaExecutableForLaunch) == "" {
		javaExecutableForLaunch = config.JavaExecutable()
	}
	javaRuntimeDirectory := os.Getenv("NEKOLAUNCHER_JAVA_RUNTIME")
	if strings.TrimSpace(javaRuntimeDirectory) == "" {
		javaRuntimeDirectory = filepath.Join(GetDefaultMinecraftDirectory(), "runtime")
	}

	options := &MinecraftLaunchOptions{
		MinecraftDirectory:      instance.MinecraftDirectory,
		GameDirectory:           effectiveGameDirectory,
		VersionId:               versionId,
		Account:                 launchAccount,
		JavaExecutable:          javaExecutableForLaunch,
		JavaRuntimeDirectory:    javaRuntimeDirectory,
		MinimumMemoryMb:         effectiveMinimumMemory,
		MaximumMemoryMb:         memoryDecision.MaximumMemoryMb,
		WindowWidth:             windowWidth,
		WindowHeight:            windowHeight,
		AdditionalJvmArguments:  additionalJvmArguments,
		AdditionalGameArguments: additionalGameArguments,
		ProcessPriority:         processPriority,
		WrapperCommand:          wrapperCommand,
		EnvironmentVariables:    parseEnvironmentVariables(environmentVariables),
		LaunchFullscreen:        launchFullscreen,
		// 插件贡献的启动变换：绑定层把插件清单解析成变换后经
		// LaunchTransformProvider 注入（未注入 → 空变换）
		Transform: currentLaunchTransform(),
		// 溯源归因所需的事实（装配完就不是"来自哪里"了，必须在这里记下来）
		MemoryFromInstanceSettings: memoryDecision.FromInstanceSettings,
		MemoryIsAutomatic:          memoryDecision.IsAutomatic,
		UsingGlobalLaunchSettings:  versionProfile.FollowGlobalAdvancedSettings,
		// 每次都采集：报告只在启动成功后供"为什么这样启动"面板查看，
		// 采集本身是纯旁路记账，不影响任何下发参数。
		CollectProvenance: true,
		GameOutputCallback: func(line string, isStderr bool) {
			if isStderr {
				line = "[stderr] " + line
			}
			s.appendLog(line, "GAME")
		},
		LogCallback: func(line string) { s.appendLog(line, "LAUNCH") },
	}

	if versionProfile.FollowGlobalAdvancedSettings {
		s.appendLog("已应用全局高级启动设置。", "LAUNCH")
	} else {
		s.appendLog("已应用当前实例的独立高级启动设置。", "LAUNCH")
	}
	if instanceJavaConfig != nil && (instanceJavaConfig.JavaExecutable != "" || 
		instanceJavaConfig.MinMemoryMB > 0 || instanceJavaConfig.MaxMemoryMB > 0 ||
		len(instanceJavaConfig.AdditionalJvmArguments) > 0 || len(instanceJavaConfig.AdditionalGameArguments) > 0) {
		s.appendLog("已应用实例级别的 Java 配置（java_config.yaml）。", "LAUNCH")
	}
	if wrapperCommand != "" {
		s.appendLog(fmt.Sprintf("已启用包装命令：%s", wrapperCommand), "LAUNCH")
	}
	switch processPriority {
	case "", "normal":
	default:
		s.appendLog(fmt.Sprintf("游戏进程优先级：%s。", processPriority), "LAUNCH")
	}
	return options, nil
}

// parseEnvironmentVariables 把 "KEY=VALUE" 列表解析为映射；
// 缺少 "=" 或键为空的条目直接丢弃（保存时已 trim，这里兜底防手改配置）。
func parseEnvironmentVariables(entries []string) map[string]string {
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		trimmed := strings.TrimSpace(entry)
		equals := strings.Index(trimmed, "=")
		if equals <= 0 {
			continue
		}
		result[trimmed[:equals]] = trimmed[equals+1:]
	}
	return result
}
