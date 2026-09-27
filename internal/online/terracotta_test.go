package online

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTerracotta 陶瓦守护进程的 HTTP 替身：按脚本返回 /state 并记录收到的请求。
type fakeTerracotta struct {
	mu       sync.Mutex
	state    map[string]any
	requests []string
	// rescanState 非空时，命中 /state/scanning 会切回这份状态
	// （模仿真实守护进程：被 /state/ide 收回后再扫一次又会进入 host-ok）
	rescanState map[string]any
	// rejectGuests 前 N 次 /state/guesting 返回 400（模拟房间码前缀形态不对）
	rejectGuests int
	panicCalls   int
}

func (f *fakeTerracotta) setState(state map[string]any) {
	f.mu.Lock()
	f.state = state
	f.mu.Unlock()
}

func (f *fakeTerracotta) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string{}, f.requests...)
}

func (f *fakeTerracotta) hasCall(prefix string) bool {
	for _, entry := range f.calls() {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}

	return false
}

func (f *fakeTerracotta) handler() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, request.URL.Path+"?"+request.URL.RawQuery)
		state := f.state
		f.mu.Unlock()

		switch request.URL.Path {
		case "/meta":
			_, _ = writer.Write([]byte(`{"version":"1.0.0-test","easytier_version":"2.0.0"}`))
		case "/state/scanning":
			f.mu.Lock()
			if f.rescanState != nil {
				f.state = f.rescanState
			}
			f.mu.Unlock()
			_, _ = writer.Write([]byte(`{}`))
		case "/state/guesting":
			// 只有进房请求才消耗"拒绝次数"，别把 /meta 之类的探测也算进去
			f.mu.Lock()
			reject := f.rejectGuests > 0
			if reject {
				f.rejectGuests--
			}
			f.mu.Unlock()

			if reject {
				writer.WriteHeader(http.StatusBadRequest)

				return
			}
			_, _ = writer.Write([]byte(`{}`))
		case "/state/ide":
			f.setState(map[string]any{"state": "waiting"})
			_, _ = writer.Write([]byte(`{}`))
		case "/state":
			payload, _ := json.Marshal(state)
			_, _ = writer.Write(payload)
		case "/panic":
			f.mu.Lock()
			f.panicCalls++
			f.mu.Unlock()
			_, _ = writer.Write([]byte(`{}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}
}

// newTestTerracotta 起一个假的陶瓦服务并返回指向它的供应商。
func newTestTerracotta(t *testing.T, fake *fakeTerracotta) *terracottaProvider {
	t.Helper()

	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)

	provider := newTerracottaProvider(newMemoryStore())
	provider.port = portOf(t, strings.TrimPrefix(server.URL, "http://"))
	provider.managed = true

	return provider
}

func TestTerracottaHostPublishesRoom(t *testing.T) {
	fake := &fakeTerracotta{}
	fake.setState(map[string]any{
		"state": "host-ok",
		"room":  "YNZE-U61D-2206-HXRG",
		"profiles": []map[string]string{
			{"name": "猫娘", "kind": "HOST", "vendor": "NekoLauncher"},
			{"name": "朋友", "kind": "GUEST", "vendor": "HMCL"},
		},
	})

	provider := newTestTerracotta(t, fake)
	defer provider.Close()

	if err := provider.Host(HostOptions{Player: "猫娘"}); err != nil {
		t.Fatalf("Host 报错：%v", err)
	}

	status := provider.Status()
	if status.State != StateHosting {
		t.Fatalf("状态 = %s（%s），期望 hosting", status.State, status.Error)
	}
	if status.Room != "U/YNZE-U61D-2206-HXRG" || status.Address != status.Room {
		t.Fatalf("房间码 = %q / 地址 = %q", status.Room, status.Address)
	}
	if len(status.Players) != 2 || status.Players[0].Kind != KindHost {
		t.Fatalf("成员列表 = %+v", status.Players)
	}
	if !fake.hasCall("/state/scanning?") || !strings.Contains(strings.Join(fake.calls(), "|"), "player=") {
		t.Fatalf("建房请求未带上玩家名：%v", fake.calls())
	}
	if status.Since == 0 {
		t.Fatal("会话开始时间应当被记录")
	}
}

func TestTerracottaJoinTriesCodeVariants(t *testing.T) {
	fake := &fakeTerracotta{}
	// 第一次（带 U/ 前缀）被拒 → 应当自动换第二种形态重试
	fake.rejectGuests = 1
	fake.setState(map[string]any{
		"state": "guest-ok",
		"url":   "127.0.0.1:25570",
		"profiles": []map[string]string{
			{"name": "房主", "kind": "HOST", "vendor": "HMCL"},
			{"name": "我", "kind": "GUEST", "vendor": "NekoLauncher"},
		},
	})

	provider := newTestTerracotta(t, fake)
	defer provider.Close()

	if err := provider.Join("ynze-u61d-2206-hxrg", "我"); err != nil {
		t.Fatalf("Join 报错：%v", err)
	}

	status := provider.Status()
	if status.State != StateJoined {
		t.Fatalf("状态 = %s（%s），期望 joined", status.State, status.Error)
	}
	if status.LocalAddress != "127.0.0.1:25570" {
		t.Fatalf("本地直连地址 = %q", status.LocalAddress)
	}
	if status.JoinHost != "127.0.0.1" || status.JoinPort != 25570 {
		t.Fatalf("进服参数 = %s:%d", status.JoinHost, status.JoinPort)
	}

	guesting := 0
	for _, entry := range fake.calls() {
		if strings.HasPrefix(entry, "/state/guesting?") {
			guesting++
		}
	}
	if guesting < 2 {
		t.Fatalf("应当尝试两种房间码形态，实际：%v", fake.calls())
	}
}

func TestTerracottaJoinRejectsBadCode(t *testing.T) {
	fake := &fakeTerracotta{}
	fake.rejectGuests = 10
	provider := newTestTerracotta(t, fake)
	defer provider.Close()

	err := provider.Join("U/AAAA-BBBB-CCCC-DDDD", "我")
	if err == nil {
		t.Fatal("房间码一直被拒时应当报错")
	}
	if status := provider.Status(); status.State != StateError || status.Error == "" {
		t.Fatalf("应当进入错误状态：%+v", status)
	}

	if err := provider.Join("   ", "我"); err == nil {
		t.Fatal("空房间码应当直接报错")
	}
}

func TestTerracottaExceptionMapping(t *testing.T) {
	fake := &fakeTerracotta{}
	fake.setState(map[string]any{"state": "waiting"})
	provider := newTestTerracotta(t, fake)
	defer provider.Close()

	for kind := 0; kind <= 5; kind++ {
		value := kind
		provider.applyState(terracottaStatePayload{State: "exception", Type: &value})

		status := provider.Status()
		if status.State != StateError {
			t.Fatalf("错误码 %d 的状态 = %s", kind, status.State)
		}
		if status.Error == "" {
			t.Fatalf("错误码 %d 没有可读文案", kind)
		}
	}
}

func TestTerracottaLeaveResetsState(t *testing.T) {
	fake := &fakeTerracotta{}
	fake.setState(map[string]any{"state": "host-ok", "room": "YNZE-U61D-2206-HXRG"})
	provider := newTestTerracotta(t, fake)
	defer provider.Close()

	if err := provider.Host(HostOptions{Player: "猫娘"}); err != nil {
		t.Fatalf("Host 报错：%v", err)
	}
	if err := provider.Leave(); err != nil {
		t.Fatalf("Leave 报错：%v", err)
	}

	if !fake.hasCall("/state/ide?") {
		t.Fatalf("退出时应把状态机拉回空闲：%v", fake.calls())
	}
	if status := provider.Status(); status.State != StateIdle || status.Since != 0 {
		t.Fatalf("退出后状态 = %+v", status)
	}
}

func TestTerracottaShutdownStopsDaemon(t *testing.T) {
	fake := &fakeTerracotta{}
	provider := newTestTerracotta(t, fake)

	if err := provider.Shutdown(); err != nil {
		t.Fatalf("Shutdown 报错：%v", err)
	}

	fake.mu.Lock()
	panics := fake.panicCalls
	fake.mu.Unlock()

	if panics != 1 {
		t.Fatalf("应当调用一次 /panic，实际 %d 次", panics)
	}
	if status := provider.Status(); status.State != StateIdle {
		t.Fatalf("关闭后状态 = %s", status.State)
	}
	if runtime := provider.Runtime(); runtime.Running || runtime.Port != 0 {
		t.Fatalf("关闭后运行时状态 = %+v", runtime)
	}
}

func TestTerracottaUnreachableService(t *testing.T) {
	provider := newTerracottaProvider(newMemoryStore())
	// 指向一个必然不可用的端口：状态轮询应当报"失去联系"而不是静默卡死
	provider.port = 1
	provider.managed = true
	provider.since = nowUnix()

	provider.pollState()

	status := provider.Status()
	if status.State != StateError {
		t.Fatalf("服务不可达时应进入错误状态，得到 %s", status.State)
	}
	if !strings.Contains(status.Error, "失去联系") {
		t.Fatalf("错误文案 = %q", status.Error)
	}
}

func TestTerracottaInfoDependsOnBinary(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "terracotta.exe")
	if err := os.WriteFile(binary, []byte("stub"), 0o600); err != nil {
		t.Fatalf("写入替身可执行文件失败：%v", err)
	}

	store := newMemoryStore()
	store.Set(keyTerracottaPath, binary)

	provider := newTerracottaProvider(store)
	info := provider.Info()
	// Hint 必须是固定文案（前端要拿去查词典），路径只从 Runtime 里拿
	if !info.Ready || info.Hint == "" {
		t.Fatalf("指定了可执行文件时应当可用：%+v", info)
	}
	if strings.Contains(info.Hint, binary) {
		t.Fatalf("Hint 不应拼接运行期路径：%q", info.Hint)
	}
	if runtime := provider.Runtime(); runtime.Binary != binary {
		t.Fatalf("Runtime.Binary = %q，期望 %q", runtime.Binary, binary)
	}

	store.Set(keyTerracottaPath, filepath.Join(directory, "missing.exe"))
	info = provider.Info()
	if info.Ready || info.Hint == "" {
		t.Fatalf("文件缺失时应当不可用并给出提示：%+v", info)
	}
}

func TestTerracottaApplyStateKeepsSessionClock(t *testing.T) {
	fake := &fakeTerracotta{}
	provider := newTestTerracotta(t, fake)
	defer provider.Close()

	provider.begin("正在创建房间…", "提示")
	started := provider.Status().Since
	if started == 0 {
		t.Fatal("begin 应当记录会话开始时间")
	}

	fake.setState(map[string]any{"state": "host-starting"})
	provider.pollState()

	if got := provider.Status().Since; got != started {
		t.Fatalf("阶段推进不应重置会话时间：%d → %d", started, got)
	}

	// 陶瓦自己回到 waiting（用户在它自己的界面里退出）也要收敛成空闲
	fake.setState(map[string]any{"state": "waiting"})
	provider.pollState()

	if status := provider.Status(); status.State != StateIdle || status.Since != 0 {
		t.Fatalf("回到 waiting 时应收敛为空闲：%+v", status)
	}

	// 收敛后应停止轮询，避免空转
	time.Sleep(50 * time.Millisecond)
	provider.mu.Lock()
	watcher := provider.watcher
	provider.mu.Unlock()
	if watcher != nil {
		t.Fatal("进入空闲后应当停止状态轮询")
	}
}
