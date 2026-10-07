package bindings

// SystemAPI 扩展：读取当前桌面壁纸路径（背景设置"与桌面壁纸保持一致"使用）。
// 与 api_system.go / api_system_ext.go 分离存放，避免与其他改动冲突。

import (
	"os"
	"os/exec"

	"nekolauncher/internal/config"
	"nekolauncher/internal/tools"
)

// GetDesktopWallpaperPath 返回当前桌面壁纸的绝对路径；未设置或不支持的平台返回空串。
// 返回的路径经应用内 /localfile 路由中转给 WebView（与自定义背景图同一通道）。
//
// 各平台实现见 api_wallpaper_desktop_*.go：
//   - Windows：user32!SystemParametersInfoW(SPI_GETDESKWALLPAPER)
//   - macOS：osascript 问 System Events 要"当前桌面的图片"
//   - Linux：按桌面环境依次尝试 gsettings（GNOME 系）/ kreadconfig（KDE）/
//     xfconf-query（XFCE）/ ~/.fehbg（feh 用户）
func (a *SystemAPI) GetDesktopWallpaperPath() (string, error) {
	return desktopWallpaperPath()
}

// runWallpaperCommand 跑一条"读取桌面壁纸"的查询命令并返回标准输出。
// 隐藏窗口、失败原样返回错误（各平台实现据此试下一种方式）。
func runWallpaperCommand(name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	tools.HideProcessWindow(command)

	output, err := command.Output()
	if err != nil {
		return "", err
	}

	return string(output), nil
}

// ---- 亚克力模糊（Acrylic 窗口背景） ----

// GetAcrylicBackdropEnabled 亚克力模糊当前是否启用。
func (a *SystemAPI) GetAcrylicBackdropEnabled() bool { return config.AcrylicBackdropEnabled() }

// SetAcrylicBackdropEnabled 保存亚克力模糊开关并使其生效。
// Win11 22621+ 支持运行时热切换（DWM backdrop 属性可直接改，返回 true，前端无需重启）；
// 旧系统不支持，回落为自动重启应用（返回 false，应用即将退出）。
func (a *SystemAPI) SetAcrylicBackdropEnabled(enabled bool) (bool, error) {
	if enabled == config.AcrylicBackdropEnabled() {
		return true, nil
	}
	config.SaveAcrylicBackdropEnabled(enabled)
	if applyAcrylicRuntime(enabled) {
		return true, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	newProcess := exec.Command(exe)
	// 自身是 GUI 程序，此处仅兜底防闪控制台
	tools.HideProcessWindow(newProcess)
	if err := newProcess.Start(); err != nil {
		return false, err
	}
	// 先拉起新进程再退出；配置落盘是临时文件+重命名的原子写，新进程读到的必是完整文件。
	//
	// 退出必须走 quitNow 而不是裸的 wailsruntime.Quit：裸调用会被 OnBeforeClose
	// 里的「选择托盘/退出」询问拦下（详见 close_behavior.go 的 quitNow 注释），
	// 结果是新进程已开、旧进程不退 → 两个窗口。
	quitNow(callCtx(a.ctx))
	return false, nil
}
