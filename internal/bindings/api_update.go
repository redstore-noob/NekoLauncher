package bindings

// UpdateAPI 启动器自身更新（X-1）：查版本 → 下载 → （Windows）就地替换并重启。
//
// 与 Terracotta 下载那种"装别人的东西"不同，这里替换的是**启动器自己**：
// 因此只做能保证安全的部分——查到新版本、下载并校验大小、Windows 上改名旧 exe 后
// 换上新的并重启；其它平台明确提示手动下载（见 internal/update 的说明）。

import (
	"context"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"nekolauncher/internal/config"
	"nekolauncher/internal/info"
	"nekolauncher/internal/logs"
	"nekolauncher/internal/update"
)

// UpdateAPI 自身更新绑定。
type UpdateAPI struct {
	ctx context.Context
}

// Startup 注入 Wails runtime ctx（重启自身需要它来退出应用）。
func (a *UpdateAPI) Startup(ctx context.Context) {
	a.ctx = ctx
	// 上一次更新留下的旧 exe 在这里清掉：本次进程不再使用它
	update.CleanupOldExecutable()
	// 启动时自动检查更新（延迟几秒，避开启动高峰）：只查并广播
	// update:available，是否下载替换由前端弹窗确认（绝不静默换掉启动器）。
	go a.autoCheckOnStartup()
}

// autoCheckDelay 自动检查的延迟：等首屏渲染、实例扫描等启动大头先跑起来。
const autoCheckDelay = 4 * time.Second

// autoCheckOnStartup 按「自动检查更新」设置与更新通道在后台查一次版本；有新版时
// 广播 update:available（payload 为 update.CheckResult）。网络失败只写日志，
// 绝不弹错误打扰用户——自动检查本来就是"有就提示，没有就安静"。
func (a *UpdateAPI) autoCheckOnStartup() {
	time.Sleep(autoCheckDelay)
	if !config.AutoUpdateEnabled() {
		return
	}
	channel := config.UpdateChannel()
	result, err := update.Check(callCtx(a.ctx), info.Version(), channel != config.UpdateChannelStable)
	if err != nil {
		logs.Write("INFO", "自动检查更新跳过："+err.Error())
		return
	}
	if !result.UpdateAvailable {
		return
	}
	emit(a.ctx, "update:available", result)
}

// GetAutoUpdateEnabled 启动时是否自动检查更新。
func (a *UpdateAPI) GetAutoUpdateEnabled() bool { return config.AutoUpdateEnabled() }

// SaveAutoUpdateEnabled 保存自动检查更新开关。
func (a *UpdateAPI) SaveAutoUpdateEnabled(enabled bool) {
	config.SaveAutoUpdateEnabled(enabled)
}

// UpdateChannelOption 更新通道的可选项（前端选择器直接渲染）。
type UpdateChannelOption struct {
	Value string `json:"Value"`
	Label string `json:"Label"`
}

// GetUpdateChannels 全部可选更新通道（稳定版 / 预览版）。
func (a *UpdateAPI) GetUpdateChannels() []UpdateChannelOption {
	options := make([]UpdateChannelOption, 0, len(config.UpdateChannels))
	for _, channel := range config.UpdateChannels {
		options = append(options, UpdateChannelOption{
			Value: channel,
			Label: config.UpdateChannelLabel(channel),
		})
	}
	return options
}

// GetUpdateChannel 当前更新通道（"stable" / "preview"）。
func (a *UpdateAPI) GetUpdateChannel() string { return config.UpdateChannel() }

// SaveUpdateChannel 保存更新通道；未知值由配置层洗成预览通道。
func (a *UpdateAPI) SaveUpdateChannel(channel string) {
	config.SaveUpdateChannel(channel)
}

// CheckLauncherUpdate 查询是否有新版本；channel 为更新通道（"stable"/"preview"，
// 见 config.UpdateChannels），预览通道把预发布版也算进来。
//
// 返回的错误只表示"查不动"（网络/限流/仓库没有版本），查得到但没资产的情况走
// CheckResult.ManualHint，前端据此引导手动下载。
func (a *UpdateAPI) CheckLauncherUpdate(channel string) (update.CheckResult, error) {
	return update.Check(callCtx(a.ctx), info.Version(),
		config.NormalizeUpdateChannel(channel) != config.UpdateChannelStable)
}

// DownloadLauncherUpdate 下载选中的版本资产，返回落盘路径。
func (a *UpdateAPI) DownloadLauncherUpdate(asset update.Asset) (string, error) {
	return update.Download(callCtx(a.ctx), asset, func(downloaded, total int64) {
		emit(a.ctx, "update:progress", map[string]int64{
			"downloaded": downloaded,
			"total":      total,
		})
	})
}

// ApplyLauncherUpdate 用下载好的文件替换启动器本体。
//
// 返回 true 表示新版本已经拉起、当前进程即将退出（前端不要再做后续操作）；
// 非 Windows 平台返回错误（update.ErrManualUpdateRequired），由前端提示手动下载。
func (a *UpdateAPI) ApplyLauncherUpdate(newExecutable string) (bool, error) {
	started, err := update.Apply(newExecutable)
	if err != nil {
		return false, err
	}
	if started {
		// 给新进程一点时间接管，然后退出自身（否则两个实例同时开着）
		go func() {
			wailsruntime.Quit(callCtx(a.ctx))
		}()
	}

	return started, nil
}
