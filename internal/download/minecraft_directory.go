// minecraft_directory.go 本包对 Minecraft 默认目录的便捷转发。
//
// 唯一实现处是 internal/tools（launch / download / instance 共用）。
// 这里保留同名转发是为了让本包内的调用点读起来短一些；新增代码也可以直接调 tools.*。
package download

import "nekolauncher/internal/tools"

// GetDefaultMinecraftDirectory 默认 .minecraft 目录（Windows 为 %APPDATA%\.minecraft）。
func GetDefaultMinecraftDirectory() string {
	return tools.DefaultMinecraftDirectory()
}

// EnsureDefaultMinecraftDirectory 确保默认目录存在并返回（含标准目录骨架）。
func EnsureDefaultMinecraftDirectory() string {
	return tools.EnsureDefaultMinecraftDirectory()
}
