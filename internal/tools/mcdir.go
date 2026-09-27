package tools

// mcdir.go Minecraft 默认目录的唯一定义处。
// 此前 launch / download 两包各持一份近似实现（路径推导与创建骨架行为不一致），
// 现统一收敛到 tools，供两个包共用。

import (
	"os"
	"path/filepath"
	"runtime"
)

// standardSubDirectories Minecraft 标准目录结构中应当存在的子文件夹列表。
// 仅包含目录骨架，不创建任何文件（列表与原 launch 实现一致）。
var standardSubDirectories = []string{
	"versions",
	"assets",
	"libraries",
	"saves",
	"resourcepacks",
	"mods",
	"config",
	"crash-reports",
	"logs",
	"screenshots",
	"shaderpacks",
}

// DefaultMinecraftDirectory 返回当前操作系统下 Minecraft 官方启动器使用的
// 默认 .minecraft 目录；主目录不可用时返回空串。
func DefaultMinecraftDirectory() string {
	home := UserHomeDir()
	if home == "" {
		return ""
	}
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			return filepath.Join(appData, ".minecraft")
		}
		return filepath.Join(home, "AppData", "Roaming", ".minecraft")
	case "darwin":
		// macOS：~/Library/Application Support/minecraft
		return filepath.Join(home, "Library", "Application Support", "minecraft")
	default:
		// Linux 及其他未识别系统：~/.minecraft
		return filepath.Join(home, ".minecraft")
	}
}

// EnsureDefaultMinecraftDirectory 确保默认 Minecraft 目录存在（不存在时在
// 平台默认路径下创建符合 Minecraft 目录结构的空文件夹骨架），并返回路径。
func EnsureDefaultMinecraftDirectory() string {
	defaultDir := DefaultMinecraftDirectory()
	if defaultDir == "" {
		return ""
	}
	if !DirectoryExists(defaultDir) {
		_ = os.MkdirAll(defaultDir, 0o755)
		for _, sub := range standardSubDirectories {
			_ = os.MkdirAll(filepath.Join(defaultDir, sub), 0o755)
		}
	}
	return defaultDir
}
