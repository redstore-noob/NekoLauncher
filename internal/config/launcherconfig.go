package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"nekolauncher/internal/tools"
)

// LauncherConfig 启动器配置的统一入口（C# 静态单例类 → Go 包级变量 + 互斥锁）。
// 底层由 ConfigFileManager 负责 YAML 读写。配置按域拆分存储（见 storagedomains.go）：
// 账户域写入 accounts.yaml，其余全部键（启动器外观/行为 + 游戏启动设置）写入
// launcher.yaml。默认目录为 %USERPROFILE%\NekoLauncher，可通过 SetStorageDirectory 切换。

var (
	configSyncRoot   sync.Mutex
	storageDirectory = defaultStorageDirectoryValue()
	sharedStore      *ConfigFileManager
)

// DefaultStorageDirectory 默认存储目录：便携模式下是 exe 同级的便携数据目录，
// 否则是 %USERPROFILE%\NekoLauncher。
func DefaultStorageDirectory() string { return defaultStorageDirectoryValue() }

// 便携模式（X-6）：
//
//	exe 同级存在 portable.flag（标记文件）或 NekoLauncher-data/（数据目录）时，
//	存储目录改用后者——U 盘/绿色版用户希望"数据跟着程序走"，而不是写进用户目录。
//
// 判定放在**默认值计算**里，因此任何读取存储目录的路径（含测试里的 SetStorageDirectory
// 还原）都会看到一致的结果；检测失败一律回落用户目录，绝不因为便携模式判断出错而拒绝启动。
// 判定实现与日志目录共用 tools.PortableDataDirectory（logs 不能 import config，
// 所以公共逻辑只能放更下层的 tools）。
func defaultStorageDirectoryValue() string {
	if portable, ok := PortableStorageDirectory(); ok {
		return portable
	}
	home := UserHome()
	if home == "" {
		return filepath.Join(".", "NekoLauncher")
	}
	return filepath.Join(home, "NekoLauncher")
}

// PortableStorageDirectory 便携模式下的存储目录；非便携模式返回 ok=false。
func PortableStorageDirectory() (string, bool) {
	return tools.PortableDataDirectory()
}

// UserHome 用户主目录（%USERPROFILE%，不可用时回落 os.UserHomeDir）。
func UserHome() string {
	if home := os.Getenv("USERPROFILE"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// StorageDirectory 配置文件所在目录。
func StorageDirectory() string {
	configSyncRoot.Lock()
	defer configSyncRoot.Unlock()
	return storageDirectory
}

// FilePath 主配置文件路径：存储目录下的 launcher.yaml。
func FilePath() string {
	configSyncRoot.Lock()
	defer configSyncRoot.Unlock()
	return launcherConfigFilePath()
}

// SetStorageDirectory 切换配置的读取目录。本方法只重置底层存储，
// 使后续读取立即应用新目录中的配置（不做任何文件搬迁）。
func SetStorageDirectory(storageDir string) error {
	if strings.TrimSpace(storageDir) == "" {
		return errors.New("storageDirectory 不能为空")
	}
	normalized := normalizeDirectory(storageDir)
	configSyncRoot.Lock()
	defer configSyncRoot.Unlock()
	if PathsEqualNormalized(normalized, storageDirectory) {
		return nil
	}
	storageDirectory = normalized
	sharedStore = nil   // 下次访问时用新路径重新加载
	resetDomainStores() // 账户域存储同样用新路径重新加载
	return nil
}

// GameDirectory 游戏根目录（.minecraft 或自定义目录）；未配置时返回空串。
func GameDirectory() string {
	return withStoreString(func(store *ConfigFileManager) string {
		value := store.MinecraftPathGet()
		if strings.TrimSpace(value) == "" {
			return ""
		}
		return value
	})
}

// JavaExecutable 首选 Java 可执行文件（java.exe）路径；未配置时返回空串。
func JavaExecutable() string {
	return withStoreString(func(store *ConfigFileManager) string {
		items := store.JavaPathGet()
		if len(items) == 0 {
			return ""
		}
		item := items[0]
		if strings.TrimSpace(item.JavaPath) == "" {
			return ""
		}
		return item.JavaPath
	})
}

// JavaVersion 首选 Java 的版本号（如 21）；未配置时返回空串。
func JavaVersion() string {
	return withStoreString(func(store *ConfigFileManager) string {
		items := store.JavaPathGet()
		if len(items) == 0 {
			return ""
		}
		item := items[0]
		if strings.TrimSpace(item.JavaVersion) == "" {
			return ""
		}
		return item.JavaVersion
	})
}

// SaveGameDirectory 保存游戏目录。
func SaveGameDirectory(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	return withStoreBool(func(store *ConfigFileManager) bool {
		return store.MinecraftPathSet(strings.TrimSpace(path))
	})
}

// ClearGameDirectory 清除已保存的游戏目录（恢复自动检测）。
func ClearGameDirectory() {
	withStoreBool(func(store *ConfigFileManager) bool {
		store.ConfigItemDelete("minecraftPath")
		return true
	})
}

// SaveJava 保存首选 Java（java.exe 路径 + 版本）。采用「先清空再写入」策略，
// 保证 launcher.yaml 中的 javaPath 始终只有一条首选配置。
func SaveJava(javaPath, javaVersion string) bool {
	if strings.TrimSpace(javaPath) == "" {
		return false
	}
	if strings.TrimSpace(javaVersion) == "" {
		javaVersion = "unknown"
	}
	return withStoreBool(func(store *ConfigFileManager) bool {
		return store.JavaPathSet(strings.TrimSpace(javaPath), strings.TrimSpace(javaVersion))
	})
}

// GetJavaPaths 已保存的全部 Java 路径（列表首位为默认 Java）。
func GetJavaPaths() []JavaPathItem {
	configSyncRoot.Lock()
	defer configSyncRoot.Unlock()
	store := ensureStore()
	if store == nil {
		return []JavaPathItem{}
	}
	return store.JavaPathGet()
}

// AddJava 添加一条 Java 路径；路径已存在时更新其版本。返回是否成功。
func AddJava(javaPath, javaVersion string) bool {
	if strings.TrimSpace(javaPath) == "" {
		return false
	}
	if strings.TrimSpace(javaVersion) == "" {
		javaVersion = "unknown"
	}
	return withStoreBool(func(store *ConfigFileManager) bool {
		return store.JavaPathAdd(strings.TrimSpace(javaPath), strings.TrimSpace(javaVersion))
	})
}

// RemoveJava 移除一条 Java 路径。返回是否移除成功。
func RemoveJava(javaPath string) bool {
	if strings.TrimSpace(javaPath) == "" {
		return false
	}
	return withStoreBool(func(store *ConfigFileManager) bool {
		return store.JavaPathRemove(strings.TrimSpace(javaPath))
	})
}

// SetPrimaryJava 把指定路径设为默认 Java（列表首位）。返回是否成功。
func SetPrimaryJava(javaPath string) bool {
	if strings.TrimSpace(javaPath) == "" {
		return false
	}
	return withStoreBool(func(store *ConfigFileManager) bool {
		return store.JavaPathSetPrimary(strings.TrimSpace(javaPath))
	})
}

// DefaultVersionIsolation 全局默认版本隔离设置。true = 新实例默认隔离
// （mods/config/saves 各实例独立），false = 默认共享 .minecraft 根目录。
// 未配置时默认 true（启用隔离，更安全、实例互不污染）。
// 仅在版本自身的 IsVersionIsolationEnabled 为 null 时生效。
func DefaultVersionIsolation() bool {
	value := GetValue("defaultVersionIsolation")
	// 缺省启用隔离：避免不同实例的 mods/saves/config 互相污染
	result, err := parseBool(value)
	if err != nil {
		return true
	}
	return result
}

// SaveDefaultVersionIsolation 保存全局默认版本隔离设置；value 为 nil 时删除配置项。
func SaveDefaultVersionIsolation(value *bool) {
	if value != nil {
		setValue("defaultVersionIsolation", formatBool(*value))
		return
	}
	withStoreBool(func(store *ConfigFileManager) bool {
		store.ConfigItemDelete("defaultVersionIsolation")
		return true
	})
}

// VerifyFilesBeforeLaunch 启动前是否校验游戏文件完整性并自动补全缺失文件。默认 true。
// 值损坏（手改 launcher.yaml 写坏）与未设置同样按默认开启处理：
// 否则一条坏配置会静默关闭校验。
func VerifyFilesBeforeLaunch() bool {
	value := GetValue("verifyFilesBeforeLaunch")
	// 未设置或损坏时默认开启
	if value == "" {
		return true
	}
	result, err := parseBool(value)
	if err != nil {
		return true
	}
	return result
}

// SaveVerifyFilesBeforeLaunch 保存启动前文件校验设置。
func SaveVerifyFilesBeforeLaunch(enabled bool) {
	setValue("verifyFilesBeforeLaunch", formatBool(enabled))
}

// AcrylicBackdropEnabled 亚克力模糊（Acrylic 背景）开关。未设置或值损坏时
// 默认关闭：默认是清晰透明（无模糊），亚克力模糊是可选项。
func AcrylicBackdropEnabled() bool {
	value := GetValue("launcherAcrylicEnabled")
	if value == "" {
		return false
	}
	result, err := parseBool(value)
	if err != nil {
		return false
	}
	return result
}

// SaveAcrylicBackdropEnabled 保存亚克力模糊开关。Win11 22621+ 由绑定层运行时热切换
// （见 internal/bindings/acrylic_windows.go），旧系统回落为重启应用。
func SaveAcrylicBackdropEnabled(enabled bool) {
	setValue("launcherAcrylicEnabled", formatBool(enabled))
}

// AutoUpdateEnabled 启动时自动检查更新开关。未设置或值损坏时默认开启：
// 只检查并提示，不会不经确认就替换启动器（替换动作始终由用户在弹窗里确认）。
func AutoUpdateEnabled() bool {
	value := GetValue("launcherAutoUpdateEnabled")
	if value == "" {
		return true
	}
	result, err := parseBool(value)
	if err != nil {
		return true
	}
	return result
}

// SaveAutoUpdateEnabled 保存自动检查更新开关。
func SaveAutoUpdateEnabled(enabled bool) {
	setValue("launcherAutoUpdateEnabled", formatBool(enabled))
}

// 更新通道取值：稳定版只看正式 Release，预览版把 prerelease 也算进来。
// 项目当前以 preview 版为主，默认预览通道。
const (
	UpdateChannelPreview = "preview"
	UpdateChannelStable  = "stable"
)

// UpdateChannels 全部合法通道值（设置页选择器顺序）。
var UpdateChannels = []string{UpdateChannelStable, UpdateChannelPreview}

// UpdateChannelLabel 通道的用户可读名称。
func UpdateChannelLabel(channel string) string {
	switch NormalizeUpdateChannel(channel) {
	case UpdateChannelStable:
		return "稳定版"
	default:
		return "预览版"
	}
}

// NormalizeUpdateChannel 把任意输入洗成合法通道值：未知/空回落预览通道
// （项目以 preview 发版为主，宁可多看到新版也不漏）。
func NormalizeUpdateChannel(channel string) string {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case UpdateChannelStable:
		return UpdateChannelStable
	default:
		return UpdateChannelPreview
	}
}

// UpdateChannel 当前更新通道。迁移规则：新键 launcherUpdateChannel 优先；
// 没写过的老配置回落到旧的 launcherAutoUpdateIncludePrerelease 布尔
// （false → 稳定版，true/未设置 → 预览版）。
func UpdateChannel() string {
	value := GetValue("launcherUpdateChannel")
	if value != "" {
		return NormalizeUpdateChannel(value)
	}
	if AutoUpdateIncludePrerelease() {
		return UpdateChannelPreview
	}
	return UpdateChannelStable
}

// SaveUpdateChannel 保存更新通道（未知值由 NormalizeUpdateChannel 洗成合法值）。
func SaveUpdateChannel(channel string) {
	setValue("launcherUpdateChannel", NormalizeUpdateChannel(channel))
}

// AutoUpdateIncludePrerelease 旧的预发布布尔偏好（已被通道取代，仅作迁移读取）。
// 未设置或值损坏时默认包含。
func AutoUpdateIncludePrerelease() bool {
	value := GetValue("launcherAutoUpdateIncludePrerelease")
	if value == "" {
		return true
	}
	result, err := parseBool(value)
	if err != nil {
		return true
	}
	return result
}

// SetValue 保存/更新任意字符串配置项。账户域的键写入 accounts.yaml，
// 其余键写入 launcher.yaml（见 storagedomains.go）。
func SetValue(key, value string) bool {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return false
	}
	return withStoreBool(func(store *ConfigFileManager) bool {
		if accountsStore := accountsStoreFor(key); accountsStore != nil {
			return accountsStore.Set(key, value)
		}
		return store.ConfigItemAdd(key, value)
	})
}

// setValue 包内小写入口：跳过外部重复校验（仅供包内已校验过的调用使用）。
func setValue(key, value string) bool {
	return SetValue(key, value)
}

// GetValue 读取任意字符串配置项；不存在（或为空白）时返回空串。
// 账户域的键从 accounts.yaml 读取，其余键从 launcher.yaml 读取。
func GetValue(key string) string {
	return withStoreString(func(store *ConfigFileManager) string {
		if accountsStore := accountsStoreFor(key); accountsStore != nil {
			return accountsStore.Get(key)
		}
		value := store.ConfigItemRead(key)
		if strings.TrimSpace(value) == "" {
			return ""
		}
		return value
	})
}

// ClearValue 删除配置项；不存在时无副作用。返回是否删除成功。
func ClearValue(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	return withStoreBool(func(store *ConfigFileManager) bool {
		if accountsStore := accountsStoreFor(key); accountsStore != nil {
			return accountsStore.Remove(key)
		}
		return store.ConfigItemDelete(key)
	})
}

// UpdateInTransaction 在单个原子写入中应用多项配置修改：任一环节失败时整体回滚并放弃落盘。
// 回调直接操作 JSON 文档，不要在其中调用本类的 SetValue 等方法（会破坏事务性）。
func UpdateInTransaction(mutation func(map[string]any) bool) bool {
	return withStoreBool(func(store *ConfigFileManager) bool {
		return store.UpdateInTransaction(mutation)
	})
}

func launcherConfigFilePath() string {
	return filepath.Join(storageDirectory, "launcher.yaml")
}

// ensureStore 取得（或按需创建）底层 ConfigFileManager，需持 configSyncRoot 调用。
func ensureStore() *ConfigFileManager {
	if sharedStore == nil {
		store, err := NewConfigFileManager(launcherConfigFilePath())
		if err != nil {
			logsWriteError("初始化配置存储失败: " + err.Error())
			return nil
		}
		sharedStore = store
	}
	return sharedStore
}

func withStoreBool(action func(*ConfigFileManager) bool) bool {
	configSyncRoot.Lock()
	defer configSyncRoot.Unlock()
	store := ensureStore()
	if store == nil {
		return false
	}
	return action(store)
}

func withStoreString(action func(*ConfigFileManager) string) string {
	configSyncRoot.Lock()
	defer configSyncRoot.Unlock()
	store := ensureStore()
	if store == nil {
		return ""
	}
	return action(store)
}

// normalizeDirectory 规范化目录：展开完整路径并去掉尾部目录分隔符。
func normalizeDirectory(path string) string {
	trimmed := strings.TrimSpace(path)
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return trimTrailingSeparator(trimmed)
	}
	return trimTrailingSeparator(filepath.Clean(abs))
}

// PathsEqualNormalized 已规范化路径的比较：Windows 忽略大小写。
func PathsEqualNormalized(left, right string) bool {
	return pathsEqualFold(left, right)
}
