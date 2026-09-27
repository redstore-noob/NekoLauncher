package bindings

// 桌面壁纸取值的公共解析（各平台共用，因此不带构建标签，便于在任意平台上测试）。

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"nekolauncher/internal/tools"
)

// kdeWallpaperImagePattern Plasma 配置文件里的壁纸行：`Image=file:///path/to.png`。
var kdeWallpaperImagePattern = regexp.MustCompile(`(?m)^\s*Image=(.+)$`)

// fehFehbgPattern ~/.fehbg 里的 feh 命令：`feh --no-fehbg --bg-fill '/path'`。
var fehFehbgPattern = regexp.MustCompile(`(?m)^\s*feh\b.*?\s'([^']+)'`)

// wallpaperPathFromValue 把各桌面环境工具的输出收敛成本地绝对路径：
// 去掉引号与空白、把 file:// URI 还原成路径、丢弃纯色/渐变/网络地址这类非本地文件取值，
// 以及已经被删除的死路径（否则 WebView 只会拿到一张打不开的 URL）。
func wallpaperPathFromValue(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, "'\"")
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	// gsettings 未设置时给 'none'；KDE 纯色/渐变背景给 colors: 前缀
	if value == "none" || strings.HasPrefix(value, "colors:") {
		return ""
	}

	if strings.HasPrefix(value, "file://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return ""
		}
		path := parsed.Path
		if parsed.Host != "" {
			switch {
			case len(parsed.Host) == 2 && strings.HasSuffix(parsed.Host, ":"):
				// file://C:/Users/... —— 主机名其实是 Windows 盘符
				path = parsed.Host + path
			default:
				// file://server/share/... —— UNC 路径
				path = "//" + parsed.Host + path
			}
		}
		if path == "" {
			return ""
		}

		// URI 里一定是正斜杠，Windows 上换回本机风格（/localfile 与系统 API 保持一致）
		return existingWallpaperFile(filepath.FromSlash(path))
	}
	if strings.Contains(value, "://") {
		// 其它 scheme（http/ftp）不是本地壁纸文件
		return ""
	}
	if !filepath.IsAbs(value) {
		return ""
	}

	return existingWallpaperFile(value)
}

// existingWallpaperFile 只认磁盘上真实存在的文件。
func existingWallpaperFile(path string) string {
	if !tools.FileExists(path) {
		return ""
	}

	return path
}

// parseKDEWallpaperConfig 从 plasma-org.kde.plasma.desktop-appletsrc 里取第一个可用的壁纸。
func parseKDEWallpaperConfig(content string) string {
	for _, match := range kdeWallpaperImagePattern.FindAllStringSubmatch(content, -1) {
		if path := wallpaperPathFromValue(match[1]); path != "" {
			return path
		}
	}

	return ""
}

// parseFehbg 从 ~/.fehbg 取最近一次设置的壁纸（脚本可能有多条历史命令）。
func parseFehbg(content string) string {
	matches := fehFehbgPattern.FindAllStringSubmatch(content, -1)
	for index := len(matches) - 1; index >= 0; index-- {
		if path := wallpaperPathFromValue(matches[index][1]); path != "" {
			return path
		}
	}

	return ""
}
