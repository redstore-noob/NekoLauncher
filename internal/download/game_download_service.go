package download

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"nekolauncher/internal/config"
	"nekolauncher/internal/download/modrinth"
	"nekolauncher/internal/models"
)

// GameDownloadPhase 下载任务阶段。
type GameDownloadPhase int

const (
	GameDownloadIdle GameDownloadPhase = iota
	GameDownloadPreparing
	GameDownloadDownloading
	GameDownloadCompleted
	GameDownloadFailed
	GameDownloadCancelled
)

// GameDownloadSnapshot 下载任务快照（对外发布下载进度的不可变状态）。
// Go 无 record with 语法：使用 Clone + 字段赋值（见 snapshotWith）。
type GameDownloadSnapshot struct {
	Revision       int64
	TaskID         int64
	Phase          GameDownloadPhase
	VersionID      string
	StageIndex     int
	StageName      string
	Detail         string
	Percentage     float64
	CompletedBytes int64
	TotalBytes     int64
	CompletedFiles int
	TotalFiles     int
	BytesPerSecond float64
}

// IdleGameDownloadSnapshot 空闲快照。
func IdleGameDownloadSnapshot() GameDownloadSnapshot {
	return GameDownloadSnapshot{
		Phase:     GameDownloadIdle,
		StageName: "尚无下载任务",
		Detail:    "请在资源下载页选择 Minecraft 版本。",
	}
}

// IsActive 任务是否活跃。
func (s GameDownloadSnapshot) IsActive() bool {
	return s.Phase == GameDownloadPreparing || s.Phase == GameDownloadDownloading
}

// clone 返回快照副本（用于 "with" 语义的字段修改）。
func (s GameDownloadSnapshot) clone() GameDownloadSnapshot { return s }

// GameDownloadService 包装 MinecraftVersionInstaller，通过阶段/快照状态机对外发布下载进度。
type GameDownloadService struct {
	// OnChanged 快照变化回调（对应 C# Changed 事件；单订阅，
	// 多订阅方需自行分发——Wails 侧一般只桥接 EventsEmit 一个出口）。
	OnChanged func(GameDownloadSnapshot)

	gate       sync.Mutex
	installer  MinecraftVersionInstaller
	modLoader  ModLoaderInstaller
	current    GameDownloadSnapshot
	activeTask context.CancelFunc
	hasActive  bool
	// warnings 本次任务期间累积的非致命警告（如 Fabric API 补装失败）。
	// 只放瞬时进度文案的话会被随后的"安装完成"终态覆盖，用户永远看不到。
	warnings []string
	revision atomic.Int64
	taskID   atomic.Int64
}

// StageNames 下载阶段名（阶段 1-7）。
var GameDownloadStageNames = []string{
	"获取版本元数据",
	"分析下载清单",
	"下载游戏客户端",
	"下载依赖库",
	"下载资源索引",
	"下载游戏资源",
	"完成校验与安装",
}

// Current 当前快照。
func (s *GameDownloadService) Current() GameDownloadSnapshot {
	s.gate.Lock()
	defer s.gate.Unlock()
	return s.current
}

func (s *GameDownloadService) init() {
	s.gate.Lock()
	if s.current.Phase == GameDownloadIdle && s.current.StageName == "" {
		s.current = IdleGameDownloadSnapshot()
	}
	s.gate.Unlock()
}

// Start 下载并安装原版 Minecraft 版本。
func (s *GameDownloadService) Start(ctx context.Context, version models.MinecraftVersion) bool {
	if strings.TrimSpace(version.ID) == "" || strings.TrimSpace(version.URL) == "" {
		return false
	}
	return s.runDownloadTask(ctx, version.ID,
		fmt.Sprintf("Minecraft %s", version.ID),
		fmt.Sprintf("Minecraft %s 下载并安装完成", version.ID),
		version.ID,
		func(targetRoot string, progress InstallProgressFunc, taskCtx context.Context) error {
			return s.installer.Install(taskCtx, version.ID, version.URL, targetRoot, progress)
		},
		nil)
}

// StartModLoader 以 Mod Loader 模式下载：先确保原版已安装，再叠加安装 Loader。
func (s *GameDownloadService) StartModLoader(
	ctx context.Context,
	version models.MinecraftVersion,
	loader ModLoaderVersion,
	instanceName string,
	skipFabricApi bool,
) bool {
	if strings.TrimSpace(instanceName) == "" {
		return false
	}
	displayName := fmt.Sprintf("%s %s (Minecraft %s)", loader.Type, loader.LoaderVersion, version.ID)
	return s.runDownloadTask(ctx, instanceName, displayName,
		fmt.Sprintf("%s 安装完成", displayName), instanceName,
		func(targetRoot string, progress InstallProgressFunc, taskCtx context.Context) error {
			return s.modLoader.Install(taskCtx, loader, instanceName, targetRoot, version.ID, progress)
		},
		func(targetRoot, sourcePath string, taskID int64, taskCtx context.Context) error {
			if skipFabricApi || loader.Type != ModLoaderFabric {
				return nil
			}
			return s.downloadFabricAPIIfNeeded(taskCtx, loader, instanceName, targetRoot, sourcePath, version.ID, taskID)
		})
}

// runDownloadTask 下载任务的公共骨架：占位活跃任务槽 → 发布"准备中" → 挂接进度转发 →
// 执行安装 → 隔离目录骨架 / 活跃目录 / 实例刷新与选中 / 终态发布 → 任务槽清理。
// 原版安装与 Loader 安装除安装动作与 Fabric API 附加步骤外完全一致，
// 抽取为一个实现避免双份流程修复时漂移。
func (s *GameDownloadService) runDownloadTask(
	ctx context.Context,
	instanceID, displayName, completedDetail, selectID string,
	install func(targetRoot string, progress InstallProgressFunc, taskCtx context.Context) error,
	afterInstall func(targetRoot, sourcePath string, taskID int64, taskCtx context.Context) error,
) bool {
	s.init()
	s.gate.Lock()
	if s.hasActive {
		s.gate.Unlock()
		return false
	}
	taskCtx, cancel := context.WithCancel(ctx)
	// 无论成功还是失败都要释放：只置空 activeTask 会把 cancelCtx 一直挂在
	// 父 ctx 的 children 上，每次下载泄一个。
	defer cancel()
	s.activeTask = cancel
	s.hasActive = true
	s.warnings = nil // 新任务清空上一轮的警告累积
	taskID := s.taskID.Add(1)
	s.gate.Unlock()

	s.publish(GameDownloadSnapshot{
		Revision:   s.nextRevision(),
		TaskID:     taskID,
		Phase:      GameDownloadPreparing,
		VersionID:  instanceID,
		StageIndex: 1,
		StageName:  GameDownloadStageNames[0],
		Detail:     fmt.Sprintf("正在准备下载 %s", displayName),
	})

	defer func() {
		s.gate.Lock()
		s.activeTask = nil
		s.hasActive = false
		s.gate.Unlock()
		// 任务终态后自动解除全局暂停：暂停是任务的 UI 状态，
		// 不能泄漏到下一次下载（未暂停时 Resume 为无副作用空操作）
		DownloadPauseGate.Resume()
	}()

	targetRoot := s.resolveTargetMinecraftDirectory()
	progress := func(update MinecraftInstallProgress) {
		if taskCtx.Err() != nil || taskID != s.taskID.Load() {
			return
		}
		s.publish(GameDownloadSnapshot{
			Revision:       s.nextRevision(),
			TaskID:         taskID,
			Phase:          GameDownloadDownloading,
			VersionID:      instanceID,
			StageIndex:     update.StageIndex,
			StageName:      update.StageName,
			Detail:         update.Detail,
			Percentage:     update.Percentage(),
			CompletedBytes: update.CompletedBytes,
			TotalBytes:     update.TotalBytes,
			CompletedFiles: update.CompletedFiles,
			TotalFiles:     update.TotalFiles,
			BytesPerSecond: update.BytesPerSecond,
		})
	}

	if err := install(targetRoot, progress, taskCtx); err != nil {
		if taskCtx.Err() != nil || ctx.Err() != nil {
			s.publishTerminal(taskID, GameDownloadCancelled, "下载已取消", displayName+" 下载已取消")
			return false
		}
		s.publishTerminal(taskID, GameDownloadFailed, "下载失败", err.Error())
		return false
	}

	// 全局默认隔离开启时，为新版本预建隔离内容目录骨架。
	if config.DefaultVersionIsolation() {
		scaffoldIsolatedContentDirectory(targetRoot, instanceID)
	}

	sourcePath := config.GameDirectory()
	if strings.TrimSpace(sourcePath) == "" {
		config.SaveGameDirectory(targetRoot)
		sourcePath = targetRoot
	}

	if afterInstall != nil {
		if err := afterInstall(targetRoot, sourcePath, taskID, taskCtx); err != nil {
			if taskCtx.Err() != nil || ctx.Err() != nil {
				s.publishTerminal(taskID, GameDownloadCancelled, "下载已取消", displayName+" 下载已取消")
				return false
			}
			s.publishTerminal(taskID, GameDownloadFailed, "下载失败", err.Error())
			return false
		}
	}

	_ = RefreshInstances(sourcePath)
	SelectInstance(selectID)

	previous := s.Current()
	completed := previous.clone()
	completed.Revision = s.nextRevision()
	completed.Phase = GameDownloadCompleted
	completed.StageIndex = StageCount
	completed.StageName = GameDownloadStageNames[len(GameDownloadStageNames)-1]
	completed.Detail = completedDetail
	// 任务级警告（如 Fabric API 补装失败）并入终态文案：只发瞬时进度的话
	// 会被这条"安装完成"覆盖，用户看到绿色对勾但实例里缺东西
	if warnings := s.takeWarnings(); len(warnings) > 0 {
		completed.Detail = completedDetail + "（警告：" + strings.Join(warnings, "；") + "）"
	}
	completed.Percentage = 100
	s.publish(completed)
	return true
}

// CancelActive 取消当前活跃下载任务。
func (s *GameDownloadService) CancelActive() bool {
	s.gate.Lock()
	defer s.gate.Unlock()
	if !s.hasActive || s.activeTask == nil {
		return false
	}
	s.activeTask()
	return true
}

// PauseActive 暂停当前活跃的下载任务：文件读取循环停在暂停门上，连接保持。
// 仅在任务活跃时生效；暂停详情会通过 OnChanged 发布。
func (s *GameDownloadService) PauseActive() bool {
	previous := s.Current()
	if !previous.IsActive() || DownloadPauseGate.IsPaused() {
		return false
	}

	DownloadPauseGate.Pause()
	updated := previous.clone()
	updated.Revision = s.nextRevision()
	updated.Detail = "已暂停（点击「继续下载」恢复）"
	s.publish(updated)
	return true
}

// ResumeActive 恢复被 PauseActive 暂停的全局下载。
func (s *GameDownloadService) ResumeActive() bool {
	if !DownloadPauseGate.IsPaused() {
		return false
	}

	previous := s.Current()
	DownloadPauseGate.Resume()
	// 恢复后安装器会在 ~120ms 内发布下一份进度，这里只刷新一次避免短暂显示过期详情
	if previous.IsActive() {
		updated := previous.clone()
		updated.Revision = s.nextRevision()
		updated.Detail = "已恢复下载"
		s.publish(updated)
	}
	return true
}

// resolveTargetMinecraftDirectory 解析下载目标根目录：
// 配置的游戏目录 → 环境变量 NEKOLAUNCHER_MINECRAFT_DIR → 默认 .minecraft；
// 若指向 versions/ 子目录则上提一级（与 C# 行为一致）。
func (s *GameDownloadService) resolveTargetMinecraftDirectory() string {
	configured := config.GameDirectory()
	if strings.TrimSpace(configured) == "" {
		configured = os.Getenv("NEKOLAUNCHER_MINECRAFT_DIR")
	}
	if strings.TrimSpace(configured) == "" {
		configured = EnsureDefaultMinecraftDirectory()
	}
	fullPath := filepath.Clean(configured)
	if strings.EqualFold(filepath.Base(fullPath), "versions") && filepath.Dir(fullPath) != fullPath {
		fullPath = filepath.Dir(fullPath)
	}

	_ = os.MkdirAll(fullPath, 0o755)
	_ = os.MkdirAll(filepath.Join(fullPath, "versions"), 0o755)
	return fullPath
}

// isolatedContentSubDirectories 版本隔离启用时的标准 Minecraft 内容子文件夹骨架。
// 游戏启动时会以该目录作为 --gameDir，提前创建可避免首次运行时目录缺失。
var isolatedContentSubDirectories = []string{
	"saves", "resourcepacks", "mods", "config",
	"crash-reports", "logs", "screenshots", "shaderpacks",
}

func scaffoldIsolatedContentDirectory(minecraftRoot, versionID string) {
	versionDirectory := filepath.Join(minecraftRoot, "versions", versionID)
	if info, err := os.Stat(versionDirectory); err != nil || !info.IsDir() {
		return
	}
	for _, sub := range isolatedContentSubDirectories {
		_ = os.MkdirAll(filepath.Join(versionDirectory, sub), 0o755)
	}
}

// fabricAPIProjectID Modrinth 上的 Fabric API 项目 ID（任何 Fabric 实例都需要的运行库）。
const fabricAPIProjectID = "P7dR8mSH"

const fabricAPIDisplayName = "Fabric API"

// downloadFabricAPIIfNeeded Fabric 实例安装完成后，默认把 Fabric API 一起下载到
// 该实例的 mods 目录。失败不阻断整个安装流程，仅作为警告提示。
func (s *GameDownloadService) downloadFabricAPIIfNeeded(
	ctx context.Context,
	loader ModLoaderVersion,
	instanceName, targetRoot, sourcePath, gameVersion string,
	taskID int64,
) error {
	if loader.Type != ModLoaderFabric {
		return nil
	}

	// 解析该实例实际的内容目录（版本隔离 / 共享布局 / 外部实例均可正确命中）。
	// 全局默认只作兜底传入，不覆盖自动检测（与 GameVersionIsolation.Resolve 语义一致）。
	var layoutContentDirectory string
	if ResolveInstanceLayoutHook != nil {
		layoutContentDirectory = ResolveInstanceLayoutHook(
			targetRoot, sourcePath, instanceName, gameVersion, config.DefaultVersionIsolation())
	}
	if strings.TrimSpace(layoutContentDirectory) == "" {
		layoutContentDirectory = targetRoot
	}
	modsDir := filepath.Join(layoutContentDirectory, "mods")
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		return err
	}

	publishStage := func(detail string) {
		s.publish(GameDownloadSnapshot{
			Revision:   s.nextRevision(),
			TaskID:     taskID,
			Phase:      GameDownloadDownloading,
			VersionID:  instanceName,
			StageIndex: StageCount,
			StageName:  "下载 Fabric API",
			Detail:     detail,
		})
	}
	// 失败同时记入任务警告：随终态快照一起展示，否则会在数秒内被
	// "安装完成"覆盖，实例缺 Fabric API 用户无从知晓。
	warn := func(detail string) {
		s.addWarning(detail)
		publishStage(detail)
	}

	publishStage(fmt.Sprintf("正在下载 %s…", fabricAPIDisplayName))

	versions, err := modrinthGetVersionsForLoader(ctx, fabricAPIProjectID, gameVersion)
	if err != nil {
		if ctx.Err() != nil {
			// 用户取消：不能伪装成"下载失败仅警告"，否则取消后任务仍按完成收尾
			return ctx.Err()
		}
		warn(fmt.Sprintf("%s 下载失败，请稍后手动安装：%v", fabricAPIDisplayName, err))
		return nil
	}

	var latest *fabricAPIFile
	for i := range versions {
		if primary := versions[i].PrimaryFile(); primary != nil && strings.TrimSpace(primary.URL) != "" {
			latest = &fabricAPIFile{filename: primary.Filename, url: primary.URL}
			break
		}
	}
	if latest == nil {
		warn(fmt.Sprintf("%s 无可用版本，已跳过。", fabricAPIDisplayName))
		return nil
	}

	targetPath := filepath.Join(modsDir, sanitizeFileName(latest.filename))
	if _, err := os.Stat(targetPath); err == nil {
		return nil
	}

	if err := DownloadFileToPath(ctx, latest.url, targetPath, nil); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		warn(fmt.Sprintf("%s 下载失败，请稍后手动安装：%v", fabricAPIDisplayName, err))
		return nil
	}
	publishStage(fmt.Sprintf("%s 已下载至 mods/。", fabricAPIDisplayName))
	return nil
}

// addWarning 记录一条任务级非致命警告，随"安装完成"终态快照一起下发。
// 持 s.gate 调用与读取。
func (s *GameDownloadService) addWarning(text string) {
	s.gate.Lock()
	s.warnings = append(s.warnings, text)
	s.gate.Unlock()
}

// takeWarnings 取走当前累积的警告（任务收尾时调用，避免跨任务泄漏）。
func (s *GameDownloadService) takeWarnings() []string {
	s.gate.Lock()
	warnings := s.warnings
	s.warnings = nil
	s.gate.Unlock()

	return warnings
}

type fabricAPIFile struct {
	filename string
	url      string
}

func (s *GameDownloadService) publishTerminal(taskID int64, phase GameDownloadPhase, stageName, detail string) {
	previous := s.Current()
	if previous.TaskID != taskID {
		return
	}
	updated := previous.clone()
	updated.Revision = s.nextRevision()
	updated.Phase = phase
	updated.StageName = stageName
	updated.Detail = detail
	s.publish(updated)
}

func (s *GameDownloadService) nextRevision() int64 { return s.revision.Add(1) }

// publish 发布快照。快照可能来自多个并行下载线程：序号必须在锁内分配并写入，
// 否则线程 A 拿到旧序号却后进锁，会把新快照覆盖回旧状态。
func (s *GameDownloadService) publish(snapshot GameDownloadSnapshot) {
	if snapshot.Revision == 0 {
		snapshot.Revision = s.nextRevision()
	}
	s.gate.Lock()
	if snapshot.Revision < s.current.Revision {
		s.gate.Unlock()
		return
	}
	s.current = snapshot
	handler := s.OnChanged
	s.gate.Unlock()

	if handler != nil {
		// 订阅者异常不影响发布方（对应 C# 逐订阅者 try/catch）
		func() {
			defer func() { _ = recover() }()
			handler(snapshot)
		}()
	}
}

// modrinthGetVersionsForLoader 查询 Fabric API 在指定 MC 版本 + fabric loader 下的版本列表。
func modrinthGetVersionsForLoader(ctx context.Context, projectID, gameVersion string) ([]models.ModrinthVersion, error) {
	return modrinth.GetVersionsForCombo(ctx, projectID, gameVersion, "fabric")
}
