package launch

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"nekolauncher/internal/auth"
)

// MinecraftAccount 可供 Minecraft 启动使用的统一账号抽象。
// C# 中接口定义在 Launch 命名空间；Go 版实际定义在 auth 包（避免循环依赖），此处别名。
type MinecraftAccount = auth.MinecraftAccount

// offlineUsernamePattern 离线用户名：1–16 位，只能包含英文字母、数字和下划线。
var offlineUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,16}$`)

// OfflineAccount 不包含任何访问令牌的离线 Minecraft 账号。
type OfflineAccount struct {
	username string
	// uuid 与服务端离线模式一致的 32 位无连字符 UUID。
	uuid string
}

// NewOfflineAccount 构造离线账号；用户名非法时返回错误。
func NewOfflineAccount(username string) (*OfflineAccount, error) {
	normalized := strings.TrimSpace(username)
	if !offlineUsernamePattern.MatchString(normalized) {
		return nil, fmt.Errorf("离线用户名必须为 1–16 位，只能包含英文字母、数字和下划线。（输入：%q）", username)
	}
	return &OfflineAccount{
		username: normalized,
		uuid:     offlineUuid(normalized),
	}, nil
}

// MustOfflineAccount 构造离线账号；用户名非法时 panic（仅用于内部已校验路径）。
func MustOfflineAccount(username string) *OfflineAccount {
	account, err := NewOfflineAccount(username)
	if err != nil {
		panic(err)
	}
	return account
}

// 以下方法实现 MinecraftAccount（auth 包）接口：离线账号无令牌，访问令牌固定为 "0"。
func (a *OfflineAccount) AccountKind() string        { return "offline" }
func (a *OfflineAccount) AccountUsername() string    { return a.username }
func (a *OfflineAccount) AccountUuid() string        { return a.uuid }
func (a *OfflineAccount) AccountAccessToken() string { return "0" }
func (a *OfflineAccount) AccountUserType() string    { return "legacy" }
func (a *OfflineAccount) AccountXboxUserId() string  { return "" }

// offlineUuid 计算 OfflinePlayer:{name} 的 UUID v3（Java UUID.nameUUIDFromBytes
// 语义：MD5 + 设置 RFC 4122 version/variant 位），输出 32 位无连字符小写。
func offlineUuid(username string) string {
	hash := md5.Sum([]byte("OfflinePlayer:" + username))
	bytes := hash[:]
	bytes[6] = (bytes[6] & 0x0f) | 0x30
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return hex.EncodeToString(bytes)
}

// MinecraftLaunchOptions 离线启动所需的外部配置。版本文件、依赖库和资源应已存在于游戏目录。
type MinecraftLaunchOptions struct {
	// MinecraftDirectory Minecraft 根目录（.minecraft）。
	MinecraftDirectory string
	// GameDirectory 可选的实例游戏目录，用于隔离 mods、config、saves 等内容。
	// 为空时使用 MinecraftDirectory。
	GameDirectory string
	// VersionId 要启动的版本 ID。
	VersionId string
	// Account 启动使用的账号；可为离线账号或正版（Microsoft）账号。
	Account MinecraftAccount
	// JavaExecutable 可选的 Java 可执行文件。为空时依次检查 NEKOLAUNCHER_JAVA、
	// JAVA_HOME 和 PATH。
	JavaExecutable string
	// JavaRuntimeDirectory 可选的 Minecraft runtime 根目录；启动器会递归查找并
	// 选择版本要求的 Java。
	JavaRuntimeDirectory    string
	MinimumMemoryMb         int
	MaximumMemoryMb         int
	WindowWidth             int
	WindowHeight            int
	LauncherName            string
	LauncherVersion         string
	AdditionalJvmArguments  []string
	AdditionalGameArguments []string
	// ProcessPriority 游戏进程优先级（"low"/"belownormal"/"abovenormal"/"high"，
	// 空串与 "normal" 表示不调整）；进程启动后尽力设置，失败仅记录日志。
	ProcessPriority string
	// WrapperCommand 包装命令模板（含 %command% 占位）；空串表示直接启动 Java。
	WrapperCommand string
	// EnvironmentVariables 注入游戏进程的额外环境变量（用户实例/全局设置）；
	// 与插件 Transform 的环境变量合并，用户设置优先。
	EnvironmentVariables map[string]string
	// LaunchFullscreen 以全屏启动（buildLaunchOptions 已转成 --fullscreen 参数，
	// 此字段仅供记录，不直接参与进程构造）。
	LaunchFullscreen bool
	// Transform 已启用插件贡献的启动变换（Java 路径覆盖、类路径与参数前后插入、
	// 环境变量等）。nil 等价于空变换，即不改变原有启动行为。
	//
	// 现状（可行性复检结论）：这条链路**还没有接线**——插件贡献目前只能通过
	// 版本档案里的额外 JVM / 游戏参数生效，没有任何地方会填充 Transform，
	// 启动时解析出来的永远是空变换。保留字段是为了不改动启动管线的公开签名，
	// 但不要以为它能用（要接线需先有"插件贡献清单 → Transform"的转换层）。
	Transform *MinecraftLaunchTransform
	// LogCallback 启动过程中的文本日志回调（如 Java 自动下载阶段的进度提示）；可为空。
	LogCallback func(string)
	// GameOutputCallback 游戏进程 stdout/stderr 行回调（Go 移植新增：C# 由
	// GameLaunchService 直接订阅 Process 事件，Go 版进程由本包创建，故以回调注入）。
	// 第一个参数为行文本，第二个参数为是否 stderr。可为空。
	GameOutputCallback func(line string, isStderr bool)
}

// WithAccount 返回一个除账号外其余配置完全相同的副本，用于在启动前替换账号。
func (o MinecraftLaunchOptions) WithAccount(account MinecraftAccount) MinecraftLaunchOptions {
	copied := o
	copied.Account = account
	return copied
}

// MinecraftLaunchPlan Minecraft 启动计划：进程拉起所需的全部参数。
type MinecraftLaunchPlan struct {
	JavaExecutable           string
	WorkingDirectory         string
	NativeDirectory          string
	RequiredJavaMajorVersion *int
	Arguments                []string
	// EnvironmentVariables 注入子进程的环境变量（对应 C# Dictionary<string,string?> 的非 null 项）。
	EnvironmentVariables map[string]string
	// RemovedEnvironmentVariables 需要从子进程环境中移除的变量名
	//（对应 C# 字典中值为 null 的项）。
	RemovedEnvironmentVariables map[string]bool
	// WrapperCommand 包装命令拆分结果（%command% 已移除，替换逻辑在进程构造时做）；
	// 空切片表示不使用包装命令。
	WrapperCommand []string
	// ProcessPriority 游戏进程优先级；空串或 "normal" 表示不调整。
	ProcessPriority string
}

// MinecraftLaunchResult Minecraft 实例启动后的结果。
type MinecraftLaunchResult struct {
	// Cmd 已启动的 Java 进程；调用方不得重复调用 Wait（本包在启动时已启动
	// 退出观察 goroutine，退出状态经 Exit 通道发布）。
	Cmd *exec.Cmd
	// exitCh 进程退出后收到一次退出结果（Wait 的返回值），容量 1。
	exitCh                   chan error
	VersionId                string
	Username                 string
	RequiredJavaMajorVersion *int
}

// Pid 游戏进程 ID；进程未就绪时返回 0。
func (r *MinecraftLaunchResult) Pid() int {
	if r == nil || r.Cmd == nil || r.Cmd.Process == nil {
		return 0
	}
	return r.Cmd.Process.Pid
}

// Exit 进程退出通道：进程结束后收到一个退出错误（正常退出为 nil）。
func (r *MinecraftLaunchResult) Exit() <-chan error {
	if r.exitCh == nil {
		ch := make(chan error, 1)
		ch <- nil
		return ch
	}
	return r.exitCh
}

// Kill 结束游戏进程（不包含进程树）。
func (r *MinecraftLaunchResult) Kill() error {
	if r == nil || r.Cmd == nil || r.Cmd.Process == nil {
		return fmt.Errorf("进程未在运行")
	}
	return r.Cmd.Process.Kill()
}

// IOfflineMinecraftLauncher 离线启动器约定。
type IOfflineMinecraftLauncher interface {
	Launch(ctx context.Context, options MinecraftLaunchOptions) (*MinecraftLaunchResult, error)
}

// IMicrosoftMinecraftLauncher 使用正版（Microsoft）账号启动 Minecraft 的入口。
// 登录与令牌刷新由 auth 包的认证器完成，实现类负责校验令牌并复用现有的
// 离线启动管线（进程构造、参数构建、资源解析）。
type IMicrosoftMinecraftLauncher interface {
	Launch(ctx context.Context, account *auth.MicrosoftAccount, options MinecraftLaunchOptions) (*MinecraftLaunchResult, error)
}
