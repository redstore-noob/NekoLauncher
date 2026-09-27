package mcserver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestShutdownAllStopsRunningServers 退出钩子：把运行中的服务器软停掉并返回说明行。
//
// 这里不真的拉起 Java：把状态与 stdin 直接摆成"运行中 + 有一个收指令的管道"，
// 验证 ShutdownAll 确实发出了 stop 并在进程退出后收敛。
func TestShutdownAllStopsRunningServers(t *testing.T) {
	id := "shutdown-stop"
	usePlayersTestServer(t, id, false)

	state := Default().ensureState(id)
	stdin := &recordingWriteCloser{}
	state.mu.Lock()
	state.status = StatusRunning
	state.stdin = stdin
	state.mu.Unlock()

	// 模拟服务端收到 stop 后自行退出
	go func() {
		for index := 0; index < 100; index++ {
			state.mu.Lock()
			status := state.status
			state.mu.Unlock()
			if status == StatusStopped {
				return
			}
			time.Sleep(20 * time.Millisecond)
			state.mu.Lock()
			if state.status == StatusStopping {
				state.status = StatusStopped
				state.stdin = nil
			}
			state.mu.Unlock()
		}
	}()

	notes := Default().ShutdownAll(5 * time.Second)

	if len(notes) != 1 {
		t.Fatalf("应返回一条说明：%+v", notes)
	}
	if !stdin.wrote("stop") {
		t.Fatalf("没有向服务器发送 stop：%q", stdin.content)
	}
	state.mu.Lock()
	status := state.status
	state.mu.Unlock()
	if status != StatusStopped {
		t.Fatalf("退出后状态 = %s，期望 stopped", status)
	}
}

// TestShutdownAllNoopWhenIdle 没有运行中的服务器时不做任何事（也不阻塞）。
func TestShutdownAllNoopWhenIdle(t *testing.T) {
	id := "shutdown-idle"
	usePlayersTestServer(t, id, false)

	state := Default().ensureState(id)
	state.mu.Lock()
	state.status = StatusStopped
	state.mu.Unlock()

	start := time.Now()
	notes := Default().ShutdownAll(2 * time.Second)
	if len(notes) != 0 {
		t.Fatalf("空闲时不该有说明行：%+v", notes)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("空闲时应立即返回，实际 %v", elapsed)
	}
}

// TestShutdownAllForcesStuckServer 宽限期内不退出的服务器要被强杀（不能挂住退出流程）。
func TestShutdownAllForcesStuckServer(t *testing.T) {
	id := "shutdown-stuck"
	usePlayersTestServer(t, id, false)

	// 起一个真实的长命子进程当"卡死的服务端"（Windows 下 ping 也能当长时间进程，
	// 但为了跨平台用自己：go test 二进制带一个特殊参数不现实，这里用系统 sleep 类命令）。
	cmd := stuckProcess(t)
	state := Default().ensureState(id)
	state.mu.Lock()
	state.status = StatusRunning
	state.stdin = nil // 没有 stdin：软停止发不出指令，只能等宽限期后强杀
	state.cmd = cmd
	state.mu.Unlock()

	start := time.Now()
	notes := Default().ShutdownAll(1200 * time.Millisecond)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("强杀路径耗时过久：%v", elapsed)
	}
	if len(notes) != 1 {
		t.Fatalf("应返回一条说明：%+v", notes)
	}
	// 关键：子进程必须真的死了。留下孤儿会锁住测试二进制，go test 收尾时删不掉
	// *.test.exe（Windows: unlinkat ... Access is denied），于是整轮测试
	// "结果全是 ok 但退出码 1"，把 CI 与本地验收都搅浑。
	if !waitForChildExit(cmd, 5*time.Second) {
		_ = cmd.Process.Kill()
		t.Fatal("宽限期后卡死的服务端进程仍在运行（强杀路径没生效）")
	}
}

// ---- 测试辅助 ----

// recordingWriteCloser 记录写入内容的伪 stdin。
type recordingWriteCloser struct {
	content string
}

func (w *recordingWriteCloser) Write(p []byte) (int, error) {
	w.content += string(p)

	return len(p), nil
}

func (w *recordingWriteCloser) Close() error { return nil }

func (w *recordingWriteCloser) wrote(substring string) bool {
	return len(w.content) > 0 && contains(w.content, substring)
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return index
		}
	}

	return -1
}

// TestAutoRestartSettingRoundTrip 自动重启开关读写。
func TestAutoRestartSettingRoundTrip(t *testing.T) {
	id := "auto-restart"
	dir := usePlayersTestServer(t, id, false)

	// 造一份最小 server.json
	cfg := &ServerConfig{ID: id, Name: "测试服", Core: CorePaper, MCVersion: "1.21.4", Port: 25565}
	if err := saveServerConfig(dir, cfg); err != nil {
		t.Fatalf("写配置失败：%v", err)
	}
	if AutoRestartEnabled(id) {
		t.Fatal("默认应为关闭")
	}
	if err := SetAutoRestart(id, true); err != nil {
		t.Fatalf("开启失败：%v", err)
	}
	if !AutoRestartEnabled(id) {
		t.Fatal("开启后应读到 true")
	}
	// 开关不该破坏其它字段
	reloaded, err := loadServerConfig(dir)
	if err != nil || reloaded.Port != 25565 || reloaded.Name != "测试服" {
		t.Fatalf("配置被破坏：%+v / %v", reloaded, err)
	}
	if err := SetAutoRestart(id, false); err != nil {
		t.Fatalf("关闭失败：%v", err)
	}
	if AutoRestartEnabled(id) {
		t.Fatal("关闭后应读到 false")
	}
	// 不存在的服务器不应 panic
	if SetAutoRestart("nope", true) == nil {
		t.Fatal("不存在的服务器应报错")
	}
}

// TestRestartServerRequiresRunning 未运行时重启应报错（提示先启动）。
func TestRestartServerRequiresRunning(t *testing.T) {
	id := "restart-idle"
	usePlayersTestServer(t, id, false)

	if err := Default().RestartServer(context.Background(), id); err == nil {
		t.Fatal("未运行时重启应报错")
	}
}

// TestNeoForgeVersionPrefix 版本前缀推导：老版本补 1.，26 起不补（回归保护）。
func TestNeoForgeVersionPrefix(t *testing.T) {
	cases := []struct {
		mc     string
		prefix string
		ok     bool
	}{
		{mc: "1.21.4", prefix: "21.4.", ok: true},
		{mc: "1.20.1", prefix: "20.1.", ok: true},
		{mc: "26.1", prefix: "26.1.", ok: true},
		{mc: "26.2", prefix: "26.2.", ok: true},
		{mc: "", ok: false},
	}

	for _, testCase := range cases {
		prefix, ok := neoforgeVersionPrefix(testCase.mc)
		if ok != testCase.ok {
			t.Fatalf("%q 可用性 = %v，期望 %v", testCase.mc, ok, testCase.ok)
		}
		if ok && prefix != testCase.prefix {
			t.Fatalf("%q → %q，期望 %q", testCase.mc, prefix, testCase.prefix)
		}
	}
}

// stuckProcess 起一个不会自己退出的子进程（测试强杀路径用）。
func stuckProcess(t *testing.T) *exec.Cmd {
	t.Helper()

	// 用测试二进制自身跑一个"睡眠"测试：go test 支持 -test.run 指定用例
	cmd := execCommand(context.Background(), os.Args[0], "-test.run=TestStuckProcessHelper", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), "NEKO_STUCK_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动替身进程失败：%v", err)
	}
	// 兜底：用例无论从哪条路径失败/提前返回，都不要把子进程留在系统里
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	return cmd
}

// waitForChildExit 等子进程退出；超时返回 false（调用方负责强杀并报错）。
func waitForChildExit(cmd *exec.Cmd, timeout time.Duration) bool {
	done := make(chan struct{})

	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// TestStuckProcessHelper 只作为 stuckProcess 的子进程入口（直接跑会立刻返回）。
func TestStuckProcessHelper(t *testing.T) {
	if os.Getenv("NEKO_STUCK_HELPER") == "" {
		t.Skip("不是替身调用")
	}
	time.Sleep(30 * time.Second)
}

var _ = filepath.Join
