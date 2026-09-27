package online

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// startEchoServer 本机"服务端"替身：收到的字节原样回写。
func startEchoServer(t *testing.T) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听回显服务失败：%v", err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()

	return listener.Addr().String(), func() { _ = listener.Close() }
}

// fakeRelayDataPlane 中继数据面（:7000）替身：读一行 API Key → 可选地回一行
// 状态 → 扮演玩家发一段数据 → 校验本机服务端的回显。
type fakeRelayDataPlane struct {
	address  string
	greeting string
	payload  []byte
	results  chan error
	close    func()
}

func startFakeRelayDataPlane(t *testing.T, greeting string, payload []byte) *fakeRelayDataPlane {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听假中继失败：%v", err)
	}

	plane := &fakeRelayDataPlane{
		address:  listener.Addr().String(),
		greeting: greeting,
		payload:  payload,
		results:  make(chan error, 8),
		close:    func() { _ = listener.Close() },
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go plane.serve(conn)
		}
	}()

	return plane
}

func (p *fakeRelayDataPlane) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)
	key, err := reader.ReadString('\n')
	if err != nil {
		p.results <- fmt.Errorf("读取 API Key 失败：%v", err)

		return
	}
	if strings.TrimSpace(key) == "" {
		p.results <- fmt.Errorf("API Key 为空")

		return
	}

	if p.greeting != "" {
		if _, err := conn.Write([]byte(p.greeting)); err != nil {
			p.results <- err

			return
		}
	}

	if _, err := conn.Write(p.payload); err != nil {
		p.results <- err

		return
	}

	echo := make([]byte, len(p.payload))
	if _, err := io.ReadFull(io.MultiReader(reader, conn), echo); err != nil {
		p.results <- fmt.Errorf("读取回显失败：%v", err)

		return
	}
	if string(echo) != string(p.payload) {
		p.results <- fmt.Errorf("回显内容不符：%q != %q", echo, p.payload)

		return
	}

	p.results <- nil
}

// startFakeRelayControlPlane 中继控制面（:3000）替身。
func startFakeRelayControlPlane(t *testing.T, listenPort int) (*httptest.Server, *relayCalls) {
	t.Helper()

	calls := &relayCalls{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.record(request)

		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/apikey":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"ok":true}`))
		case request.Method == http.MethodGet && request.URL.Path == "/tunnels":
			_ = json.NewEncoder(writer).Encode(map[string]any{"tunnels": []any{}})
		case request.Method == http.MethodPost && request.URL.Path == "/tunnels":
			_ = json.NewEncoder(writer).Encode(map[string]any{"listenPort": listenPort})
		case request.Method == http.MethodDelete && request.URL.Path == "/tunnels":
			writer.WriteHeader(http.StatusOK)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))

	return server, calls
}

// relayCalls 记录控制面收到的请求（含 Authorization 头）与并发访问保护。
type relayCalls struct {
	entries []string
}

func (c *relayCalls) record(request *http.Request) {
	c.entries = append(c.entries, request.Method+" "+request.URL.RequestURI()+" auth="+request.Header.Get("Authorization"))
}

func (c *relayCalls) count(prefix string) int {
	total := 0
	for _, entry := range c.entries {
		if strings.HasPrefix(entry, prefix) {
			total++
		}
	}

	return total
}

func TestRelayAddress(t *testing.T) {
	host, api := relayAddress("122.51.108.96")
	if host != "122.51.108.96" || api != "122.51.108.96:3000" {
		t.Fatalf("relayAddress(裸主机) = %q / %q", host, api)
	}

	host, api = relayAddress("http://relay.example.com:8080/")
	if host != "relay.example.com" || api != "relay.example.com:8080" {
		t.Fatalf("relayAddress(自定义端口) = %q / %q", host, api)
	}

	host, api = relayAddress("   ")
	if host != "" || api != "" {
		t.Fatalf("空地址应返回空，得到 %q / %q", host, api)
	}
}

func TestParseTunnelPort(t *testing.T) {
	if got := parseTunnelPort([]byte(`{"listenPort":41234}`)); got != 41234 {
		t.Fatalf("单隧道响应解析 = %d", got)
	}
	if got := parseTunnelPort([]byte(`{"tunnels":[{"listenPort":41000},{"listenPort":42000}]}`)); got != 41000 {
		t.Fatalf("隧道列表解析 = %d", got)
	}
	if got := parseTunnelPort([]byte(`{}`)); got != 0 {
		t.Fatalf("无端口时应为 0，得到 %d", got)
	}
	if got := parseTunnelPort(nil); got != 0 {
		t.Fatalf("空响应应为 0，得到 %d", got)
	}
}

// TestRedstoneHostForwardsTraffic 端到端：申请隧道 → 中继打状态行 → 玩家数据
// 经本机服务端回显，验证 7000 数据面的转发链路。
func TestRedstoneHostForwardsTraffic(t *testing.T) {
	echoAddress, stopEcho := startEchoServer(t)
	defer stopEcho()

	controlPlane, calls := startFakeRelayControlPlane(t, 41234)
	defer controlPlane.Close()

	plane := startFakeRelayDataPlane(t, "OK TUNNEL 41234\n", []byte("hello-from-player\n"))
	defer plane.close()

	store := newMemoryStore()
	store.Set(keyRedstoneRelay, strings.TrimPrefix(controlPlane.URL, "http://"))
	store.Set(keyRedstoneKey, "testkey0123456789abcd")

	provider := newRedstoneProvider(store)
	provider.tunnelPort = portOf(t, plane.address)

	if err := provider.Host(HostOptions{Target: echoAddress, MaxPlayers: 1}); err != nil {
		t.Fatalf("Host 报错：%v", err)
	}
	defer func() { _ = provider.Leave() }()

	status := provider.Status()
	if status.State != StateHosting {
		t.Fatalf("状态 = %s（%s），期望 hosting", status.State, status.Error)
	}
	if status.Address != "127.0.0.1:41234" {
		t.Fatalf("公网地址 = %q，期望 127.0.0.1:41234", status.Address)
	}
	if status.JoinHost != "127.0.0.1" || status.JoinPort == 0 {
		t.Fatalf("一键进服参数 = %s:%d", status.JoinHost, status.JoinPort)
	}
	// Authorization 头是原样发送的密钥（与模组一致，不加 Bearer）
	if calls.count("POST /tunnels?maxPlayers=1 auth=testkey0123456789abcd") != 1 {
		t.Fatalf("申请隧道的请求不符合预期：%v", calls.entries)
	}

	select {
	case err := <-plane.results:
		if err != nil {
			t.Fatalf("数据面转发失败：%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("等待数据面转发超时")
	}

	// 玩家接入后连接数应被统计到（前端据此显示"有人进来了"）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if provider.Status().Connections > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("活跃连接数没有被统计：%+v", provider.status)
}

// TestRedstoneHostWithoutGreeting 有些节点不打状态行：首批字节必须原样转给
// 本机服务端，不能被当成握手文本吞掉。
func TestRedstoneHostWithoutGreeting(t *testing.T) {
	echoAddress, stopEcho := startEchoServer(t)
	defer stopEcho()

	controlPlane, _ := startFakeRelayControlPlane(t, 41500)
	defer controlPlane.Close()

	payload := []byte{0x10, 0x00, 0x2f, 0x0a, 0x41}
	plane := startFakeRelayDataPlane(t, "", payload)
	defer plane.close()

	store := newMemoryStore()
	store.Set(keyRedstoneRelay, strings.TrimPrefix(controlPlane.URL, "http://"))

	provider := newRedstoneProvider(store)
	provider.tunnelPort = portOf(t, plane.address)

	if err := provider.Host(HostOptions{Target: echoAddress, MaxPlayers: 1}); err != nil {
		t.Fatalf("Host 报错：%v", err)
	}
	defer func() { _ = provider.Leave() }()

	select {
	case err := <-plane.results:
		if err != nil {
			t.Fatalf("无状态行时转发失败：%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("等待无状态行转发超时")
	}
}

// TestRedstoneLeaveReleasesTunnel 退出会话要断隧道并通知中继释放端口。
func TestRedstoneLeaveReleasesTunnel(t *testing.T) {
	echoAddress, stopEcho := startEchoServer(t)
	defer stopEcho()

	controlPlane, calls := startFakeRelayControlPlane(t, 41600)
	defer controlPlane.Close()

	store := newMemoryStore()
	store.Set(keyRedstoneRelay, strings.TrimPrefix(controlPlane.URL, "http://"))

	provider := newRedstoneProvider(store)
	// 指向一个没人监听的端口：转发协程只会在重试间隔里等待，不影响本用例
	provider.tunnelPort = 1

	if err := provider.Host(HostOptions{Target: echoAddress, MaxPlayers: 2}); err != nil {
		t.Fatalf("Host 报错：%v", err)
	}
	if err := provider.Leave(); err != nil {
		t.Fatalf("Leave 报错：%v", err)
	}

	if calls.count("DELETE /tunnels") != 1 {
		t.Fatalf("退出时应释放隧道，实际请求：%v", calls.entries)
	}
	if status := provider.Status(); status.State != StateIdle {
		t.Fatalf("退出后状态 = %s，期望 idle", status.State)
	}
}

// TestRedstoneJoinStoresAddress 房客侧只是记住地址（红石联机不需要本地转发）。
func TestRedstoneJoinStoresAddress(t *testing.T) {
	provider := newRedstoneProvider(newMemoryStore())

	if err := provider.Join("122.51.108.96:12345", "猫娘"); err != nil {
		t.Fatalf("Join 报错：%v", err)
	}

	status := provider.Status()
	if status.State != StateJoined {
		t.Fatalf("状态 = %s，期望 joined", status.State)
	}
	if status.JoinHost != "122.51.108.96" || status.JoinPort != 12345 {
		t.Fatalf("进服参数 = %s:%d", status.JoinHost, status.JoinPort)
	}
	if status.Room != "122.51.108.96:12345" {
		t.Fatalf("房间地址 = %q", status.Room)
	}

	// 缺端口时补 Minecraft 默认端口
	if err := provider.Join("relay.example.com", ""); err != nil {
		t.Fatalf("Join 报错：%v", err)
	}
	if status := provider.Status(); status.JoinPort != 25565 || status.Room != "relay.example.com" {
		t.Fatalf("缺端口时应补 25565：%+v", status)
	}

	if err := provider.Join("这不是地址", ""); err == nil {
		t.Fatal("非法地址应当报错")
	}
}

// portOf 取 "host:port" 里的端口。
func portOf(t *testing.T, address string) int {
	t.Helper()

	_, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("解析地址 %q 失败：%v", address, err)
	}

	value, err := net.LookupPort("tcp", port)
	if err != nil {
		t.Fatalf("解析端口 %q 失败：%v", port, err)
	}

	return value
}
