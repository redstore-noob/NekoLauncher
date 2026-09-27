package bindings

// ConfigAPI：启动器配置（launcher.yaml / accounts.yaml）读写。对应 C#
// LauncherConfig / GameVersionProfileStore / GlobalLaunchSettingsStore 的公开面。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekolauncher/internal/config"
	"nekolauncher/internal/instance"
	"nekolauncher/internal/launch"
	"nekolauncher/internal/network"
)

// ---- 存储目录与基础路径 ----

// GetStorageDirectory 启动器存储目录。
func (a *ConfigAPI) GetStorageDirectory() string { return config.StorageDirectory() }

// IsPortableMode 当前是否处于便携模式（存储目录跟着程序走，X-6）。
func (a *ConfigAPI) IsPortableMode() bool {
	_, ok := config.PortableStorageDirectory()

	return ok
}

// SetStorageDirectory 设置存储目录（需在启动早期调用）。
func (a *ConfigAPI) SetStorageDirectory(storageDir string) error {
	return config.SetStorageDirectory(storageDir)
}

// ---- 游戏目录 ----

// GetGameDirectory 游戏目录；空串表示未设置。
func (a *ConfigAPI) GetGameDirectory() string { return config.GameDirectory() }

// SaveGameDirectory 保存游戏目录，并立刻按新目录重扫实例。
//
// 光写配置不重扫会让主页按新目录列出"已安装版本"，而启动用的还是旧目录的实例
// 快照（选中动作会被快照校验拒绝），点启动等于启动上一个目录的游戏。
func (a *ConfigAPI) SaveGameDirectory(path string) bool {
	if !config.SaveGameDirectory(path) {
		return false
	}
	a.refreshInstances()

	return true
}

// ClearGameDirectory 清除游戏目录配置（回到默认目录），同样立刻重扫。
func (a *ConfigAPI) ClearGameDirectory() {
	config.ClearGameDirectory()
	a.refreshInstances()
}

// refreshInstances 按当前配置的游戏目录重扫实例（异步，结果经 instance:changed 推送）。
func (a *ConfigAPI) refreshInstances() {
	go instance.Refresh(callCtx(a.ctx), instance.ResolveConfiguredSourcePath())
}

// ---- Java ----

// GetJavaExecutable 全局 Java 路径。
func (a *ConfigAPI) GetJavaExecutable() string { return config.JavaExecutable() }

// GetJavaVersion 全局 Java 版本。
func (a *ConfigAPI) GetJavaVersion() string { return config.JavaVersion() }

// SaveJava 保存全局 Java。
func (a *ConfigAPI) SaveJava(javaPath, javaVersion string) bool {
	return config.SaveJava(javaPath, javaVersion)
}

// GetJavaPaths Java 列表。
func (a *ConfigAPI) GetJavaPaths() []config.JavaPathItem { return config.GetJavaPaths() }

// AddJava 追加 Java 条目。
func (a *ConfigAPI) AddJava(javaPath, javaVersion string) bool {
	return config.AddJava(javaPath, javaVersion)
}

// RemoveJava 删除 Java 条目。
func (a *ConfigAPI) RemoveJava(javaPath string) bool { return config.RemoveJava(javaPath) }

// SetPrimaryJava 设为主 Java。
func (a *ConfigAPI) SetPrimaryJava(javaPath string) bool { return config.SetPrimaryJava(javaPath) }

// ---- Java 自动检索（X-5）----

// JavaScanResult 一次自动检索的结果。
type JavaScanResult struct {
	// Found 本机找到的全部 java 可执行文件（绝对路径，已去重）。
	Found []string `json:"Found"`
	// Added 本次新加入 Java 列表的路径。
	Added []string `json:"Added"`
	// Skipped 已在列表里、这次跳过的路径。
	Skipped []string `json:"Skipped"`
	// Versions 每个新加入条目探测到的主版本描述（与 Added 一一对应；探测失败为空串）。
	Versions []string `json:"Versions"`
}

// AutoDetectJava 自动检索本机 Java 并合并进 Java 列表。
//
// 只做"发现 + 合并"，不改主 Java、不删任何已有条目：用户自己配的路径哪怕探测不到版本
// 也留着（可能只是探测失败，比如 java 在别处）。返回 Added/Skipped 便于界面如实汇报。
func (a *ConfigAPI) AutoDetectJava() JavaScanResult {
	result := JavaScanResult{Found: []string{}, Added: []string{}, Skipped: []string{}, Versions: []string{}}

	runtimeDirectory := os.Getenv("NEKOLAUNCHER_JAVA_RUNTIME")
	if strings.TrimSpace(runtimeDirectory) == "" {
		runtimeDirectory = filepath.Join(launch.GetDefaultMinecraftDirectory(), "runtime")
	}

	found := launch.DefaultJavaRuntimeLocator{}.FindAllJavaExecutables(runtimeDirectory)
	result.Found = found

	existing := map[string]bool{}
	for _, item := range config.GetJavaPaths() {
		if key := strings.ToLower(strings.TrimSpace(item.JavaPath)); key != "" {
			existing[key] = true
		}
	}

	for _, path := range found {
		key := strings.ToLower(strings.TrimSpace(path))
		if key == "" || existing[key] {
			result.Skipped = append(result.Skipped, path)

			continue
		}
		description := ""
		if major := launch.TryDetectJavaMajorVersion(path); major != nil {
			description = fmt.Sprintf("Java %d", *major)
		}
		if config.AddJava(path, description) {
			existing[key] = true
			result.Added = append(result.Added, path)
			result.Versions = append(result.Versions, description)
		}
	}

	return result
}

// ---- 开关与通用键值 ----

// GetDefaultVersionIsolation 全局默认版本隔离开关。
func (a *ConfigAPI) GetDefaultVersionIsolation() bool { return config.DefaultVersionIsolation() }

// SaveDefaultVersionIsolation 保存全局默认版本隔离；nil 表示清除为推断默认。
func (a *ConfigAPI) SaveDefaultVersionIsolation(value *bool) {
	config.SaveDefaultVersionIsolation(value)
}

// GetVerifyFilesBeforeLaunch 启动前是否校验文件。
func (a *ConfigAPI) GetVerifyFilesBeforeLaunch() bool { return config.VerifyFilesBeforeLaunch() }

// SaveVerifyFilesBeforeLaunch 保存启动前校验开关。
func (a *ConfigAPI) SaveVerifyFilesBeforeLaunch(enabled bool) {
	config.SaveVerifyFilesBeforeLaunch(enabled)
}

// SetValue 写入任意配置键。
func (a *ConfigAPI) SetValue(key, value string) bool { return config.SetValue(key, value) }

// GetValue 读取任意配置键（不存在为空串）。
func (a *ConfigAPI) GetValue(key string) string { return config.GetValue(key) }

// ClearValue 删除配置键。
func (a *ConfigAPI) ClearValue(key string) bool { return config.ClearValue(key) }

// ---- 全局高级启动设置 ----

// LoadGlobalLaunchSettings 读取全局高级启动设置。
func (a *ConfigAPI) LoadGlobalLaunchSettings() config.GlobalLaunchSettings {
	return config.LoadGlobalLaunchSettings()
}

// SaveGlobalLaunchSettings 保存全局高级启动设置。
func (a *ConfigAPI) SaveGlobalLaunchSettings(settings config.GlobalLaunchSettings) bool {
	return config.SaveGlobalLaunchSettings(settings)
}

// SaveGlobalWindowSize 保存全局窗口尺寸（启动器窗口，记忆用）。
func (a *ConfigAPI) SaveGlobalWindowSize(width, height int) bool {
	return config.SaveGlobalWindowSize(width, height)
}

// ---- 实例档案（独立内存 / 窗口 / JVM 参数 / 图标偏好） ----

// GetVersionProfile 读取实例档案（无则返回默认值）。
func (a *ConfigAPI) GetVersionProfile(minecraftDirectory, versionID string) config.GameVersionProfile {
	return config.Get(minecraftDirectory, versionID)
}

// SaveVersionProfile 保存实例档案。
func (a *ConfigAPI) SaveVersionProfile(profile config.GameVersionProfile) bool {
	return config.Save(profile)
}

// GetProfileFolders 额外扫描的游戏目录列表。
func (a *ConfigAPI) GetProfileFolders() []string { return config.GetFolders() }

// AddProfileFolder 追加额外游戏目录。
func (a *ConfigAPI) AddProfileFolder(path string) bool { return config.AddFolder(path) }

// RemoveProfileFolder 移除额外游戏目录。
func (a *ConfigAPI) RemoveProfileFolder(path string) bool { return config.RemoveFolder(path) }

// MigrateRenamedVersion 版本重命名后迁移关联配置（实例档案 / 目录 / 选中记录）。
func (a *ConfigAPI) MigrateRenamedVersion(
	minecraftDirectory, oldVersionID, newVersionID, oldVersionDirectory, newVersionDirectory string,
) {
	config.MigrateRenamedVersion(minecraftDirectory, oldVersionID, newVersionID, oldVersionDirectory, newVersionDirectory)
}

// ---- 代理（网络） ----

// GetProxySettings 读取代理设置。
func (a *ConfigAPI) GetProxySettings() network.ProxySettings { return network.LoadProxySettings() }

// SaveProxySettings 保存代理设置并立即应用到全局默认 Transport。
func (a *ConfigAPI) SaveProxySettings(settings network.ProxySettings) error {
	if err := network.SaveProxySettings(settings); err != nil {
		return err
	}
	network.ApplyProxySettings()
	return nil
}

// TestProxy 用给定设置探测微软服务连通性（不改动当前生效的全局配置）。
func (a *ConfigAPI) TestProxy(settings network.ProxySettings) (string, error) {
	return network.TestProxy(settings)
}
