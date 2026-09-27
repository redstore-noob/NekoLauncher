package bindings

// UpdateAPI 启动器自身更新（X-1）：查版本 → 下载 → （Windows）就地替换并重启。
//
// 与 Terracotta 下载那种"装别人的东西"不同，这里替换的是**启动器自己**：
// 因此只做能保证安全的部分——查到新版本、下载并校验大小、Windows 上改名旧 exe 后
// 换上新的并重启；其它平台明确提示手动下载（见 internal/update 的说明）。

import (
	"context"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"nekolauncher/internal/info"
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
}

// GetLauncherVersion 当前启动器版本（与设置页展示的同一来源）。
func (a *UpdateAPI) GetLauncherVersion() string { return info.Version() }

// CheckLauncherUpdate 查询是否有新版本；includePrerelease 为 true 时把预发布版也算进来。
//
// 返回的错误只表示"查不动"（网络/限流/仓库没有版本），查得到但没资产的情况走
// CheckResult.ManualHint，前端据此引导手动下载。
func (a *UpdateAPI) CheckLauncherUpdate(includePrerelease bool) (update.CheckResult, error) {
	return update.Check(callCtx(a.ctx), info.Version(), includePrerelease)
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
