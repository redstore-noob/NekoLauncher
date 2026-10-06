//go:build linux

package bindings

// Linux：桌面壁纸没有统一接口，只能按桌面环境挨个问。顺序按"覆盖人数 + 读取可靠性"排：
//
//	1. gsettings（GNOME / Cinnamon / MATE / Unity 系）——picture-uri 与 picture-uri-dark
//	2. kreadconfig5/6 或直接解析 plasma-org.kde.plasma.desktop-appletsrc（KDE Plasma）
//	3. xfconf-query（XFCE）
//	4. ~/.fehbg（feh 用户，脚本里写着上一条 --bg 命令）
//	5. swww query（wlroots 系桌面：Sway / Hyprland 等，没有 gsettings 可问）
//
// 全部拿不到就返回空串，由前端回退默认图——不报错，因为"这个环境没读过壁纸"并不是故障。
// 取值解析（去引号、file:// URI、死路径过滤）在 api_wallpaper_desktop_parse.go。

import (
	"os"
	"path/filepath"
	"strings"
)

// desktopWallpaperPath 依次尝试各桌面环境的读取方式。swww 放最后：
// GNOME/KDE/XFCE 用户走不到这一步就返回了，少 spawn 一次子进程。
func desktopWallpaperPath() (string, error) {
	for _, probe := range []func() string{
		gnomeWallpaperPath,
		kdeWallpaperPath,
		xfceWallpaperPath,
		fehWallpaperPath,
		swwwWallpaperPath,
	} {
		if path := probe(); path != "" {
			return path, nil
		}
	}

	return "", nil
}

// ---- GNOME 系 ----

// gnomeWallpaperPath 读 gsettings 的 picture-uri（浅色主题优先，其次深色主题）。
func gnomeWallpaperPath() string {
	for _, key := range [][2]string{
		{"org.gnome.desktop.background", "picture-uri"},
		{"org.gnome.desktop.background", "picture-uri-dark"},
	} {
		output, err := runWallpaperCommand("gsettings", "get", key[0], key[1])
		if err != nil {
			continue
		}
		if path := wallpaperPathFromValue(output); path != "" {
			return path
		}
	}

	return ""
}

// ---- KDE Plasma ----

// kdeWallpaperPath 先试 kreadconfig5/6，再退回直接解析配置文件。
func kdeWallpaperPath() string {
	for _, tool := range []string{"kreadconfig6", "kreadconfig5"} {
		output, err := runWallpaperCommand(tool,
			"--file", "plasma-org.kde.plasma.desktop-appletsrc",
			"--group", "Wallpaper", "--group", "org.kde.image", "--group", "General",
			"--key", "Image")
		if err != nil {
			continue
		}
		if path := wallpaperPathFromValue(output); path != "" {
			return path
		}
	}

	return kdeWallpaperPathFromConfig()
}

// kdeWallpaperPathFromConfig 直接读 Plasma 的 appletsrc（没有 kreadconfig 时的兜底）。
func kdeWallpaperPathFromConfig() string {
	configDirectory := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configDirectory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configDirectory = filepath.Join(home, ".config")
	}

	data, err := os.ReadFile(filepath.Join(configDirectory, "plasma-org.kde.plasma.desktop-appletsrc"))
	if err != nil {
		return ""
	}

	return parseKDEWallpaperConfig(string(data))
}

// ---- XFCE ----

// xfceWallpaperPath 找 xfce4-desktop 里第一个 last-image 属性。
func xfceWallpaperPath() string {
	list, err := runWallpaperCommand("xfconf-query", "-c", "xfce4-desktop", "-l")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(list, "\n") {
		property := strings.TrimSpace(line)
		if !strings.HasSuffix(property, "last-image") {
			continue
		}
		output, err := runWallpaperCommand("xfconf-query", "-c", "xfce4-desktop", "-p", property)
		if err != nil {
			continue
		}
		if path := wallpaperPathFromValue(output); path != "" {
			return path
		}
	}

	return ""
}

// ---- feh ----

// fehWallpaperPath 解析 ~/.fehbg（feh 用它记住上次设置的壁纸）。
func fehWallpaperPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".fehbg"))
	if err != nil {
		return ""
	}

	return parseFehbg(string(data))
}
