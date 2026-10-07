package bindings

// 12 个 API 结构体：每个导出方法 = 一个 Wails 命令。
// 持有 wails runtime 的 ctx（由 main.go 的 OnStartup 调 API.Startup 注入），
// 各包回调经 emit 桥接为 EventsEmit 事件。

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nekolauncher/internal/auth"
	"nekolauncher/internal/config"
	"nekolauncher/internal/download"
	"nekolauncher/internal/instance"
	"nekolauncher/internal/launch"
	"nekolauncher/internal/logs"
	"nekolauncher/internal/mcserver"
	"nekolauncher/internal/music"
	"nekolauncher/internal/network"
)

// ConfigAPI 配置读写命令集（config 包的绑定门面）。
type ConfigAPI struct {
	ctx context.Context
}

// Startup 注入 Wails runtime ctx（main.go OnStartup 调用）。
func (a *ConfigAPI) Startup(ctx context.Context) { a.ctx = ctx }

// LauncherAPI 游戏启动相关命令集。
type LauncherAPI struct {
	ctx context.Context
	// service 启动服务（全进程唯一活动启动管线）。
	service *launch.GameLaunchService
}

// Startup 注入 Wails runtime ctx。
func (a *LauncherAPI) Startup(ctx context.Context) { a.ctx = ctx }

// DownloadAPI 下载任务相关命令集。
type DownloadAPI struct {
	ctx context.Context
	// service 下载任务状态机（进度经 OnChanged 桥接为 download:progress 事件）。
	service *download.GameDownloadService
}

// Startup 注入 Wails runtime ctx。
func (a *DownloadAPI) Startup(ctx context.Context) { a.ctx = ctx }

// AccountAPI 账号管理相关命令集。
type AccountAPI struct {
	ctx context.Context
	// microsoft Microsoft 设备码认证器；authlib 皮肤站认证器。
	microsoft *auth.MicrosoftDeviceCodeAuthenticator
	authlib   *auth.AuthlibAuthenticator
	// browserAuth 内嵌浏览器 OAuth 认证器（复用 microsoft 的令牌交换链路）。
	browserAuth *auth.MicrosoftBrowserAuthenticator
	// loginMu / loginCancel 当前微软登录（设备码或内嵌浏览器）的取消句柄
	// （见 api_account_ext.go）。以指针包装保存，便于 done 时判断句柄是否
	// 仍属于本次登录（指针可比较，func 不可）。
	loginMu     sync.Mutex
	loginCancel *microsoftLoginHandle
	// browserMu / browserState / browserReturnTo 内嵌浏览器登录的进度状态
	// （见 api_account_browser.go）。SPA 会在登录页跳转往返间重载，
	// 进度必须由后端持有，前端靠事件 + 轮询接力显示。
	browserMu       sync.Mutex
	browserState    MicrosoftBrowserLoginState
	browserReturnTo string
	// authlibMu / authlibPending 多角色皮肤站登录的待确认会话：登录返回的
	// 访问令牌与角色 UUID 只留在后端，前端按序号选择角色（见 AuthlibLogin /
	// ConfirmAuthlibProfile）。同一时刻只有一份，下次登录直接覆盖。
	authlibMu      sync.Mutex
	authlibPending *authlibPendingSession
}

// authlibPendingSession 一次皮肤站登录的待确认状态。
type authlibPendingSession struct {
	credential auth.AuthlibCredential // 除 ProfileName/ProfileUuid 外已就绪
	profiles   []auth.AuthlibProfileInfo
}

// Startup 注入 Wails runtime ctx，并启动皮肤缓存的后台每日自动刷新。
func (a *AccountAPI) Startup(ctx context.Context) {
	a.ctx = ctx
	go a.runSkinCacheRefreshScheduler()
}

// InstanceAPI 实例管理相关命令集。
type InstanceAPI struct {
	ctx context.Context
}

// Startup 注入 Wails runtime ctx。
func (a *InstanceAPI) Startup(ctx context.Context) { a.ctx = ctx }

// WorldAPI 世界（存档）相关命令集。
type WorldAPI struct {
	ctx context.Context
}

// Startup 注入 Wails runtime ctx。
func (a *WorldAPI) Startup(ctx context.Context) { a.ctx = ctx }

// ContentAPI 实例内容相关命令集。
type ContentAPI struct {
	ctx context.Context
	// launch 启动服务（全进程唯一活动启动管线）：实例级快照/回滚前用它
	// 确认游戏未在运行，避免把"写了一半"的实例状态定格或替换。
	launch *launch.GameLaunchService
}

// Startup 注入 Wails runtime ctx。
func (a *ContentAPI) Startup(ctx context.Context) { a.ctx = ctx }

// ModpackAPI 整合包相关命令集。
type ModpackAPI struct {
	ctx context.Context
	// exportMu / exportCancel 当前导出（.mrpack/.zip 或 NekoSolo .exe）的取消句柄，
	// 供 CancelExport 中断进行中的导出（模式同 AccountAPI 的 loginCancel）。
	exportMu     sync.Mutex
	exportCancel *exportHandle
}

// Startup 注入 Wails runtime ctx。
func (a *ModpackAPI) Startup(ctx context.Context) { a.ctx = ctx }

// MusicAPI 音乐播放器相关命令集。
type MusicAPI struct {
	ctx context.Context
	// library 曲库（扫描 / 排序 / 音量等持久化偏好）。
	library *music.MusicLibrary
	// positionNs / durationNs 前端音频实现回传的播放进度（纳秒）。
	positionNs atomic.Int64
	durationNs atomic.Int64
	// finishedMu 保护自然播完回调（由播放器状态机注册）。
	finishedMu      sync.Mutex
	onTrackFinished func()
}

// Startup 注入 Wails runtime ctx。
func (a *MusicAPI) Startup(ctx context.Context) { a.ctx = ctx }

// MonitorAPI 内存监控命令集。
type MonitorAPI struct {
	ctx context.Context
}

// Startup 注入 Wails runtime ctx。
func (a *MonitorAPI) Startup(ctx context.Context) { a.ctx = ctx }

// ServerAPI 服务器状态查询命令集。
type ServerAPI struct {
	ctx context.Context
}

// Startup 注入 Wails runtime ctx。
func (a *ServerAPI) Startup(ctx context.Context) { a.ctx = ctx }

// SystemAPI 版本信息、日志与系统操作命令集。
type SystemAPI struct {
	ctx context.Context
	// writeMu / approvedWritePaths 用户经文件对话框亲自选中的路径集合：
	// WriteTextFile/WritePngFile 的写入白名单之一（另一类是游戏/音乐目录内）。
	// 插件与宿主同 WebView，无法区分调用方——对话框批准是唯一能证明
	// "用户知情同意这个路径"的信号。
	writeMu            sync.Mutex
	approvedWritePaths map[string]bool
}

// approveWritePath 记录一次用户对话框选择（保存/打开）产生的路径。
func (a *SystemAPI) approveWritePath(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	a.writeMu.Lock()
	if a.approvedWritePaths == nil {
		a.approvedWritePaths = make(map[string]bool)
	}
	a.approvedWritePaths[path] = true
	a.writeMu.Unlock()
}

// isApprovedWritePath 路径是否经对话框批准过（精确匹配）。
func (a *SystemAPI) isApprovedWritePath(path string) bool {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return a.approvedWritePaths[path]
}

// Startup 注入 Wails runtime ctx，并启动窗口透明/亚克力的 DWM 框架修正
// （窗口创建后铺满玻璃框架，见 acrylic_windows.go）。
func (a *SystemAPI) Startup(ctx context.Context) {
	a.ctx = ctx
	fixupAcrylicBackdrop()
}

// Startup 把 ctx 分发给全部 API；并执行一次性启动初始化（日志、下载源、首次实例扫描）。
func (a *API) Startup(ctx context.Context) {
	a.Config.Startup(ctx)
	a.Launcher.Startup(ctx)
	a.Download.Startup(ctx)
	a.Account.Startup(ctx)
	a.Instance.Startup(ctx)
	a.World.Startup(ctx)
	a.Content.Startup(ctx)
	a.Modpack.Startup(ctx)
	a.Music.Startup(ctx)
	a.Monitor.Startup(ctx)
	a.Server.Startup(ctx)
	a.ServerHost.Startup(ctx)
	a.Online.Startup(ctx)
	a.System.Startup(ctx)
	a.Plugin.Startup(ctx)
	a.Update.Startup(ctx)

	// 一次性启动逻辑（对应 C# App 构造 / OnStartup）
	// 代理最先应用：后续任何出站请求（更新检查、皮肤缓存、实例扫描的远程
	// 元数据）都应当拿到用户配置的代理，而不是先直连失败再等下一轮。
	network.ApplyProxySettings()
	logs.Init()
	download.ApplySavedSettings()
	download.SetDownloadSpeedLimitKbps(download.SpeedLimitKbps())
	go instance.Refresh(ctx, instance.ResolveConfiguredSourcePath())
	// 自动备份巡检（10 分钟一轮，是否真的备份由设置里的开关 + 间隔决定）
	mcserver.StartBackupScheduler(ctx)
}

// Shutdown 退出前收尾。Wails 只回调 options.OnShutdown，不会逐个调用绑定
// 结构体的方法，所以需要在这里反向分发。
//
// 顺序有讲究：先优雅停掉托管服务器（它们持有世界文件与会话锁），再收联机会话
// （隧道与外部进程）。停服整体上限 20 秒——超过就强杀，宁可丢一点未落盘的进度，
// 也不要留下占着 session.lock 的孤儿 Java 进程。
func (a *API) Shutdown() {
	for _, note := range mcserver.Default().ShutdownAll(20 * time.Second) {
		logs.Write("LAUNCH", note)
	}
	a.Online.Shutdown(context.Background())
}

// musicConfigStore 把 music.ConfigStore 适配到 config 包的持久化键值。
type musicConfigStore struct{}

func (musicConfigStore) GetValue(key string) string { return config.GetValue(key) }
func (musicConfigStore) SetValue(key, value string) { _ = config.SetValue(key, value) }
