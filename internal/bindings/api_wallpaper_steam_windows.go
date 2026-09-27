//go:build windows

package bindings

// Windows：从注册表读取 Steam 安装目录。
// 单独成文件是因为 golang.org/x/sys/windows/registry 在非 Windows 上没有任何
// 可编译文件——放在共用文件里会让整个 bindings 包无法跨平台编译。

import "golang.org/x/sys/windows/registry"

// steamPathFromRegistry 注册表 HKCU\Software\Valve\Steam 的 SteamPath；不可用返回空串。
func steamPathFromRegistry() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	value, _, err := key.GetStringValue("SteamPath")
	if err != nil {
		return ""
	}
	return value
}
