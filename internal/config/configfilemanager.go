// Package config 启动器配置、全局启动设置与实例档案的读写。
//
// 存储格式：启动器自身的配置一律存 YAML（launcher.yaml / accounts.yaml）。
// 只有 Minecraft 本体或跨启动器互操作要求的文件（版本 JSON、整合包 mrpack /
// mmc-pack.json 等）才保持 JSON——那些格式由外部规范定义，不由本项目决定。
package config

import (
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"nekolauncher/internal/logs"
)

// JavaPathItem 一条已保存的 Java 路径（对应 C# ConfigFileManager.JavaPathItem，
// 仅内存使用，配置文件中以 {"path","version"} 对象数组存储）。
type JavaPathItem struct {
	JavaPath    string
	JavaVersion string
}

const (
	javaPathKey      = "javaPath"
	minecraftPathKey = "minecraftPath"
)

// ConfigFileManager 管理启动器的配置文档（YAML），同时保留旧版按键与 Java 路径 API。
// 配置文档在内存中为 map[string]any，写入时由 yaml.v3 格式化并原子落盘。
type ConfigFileManager struct {
	mu       sync.Mutex
	filePath string
	config   map[string]any
}

// NewConfigFileManager 构造时立即加载目标配置文件。
func NewConfigFileManager(filePath string) (*ConfigFileManager, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, errors.New("filePath 不能为空")
	}
	m := &ConfigFileManager{filePath: filePath}
	doc, err := m.loadConfig()
	if err != nil {
		return nil, err
	}
	m.config = doc
	return m, nil
}

// FilePath 配置文件路径。
func (m *ConfigFileManager) FilePath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.filePath
}

// ConfigItemAdd 添加或更新字符串配置项。
func (m *ConfigFileManager) ConfigItemAdd(key, value string) bool {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return false
	}
	return m.update(func(config map[string]any) bool {
		config[key] = value
		return true
	}, "添加配置项")
}

// ConfigItemRead 读取字符串配置项；不存在或类型不匹配时返回空串。
func (m *ConfigFileManager) ConfigItemRead(key string) string {
	if strings.TrimSpace(key) == "" {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	value, err := readString(m.config[key])
	if err != nil {
		logs.Write("ERROR", fmt.Sprintf("读取配置项失败: %v", err))
		return ""
	}
	return value
}

// ConfigItemDelete 删除配置项。
func (m *ConfigFileManager) ConfigItemDelete(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	return m.update(func(config map[string]any) bool {
		if _, ok := config[key]; !ok {
			return false
		}
		delete(config, key)
		return true
	}, "删除配置项")
}

// JavaPathGet 获取已保存的 Java 路径。
func (m *ConfigFileManager) JavaPathGet() []JavaPathItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, ok := m.config[javaPathKey].([]any)
	if !ok {
		return []JavaPathItem{}
	}
	result := make([]JavaPathItem, 0, len(entries))
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := entry["path"]; !ok {
			continue
		}
		if _, ok := entry["version"]; !ok {
			continue
		}
		path, _ := readString(entry["path"])
		version, _ := readString(entry["version"])
		result = append(result, JavaPathItem{JavaPath: path, JavaVersion: version})
	}
	return result
}

// JavaPathSet 用唯一的首选项替换 Java 路径列表。整个替换只进行一次原子写入。
func (m *ConfigFileManager) JavaPathSet(javaPath, javaVersion string) bool {
	if strings.TrimSpace(javaPath) == "" || strings.TrimSpace(javaVersion) == "" {
		return false
	}
	return m.update(func(config map[string]any) bool {
		config[javaPathKey] = []any{map[string]any{
			"path":    javaPath,
			"version": javaVersion,
		}}
		return true
	}, "保存 Java 路径")
}

// JavaPathAdd 追加一条 Java 路径。路径已存在时更新其版本（不重复添加），返回 true。
func (m *ConfigFileManager) JavaPathAdd(javaPath, javaVersion string) bool {
	if strings.TrimSpace(javaPath) == "" {
		return false
	}
	return m.update(func(config map[string]any) bool {
		entries := getOrCreateJavaEntries(config)
		for _, raw := range entries {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			existing, err := readString(entry["path"])
			if err == nil && strings.EqualFold(existing, javaPath) {
				if strings.TrimSpace(javaVersion) != "" {
					entry["version"] = javaVersion
				}
				return true
			}
		}
		entries = append(entries, map[string]any{
			"path":    javaPath,
			"version": javaVersion,
		})
		config[javaPathKey] = entries
		return true
	}, "添加 Java 路径")
}

// JavaPathRemove 移除指定路径的 Java 条目；不存在时返回 false。
func (m *ConfigFileManager) JavaPathRemove(javaPath string) bool {
	if strings.TrimSpace(javaPath) == "" {
		return false
	}
	return m.update(func(config map[string]any) bool {
		entries := getOrCreateJavaEntries(config)
		removed := false
		kept := entries[:0]
		for _, raw := range entries {
			// 跳过损坏条目（JSON null / 非对象）：一条脏数据不能让整个 Java 管理失效
			entry, ok := raw.(map[string]any)
			if !ok {
				kept = append(kept, raw)
				continue
			}
			existing, err := readString(entry["path"])
			if err == nil && strings.EqualFold(existing, javaPath) {
				removed = true
				continue
			}
			kept = append(kept, raw)
		}
		config[javaPathKey] = kept
		return removed
	}, "移除 Java 路径")
}

// JavaPathSetPrimary 把指定路径移到列表首位（成为默认 Java）；路径不存在时返回 false。
func (m *ConfigFileManager) JavaPathSetPrimary(javaPath string) bool {
	if strings.TrimSpace(javaPath) == "" {
		return false
	}
	return m.update(func(config map[string]any) bool {
		entries := getOrCreateJavaEntries(config)
		for i, raw := range entries {
			// 跳过损坏条目（JSON null / 非对象）
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			existing, err := readString(entry["path"])
			if err == nil && strings.EqualFold(existing, javaPath) {
				if i == 0 {
					return true
				}
				entries = append(entries[:i], entries[i+1:]...)
				entries = append([]any{entry}, entries...)
				config[javaPathKey] = entries
				return true
			}
		}
		return false
	}, "设为默认 Java")
}

// getOrCreateJavaEntries 取出（或创建）javaPath 数组，需持锁调用。
func getOrCreateJavaEntries(config map[string]any) []any {
	if entries, ok := config[javaPathKey].([]any); ok {
		return entries
	}
	created := []any{}
	config[javaPathKey] = created
	return created
}

// MinecraftPathGet 获取 Minecraft 游戏路径；未配置时返回空字符串。
func (m *ConfigFileManager) MinecraftPathGet() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, err := readString(m.config[minecraftPathKey])
	if err != nil {
		logs.Write("ERROR", fmt.Sprintf("读取 Minecraft 路径失败: %v", err))
		return ""
	}
	return value
}

// MinecraftPathSet 设置 Minecraft 游戏路径。
func (m *ConfigFileManager) MinecraftPathSet(minecraftPath string) bool {
	if strings.TrimSpace(minecraftPath) == "" {
		return false
	}
	return m.ConfigItemAdd(minecraftPathKey, minecraftPath)
}

// loadConfig 加载当前路径的配置文档（加锁版本供构造/SetFilePath 之外的内部使用）。
func (m *ConfigFileManager) loadConfig() (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadConfigLocked()
}

func (m *ConfigFileManager) loadConfigLocked() (map[string]any, error) {
	filePath := m.filePath
	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 文件不存在：创建默认配置
			defaultConfig := createDefaultConfig()
			if !m.saveConfig(defaultConfig) {
				return nil, fmt.Errorf("无法创建配置文件：%s", absolutePath(filePath))
			}
			return defaultConfig, nil
		}
		// 文件被占用、磁盘抖动等瞬时 IO 失败：不覆盖原文件，向上抛出
		return nil, fmt.Errorf("读取配置文件失败：%s（%w）", filePath, err)
	}

	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		// 仅"内容损坏"才走备份+重建：瞬时 IO 失败绝不能用默认配置覆盖原文件
		logs.Write("ERROR", fmt.Sprintf("配置文件损坏: %v，已备份并重建默认配置", err))
		backupPath := filePath + fmt.Sprintf(".corrupted-%s.bak", time.Now().Format("20060102150405"))
		if copyErr := copyFile(filePath, backupPath); copyErr != nil {
			// 备份失败：为避免数据彻底丢失，抛出异常让调用方处理，而非覆盖原文件
			return nil, fmt.Errorf("配置文件损坏且无法备份：%s（%v）", filePath, copyErr)
		}
		logs.Write("INFO", "已备份损坏的配置文件到: "+backupPath)

		rebuiltConfig := createDefaultConfig()
		if !m.saveConfig(rebuiltConfig) {
			return nil, fmt.Errorf("无法重建配置文件：%s", absolutePath(filePath))
		}
		return rebuiltConfig, nil
	}
	if root == nil {
		return nil, errors.New("配置文件根节点必须是 YAML 映射")
	}
	return root, nil
}

// UpdateInTransaction 在单个原子写入中应用多项配置修改：内存深拷贝 → 变更 → 一次性落盘，
// 失败时整体回滚内存态。回调内不要调用本类的其他修改方法（会造成多次落盘）。
func (m *ConfigFileManager) UpdateInTransaction(mutation func(map[string]any) bool) bool {
	return m.update(mutation, "批量更新配置")
}

func (m *ConfigFileManager) update(mutation func(map[string]any) bool, operationName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := deepCloneMap(m.config)
	defer func() {
		if r := recover(); r != nil {
			m.config = previous
			logs.Write("ERROR", fmt.Sprintf("%s失败: %v", operationName, r))
		}
	}()
	if !mutation(m.config) {
		return false
	}
	if m.saveConfig(m.config) {
		return true
	}
	m.config = previous
	return false
}

func (m *ConfigFileManager) saveConfig(config map[string]any) bool {
	temporaryPath := ""
	defer func() {
		if temporaryPath != "" {
			// 临时文件清理失败不应掩盖原始保存结果。
			_ = os.Remove(temporaryPath)
		}
	}()
	data, err := yaml.Marshal(config)
	if err != nil {
		logs.Write("ERROR", fmt.Sprintf("保存配置文件失败: %v", err))
		return false
	}

	fullPath := absolutePath(m.filePath)
	directory := filepath.Dir(fullPath)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		logs.Write("ERROR", fmt.Sprintf("保存配置文件失败: %v", err))
		return false
	}

	// 临时文件写入同一目录，确保 rename 是同一文件系统内的原子操作
	temporaryPath = filepath.Join(directory,
		fmt.Sprintf(".%s.%s.tmp", filepath.Base(fullPath), newGUID()))
	if err := os.WriteFile(temporaryPath, data, 0o644); err != nil {
		logs.Write("ERROR", fmt.Sprintf("保存配置文件失败: %v", err))
		return false
	}
	// os.Rename 在 Windows 上同样以原子替换方式覆盖已存在文件
	if err := os.Rename(temporaryPath, fullPath); err != nil {
		logs.Write("ERROR", fmt.Sprintf("保存配置文件失败: %v", err))
		return false
	}
	temporaryPath = ""
	return true
}

// createDefaultConfig 默认配置文档。
func createDefaultConfig() map[string]any {
	return map[string]any{
		javaPathKey:      []any{},
		minecraftPathKey: "",
	}
}

// readString 断言配置值为字符串类型；键不存在或值为 null 时返回空串
// （对应 C# ReadString 返回 null，静默处理）；类型不匹配返回错误。
func readString(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	result, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("值不是字符串类型")
	}
	return result, nil
}

// deepCloneMap 深拷贝配置文档，用于写入失败时回滚内存态。
// 用 JSON 而非 YAML：这里只处理内存中的 map[string]any（值类型限于
// string / bool / 数字 / []any / map[string]any），JSON 往返最快且无歧义；
// 落盘格式由 saveConfig 决定，两者互不影响。
func deepCloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	data, err := json.Marshal(source)
	if err != nil {
		return nil
	}
	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil
	}
	return cloned
}

func absolutePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// newGUID 生成无连字符的随机 GUID（临时文件名用）。
func newGUID() string {
	const hexDigits = "0123456789abcdef"
	b := make([]byte, 32)
	if _, err := crand.Read(b); err != nil {
		// 回退：基于时间的伪随机即可（仅用于文件名唯一性）
		now := time.Now().UnixNano()
		for i := range b {
			now = now*6364136223846793005 + 1442695040888963407
			b[i] = byte(now >> 33)
		}
	}
	for i := range b {
		b[i] = hexDigits[int(b[i])%16]
	}
	return string(b)
}

// copyFile 复制文件内容（损坏配置备份用）。
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// isWindows 平台判断（路径比较语义用）。
func isWindows() bool { return runtime.GOOS == "windows" }
