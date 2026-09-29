package bindings

// 包间接线：
//   - wireInstance：把 instance / config 注入 download 包的占位钩子
//     （host_hooks.go 的 RefreshInstancesHook / SelectInstanceHook /
//     ResolveContentDirectoryHook / ResolveInstanceLayoutHook / SetConfigHooks）；
//   - wireMusic：构造曲库 + 前端音频桥接（AudioPlayer 由前端实现），
//     把 music.Shared 的回调转发为 Wails 事件；
//   - 各 API 的 OnChanged 等回调在 New() 里统一接事件。

import (
	"context"
	"errors"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"nekolauncher/internal/config"
	"nekolauncher/internal/download"
	"nekolauncher/internal/instance"
	"nekolauncher/internal/launch"
	"nekolauncher/internal/modname"
	"nekolauncher/internal/music"
)

// LogLineEvent 逐行日志事件的载荷（launch:logLine）。
type LogLineEvent struct {
	// Tag "LAUNCH"（启动器阶段日志）/ "GAME"（游戏进程输出，stderr 行带 [stderr] 前缀）
	Tag  string `json:"Tag"`
	Line string `json:"Line"`
}

// toLaunchInstanceSnapshot 把 instance 存储快照映射为 launch 包的最小视图。
func toLaunchInstanceSnapshot(s instance.GameInstanceSnapshot) launch.GameInstanceSnapshot {
	return launch.GameInstanceSnapshot{
		IsLoading:          s.IsLoading,
		ErrorMessage:       s.ErrorMessage,
		SelectedVersionId:  s.SelectedVersionId,
		MinecraftDirectory: s.MinecraftDirectory,
		VersionIds:         s.VersionIds,
		SourcePath:         s.SourcePath,
	}
}

// emit 通过 Wails runtime 推送事件；ctx 未注入（启动前）时静默丢弃。
func emit(ctx context.Context, eventName string, payload ...interface{}) {
	if ctx == nil {
		return
	}
	wailsruntime.EventsEmit(ctx, eventName, payload...)
}

// callCtx 返回可用的 context：优先 Startup 注入的 a.ctx，未注入时回退 Background。
// 绑定方法签名不再携带 ctx（Wails 反射不支持），内部改用此函数取上下文。
func callCtx(c context.Context) context.Context {
	if c == nil {
		return context.Background()
	}
	return c
}

// New 之后、Startup 之前的事件都可能在 ctx 就绪前触发；
// 这里用 API 级共享 ctx 简化：各结构体 Startup 会同步覆盖。
// 事件桥接统一读各自结构体的 ctx。

// wireInstance download 包的宿主钩子注入（占位钩子 → 正式实现，含 host_hooks.go 的扁平化钩子）。
func (a *API) wireInstance() {
	// 启动管线接线：把 instance 存储接入 launch 的实例钩子。
	// 此前 launch.InstanceSnapshotProvider 从未被接线，快照恒为"尚未就绪"，
	// 任何启动都会失败在"游戏实例仍在扫描，请稍候。"。
	launch.InstanceSnapshotProvider = func() launch.GameInstanceSnapshot {
		return toLaunchInstanceSnapshot(instance.CurrentSnapshot())
	}
	launch.InstanceSnapshotWaiter = func(timeout time.Duration) launch.GameInstanceSnapshot {
		return toLaunchInstanceSnapshot(instance.WaitForReady(timeout))
	}
	launch.ExternalInstanceResolver = func(sourcePath string) (launch.ExternalInstanceInfo, bool) {
		external, ok := instance.TryResolveExternalInstance(sourcePath)
		if !ok {
			return launch.ExternalInstanceInfo{}, false
		}
		return launch.ExternalInstanceInfo{
			InstanceId: external.InstanceId,
			Provider:   external.Provider,
		}, true
	}
	launch.IsolatedGameDirectoryResolver = func(minecraftDirectory, sourcePath, versionId string) string {
		// 用启动快照里的目录/来源构造视图，而不是 instance.CurrentSnapshot()：
		// 校验阶段可能持续数秒，用户中途切换实例会让"实例 A 的版本 + 实例 B 的
		// 游戏目录"混搭启动（加载 B 的 mods、A 的加载器 → 直接崩）
		return instance.GameVersionIsolationGetGameDirectory(instance.GameInstanceSnapshot{
			SourcePath:         sourcePath,
			MinecraftDirectory: minecraftDirectory,
		}, versionId)
	}

	// 配置读写：download 包已直接依赖 internal/config，无需钩子注入

	// 版本继承扁平化：Loader 安装统一走 launch 包的完整实现
	// （原子写入、arguments 按段拼接、clientVersion 元字段、循环/深度校验）
	download.FlattenVersionJSONHook = func(ctx context.Context, minecraftDirectory, versionId string) error {
		_, err := launch.VersionJsonFlattener.Flatten(ctx, minecraftDirectory, versionId)
		return err
	}
	download.IsVersionReferencedHook = launch.VersionJsonFlattener.IsVersionReferenced

	// 实例扫描 / 选中钩子（GameInstanceStore）
	download.RefreshInstancesHook = func(gameDirectory string) error {
		snapshot := instance.Refresh(context.Background(), gameDirectory)
		if snapshot.ErrorMessage != "" {
			return errors.New(snapshot.ErrorMessage)
		}
		return nil
	}
	download.SelectInstanceHook = func(instanceID string) {
		instance.Select(instanceID)
	}

	// 内容目录解析钩子（GameVersionIsolation）：与启动时隔离判定一致
	// （同样用调用方给的目录/来源，不要回落到实时快照）
	download.ResolveContentDirectoryHook = func(minecraftDirectory, sourcePath, versionID string) string {
		return instance.GameVersionIsolationGetContentDirectory(instance.GameInstanceSnapshot{
			SourcePath:         sourcePath,
			MinecraftDirectory: minecraftDirectory,
		}, versionID)
	}

	// 实例基础版本 / 加载器解析钩子（X-4 更新检测）：
	// 检测侧要用它把「最新版本」限定在当前实例可用的范围内（1.21.1 + Fabric），
	// 否则会把别的 MC 版本的新版本报成本实例的更新。
	// 解析失败返回空串 → 检测侧跳过过滤，绝不编造。
	download.InstanceGameInfoHook = func(minecraftDirectory, sourcePath, versionID string) (string, string) {
		details, err := instance.LoadDetails(context.Background(), instance.GameInstanceSnapshot{
			SourcePath:         sourcePath,
			MinecraftDirectory: minecraftDirectory,
		}, versionID)
		if err != nil {
			return "", ""
		}
		gameVersion := strings.TrimSpace(details.BaseGameVersion)
		switch gameVersion {
		case "未识别", "未知", "未提供":
			gameVersion = ""
		}
		return gameVersion, strings.TrimSpace(details.LoaderName)
	}

	// Loader 安装布局解析钩子（GameInstanceLayoutResolver）
	//
	// 隔离布局是"实例目录名 → versions/<实例名>"算出来的：下载侧第 4 个参数给的是
	// 原版 MC 版本号（Fabric 实例名却是 fabric-loader-x-y 这种），拿它去算会得到
	// versions/<MC版本>，于是 Fabric API 被放进另一个目录（甚至共享目录），
	// 而游戏以 versions/<实例名> 为游戏目录启动——模组静默缺失。
	download.ResolveInstanceLayoutHook = func(targetRoot, sourcePath, instanceName, versionID string, defaultIsolation bool) string {
		instanceID := strings.TrimSpace(instanceName)
		if instanceID == "" {
			instanceID = versionID
		}
		layout := instance.ResolveLayout(targetRoot, sourcePath, instanceID, nil, &defaultIsolation)
		return layout.ContentDirectory
	}

	// 下载暂停状态 → download:pauseChanged 事件
	download.OnPauseStateChanged = func() {
		emit(a.Download.ctx, "download:pauseChanged", download.IsDownloadPaused())
	}

	// 下载任务快照 → download:progress 事件
	a.Download.service.OnChanged = func(snapshot download.GameDownloadSnapshot) {
		emit(a.Download.ctx, "download:progress", snapshot)
	}

	// 实例快照变更 → instance:changed 事件
	instance.SubscribeChanged(func(snapshot instance.GameInstanceSnapshot) {
		emit(a.Instance.ctx, "instance:changed", snapshot)
	})

	// 实例档案（独立内存 / 窗口尺寸等）变更 → config:profilesChanged 事件
	config.AddChangedHandler(func() {
		emit(a.Config.ctx, "config:profilesChanged")
	})

	// 启动快照变更 → launch:changed 事件
	a.Launcher.service.OnChanged = func(snapshot launch.GameLaunchSnapshot) {
		emit(a.Launcher.ctx, "launch:changed", snapshot)
	}

	// 逐行日志 → launch:logLine 事件（前端增量追加，不再全量轮询日志文本）
	a.Launcher.service.OnLogLine = func(tag, line string) {
		emit(a.Launcher.ctx, "launch:logLine", LogLineEvent{Tag: tag, Line: line})
	}
}

// wireMusic 音乐播放器：状态机用 Go 侧 music.Shared（重新挂上前端音频桥接），
// 实际解码输出由前端 Web Audio 完成（见 audio_bridge.go）。
func (a *API) wireMusic() {
	a.Music.library = music.NewMusicLibrary(musicConfigStore{})

	// 替换全局播放器：注入前端音频桥接（默认 Shared 构造时 audio 为 nil）
	bridge := &audioBridge{api: a.Music}
	music.Shared = music.NewMusicPlayerService(bridge)

	// 播放器状态回调 → Wails 事件
	music.Shared.OnStateChanged = func() {
		emit(a.Music.ctx, "music:stateChanged", music.Shared.State())
	}
	music.Shared.OnTrackChanged = func() {
		emit(a.Music.ctx, "music:trackChanged", music.Shared.CurrentTrack())
	}
	music.Shared.OnPlaybackModeChanged = func() {
		emit(a.Music.ctx, "music:playbackModeChanged", music.Shared.PlaybackMode())
	}
	music.Shared.OnTrackFinished = func() {
		emit(a.Music.ctx, "music:trackFinished")
	}
}

// wireModName 模组中文名缓存更新回调 → Wails 事件（前端收到后重查缓存刷新列表）。
func (a *API) wireModName() {
	modname.SetNotifier(func() {
		emit(a.Content.ctx, "modname:updated")
	})
}
