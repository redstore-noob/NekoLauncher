package launch

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nekolauncher/internal/auth"
	"nekolauncher/internal/config"
	"nekolauncher/internal/logs"
)

// GameLaunchPhase 启动生命周期阶段。
type GameLaunchPhase int

const (
	GameLaunchPhaseIdle GameLaunchPhase = iota
	GameLaunchPhasePreparing
	GameLaunchPhaseRunning
	GameLaunchPhaseFailed
	GameLaunchPhaseExited
)

// GameLaunchSnapshot 某一时刻的启动状态视图；不可变，发布后不再修改。
type GameLaunchSnapshot struct {
	Revision    int64
	Phase       GameLaunchPhase
	Title       string
	Message     string
	VersionId   string
	AccountName string
	ProcessId   int
	// ExitCode 游戏进程退出码。仅 Phase==Exited 时有意义（其它阶段为 0）；
	// 结构化下发，前端不再解析中文文案来还原退出码。
	ExitCode int
	// StoppedManually 是否由用户手动停止（Kill 的退出码非 0，不能当异常报）。
	StoppedManually bool
}

// ShouldShowIndicator 是否应显示"游戏运行中"指示器。
func (s GameLaunchSnapshot) ShouldShowIndicator() bool {
	return s.Phase == GameLaunchPhasePreparing || s.Phase == GameLaunchPhaseRunning
}

// idleSnapshot 尚未发起任何启动时的初始快照。
func idleSnapshot() GameLaunchSnapshot {
	return GameLaunchSnapshot{
		Revision: 0,
		Phase:    GameLaunchPhaseIdle,
		Title:    "尚未启动游戏",
		Message:  "选择账号和游戏实例后即可启动。",
	}
}

// maximumLogLines 内存日志的行数上限，超出后丢弃最早的行。
const maximumLogLines = 2000

// GameLaunchService 启动页与组件共用的唯一活动启动管线。生命周期状态以不可变快照
// 发布；进程输出保留在有界内存日志中供日志窗口查看。
type GameLaunchService struct {
	gate     sync.Mutex
	logLines []string
	current  GameLaunchSnapshot
	revision int64
	launchId int64
	// launchInProgress 0/1 启动互斥标记：同一时间只允许一次启动尝试。
	launchInProgress atomic.Int32
	// prepareStopRequested 准备阶段的取消请求（TryStopGame 在 Preparing 时置位）。
	// launch 在各阶段边界检查：校验补全/凭据刷新/Java 下载都可能耗时数分钟。
	prepareStopRequested atomic.Bool

	gameProcess   *exec.Cmd
	stopRequested bool

	// accounts 账号存储（可注入，默认取全局共享实例），便于测试与多存储目录隔离。
	accounts *auth.AccountStoreService
	// offlineLauncher / microsoftLauncher 启动管线实现。
	offlineLauncher   IOfflineMinecraftLauncher
	microsoftLauncher IMicrosoftMinecraftLauncher

	// OnChanged 快照变更回调（对应 C# Changed 多播事件；
	// Go 移植为单回调字段，多订阅方需自行分发——语义偏离见 PORTING_NOTES.md）。
	OnChanged func(GameLaunchSnapshot)

	// OnLogLine 逐行日志回调：游戏进程 stdout/stderr 每出一行就回调一次
	// （tag 为 "GAME"），启动器自身的阶段日志回调 tag 为 "LAUNCH"。
	//
	// 与 OnChanged 的区别：快照只在阶段切换时变，日志窗口过去只能靠
	// GetLogText() 全量轮询（长日志每次重传 2000 行）。这里把行直接推给前端，
	// 轮询退化为"重连兜底"。回调在日志追加之后、快照发布之前触发。
	OnLogLine func(tag, line string)

	// lastProvenance 最近一次成功启动的参数溯源报告（未采集时为 nil）。
	// 与 gameProcess 同锁保护：读它的面板与启动流程是并发的。
	lastProvenance *LaunchProvenanceReport
	// lastLaunchVersionId 上述报告对应的版本 id（面板标题用）。
	lastLaunchVersionId string
}

// LastProvenance 返回最近一次成功启动的参数溯源报告；从未启动过时为 nil。
func (s *GameLaunchService) LastProvenance() *LaunchProvenanceReport {
	s.gate.Lock()
	defer s.gate.Unlock()
	return s.lastProvenance
}

// LastLaunchVersionId 返回最近一次成功启动的版本 id（无记录时为空串）。
func (s *GameLaunchService) LastLaunchVersionId() string {
	s.gate.Lock()
	defer s.gate.Unlock()
	return s.lastLaunchVersionId
}

// NewGameLaunchService 构造启动服务；accountStore 为空时使用 auth.Shared。
func NewGameLaunchService(accountStore *auth.AccountStoreService) *GameLaunchService {
	if accountStore == nil {
		accountStore = auth.Shared
	}
	return &GameLaunchService{
		accounts:          accountStore,
		current:           idleSnapshot(),
		offlineLauncher:   NewOfflineMinecraftLauncher(nil),
		microsoftLauncher: NewMicrosoftMinecraftLauncher(nil),
	}
}

// Current 返回当前快照。
func (s *GameLaunchService) Current() GameLaunchSnapshot {
	s.gate.Lock()
	defer s.gate.Unlock()
	return s.current
}

// GetLogText 返回内存日志全文。
func (s *GameLaunchService) GetLogText() string {
	s.gate.Lock()
	defer s.gate.Unlock()
	return strings.Join(s.logLines, "\n")
}

// LaunchSelected 启动当前选中的实例与账号（对应 C# LaunchSelectedAsync）。
// serverHost/serverPort 非空时直接进服，worldName 非空时直接进存档。
func (s *GameLaunchService) LaunchSelected(
	ctx context.Context,
	serverHost string,
	serverPort *int,
	worldName string,
) LaunchResult {
	return s.launch(ctx, "", serverHost, serverPort, worldName)
}

// LaunchExplicit 以显式版本启动，不改变用户"当前选中"的实例（插件 API 使用）。
// 显式路径只为本次启动临时改写快照选中：不落存储、不发 changed 事件。
func (s *GameLaunchService) LaunchExplicit(
	ctx context.Context,
	versionID, serverHost string,
	serverPort *int,
	worldName string,
) LaunchResult {
	return s.launch(ctx, versionID, serverHost, serverPort, worldName)
}

// launch 统一启动入口：explicitVersionID 非空时按显式版本启动，为空时按当前选中。
func (s *GameLaunchService) launch(
	ctx context.Context,
	explicitVersionID, serverHost string,
	serverPort *int,
	worldName string,
) LaunchResult {
	// 原子占位：结束（含异常）时在 finally 释放
	if !s.launchInProgress.CompareAndSwap(0, 1) {
		// 仅记日志不发布失败快照：此时另一个启动正在跑，发布会覆盖它的状态
		s.appendLog("拒绝重复启动：已有启动流程进行中。", "LAUNCH")
		return FailedLaunch("游戏正在启动，请稍候。")
	}
	defer s.launchInProgress.Store(0)
	// 上一次启动残留的取消请求（极窄窗口）不带入本次
	s.prepareStopRequested.Store(false)

	// 早期守卫统一走 fail：写文件日志 + 发布失败快照。
	// 此前这些路径只返回结果不发快照——前端只在按钮下方显示一行小字、
	// 日志文件毫无痕迹，用户"点了启动没反应"时无从排查。
	fail := func(message string) LaunchResult {
		s.appendLog(fmt.Sprintf("启动失败：%s", message), "LAUNCH")
		s.publishFailure("启动失败", message, 0)
		return FailedLaunch(message)
	}

	// 前置校验：进程未运行、实例扫描完成、目录有效、已选实例，
	// 且不是暂不支持直接启动的外部启动器实例。
	if s.isProcessRunning() {
		// 游戏确实在运行：快照已是运行态，不发失败事件覆盖它
		s.appendLog("拒绝启动：游戏已经在运行。", "LAUNCH")
		return FailedLaunch("游戏已经在运行。")
	}
	// 首次扫描在 Startup 异步执行：等待其就绪（旧移植版此处直接拒绝，
	// 且快照钩子未接线，导致每次启动都失败在"仍在扫描"）。
	snap := waitForInstanceSnapshot()
	if snap.IsLoading {
		return fail("游戏实例仍在扫描，请稍候。")
	}
	if strings.TrimSpace(snap.ErrorMessage) != "" {
		return fail(fmt.Sprintf("Minecraft 目录无效：%s", snap.ErrorMessage))
	}
	// 显式版本启动（插件 API）：先校验版本存在（与实例存储同一套大小写不敏感
	// 匹配，杜绝把任意字符串/路径穿越串送进启动管线），再只在本次启动内改写
	// 快照选中——不落存储、不发 changed 事件，用户"当前选中"保持原样。
	if explicitVersionID != "" {
		var match string
		for _, id := range snap.VersionIds {
			if strings.EqualFold(id, explicitVersionID) {
				match = id
				break
			}
		}
		if match == "" {
			return fail(fmt.Sprintf("找不到游戏实例：%s", explicitVersionID))
		}
		snap.SelectedVersionId = match
	}
	if strings.TrimSpace(snap.SelectedVersionId) == "" ||
		strings.TrimSpace(snap.MinecraftDirectory) == "" {
		return fail("请先选择一个已安装的游戏实例。")
	}

	// 外部启动器（MultiMC/CurseForge 等）的实例可管理内容，但暂不能直接启动
	if external, ok := resolveExternalInstance(snap.SourcePath); ok &&
		strings.EqualFold(external.InstanceId, snap.SelectedVersionId) {
		return fail(fmt.Sprintf(
			"已识别 %s 实例并可管理其内容，但其原生版本补丁元数据暂不能由 NekoLauncher 直接启动。",
			external.Provider))
	}

	// 校验已选账号
	selectedAccount := s.accounts.Selected()
	if selectedAccount == nil {
		if len(s.accounts.Current()) == 0 {
			return fail("请先添加并选择一个账号。")
		}
		return fail("请先选择账号。")
	}

	// prepareCancelled 准备阶段取消判定：用户点"停止"（prepareStopRequested）
	// 或外层 ctx 取消（插件/自动化取消启动）都算。各耗时步骤的边界都会检查。
	prepareCancelled := func() bool {
		return ctx.Err() != nil || s.prepareStopRequested.Load()
	}
	abortCancelled := func() LaunchResult {
		s.appendLog("启动操作已取消。", "LAUNCH")
		s.publishFailure("启动已取消", "游戏启动操作已取消。", 0)
		return FailedLaunch("游戏启动已取消。")
	}

	versionId := snap.SelectedVersionId
	launchId := atomic.AddInt64(&s.launchId, 1)
	s.resetLog()
	s.appendLog(fmt.Sprintf("准备启动 Minecraft %s。", versionId), "LAUNCH")
	s.publishPreparing(snap, selectedAccount, "正在准备账号与 Java 启动参数…")

	// 新实例首次启动：options.txt 还没有 lang 时按系统语言写入（已有则不动）
	s.syncInstanceLanguage(snap, versionId)

	// 启动前按配置校验并补全缺失的游戏文件；失败只记录日志，不阻断启动。
	if config.VerifyFilesBeforeLaunch() {
		s.appendLog("正在校验游戏文件完整性…", "LAUNCH")
		s.publishPreparing(snap, selectedAccount, "正在校验游戏文件完整性…")
		var verifier downloadVerifier
		repaired, err := verifier.verifyAndRepair(ctx, snap.MinecraftDirectory, versionId,
			// 与启动参数装配同一来源：实例独立窗口尺寸，未跟随全局时取全局值
			effectiveHasCustomResolution(snap.MinecraftDirectory, versionId),
			func(status string) {
				s.appendLog(status, "LAUNCH")
			})
		if err != nil {
			// 校验失败不阻断启动：缺失文件由游戏侧自行暴露
			s.appendLog(fmt.Sprintf("文件校验异常（将继续启动）：%v", err), "LAUNCH")
		} else if repaired > 0 {
			s.appendLog(fmt.Sprintf("文件校验完成，已补全 %d 项缺失文件。", repaired), "LAUNCH")
		} else {
			s.appendLog("文件校验完成，所有文件正常。", "LAUNCH")
		}
	}
	// 校验补全里没有 ctx 检查点（下载器有自己的超时），这里统一收口
	if prepareCancelled() {
		s.prepareStopRequested.Store(false)
		return abortCancelled()
	}

	// 校验所选账号凭据并准备启动用账号对象（微软账号可能要走一轮刷新，
	// 数十秒级）；此后 Java 下载、存档还原点都在 runLauncher 内完成。
	s.publishPreparing(snap, selectedAccount, "正在校验账号凭据…")
	launchAccount, prepareErr := s.prepareAccount(ctx, selectedAccount)
	if prepareErr != nil {
		if prepareCancelled() {
			s.prepareStopRequested.Store(false)
			return abortCancelled()
		}
		return s.reportPrepareFailure(prepareErr)
	}
	if prepareCancelled() {
		s.prepareStopRequested.Store(false)
		return abortCancelled()
	}

	s.publishPreparing(snap, selectedAccount, "正在准备 Java 运行时与存档还原点…")
	options, optionsErr := s.buildLaunchOptions(snap, versionId, launchAccount, serverHost, serverPort, worldName)
	if optionsErr != nil {
		s.appendLog(fmt.Sprintf("启动失败：%v", optionsErr), "LAUNCH")
		s.publishFailure("启动失败", optionsErr.Error(), 0)
		return FailedLaunch(optionsErr.Error())
	}
	if prepareCancelled() {
		s.prepareStopRequested.Store(false)
		return abortCancelled()
	}

	return s.runLauncher(selectedAccount, launchAccount, *options, launchId)
}

// reportPrepareFailure 发布账号准备失败的快照并返回失败结果。
// RotatedCredentialsError 属于特殊路径：凭据已刷新但档案交换失败，
// 刷新锁内已拿到轮换后的新凭据（在 prepareAccount 内先落库再返回错误）。
func (s *GameLaunchService) reportPrepareFailure(prepareErr error) LaunchResult {
	message := prepareErr.Error()
	s.appendLog(fmt.Sprintf("启动失败：%s", message), "LAUNCH")
	s.publishFailure("启动失败", message, 0)
	return FailedLaunch(message)
}

// prepareAccount 校验所选账号凭据并返回启动用账号对象：微软账号走凭据刷新，
// 皮肤站账号走令牌校验/刷新，离线账号直接构造。
func (s *GameLaunchService) prepareAccount(
	ctx context.Context,
	selectedAccount *auth.LaunchAccount,
) (MinecraftAccount, error) {
	switch selectedAccount.Type {
	case "microsoft":
		return s.prepareMicrosoftAccount(ctx, selectedAccount)
	case "authlib":
		return s.prepareAuthlibAccount(ctx, selectedAccount)
	default:
		s.appendLog("已准备离线账号。", "LAUNCH")
		return NewOfflineAccount(fallbackOfflineName(selectedAccount))
	}
}

func fallbackOfflineName(selectedAccount *auth.LaunchAccount) string {
	if strings.TrimSpace(selectedAccount.OfflineName) != "" {
		return selectedAccount.OfflineName
	}
	return "Player_01"
}

// prepareMicrosoftAccount 微软账号：与皮肤/档案服务共用同一把按账号的刷新锁，
// 锁内重读最新凭据，避免与并发刷新先后用同一个 refresh_token
// （轮换策略下会强制下线）。
func (s *GameLaunchService) prepareMicrosoftAccount(
	ctx context.Context,
	selectedAccount *auth.LaunchAccount,
) (MinecraftAccount, error) {
	s.appendLog("正在校验正版账号凭据。", "LAUNCH")

	var validated *auth.MicrosoftAccount
	var validateErr error
	lockErr := s.accounts.WithRefreshLock(selectedAccount, func() error {
		// 锁内重读最新凭据：等待锁的期间可能已有并发刷新写入轮换后的令牌
		current := selectedAccount.Microsoft
		if current == nil {
			return newMicrosoftCredentialsError("账号缺少正版凭据，请重新登录。")
		}
		result, err := auth.MicrosoftAuthentication.Validate(ctx, *current)
		if err != nil {
			// 刷新锁内已拿到轮换后的新凭据：先落库再返回错误，由上层提示重新登录
			var rotated *auth.RotatedCredentialsError
			if asRotatedCredentials(err, &rotated) {
				rotatedCopy := rotated.RefreshedAccount
				s.accounts.UpdateMicrosoftAccount(selectedAccount, &rotatedCopy)
			}
			validateErr = err
			return err
		}
		validated = &result
		return nil
	})
	if lockErr != nil {
		if validateErr != nil {
			return nil, validateErr
		}
		return nil, lockErr
	}

	// 通过账号存储更新，确保 UI 订阅者收到变更通知；
	// 账号可能在凭据校验的网络往返期间被删除，失败不阻断启动
	if err := s.accounts.UpdateMicrosoftAccount(selectedAccount, validated); err != nil {
		s.appendLog(fmt.Sprintf("凭据写回失败：%v", err), "LAUNCH")
	}
	s.appendLog("正版账号凭据校验完成。", "LAUNCH")
	accountCopy := *validated
	return &accountCopy, nil
}

// prepareAuthlibAccount 皮肤站账号：校验访问令牌，过期时自动刷新并写回账号存储。
// 凭据数据缺失时回退为离线账号。
func (s *GameLaunchService) prepareAuthlibAccount(
	ctx context.Context,
	selectedAccount *auth.LaunchAccount,
) (MinecraftAccount, error) {
	if selectedAccount.Authlib == nil {
		s.appendLog("已准备离线账号。", "LAUNCH")
		return NewOfflineAccount(fallbackOfflineName(selectedAccount))
	}

	s.appendLog("正在校验皮肤站账号凭据。", "LAUNCH")
	var credential auth.AuthlibCredential
	var accessToken string
	var validateErr error
	lockErr := s.accounts.WithRefreshLock(selectedAccount, func() error {
		// 锁内重读最新凭据：等待锁的期间可能已有并发刷新写入轮换后的令牌
		//（与 prepareMicrosoftAccount 同一口径；锁外拷贝会用已被作废的旧令牌校验）
		current := selectedAccount.Authlib
		if current == nil {
			validateErr = errors.New("账号缺少皮肤站凭据，请重新登录。")
			return validateErr
		}
		credential = *current
		token, err := auth.DefaultAuthlibAuthenticator.ValidateOrRefresh(ctx, &credential, "")
		if err != nil {
			validateErr = err
			return err
		}
		accessToken = token
		return nil
	})
	if lockErr != nil {
		if validateErr != nil {
			return nil, validateErr
		}
		return nil, lockErr
	}

	if accessToken != credential.AccessToken {
		// 令牌已刷新：通过账号存储更新，确保 UI 订阅者收到变更通知
		credential.AccessToken = accessToken
		s.accounts.UpdateAuthlibAccount(selectedAccount, &credential)
		s.appendLog("皮肤站令牌已自动刷新。", "LAUNCH")
	}

	s.appendLog("皮肤站账号凭据校验完成。", "LAUNCH")
	return auth.AuthlibAccount{
		ProfileName: credential.ProfileName,
		ProfileUuid: credential.ProfileUuid,
		AccessToken: accessToken,
		ApiRoot:     credential.ApiRoot,
	}, nil
}

// runLauncher 启动 Java 进程并发布"运行中"快照，随后交由进程观察器接管退出事件。
func (s *GameLaunchService) runLauncher(
	selectedAccount *auth.LaunchAccount,
	launchAccount MinecraftAccount,
	options MinecraftLaunchOptions,
	launchId int64,
) LaunchResult {
	s.appendLog("正在解析版本、依赖库与 Java 运行时。", "LAUNCH")
	// 启动前留还原点（时间机器）：在进程真正拉起之前，此刻的磁盘状态就是
	// "玩家上一次退出的样子"。失败只记日志，绝不阻断启动。
	if notice := runPreLaunchSnapshot(options.GameDirectory, options.VersionId); notice != "" {
		s.appendLog(notice, "LAUNCH")
	}
	// 微软账号走正版分支（带在线会话校验），其余账号走离线/第三方分支
	var result *MinecraftLaunchResult
	var err error
	if microsoft, ok := launchAccount.(*auth.MicrosoftAccount); ok {
		result, err = s.microsoftLauncher.Launch(context.Background(), microsoft, options)
	} else {
		result, err = s.offlineLauncher.Launch(context.Background(), options)
	}
	if err != nil {
		message := err.Error()
		s.appendLog(fmt.Sprintf("启动失败：%s", message), "LAUNCH")
		s.publishFailure("启动失败", message, 0)
		return FailedLaunch(message)
	}

	// 注册进程句柄：isProcessRunning / TryStopGame / 退出观察者（sameLaunch 判定）
	// 都依赖该引用；缺失会导致停止按钮失效、重复启动被放行、退出快照永不发布。
	s.gate.Lock()
	s.gameProcess = result.Cmd
	// 记录本次启动的参数溯源，供"为什么这样启动"面板在运行中/退出后查看
	s.lastProvenance = result.Provenance
	s.lastLaunchVersionId = result.VersionId
	s.gate.Unlock()

	javaHint := describeJavaRequirement(result.RequiredJavaMajorVersion)
	s.appendLog(fmt.Sprintf("Java 进程已启动，进程 ID：%d。", result.Pid()), "LAUNCH")
	s.publish(GameLaunchSnapshot{
		Revision:    s.nextRevision(),
		Phase:       GameLaunchPhaseRunning,
		Title:       fmt.Sprintf("%s 正在运行", result.VersionId),
		Message:     fmt.Sprintf("账号：%s · %s", result.Username, javaHint),
		VersionId:   result.VersionId,
		AccountName: selectedAccount.DisplayName,
		ProcessId:   result.Pid(),
	})

	// 进程观察与退出收尾（对应 C# PrepareProcessObservation / CompleteProcessExit）
	// startedAt 供退出时结算游玩时长（写入实例档案的累计统计）
	s.observeProcess(result, launchId, options.MinecraftDirectory, time.Now())
	return CompletedLaunch(fmt.Sprintf("已启动 %s。", result.VersionId))
}

// describeJavaRequirement 把版本 JSON 里的 Java 主版本号翻译成用户可读的要求说明。
func describeJavaRequirement(requiredJavaMajorVersion *int) string {
	if requiredJavaMajorVersion != nil {
		return fmt.Sprintf("至少需要 Java %d", *requiredJavaMajorVersion)
	}
	return "Java 版本要求已满足"
}

// ---------------------------------------------------------------------------
// 快照与日志管线
// ---------------------------------------------------------------------------

// publishPreparing 发布"准备中"阶段快照。
func (s *GameLaunchService) publishPreparing(
	instance GameInstanceSnapshot,
	account *auth.LaunchAccount,
	message string,
) {
	s.publish(GameLaunchSnapshot{
		Revision:    s.nextRevision(),
		Phase:       GameLaunchPhasePreparing,
		Title:       fmt.Sprintf("正在启动 %s", instance.SelectedVersionId),
		Message:     message,
		VersionId:   instance.SelectedVersionId,
		AccountName: account.DisplayName,
	})
}

// publishFailure 发布失败快照：保留上一次的版本与账号信息便于定位。
func (s *GameLaunchService) publishFailure(title, message string, processId int) {
	lastSnapshot := s.Current()
	s.publish(GameLaunchSnapshot{
		Revision:    s.nextRevision(),
		Phase:       GameLaunchPhaseFailed,
		Title:       title,
		Message:     message,
		VersionId:   lastSnapshot.VersionId,
		AccountName: lastSnapshot.AccountName,
		ProcessId:   processId,
	})
}

func (s *GameLaunchService) nextRevision() int64 {
	return atomic.AddInt64(&s.revision, 1)
}

func (s *GameLaunchService) resetLog() {
	s.gate.Lock()
	s.logLines = nil
	s.gate.Unlock()
}

func (s *GameLaunchService) appendLog(line, logType string) {
	if strings.TrimSpace(line) == "" {
		return
	}

	// 内存日志与日志文件共用同一条脱敏结果，保证两边口径一致。
	redacted := RedactSecrets(line)
	stamped := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), redacted)
	s.gate.Lock()
	s.logLines = append(s.logLines, stamped)
	if len(s.logLines) > maximumLogLines {
		s.logLines = s.logLines[len(s.logLines)-maximumLogLines:]
	}
	handler := s.OnLogLine
	s.gate.Unlock()

	// 逐行推送（tag = LAUNCH / GAME），前端据此增量追加；在 gate 之外回调，
	// 避免订阅方（Wails 事件）拖住日志写入与快照查询。
	if handler != nil {
		handler(logType, stamped)
	}

	// 文件写入放在 gate 之外：logs.Write 内部有独立写锁并做磁盘 I/O，
	// 持 gate 写文件会阻塞快照查询与 stdout 读取线程。
	logs.Write(logType, redacted)
}

// 凭据脱敏正则组：--accessToken 参数、旧版 token:<token>:<uuid> 会话、Bearer 头。
var (
	accessTokenArgumentPattern = regexp.MustCompile(`--accessToken\s+\S+`)
	legacySessionTokenPattern  = regexp.MustCompile(`token:[^:\s"]{8,}(:[0-9a-fA-F]{32})`)
	bearerTokenPattern         = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]{16,}`)
)

// RedactSecrets 凭据脱敏：游戏 stdout 或后续日志点若带入访问令牌
// （--accessToken 参数、旧版 token:<token>:<uuid> 会话、Bearer 头），
// 进入内存日志前统一打码；玩家 UUID 非敏感，保留以便排查。
func RedactSecrets(line string) string {
	masked := accessTokenArgumentPattern.ReplaceAllString(line, "--accessToken ***")
	masked = legacySessionTokenPattern.ReplaceAllString(masked, "token:***$1")
	masked = bearerTokenPattern.ReplaceAllString(masked, "Bearer ***")
	return masked
}

// publish 更新当前快照并通知订阅者；回调异常被隔离，不扩散。
func (s *GameLaunchService) publish(snapshot GameLaunchSnapshot) {
	s.gate.Lock()
	s.current = snapshot
	handler := s.OnChanged
	s.gate.Unlock()

	if handler == nil {
		return
	}
	func() {
		defer func() {
			// 单个订阅者异常不扩散
			_ = recover()
		}()
		handler(snapshot)
	}()
}

// newMicrosoftCredentialsError 微软凭据缺失的错误包装。
func newMicrosoftCredentialsError(message string) error {
	return fmt.Errorf("%s", message)
}

// asRotatedCredentials errors.As 包装（独立函数便于将来替换错误语义）。
// 用 errors.As 而非直接断言：错误可能被中间层 wrap。
func asRotatedCredentials(err error, target **auth.RotatedCredentialsError) bool {
	return errors.As(err, target)
}
