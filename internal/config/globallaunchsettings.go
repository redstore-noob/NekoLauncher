package config

import (
	"encoding/json"
	"strings"
)

// GlobalLaunchSettings 全局高级启动设置的内存快照。
type GlobalLaunchSettings struct {
	WindowWidth             int
	WindowHeight            int
	JavaExecutable          string
	AdditionalJvmArguments  []string
	AdditionalGameArguments []string
	// ProcessPriority 游戏进程优先级："low" / "belownormal" / "normal" /
	// "abovenormal" / "high"；空串与 "normal" 表示不调整。
	ProcessPriority string
	// WrapperCommand 包装命令模板，需含 %command% 占位（如 "gamemoderun %command%"）；
	// 启动时占位被替换为 Java 可执行文件与全部参数。
	WrapperCommand string
	// AdditionalEnvironmentVariables 注入游戏进程的额外环境变量（"KEY=VALUE" 每项一条）。
	AdditionalEnvironmentVariables []string
	// LaunchFullscreen 以全屏启动游戏（追加 --fullscreen 游戏参数）。
	LaunchFullscreen bool
}

// GlobalLaunchSettingsStore 的配置键：所有字段写入 launcher.yaml 的独立键，
// 损坏的 JSON 值回退到默认值。
const (
	windowWidthKey    = "globalLaunchWindowWidth"
	windowHeightKey   = "globalLaunchWindowHeight"
	javaExecutableKey = "globalLaunchJavaExecutable"
	jvmArgumentsKey   = "globalLaunchJvmArguments"
	gameArgumentsKey  = "globalLaunchGameArguments"

	processPriorityKey  = "globalLaunchProcessPriority"
	wrapperCommandKey   = "globalLaunchWrapperCommand"
	envVariablesKey     = "globalLaunchEnvVars"
	fullscreenLaunchKey = "globalLaunchFullscreen"

	// automaticJavaValue Java 路径的"自动选择"占位值
	// （空路径在保存时写成它，避免与未配置混淆）。
	automaticJavaValue = "$auto"

	// launcherWindowWidthKey/launcherWindowHeightKey 启动器自身窗口尺寸的
	// 专用键。注意绝不能复用 globalLaunchWindowWidth（游戏启动分辨率）：
	// 两者语义无关，混用会在记忆窗口尺寸时冲掉游戏启动设置。
	launcherWindowWidthKey  = "launcherWindowWidth"
	launcherWindowHeightKey = "launcherWindowHeight"

	minimumWindowWidth  = 320
	minimumWindowHeight = 240
	defaultWindowWidth  = 854
	defaultWindowHeight = 480

	// 启动器窗口默认尺寸（与 main.go 的 Wails 窗口选项一致）。
	defaultLauncherWindowWidth  = 760
	defaultLauncherWindowHeight = 480
)

// LoadGlobalLaunchSettings 加载全局高级启动设置。
func LoadGlobalLaunchSettings() GlobalLaunchSettings {
	return GlobalLaunchSettings{
		WindowWidth:                    readInt(windowWidthKey, defaultWindowWidth, minimumWindowWidth),
		WindowHeight:                   readInt(windowHeightKey, defaultWindowHeight, minimumWindowHeight),
		JavaExecutable:                 readJavaExecutable(),
		AdditionalJvmArguments:         readArguments(jvmArgumentsKey),
		AdditionalGameArguments:        readArguments(gameArgumentsKey),
		ProcessPriority:                normalizeProcessPriority(GetValue(processPriorityKey)),
		WrapperCommand:                 strings.TrimSpace(GetValue(wrapperCommandKey)),
		AdditionalEnvironmentVariables: readArguments(envVariablesKey),
		LaunchFullscreen:               readBoolFlag(fullscreenLaunchKey),
	}
}

// normalizeProcessPriority 归一化进程优先级取值；非法值（含空串）归一为不调整。
func normalizeProcessPriority(value string) string {
	switch strings.TrimSpace(value) {
	case "low":
		return "low"
	case "belownormal":
		return "belownormal"
	case "abovenormal":
		return "abovenormal"
	case "high":
		return "high"
	default:
		return "normal"
	}
}

// readBoolFlag 读取布尔配置；未设置或值损坏时返回 false。
func readBoolFlag(key string) bool {
	result, err := parseBool(GetValue(key))
	return err == nil && result
}

// SaveGlobalLaunchSettings 保存整份全局设置（参数先规范化：去空白、剔除空参数）。
// 尺寸过小时拒绝保存。
func SaveGlobalLaunchSettings(settings GlobalLaunchSettings) bool {
	if settings.WindowWidth < minimumWindowWidth ||
		settings.WindowHeight < minimumWindowHeight {
		return false
	}

	normalized := settings
	normalized.JavaExecutable = strings.TrimSpace(settings.JavaExecutable)
	normalized.AdditionalJvmArguments = normalizeArguments(settings.AdditionalJvmArguments)
	normalized.AdditionalGameArguments = normalizeArguments(settings.AdditionalGameArguments)
	normalized.ProcessPriority = normalizeProcessPriority(settings.ProcessPriority)
	normalized.WrapperCommand = strings.TrimSpace(settings.WrapperCommand)
	normalized.AdditionalEnvironmentVariables = normalizeArguments(settings.AdditionalEnvironmentVariables)
	// 九个键一次事务落盘，避免中途失败留下半新半旧的全局设置
	return UpdateInTransaction(func(config map[string]any) bool {
		config[windowWidthKey] = itoa(normalized.WindowWidth)
		config[windowHeightKey] = itoa(normalized.WindowHeight)
		if strings.TrimSpace(normalized.JavaExecutable) == "" {
			config[javaExecutableKey] = automaticJavaValue
		} else {
			config[javaExecutableKey] = normalized.JavaExecutable
		}
		config[jvmArgumentsKey] = serializeStringList(normalized.AdditionalJvmArguments)
		config[gameArgumentsKey] = serializeStringList(normalized.AdditionalGameArguments)
		config[processPriorityKey] = normalized.ProcessPriority
		config[wrapperCommandKey] = normalized.WrapperCommand
		config[envVariablesKey] = serializeStringList(normalized.AdditionalEnvironmentVariables)
		config[fullscreenLaunchKey] = formatBool(normalized.LaunchFullscreen)
		return true
	})
}

// SaveGlobalWindowSize 保存启动器自身窗口尺寸（宽/高）。窗口尺寸写入
// launcher.yaml，不触碰游戏启动分辨率与其它全局设置。用于窗口尺寸记忆。
func SaveGlobalWindowSize(width, height int) bool {
	if width < minimumWindowWidth || height < minimumWindowHeight {
		return false
	}
	// 两条键分别落盘：半新半旧的窗口尺寸无害，不值得为此引入跨文件事务
	if !setValue(launcherWindowWidthKey, itoa(width)) {
		return false
	}
	return setValue(launcherWindowHeightKey, itoa(height))
}

// LoadLauncherWindowSize 读取上次保存的启动器窗口尺寸；
// 未保存过或值非法时返回默认尺寸（760×480，与 main.go 初始窗口一致）。
func LoadLauncherWindowSize() (int, int) {
	width := readInt(launcherWindowWidthKey, defaultLauncherWindowWidth, minimumWindowWidth)
	height := readInt(launcherWindowHeightKey, defaultLauncherWindowHeight, minimumWindowHeight)
	return width, height
}

func readInt(key string, fallback, minimum int) int {
	if value, err := parseInt(GetValue(key)); err == nil && value >= minimum {
		return value
	}
	return fallback
}

func readJavaExecutable() string {
	configured := GetValue(javaExecutableKey)
	if configured == automaticJavaValue {
		return ""
	}
	// 从未配置过时回退到全局探测到的 Java
	if configured == "" {
		return JavaExecutable()
	}
	return configured
}

func readArguments(key string) []string {
	value := GetValue(key)
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	var arguments []string
	if err := json.Unmarshal([]byte(value), &arguments); err != nil {
		// 用户手改 launcher.yaml 写坏数组 → 回退为空参数，不影响启动
		return []string{}
	}
	return normalizeArguments(arguments)
}
