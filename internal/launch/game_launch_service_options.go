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
// 直接进服参数、插件启动贡献（对应 C# GameLaunchService.Options.cs）。
func (s *GameLaunchService) buildLaunchOptions(
	instance GameInstanceSnapshot,
	versionId string,
	launchAccount MinecraftAccount,
	serverHost string,
	serverPort *int,
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
		// 直接进服：原版客户端会读取追加在末尾的 --server / --port 参数
		effectivePort := 25565
		if serverPort != nil {
			effectivePort = *serverPort
		}
		additionalGameArguments = append(append([]string{}, additionalGameArguments...),
			"--server", serverHost,
			"--port", fmt.Sprintf("%d", effectivePort))
		s.appendLog(fmt.Sprintf("已指定进入服务器：%s:%d。", serverHost, effectivePort), "LAUNCH")
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
			context.Background(),
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
		MinecraftDirectory:             instance.MinecraftDirectory,
		GameDirectory:                  effectiveGameDirectory,
		VersionId:                      versionId,
		Account:                        launchAccount,
		JavaExecutable:                 javaExecutableForLaunch,
		JavaRuntimeDirectory:           javaRuntimeDirectory,
		MinimumMemoryMb:                effectiveMinimumMemory,
		MaximumMemoryMb:                memoryDecision.MaximumMemoryMb,
		WindowWidth:                    windowWidth,
		WindowHeight:                   windowHeight,
		AdditionalJvmArguments:         additionalJvmArguments,
		AdditionalGameArguments:        additionalGameArguments,
		ProcessPriority:                processPriority,
		WrapperCommand:                 wrapperCommand,
		EnvironmentVariables:           parseEnvironmentVariables(environmentVariables),
		LaunchFullscreen:               launchFullscreen,
		// 插件贡献的启动变换：绑定层把插件清单解析成变换后经
		// LaunchTransformProvider 注入（未注入 → 空变换）
		Transform: currentLaunchTransform(),
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
