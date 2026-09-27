//go:build windows

package bindings

import (
	"os"
	"testing"
)

// TestWindowsDesktopWallpaperPath 真读一次当前桌面壁纸：只读、不改系统设置。
// 验证放大缓冲后路径可用（本机设了壁纸时路径必须能打开；纯色背景返回空串同样算通过）。
func TestWindowsDesktopWallpaperPath(t *testing.T) {
	path, err := desktopWallpaperPath()
	if err != nil {
		t.Fatalf("读取桌面壁纸失败：%v", err)
	}
	if path == "" {
		t.Log("当前未设置壁纸（纯色背景）或路径过长，按设计返回空串由前端回落默认图")

		return
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("读到的壁纸路径打不开：%s（%v）", path, err)
	}

	t.Logf("桌面壁纸：%s（%d 个字符，缓冲 %d）", path, len([]rune(path)), desktopWallpaperBufferChars)
}
