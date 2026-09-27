package online

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRedstonePlayersFromManagedServer 转发目标是启动器托管的服务器时，
// 成员列表要用服务器 `list` 的真实在线玩家（中继自己不提供名单）。
func TestRedstonePlayersFromManagedServer(t *testing.T) {
	echoAddress, stopEcho := startEchoServer(t)
	defer stopEcho()

	controlPlane, _ := startFakeRelayControlPlane(t, 41900)
	defer controlPlane.Close()

	store := newMemoryStore()
	store.Set(keyRedstoneRelay, strings.TrimPrefix(controlPlane.URL, "http://"))
	store.Set(keyRedstoneKey, "testkey0123456789abcd")

	provider := newRedstoneProvider(store)
	provider.tunnelPort = 1

	// 名单来源：先空 → 一个玩家 → 两个玩家（模拟玩家进服）
	var mu sync.Mutex
	online := []string{}

	provider.playersFor = func(serverID string) []Player {
		if serverID != "srv-1" {
			t.Errorf("轮询的服务器 id = %q，期望 srv-1", serverID)
		}
		mu.Lock()
		names := append([]string{}, online...)
		mu.Unlock()

		players := make([]Player, 0, len(names))
		for _, name := range names {
			players = append(players, Player{Name: name, Kind: KindLocal, Vendor: "NekoLauncher"})
		}

		return players
	}

	if err := provider.Host(HostOptions{
		Target:   echoAddress,
		ServerID: "srv-1",
		// MaxPlayers=1 足够：本用例不校验转发
		MaxPlayers: 1,
	}); err != nil {
		t.Fatalf("Host 报错：%v", err)
	}
	defer func() { _ = provider.Leave() }()

	if got := provider.Status().Players; len(got) != 0 {
		t.Fatalf("没人进服时成员列表应为空，得到 %+v", got)
	}

	// 有人进服 → 轮询（Host 会立刻刷一次，之后每 5 秒一次）
	mu.Lock()
	online = []string{"猫娘"}
	mu.Unlock()
	provider.refreshPlayers("srv-1")

	players := waitPlayers(t, provider, 1)
	if players[0].Name != "猫娘" || players[0].Kind != KindLocal {
		t.Fatalf("成员 = %+v", players[0])
	}

	// 又来一个 → 名单更新
	mu.Lock()
	online = []string{"猫娘", "朋友"}
	mu.Unlock()
	provider.refreshPlayers("srv-1")

	players = waitPlayers(t, provider, 2)
	if players[0].Name != "猫娘" || players[1].Name != "朋友" {
		t.Fatalf("成员名单 = %+v", players)
	}

	// 名单没变化时不该反复推送（前端每 5 秒闪一次会很吵）
	pushes := &statusRecorder{}
	provider.observer.setObserver(pushes.record)
	provider.refreshPlayers("srv-1")
	if len(pushes.snapshot()) != 0 {
		t.Fatalf("名单未变化时不该推送，实际推送 %d 次", len(pushes.snapshot()))
	}
}

// TestRedstonePlayersSkipManualTarget 手动地址模式没有名单来源：即使注入了
// 名单回调也不该去查（ServerID 为空），成员列表保持空。
func TestRedstonePlayersSkipManualTarget(t *testing.T) {
	echoAddress, stopEcho := startEchoServer(t)
	defer stopEcho()

	controlPlane, _ := startFakeRelayControlPlane(t, 41910)
	defer controlPlane.Close()

	store := newMemoryStore()
	store.Set(keyRedstoneRelay, strings.TrimPrefix(controlPlane.URL, "http://"))

	provider := newRedstoneProvider(store)
	provider.tunnelPort = 1

	called := 0
	provider.playersFor = func(string) []Player {
		called++

		return []Player{{Name: "不该出现", Kind: KindLocal}}
	}

	if err := provider.Host(HostOptions{Target: echoAddress, MaxPlayers: 1}); err != nil {
		t.Fatalf("Host 报错：%v", err)
	}
	defer func() { _ = provider.Leave() }()

	if called != 0 {
		t.Fatalf("手动地址模式不该查询成员名单，实际调用 %d 次", called)
	}
	if players := provider.Status().Players; len(players) != 0 {
		t.Fatalf("手动地址模式成员列表应为空，得到 %+v", players)
	}
}

// TestManagerInjectsPlayersHook 管理器把注入的名单来源接到红石供应商上，
// 未注入时保持 nil（不 panic）。
func TestManagerInjectsPlayersHook(t *testing.T) {
	manager := NewManager(newMemoryStore())

	redstone, ok := manager.providers[ProviderRedstone].(*redstoneProvider)
	if !ok {
		t.Fatal("红石供应商类型不符")
	}
	if redstone.playersFor == nil {
		t.Fatal("管理器应给红石供应商接上名单回调")
	}
	if players := redstone.playersFor("srv-1"); players != nil {
		t.Fatalf("未注入来源时应返回 nil，得到 %+v", players)
	}

	manager.PlayersFor = func(serverID string) []Player {
		return []Player{{Name: serverID, Kind: KindLocal}}
	}
	players := redstone.playersFor("srv-9")
	if len(players) != 1 || players[0].Name != "srv-9" {
		t.Fatalf("注入后应转发给实现，得到 %+v", players)
	}
}

// waitPlayers 等成员列表涨到期望数量（轮询是异步的）。
func waitPlayers(t *testing.T, provider *redstoneProvider, want int) []Player {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if players := provider.Status().Players; len(players) == want {
			return players
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("等待成员列表变成 %d 人超时，当前 %+v", want, provider.Status().Players)

	return nil
}
