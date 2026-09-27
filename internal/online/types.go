// Package online 联机大厅：把形态完全不同的联机供应商统一成"建房 / 加入 /
// 退出 / 看状态"四件事，供前端「联机」页调用。
//
// 目前接入两家（协议来自各自的公开实现，细节见 provider 文件）：
//
//   - Terracotta（陶瓦联机，https://github.com/burningtnt/Terracotta）：
//     基于 EasyTier 的虚拟局域网。第三方程序按官方约定用 `--hmcl <文件>`
//     拉起进程，进程把本地 HTTP 服务端口原子写入该文件，之后靠 /state 系列
//     接口驱动状态机并读回房间码与成员列表。房主由进程自己扫描本机"对局域网
//     开放"的世界，房客拿房间码入网，本机得到 127.0.0.1:<port> 直连地址。
//
//   - RedstoneOnline（红石联机，https://github.com/hongshionline/RedstoneOnline）：
//     frp 式中继。先用 API Key 在中继 3000 端口的 HTTP 接口上申请隧道（拿到
//     公网 listenPort），再在 7000 端口上按玩家数维持若干条带 Key 的 TCP
//     连接，把每条连接的数据转发到本机 Minecraft 服务端。
//
// 两家协议形态差异太大，所以对外只统一"状态快照"，不强行统一内部流程。
package online

import "strings"

// ProviderID 供应商标识。会写进 launcher.yaml，改名会让用户已保存的选择失效。
type ProviderID string

const (
	// ProviderTerracotta 陶瓦联机（Terracotta / EasyTier 虚拟局域网）。
	ProviderTerracotta ProviderID = "terracotta"
	// ProviderRedstone 红石联机（RedstoneOnline / frp 公网中继）。
	ProviderRedstone ProviderID = "redstone"
)

// NormalizeProviderID 收敛前端传来的供应商 id：无法识别时回落到陶瓦联机。
func NormalizeProviderID(raw string) ProviderID {
	if ProviderID(strings.TrimSpace(raw)) == ProviderRedstone {
		return ProviderRedstone
	}

	return ProviderTerracotta
}

// 会话状态（前端据此决定按钮可用性与展示的分支）。
const (
	StateIdle     = "idle"     // 空闲，可以建房或加入
	StateStarting = "starting" // 准备中（拉起进程 / 申请隧道 / 等待对端）
	StateHosting  = "hosting"  // 我是房主，房间已就绪
	StateJoined   = "joined"   // 我是房客，已接入房主的网络
	StateError    = "error"    // 出错，需要用户处理
)

// 房间成员身份，取值与 Scaffolding 协议的 kind 字段一致。
const (
	KindHost  = "HOST"
	KindLocal = "LOCAL"
	KindGuest = "GUEST"
)

// Player 房间成员（陶瓦联机 /state 的 profiles 字段）。
type Player struct {
	Name   string `json:"Name"`
	Kind   string `json:"Kind"`
	Vendor string `json:"Vendor"`
}

// Status 联机会话快照：前端唯一的状态来源，由后端主动推送（online:changed）。
type Status struct {
	Provider string `json:"Provider"`
	State    string `json:"State"`
	// Phase 当前阶段的说明文案（中文原文，前端用 t() 翻译）
	Phase string `json:"Phase"`
	// Room 房间码（陶瓦）或房间地址（红石）
	Room string `json:"Room"`
	// Address 别人要连接的目标：红石是公网 host:port；陶瓦是房间码（虚拟局域网）
	Address string `json:"Address"`
	// LocalAddress 本机在游戏里直连的地址（陶瓦房客为 127.0.0.1:<port>）
	LocalAddress string `json:"LocalAddress"`
	// JoinHost / JoinPort 一键进服参数（为空表示当前状态还不能进服）
	JoinHost string `json:"JoinHost"`
	JoinPort int    `json:"JoinPort"`
	// Players 房间成员（陶瓦联机提供；红石中继不暴露成员列表，恒为空）
	Players []Player `json:"Players"`
	// Connections 当前隧道里活跃的转发连接数（红石；陶瓦用 Players 长度即可）
	Connections int `json:"Connections"`
	// Tip 当前状态下给用户的一句操作提示（中文原文）
	Tip string `json:"Tip"`
	// RelayNote 中继节点说明：预检发现配置节点不可达、自动切换或全部不通时的
	// 运行期提示（含地址与延迟，前端原样展示，不查词典）
	RelayNote string `json:"RelayNote"`
	// Error 出错信息（State == "error" 时有值）
	Error string `json:"Error"`
	// Since 本次会话开始时间（Unix 秒；0 表示当前没有会话）
	Since int64 `json:"Since"`
}

// ProviderInfo 供应商的静态介绍（前端供应商切换器与说明卡片用）。
type ProviderInfo struct {
	ID       string `json:"ID"`
	Name     string `json:"Name"`
	Summary  string `json:"Summary"`
	Homepage string `json:"Homepage"`
	// Ready 当前是否可用：陶瓦需要本机存在可执行文件；红石只需要网络
	Ready bool `json:"Ready"`
	// Hint 不可用时的处理建议（中文原文）
	Hint string `json:"Hint"`
	// NeedsMod 房主是否必须先给游戏装配套模组
	NeedsMod bool `json:"NeedsMod"`
	// GuestNeedsMod 房客是否需要装模组（两家都不需要）
	GuestNeedsMod bool `json:"GuestNeedsMod"`
	// HostNote / JoinNote 建房与加入的说明（中文原文，前端 t() 翻译）
	HostNote string `json:"HostNote"`
	JoinNote string `json:"JoinNote"`
}

// Runtime 供应商在本机的运行时状态（设置卡片与"关闭后台服务"按钮用）。
type Runtime struct {
	Provider string `json:"Provider"`
	// Running 本机服务已在运行（陶瓦：HTTP 服务可达；红石：隧道存活）
	Running bool `json:"Running"`
	// Managed 由本启动器会话拉起（关闭它不会影响用户自己开的窗口）
	Managed bool   `json:"Managed"`
	Version string `json:"Version"`
	Binary  string `json:"Binary"`
	Port    int    `json:"Port"`
}

// Settings 联机页的可持久化设置（落在 launcher.yaml 的 online.* 键）。
type Settings struct {
	// Provider 上次选择的供应商
	Provider string `json:"Provider"`
	// Player 联机时展示给别人的名字（默认取当前账号名）
	Player string `json:"Player"`
	// TerracottaPath 手动指定的陶瓦联机可执行文件路径（空 = 自动探测）
	TerracottaPath string `json:"TerracottaPath"`
	// RedstoneRelay 红石联机中继地址（默认官方上海节点）
	RedstoneRelay string `json:"RedstoneRelay"`
	// RedstoneKey 红石联机 API Key（首次使用自动生成并缓存）
	RedstoneKey string `json:"RedstoneKey"`
	// Target 本地 Minecraft 服务端地址（红石联机转发目标，host:port）
	Target string `json:"Target"`
	// ServerID 本地目标取自启动器托管的服务器时填其 id（建房时自动启动）
	ServerID string `json:"ServerID"`
	// MaxPlayers 红石联机隧道并发连接数上限
	MaxPlayers int `json:"MaxPlayers"`
}

// HostOptions 建房参数（前端表单 → 管理器）。
type HostOptions struct {
	Provider string `json:"Provider"`
	// Player 房主昵称
	Player string `json:"Player"`
	// Target 本地转发目标（红石联机用）
	Target string `json:"Target"`
	// ServerID 本地目标为启动器托管服务器时填其 id
	ServerID string `json:"ServerID"`
	// MaxPlayers 红石联机隧道并发上限
	MaxPlayers int `json:"MaxPlayers"`
}

// LocalServer 启动器托管的服务器（作为红石联机转发目标的候选）。
type LocalServer struct {
	ID      string `json:"ID"`
	Name    string `json:"Name"`
	Port    int    `json:"Port"`
	Status  string `json:"Status"`
	Running bool   `json:"Running"`
}
