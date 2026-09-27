package online

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestParseRelayList 校验自定义节点的解析、清洗与去重。
func TestParseRelayList(t *testing.T) {
	raw := strings.Join([]string{
		"",
		"   ",
		"我的节点=1.2.3.4:3999",
		"http://5.6.7.8",
		"7.8.9.10/",
		"122.51.108.96",         // 与内置节点同址（裸主机名形态）
		"镜像=122.51.108.96:3000", // 与内置节点同址（带默认端口形态）
		"=1.1.1.1",              // 没有名称，地址照样保留
	}, "\n")

	options := parseRelayList(raw)
	if len(options) != 5 {
		t.Fatalf("节点数量 = %d，期望 5：%+v", len(options), options)
	}
	if options[0].Address != DefaultRelayAddress || !options[0].Builtin {
		t.Fatalf("内置节点应排在最前：%+v", options[0])
	}

	want := []struct {
		name    string
		address string
	}{
		{"我的节点", "1.2.3.4:3999"},
		{"5.6.7.8", "5.6.7.8"},
		{"7.8.9.10", "7.8.9.10"},
		{"1.1.1.1", "1.1.1.1"},
	}
	for index, item := range want {
		got := options[index+1]
		if got.Name != item.name || got.Address != item.address {
			t.Errorf("第 %d 个自定义节点 = %q/%q，期望 %q/%q",
				index, got.Name, got.Address, item.name, item.address)
		}
		if got.Builtin {
			t.Errorf("自定义节点不该标记为内置：%+v", got)
		}
	}
}

// TestParseRelayListEmpty 没有自定义时只返回内置节点。
func TestParseRelayListEmpty(t *testing.T) {
	options := parseRelayList("")
	if len(options) != 1 {
		t.Fatalf("空列表应只有内置节点，得到 %+v", options)
	}
	if _, api := relayAddress(options[0].Address); api == "" {
		t.Fatalf("内置节点地址不可解析：%+v", options[0])
	}
}

// TestRelayAddressNormalize 中继地址规范化（默认端口 / scheme / 尾斜杠）。
func TestRelayAddressNormalize(t *testing.T) {
	cases := []struct {
		raw      string
		wantHost string
		wantAPI  string
	}{
		{"122.51.108.96", "122.51.108.96", "122.51.108.96:3000"},
		{"http://122.51.108.96/", "122.51.108.96", "122.51.108.96:3000"},
		{"example.com:3999", "example.com", "example.com:3999"},
		{"", "", ""},
	}
	for _, item := range cases {
		host, api := relayAddress(item.raw)
		if host != item.wantHost || api != item.wantAPI {
			t.Errorf("relayAddress(%q) = %q/%q，期望 %q/%q",
				item.raw, host, api, item.wantHost, item.wantAPI)
		}
	}
}

// TestProbeRedstoneRelayReachable 能拿到任何 HTTP 响应（含 401）就算节点可达。
func TestProbeRedstoneRelayReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != relayProbePath {
			t.Errorf("预检请求路径 = %q，期望 %q", request.URL.Path, relayProbePath)
		}
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	probe := ProbeRedstoneRelay(context.Background(), "测试节点", server.URL)
	if !probe.Reachable {
		t.Fatalf("节点应判定为可达：%+v", probe)
	}
	if probe.Status != http.StatusUnauthorized {
		t.Errorf("状态码 = %d，期望 401", probe.Status)
	}
	if probe.Error != "" {
		t.Errorf("可达时不该有错误：%q", probe.Error)
	}
}

// TestProbeRedstoneRelayUnreachable 连接被拒绝时应给出可读原因。
func TestProbeRedstoneRelayUnreachable(t *testing.T) {
	address := closedPortAddress(t)

	probe := ProbeRedstoneRelay(context.Background(), "死节点", address)
	if probe.Reachable {
		t.Fatalf("关闭的端口不该判定为可达：%+v", probe)
	}
	if probe.Error == "" {
		t.Fatal("不可达时应带失败原因")
	}
	if probe.LatencyMs != 0 || probe.Status != 0 {
		t.Errorf("不可达时延迟/状态码应为 0：%+v", probe)
	}
}

// TestProbeRedstoneRelayTimeout 对端收下连接却不回包时，走超时分支并翻译成中文。
func TestProbeRedstoneRelayTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败：%v", err)
	}
	defer func() { _ = listener.Close() }()

	accepted := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			close(accepted)

			return
		}
		close(accepted)
		// 收下连接但永不响应，把请求吊到超时
		time.Sleep(2 * time.Second)
		_ = conn.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	probe := ProbeRedstoneRelay(ctx, "假死节点", listener.Addr().String())
	<-accepted
	if probe.Reachable {
		t.Fatalf("没有响应的节点不该判定为可达：%+v", probe)
	}
	if !strings.Contains(probe.Error, "超时") {
		t.Errorf("超时错误应翻译成中文提示，得到 %q", probe.Error)
	}
}

// TestProbeRedstoneRelaysOrdering 并发预检的排序：可达的按延迟升序，不可达的垫底。
func TestProbeRedstoneRelaysOrdering(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		writer.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()

	fast := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer fast.Close()

	options := []RelayOption{
		{Name: "死节点", Address: closedPortAddress(t)},
		{Name: "慢节点", Address: slow.URL},
		{Name: "快节点", Address: fast.URL},
	}

	probes := ProbeRedstoneRelays(context.Background(), options)
	if len(probes) != 3 {
		t.Fatalf("预检结果数量 = %d，期望 3", len(probes))
	}
	if probes[0].Name != "快节点" || probes[1].Name != "慢节点" || probes[2].Name != "死节点" {
		t.Fatalf("排序不符合预期：%q → %q → %q", probes[0].Name, probes[1].Name, probes[2].Name)
	}
	if probes[1].LatencyMs < 150 {
		t.Errorf("慢节点延迟应不少于 150 ms，得到 %d", probes[1].LatencyMs)
	}
	if !probes[0].Reachable || probes[2].Reachable {
		t.Errorf("可达性判定异常：%+v", probes)
	}
}

// TestPreflightRelayKeepsReachableNode 配置节点可达时原样使用、不加说明。
func TestPreflightRelayKeepsReachableNode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, api := relayAddress(server.URL)
	gotHost, gotAPI, note := preflightRelay(context.Background(), nil, host, api, server.URL)
	if gotHost != host || gotAPI != api || note != "" {
		t.Fatalf("可达节点不该被替换：%q/%q/%q", gotHost, gotAPI, note)
	}
}

// TestPreflightRelayFallsBackToReachable 配置节点不可达时自动切到可达候选。
func TestPreflightRelayFallsBackToReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	dead := closedPortAddress(t)
	deadHost, deadAPI := relayAddress(dead)

	options := []RelayOption{
		{Name: "内置", Address: DefaultRelayAddress},
		{Name: "备用", Address: server.URL},
	}

	// 用较短的预算跑：死节点在 Windows 上会一直吊到超时，默认 5 s 太慢
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	gotHost, gotAPI, note := preflightRelay(ctx, options, deadHost, deadAPI, dead)
	wantHost, wantAPI := relayAddress(server.URL)
	if gotHost != wantHost || gotAPI != wantAPI {
		t.Fatalf("应回落到可达节点 %q/%q，得到 %q/%q", wantHost, wantAPI, gotHost, gotAPI)
	}
	if !strings.Contains(note, "已自动改用") || !strings.Contains(note, wantAPI) {
		t.Errorf("说明文案缺少切换信息：%q", note)
	}
}

// TestPreflightRelayAllUnreachable 全部不可达时保留原配置，并给出带原因的说明。
func TestPreflightRelayAllUnreachable(t *testing.T) {
	first := closedPortAddress(t)
	second := closedPortAddress(t)
	firstHost, firstAPI := relayAddress(first)

	options := []RelayOption{{Name: "备用", Address: second}}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	gotHost, gotAPI, note := preflightRelay(ctx, options, firstHost, firstAPI, first)

	if gotHost != firstHost || gotAPI != firstAPI {
		t.Fatalf("全都不可达时应保持原配置，得到 %q/%q", gotHost, gotAPI)
	}
	if !strings.Contains(note, "都不可达") {
		t.Errorf("说明应提示全部不可达：%q", note)
	}
}

// TestFastestRedstoneRelaySelectsLowestLatency 内置+自定义候选里挑延迟最低的可达节点。
func TestFastestRedstoneRelaySelectsLowestLatency(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(250 * time.Millisecond)
		writer.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()

	fast := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer fast.Close()

	options := []RelayOption{
		// 内置节点在这里被替换成慢节点，避免测试真的去连官方节点
		{Name: "慢", Address: slow.URL, Builtin: true},
		{Name: "快", Address: fast.URL},
		{Name: "死", Address: closedPortAddress(t)},
	}

	probes := ProbeRedstoneRelays(context.Background(), options)
	fastest := probes[0]
	if !fastest.Reachable {
		t.Fatalf("应至少有一个可达节点：%+v", probes)
	}
	if fastest.Name != "快" {
		t.Fatalf("最快节点 = %q，期望「快」：%+v", fastest.Name, probes)
	}

	// FastestRedstoneRelay 内部读真实配置，这里只校验它的选优逻辑
	// （preflightRelay 的回落分支已覆盖同一段代码路径）。
	if relayAddressInput(fastest.Address) == ":" {
		t.Fatal("地址还原异常")
	}
}

// TestLiveRedstoneRelayProbe 真连官方/自定义中继，验证预检在真实网络下的判定。
// 需要 NEKO_LIVE_REDSTONE_RELAY=1 才跑（CI 与本地默认跳过，避免依赖外网）。
//
// 这个测试不"要求"节点可达：官方节点在国内本来就时通时不通，能打印出
// 判定与延迟就说明预检链路是通的（不可达时也会给出可读原因）。
func TestLiveRedstoneRelayProbe(t *testing.T) {
	if os.Getenv("NEKO_LIVE_REDSTONE_RELAY") != "1" {
		t.Skip("设置 NEKO_LIVE_REDSTONE_RELAY=1 才跑真实网络预检")
	}

	options := RedstoneRelayOptions()
	probes := ProbeRedstoneRelays(context.Background(), options)
	if len(probes) == 0 {
		t.Fatal("没有任何可预检的节点")
	}

	reachable := 0
	for _, probe := range probes {
		t.Logf("节点 %s（%s）：可达=%v 延迟=%d ms 状态=%d 错误=%q",
			probe.Name, probe.Address, probe.Reachable, probe.LatencyMs, probe.Status, probe.Error)
		if probe.Reachable {
			reachable++
		}
	}

	if reachable > 0 {
		fastest, ok := FastestRedstoneRelay(context.Background())
		if !ok {
			t.Fatal("有可达节点时应该能选出最快节点")
		}
		t.Logf("最快节点：%s（%s）%d ms", fastest.Name, fastest.Address, fastest.LatencyMs)
		if !strings.HasPrefix(fastest.Address, "http") && fastest.Address == "" {
			t.Fatalf("最快节点地址为空：%+v", fastest)
		}
	} else {
		t.Log("当前网络下所有节点都不可达（预检已给出原因），跳过选优校验")
	}
}

// TestRedstoneHostReportsUnreachableRelay 端到端：中继不可达时，建房失败信息里
// 必须带上预检结论（不是只丢一句"注册密钥失败"）。
func TestRedstoneHostReportsUnreachableRelay(t *testing.T) {
	restore := relayProbeBudget

	relayProbeBudget = 300 * time.Millisecond
	t.Cleanup(func() { relayProbeBudget = restore })

	echoAddress, stopEcho := startEchoServer(t)
	defer stopEcho()

	store := newMemoryStore()
	store.Set(keyRedstoneRelay, closedPortAddress(t))
	store.Set(keyRedstoneKey, "testkey0123456789abcd")

	provider := newRedstoneProvider(store)

	err := provider.Host(HostOptions{Target: echoAddress, MaxPlayers: 1})
	if err == nil {
		_ = provider.Leave()
		t.Fatal("中继不可达时建房应该失败")
	}
	if !strings.Contains(err.Error(), "都不可达") {
		t.Fatalf("失败信息应带上预检结论，得到：%v", err)
	}

	status := provider.Status()
	if status.State != StateError {
		t.Fatalf("状态 = %s，期望 error", status.State)
	}
	if !strings.Contains(status.Error, "都不可达") {
		t.Fatalf("状态里的错误也应带预检结论，得到：%q", status.Error)
	}
}

// closedPortAddress 返回一个"刚被释放、必定连不上"的 127.0.0.1 地址。
func closedPortAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败：%v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	return address
}
