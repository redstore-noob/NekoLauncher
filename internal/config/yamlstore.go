package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// yamlFileManager 管理单个 YAML 配置文档（内存中为 map[string]any）。
// 语义与 ConfigFileManager 保持一致：原子落盘、损坏时备份后重建、
// 瞬时 IO 失败不覆盖原文件。用于从 launcher.yaml 拆分出的账户域与个性化域。
type yamlFileManager struct {
	mu       sync.Mutex
	filePath string
	doc      map[string]any
}

// newYamlFileManager 构造时立即加载目标配置文件；文件不存在时创建空文档。
func newYamlFileManager(filePath string) (*yamlFileManager, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, errors.New("filePath 不能为空")
	}
	m := &yamlFileManager{filePath: filePath}
	doc, err := m.load()
	if err != nil {
		return nil, err
	}
	m.doc = doc
	return m, nil
}

// FilePath 配置文件路径。
func (m *yamlFileManager) FilePath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.filePath
}

// Get 读取字符串配置项；不存在或类型不匹配时返回空串。
func (m *yamlFileManager) Get(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, err := readString(m.doc[key])
	if err != nil {
		logsWriteError(fmt.Sprintf("读取 YAML 配置项失败: %v", err))
		return ""
	}
	return value
}

// Set 添加或更新字符串配置项。
func (m *yamlFileManager) Set(key, value string) bool {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return false
	}
	return m.update(func(doc map[string]any) bool {
		doc[key] = value
		return true
	}, "写入 YAML 配置项")
}

// Remove 删除配置项；不存在时返回 false。
func (m *yamlFileManager) Remove(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	return m.update(func(doc map[string]any) bool {
		if _, ok := doc[key]; !ok {
			return false
		}
		delete(doc, key)
		return true
	}, "删除 YAML 配置项")
}

func (m *yamlFileManager) update(mutation func(map[string]any) bool, operationName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := deepCloneMap(m.doc)
	defer func() {
		if r := recover(); r != nil {
			m.doc = previous
			logsWriteError(fmt.Sprintf("%s失败: %v", operationName, r))
		}
	}()
	if !mutation(m.doc) {
		return false
	}
	if m.save(m.doc) {
		return true
	}
	m.doc = previous
	return false
}

func (m *yamlFileManager) load() (map[string]any, error) {
	filePath := m.filePath
	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 文件不存在：创建空文档
			defaultDoc := map[string]any{}
			if !m.save(defaultDoc) {
				return nil, fmt.Errorf("无法创建配置文件：%s", absolutePath(filePath))
			}
			return defaultDoc, nil
		}
		// 文件被占用、磁盘抖动等瞬时 IO 失败：不覆盖原文件，向上抛出
		return nil, fmt.Errorf("读取配置文件失败：%s（%w）", filePath, err)
	}

	var root map[string]any
	if len(strings.TrimSpace(string(data))) == 0 {
		root = map[string]any{}
	} else if err := yaml.Unmarshal(data, &root); err != nil {
		// 仅"内容损坏"才走备份+重建：瞬时 IO 失败绝不能用空文档覆盖原文件
		logsWriteError(fmt.Sprintf("配置文件损坏: %v，已备份并重建默认配置", err))
		backupPath := filePath + fmt.Sprintf(".corrupted-%s.bak", time.Now().Format("20060102150405"))
		if copyErr := copyFile(filePath, backupPath); copyErr != nil {
			return nil, fmt.Errorf("配置文件损坏且无法备份：%s（%v）", filePath, copyErr)
		}
		logsWriteError("已备份损坏的配置文件到: " + backupPath)

		rebuiltDoc := map[string]any{}
		if !m.save(rebuiltDoc) {
			return nil, fmt.Errorf("无法重建配置文件：%s", absolutePath(filePath))
		}
		return rebuiltDoc, nil
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

func (m *yamlFileManager) save(doc map[string]any) bool {
	temporaryPath := ""
	defer func() {
		if temporaryPath != "" {
			// 临时文件清理失败不应掩盖原始保存结果。
			_ = os.Remove(temporaryPath)
		}
	}()
	data, err := yaml.Marshal(doc)
	if err != nil {
		logsWriteError(fmt.Sprintf("保存配置文件失败: %v", err))
		return false
	}

	fullPath := absolutePath(m.filePath)
	directory := filepath.Dir(fullPath)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		logsWriteError(fmt.Sprintf("保存配置文件失败: %v", err))
		return false
	}

	// 临时文件写入同一目录，确保 rename 是同一文件系统内的原子操作
	temporaryPath = filepath.Join(directory,
		fmt.Sprintf(".%s.%s.tmp", filepath.Base(fullPath), newGUID()))
	if err := os.WriteFile(temporaryPath, data, 0o644); err != nil {
		logsWriteError(fmt.Sprintf("保存配置文件失败: %v", err))
		return false
	}
	// os.Rename 在 Windows 上同样以原子替换方式覆盖已存在文件
	if err := os.Rename(temporaryPath, fullPath); err != nil {
		logsWriteError(fmt.Sprintf("保存配置文件失败: %v", err))
		return false
	}
	temporaryPath = ""
	return true
}
