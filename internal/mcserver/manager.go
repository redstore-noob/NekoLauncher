// 服务器运行期管理：java 子进程启停、控制台环形缓冲、CPU/内存采样、
// 在线玩家数（周期发送 list 指令并解析响应）。
package mcserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"nekolauncher/internal/logs"
)

const (
	consoleCapacity  = 800 // 控制台环形缓冲行数
	stopGracePeriod  = 15 * time.Second
	playerListPeriod = 10 * time.Second
)

var listResponseRe = regexp.MustCompile(`There are (\d+) of a max of (\d+) players online`)

// runtimeState 单个服务器的运行期数据。
type runtimeState struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	status   string
	logs     []string // 环形缓冲（超容量丢弃最旧行）
	cursor   int64    // 已写入的总行数（轮询游标，只增不减）
	players  int
	cpu      float64
	memoryMB float64
	lastCPU  *process.Process
	// cancelStart 取消"挑选/下载 Java → 拉起进程"这一段启动准备。
	// 该窗口内 cmd 还是 nil，强杀无处下手，只能靠取消 context 中止。
	cancelStart context.CancelFunc
	// rcon 该服务器的 RCON 配置（启动时从 server.properties 读/补）。
	rcon rconConfiguration
	// stopRequested 是否由用户主动停止（区分"手动停"与"崩溃退出"，
	// 后者才走自动重启）。
	stopRequested bool
	// restartCount / lastRestart 自动重启的窗口计数。
	restartCount int
	lastRestart  time.Time
}

// Manager 全部服务器的运行期管理器（进程全局单例）。
type Manager struct {
	mu           sync.Mutex
	runtimes     map[string]*runtimeState
	playerTicker *time.Ticker
	tickOnce     sync.Once
}

var defaultManager = &Manager{runtimes: map[string]*runtimeState{}}

// Default 返回全局管理器。
func Default() *Manager { return defaultManager }

func (m *Manager) state(id string) *runtimeState {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.runtimes[id]
}

func (m *Manager) ensureState(id string) *runtimeState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runtimes[id] == nil {
		m.runtimes[id] = &runtimeState{status: StatusStopped}
	}

	return m.runtimes[id]
}

// StartServer 启动服务器进程。status: starting →（检测到 Done）running。
func (m *Manager) StartServer(ctx context.Context, id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	dir := serverDirectory(id)
	cfg, err := loadServerConfig(dir)
	if err != nil {
		return fmt.Errorf("读取服务器配置失败：%w", err)
	}
	// EULA 只在创建时写过一次，用户（或别的启动器）删掉它之后，服务端会在
	// 控制台里打印一行提示就退出，前端只会一直停在"启动中"。这里提前挡掉。
	if err := requireEulaAccepted(dir); err != nil {
		return err
	}

	state := m.ensureState(id)
	state.mu.Lock()
	if state.status == StatusRunning || state.status == StatusStarting {
		state.mu.Unlock()

		return errors.New("服务器已在运行中")
	}
	state.status = StatusStarting
	// 启动准备阶段（可能包含 Java 下载）没有进程可杀，用户点"停止"时靠这个
	// context 中断；成功拉起进程后清掉，此后走正常的进程停止路径。
	startCtx, cancelStart := context.WithCancel(ctx)
	state.cancelStart = cancelStart
	state.mu.Unlock()

	// 启动准备阶段结束后清掉取消句柄：此后停止走进程路径，不再需要它。
	// （cancelStart 本身幂等，进程起来之后再调用没有副作用——execCommand
	// 用的是外层的 ctx，不受这里影响。）
	defer func() {
		state.mu.Lock()
		state.cancelStart = nil
		state.mu.Unlock()
		cancelStart()
	}()

	// 启动前补一次 RCON 配置：老服务器（本功能之前创建的）没有这几项，
	// 补上之后在线人数与停止指令就能走 RCON。写失败只记日志，不影响启动。
	if rcon, rconErr := ensureRCONProperties(dir); rconErr != nil {
		logs.Write("WARN", fmt.Sprintf("为服务器 %s 写入 RCON 配置失败：%v（将退回 stdin 控制台路径）", id, rconErr))
	} else {
		state.mu.Lock()
		state.rcon = rcon
		state.mu.Unlock()
	}

	// 上次启动器退出时可能留下仍在占用服务器目录的 Java 孤儿进程，
	// 不清理会导致服务端因 session.lock/latest.log 被占用而启动失败。
	orphanNotes := killOrphanServerProcesses(dir)

	// 按 MC 版本要求挑选 Java（缺失时自动下载），进度说明先收集下来，
	// 进程成功拉起后作为控制台的首批日志展示。
	var javaLogs []string
	java, err := resolveJavaForServer(startCtx, cfg, func(line string) {
		javaLogs = append(javaLogs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), line))
	})
	if err != nil {
		state.mu.Lock()
		state.status = StatusStopped
		state.mu.Unlock()

		if errors.Is(err, context.Canceled) {
			return errors.New("启动已取消")
		}

		return err
	}

	var args []string
	if cfg.MemoryMB > 0 {
		args = append(args, fmt.Sprintf("-Xmx%dM", cfg.MemoryMB))
	}
	if cfg.MemoryMinMB > 0 {
		args = append(args, fmt.Sprintf("-Xms%dM", cfg.MemoryMinMB))
	}
	// 图形化编辑的额外 JVM 参数（已校验不含 -jar / @入口）
	args = append(args, cfg.ExtraJavaArgs...)
	jarName := ""
	switch cfg.Core {
	case CoreVanilla:
		jarName = "server.jar"
	case CorePaper:
		jarName = "paper.jar"
	case CoreFabric:
		jarName = "fabric-server.jar"
	case CoreNeoForge:
		version := cfg.CoreVersion
		if version == "" {
			state.mu.Lock()
			state.status = StatusStopped
			state.mu.Unlock()

			return errors.New("缺少 NeoForge 版本信息，请重新创建服务器")
		}
		argsFile := fmt.Sprintf(
			"libraries/net/neoforged/neoforge/%s/win_args.txt", version)
		if _, err := os.Stat(filepath.Join(dir, argsFile)); err != nil {
			argsFile = fmt.Sprintf(
				"libraries/net/neoforged/neoforge/%s/unix_args.txt", version)
		}
		args = append(args, "@"+argsFile)
	}
	if jarName != "" {
		args = append(args, "-jar", jarName, "nogui")
	}

	cmd := execCommand(ctx, java, args...)
	cmd.Dir = dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		state.mu.Lock()
		state.status = StatusStopped
		state.mu.Unlock()

		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		state.mu.Lock()
		state.status = StatusStopped
		state.mu.Unlock()

		return err
	}
	cmd.Stderr = cmd.Stdout // 合并 stderr 到同一管道

	if err := cmd.Start(); err != nil {
		state.mu.Lock()
		state.status = StatusStopped
		state.mu.Unlock()

		return fmt.Errorf("启动 Java 进程失败：%w", err)
	}

	state.mu.Lock()
	state.cmd = cmd
	state.stdin = stdin
	// 保留 Java 选取阶段的进度日志（自动下载等），随控制台一起展示。
	// cursor 必须只增不减：它是"已写入的总行数"，前端拿上一轮的值当轮询游标，
	// 归零会让前端（游标还停在上一轮末尾）漏掉新会话开头的这批日志。
	// Poll 用 `cursor - (state.cursor - len(state.logs))` 换算窗口起点，
	// 窗口起点正好等于本次的 previousCursor，公式依然成立。
	previousCursor := state.cursor
	state.logs = append(append([]string{}, javaLogs...), orphanNotes...)
	state.cursor = previousCursor + int64(len(state.logs))
	state.players = 0
	state.mu.Unlock()

	go m.consumeOutput(id, state, stdout)
	go m.awaitExit(id, state, cmd)
	m.startPlayerTicker()

	return nil
}

// consumeOutput 读取子进程输出进环形缓冲，识别启动完成与 list 响应。
func (m *Manager) consumeOutput(id string, state *runtimeState, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		state.mu.Lock()
		state.cursor++
		state.logs = append(state.logs, line)
		if len(state.logs) > consoleCapacity {
			state.logs = state.logs[len(state.logs)-consoleCapacity:]
		}
		// Vanilla 系服务端启动完成标志
		if state.status == StatusStarting && strings.Contains(line, "Done (") {
			state.status = StatusRunning
		}
		if matches := listResponseRe.FindStringSubmatch(line); matches != nil {
			fmt.Sscanf(matches[1], "%d", &state.players)
		}
		state.mu.Unlock()
	}
}

// awaitExit 等进程退出并把状态落回 stopped；必要时按设置自动重启。
func (m *Manager) awaitExit(id string, state *runtimeState, cmd *exec.Cmd) {
	waitErr := cmd.Wait()
	state.mu.Lock()
	state.status = StatusStopped
	state.players = 0
	state.stdin = nil
	state.cmd = nil
	state.cursor++
	// 无条件留一行退出记录：启动阶段就崩掉的进程否则在控制台里毫无痕迹
	detail := fmt.Sprintf("[%s] 服务器进程已退出", time.Now().Format("15:04:05"))
	if waitErr != nil {
		detail = fmt.Sprintf("[%s] 服务器进程已退出：%v", time.Now().Format("15:04:05"), waitErr)
	}
	state.logs = append(state.logs, detail)
	if len(state.logs) > consoleCapacity {
		state.logs = state.logs[len(state.logs)-consoleCapacity:]
	}
	// 用户主动停止 / 重启：不触发自动重启
	userRequested := state.stopRequested
	state.stopRequested = false
	state.mu.Unlock()

	if !userRequested {
		m.maybeAutoRestart(id, waitErr)
	}
}

// autoRestartWindow 自动重启的观察窗口：窗口内最多重启 maxAutoRestarts 次，
// 避免"服务端一启动就崩"时无限重启把磁盘和 CPU 打满。
const (
	autoRestartDelay = 5 * time.Second
	maxAutoRestarts  = 3
	autoRestartReset = 10 * time.Minute
)

// maybeAutoRestart 崩溃自动重启（需在服务器配置里开启）。
func (m *Manager) maybeAutoRestart(id string, waitErr error) {
	cfg, err := loadServerConfig(serverDirectory(id))
	if err != nil || !cfg.AutoRestart {
		return
	}

	state := m.state(id)
	if state == nil {
		return
	}
	state.mu.Lock()
	now := time.Now()
	if state.lastRestart.Before(now.Add(-autoRestartReset)) {
		state.restartCount = 0
	}
	if state.restartCount >= maxAutoRestarts {
		state.logs = append(state.logs, fmt.Sprintf(
			"[%s] 已达自动重启上限（%d 次 / %d 分钟），不再重启：请检查崩溃原因",
			now.Format("15:04:05"), maxAutoRestarts, int(autoRestartReset.Minutes())))
		state.cursor++
		state.mu.Unlock()

		return
	}
	state.restartCount++
	state.lastRestart = now
	attempt := state.restartCount
	state.logs = append(state.logs, fmt.Sprintf(
		"[%s] 检测到异常退出（%v），%s 后自动重启（第 %d/%d 次）",
		now.Format("15:04:05"), waitErr, autoRestartDelay, attempt, maxAutoRestarts))
	state.cursor++
	state.mu.Unlock()

	time.Sleep(autoRestartDelay)

	// 用户在这 5 秒里手动启动/停止的话就别抢了
	state.mu.Lock()
	idle := state.status == StatusStopped
	state.mu.Unlock()
	if !idle {
		return
	}
	if err := m.StartServer(context.Background(), id); err != nil {
		logs.Write("WARN", fmt.Sprintf("自动重启 %s 失败：%v", id, err))
	}
}

// RestartServer 重启服务器：先软停止（宽限期内退出），再重新启动。
func (m *Manager) RestartServer(ctx context.Context, id string) error {
	state := m.state(id)
	if state == nil || !m.IsRunning(id) {
		return errors.New("服务器未在运行")
	}
	state.mu.Lock()
	// 标记为用户主动停止：awaitExit 不会把它当成崩溃去自动重启
	state.stopRequested = true
	state.restartCount = 0
	state.mu.Unlock()

	if err := m.StopServer(id, false); err != nil {
		return err
	}

	return m.StartServer(ctx, id)
}

// ShutdownAll 退出启动器前优雅停止全部托管服务器，返回给日志用的说明行。
//
// 顺序：先并发发 stop（让每个服务端各自保存世界），整体等 timeout；
// 到点还没退出的强杀进程树。这样"关启动器"不会再留下孤儿 Java 进程占着
// session.lock（原来只能靠下次启动时的 orphan.go 事后清理）。
func (m *Manager) ShutdownAll(timeout time.Duration) []string {
	m.mu.Lock()
	ids := make([]string, 0, len(m.runtimes))
	for id, state := range m.runtimes {
		state.mu.Lock()
		active := state.status == StatusRunning || state.status == StatusStarting
		state.mu.Unlock()
		if active {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()

	if len(ids) == 0 {
		return nil
	}

	var waitGroup sync.WaitGroup
	for _, id := range ids {
		waitGroup.Add(1)
		go func(serverID string) {
			defer waitGroup.Done()
			if err := m.StopServer(serverID, false); err != nil {
				logs.Write("WARN", fmt.Sprintf("退出前停止服务器 %s 失败：%v", serverID, err))
			}
		}(id)
	}

	done := make(chan struct{})
	go func() {
		waitGroup.Wait()
		close(done)
	}()

	notes := make([]string, 0, len(ids))
	select {
	case <-done:
		for _, id := range ids {
			notes = append(notes, fmt.Sprintf("已停止托管服务器 %s", id))
		}
	case <-time.After(timeout):
		notes = m.forceStopPendingServers(ids, timeout)
	}

	return notes
}

// forceStopWait 强杀之后等待进程真正消失的上限。
//
// 启动器拿到返回就要退出了，这里必须等"人真的没了"再走：以前是发完强杀就返回、
// 由软停止协程在 15 秒宽限期后再补一刀——启动器早退出了，那一刀根本没机会落下，
// 孤儿 Java 进程照旧占着 session.lock（P2-5 要解决的正是这件事）。
const forceStopWait = 3 * time.Second

// forceStopPendingServers 宽限期到点后的收尾：强杀所有还没退出的服务器，
// 等进程真的消失，并如实汇报（杀不掉就说杀不掉，不谎报"已停止"）。
func (m *Manager) forceStopPendingServers(ids []string, timeout time.Duration) []string {
	notes := make([]string, 0, len(ids))
	pending := make([]string, 0, len(ids))

	for _, id := range ids {
		if m.serverProcessGone(id) {
			notes = append(notes, fmt.Sprintf("已停止托管服务器 %s", id))

			continue
		}
		pending = append(pending, id)
	}

	for _, id := range pending {
		// force=true：不再走软停止（不发 stop、不等宽限期），直接结束进程树
		if err := m.StopServer(id, true); err != nil {
			logs.Write("WARN", fmt.Sprintf("强杀托管服务器 %s 失败：%v", id, err))
		}
	}

	deadline := time.Now().Add(forceStopWait)
	for _, id := range pending {
		gone := false
		for time.Now().Before(deadline) {
			if m.serverProcessGone(id) {
				gone = true

				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if gone {
			notes = append(notes, fmt.Sprintf("托管服务器 %s 未在 %s 内退出，已强制结束", id, timeout))

			continue
		}
		logs.Write("ERROR", fmt.Sprintf(
			"托管服务器 %s 强杀后仍在运行，可能残留进程占用服务器目录，请在任务管理器里结束它", id))
		notes = append(notes, fmt.Sprintf("托管服务器 %s 强杀后仍未退出，请手动检查进程", id))
	}

	return notes
}

// serverProcessGone 服务器是否已经没有存活的进程。
//
// 判据取"记录的进程是否还活着"，而不是只看状态：软停止会把状态置成 stopping，
// 只看状态既会把还在跑的进程当成已停，也会把已被杀死的进程当成在跑。
func (m *Manager) serverProcessGone(id string) bool {
	state := m.state(id)
	if state == nil {
		return true
	}
	state.mu.Lock()
	status := state.status
	cmd := state.cmd
	state.mu.Unlock()

	if status == StatusStopped {
		return true
	}
	if cmd == nil || cmd.Process == nil {
		// 还没有进程（在选 Java / 下载阶段）：算作没有需要强杀的东西
		return true
	}
	// 被 Wait 收过尸（ProcessState 非空）说明已退出
	if cmd.ProcessState != nil {
		return true
	}

	return !processAlive(cmd.Process.Pid)
}

// StopServer 停止服务器。force=false 软停止：发送 stop 指令让服务端正常
// 保存并退出，超过宽限期再强杀；force=true 硬停止：立即结束整棵进程树，
// 不给保存机会（适用于服务端卡死时，可能丢失未落盘的世界数据）。
func (m *Manager) StopServer(id string, force bool) error {
	state := m.state(id)
	if state == nil {
		return nil
	}
	state.mu.Lock()
	// stopping 状态也必须放行：软停止此刻正在等宽限期，如果再收到强杀请求却直接返回，
	// "强杀"就是空转（调用方以为已经处理，进程其实还活着）。
	active := state.status == StatusRunning || state.status == StatusStarting ||
		state.status == StatusStopping
	if !active {
		state.mu.Unlock()

		return nil
	}
	state.stopRequested = true
	state.status = StatusStopping
	stdin := state.stdin
	cancelStart := state.cancelStart
	cmd := state.cmd
	state.mu.Unlock()

	if force {
		switch {
		case cmd != nil && cmd.Process != nil:
			_ = killProcessTree(cmd.Process.Pid)
		case cancelStart != nil:
			// 还在挑选/下载 Java 的窗口里，没有进程可杀，只能中断准备流程；
			// 否则这里会"成功返回但服务器照样启动"，用户以为停掉了。
			cancelStart()
		}

		return nil
	}

	if stdin != nil {
		_, _ = io.WriteString(stdin, "stop\n")
	} else if cancelStart != nil {
		// 同样处于启动准备阶段：软停止也无从下手，直接取消更符合预期
		cancelStart()

		return nil
	}

	deadline := time.Now().Add(stopGracePeriod)
	for time.Now().Before(deadline) {
		state.mu.Lock()
		status := state.status
		state.mu.Unlock()
		if status == StatusStopped {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	state.mu.Lock()
	cmd = state.cmd
	state.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		// 杀整棵进程树：Java 可能经由转发器（javapath 等）启动，只杀直接
		// 子进程会把真正的 JVM 留成占用服务器目录的孤儿进程。
		_ = killProcessTree(cmd.Process.Pid)
	}

	return nil
}

// SendServerCommand 向控制台写入一条指令。
func (m *Manager) SendServerCommand(id, command string) error {
	state := m.state(id)
	if state == nil {
		return errors.New("服务器未运行")
	}
	state.mu.Lock()
	stdin := state.stdin
	status := state.status
	state.mu.Unlock()

	if stdin == nil || (status != StatusRunning && status != StatusStarting) {
		return errors.New("服务器未运行")
	}

	_, err := io.WriteString(stdin, strings.TrimRight(command, "\r\n")+"\n")

	return err
}

// Snapshot 轮询返回值：状态 + 占用 + 新增日志。
type Snapshot struct {
	Status     string
	Players    int
	CPUPercent float64
	MemoryMB   float64
	LogLines   []string
	NextCursor int64
}

// Poll 前端轮询：返回 cursor 之后的新日志与运行指标。
func (m *Manager) Poll(id string, cursor int64) Snapshot {
	snapshot := Snapshot{Status: StatusStopped, LogLines: []string{}}
	state := m.state(id)
	if state == nil {
		return snapshot
	}

	// CPU 百分比：非阻塞（相对上次采样计算差值，首次为 0）
	state.mu.Lock()
	proc := state.lastCPU
	state.mu.Unlock()
	if proc != nil {
		if percent, err := proc.Percent(0); err == nil {
			state.mu.Lock()
			state.cpu = percent
			state.mu.Unlock()
		}
	}
	state.mu.Lock()
	if proc == nil && state.cmd != nil && state.cmd.Process != nil {
		if p, err := process.NewProcess(int32(state.cmd.Process.Pid)); err == nil {
			state.lastCPU = p
		}
	}
	state.mu.Unlock()
	if proc != nil {
		if mem, err := proc.MemoryInfo(); err == nil {
			state.mu.Lock()
			state.memoryMB = float64(mem.RSS) / 1024 / 1024
			state.mu.Unlock()
		}
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	snapshot.Status = state.status
	snapshot.Players = state.players
	snapshot.CPUPercent = state.cpu
	snapshot.MemoryMB = state.memoryMB
	if state.cursor > cursor {
		from := int(cursor - (state.cursor - int64(len(state.logs))))
		if from < 0 {
			from = 0
		}
		snapshot.LogLines = append(snapshot.LogLines, state.logs[from:]...)
	}
	snapshot.NextCursor = state.cursor

	return snapshot
}

// startPlayerTicker 周期刷新在线人数：优先 RCON，退回 stdin 控制台 list。
//
// RCON 更稳：不依赖服务端 stdout 的文案格式，也不会被日志刷屏影响；
// 没配 RCON 的服务器（本功能之前创建的、或用户手动关掉的）仍走原来的 stdin 路径，
// 响应由 consumeOutput 的 listResponseRe 解析。
func (m *Manager) startPlayerTicker() {
	m.tickOnce.Do(func() {
		m.playerTicker = time.NewTicker(playerListPeriod)
		go func() {
			for range m.playerTicker.C {
				m.mu.Lock()
				all := make([]*runtimeState, 0, len(m.runtimes))
				for _, state := range m.runtimes {
					all = append(all, state)
				}
				m.mu.Unlock()
				for _, state := range all {
					m.refreshPlayers(state)
				}
			}
		}()
	})
}

// refreshPlayers 刷新单个服务器的在线人数（RCON 优先）。
func (m *Manager) refreshPlayers(state *runtimeState) {
	state.mu.Lock()
	running := state.status == StatusRunning
	stdin := state.stdin
	rcon := state.rcon
	state.mu.Unlock()

	if !running {
		return
	}

	if rcon.Enabled {
		ctx, cancel := context.WithTimeout(context.Background(), rconCommandTimeout)
		response, err := runRCONCommand(ctx, rconAddress(rcon.Port), rcon.Password, "list")
		cancel()

		if err == nil {
			if players, ok := parsePlayerList(response); ok {
				state.mu.Lock()
				state.players = players
				state.mu.Unlock()

				return
			}
		}
		// RCON 失败（服务端还没监听 / 密码被改 / 端口占）：静默退回 stdin，
		// 不刷日志——每秒一次的轮询刷日志比"人数晚一拍"更烦人。
	}

	if stdin != nil {
		_, _ = io.WriteString(stdin, "list\n")
	}
}

// parsePlayerList 从 RCON 的 list 响应里取在线人数。
// 响应形如 "There are 2 of a max of 20 players online: A, B"。
func parsePlayerList(response string) (int, bool) {
	matches := listResponseRe.FindStringSubmatch(response)
	if matches == nil {
		return 0, false
	}
	count := 0
	if _, err := fmt.Sscanf(matches[1], "%d", &count); err != nil {
		return 0, false
	}

	return count, true
}

// RunRCONCommand 对指定服务器执行一条 RCON 指令（广播、查玩家等）。
// 未配置 RCON 时返回错误，由调用方决定是否退回控制台。
func (m *Manager) RunRCONCommand(id, command string) (string, error) {
	state := m.state(id)
	if state == nil {
		return "", errors.New("服务器未在运行")
	}
	state.mu.Lock()
	rcon := state.rcon
	running := state.status == StatusRunning
	state.mu.Unlock()

	if !running {
		return "", errors.New("服务器未在运行")
	}
	if !rcon.Enabled {
		return "", errors.New("该服务器未启用 RCON")
	}

	return runRCONCommand(context.Background(), rconAddress(rcon.Port), rcon.Password, command)
}

// IsRunning 服务器进程是否存活。
func (m *Manager) IsRunning(id string) bool {
	state := m.state(id)
	if state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()

	return state.status == StatusRunning || state.status == StatusStarting ||
		state.status == StatusStopping
}
