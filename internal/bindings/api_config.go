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
	"nekolauncher/internal/logs"
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

// ---- 游戏目录 ----

// GetGameDirectory 游戏目录；空串表示未设置。
func (a *ConfigAPI) GetGameDirectory() string { return config.GameDirectory() }

// SaveGameDirectory 保存游戏目录，并立刻按新目录重扫实例。
//
// 光写配置不重扫会让主页按新目录列出"已安装版本"，而启动用的还是旧目录的实例
// 快照（选中动作会被快照校验拒绝），点启动等于启动上一个目录的游戏。
//
// 保存动作本身允许任何存在的目录（空的 / 暂无版本的目录是合法状态），
// 但保存后若没发现任何版本，会推送 instance:emptyDirectory 事件提示用户。
func (a *ConfigAPI) SaveGameDirectory(path string) bool {
	if !config.SaveGameDirectory(path) {
		return false
	}
	a.refreshInstances()
	a.notifyDirectoryWithoutVersions(path)

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

// notifyDirectoryWithoutVersions 用户主动保存 / 添加了一个存在、但其中没有
// 任何 Minecraft 版本的目录时，推送 instance:emptyDirectory 事件（载荷为目录
// 路径），由前端给出温和提示。外部实例（MultiMC/PCL/HMCL 等布局）与版本读取
// 失败（权限 / 占用）不打扰——前者有实例只是没 versions，后者不该误报。
func (a *ConfigAPI) notifyDirectoryWithoutVersions(path string) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return
	}
	if _, ok := instance.TryResolveExternalInstance(trimmed); ok {
		return
	}
	ids, readable := instance.GetInstalledVersionIdsChecked(trimmed)
	if readable && len(ids) == 0 {
		emit(a.ctx, "instance:emptyDirectory", trimmed)
	}
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

// 账户域键（accounts / authlibClientToken 等）只允许 Go 侧（internal/auth）
// 读写：这些键背后是 accounts.yaml 的加密凭据。前端/插件的合法账号操作
// 全部走 AccountAPI；从 WebView 直呼这三个通用键值接口触碰账户域，
// 只可能是恶意插件在绕过权限门——直接拒绝并记 WARN。
// "secret:" 前缀同理：那是 SystemAPI.StoreSecret/ReadSecret 加密存储的命名
// 空间，通用键值接口只许绕过解密层拿到裸 blob，一律拒绝。
func guardAccountDomainKey(key string) bool {
	if !config.IsAccountDomainKey(key) && !isSecretStorageKey(key) {
		return true
	}
	logs.Write("WARN", "已拒绝来自界面层的受保护配置访问："+key)
	return false
}

// SetValue 写入任意配置键（账户域键被拒绝，见 guardAccountDomainKey）。
func (a *ConfigAPI) SetValue(key, value string) bool {
	if !guardAccountDomainKey(key) {
		return false
	}
	return config.SetValue(key, value)
}

// GetValue 读取任意配置键（不存在为空串；账户域键被拒绝，恒返回空串）。
func (a *ConfigAPI) GetValue(key string) string {
	if !guardAccountDomainKey(key) {
		return ""
	}
	return config.GetValue(key)
}

// ClearValue 删除配置键（账户域键被拒绝）。
func (a *ConfigAPI) ClearValue(key string) bool {
	if !guardAccountDomainKey(key) {
		return false
	}
	return config.ClearValue(key)
}

// ---- 全局高级启动设置 ----

// LoadGlobalLaunchSettings 读取全局高级启动设置。
func (a *ConfigAPI) LoadGlobalLaunchSettings() config.GlobalLaunchSettings {
	return config.LoadGlobalLaunchSettings()
}

// SaveGlobalLaunchSettings 保存全局高级启动设置。
func (a *ConfigAPI) SaveGlobalLaunchSettings(settings config.GlobalLaunchSettings) bool {
	return config.SaveGlobalLaunchSettings(settings)
}

// （不再暴露 SaveGlobalWindowSize：只有后端关窗钩子自己调 config 包，前端零调用。）

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

// AddProfileFolder 追加额外游戏目录。追加成功但目录下没有发现版本时，
// 同样推送 instance:emptyDirectory 事件提示用户。
func (a *ConfigAPI) AddProfileFolder(path string) bool {
	if !config.AddFolder(path) {
		return false
	}
	a.notifyDirectoryWithoutVersions(path)

	return true
}

// RemoveProfileFolder 移除额外游戏目录。
func (a *ConfigAPI) RemoveProfileFolder(path string) bool { return config.RemoveFolder(path) }

// （不再暴露 MigrateRenamedVersion：实例重命名服务在 Go 侧直调 config 包，前端零调用。）

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
