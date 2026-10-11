package network

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// stubSRVRecords 替换 SRV 查询替身并记录最近一次查询的域名，测试结束自动还原。
func stubSRVRecords(t *testing.T, records []*net.SRV, err error) (queried *string) {
	t.Helper()
	original := lookupSRVRecords
	queried = new(string)
	lookupSRVRecords = func(_ context.Context, name string) ([]*net.SRV, error) {
		*queried = name
		return records, err
	}
	t.Cleanup(func() { lookupSRVRecords = original })
	return queried
}

func TestResolveServerTarget(t *testing.T) {
	t.Run("默认端口域名命中 SRV 记录", func(t *testing.T) {
		queried := stubSRVRecords(t, []*net.SRV{
			{Target: "real.example.com.", Port: 25577, Priority: 1, Weight: 5},
		}, nil)

		host, port := resolveServerTarget(context.Background(), "mc.example.com", 25565)
		if host != "real.example.com" || port != 25577 {
			t.Fatalf("got %s:%d, want real.example.com:25577", host, port)
		}
		if *queried != "mc.example.com" {
			t.Fatalf("查询域名 got %q, want %q", *queried, "mc.example.com")
		}
	})

	t.Run("多条记录取第一条（排序由解析器负责）", func(t *testing.T) {
		stubSRVRecords(t, []*net.SRV{
			{Target: "a.example.com.", Port: 25570},
			{Target: "b.example.com.", Port: 25571},
		}, nil)

		host, port := resolveServerTarget(context.Background(), "mc.example.com", 25565)
		if host != "a.example.com" || port != 25570 {
			t.Fatalf("got %s:%d, want a.example.com:25570", host, port)
		}
	})

	t.Run("无记录回退直连", func(t *testing.T) {
		stubSRVRecords(t, nil, nil)

		host, port := resolveServerTarget(context.Background(), "mc.example.com", 25565)
		if host != "mc.example.com" || port != 25565 {
			t.Fatalf("got %s:%d, want mc.example.com:25565", host, port)
		}
	})

	t.Run("查询失败回退直连", func(t *testing.T) {
		stubSRVRecords(t, nil, errors.New("dns 不可达"))

		host, port := resolveServerTarget(context.Background(), "mc.example.com", 25565)
		if host != "mc.example.com" || port != 25565 {
			t.Fatalf("got %s:%d, want mc.example.com:25565", host, port)
		}
	})

	t.Run("Target 为根域表示服务不可用，回退直连", func(t *testing.T) {
		stubSRVRecords(t, []*net.SRV{{Target: "."}}, nil)

		host, port := resolveServerTarget(context.Background(), "mc.example.com", 25565)
		if host != "mc.example.com" || port != 25565 {
			t.Fatalf("got %s:%d, want mc.example.com:25565", host, port)
		}
	})

	t.Run("SRV 端口为 0 回退直连", func(t *testing.T) {
		stubSRVRecords(t, []*net.SRV{{Target: "real.example.com.", Port: 0}}, nil)

		host, port := resolveServerTarget(context.Background(), "mc.example.com", 25565)
		if host != "mc.example.com" || port != 25565 {
			t.Fatalf("got %s:%d, want mc.example.com:25565", host, port)
		}
	})

	t.Run("显式端口跳过查询", func(t *testing.T) {
		queried := stubSRVRecords(t, []*net.SRV{
			{Target: "real.example.com.", Port: 25577},
		}, nil)

		host, port := resolveServerTarget(context.Background(), "mc.example.com", 25566)
		if host != "mc.example.com" || port != 25566 {
			t.Fatalf("got %s:%d, want mc.example.com:25566", host, port)
		}
		if *queried != "" {
			t.Fatalf("显式端口不应发起 SRV 查询，实际查询了 %q", *queried)
		}
	})

	t.Run("IPv4 与 IPv6 字面量跳过查询", func(t *testing.T) {
		queried := stubSRVRecords(t, []*net.SRV{
			{Target: "real.example.com.", Port: 25577},
		}, nil)

		for _, address := range []string{"1.2.3.4", "::1", "2606:4700::1111"} {
			host, port := resolveServerTarget(context.Background(), address, 25565)
			if host != address || port != 25565 {
				t.Fatalf("%s: got %s:%d", address, host, port)
			}
		}
		if *queried != "" {
			t.Fatalf("IP 字面量不应发起 SRV 查询，实际查询了 %q", *queried)
		}
	})
}

// TestPingResolvesSRVTarget 端到端：SRV 替身指向本机假服务器，
// 验证 Ping 按解析后的目标建连并完成一次完整状态查询。
func TestPingResolvesSRVTarget(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动假服务器失败: %v", err)
	}
	defer listener.Close()

	go serveFakeStatus(listener)

	stubSRVRecords(t, []*net.SRV{
		{Target: "127.0.0.1", Port: uint16(listener.Addr().(*net.TCPAddr).Port)},
	}, nil)

	status, err := MinecraftServerPinger{}.Ping(context.Background(), "mc.example.com", 25565)
	if err != nil {
		t.Fatalf("意外报错: %v", err)
	}
	if status.VersionName != "1.21" || status.OnlinePlayers != 1 || status.MaxPlayers != 20 || status.Motd != "SRV OK" {
		t.Fatalf("状态解析不符: %+v", status)
	}
}

// serveFakeStatus 循环接受连接，每个连接回答一次状态响应（握手内容不校验）。
func serveFakeStatus(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		handleFakeStatusConn(conn)
	}
}

func handleFakeStatusConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	// 依次吞掉握手包与状态请求包
	if _, err := readPacket(conn); err != nil {
		return
	}
	if _, err := readPacket(conn); err != nil {
		return
	}

	response := []byte(`{"version":{"name":"1.21","protocol":767},"players":{"online":1,"max":20},"description":"SRV OK"}`)
	var packet bytesBuffer
	writeVarint(&packet, 0x00)
	writeString(&packet, string(response))
	_ = writePacket(conn, packet.bytes())
}
