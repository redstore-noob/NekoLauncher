package online

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/tools"
)

// 陶瓦联机（Terracotta）对接要点 —— 全部来自其公开实现：
//
//	https://github.com/burningtnt/Terracotta
//
//  1. 进程按官方约定的参数拉起：`terracotta --hmcl <文件>`。Windows 下它会再
//     派生一个分离的 `--hmcl2` 进程后自身退出，macOS/Linux 下直接在当前进程
//     跑服务；两种形态都会在 HTTP 服务就绪后把 `{"port":N}` 原子写进该文件
//     （先写 <文件>.tmp 再 rename），所以读文件就是"端口探测"。
//  2. 端口就绪后可用的接口（rocket，全部 GET）：
//     /meta                 版本与平台信息
//     /state                当前状态机快照（JSON）
//     /state/scanning       开始建房（扫描本机"对局域网开放"的世界）
//     /state/guesting?room= 加入房间（房间码不被接受时返回 400）
//     /state/ide            回到空闲
//     /panic?peaceful=true  结束守护进程
//  3. 陶瓦自身有全局锁，重复拉起会以 secondary 模式附加上已有实例，
//     所以"复用已在运行的实例"是官方行为，不是我们偷懒。
const (
	// terracottaHomepage 项目主页（前端"了解/下载"按钮用）
	terracottaHomepage = "https://github.com/burningtnt/Terracotta"
	// terracottaReleases 发行包下载页
	terracottaReleases = "https://github.com/burningtnt/Terracotta/releases"
	// terracottaMirrorReleases 国内镜像（GitHub 下载慢时的备选）
	terracottaMirrorReleases = "https://gitee.com/burningtnt/Terracotta/releases"

	// terracottaStartTimeout 等待端口文件的上限（进程要解包内嵌 EasyTier，留足余量）
	terracottaStartTimeout = 45 * time.Second
	// terracottaPortPollInterval 端口文件轮询间隔
	terracottaPortPollInterval = 200 * time.Millisecond
	// terracottaStateInterval 状态机轮询间隔（陶瓦界面自身也是这个量级）
	terracottaStateInterval = 700 * time.Millisecond
	// terracottaRequestTimeout 单次本地 HTTP 调用的超时
	terracottaRequestTimeout = 5 * time.Second
)

// terracottaProvider 陶瓦联机供应商：只管驱动本机那个守护进程。
type terracottaProvider struct {
	observer

	store settingStore

	mu      sync.Mutex
	port    int
	version string
	running bool
	// managed 本次启动器会话里由我们拉起的进程（关闭服务时只关这种）
	managed bool
	cmd     *exec.Cmd
	status  Status
	since   int64
	watcher context.CancelFunc
}

// newTerracottaProvider 构造供应商（store 为配置存储，测试可传内存实现）。
func newTerracottaProvider(store settingStore) *terracottaProvider {
	return &terracottaProvider{
		store:  store,
		status: Status{Provider: string(ProviderTerracotta), State: StateIdle, Phase: "空闲"},
	}
}

// ID 供应商标识。
func (p *terracottaProvider) ID() ProviderID { return ProviderTerracotta }

// Info 静态介绍 + 当前可用性（可执行文件是否存在）。
//
// Hint/HostNote/JoinNote 都是固定文案：前端会原样丢给 t() 查词典，
// 所以这里不能拼接路径、端口之类的运行期内容（路径在 Runtime 里单独给）。
func (p *terracottaProvider) Info() ProviderInfo {
	info := ProviderInfo{
		ID:       string(ProviderTerracotta),
		Name:     "陶瓦联机",
		Summary:  "基于 EasyTier 的虚拟局域网：贴一个房间码就能连，不需要公网 IP，也不用改路由器。",
		Homepage: terracottaHomepage,
		HostNote: "先进入存档 → Esc → 对局域网开放，再回到这里点「创建房间」；陶瓦会自动发现你的世界并生成房间码。",
		JoinNote: "把房主发来的房间码粘进来即可加入；连上后在游戏里直连 127.0.0.1 就能进服。",
	}

	if _, err := p.resolveBinary(); err != nil {
		info.Ready = false
		info.Hint = "本机还没有陶瓦联机：请先下载并解压，再在右侧设置里选中它的可执行文件。"
	} else {
		info.Ready = true
		info.Hint = "已找到可执行文件，路径见右侧设置。"
	}

	return info
}

// Runtime 本机守护进程状态（前端设置卡片与"关闭后台服务"按钮用）。
func (p *terracottaProvider) Runtime() Runtime {
	p.mu.Lock()
	defer p.mu.Unlock()

	binary, _ := p.resolveBinary()

	return Runtime{
		Provider: string(ProviderTerracotta),
		Running:  p.running,
		Managed:  p.managed,
		Version:  p.version,
		Binary:   binary,
		Port:     p.port,
	}
}

// Status 当前会话快照。
func (p *terracottaProvider) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.status
}

// Host 建房：让陶瓦扫描本机开放的局域网世界并生成房间码。
func (p *terracottaProvider) Host(options HostOptions) error {
	p.begin("正在准备陶瓦联机…", "第一次使用会先拉起本机服务，可能要几秒钟。")
	if err := p.ensureServer(); err != nil {
		p.fail(err)

		return err
	}

	p.begin("正在寻找局域网世界…", "还没找到世界？先进入存档 → Esc → 对局域网开放，陶瓦会自动发现它。")

	query := url.Values{}
	if player := strings.TrimSpace(options.Player); player != "" {
		query.Set("player", player)
	}
	if _, _, err := p.request("/state/scanning", query); err != nil {
		wrapped := fmt.Errorf("创建房间失败：%v", err)
		p.fail(wrapped)

		return wrapped
	}

	p.startWatcher()

	return nil
}

// Join 加入房间：房间码的形态（带不带 U/ 前缀）不做假设，逐个候选试。
func (p *terracottaProvider) Join(room, player string) error {
	variants := RoomCodeVariants(room)
	if len(variants) == 0 {
		err := errors.New("请先填写房主给你的房间码")
		p.fail(err)

		return err
	}

	p.begin("正在准备陶瓦联机…", "第一次使用会先拉起本机服务，可能要几秒钟。")
	if err := p.ensureServer(); err != nil {
		p.fail(err)

		return err
	}

	p.begin("正在连接房间…", "正在与房主建立虚拟局域网，第一次连接通常需要十几秒。")

	query := url.Values{}
	if player = strings.TrimSpace(player); player != "" {
		query.Set("player", player)
	}

	var lastErr error
	for _, variant := range variants {
		query.Set("room", variant)

		_, status, err := p.request("/state/guesting", query)
		if err == nil {
			p.startWatcher()

			return nil
		}
		if status == http.StatusBadRequest {
			lastErr = fmt.Errorf("房间码 %s 没有被接受", variant)

			continue
		}
		lastErr = err

		break
	}
	if lastErr == nil {
		lastErr = errors.New("房间码无效")
	}

	wrapped := fmt.Errorf("加入房间失败：%v", lastErr)
	p.fail(wrapped)

	return wrapped
}

// Leave 退出会话：把陶瓦状态机拉回空闲，但保留守护进程（马上再开一局更快）。
func (p *terracottaProvider) Leave() error {
	p.stopWatcher()

	p.mu.Lock()
	hadSession := p.since != 0
	p.since = 0
	port := p.port
	p.mu.Unlock()

	if hadSession && port != 0 {
		_, _, _ = p.request("/state/ide", nil)
	}

	p.publish(Status{
		Provider: string(ProviderTerracotta),
		State:    StateIdle,
		Phase:    "空闲",
		Tip:      "点「创建房间」开一局，或粘贴朋友的房间码加入。",
	})

	return nil
}

// Shutdown 结束本机守护进程（用户在界面上显式要求；会一并关掉用户自己开的窗口）。
func (p *terracottaProvider) Shutdown() error {
	p.stopWatcher()

	p.mu.Lock()
	port := p.port
	p.since = 0
	p.mu.Unlock()

	var err error
	if port != 0 {
		if _, _, requestErr := p.request("/panic", url.Values{"peaceful": []string{"true"}}); requestErr != nil {
			err = fmt.Errorf("关闭陶瓦联机失败：%v", requestErr)
		}
	}

	p.mu.Lock()
	p.port = 0
	p.version = ""
	p.running = false
	p.managed = false
	p.cmd = nil
	p.mu.Unlock()

	p.publish(Status{
		Provider: string(ProviderTerracotta),
		State:    StateIdle,
		Phase:    "空闲",
		Tip:      "点「创建房间」开一局，或粘贴朋友的房间码加入。",
	})

	return err
}

// Close 应用退出：只停本进程的轮询协程。陶瓦守护进程按设计常驻
// （它的界面提示就是"请保持陶瓦运行"），下次进来直接复用。
func (p *terracottaProvider) Close() {
	p.stopWatcher()
}

// ---- 进程与端口 ----

// resolveBinary 定位陶瓦联机可执行文件：用户指定 → 启动器数据目录 → PATH。
func (p *terracottaProvider) resolveBinary() (string, error) {
	if p.store != nil {
		if custom := strings.TrimSpace(p.store.Get(keyTerracottaPath)); custom != "" {
			if tools.FileExists(custom) {
				return custom, nil
			}

			return "", fmt.Errorf("设置里指定的陶瓦联机文件不存在：%s", custom)
		}
	}

	names := terracottaBinaryNames()
	if directory := config.StorageDirectory(); directory != "" {
		installDirectory := filepath.Join(directory, "terracotta")
		candidates := make([]string, 0, len(names))
		for _, name := range names {
			candidates = append(candidates, filepath.Join(installDirectory, name))
		}
		for _, candidate := range candidates {
			if tools.FileExists(candidate) {
				return candidate, nil
			}
		}
		// 固定名没命中时再按新版发行包的带版本号文件名找一遍
		// （自动安装解出来的就是 terracotta-<版本>-<平台> 这种名字）
		if found, locateErr := locateTerracottaBinary(installDirectory); locateErr == nil {
			return found, nil
		}
	}
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf(
		"没有找到陶瓦联机（Terracotta）可执行文件。请到 %s 下载后解压，"+
			"再在右侧「陶瓦联机」设置里选中它（也可参考国内镜像 %s）。",
		terracottaReleases, terracottaMirrorReleases)
}

// terracottaBinaryNames 各平台的候选文件名。
func terracottaBinaryNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"terracotta.exe", "terracotta-windows-amd64.exe"}
	}

	return []string{"terracotta", "terracotta-daemon"}
}

// ensureServer 保证本机有一个可用的陶瓦服务：可复用就复用，否则按官方约定拉起。
func (p *terracottaProvider) ensureServer() error {
	if p.currentPort() != 0 && p.refreshMeta() == nil {
		return nil
	}
	p.setPort(0)

	binary, err := p.resolveBinary()
	if err != nil {
		return err
	}

	probe := filepath.Join(os.TempDir(), fmt.Sprintf("nekolauncher-terracotta-%d.json", time.Now().UnixNano()))
	tools.RemoveFileIfExists(probe)
	tools.RemoveFileIfExists(probe + ".tmp")

	cmd := exec.Command(binary, "--hmcl", probe)
	tools.HideProcessWindow(cmd)
	cmd.Dir = filepath.Dir(binary)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动陶瓦联机失败：%v", err)
	}
	// Windows 下 --hmcl 会立刻派生 --hmcl2 后自己退出，这里只负责回收句柄；
	// macOS/Linux 下这个进程就是服务本体，等它自然结束即可。
	go func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(terracottaStartTimeout)
	for {
		if port := readTerracottaPortFile(probe); port != 0 {
			tools.RemoveFileIfExists(probe)

			p.mu.Lock()
			p.port = port
			p.managed = true
			p.cmd = cmd
			p.mu.Unlock()

			if err := p.refreshMeta(); err != nil {
				p.setPort(0)

				return fmt.Errorf("陶瓦联机已启动，但本地接口不可用：%v", err)
			}

			return nil
		}
		if time.Now().After(deadline) {
			tools.RemoveFileIfExists(probe)

			return errors.New("等待陶瓦联机启动超时（45 秒）。建议先手动运行一次 terracotta，" +
				"确认它能正常打开窗口，再回到联机页重试")
		}

		time.Sleep(terracottaPortPollInterval)
	}
}

// readTerracottaPortFile 读取端口探测文件；未就绪/内容不完整时返回 0
// （进程先写 .tmp 再 rename，读到即是完整 JSON，这里再校验一次更稳）。
func readTerracottaPortFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}

	var payload struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0
	}
	if payload.Port <= 0 || payload.Port > 65535 {
		return 0
	}

	return payload.Port
}

// refreshMeta 拉取 /meta，顺带确认服务确实活着。
func (p *terracottaProvider) refreshMeta() error {
	body, _, err := p.request("/meta", nil)
	if err != nil {
		return err
	}

	var meta struct {
		Version         string `json:"version"`
		EasytierVersion string `json:"easytier_version"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return err
	}

	p.mu.Lock()
	p.version = meta.Version
	p.running = true
	p.mu.Unlock()

	return nil
}

// request 调用本地接口；返回响应体、HTTP 状态码与错误（2xx 之外都算错误，
// 但状态码仍然返回，调用方可用它区分"房间码不被接受(400)"这类业务失败）。
func (p *terracottaProvider) request(path string, query url.Values) ([]byte, int, error) {
	port := p.currentPort()
	if port == 0 {
		return nil, 0, errors.New("陶瓦联机尚未启动")
	}

	target := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	client := &http.Client{Timeout: terracottaRequestTimeout}
	response, err := client.Get(target)
	if err != nil {
		return nil, 0, fmt.Errorf("无法连接陶瓦联机本地服务：%v", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return body, response.StatusCode, fmt.Errorf("陶瓦联机接口 %s 返回 %d", path, response.StatusCode)
	}

	return body, response.StatusCode, nil
}

func (p *terracottaProvider) currentPort() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.port
}

func (p *terracottaProvider) setPort(port int) {
	p.mu.Lock()
	p.port = port
	if port == 0 {
		p.running = false
	}
	p.mu.Unlock()
}

// ---- 状态机 ----

// terracottaStatePayload 陶瓦 /state 的响应（字段名与其 JSON 输出一致）。
type terracottaStatePayload struct {
	State      string `json:"state"`
	Room       string `json:"room"`
	URL        string `json:"url"`
	Difficulty string `json:"difficulty"`
	Type       *int   `json:"type"`
	Profiles   []struct {
		Name   string `json:"name"`
		Vendor string `json:"vendor"`
		Kind   string `json:"kind"`
	} `json:"profiles"`
}

// startWatcher 启动状态轮询（重复调用会替换掉上一个协程）。
func (p *terracottaProvider) startWatcher() {
	p.stopWatcher()

	ctx, cancel := context.WithCancel(context.Background())

	p.mu.Lock()
	p.watcher = cancel
	p.mu.Unlock()

	p.pollState()
	go func() {
		ticker := time.NewTicker(terracottaStateInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.pollState()
			}
		}
	}()
}

func (p *terracottaProvider) stopWatcher() {
	p.mu.Lock()
	cancel := p.watcher
	p.watcher = nil
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// pollState 拉一次 /state 并映射成统一状态。
func (p *terracottaProvider) pollState() {
	body, _, err := p.request("/state", nil)
	if err != nil {
		p.fail(fmt.Errorf("与陶瓦联机的本地服务失去联系（可能被手动关闭了）：%v", err))

		return
	}

	var payload terracottaStatePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}

	p.applyState(payload)
}

// applyState 把陶瓦状态机映射为统一快照。
func (p *terracottaProvider) applyState(payload terracottaStatePayload) {
	players := make([]Player, 0, len(payload.Profiles))
	for _, profile := range payload.Profiles {
		players = append(players, Player{Name: profile.Name, Kind: profile.Kind, Vendor: profile.Vendor})
	}

	p.mu.Lock()
	since := p.since
	p.mu.Unlock()

	status := Status{Provider: string(ProviderTerracotta), Players: players, Since: since}

	switch payload.State {
	case "host-scanning":
		status.State = StateStarting
		status.Phase = "正在寻找局域网世界…"
		status.Tip = "还没找到世界？先进入存档 → Esc → 对局域网开放，陶瓦会自动发现它。"
	case "host-starting":
		status.State = StateStarting
		status.Phase = "正在创建房间…"
		status.Tip = "正在申请联机房间，请稍候。"
	case "host-ok":
		room := FormatRoomCodeForDisplay(payload.Room)
		status.State = StateHosting
		status.Phase = "房间已就绪"
		status.Room = room
		status.Address = room
		status.Tip = "把房间码发给朋友，Ta 在联机页粘贴就能加入。"
	case "guest-connecting":
		status.State = StateStarting
		status.Phase = "正在连接房间…"
		status.Tip = "正在连接房主，请稍候。"
	case "guest-starting":
		status.State = StateStarting
		status.Phase = "正在建立虚拟局域网…"
		status.Tip = "正在与房主建立虚拟局域网，首次连接通常需要十几秒。"
	case "guest-ok":
		host, port := SplitHostPort(payload.URL)
		status.State = StateJoined
		status.Phase = "已进入房间"
		status.LocalAddress = payload.URL
		status.JoinHost = host
		status.JoinPort = port
		if payload.URL == "" {
			status.JoinHost, status.JoinPort = "", 0
		}
		status.Tip = "已经接入房主的局域网，点下面的「一键进服」就能直接进游戏。"
	case "exception":
		status.State = StateError
		status.Phase = "出错"
		status.Error = terracottaExceptionText(payload.Type)
		status.Tip = "点「退出房间」后可以重新建房或加入。"
	default: // waiting
		status.State = StateIdle
		status.Phase = "空闲"
		status.Tip = "点「创建房间」开一局，或粘贴朋友的房间码加入。"
		status.Since = 0
	}

	p.publish(status)

	if status.State == StateIdle || status.State == StateError {
		// 会话已经不在陶瓦那边了，没必要继续轮询
		p.mu.Lock()
		p.since = 0
		p.mu.Unlock()
		p.stopWatcher()
	}
}

// terracottaExceptionText 陶瓦 /state 里 exception 的 type 取值含义
// （对应其 ExceptionType 枚举顺序）。
func terracottaExceptionText(kind *int) string {
	if kind == nil {
		return "陶瓦联机发生了未知错误。"
	}

	switch *kind {
	case 0:
		return "连接不上房主：房间码可能不对，或房主已经退出。"
	case 1:
		return "房主的网络拒绝了连接，可能是防火墙或运营商拦截。"
	case 2:
		return "房客侧的虚拟局域网组件异常退出，请重试。"
	case 3:
		return "房主侧的虚拟局域网组件异常退出，请重试。"
	case 4:
		return "房主的 Minecraft 没有响应：请确认游戏里已经「对局域网开放」。"
	case 5:
		return "联机中心返回了无法识别的数据，建议升级陶瓦联机版本。"
	default:
		return fmt.Sprintf("陶瓦联机报错（代码 %d）。", *kind)
	}
}

// ---- 状态发布 ----

// begin 进入"准备中"：记录会话起点并立即推一次状态（前端按钮马上变灰）。
func (p *terracottaProvider) begin(phase, tip string) {
	since := nowUnix()

	p.mu.Lock()
	p.since = since
	p.mu.Unlock()

	p.publish(Status{
		Provider: string(ProviderTerracotta),
		State:    StateStarting,
		Phase:    phase,
		Tip:      tip,
		Since:    since,
	})
}

// fail 发布错误状态（同时停掉轮询，避免错误状态被后续 waiting 覆盖）。
func (p *terracottaProvider) fail(err error) {
	p.stopWatcher()

	p.mu.Lock()
	p.since = 0
	p.mu.Unlock()

	p.publish(Status{
		Provider: string(ProviderTerracotta),
		State:    StateError,
		Phase:    "出错",
		Error:    err.Error(),
		Tip:      "按提示处理后可以重新建房或加入。",
	})
}

// publish 交给管理器落地（供应商自身不直接碰前端）。
func (p *terracottaProvider) publish(status Status) {
	p.mu.Lock()
	p.status = status
	p.mu.Unlock()

	p.notify(status)
}
