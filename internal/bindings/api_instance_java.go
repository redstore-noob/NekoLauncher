package bindings

import (
	"path/filepath"

	"nekolauncher/internal/config"
	"nekolauncher/internal/instance"
)

// GetInstanceJavaConfig 获取实例的 Java 配置
func (a *InstanceAPI) GetInstanceJavaConfig(versionId string) (*config.JavaConfig, error) {
	snapshot := instance.CurrentSnapshot()
	instanceDir := filepath.Join(snapshot.MinecraftDirectory, "versions", versionId)

	return config.LoadInstanceJavaConfig(instanceDir)
}

// SaveInstanceJavaConfig 保存实例的 Java 配置
func (a *InstanceAPI) SaveInstanceJavaConfig(versionId string, javaConfig *config.JavaConfig) error {
	snapshot := instance.CurrentSnapshot()
	instanceDir := filepath.Join(snapshot.MinecraftDirectory, "versions", versionId)

	return config.SaveInstanceJavaConfig(instanceDir, javaConfig)
}

// DeleteInstanceJavaConfig 删除实例的 Java 配置（恢复使用全局设置）
func (a *InstanceAPI) DeleteInstanceJavaConfig(versionId string) error {
	snapshot := instance.CurrentSnapshot()
	instanceDir := filepath.Join(snapshot.MinecraftDirectory, "versions", versionId)

	return config.DeleteInstanceJavaConfig(instanceDir)
}
