package online

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 红石联机（RedstoneOnline）对接要点 —— 来自其公开实现：
//
//	https://github.com/hongshionline/RedstoneOnline
//
// 服务端由两部分组成（官方内置节点 122.51.108.96，另有多个可选节点）：
//
//	:3000  HTTP 控制面（API Key 放在 Authorization 头里，原样发送，不加 Bearer）
//	       POST /apikey            注册密钥（已存在返回 409）
//	       POST /tunnels?maxPlayers=N   申请隧道，返回 {"listenPort":N}；已有隧道返回 429
//	       GET  /tunnels           查询已有隧道 {"tunnels":[{"listenPort":N}]}
//	       DELETE /tunnels         释放隧道
//	:7000  TCP 数据面：连上后先发一行 "<apikey>\n"，中继回一行 "OK TUNNEL <port>"，
//	       之后这条连接就与某个连到 listenPort 的玩家配对，双向透传。
//
// 所以启动器这边要做的事就是：申请隧道 → 按并发上限维持若干条 :7000 连接 →
// 每条连接与"本机 Minecraft 服务端"对接。房主需要装模组只针对"对局域网开放"
// 的存档（模组负责发布局域网）；如果目标本来就是启动器托管/自建的服务器，
// 房主端完全不需要模组，房客端任何情况下都不需要。
const (
	// redstoneHomepage 模组主页（Modrinth）
	redstoneHomepage = "https://modrinth.com/mod/redstoneonline"

	// redstoneAPIPort 控制面端口
	redstoneAPIPort = 3000
	// redstoneTunnelPort 数据面端口
	redstoneTunnelPort = 7000

	// redstoneRequestTimeout 控制面单次请求超时
	redstoneRequestTimeout = 12 * time.Second
	// redstoneGreetingTimeout 读取中继握手行的上限：超过就当"中继不打状态行"，
	// 已读到的字节原样转给本地服务端
	redstoneGreetingTimeout = 400 * time.Millisecond
	// redstoneRetryDelay 单条隧道连接失败后的重试间隔
	redstoneRetryDelay = 1500 * time.Millisecond
	// redstoneDialTimeout 连接中继/本地服务端的超时
	redstoneDialTimeout = 10 * time.Second
	// redstonePlayerPollInterval 成员列表轮询间隔。红石中继本身不暴露成员名单，
	// 只有"转发目标 = 启动器托管的服务器"时才能从服务器的 `list` 命令拿到真实玩家，
	// 所以只有那种情况才会启动这个轮询。
	redstonePlayerPollInterval = 5 * time.Second
)

// redstoneProvider 红石联机供应商：自己实现中继客户端，不依赖游戏模组。
type redstoneProvider struct {
	observer

	store settingStore

	// httpClient / tunnelPort 可在测试里替换
	httpClient *http.Client
	tunnelPort int
	// playersFor 由管理器/绑定层注入："启动器托管的服务器"里当前在线的玩家。
	// 为 nil（或转发目标是手动填的地址）时成员列表保持为空。
	playersFor func(serverID string) []Player

	mu         sync.Mutex
	status     Status
	since      int64
	listenPort int
	// relayHost 裸主机名（数据面 :7000 用）；relayAPI 是 host:port（控制面用）
	relayHost  string
	relayAPI   string
	target     string
	apiKey     string
	cancel     context.CancelFunc
	workers    sync.WaitGroup
	// connections 当前正在透传的玩家连接数
	connections atomic.Int32
}

// newRedstoneProvider 构造供应商。
func newRedstoneProvider(store settingStore) *redstoneProvider {
	return &redstoneProvider{
		store:      store,
		httpClient: &http.Client{Timeout: redstoneRequestTimeout},
		tunnelPort: redstoneTunnelPort,
		status:     Status{Provider: string(ProviderRedstone), State: StateIdle, Phase: "空闲"},
	}
}

// ID 供应商标识。
func (p *redstoneProvider) ID() ProviderID { return ProviderRedstone }

// Info 静态介绍。
//
// Hint/HostNote/JoinNote 都是固定文案（前端拿去查词典），不要拼接运行期内容。
func (p *redstoneProvider) Info() ProviderInfo {
	return ProviderInfo{
		ID:            string(ProviderRedstone),
		Name:          "红石联机",
		Summary:       "公网中继（frp）：给本机服务器或局域网世界分配一个公网地址，房客直接连，不用装任何东西。",
		Homepage:      redstoneHomepage,
		Ready:         true,
		Hint:          "默认使用官方中继节点，可以在右侧设置里改成你自己的节点。",
		NeedsMod:      true,
		GuestNeedsMod: false,
		HostNote: "转发启动器托管的服务器时不需要模组；如果要联机的是「对局域网开放」的存档，" +
			"请先给这个实例装 RedstoneOnline 模组并用 /rs open 发布（模组只负责发布，隧道由启动器接管）。",
		JoinNote: "房主发来的是一段公网地址，粘进来即可保存并一键进服；房客不需要装模组。",
	}
}

// Runtime 本机隧道状态。
func (p *redstoneProvider) Runtime() Runtime {
	p.mu.Lock()
	defer p.mu.Unlock()

	return Runtime{
		Provider: string(ProviderRedstone),
		Running:  p.cancel != nil,
		Managed:  p.cancel != nil,
		Version:  "",
		Binary:   p.relayAPI,
		Port:     p.listenPort,
	}
}

// Status 当前会话快照。
func (p *redstoneProvider) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.status
}

// Host 建房：申请隧道并开始转发本机服务端。
func (p *redstoneProvider) Host(options HostOptions) error {
	settings := loadSettings(p.store)

	target, err := NormalizeTargetAddress(options.Target)
	if err != nil {
		p.fail(err)

		return err
	}
	relayHost, relayAPI := relayAddress(settings.RedstoneRelay)
	if relayHost == "" || relayAPI == "" {
		err := errors.New("请先填写红石联机的中继服务器地址")
		p.fail(err)

		return err
	}

	// 先预检节点：官方节点在国内并不总是可用，直接去注册密钥失败时用户
	// 只会看到"注册联机密钥失败"，既不知道是网络问题也不知道能换节点。
	p.beginPhase("正在检查中继节点…", "先确认中继服务器能不能连上；连不上会自动换一个可达的。")
	relayHost, relayAPI, relayNote := preflightRedstoneRelay(
		context.Background(), relayHost, relayAPI, settings.RedstoneRelay)

	maxPlayers := options.MaxPlayers
	if maxPlayers <= 0 {
		maxPlayers = settings.MaxPlayers
	}
	if maxPlayers <= 0 {
		maxPlayers = DefaultMaxPlayers
	}
	if maxPlayers > 50 {
		maxPlayers = 50
	}

	apiKey, err := p.ensureAPIKey(settings.RedstoneKey)
	if err != nil {
		p.fail(err)

		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	since := nowUnix()

	p.mu.Lock()
	p.status = Status{
		Provider: string(ProviderRedstone),
		State:    StateStarting,
		Phase:    "正在注册联机密钥…",
		Tip:      "正在向中继服务器登记 API Key。",
		Since:    since,
	}
	p.since = since
	p.relayHost = relayHost
	p.relayAPI = relayAPI
	p.target = target
	p.apiKey = apiKey
	p.cancel = cancel
	starting := p.status
	p.mu.Unlock()
	p.notify(starting)

	if err := p.registerKey(ctx, relayAPI, apiKey); err != nil {
		p.abortSession()
		failure := withRelayNote(fmt.Errorf("注册联机密钥失败：%v", err), relayNote)
		p.fail(failure)

		return failure
	}

	p.beginPhase("正在申请公网隧道…", "正在向中继申请端口，通常一两秒。")

	listenPort, err := p.openTunnel(ctx, relayAPI, apiKey, maxPlayers)
	if err != nil {
		p.abortSession()
		failure := withRelayNote(fmt.Errorf("申请隧道失败：%v", err), relayNote)
		p.fail(failure)

		return failure
	}

	// 公网地址用"裸主机名 + 中继分配的端口"，房客直接连这个地址
	address := net.JoinHostPort(relayHost, strconv.Itoa(listenPort))
	host, port := SplitHostPort(target)

	status := Status{
		Provider:     string(ProviderRedstone),
		State:        StateHosting,
		Phase:        "隧道已就绪",
		Room:         DisplayAddress(address),
		Address:      DisplayAddress(address),
		LocalAddress: target,
		JoinHost:     host,
		JoinPort:     port,
		Tip:          "把地址发给朋友，Ta 在游戏里「直接连接」就能进来；转发目标在下面。",
		RelayNote:    relayNote,
		Since:        since,
	}

	p.mu.Lock()
	p.listenPort = listenPort
	p.status = status
	p.mu.Unlock()

	p.startWorkers(ctx, maxPlayers)
	// 转发目标是启动器托管的服务器时，额外轮询它的在线玩家，把真实成员名单
	// 填进快照（中继自己不提供名单；手动地址模式没有名单来源，只显示连接数）。
	if serverID := strings.TrimSpace(options.ServerID); serverID != "" {
		p.startPlayersWatcher(ctx, serverID)
	}
	p.publish(status)

	return nil
}

// startPlayersWatcher 启动成员列表轮询（会话结束即随 ctx 退出）。
func (p *redstoneProvider) startPlayersWatcher(ctx context.Context, serverID string) {
	p.refreshPlayers(serverID)

	go func() {
		ticker := time.NewTicker(redstonePlayerPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.refreshPlayers(serverID)
			}
		}
	}()
}

// refreshPlayers 刷新成员列表；名单没变就不推送（避免每 5 秒刷一次前端）。
func (p *redstoneProvider) refreshPlayers(serverID string) {
	if p.playersFor == nil {
		return
	}

	players := p.playersFor(serverID)
	if players == nil {
		players = []Player{}
	}

	p.mu.Lock()
	if p.status.State != StateHosting || samePlayers(p.status.Players, players) {
		p.mu.Unlock()

		return
	}
	p.status.Players = players
	status := p.status
	p.mu.Unlock()

	p.notify(status)
}

// samePlayers 两张成员名单是否一致（顺序敏感：服务端返回前已排序）。
func samePlayers(left, right []Player) bool {
	if len(left) != len(right) {
		return false
	}

	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}

	return true
}

// Join 加入房间：红石联机的"加入"不需要本机做任何事，只要记住地址即可，
// 因此这里把它规范化并落到状态里，交给前端做"复制 / 一键进服"。
func (p *redstoneProvider) Join(room, player string) error {
	address, err := NormalizeTargetAddress(room)
	if err != nil {
		wrapped := errors.New("请填写房主给你的公网地址，例如 122.51.108.96:12345")
		p.fail(wrapped)

		return wrapped
	}

	host, port := SplitHostPort(address)
	display := DisplayAddress(address)
	since := nowUnix()

	p.mu.Lock()
	p.since = since
	p.status = Status{
		Provider: string(ProviderRedstone),
		State:    StateJoined,
		Phase:    "已记录房间地址",
		Room:     display,
		Address:  display,
		JoinHost: host,
		JoinPort: port,
		Tip: "红石联机的房客不用装任何东西：点下面的「一键进服」，或在游戏里手动连接上面的地址。",
		Since: since,
	}
	status := p.status
	p.mu.Unlock()

	p.publish(status)

	return nil
}

// Leave 退出会话：断开全部隧道并通知中继释放端口。
func (p *redstoneProvider) Leave() error {
	relay, apiKey := p.stopWorkers()

	var err error
	if relay != "" && apiKey != "" {
		err = p.deleteTunnel(context.Background(), relay, apiKey)
	}

	p.mu.Lock()
	p.listenPort = 0
	p.since = 0
	p.status = Status{
		Provider: string(ProviderRedstone),
		State:    StateIdle,
		Phase:    "空闲",
		Tip:      "点「创建房间」把本机服务器发布到公网，或粘贴房主给的地址加入。",
	}
	status := p.status
	p.mu.Unlock()

	p.publish(status)

	return err
}

// Shutdown 关闭隧道（与 Leave 同义：红石联机的后台服务就是隧道本身）。
func (p *redstoneProvider) Shutdown() error { return p.Leave() }

// Close 应用退出：停掉转发并尽力释放中继端口，避免下次启动撞上 429。
func (p *redstoneProvider) Close() {
	relay, apiKey := p.stopWorkers()
	if relay == "" || apiKey == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, _, _ = p.apiRequest(ctx, http.MethodDelete, relay, "/tunnels", nil, nil, nil, apiKey)
}

// ---- HTTP 控制面 ----

// relayAddress 规范化中继地址：允许只写主机名（默认 3000 端口）。
// 返回裸主机名（数据面 :7000 连接用）与控制面地址 host:apiPort。
func relayAddress(raw string) (string, string) {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimSuffix(value, "/")
	if value == "" {
		return "", ""
	}

	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return value, net.JoinHostPort(value, strconv.Itoa(redstoneAPIPort))
	}

	return host, net.JoinHostPort(host, port)
}

// apiBase 控制面根地址（relay 已是 host:port 形态）。
func (p *redstoneProvider) apiBase(relay string) string {
	return "http://" + relay
}

// apiRequest 调控制面接口；apiKey 非空时放进 Authorization 头（原样发送，
// 与模组实现一致）。返回响应体、状态码与错误。
func (p *redstoneProvider) apiRequest(
	ctx context.Context,
	method, relay, path string,
	query url.Values,
	body []byte,
	contentType *string,
	apiKey string,
) ([]byte, int, error) {
	target := p.apiBase(relay) + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, 0, err
	}
	if contentType != nil {
		request.Header.Set("Content-Type", *contentType)
	}
	if apiKey != "" {
		request.Header.Set("Authorization", apiKey)
	}

	response, err := p.httpClient.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("无法连接中继 %s：%v", relay, err)
	}
	defer func() { _ = response.Body.Close() }()

	payload, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))

	return payload, response.StatusCode, nil
}

// ensureAPIKey 复用已保存的密钥，没有就现生成一个（20 位字母数字，与模组一致）。
func (p *redstoneProvider) ensureAPIKey(existing string) (string, error) {
	if key := strings.TrimSpace(existing); key != "" {
		return key, nil
	}
	if p.store != nil {
		if key := strings.TrimSpace(p.store.Get(keyRedstoneKey)); key != "" {
			return key, nil
		}
	}

	key, err := generateAPIKey(20)
	if err != nil {
		return "", fmt.Errorf("生成联机密钥失败：%v", err)
	}
	saveSetting(p.store, keyRedstoneKey, key)

	return key, nil
}

// apiKeyLength 红石联机密钥长度（与模组实现一致：20 位字母数字）。
const apiKeyLength = 20

// NewAPIKey 生成一个新的红石联机 API Key（前端"换一个密钥"按钮用）。
func NewAPIKey() (string, error) { return generateAPIKey(apiKeyLength) }

// generateAPIKey 生成 length 位字母数字密钥（crypto/rand）。
func generateAPIKey(length int) (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

	var builder strings.Builder
	builder.Grow(length)

	for index := 0; index < length; index++ {
		position, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		builder.WriteByte(alphabet[position.Int64()])
	}

	return builder.String(), nil
}

// registerKey 注册密钥：200 新注册、409 已存在，都算成功。
func (p *redstoneProvider) registerKey(ctx context.Context, relay, apiKey string) error {
	payload, err := json.Marshal(map[string]string{"apikey": apiKey})
	if err != nil {
		return err
	}

	contentType := "application/json"
	body, status, err := p.apiRequest(ctx, http.MethodPost, relay, "/apikey", nil, payload, &contentType, "")
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusConflict {
		return nil
	}

	return fmt.Errorf("中继返回 %d：%s", status, strings.TrimSpace(string(body)))
}

// openTunnel 申请隧道：已有隧道直接复用（模组也是这个行为），
// 撞上 429（配额已满）时先删掉旧隧道再申请一次。
func (p *redstoneProvider) openTunnel(ctx context.Context, relay, apiKey string, maxPlayers int) (int, error) {
	if port := p.existingTunnelPort(ctx, relay, apiKey); port > 0 {
		return port, nil
	}

	query := url.Values{"maxPlayers": []string{strconv.Itoa(maxPlayers)}}
	body, status, err := p.apiRequest(ctx, http.MethodPost, relay, "/tunnels", query, nil, nil, apiKey)
	if err != nil {
		return 0, err
	}
	if status == http.StatusTooManyRequests {
		if err := p.deleteTunnel(ctx, relay, apiKey); err != nil {
			return 0, err
		}
		body, status, err = p.apiRequest(ctx, http.MethodPost, relay, "/tunnels", query, nil, nil, apiKey)
		if err != nil {
			return 0, err
		}
	}
	if status < 200 || status >= 300 {
		return 0, fmt.Errorf("中继返回 %d：%s", status, strings.TrimSpace(string(body)))
	}

	if port := parseTunnelPort(body); port > 0 {
		return port, nil
	}

	// 有些节点创建成功但响应体里没有端口，回头查一次
	if port := p.existingTunnelPort(ctx, relay, apiKey); port > 0 {
		return port, nil
	}

	return 0, errors.New("中继没有返回隧道端口")
}

// existingTunnelPort 查询已有隧道的公网端口（没有则 0）。
func (p *redstoneProvider) existingTunnelPort(ctx context.Context, relay, apiKey string) int {
	body, status, err := p.apiRequest(ctx, http.MethodGet, relay, "/tunnels", nil, nil, nil, apiKey)
	if err != nil || status != http.StatusOK {
		return 0
	}

	return parseTunnelPort(body)
}

// deleteTunnel 释放隧道（失败也不影响本地流程，只要不是致命错误）。
func (p *redstoneProvider) deleteTunnel(ctx context.Context, relay, apiKey string) error {
	_, status, err := p.apiRequest(ctx, http.MethodDelete, relay, "/tunnels", nil, nil, nil, apiKey)
	if err != nil {
		return err
	}
	if status >= 400 && status != http.StatusNotFound && status != http.StatusTooManyRequests {
		return fmt.Errorf("释放隧道失败：中继返回 %d", status)
	}

	return nil
}

// parseTunnelPort 从 `{"listenPort":N}` 或 `{"tunnels":[{"listenPort":N}]}` 里取端口。
func parseTunnelPort(body []byte) int {
	if len(body) == 0 {
		return 0
	}

	var single struct {
		ListenPort int `json:"listenPort"`
	}
	if err := json.Unmarshal(body, &single); err == nil && single.ListenPort > 0 {
		return single.ListenPort
	}

	var list struct {
		Tunnels []struct {
			ListenPort int `json:"listenPort"`
		} `json:"tunnels"`
	}
	if err := json.Unmarshal(body, &list); err == nil {
		for _, tunnel := range list.Tunnels {
			if tunnel.ListenPort > 0 {
				return tunnel.ListenPort
			}
		}
	}

	return 0
}

// ---- 数据面 ----

// startWorkers 按并发上限拉起转发协程：每条连接服务一个玩家，断开后自动补位。
func (p *redstoneProvider) startWorkers(ctx context.Context, maxPlayers int) {
	p.workers.Add(maxPlayers)

	for index := 0; index < maxPlayers; index++ {
		go func() {
			defer p.workers.Done()
			p.tunnelLoop(ctx)
		}()
	}
}

// stopWorkers 停掉全部转发协程并返回当前会话的控制面地址/密钥（供释放隧道用）。
func (p *redstoneProvider) stopWorkers() (string, string) {
	p.mu.Lock()
	cancel := p.cancel
	relay := p.relayAPI
	apiKey := p.apiKey
	p.cancel = nil
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	p.workers.Wait()
	p.connections.Store(0)

	return relay, apiKey
}

// abortSession 建房中途失败时收尾：停掉还没开始干活的会话上下文，
// 让 Runtime() 不会谎报"隧道在运行"。
func (p *redstoneProvider) abortSession() {
	p.mu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.listenPort = 0
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// tunnelLoop 单条转发协程：连中继 → 等玩家 → 对接本地服务端 → 循环。
func (p *redstoneProvider) tunnelLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}

		served, err := p.serveTunnelConnection(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			// 只有"确实服务过玩家"才立刻接下一个，否则退避一下避免空转打爆中继
			if !served {
				select {
				case <-ctx.Done():
					return
				case <-time.After(redstoneRetryDelay):
				}
			}

			continue
		}
	}
}

// serveTunnelConnection 处理一条隧道连接，返回是否真正服务过玩家。
func (p *redstoneProvider) serveTunnelConnection(ctx context.Context) (bool, error) {
	p.mu.Lock()
	relay := p.relayHost
	apiKey := p.apiKey
	target := p.target
	p.mu.Unlock()

	if relay == "" || apiKey == "" || target == "" {
		return false, errors.New("会话参数不完整")
	}

	dialer := &net.Dialer{Timeout: redstoneDialTimeout}
	relayConn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(relay, strconv.Itoa(p.tunnelPort)))
	if err != nil {
		return false, err
	}
	defer func() { _ = relayConn.Close() }()

	if _, err := relayConn.Write([]byte(apiKey + "\n")); err != nil {
		return false, err
	}

	prefix, err := readTunnelGreeting(relayConn)
	if err != nil {
		return false, err
	}

	localConn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return false, fmt.Errorf("连接本机服务端 %s 失败：%v", target, err)
	}
	defer func() { _ = localConn.Close() }()

	if len(prefix) > 0 {
		if _, err := localConn.Write(prefix); err != nil {
			return false, err
		}
	}

	// 会话结束时把两条连接一起关掉，让阻塞中的转发立刻返回
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = relayConn.Close()
			_ = localConn.Close()
		case <-done:
		}
	}()

	p.bumpConnections(1)
	defer p.bumpConnections(-1)

	pipeBoth(relayConn, localConn)

	return true, nil
}

// readTunnelGreeting 读中继握手后的状态行，返回值是需要原样转给本地服务端的
// 前置字节：
//
//   - "OK TUNNEL <port>"：成功，返回 nil；
//   - 其它短 ASCII 文本行：中继拒绝（密钥错误/配额满），返回错误；
//   - 超时或首个字节是二进制：中继不打状态行，把这批字节当玩家数据返回。
//
// 首字节判据很关键：Minecraft 握手包以 VarInt 长度开头（0x10 之类），
// 而中继状态行一定以字母 O/E 开头，两者不会混淆。
func readTunnelGreeting(conn net.Conn) ([]byte, error) {
	_ = conn.SetReadDeadline(time.Now().Add(redstoneGreetingTimeout))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	head := make([]byte, 1)
	count, err := io.ReadFull(conn, head)
	if count == 0 {
		if err == nil || isTimeout(err) {
			// 中继还没说话：等真正的玩家数据即可
			return nil, nil
		}

		return nil, err
	}

	if head[0] != 'O' && head[0] != 'E' && head[0] != 'W' {
		return []byte{head[0]}, nil
	}

	line := []byte{head[0]}
	single := make([]byte, 1)
	for len(line) < 128 {
		count, err := conn.Read(single)
		if count > 0 {
			line = append(line, single[0])
			if single[0] == '\n' {
				break
			}

			continue
		}
		if err != nil && !isTimeout(err) {
			return nil, err
		}
		if isTimeout(err) {
			break
		}
	}

	text := strings.TrimSpace(string(line))
	if strings.HasPrefix(strings.ToUpper(text), "OK") {
		return nil, nil
	}
	// 非 ASCII 内容说明这不是状态行
	if !isPrintableASCII(line) {
		return line, nil
	}
	if text == "" {
		return nil, nil
	}

	return nil, fmt.Errorf("中继拒绝了隧道连接：%s", text)
}

// isTimeout 判断是否为超时错误（含 i/o timeout 包装）。
func isTimeout(err error) bool {
	var netErr net.Error

	return errors.As(err, &netErr) && netErr.Timeout()
}

// isPrintableASCII 判断一段字节是否全是可打印 ASCII（含 CR/LF/TAB）。
func isPrintableASCII(data []byte) bool {
	for _, value := range data {
		if value == '\r' || value == '\n' || value == '\t' {
			continue
		}
		if value < 0x20 || value > 0x7e {
			return false
		}
	}

	return true
}

// pipeBoth 双向透传，任意一侧读到 EOF/错误就返回。
func pipeBoth(left, right net.Conn) {
	done := make(chan struct{}, 2)

	copyOne := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		// 让对端也知道这条流结束了
		if closer, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		}
		done <- struct{}{}
	}

	go copyOne(left, right)
	go copyOne(right, left)

	<-done
}

// bumpConnections 更新活跃连接数并推送状态（前端实时看到"有人进来了"）。
func (p *redstoneProvider) bumpConnections(delta int32) {
	total := p.connections.Add(delta)
	if total < 0 {
		p.connections.Store(0)
		total = 0
	}

	p.mu.Lock()
	if p.status.State != StateHosting {
		p.mu.Unlock()

		return
	}
	p.status.Connections = int(total)
	status := p.status
	p.mu.Unlock()

	p.notify(status)
}

// ---- 状态发布 ----

// beginPhase 更新阶段文案（会话已存在时只改 Phase/Tip）。
func (p *redstoneProvider) beginPhase(phase, tip string) {
	p.mu.Lock()
	p.status.Provider = string(ProviderRedstone)
	p.status.State = StateStarting
	p.status.Phase = phase
	p.status.Tip = tip
	p.status.Error = ""
	status := p.status
	p.mu.Unlock()

	p.notify(status)
}

// fail 发布错误状态。
func (p *redstoneProvider) fail(err error) {
	p.mu.Lock()
	p.since = 0
	p.status = Status{
		Provider: string(ProviderRedstone),
		State:    StateError,
		Phase:    "出错",
		Error:    err.Error(),
		Tip:      "检查中继地址与本地服务端地址后可以重试。",
	}
	status := p.status
	p.mu.Unlock()

	p.notify(status)
}

func (p *redstoneProvider) publish(status Status) { p.notify(status) }
