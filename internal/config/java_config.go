package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// JavaConfig 实例 Java 配置（存储在实例目录/java_config.yaml）
type JavaConfig struct {
	// JavaExecutable Java 可执行文件路径（留空使用全局设置）
	JavaExecutable string `yaml:"javaExecutable,omitempty"`
	// MinMemoryMB 最小内存（-Xms，单位 MB）
	MinMemoryMB int `yaml:"minMemoryMB,omitempty"`
	// MaxMemoryMB 最大内存（-Xmx，单位 MB）
	MaxMemoryMB int `yaml:"maxMemoryMB,omitempty"`
	// AdditionalJvmArguments 附加 JVM 参数
	AdditionalJvmArguments []string `yaml:"additionalJvmArguments,omitempty"`
	// AdditionalGameArguments 附加游戏参数
	AdditionalGameArguments []string `yaml:"additionalGameArguments,omitempty"`
}

// LoadInstanceJavaConfig 加载实例的 Java 配置
func LoadInstanceJavaConfig(instanceDir string) (*JavaConfig, error) {
	configPath := filepath.Join(instanceDir, "java_config.yaml")

	// 文件不存在时返回空配置（使用全局设置）
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return &JavaConfig{}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config JavaConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// SaveInstanceJavaConfig 保存实例的 Java 配置
func SaveInstanceJavaConfig(instanceDir string, config *JavaConfig) error {
	configPath := filepath.Join(instanceDir, "java_config.yaml")

	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// DeleteInstanceJavaConfig 删除实例的 Java 配置（回退到全局设置）
func DeleteInstanceJavaConfig(instanceDir string) error {
	configPath := filepath.Join(instanceDir, "java_config.yaml")
	err := os.Remove(configPath)
	if os.IsNotExist(err) {
		return nil // 文件不存在，视为成功
	}
	return err
}
