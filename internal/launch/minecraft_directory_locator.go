package launch

// 目录定位：这里只保留启动管线真正用到的那一个入口。
//
// P3-7 清理说明：原先这个文件是从 C# 版 MinecraftDirectoryLocator 整段搬过来的，
// 其中 `MinecraftInstallationLocation` / `ResolveInstallationPath` /
// `EnsureDefaultMinecraftDirectory` / `GetInstalledVersionIds`（包内兼容包装）在 Go 侧
// 从来没有调用者——实例扫描、目录校验、导入这些事已经全部由 `internal/instance`
// 承担（bindings/api_instance.go 直接用 instance 的实现）。留着它们只会让"改哪一份才对"
// 变成新的坑，因此删掉；`GetDefaultMinecraftDirectory` 与 `isWindows` 仍被启动管线使用，
// 保留在本文件。

import (
	"nekolauncher/internal/tools"
)

// GetDefaultMinecraftDirectory 获取当前操作系统下 Minecraft 官方启动器使用的
// 默认 .minecraft 目录。实现在 tools 包（launch / download 共用的唯一定义处）。
func GetDefaultMinecraftDirectory() string {
	return tools.DefaultMinecraftDirectory()
}

// isWindows 当前平台是否为 Windows（goos 在 platform.go，测试里可替换）。
func isWindows() bool { return goos == "windows" }
