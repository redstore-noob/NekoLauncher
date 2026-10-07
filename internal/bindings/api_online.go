// OnlineAPI 联机大厅（internal/online 的 Wails 绑定）。
//
// 前端「联机」页只跟这个结构体打交道：供应商列表 / 状态快照 / 建房 / 加入 /
// 退出 / 设置。会话状态由后端主动推送（online:changed），前端不需要轮询。
package bindings

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"nekolauncher/internal/mcserver"
	"nekolauncher/internal/online"
)

// onlineServerPollInterval 等待"转发目标服务器"就绪的轮询间隔。
const onlineServerPollInterval = 500 * time.Millisecond

// OnlineAPI 联机：陶瓦联机 / 红石联机双供应商。
type OnlineAPI struct {
	ctx context.Context
}

// Startup 注入 Wails runtime ctx，接线事件与"本机服务器"两个钩子。
func (a *OnlineAPI) Startup(ctx context.Context) {
	a.ctx = ctx

	manager := online.Default()
	manager.SetOnChanged(func(status online.Status) {
		emit(ctx, "online:changed", status)
	})

	// 本地服务器（红石联机的转发目标）在这里注入：online 包不需要认识
	// mcserver 的下载与进程管理，绑定层做这层转接最合适。
	manager.LocalServers = func() []online.LocalServer {
		infos := mcserver.ListServerInfos()
		servers := make([]online.LocalServer, 0, len(infos))

		for _, info := range infos {
			servers = append(servers, online.LocalServer{
				ID:      info.ID,
				Name:    info.Name,
				Port:    info.Port,
				Status:  info.Status,
				Running: info.Status == mcserver.StatusRunning,
			})
		}

		return servers
	}
	manager.StartLocalServer = startServerForOnline
	// 红石中继不提供成员名单；转发目标是启动器托管的服务器时，从服务器的
	// `list` 命令取真实在线玩家补进快照（手动地址模式没有名单来源）。
	manager.PlayersFor = func(serverID string) []online.Player {
		players := mcserver.GetServerPlayers(serverID)
		result := make([]online.Player, 0, len(players.Online))

		for _, name := range players.Online {
			result = append(result, online.Player{
				Name:   name,
				Kind:   online.KindLocal,
				Vendor: "NekoLauncher",
			})
		}

		return result
	}
}

// Shutdown 应用退出：断开隧道、停掉本进程的轮询协程。
// 陶瓦联机的守护进程按设计常驻（用户可能正在用它），只有用户点「关闭后台服务」
// 才会结束它。
func (a *OnlineAPI) Shutdown(context.Context) {
	online.Default().Close()
}

// ListProviders 供应商列表（含可用性与说明）。
func (a *OnlineAPI) ListProviders() []online.ProviderInfo {
	return online.Default().Providers()
}

// ListStatuses 全部供应商的会话快照（前端首帧各标签页各取一份，
// 之后靠 online:changed 事件增量更新）。两家可以同时开着，所以这里是列表。
func (a *OnlineAPI) ListStatuses() []online.Status {
	return online.Default().Statuses()
}

// GetRuntime 指定供应商在本机的运行时状态（是否已在运行、版本、路径）。
func (a *OnlineAPI) GetRuntime(provider string) online.Runtime {
	return online.Default().Runtime(provider)
}

// GetSettings 读取联机设置。红石 API Key 是隧道鉴权凭据，原文不下发
// （插件与宿主同 WebView 可直呼本绑定）：清空 RedstoneKey、以
// HasRedstoneKey 标记存在性，输入框回显见前端占位提示。
func (a *OnlineAPI) GetSettings() online.Settings {
	settings := online.Default().Settings()
	if settings.RedstoneKey != "" {
		settings.HasRedstoneKey = true
		settings.RedstoneKey = ""
	}
	return settings
}

// SaveSettings 保存联机设置（空供应商/空中继会按默认值补齐）。
// RedstoneKey 为空串视为"未重新输入"，沿用已保存的 Key——回显已是
// 空串，直接透传会把凭据抹掉。
func (a *OnlineAPI) SaveSettings(settings online.Settings) error {
	if strings.TrimSpace(settings.RedstoneKey) == "" {
		settings.RedstoneKey = online.Default().Settings().RedstoneKey
	}
	return online.Default().SaveSettings(settings)
}

// Host 建房。
func (a *OnlineAPI) Host(options online.HostOptions) error {
	return online.Default().Host(options)
}

// Join 加入房间（陶瓦联机用房间码；红石联机填房主给的公网地址）。
func (a *OnlineAPI) Join(provider, room, player string) error {
	return online.Default().Join(provider, room, player)
}

// Leave 退出指定供应商的会话（provider 为空时退出全部）。
// 只停这一家：另一家的会话（另一条链路）继续跑。
func (a *OnlineAPI) Leave(provider string) error {
	return online.Default().Leave(provider)
}

// ShutdownProvider 关闭指定供应商在本机的后台服务
// （陶瓦：结束守护进程；红石：断开隧道并释放中继端口）。
func (a *OnlineAPI) ShutdownProvider(provider string) error {
	return online.Default().Shutdown(provider)
}

// ListLocalServers 启动器托管的服务器（作为红石联机的转发目标候选）。
func (a *OnlineAPI) ListLocalServers() []online.LocalServer {
	return online.Default().LocalServerOptions()
}

// GenerateAPIKey 生成一个红石联机 API Key（用户想换一个时用）。
func (a *OnlineAPI) GenerateAPIKey() (string, error) {
	return online.NewAPIKey()
}

// InstallTerracotta 自动下载并安装陶瓦联机（GitHub Releases → 解压 → 写回设置）。
//
// 返回结果的 ManualHint 非空时表示"需要用户手动完成"（本平台资产选不中，或
// 发行包格式不便自动解压）：前端据此给出提示与发行页链接，而不是含糊地报失败。
func (a *OnlineAPI) InstallTerracotta() (online.TerracottaInstallResult, error) {
	return online.Default().InstallTerracotta(callCtx(a.ctx))
}

// ListRelayOptions 红石联机可选的中继节点（内置官方节点 + 用户自定义）。
func (a *OnlineAPI) ListRelayOptions() []online.RelayOption {
	return online.Default().RelayOptions()
}

// GetRelayList 用户自定义的中继节点原文（每行一条：名称=地址 或 仅地址）。
func (a *OnlineAPI) GetRelayList() string {
	return online.Default().RelayList()
}

// SaveRelayList 保存用户自定义的中继节点。
func (a *OnlineAPI) SaveRelayList(list string) error {
	return online.Default().SaveRelayList(list)
}

// ProbeRelays 并发预检全部中继节点（可达性 + 延迟），前端「测速」按钮用。
func (a *OnlineAPI) ProbeRelays() []online.RelayProbe {
	return online.Default().ProbeRelays(callCtx(a.ctx))
}

// UseFastestRelay 预检并把中继设置换成最快的一个（全部不可达时返回错误）。
func (a *OnlineAPI) UseFastestRelay() (online.RelayProbe, error) {
	return online.Default().UseFastestRelay(callCtx(a.ctx))
}

// OpenPage 用系统默认浏览器打开供应商主页/发行页（前端"了解 / 下载"按钮）。
func (a *OnlineAPI) OpenPage(target string) error {
	url := strings.TrimSpace(target)
	if url == "" {
		return errors.New("地址为空")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("只允许打开 http(s) 地址：%s", target)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}

	return startDetached(cmd)
}

// startServerForOnline 保证指定服务器处于运行状态并返回端口：
// 联机最常见的翻车点就是"忘了开服就点联机"，这里顺手把它拉起来。
func startServerForOnline(ctx context.Context, id string, timeout time.Duration) (int, error) {
	info, ok := findServerInfo(id)
	if !ok {
		return 0, fmt.Errorf("找不到服务器：%s", id)
	}

	if info.Status != mcserver.StatusRunning {
		if err := mcserver.Default().StartServer(ctx, id); err != nil {
			return 0, err
		}
	}

	deadline := time.Now().Add(timeout)
	for {
		snapshot := mcserver.Default().Poll(id, math.MaxInt64)
		switch snapshot.Status {
		case mcserver.StatusRunning:
			port := info.Port
			if port <= 0 {
				port = 25565
			}

			return port, nil
		case mcserver.StatusStopped:
			return 0, errors.New("服务器未能启动（已停止），请先在「服务器」页确认它能正常运行")
		}

		if time.Now().After(deadline) {
			return 0, errors.New("等待服务器启动超时，请先在「服务器」页手动启动一次")
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(onlineServerPollInterval):
		}
	}
}

// findServerInfo 按 id 查托管服务器（列表很短，线性查找足够）。
func findServerInfo(id string) (mcserver.ServerInfo, bool) {
	for _, info := range mcserver.ListServerInfos() {
		if info.ID == id {
			return info, true
		}
	}

	return mcserver.ServerInfo{}, false
}
