//go:build windows

package main

import "golang.org/x/sys/windows/registry"

// startupBackgroundColour 返回窗口首帧（WebView 渲染前）的底色，跟随系统深浅色，
// 避免启动瞬间闪出一块与前端主题不符的颜色。透明分量由 WebviewIsTransparent 统一控制。
func startupBackgroundColour() (r, g, b uint8) {
	light := true
	if k, err := registry.OpenKey(registry.CURRENT_USER,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetIntegerValue("AppsUseLightTheme"); err == nil {
			light = v != 0
		}
		k.Close()
	}
	if light {
		return 0xFF, 0xFF, 0xFF
	}
	return 0x14, 0x14, 0x14 // 贴近前端 dark:bg-gray-950 的深色底
}
