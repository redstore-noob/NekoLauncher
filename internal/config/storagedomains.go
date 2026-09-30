package config

import "path/filepath"

// 账户域存储：启动器配置分为两个 YAML 文件，
//
//   - launcher.yaml  全部启动器配置（窗口/外观/侧边栏/主页/行为，以及
//                    Java 路径、游戏目录、内存、版本隔离、全局启动参数、
//                    下载设置、音乐、插件等游戏相关设置）
//   - accounts.yaml  账户凭据（加密 blob，与普通配置分文件存放，便于单独备份/清理）
//
// 两者都是 YAML。只有 Minecraft 本体或跨启动器互操作规定的文件（版本 JSON、
// mrpack、mmc-pack.json 等）保持 JSON，因为那些格式由外部规范定义。
//
// 账户域之外的键一律落在 launcher.yaml，因此这里只需要一张账户键表，
// 不再维护"个性化域"的路由。

const accountsYamlFileName = "accounts.yaml"

// accountsDomainKeys 账户域键集合（对应 internal/auth 中的存储键）。
var accountsDomainKeys = map[string]bool{
	"accounts":           true,
	"authlibClientToken": true,
}

var (
	// accountsYamlStore 惰性创建的账户域存储，由 configSyncRoot 保护
	// （与 sharedStore 一致）。
	accountsYamlStore *yamlFileManager
)

// IsAccountDomainKey 判断 key 是否属于账户域（accounts.yaml）。
// 供绑定层做访问控制：账户域键只能由 Go 侧（internal/auth）读写，
// 绝不能经 Wails 绑定暴露给 WebView——否则任何插件 JS 都能整包
// 读出或替换 accounts.yaml（伪造正版身份）。
func IsAccountDomainKey(key string) bool {
	return accountsDomainKeys[key]
}

// accountsStoreFor 返回账户域键对应的 YAML 存储；非账户键返回 nil
// （由调用方写入主配置 launcher.yaml）。需持 configSyncRoot 调用。
func accountsStoreFor(key string) *yamlFileManager {
	if !accountsDomainKeys[key] {
		return nil
	}
	if accountsYamlStore == nil {
		store, err := newYamlFileManager(filepath.Join(storageDirectory, accountsYamlFileName))
		if err != nil {
			logsWriteError("初始化账户存储失败: " + err.Error())
			return nil
		}
		accountsYamlStore = store
	}
	return accountsYamlStore
}

// resetDomainStores 切换存储目录后重置账户域存储，使后续访问重新加载新目录。
// 需持 configSyncRoot 调用。
func resetDomainStores() {
	accountsYamlStore = nil
}
