package online

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/config"
)

// ---- 设置持久化 ----
//
// 联机设置落在 launcher.yaml 的 online.* 键上（全局 KV，用户可直接编辑）。
// 这里抽一层 settingStore，测试里换成内存实现就不必碰真实配置目录。

const (
	keyProvider       = "online.provider"
	keyPlayer         = "online.player"
	keyTerracottaPath = "online.terracottaPath"
	keyRedstoneRelay  = "online.redstoneRelay"
	keyRedstoneKey    = "online.redstoneKey"
	keyTarget         = "online.target"
	keyServerID       = "online.serverID"
	keyMaxPlayers     = "online.maxPlayers"
)

// settingStore 全局 KV 读写（生产实现接 internal/config）。
type settingStore interface {
	Get(key string) string
	Set(key, value string) bool
}

// configStore 基于 launcher.yaml 的实现。
type configStore struct{}

func (configStore) Get(key string) string { return config.GetValue(key) }

func (configStore) Set(key, value string) bool { return config.SetValue(key, value) }

// DefaultRelayAddress 红石联机官方中继（模组内置的上海节点）。
const DefaultRelayAddress = "122.51.108.96"

// DefaultTargetAddress 联机转发目标的默认值：本机默认端口上的 Minecraft。
const DefaultTargetAddress = "127.0.0.1:25565"

// DefaultMaxPlayers 红石联机隧道并发上限的默认值。
const DefaultMaxPlayers = 8

// localServerStartTimeout 建房时等待"启动器托管的服务器"就绪的上限。
const localServerStartTimeout = 90 * time.Second

// loadSettings 读取并规范化设置；store 为 nil 时返回默认值（测试/首启）。
func loadSettings(store settingStore) Settings {
	settings := Settings{
		Provider:       string(ProviderTerracotta),
		TerracottaPath: "",
		RedstoneRelay:  DefaultRelayAddress,
		Target:         DefaultTargetAddress,
		MaxPlayers:     DefaultMaxPlayers,
	}
	if store == nil {
		return settings
	}

	if value := strings.TrimSpace(store.Get(keyProvider)); value != "" {
		settings.Provider = value
	}
	settings.Player = store.Get(keyPlayer)
	settings.TerracottaPath = store.Get(keyTerracottaPath)
	settings.RedstoneRelay = store.Get(keyRedstoneRelay)
	settings.RedstoneKey = store.Get(keyRedstoneKey)
	settings.Target = store.Get(keyTarget)
	settings.ServerID = store.Get(keyServerID)
	if value, err := strconv.Atoi(strings.TrimSpace(store.Get(keyMaxPlayers))); err == nil {
		settings.MaxPlayers = value
	}

	return normalizeSettings(settings)
}

// normalizeSettings 补齐空字段并夹紧取值范围。
func normalizeSettings(settings Settings) Settings {
	if strings.TrimSpace(settings.Provider) == "" {
		settings.Provider = string(ProviderTerracotta)
	}
	if strings.TrimSpace(settings.RedstoneRelay) == "" {
		settings.RedstoneRelay = DefaultRelayAddress
	}
	if strings.TrimSpace(settings.Target) == "" {
		settings.Target = DefaultTargetAddress
	}
	if settings.MaxPlayers <= 0 {
		settings.MaxPlayers = DefaultMaxPlayers
	}
	if settings.MaxPlayers > 50 {
		settings.MaxPlayers = 50
	}
	settings.Player = strings.TrimSpace(settings.Player)
	settings.TerracottaPath = strings.TrimSpace(settings.TerracottaPath)
	settings.RedstoneRelay = strings.TrimSpace(settings.RedstoneRelay)
	settings.RedstoneKey = strings.TrimSpace(settings.RedstoneKey)
	settings.Target = strings.TrimSpace(settings.Target)
	settings.ServerID = strings.TrimSpace(settings.ServerID)

	return settings
}

// saveSettings 全量落盘；任一键写入失败时返回错误（其余键仍会尝试写入）。
func saveSettings(store settingStore, settings Settings) error {
	if store == nil {
		return errors.New("配置存储不可用")
	}

	settings = normalizeSettings(settings)
	// 红石联机密钥在保存时就校验形态：等到建房时才报错，用户只会看到
	// "注册联机密钥失败"，不知道是自己粘贴密钥时带进了空格。
	if err := validateAPIKey(settings.RedstoneKey); err != nil {
		return err
	}
	pairs := []struct{ key, value string }{
		{keyProvider, settings.Provider},
		{keyPlayer, settings.Player},
		{keyTerracottaPath, settings.TerracottaPath},
		{keyRedstoneRelay, settings.RedstoneRelay},
		{keyRedstoneKey, settings.RedstoneKey},
		{keyTarget, settings.Target},
		{keyServerID, settings.ServerID},
		{keyMaxPlayers, strconv.Itoa(settings.MaxPlayers)},
	}

	for _, pair := range pairs {
		if !store.Set(pair.key, pair.value) {
			return fmt.Errorf("保存联机设置失败：%s", pair.key)
		}
	}

	return nil
}

// saveSetting 写单个键（供应商在运行期顺手缓存 API Key 等）。
func saveSetting(store settingStore, key, value string) bool {
	if store == nil {
		return false
	}

	return store.Set(key, value)
}

// ---- 供应商 ----

// Provider 一个联机供应商：管理器只认这五个动作。
type Provider interface {
	ID() ProviderID
	// Info 静态介绍（页面展示；Ready 反映"现在能不能用"）
	Info() ProviderInfo
	// Host 建房：同步返回"是否成功发起"，后续进展通过状态推送
	Host(options HostOptions) error
	// Join 加入别人的房间
	Join(room, player string) error
	// Leave 退出当前会话（保留本机后台服务，方便马上再开一局）
	Leave() error
	// Status 当前状态快照
	Status() Status
	// Runtime 本机运行时状态
	Runtime() Runtime
	// Shutdown 关闭本机后台服务/隧道（用户在界面上显式要求时调用）
	Shutdown() error
	// Close 应用退出：停掉协程与连接，不动用户自己开的程序
	Close()
}

// observer 供应商 → 管理器的状态回调。供应商内部持锁时不要直接调 notify，
// 统一走这里，由管理器落地并转发给前端。
type observer struct {
	mu      sync.Mutex
	onState func(Status)
}

func (o *observer) setObserver(handler func(Status)) {
	o.mu.Lock()
	o.onState = handler
	o.mu.Unlock()
}

func (o *observer) notify(status Status) {
	o.mu.Lock()
	handler := o.onState
	o.mu.Unlock()

	if handler != nil {
		handler(status)
	}
}

// ---- 管理器 ----

// Manager 联机会话管理器：**每个供应商各持一个会话**，互不打扰。
//
// 为什么不是"全局单会话"：陶瓦的房间（虚拟局域网）与红石的隧道（公网中继）
// 是两条完全独立的链路，一个走不通不代表另一个走不通——开局域网世界给近处的
// 朋友、同时用中继发给连不上虚拟局域网的人，是很常见的用法。因此换供应商不再
// 打断另一家；只有对**同一家**再次建房/加入才会先退出它自己的旧会话。
type Manager struct {
	// opMu 串行化 Host/Join/Leave/Shutdown：单家的会话是独占资源，
	// 两个并发的建房请求会互相把对方的进程/隧道踩掉。
	opMu sync.Mutex

	mu        sync.Mutex
	store     settingStore
	providers map[ProviderID]Provider
	// sessions 正在使用的会话（键是供应商 id）；没有会话的供应商不在表里
	sessions  map[ProviderID]Provider
	onChanged func(Status)

	// LocalServers / StartLocalServer 由绑定层注入，避免 online 包直接依赖
	// mcserver 的下载与进程管理（也便于测试注入假实现）。
	LocalServers     func() []LocalServer
	StartLocalServer func(ctx context.Context, id string, timeout time.Duration) (int, error)
	// PlayersFor 同样由绑定层注入：指定托管服务器里当前在线的玩家
	// （红石联机转发托管服务器时用它补成员名单）。
	PlayersFor func(serverID string) []Player
}

var (
	defaultOnce    sync.Once
	defaultManager *Manager
)

// Default 进程内单例：联机会话（含后台 terracotta 进程与中继隧道）跨页面常驻。
func Default() *Manager {
	defaultOnce.Do(func() {
		defaultManager = NewManager(configStore{})
	})

	return defaultManager
}

// NewManager 构造管理器并注册全部供应商（测试可传内存 store）。
func NewManager(store settingStore) *Manager {
	manager := &Manager{
		store:     store,
		providers: map[ProviderID]Provider{},
		sessions:  map[ProviderID]Provider{},
	}

	for _, provider := range []Provider{newTerracottaProvider(store), newRedstoneProvider(store)} {
		manager.providers[provider.ID()] = provider
		// 红石联机需要"转发目标服务器里谁在线"这个外部信息，运行期读取
		// manager.PlayersFor（绑定层可能在 NewManager 之后才注入）
		if redstone, ok := provider.(*redstoneProvider); ok {
			redstone.playersFor = manager.serverPlayers
		}
	}

	return manager
}

// serverPlayers 转发给注入的实现；未注入时返回 nil（成员列表保持为空）。
func (m *Manager) serverPlayers(serverID string) []Player {
	m.mu.Lock()
	provider := m.PlayersFor
	m.mu.Unlock()

	if provider == nil {
		return nil
	}

	return provider(serverID)
}

// SetOnChanged 注册状态变更回调（绑定层把它桥接为 online:changed 事件）。
func (m *Manager) SetOnChanged(handler func(Status)) {
	m.mu.Lock()
	m.onChanged = handler
	m.mu.Unlock()
}

// Providers 全部供应商的介绍，顺序固定（陶瓦在前，红石在后）。
func (m *Manager) Providers() []ProviderInfo {
	infos := make([]ProviderInfo, 0, len(m.providers))
	for _, id := range []ProviderID{ProviderTerracotta, ProviderRedstone} {
		if provider, ok := m.providers[id]; ok {
			infos = append(infos, provider.Info())
		}
	}

	return infos
}

// Status 指定供应商的会话快照（前端首帧按标签页各取一份）。
func (m *Manager) Status(providerID string) Status {
	provider, err := m.provider(NormalizeProviderID(providerID))
	if err != nil {
		return Status{Provider: string(NormalizeProviderID(providerID)), State: StateIdle}
	}

	return normalizeStatus(provider.Status(), provider.ID())
}

// Statuses 全部供应商的会话快照，顺序固定（陶瓦在前，红石在后）。
func (m *Manager) Statuses() []Status {
	statuses := make([]Status, 0, len(m.providers))
	for _, id := range []ProviderID{ProviderTerracotta, ProviderRedstone} {
		provider, ok := m.providers[id]
		if !ok {
			continue
		}
		statuses = append(statuses, normalizeStatus(provider.Status(), id))
	}

	return statuses
}

// normalizeStatus 补齐快照里的兜底字段（前端直接渲染，不能有 nil 切片）。
func normalizeStatus(status Status, id ProviderID) Status {
	if strings.TrimSpace(status.Provider) == "" {
		status.Provider = string(id)
	}
	if status.Players == nil {
		status.Players = []Player{}
	}
	// 未开工的供应商给一份 idle 快照，前端首帧不用特判空值
	if status.State == "" {
		status.State = StateIdle
	}

	return status
}

// Settings 读取联机设置。
func (m *Manager) Settings() Settings { return loadSettings(m.store) }

// SaveSettings 保存联机设置（顺便让运行时按新设置生效，例如换中继）。
func (m *Manager) SaveSettings(settings Settings) error { return saveSettings(m.store, settings) }

// Runtime 指定供应商的本机运行时状态。
func (m *Manager) Runtime(providerID string) Runtime {
	provider, err := m.provider(NormalizeProviderID(providerID))
	if err != nil {
		return Runtime{Provider: string(NormalizeProviderID(providerID))}
	}

	return provider.Runtime()
}

// InstallTerracotta 自动下载安装陶瓦联机（管理器转发给供应商，便于绑定层调用）。
func (m *Manager) InstallTerracotta(ctx context.Context) (TerracottaInstallResult, error) {
	provider, err := m.provider(ProviderTerracotta)
	if err != nil {
		return TerracottaInstallResult{}, err
	}
	terracotta, ok := provider.(*terracottaProvider)
	if !ok {
		return TerracottaInstallResult{}, errors.New("陶瓦联机供应商不可用")
	}

	return terracotta.InstallTerracotta(ctx)
}

// RelayOptions 红石联机可选的中继节点（内置 + 用户自定义，按地址去重）。
func (m *Manager) RelayOptions() []RelayOption { return RedstoneRelayOptions() }

// RelayList 用户自定义中继节点原文（每行一条）。
func (m *Manager) RelayList() string { return GetRedstoneRelayList() }

// SaveRelayList 保存用户自定义中继节点。
func (m *Manager) SaveRelayList(list string) error {
	if m.store == nil {
		return errors.New("配置存储不可用")
	}
	SaveRedstoneRelayList(list)

	return nil
}

// ProbeRelays 并发预检全部中继节点，按"可达优先、延迟升序"返回。
func (m *Manager) ProbeRelays(ctx context.Context) []RelayProbe {
	return ProbeRedstoneRelays(ctx, RedstoneRelayOptions())
}

// UseFastestRelay 预检全部节点并把设置里的中继换成最快的一个（用户点
// 「自动选最快节点」时调用）。全部不可达时返回错误，不改动设置。
func (m *Manager) UseFastestRelay(ctx context.Context) (RelayProbe, error) {
	probe, ok := FastestRedstoneRelay(ctx)
	if !ok {
		return RelayProbe{}, errors.New("所有中继节点都不可达：请检查网络，或填写自己的节点")
	}

	settings := m.Settings()
	settings.RedstoneRelay = probe.Address
	if err := m.SaveSettings(settings); err != nil {
		return RelayProbe{}, err
	}

	return probe, nil
}

// Host 建房。
func (m *Manager) Host(options HostOptions) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	providerID := NormalizeProviderID(options.Provider)
	provider, err := m.provider(providerID)
	if err != nil {
		return err
	}

	// 只退出这一家的旧会话：另一家的会话（另一条链路）继续跑
	m.leaveLocked(providerID)

	settings := m.Settings()
	options.Player = strings.TrimSpace(options.Player)
	if options.Player == "" {
		options.Player = settings.Player
	}
	if options.MaxPlayers <= 0 {
		options.MaxPlayers = settings.MaxPlayers
	}

	if providerID == ProviderRedstone {
		target, err := m.resolveTarget(options)
		if err != nil {
			return err
		}
		options.Target = target
	}

	m.setSession(provider)

	return provider.Host(options)
}

// resolveTarget 决定红石联机的本地转发目标：优先"启动器托管的服务器"，
// 需要时顺手把它启动起来（新手最容易卡在"忘了开服就点联机"）。
func (m *Manager) resolveTarget(options HostOptions) (string, error) {
	settings := m.Settings()
	serverID := strings.TrimSpace(options.ServerID)
	target := strings.TrimSpace(options.Target)

	if serverID != "" && m.StartLocalServer != nil {
		// 拉起服务器可能要几十秒，先推一次"准备中"，否则界面这段时间是空白的
		m.publish(Status{
			Provider: string(ProviderRedstone),
			State:    StateStarting,
			Phase:    "正在启动本机服务器…",
			Tip:      "要转发的服务器还没运行，先把它启动起来。",
			Since:    nowUnix(),
		})

		port, err := m.StartLocalServer(context.Background(), serverID, localServerStartTimeout)
		if err != nil {
			return "", err
		}
		target = fmt.Sprintf("127.0.0.1:%d", port)
	}
	if target == "" {
		target = settings.Target
	}

	return NormalizeTargetAddress(target)
}

// Join 加入房间。
func (m *Manager) Join(providerID, room, player string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	id := NormalizeProviderID(providerID)
	provider, err := m.provider(id)
	if err != nil {
		return err
	}

	m.leaveLocked(id)

	if strings.TrimSpace(player) == "" {
		player = m.Settings().Player
	}

	m.setSession(provider)

	return provider.Join(room, player)
}

// Leave 退出指定供应商的会话（只停这一家，另一家的会话不受影响）；
// providerID 为空时退出全部。
func (m *Manager) Leave(providerID string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	if strings.TrimSpace(providerID) == "" {
		var failure error
		for _, id := range []ProviderID{ProviderTerracotta, ProviderRedstone} {
			if err := m.leaveLocked(id); err != nil {
				failure = err
			}
		}

		return failure
	}

	return m.leaveLocked(NormalizeProviderID(providerID))
}

// Shutdown 关闭指定供应商在本机的后台服务（陶瓦：结束守护进程；红石：断开隧道）。
func (m *Manager) Shutdown(providerID string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	provider, err := m.provider(NormalizeProviderID(providerID))
	if err != nil {
		return err
	}

	return provider.Shutdown()
}

// LocalServerOptions 供前端选择"转发到哪台本机服务器"。
func (m *Manager) LocalServerOptions() []LocalServer {
	if m.LocalServers == nil {
		return []LocalServer{}
	}

	servers := m.LocalServers()
	if servers == nil {
		return []LocalServer{}
	}

	return servers
}

// Close 应用退出：停掉后台协程与网络连接。刻意不结束用户自己开的
// Terracotta 窗口（那属于用户，不属于启动器）。
func (m *Manager) Close() {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	for _, provider := range m.providers {
		provider.Close()
	}
}

// leaveLocked 退出指定供应商的会话（调用方需已持 opMu）；返回退出时的错误。
func (m *Manager) leaveLocked(id ProviderID) error {
	provider := m.session(id)
	if provider == nil {
		return nil
	}

	err := provider.Leave()
	m.publish(provider.Status())

	return err
}

// session 取指定供应商正在使用的会话（没有则 nil）。
func (m *Manager) session(id ProviderID) Provider {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.sessions[id]
}

// setSession 登记会话所属供应商：把它的状态回调接到管理器上。
func (m *Manager) setSession(provider Provider) {
	if provider == nil {
		return
	}
	if base, ok := provider.(interface{ setObserver(func(Status)) }); ok {
		base.setObserver(m.publish)
	}

	m.mu.Lock()
	m.sessions[provider.ID()] = provider
	m.mu.Unlock()
}

// publish 把状态转发给前端（供应商内部持锁时不要直接调用）。
func (m *Manager) publish(status Status) {
	status = normalizeStatus(status, NormalizeProviderID(status.Provider))

	m.mu.Lock()
	handler := m.onChanged
	m.mu.Unlock()

	if handler != nil {
		handler(status)
	}
}

func (m *Manager) provider(id ProviderID) (Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	provider, ok := m.providers[id]
	if !ok {
		return nil, fmt.Errorf("未知的联机供应商：%s", id)
	}

	return provider, nil
}

// ---- 地址与小工具 ----

// NormalizeTargetAddress 规范化本机转发目标：允许 host、host:port、:port，
// 缺端口补 25565（Minecraft 默认端口）。
func NormalizeTargetAddress(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errors.New("请先填写本地服务器地址，例如 127.0.0.1:25565")
	}

	// 中文全角冒号也认：用户从聊天/教程里复制粘贴很常见
	value = strings.ReplaceAll(value, "：", ":")
	if !strings.Contains(value, ":") {
		if !validHost(value) {
			return "", fmt.Errorf("地址格式不正确：%s", raw)
		}

		return net.JoinHostPort(value, "25565"), nil
	}

	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return "", fmt.Errorf("本地服务器地址格式不正确：%s", raw)
	}
	if strings.TrimSpace(host) == "" {
		host = "127.0.0.1"
	}
	if !validHost(host) {
		return "", fmt.Errorf("地址格式不正确：%s", host)
	}
	number, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || number <= 0 || number > 65535 {
		return "", fmt.Errorf("端口不合法：%s", port)
	}

	return net.JoinHostPort(host, strconv.Itoa(number)), nil
}

// validHost 判断主机部分是否像一个 IP / 域名：挡掉中文、空格等明显不是地址的
// 输入（否则 net.JoinHostPort 会照单全收，错误要到连接时才暴露）。
func validHost(host string) bool {
	if host == "" {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}

	for _, char := range host {
		switch {
		case char >= 'a' && char <= 'z',
			char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9',
			char == '.', char == '-', char == '_':
		default:
			return false
		}
	}

	return true
}

// SplitHostPort 拆分 host:port，供"一键进服"参数使用；端口缺失时用 25565。
func SplitHostPort(address string) (string, int) {
	normalized, err := NormalizeTargetAddress(address)
	if err != nil {
		return strings.TrimSpace(address), 25565
	}

	host, port, err := net.SplitHostPort(normalized)
	if err != nil {
		return strings.TrimSpace(address), 25565
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		number = 25565
	}

	return host, number
}

// DisplayAddress 展示用地址：默认端口时省略 :25565（Minecraft 玩家的习惯写法）。
func DisplayAddress(address string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return strings.TrimSpace(address)
	}
	if port == "25565" {
		return host
	}

	return net.JoinHostPort(host, port)
}

// nowUnix 会话时间戳（统一在这里取，便于测试覆盖）。
func nowUnix() int64 { return time.Now().Unix() }
