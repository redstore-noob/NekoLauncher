// Package mcserver Minecraft 开服管理：服务器实例的创建、进程控制、
// 配置（server.properties）读写、文件管理与控制台。
//
// 每个服务器是存储目录下的一个子目录（mc-servers/<id>/），元数据落在
// 目录内 server.json；运行期由 manager 持有 java 子进程并采集输出。
package mcserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nekolauncher/internal/config"
)

// 服务器核心类型（与前端 CORES 对应）。
const (
	CoreVanilla  = "vanilla"
	CorePaper    = "paper"
	CoreNeoForge = "neoforge"
	CoreFabric   = "fabric"
)

// 运行状态。
const (
	StatusStopped  = "stopped"
	StatusStarting = "starting"
	StatusRunning  = "running"
	StatusStopping = "stopping"
)

// ServerConfig 落盘的服务器元数据（server.json）。
type ServerConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Core        string `json:"core"`
	CoreVersion string `json:"coreVersion"`
	MCVersion   string `json:"mcVersion"`
	Port        int    `json:"port"`
	MaxPlayers  int    `json:"maxPlayers"`
	JavaPath    string `json:"javaPath"`
	MemoryMB    int    `json:"memoryMB"`
	// MemoryMinMB 初始堆（-Xms），0 表示不设置。
	MemoryMinMB int `json:"memoryMinMB"`
	// ExtraJavaArgs 额外 JVM 参数（图形化编辑，每条一个元素）。
	ExtraJavaArgs []string `json:"extraJavaArgs"`
	// AutoRestart 进程异常退出时自动重启（窗口内最多 3 次，见 manager.maybeAutoRestart）。
	AutoRestart bool `json:"autoRestart,omitempty"`
}

// ServerInfo 列表用摘要（合并运行时状态）。
type ServerInfo struct {
	ID          string
	Name        string
	Core        string
	CoreVersion string
	MCVersion   string
	Port        int
	MaxPlayers  int
	Status      string
	Players     int
}

// CreateOptions 创建参数。AcceptEULA 必须为 true 才会创建（写入 eula.txt）。
// CoreVersion 为服务端版本（Paper build id / Fabric loader / NeoForge 版本；
// 空表示用最新）。
type CreateOptions struct {
	Name        string
	Core        string
	MCVersion   string
	CoreVersion string
	Port        int
	MaxPlayers  int
	JavaPath    string
	AcceptEULA  bool
}

// serverRootOverride 测试用根目录覆盖（非 nil 时生效）。
var serverRootOverride *string

// ServerRootDirectory 所有服务器的根目录。
func ServerRootDirectory() string {
	if serverRootOverride != nil {
		return *serverRootOverride
	}

	return filepath.Join(config.StorageDirectory(), "mc-servers")
}

// serverDirectory 某个服务器的目录。
func serverDirectory(id string) string {
	return filepath.Join(ServerRootDirectory(), id)
}

// validateID 拦截路径越界（id 来自前端）。
func validateID(id string) error {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return errors.New("服务器 id 非法")
	}
	return nil
}

// sanitizeServerName 把显示名转换成合法目录名（非法字符替换为 _）。
func sanitizeServerName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, strings.TrimSpace(name))

	return strings.Trim(name, ". ")
}

// uniqueServerDirectory 在根目录下为名字找不冲突的目录（存在则追加 -N）。
func uniqueServerDirectory(name string) string {
	base := filepath.Join(ServerRootDirectory(), name)
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return name
	}
	for index := 1; ; index++ {
		candidate := fmt.Sprintf("%s-%d", name, index)
		if _, err := os.Stat(filepath.Join(ServerRootDirectory(), candidate)); os.IsNotExist(err) {
			return candidate
		}
	}
}

// loadServerConfig 读取 server.json。
func loadServerConfig(dir string) (*ServerConfig, error) {
	data, err := os.ReadFile(filepath.Join(dir, "server.json"))
	if err != nil {
		return nil, err
	}
	var cfg ServerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// saveServerConfig 写回 server.json。
func saveServerConfig(dir string, cfg *ServerConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "server.json"), data, 0o644)
}

// LoadServers 扫描全部服务器元数据（按名称排序）。
func LoadServers() []*ServerConfig {
	entries, err := os.ReadDir(ServerRootDirectory())
	if err != nil {
		return nil
	}
	var result []*ServerConfig
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cfg, err := loadServerConfig(filepath.Join(ServerRootDirectory(), entry.Name()))
		if err != nil {
			continue
		}
		result = append(result, cfg)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })

	return result
}

// AutoRestartEnabled 读取服务器的崩溃自动重启开关。
func AutoRestartEnabled(id string) bool {
	cfg, err := loadServerConfig(serverDirectory(id))
	if err != nil {
		return false
	}

	return cfg.AutoRestart
}

// SetAutoRestart 保存崩溃自动重启开关。
func SetAutoRestart(id string, enabled bool) error {
	if err := validateID(id); err != nil {
		return err
	}
	dir := serverDirectory(id)
	cfg, err := loadServerConfig(dir)
	if err != nil {
		return err
	}
	cfg.AutoRestart = enabled

	return saveServerConfig(dir, cfg)
}

// writeEula 写入 eula.txt（用户在创建流程中确认后才调用）。
func writeEula(dir string) error {
	content := fmt.Sprintf(
		"# Accepted via NekoLauncher at %s\neula=true\n",
		time.Now().Format(time.RFC3339),
	)

	return os.WriteFile(filepath.Join(dir, "eula.txt"), []byte(content), 0o644)
}

// requireEulaAccepted 启动前确认 eula.txt 仍是同意状态。
//
// 创建时写过一次并不代表它一直在：用户手工删掉、或从别处拷来一个没同意过的
// 服务器目录时，服务端会打印一行提示就退出，而前端只会一直停在"启动中"。
// 这里提前给出可操作的错误——刻意不自动补写：EULA 需要玩家本人同意，
// 启动器替用户"补签"与创建流程里强制勾选确认的设计相矛盾。
func requireEulaAccepted(dir string) error {
	path := filepath.Join(dir, "eula.txt")
	missing := false

	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.EqualFold(strings.TrimSpace(line), "eula=true") {
				return nil
			}
		}
	} else {
		missing = true
	}

	if missing {
		return errors.New("服务器目录里没有 eula.txt：请到「文件」页新建该文件并写入 eula=true" +
			"（Minecraft EULA 需你本人同意，启动器不会代为确认）")
	}

	return errors.New("eula.txt 里不是 eula=true：请到「文件」页改成 eula=true 后再启动" +
		"（Minecraft EULA 需你本人同意，启动器不会代为确认）")
}

// writeInitialProperties 写入最小 server.properties（其余键由服务端首次启动补全）。
func writeInitialProperties(dir string, port, maxPlayers int, motd string) error {
	content := fmt.Sprintf(
		"server-port=%d\nmax-players=%d\nmotd=%s\n",
		port, maxPlayers, sanitizePropertyValue(motd),
	)

	return os.WriteFile(filepath.Join(dir, "server.properties"), []byte(content), 0o644)
}

// sanitizePropertyValue 去掉会被 properties 解析器当成结构字符的控制字符：
// 换行/回车能在 motd 里注入额外的键（如 online-mode=false），NUL 会截断整行。
func sanitizePropertyValue(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == 0 {
			return -1
		}

		return r
	}, value)
}
