//go:build linux

package bindings

// SystemAPI 扩展：Linux 动态桌面壁纸（swww / mpvpaper 方案）。
//
// 背景：Wayland 下 wlroots 系合成器（Sway / Hyprland / river 等）没有统一的
// 桌面壁纸接口，社区事实标准是两个工具：
//   - swww（Wayland 壁纸守护进程）：客户端 swww img/query/kill 经 Unix socket
//     控制 swww-daemon，支持 JPEG/PNG/WebP/GIF（GIF 逐帧动画），换图带转场效果。
//   - mpvpaper：基于 wlr-layer-shell 把 mpv 输出铺成壁纸层，用来放视频壁纸，
//     自身不记录"当前在放什么"，也没有查询接口。
//
// 分工：查询当前壁纸只能问 swww（swww query）；设置桌面壁纸时图片/GIF 走
// swww img，视频走 mpvpaper（先杀旧进程再拉新进程，-p 后台运行）。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LinuxWallpaperTools 报告两个工具的安装情况，前端据此展示/隐藏对应入口。
type LinuxWallpaperTools struct {
	// Swww 可执行文件是否在 PATH 里（swww 客户端；daemon 由 img 时自动拉起）。
	Swww bool `json:"Swww"`
	// Mpvpaper 可执行文件是否在 PATH 里（视频壁纸需要）。
	Mpvpaper bool `json:"Mpvpaper"`
}

// GetLinuxWallpaperTools 检测 swww / mpvpaper 是否可用。非 Linux 平台的桩实现
// 恒返回两者都不可用。
func (a *SystemAPI) GetLinuxWallpaperTools() (LinuxWallpaperTools, error) {
	_, swwwErr := exec.LookPath("swww")
	_, mpvpaperErr := exec.LookPath("mpvpaper")
	return LinuxWallpaperTools{
		Swww:     swwwErr == nil,
		Mpvpaper: mpvpaperErr == nil,
	}, nil
}

// swwwQueryOutput 跑 swww query，原样返回输出；daemon 未运行 / 未装时返回错误。
func swwwQueryOutput() (string, error) {
	return runWallpaperCommand("swww", "query")
}

// parseSwwwQuery 解析 swww query 的输出。格式（每个输出一行）：
//
//	eDP-1: image: /home/user/Pictures/wall.png
//	HDMI-A-1: image: /home/user/Pictures/wall.png
//
// 多显示器时取第一行的路径；只在 image: 前缀存在时认——swww 的
// "clear" 状态输出的是 "eDP-1: No images!"，没有 image: 字样。
func parseSwwwQuery(output string) string {
	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, "image:")
		if idx < 0 {
			continue
		}
		path := strings.TrimSpace(line[idx+len("image:"):])
		if path != "" {
			return path
		}
	}
	return ""
}

// swwwWallpaperPath swww 当前壁纸路径；未装 / daemon 没跑 / 没设壁纸返回空串。
// 同时被 api_wallpaper_desktop_linux.go 的桌面壁纸探测链复用：wlroots 系桌面
// 没有 gsettings 可问，swww 往往是唯一能拿到壁纸的地方。
func swwwWallpaperPath() string {
	output, err := swwwQueryOutput()
	if err != nil {
		return ""
	}
	path := parseSwwwQuery(output)
	if path == "" {
		return ""
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return ""
	}
	return path
}

// isVideoWallpaperFile 按扩展名判断是否视频文件（mpvpaper 路径）。
func isVideoWallpaperFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".webm", ".mkv", ".mov", ".avi", ".gifv":
		return true
	}
	return false
}

// ApplyDesktopWallpaper 把本地文件设置成桌面壁纸（Linux 的 swww / mpvpaper 方案；
// 其他平台桩实现返回"平台不支持"）。图片与 GIF 走 swww，视频走 mpvpaper。
func (a *SystemAPI) ApplyDesktopWallpaper(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("壁纸路径不能为空")
	}
	if info, err := os.Stat(path); err != nil {
		return fmt.Errorf("壁纸文件不存在：%s", path)
	} else if info.IsDir() {
		return fmt.Errorf("壁纸路径是目录：%s", path)
	}

	if isVideoWallpaperFile(path) {
		return applyMpvpaperWallpaper(path)
	}
	return applySwwwWallpaper(path)
}

// applySwwwWallpaper 用 swww 设置图片/GIF 壁纸。旧版 swww 需要 daemon 已由
// swww init 拉起：直接 img 失败时补一次 init 再试，兼容两种行为。
func applySwwwWallpaper(path string) error {
	if _, err := exec.LookPath("swww"); err != nil {
		return fmt.Errorf("未找到 swww，请先安装（如 paru -S swww）")
	}
	args := []string{"img", path, "--transition-type", "random"}
	if err := exec.Command("swww", args...).Run(); err == nil {
		return nil
	}
	// img 失败多半是 daemon 没跑：init 后重试一次
	if err := exec.Command("swww", "init").Run(); err != nil {
		return fmt.Errorf("swww 初始化失败：%w", err)
	}
	if err := exec.Command("swww", args...).Run(); err != nil {
		return fmt.Errorf("swww 设置壁纸失败：%w", err)
	}
	return nil
}

// applyMpvpaperWallpaper 用 mpvpaper 在所有输出上循环静音播放视频。
// 先 pkill 旧实例避免两个视频层叠着播；-p 让它 fork 到后台，
// "*" 表示应用到全部输出（用户手动跑时可以指定 eDP-1 等单个输出）。
func applyMpvpaperWallpaper(path string) error {
	if _, err := exec.LookPath("mpvpaper"); err != nil {
		return fmt.Errorf("未找到 mpvpaper，请先安装（视频壁纸需要它）")
	}
	// 杀旧进程失败不阻塞：可能本来就没在跑
	_ = exec.Command("pkill", "-x", "mpvpaper").Run()

	command := exec.Command("mpvpaper", "-p", "-o",
		"no-audio --loop-file=inf",
		"*", path)
	if err := command.Start(); err != nil {
		return fmt.Errorf("mpvpaper 启动失败：%w", err)
	}
	// -p 模式 mpvpaper 会立刻 fork 出去，父进程随我们退出没关系；
	// 但 Start 的句柄要释放，避免僵尸进程条目
	go func() { _ = command.Wait() }()
	return nil
}
