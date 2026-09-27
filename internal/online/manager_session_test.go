package online

import (
	"strings"
	"sync"
	"testing"
)

// TestManagerKeepsBothProviderSessionsAlive 联机会话多开（P2-9 的核心行为）：
// 陶瓦的房间与红石的隧道是两条独立链路，开一家不能把另一家打断；
// Leave(provider) 只停那一家。
func TestManagerKeepsBothProviderSessionsAlive(t *testing.T) {
	fake := &fakeTerracotta{}
	fake.setState(map[string]any{
		"state": "host-ok",
		"room":  "YNZE-U61D-2206-HXRG",
		"profiles": []map[string]string{
			{"name": "猫娘", "kind": "HOST", "vendor": "NekoLauncher"},
		},
	})

	terracotta := newTestTerracotta(t, fake)
	defer terracotta.Close()

	controlPlane, calls := startFakeRelayControlPlane(t, 41888)
	defer controlPlane.Close()

	echoAddress, stopEcho := startEchoServer(t)
	defer stopEcho()

	store := newMemoryStore()
	store.Set(keyRedstoneRelay, strings.TrimPrefix(controlPlane.URL, "http://"))
	store.Set(keyRedstoneKey, "testkey0123456789abcd")

	redstone := newRedstoneProvider(store)
	// 数据面指向没人监听的端口：转发协程只在重试间隔里等待，不影响本用例
	redstone.tunnelPort = 1

	manager := NewManager(store)
	manager.providers[ProviderTerracotta] = terracotta
	manager.providers[ProviderRedstone] = redstone

	pushes := &statusRecorder{}
	manager.SetOnChanged(pushes.record)

	// 1) 先开陶瓦的房间
	if err := manager.Host(HostOptions{Provider: string(ProviderTerracotta), Player: "猫娘"}); err != nil {
		t.Fatalf("陶瓦建房失败：%v", err)
	}
	if state := manager.Status(string(ProviderTerracotta)).State; state != StateHosting {
		t.Fatalf("陶瓦状态 = %s，期望 hosting", state)
	}

	// 2) 再开红石隧道：陶瓦的房间必须还开着
	if err := manager.Host(HostOptions{
		Provider:   string(ProviderRedstone),
		Target:     echoAddress,
		MaxPlayers: 1,
	}); err != nil {
		t.Fatalf("红石建房失败：%v", err)
	}
	if state := manager.Status(string(ProviderTerracotta)).State; state != StateHosting {
		t.Fatalf("开红石隧道不该打断陶瓦的房间，实际状态 = %s", state)
	}
	if state := manager.Status(string(ProviderRedstone)).State; state != StateHosting {
		t.Fatalf("红石状态 = %s，期望 hosting", state)
	}

	// 两家各自的地址都要在，前端才能同时展示两个房间
	if room := manager.Status(string(ProviderTerracotta)).Room; room == "" {
		t.Fatal("陶瓦房间码不应为空")
	}
	if address := manager.Status(string(ProviderRedstone)).Address; address != "127.0.0.1:41888" {
		t.Fatalf("红石公网地址 = %q", address)
	}

	// Statuses() 顺序固定：陶瓦在前、红石在后，且都带供应商标识
	statuses := manager.Statuses()
	if len(statuses) != 2 {
		t.Fatalf("快照数量 = %d，期望 2", len(statuses))
	}
	if statuses[0].Provider != string(ProviderTerracotta) ||
		statuses[1].Provider != string(ProviderRedstone) {
		t.Fatalf("快照顺序不符：%s, %s", statuses[0].Provider, statuses[1].Provider)
	}
	for _, status := range statuses {
		if status.State != StateHosting {
			t.Fatalf("%s 应为 hosting，实际 %s", status.Provider, status.State)
		}
		if status.Players == nil {
			t.Fatalf("%s 的成员列表不应为 nil", status.Provider)
		}
	}

	// 两家的状态变化都要推给前端（观察者按供应商分别接线）
	if !pushes.sawProviderAtState(string(ProviderTerracotta), StateHosting) ||
		!pushes.sawProviderAtState(string(ProviderRedstone), StateHosting) {
		t.Fatalf("状态推送缺少某一家：%+v", pushes.snapshot())
	}

	// 3) 只退出陶瓦：红石继续跑
	if err := manager.Leave(string(ProviderTerracotta)); err != nil {
		t.Fatalf("退出陶瓦失败：%v", err)
	}
	if state := manager.Status(string(ProviderTerracotta)).State; state != StateIdle {
		t.Fatalf("陶瓦状态 = %s，期望 idle", state)
	}
	if state := manager.Status(string(ProviderRedstone)).State; state != StateHosting {
		t.Fatalf("退出陶瓦不该影响红石，实际状态 = %s", state)
	}

	// 4) 退出全部（provider 为空）
	if err := manager.Leave(""); err != nil {
		t.Fatalf("退出全部失败：%v", err)
	}
	for _, status := range manager.Statuses() {
		if status.State != StateIdle {
			t.Fatalf("%s 应回到 idle，实际 %s", status.Provider, status.State)
		}
	}

	// 红石退出时应当通知中继释放端口（不然下次建房撞 429）
	if calls.count("DELETE /tunnels") != 1 {
		t.Fatalf("退出红石应释放隧道，实际请求：%v", calls.entries)
	}
}

// TestManagerHostSameProviderReplacesSession 同一家再次建房：先退出它自己的旧会话，
// 但不动另一家。
func TestManagerHostSameProviderReplacesSession(t *testing.T) {
	hostState := map[string]any{"state": "host-ok", "room": "YNZE-U61D-2206-HXRG"}

	fake := &fakeTerracotta{rescanState: hostState}
	fake.setState(hostState)

	terracotta := newTestTerracotta(t, fake)
	defer terracotta.Close()

	store := newMemoryStore()
	manager := NewManager(store)
	manager.providers[ProviderTerracotta] = terracotta

	if err := manager.Host(HostOptions{Provider: string(ProviderTerracotta), Player: "猫娘"}); err != nil {
		t.Fatalf("第一次建房失败：%v", err)
	}
	if err := manager.Host(HostOptions{Provider: string(ProviderTerracotta), Player: "猫娘"}); err != nil {
		t.Fatalf("第二次建房失败：%v", err)
	}

	// 旧会话被收起时会给陶瓦发一次"回到空闲"，之后重新建房
	if !fake.hasCall("/state/ide") {
		t.Fatalf("再次建房应先退出旧会话，实际请求：%v", fake.calls())
	}
	if state := manager.Status(string(ProviderTerracotta)).State; state != StateHosting {
		t.Fatalf("最终状态 = %s，期望 hosting", state)
	}
}

// TestManagerUnknownProviderStatus 未知 id 归一到陶瓦联机，不 panic（前端首帧可能带脏值）。
func TestManagerUnknownProviderStatus(t *testing.T) {
	manager := NewManager(newMemoryStore())

	status := manager.Status("no-such-provider")
	if status.Provider != string(ProviderTerracotta) {
		t.Fatalf("未知供应商应归一到陶瓦联机，得到 %q", status.Provider)
	}
	if status.State != StateIdle {
		t.Fatalf("未知供应商应回 idle 快照，得到 %q", status.State)
	}
	if err := manager.Leave("no-such-provider"); err != nil {
		t.Fatalf("退出未知供应商不该报错：%v", err)
	}
}

// statusRecorder 收集推送给前端的状态快照（多协程写入，需加锁）。
type statusRecorder struct {
	mu      sync.Mutex
	records []Status
}

func (r *statusRecorder) record(status Status) {
	r.mu.Lock()
	r.records = append(r.records, status)
	r.mu.Unlock()
}

func (r *statusRecorder) snapshot() []Status {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]Status{}, r.records...)
}

func (r *statusRecorder) sawProviderAtState(provider, state string) bool {
	for _, record := range r.snapshot() {
		if record.Provider == provider && record.State == state {
			return true
		}
	}

	return false
}
