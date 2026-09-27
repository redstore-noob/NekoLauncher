// 本文件已经过验证.
// 关于与服务器操作有关的函数.
package mcserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"nekolauncher/internal/launch"
)

// 删除服务器时调用，需保证Server状态为Stop.
func removeDirectory(dir string) error {
	return os.RemoveAll(dir)
}

// ============================================================================
// 以下内容合并自 export.go
// ============================================================================

// 绑定层使用的对外封装：列表合并运行态、删除服务器、进程命令助手。

// execCommand 便于测试替换的进程构造助手。
func execCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// ListServerInfos 全部服务器摘要（元数据 + 运行状态/在线数）。
func ListServerInfos() []ServerInfo {
	configs := LoadServers()
	result := make([]ServerInfo, 0, len(configs))
	for _, cfg := range configs {
		info := ServerInfo{
			ID:          cfg.ID,
			Name:        cfg.Name,
			Core:        cfg.Core,
			CoreVersion: cfg.CoreVersion,
			MCVersion:   cfg.MCVersion,
			Port:        cfg.Port,
			MaxPlayers:  cfg.MaxPlayers,
			Status:      StatusStopped,
		}
		if state := Default().state(cfg.ID); state != nil {
			state.mu.Lock()
			info.Status = state.status
			info.Players = state.players
			state.mu.Unlock()
		}
		result = append(result, info)
	}

	return result
}

// GetServerInfo 单个服务器摘要。
func GetServerInfo(id string) (*ServerInfo, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	cfg, err := loadServerConfig(serverDirectory(id))
	if err != nil {
		return nil, errors.New("服务器不存在")
	}
	info := &ServerInfo{
		ID:          cfg.ID,
		Name:        cfg.Name,
		Core:        cfg.Core,
		CoreVersion: cfg.CoreVersion,
		MCVersion:   cfg.MCVersion,
		Port:        cfg.Port,
		MaxPlayers:  cfg.MaxPlayers,
		Status:      StatusStopped,
	}
	if state := Default().state(cfg.ID); state != nil {
		state.mu.Lock()
		info.Status = state.status
		info.Players = state.players
		state.mu.Unlock()
	}

	return info, nil
}

// DeleteServer 删除服务器（必须已停止）。
func DeleteServer(id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	if Default().IsRunning(id) {
		return errors.New("服务器运行中，请先停止")
	}

	return removeDirectory(serverDirectory(id))
}

// ============================================================================
// 以下内容合并自 launchoptions.go
// ============================================================================

// JVM 启动参数的图形化编辑：读写 server.json 的内存与额外参数，
// 并在 StartServer 拼接命令行时生效。

// LaunchOptions 启动参数编辑页的读写结构。
type LaunchOptions struct {
	MemoryMB      int      // 最大堆 -Xmx，0 = 不设置
	MemoryMinMB   int      // 初始堆 -Xms，0 = 不设置
	ExtraJavaArgs []string // 额外 JVM 参数（不含内存与 -jar）
}

// GetLaunchOptions 读取服务器的 JVM 启动参数。
func GetLaunchOptions(id string) (*LaunchOptions, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	cfg, err := loadServerConfig(serverDirectory(id))
	if err != nil {
		return nil, errors.New("服务器不存在")
	}

	return &LaunchOptions{
		MemoryMB:      cfg.MemoryMB,
		MemoryMinMB:   cfg.MemoryMinMB,
		ExtraJavaArgs: cfg.ExtraJavaArgs,
	}, nil
}

// SaveLaunchOptions 校验并保存 JVM 启动参数（服务器运行中拒绝修改）。
func SaveLaunchOptions(id string, options LaunchOptions) error {
	if err := validateID(id); err != nil {
		return err
	}
	if Default().IsRunning(id) {
		return errors.New("服务器运行中，请先停止再修改启动参数")
	}
	cleaned := cleanExtraArgs(options.ExtraJavaArgs)
	if err := validateExtraArgs(cleaned); err != nil {
		return err
	}
	if options.MemoryMB < 0 || options.MemoryMB > 1024*1024 {
		return errors.New("最大内存取值非法（0 ~ 1048576 MB）")
	}
	if options.MemoryMinMB < 0 || options.MemoryMinMB > 1024*1024 {
		return errors.New("初始内存取值非法（0 ~ 1048576 MB）")
	}
	if options.MemoryMinMB > 0 && options.MemoryMB > 0 &&
		options.MemoryMinMB > options.MemoryMB {
		return errors.New("初始内存不能大于最大内存")
	}

	dir := serverDirectory(id)
	cfg, err := loadServerConfig(dir)
	if err != nil {
		return errors.New("服务器不存在")
	}
	cfg.MemoryMB = options.MemoryMB
	cfg.MemoryMinMB = options.MemoryMinMB
	cfg.ExtraJavaArgs = cleaned

	return saveServerConfig(dir, cfg)
}

// cleanExtraArgs 去空白、按空白拆分并丢弃空串（允许一行写多个参数）。
func cleanExtraArgs(args []string) []string {
	var result []string
	for _, arg := range args {
		for _, field := range strings.Fields(arg) {
			result = append(result, field)
		}
	}

	return result
}

// validateExtraArgs 拦截会改变启动语义的参数（-jar / @文件 / 主类名）。
func validateExtraArgs(args []string) error {
	for _, arg := range args {
		if arg == "-jar" || strings.HasPrefix(arg, "@") {
			return fmt.Errorf("额外参数不允许包含 %q（启动入口由启动器管理）", arg)
		}
	}

	return nil
}

// ============================================================================
// 以下内容合并自 java.go
// ============================================================================

// RequiredJavaMajorVersion 运行指定 Minecraft 版本所需的最低 Java 主版本。
// 兼容两代版本命名：
//   - 1.x：1.16 及以下 → 8；1.17 → 16；1.18–1.20.4 → 17；1.20.5 与 1.21+ → 21；
//   - 26.x 日期式新命名：26.1 起需要 Java 25（与 PaperMC 的要求一致），
//     之后只会更高，统一按 25 起步处理。
//
// 快照 / 预发布（如 "26.1-pre1"、"1.21.4-rc1"）按开头的数字主体解析；
// 无法识别的版本返回 nil，表示不校验（自动选择沿用通用优先级）。
func RequiredJavaMajorVersion(mcVersion string) *int {
	major, minor, patch := splitVersionComponents(mcVersion)
	if major < 0 {
		return nil
	}
	if major == 1 {
		switch {
		case minor >= 21:
			return intPtr(21)
		case minor == 20 && patch >= 5:
			return intPtr(21)
		case minor == 20:
			return intPtr(17)
		case minor >= 18:
			return intPtr(17)
		case minor == 17:
			return intPtr(16)
		default:
			return intPtr(8)
		}
	}
	if major >= 26 {
		return intPtr(25)
	}

	return nil
}

// resolveJavaForServer 为服务器进程选取 Java 可执行文件。
// 配置里显式指定的 Java 仍然优先（尊重用户选择），但整体按服务器的
// MC 版本要求做版本感知挑选：精确主版本 → 满足最低要求的最低版本，
// 全部缺失时自动下载（Temurin），避免用 PATH 里的老 Java 启动新版本
// 服务端后撞上 "requires running the server with Java 25 or above"。
// 下载等进度说明通过 log 输出（可为 nil），由调用方写进服务器控制台。
func resolveJavaForServer(ctx context.Context, cfg *ServerConfig, log func(string)) (string, error) {
	runtimeDirectory := os.Getenv("NEKOLAUNCHER_JAVA_RUNTIME")
	if strings.TrimSpace(runtimeDirectory) == "" {
		runtimeDirectory = filepath.Join(launch.GetDefaultMinecraftDirectory(), "runtime")
	}

	return launch.EnsureJavaRuntime(
		launch.DefaultJavaRuntimeLocator{},
		ctx,
		cfg.JavaPath,
		RequiredJavaMajorVersion(cfg.MCVersion),
		runtimeDirectory,
		log,
	)
}

// splitVersionComponents 解析版本号开头的三段数字（"1.21.4" → 1,21,4；
// "26.1" → 26,1,-1）。缺失的段返回 -1；完全无法解析时 major 为 -1。
func splitVersionComponents(version string) (major, minor, patch int) {
	major, minor, patch = -1, -1, -1
	fields := strings.FieldsFunc(version, func(r rune) bool {
		return r < '0' || r > '9'
	})
	values := []*int{&major, &minor, &patch}
	for index, field := range fields {
		if index >= len(values) {
			break
		}
		if parsed, err := strconv.Atoi(field); err == nil {
			*values[index] = parsed
		}
	}

	return major, minor, patch
}

func intPtr(value int) *int {
	return &value
}

// ============================================================================
// 以下内容合并自 properties.go
// ============================================================================

// server.properties 的读写：保持行序与注释，仅替换/追加键值。

// Property 单条配置。
type Property struct {
	Key   string
	Value string
}

// propertyLine 内部模型：注释行/空行/键值行按原顺序保留。
type propertyLine struct {
	comment string // 以 # 开头的原始行（含 #）
	key     string
	value   string
}

// loadPropertyLines 读取并解析 server.properties。
func loadPropertyLines(dir string) ([]propertyLine, error) {
	file, err := os.Open(dir + string(os.PathSeparator) + "server.properties")
	if err != nil {
		return nil, errors.New("server.properties 不存在（先启动一次服务器后重试）")
	}
	defer file.Close()

	var lines []propertyLine
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		switch {
		case text == "" || strings.HasPrefix(text, "#"):
			lines = append(lines, propertyLine{comment: scanner.Text()})
		default:
			key, value, found := strings.Cut(text, "=")
			if !found {
				lines = append(lines, propertyLine{comment: scanner.Text()})

				continue
			}
			lines = append(lines, propertyLine{key: strings.TrimSpace(key), value: value})
		}
	}

	return lines, scanner.Err()
}

// GetServerProperties 返回全部键值（按文件顺序）。
func GetServerProperties(id string) ([]Property, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	lines, err := loadPropertyLines(serverDirectory(id))
	if err != nil {
		return nil, err
	}
	result := make([]Property, 0, len(lines))
	for _, line := range lines {
		if line.key != "" {
			result = append(result, Property{Key: line.key, Value: line.value})
		}
	}

	return result, nil
}

// SetServerProperties 更新键值（保留注释与未知行；新键追加到文件末尾）。
func SetServerProperties(id string, properties []Property) error {
	if err := validateID(id); err != nil {
		return err
	}
	dir := serverDirectory(id)

	// 服务器运行中改配置不会生效，直接拒绝以免误导
	if Default().IsRunning(id) {
		return errors.New("服务器运行中，请先停止再修改配置")
	}

	lines, err := loadPropertyLines(dir)
	if err != nil {
		return err
	}
	updates := make(map[string]string, len(properties))
	for _, property := range properties {
		updates[property.Key] = property.Value
	}

	var builder strings.Builder
	seen := map[string]bool{}
	for _, line := range lines {
		switch {
		case line.key == "":
			builder.WriteString(line.comment + "\n")
		default:
			seen[line.key] = true
			if value, ok := updates[line.key]; ok {
				builder.WriteString(line.key + "=" + value + "\n")
			} else {
				builder.WriteString(line.key + "=" + line.value + "\n")
			}
		}
	}
	// 新增键追加到末尾
	for _, property := range properties {
		if !seen[property.Key] && property.Key != "" {
			builder.WriteString(property.Key + "=" + property.Value + "\n")
		}
	}

	return os.WriteFile(dir+string(os.PathSeparator)+"server.properties",
		[]byte(builder.String()), 0o644)
}
