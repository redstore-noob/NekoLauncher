//go:build darwin

package bindings

// macOS：桌面壁纸路径只能问系统要——没有等价的 C 层公开 API（NSWorkspace 需要
// Objective-C 运行时），最稳的是走 osascript 问 System Events。
//
// 注意两点：
//   - 多显示器/多空间时 osascript 会返回多行，每行一张图片；取第一张（主桌面）即可，
//     启动器背景只需要一张图。
//   - 首次调用可能弹"允许控制 System Events"的授权框；被拒绝时脚本报错，
//     这里一律当成"拿不到"，由前端回退默认图，不打扰用户。

import (
	"strings"
)

// desktopWallpaperScript 取当前桌面图片路径的 AppleScript。
const desktopWallpaperScript = `tell application "System Events" to get picture of current desktop`

// desktopWallpaperPath 返回当前桌面壁纸路径（多屏时取第一张）；取不到返回空串。
func desktopWallpaperPath() (string, error) {
	output, err := runWallpaperCommand("osascript", "-e", desktopWallpaperScript)
	if err != nil {
		return "", nil
	}

	for _, line := range strings.Split(output, "\n") {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		// 正常给 POSIX 绝对路径，交给前端经 /localfile 中转
		return path, nil
	}

	return "", nil
}
