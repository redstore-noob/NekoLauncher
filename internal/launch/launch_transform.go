package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

// MinecraftClasspathReplacement 精确替换原版 classpath 中的一个条目。
type MinecraftClasspathReplacement struct {
	ExistingPath    string
	ReplacementPath string
}

// MinecraftLaunchTransform 宿主在解析原版元数据之后、渲染 Java 命令之前施加的启动变换。
// 契约中的所有路径必须是绝对路径。
type MinecraftLaunchTransform struct {
	PrependClasspath []string
	AppendClasspath  []string
	// ReplaceClasspath 整体替换原版 classpath；nil 表示保留原版。
	ReplaceClasspath []string
	// ClasspathReplacements 对原版 classpath 中的某个条目做精确替换。
	ClasspathReplacements []MinecraftClasspathReplacement
	// RemoveClasspath 按路径精确比较移除条目。
	RemoveClasspath          []string
	MainClassOverride        string
	JavaExecutableOverride   string
	WorkingDirectoryOverride string
	// PrependJvmArguments 插入到所有启动器与版本 JVM 参数之前。
	PrependJvmArguments []string
	// AppendJvmArguments 插入到版本 JVM 参数之后、主类之前。
	AppendJvmArguments []string
	// PrependGameArguments 紧跟主类之后插入。
	PrependGameArguments []string
	// AppendGameArguments 插入到所有启动器与版本游戏参数之后。
	AppendGameArguments []string
	// EnvironmentVariables 值语义：普通值注入子进程；
	// 需要移除的变量加入 RemovedEnvironmentVariables（对应 C# 的 null 值）。
	EnvironmentVariables        map[string]string
	RemovedEnvironmentVariables []string
}

// resolvedMinecraftLaunchTransform 变换解析后的不可变启动快照。
type resolvedMinecraftLaunchTransform struct {
	MainClass                   string
	WorkingDirectory            string
	JavaExecutableOverride      string
	Classpath                   []string
	PrependJvmArguments         []string
	AppendJvmArguments          []string
	PrependGameArguments        []string
	AppendGameArguments         []string
	EnvironmentVariables        map[string]string
	RemovedEnvironmentVariables map[string]bool
}

// pathsEqual Windows 上路径与环境变量名不区分大小写。
func transformPathsEqual(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

// resolveMinecraftTransform 把声明式变换规范化为启动时快照。所有入口共用这里的
// 路径、冲突与空值处理规则。
func resolveMinecraftTransform(
	options MinecraftLaunchOptions,
	baseMainClass, gameDirectory string,
	baseClasspath []string,
) (*resolvedMinecraftLaunchTransform, error) {
	transform := options.Transform
	if transform == nil {
		transform = &MinecraftLaunchTransform{}
	}

	environment, removed, err := resolveEnvironmentVariables(transform)
	if err != nil {
		return nil, err
	}
	classpath, err := composeClasspath(baseClasspath, transform)
	if err != nil {
		return nil, err
	}
	prependJvm, err := copyArguments(transform.PrependJvmArguments)
	if err != nil {
		return nil, err
	}
	appendJvm, err := copyArguments(transform.AppendJvmArguments)
	if err != nil {
		return nil, err
	}
	prependGame, err := copyArguments(transform.PrependGameArguments)
	if err != nil {
		return nil, err
	}
	appendGame, err := copyArguments(transform.AppendGameArguments)
	if err != nil {
		return nil, err
	}
	mainClass, err := resolveMainClass(baseMainClass, transform.MainClassOverride)
	if err != nil {
		return nil, err
	}
	workingDirectory, err := resolveExistingDirectory(gameDirectory, transform.WorkingDirectoryOverride, "工作目录覆写")
	if err != nil {
		return nil, err
	}
	javaOverride, err := resolveExistingFile(transform.JavaExecutableOverride, "Java 可执行文件覆写")
	if err != nil {
		return nil, err
	}

	return &resolvedMinecraftLaunchTransform{
		MainClass:                   mainClass,
		WorkingDirectory:            workingDirectory,
		JavaExecutableOverride:      javaOverride,
		Classpath:                   classpath,
		PrependJvmArguments:         prependJvm,
		AppendJvmArguments:          appendJvm,
		PrependGameArguments:        prependGame,
		AppendGameArguments:         appendGame,
		EnvironmentVariables:        environment,
		RemovedEnvironmentVariables: removed,
	}, nil
}

// validateFinalArguments 最终 Java 命令参数不允许空串或包含 \0。
// 报错带上前后参数：空串本身没有内容，只说下标几乎无法定位（历史上这条报错就是这样
// 把一个"离线账号的 --clientId 解析成空值"的问题藏了很久）。
func validateFinalArguments(arguments []string) error {
	for index, argument := range arguments {
		if argument == "" {
			previous := ""
			if index > 0 {
				previous = arguments[index-1]
			}
			return newLaunchError(fmt.Sprintf(
				"最终 Java 命令出现空参数（下标 %d，前一项 %q）。", index, previous))
		}
		if strings.ContainsRune(argument, '\x00') {
			return newLaunchError(fmt.Sprintf(
				"最终 Java 命令在下标 %d 处包含非法字符 \\0。", index))
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Classpath：前插 + （替换/剔除后的）原版 + 后插，去重时保留首次出现
// ---------------------------------------------------------------------------

func composeClasspath(baseClasspath []string, transform *MinecraftLaunchTransform) ([]string, error) {
	baseSource := transform.ReplaceClasspath
	if baseSource == nil {
		baseSource = baseClasspath
	}
	normalizedBase, err := normalizeEach(baseSource, "解析后的 classpath")
	if err != nil {
		return nil, err
	}
	// baseEntries 保持首次出现顺序
	baseEntries := make([]string, 0, len(normalizedBase))
	seen := map[string]bool{}
	for _, entry := range normalizedBase {
		key := classpathKey(entry)
		if !seen[key] {
			seen[key] = true
			baseEntries = append(baseEntries, entry)
		}
	}

	replacements, err := collectReplacements(transform.ClasspathReplacements, baseEntries)
	if err != nil {
		return nil, err
	}
	removals, err := collectRemovals(transform.RemoveClasspath, baseEntries)
	if err != nil {
		return nil, err
	}

	for source := range replacements {
		if removals[source] {
			return nil, newLaunchError(fmt.Sprintf("同一条目不能同时被替换和移除：%s", source))
		}
	}

	prepend, err := normalizeEach(transform.PrependClasspath, "前插 classpath")
	if err != nil {
		return nil, err
	}
	result := append([]string{}, prepend...)
	for _, entry := range baseEntries {
		key := classpathKey(entry)
		if removals[key] {
			continue
		}
		if replacement, ok := replacements[key]; ok {
			result = append(result, replacement)
		} else {
			result = append(result, entry)
		}
	}
	appendEntries, err := normalizeEach(transform.AppendClasspath, "后插 classpath")
	if err != nil {
		return nil, err
	}
	result = append(result, appendEntries...)

	final := make([]string, 0, len(result))
	finalSeen := map[string]bool{}
	for _, entry := range result {
		key := classpathKey(entry)
		if !finalSeen[key] {
			finalSeen[key] = true
			final = append(final, entry)
		}
	}
	if len(final) == 0 {
		return nil, newLaunchError("最终 classpath 不能为空。")
	}
	return final, nil
}

func collectReplacements(
	replacements []MinecraftClasspathReplacement,
	baseEntries []string,
) (map[string]string, error) {
	result := map[string]string{}
	for _, replacement := range replacements {
		source, err := requireAbsolutePath(replacement.ExistingPath, "classpath 替换源")
		if err != nil {
			return nil, err
		}
		target, err := requireAbsolutePath(replacement.ReplacementPath, "classpath 替换目标")
		if err != nil {
			return nil, err
		}
		if !baseEntriesContain(baseEntries, source) {
			return nil, newLaunchError(fmt.Sprintf("classpath 替换源不存在：%s", source))
		}
		key := classpathKey(source)
		if previous, ok := result[key]; ok && !transformPathsEqual(previous, target) {
			return nil, newLaunchError(fmt.Sprintf("classpath 替换目标冲突：%s", source))
		}
		result[key] = target
	}
	return result, nil
}

func collectRemovals(removals []string, baseEntries []string) (map[string]bool, error) {
	result := map[string]bool{}
	for _, path := range removals {
		normalized, err := requireAbsolutePath(path, "classpath 移除")
		if err != nil {
			return nil, err
		}
		if !baseEntriesContain(baseEntries, normalized) {
			return nil, newLaunchError(fmt.Sprintf("classpath 移除源不存在：%s", normalized))
		}
		result[classpathKey(normalized)] = true
	}
	return result, nil
}

func baseEntriesContain(baseEntries []string, path string) bool {
	key := classpathKey(path)
	for _, entry := range baseEntries {
		if classpathKey(entry) == key {
			return true
		}
	}
	return false
}

// classpathKey classpath 条目的比较键（Windows 忽略大小写）。
func classpathKey(path string) string {
	cleaned := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

// ---------------------------------------------------------------------------
// 单值解析
// ---------------------------------------------------------------------------

func resolveMainClass(fallback, overrideValue string) (string, error) {
	mainClass := strings.TrimSpace(fallback)
	if overrideValue != "" {
		mainClass = strings.TrimSpace(overrideValue)
	}
	if !isValidJavaClassName(mainClass) {
		return "", newLaunchError(fmt.Sprintf("Minecraft 主类名非法：%s", mainClass))
	}
	return mainClass, nil
}

func isValidJavaClassName(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	atSegmentStart := true
	for _, character := range value {
		if character == '.' {
			if atSegmentStart {
				return false
			}
			atSegmentStart = true
			continue
		}
		if atSegmentStart {
			if !isJavaIdentifierStart(character) {
				return false
			}
			atSegmentStart = false
			continue
		}
		if !isJavaIdentifierPart(character) {
			return false
		}
	}
	return !atSegmentStart
}

func isJavaIdentifierStart(character rune) bool {
	return unicode.IsLetter(character) || character == '_' || character == '$'
}

func isJavaIdentifierPart(character rune) bool {
	return unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '$'
}

func resolveExistingFile(path, description string) (string, error) {
	if path == "" {
		return "", nil
	}
	fullPath, err := requireAbsolutePath(path, description)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(fullPath); statErr != nil || info.IsDir() {
		return "", newLaunchError(fmt.Sprintf("%s不存在：%s", description, fullPath))
	}
	return fullPath, nil
}

func resolveExistingDirectory(fallback, overrideValue, description string) (string, error) {
	if overrideValue == "" {
		return fallback, nil
	}
	fullPath, err := requireAbsolutePath(overrideValue, description)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(fullPath); statErr != nil || !info.IsDir() {
		return "", newLaunchError(fmt.Sprintf("%s不存在：%s", description, fullPath))
	}
	return fullPath, nil
}

// ---------------------------------------------------------------------------
// 路径与集合的公共校验
// ---------------------------------------------------------------------------

// requireAbsolutePath 展开环境变量、去除引号并要求绝对路径；文件或目录必须存在。
func requireAbsolutePath(path, description string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", newLaunchError(fmt.Sprintf("%s不能为空。", description))
	}
	trimmed := strings.Trim(strings.TrimSpace(path), `"`)
	expanded := os.ExpandEnv(trimmed)
	if !filepath.IsAbs(expanded) {
		return "", newLaunchError(fmt.Sprintf("%s必须是绝对路径：%s", description, path))
	}
	fullPath, err := filepath.Abs(expanded)
	if err != nil {
		return "", newLaunchError(fmt.Sprintf("%s非法：%s", description, path))
	}
	if _, statErr := os.Stat(fullPath); statErr != nil {
		return "", newLaunchError(fmt.Sprintf("%s不存在：%s", description, fullPath))
	}
	return fullPath, nil
}

func normalizeEach(paths []string, description string) ([]string, error) {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		normalized, err := requireAbsolutePath(path, description)
		if err != nil {
			return nil, err
		}
		result = append(result, normalized)
	}
	return result, nil
}

// copyArguments 拷贝参数列表并逐项校验（拒绝空串与空字符）。
// Go 的 nil 切片语义上就是"没有参数"（与 C# 的 null 不同）：
// 宿主不设置 Prepend/Append 列表时字段为 nil，这是正常路径，
// 不能报"启动参数列表不能为 null"，否则任何启动都会失败。
func copyArguments(arguments []string) ([]string, error) {
	copied := make([]string, 0, len(arguments))
	for index, argument := range arguments {
		if argument == "" || strings.ContainsRune(argument, '\x00') {
			return nil, newLaunchError(fmt.Sprintf("启动参数第 %d 项非法。", index))
		}
		copied = append(copied, argument)
	}
	return copied, nil
}

func resolveEnvironmentVariables(transform *MinecraftLaunchTransform) (map[string]string, map[string]bool, error) {
	result := map[string]string{}
	removed := map[string]bool{}
	for name, value := range transform.EnvironmentVariables {
		if strings.TrimSpace(name) == "" || strings.ContainsRune(name, '=') || strings.ContainsRune(name, '\x00') {
			return nil, nil, newLaunchError(fmt.Sprintf("环境变量名非法：%s", name))
		}
		if strings.ContainsRune(value, '\x00') {
			return nil, nil, newLaunchError(fmt.Sprintf("环境变量值包含空字符：%s", name))
		}
		key := name
		if runtime.GOOS == "windows" {
			key = strings.ToLower(name)
		}
		if previous, ok := result[key]; ok && previous != value {
			return nil, nil, newLaunchError(fmt.Sprintf("同名环境变量的取值冲突：%s", name))
		}
		result[key] = value
	}
	for _, name := range transform.RemovedEnvironmentVariables {
		if strings.TrimSpace(name) == "" || strings.ContainsRune(name, '=') || strings.ContainsRune(name, '\x00') {
			return nil, nil, newLaunchError(fmt.Sprintf("环境变量名非法：%s", name))
		}
		key := name
		if runtime.GOOS == "windows" {
			key = strings.ToLower(name)
		}
		removed[key] = true
	}
	return result, removed, nil
}
