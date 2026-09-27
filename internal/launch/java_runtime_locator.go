package launch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/tools"
)

// JavaRuntimeLocator Java 运行时定位器的抽象接口。
// 负责在系统中找到可用的 java 可执行文件，并可校验其版本是否满足
// Minecraft 版本的最低要求。
type JavaRuntimeLocator interface {
	// FindJavaExecutable 查找 Java 可执行文件。
	// configuredPath 用户显式配置的 java 路径（最高优先）；
	// requiredMajorVersion Minecraft 版本要求的最低 Java 主版本；为 nil 时不校验版本；
	// runtimeDirectory Minecraft runtime 根目录（会递归扫描其中的 java）；
	// 为空时读取 NEKOLAUNCHER_JAVA_RUNTIME。
	FindJavaExecutable(configuredPath string, requiredMajorVersion *int, runtimeDirectory string) (string, error)
	// FindExactMatchJava 在不进行"最低版本"回退的前提下，查找主版本精确匹配
	// requiredMajorVersion 的 Java。找不到时返回空串（不报错），
	// 供调用方决定是否自动下载所需 Java。显式配置的精确匹配优先于自动探测的精确匹配。
	FindExactMatchJava(configuredPath string, requiredMajorVersion int, runtimeDirectory string) string
	// FindAllJavaExecutables 列出所有可用的 Java 可执行文件路径（去重、存在性过滤），
	// 供设置页「自动检索」一次性全部加入管理列表。
	FindAllJavaExecutables(runtimeDirectory string) []string
}

// javaCandidate 候选 java 路径；IsPreferred 表示该来源是否为"显式配置"（优先返回）。
type javaCandidate struct {
	Path        string
	IsPreferred bool
}

// DefaultJavaRuntimeLocator 默认的 Java 运行时定位器实现。
// 按优先级依次从"显式配置、NEKOLAUNCHER_JAVA、Minecraft runtime、JAVA_HOME、PATH"
// 寻找 java，在要求版本时通过执行 java -version 探测版本，并按以下优先级选择：
//  1. 主版本精确匹配 requiredMajorVersion（避免用 Java 25 启动要求 Java 17 的旧加载器，
//     如 Forge 1.20.x 在 Java 21+ 上会因 JPMS 模块冲突崩溃）；
//  2. 显式配置且 ≥ 最低要求；
//  3. 自动探测中最低且 ≥ 最低要求。
type DefaultJavaRuntimeLocator struct{}

// javaVersionPattern 用于解析 java -version 输出中的主/次版本号。
// 匹配形如 version "21.0.1" 或旧式的 version "1.8.0_202"。
var javaVersionPattern = regexp.MustCompile(`version\s+"(\d+)(?:\.(\d+))?`)

// FindJavaExecutable 查找符合要求的 Java 可执行文件。
func (DefaultJavaRuntimeLocator) FindJavaExecutable(
	configuredPath string, requiredMajorVersion *int, runtimeDirectory string,
) (string, error) {
	candidates := buildJavaCandidates(configuredPath, runtimeDirectory)

	var discoveredVersions []int
	type probedCandidate struct {
		Path         string
		MajorVersion int
		Index        int
		IsPreferred  bool
	}
	var probed []probedCandidate
	visitedPaths := map[string]bool{}
	for index, candidate := range candidates {
		if strings.TrimSpace(candidate.Path) == "" {
			continue
		}

		// 展开环境变量（%VAR%/$VAR），去重（同一路径只处理一次），并确认文件存在
		expandedPath := os.ExpandEnv(candidate.Path)
		key := strings.ToLower(expandedPath)
		if visitedPaths[key] || !tools.FileExists(expandedPath) {
			continue
		}
		visitedPaths[key] = true

		fullPath, err := filepath.Abs(expandedPath)
		if err != nil {
			continue
		}

		// 不要求特定版本时，直接返回第一个找到的 java
		if requiredMajorVersion == nil {
			return fullPath, nil
		}

		// 要求版本时，执行 java -version 探测实际主版本
		majorVersion := tryGetJavaMajorVersion(fullPath)
		if majorVersion != nil {
			discoveredVersions = append(discoveredVersions, *majorVersion)
			probed = append(probed, probedCandidate{
				Path: fullPath, MajorVersion: *majorVersion, Index: index, IsPreferred: candidate.IsPreferred,
			})
		}
	}

	if len(probed) == 0 {
		requirement := ""
		if requiredMajorVersion != nil {
			requirement = fmt.Sprintf("该 Minecraft 版本至少需要 Java %d。", *requiredMajorVersion)
		}
		return "", newLaunchError(requirement +
			"未找到可用的 Java 运行时。请在启动器下载页安装 Java，或配置 JAVA_HOME / NEKOLAUNCHER_JAVA。")
	}

	requiredVersion := *requiredMajorVersion

	// 优先：主版本精确匹配——避免用 Java 25 启动要求 Java 17 的旧版本/加载器。
	// 精确匹配中显式配置优先，其次来源顺序。
	sort.SliceStable(probed, func(left, right int) bool {
		if probed[left].MajorVersion != probed[right].MajorVersion {
			return probed[left].MajorVersion < probed[right].MajorVersion
		}
		if probed[left].IsPreferred != probed[right].IsPreferred {
			return probed[left].IsPreferred
		}
		return probed[left].Index < probed[right].Index
	})
	for _, candidate := range probed {
		if candidate.MajorVersion == requiredVersion {
			return candidate.Path, nil
		}
	}

	// 次选：显式配置且 ≥ 最低要求（尊重用户指定，只要不低于最低版本）
	for _, candidate := range probed {
		if candidate.IsPreferred && candidate.MajorVersion >= requiredVersion {
			return candidate.Path, nil
		}
	}

	// 再次：自动探测中最低且 ≥ 最低要求（最接近最低要求）
	for _, candidate := range probed {
		if candidate.MajorVersion >= requiredVersion {
			return candidate.Path, nil
		}
	}

	// 全部低于最低要求
	distinct := distinctInts(discoveredVersions)
	sort.Ints(distinct)
	versionsText := make([]string, 0, len(distinct))
	for _, version := range distinct {
		versionsText = append(versionsText, fmt.Sprintf("%d", version))
	}
	return "", newLaunchError(fmt.Sprintf(
		"该 Minecraft 版本至少需要 Java %d。 已检测到 Java %s。 请在启动器下载页安装兼容的 Java 运行时，或配置 JAVA_HOME / NEKOLAUNCHER_JAVA。",
		requiredVersion, strings.Join(versionsText, "、")))
}

// FindExactMatchJava 查找主版本精确匹配的 Java；找不到返回空串。
func (DefaultJavaRuntimeLocator) FindExactMatchJava(
	configuredPath string, requiredMajorVersion int, runtimeDirectory string,
) string {
	candidates := buildJavaCandidates(configuredPath, runtimeDirectory)
	visitedPaths := map[string]bool{}
	var preferredExact string

	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Path) == "" {
			continue
		}
		expandedPath := os.ExpandEnv(candidate.Path)
		key := strings.ToLower(expandedPath)
		if visitedPaths[key] || !tools.FileExists(expandedPath) {
			continue
		}
		visitedPaths[key] = true

		fullPath, err := filepath.Abs(expandedPath)
		if err != nil {
			continue
		}
		if major := tryGetJavaMajorVersion(fullPath); major != nil && *major == requiredMajorVersion {
			// 显式配置的精确匹配优先返回；否则记录第一个自动探测的精确匹配
			if candidate.IsPreferred {
				return fullPath
			}
			if preferredExact == "" {
				preferredExact = fullPath
			}
		}
	}

	return preferredExact
}

// FindAllJavaExecutables 列出所有可用的 Java 可执行文件路径。
func (DefaultJavaRuntimeLocator) FindAllJavaExecutables(runtimeDirectory string) []string {
	candidates := buildJavaCandidates("", runtimeDirectory)
	var results []string
	visitedPaths := map[string]bool{}

	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Path) == "" {
			continue
		}
		expandedPath := os.ExpandEnv(candidate.Path)
		key := strings.ToLower(expandedPath)
		if visitedPaths[key] || !tools.FileExists(expandedPath) {
			continue
		}
		visitedPaths[key] = true
		if fullPath, err := filepath.Abs(expandedPath); err == nil {
			results = append(results, fullPath)
		}
	}
	return results
}

// buildJavaCandidates 收集所有候选 java 路径。
func buildJavaCandidates(configuredPath, runtimeDirectory string) []javaCandidate {
	var candidates []javaCandidate

	// 1. 用户显式指定的路径（来自启动选项 JavaExecutable）
	if strings.TrimSpace(configuredPath) != "" {
		candidates = append(candidates, javaCandidate{Path: configuredPath, IsPreferred: true})
	}

	// 2. NEKOLAUNCHER_JAVA 环境变量
	candidates = append(candidates, javaCandidate{Path: os.Getenv("NEKOLAUNCHER_JAVA"), IsPreferred: true})

	// 3. 启动器中已保存的全部 Java（设置页手动添加 / 自动检索的结果）。
	//    标记为非首选：不抢占显式配置，但参与"精确主版本匹配"与
	//    "满足最低要求的最低版本"挑选——否则主 Java 版本不合适时，
	//    列表里明明存着的合适 Java 永远不会被选中。
	for _, item := range config.GetJavaPaths() {
		if strings.TrimSpace(item.JavaPath) == "" {
			continue
		}
		candidates = append(candidates, javaCandidate{Path: item.JavaPath})
	}

	// 4. Minecraft runtime 目录：优先使用参数，其次读取 NEKOLAUNCHER_JAVA_RUNTIME，
	//    递归扫描其中所有名为 java/java.exe 的可执行文件
	configuredRuntime := runtimeDirectory
	if strings.TrimSpace(configuredRuntime) == "" {
		configuredRuntime = os.Getenv("NEKOLAUNCHER_JAVA_RUNTIME")
	}
	candidates = append(candidates, enumerateRuntimeJavaExecutables(configuredRuntime)...)

	// 5. JAVA_HOME/bin/java
	javaHome := os.Getenv("JAVA_HOME")
	if strings.TrimSpace(javaHome) != "" {
		candidates = append(candidates, javaCandidate{
			Path: filepath.Join(javaHome, "bin", javaExecutableName()),
		})
	}

	// 6. PATH 环境变量中的每个目录
	pathValue := os.Getenv("PATH")
	if strings.TrimSpace(pathValue) != "" {
		for _, directory := range filepath.SplitList(pathValue) {
			directory = strings.Trim(strings.TrimSpace(directory), `"`)
			if directory == "" {
				continue
			}
			candidates = append(candidates, javaCandidate{
				Path: filepath.Join(directory, javaExecutableName()),
			})
		}
	}

	return candidates
}

// javaExecutableName 当前平台下 Java 可执行文件的文件名
// （Windows 为 java.exe，其余为 java）。
func javaExecutableName() string {
	if isWindows() {
		return "java.exe"
	}
	return "java"
}

// enumerateRuntimeJavaExecutables 递归枚举 runtime 目录下所有名为 java/java.exe 的文件。
func enumerateRuntimeJavaExecutables(runtimeDirectory string) []javaCandidate {
	if strings.TrimSpace(runtimeDirectory) == "" {
		return nil
	}
	// 规范化：展开环境变量、去掉首尾引号
	expanded := os.ExpandEnv(strings.Trim(strings.TrimSpace(runtimeDirectory), `"`))
	if !tools.DirectoryExists(expanded) {
		return nil
	}
	var candidates []javaCandidate
	_ = filepath.WalkDir(expanded, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() && entry.Name() == javaExecutableName() {
			candidates = append(candidates, javaCandidate{Path: path})
		}
		return nil
	})
	return candidates
}

// TryDetectJavaMajorVersion 探测指定 java 可执行文件的主版本号；
// 无法执行或解析失败时返回 nil。供设置页等 UI 在添加 Java 路径时即时识别版本。
func TryDetectJavaMajorVersion(javaExecutable string) *int {
	return tryGetJavaMajorVersion(javaExecutable)
}

// tryGetJavaMajorVersion 执行 java -version 探测 Java 的主版本号。
// 兼容新旧两种版本号格式：新式 "21.0.1" 直接取主版本 21；
// 旧式 "1.8.0_202" 的主版本为 1、次版本为 8，需要返回次版本 8 作为实际主版本。
// 3 秒超时防止 JVM 挂起时无限阻塞；探测失败返回 nil，不影响整体查找流程。
func tryGetJavaMajorVersion(javaExecutable string) *int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, javaExecutable, "-version")
	var output strings.Builder
	command.Stdout = &output
	command.Stderr = &output
	// java.exe 是控制台程序，探测时禁止闪现 cmd 窗口
	tools.HideProcessWindow(command)
	if err := command.Run(); err != nil {
		// java -version 通常输出到 stderr 且退出码可能非 0，但仍会打印版本；
		// 输出为空才算探测失败
		if output.Len() == 0 {
			return nil
		}
	}

	match := javaVersionPattern.FindStringSubmatch(output.String())
	if match == nil {
		return nil
	}
	major := parseLeadingInt(match[1])
	if major == nil {
		return nil
	}
	// 旧式版本号：如 1.8，主版本是 1，实际对应的 Java 主版本是次版本 8
	if *major == 1 {
		if minor := parseLeadingInt(match[2]); minor != nil {
			return minor
		}
	}
	return major
}

func parseLeadingInt(value string) *int {
	value = strings.TrimSpace(value)
	digits := make([]byte, 0, len(value))
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= '0' && character <= '9' {
			digits = append(digits, character)
			continue
		}
		break
	}
	if len(digits) == 0 {
		return nil
	}
	result := 0
	for _, digit := range digits {
		result = result*10 + int(digit-'0')
	}
	return &result
}

func distinctInts(values []int) []int {
	seen := map[int]bool{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
