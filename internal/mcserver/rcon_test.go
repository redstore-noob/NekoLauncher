package mcserver

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// startFakeRCON 起一个最小可用的假 RCON 服务端：校验密码后按指令回响应。
// 返回监听地址与关闭函数。
func startFakeRCON(t *testing.T, password string, responses map[string]string) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败：%v", err)
	}

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			go func() {
				defer conn.Close()
				reader := bufio.NewReader(conn)

				readPacket := func() (int32, int32, string, error) {
					header := make([]byte, 4)
					if _, readErr := io.ReadFull(reader, header); readErr != nil {
						return 0, 0, "", readErr
					}
					length := int32(binary.LittleEndian.Uint32(header))
					payload := make([]byte, length)
					if _, readErr := io.ReadFull(reader, payload); readErr != nil {
						return 0, 0, "", readErr
					}

					return int32(binary.LittleEndian.Uint32(payload[0:4])),
						int32(binary.LittleEndian.Uint32(payload[4:8])),
						strings.TrimRight(string(payload[8:]), "\x00"), nil
				}

				writePacket := func(id, kind int32, body string) {
					payload := make([]byte, 0, len(body)+12)
					payload = binary.LittleEndian.AppendUint32(payload, uint32(len(body)+10))
					payload = binary.LittleEndian.AppendUint32(payload, uint32(id))
					payload = binary.LittleEndian.AppendUint32(payload, uint32(kind))
					payload = append(payload, body...)
					payload = append(payload, 0x00, 0x00)
					_, _ = conn.Write(payload)
				}

				for {
					id, kind, body, readErr := readPacket()
					if readErr != nil {
						return
					}
					switch kind {
					case rconTypeAuth:
						if body != password {
							writePacket(rconAuthFailedRequestID, rconTypeResponse, "")

							continue
						}
						writePacket(id, rconTypeResponse, "")
					case rconTypeCommand:
						response, ok := responses[body]
						if !ok {
							response = "Unknown command"
						}
						writePacket(id, rconTypeResponse, response)
					}
				}
			}()
		}
	}()

	return listener.Addr().String(), func() { _ = listener.Close() }
}

// TestRCONCommandRoundTrip 正常路径：鉴权通过后指令能拿到响应。
func TestRCONCommandRoundTrip(t *testing.T) {
	address, stop := startFakeRCON(t, "s3cret", map[string]string{
		"list": "There are 3 of a max of 20 players online: A, B, C",
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	response, err := runRCONCommand(ctx, address, "s3cret", "list")
	if err != nil {
		t.Fatalf("执行 RCON 指令失败：%v", err)
	}
	if !strings.Contains(response, "There are 3 of a max of 20") {
		t.Fatalf("响应不符：%q", response)
	}

	players, ok := parsePlayerList(response)
	if !ok || players != 3 {
		t.Fatalf("在线人数解析失败：%d / %v", players, ok)
	}
}

// TestRCONWrongPassword 密码错误必须报错而不是静默当成成功。
func TestRCONWrongPassword(t *testing.T) {
	address, stop := startFakeRCON(t, "right", nil)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := runRCONCommand(ctx, address, "wrong", "list"); err == nil {
		t.Fatal("密码错误时应当报错")
	}
}

// TestRCONMissingConfiguration 没配密码/地址时不建连接，直接报错（调用方据此退回 stdin）。
func TestRCONMissingConfiguration(t *testing.T) {
	ctx := context.Background()

	if _, err := runRCONCommand(ctx, "127.0.0.1:25575", "", "list"); err == nil {
		t.Fatal("空密码应报错")
	}
	if _, err := runRCONCommand(ctx, "", "pass", "list"); err == nil {
		t.Fatal("空地址应报错")
	}
}

// TestRCONUnreachable 服务端没监听时返回错误（不能 panic 也不能挂死）。
func TestRCONUnreachable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败：%v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close() // 立刻关掉：端口必然无人监听

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	if _, err := runRCONCommand(ctx, address, "pass", "list"); err == nil {
		t.Fatal("无人监听时应报错")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("连接失败应快速返回，实际 %v", elapsed)
	}
}

// TestEnsureRCONProperties 首次写入会生成密码并开启 RCON；二次调用保持原密码。
func TestEnsureRCONProperties(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server.properties"),
		[]byte("#Minecraft server properties\nserver-port=25565\nmotd=测试\n"), 0o644); err != nil {
		t.Fatalf("写 properties 失败：%v", err)
	}

	first, err := ensureRCONProperties(dir)
	if err != nil {
		t.Fatalf("写入 RCON 配置失败：%v", err)
	}
	if !first.Enabled || first.Password == "" || first.Port != 25575 {
		t.Fatalf("RCON 配置不符：%+v", first)
	}
	if len(first.Password) != 24 {
		t.Fatalf("密码长度 = %d，期望 24", len(first.Password))
	}

	// 原有键必须保留，新键追加
	lines, err := loadPropertyLines(dir)
	if err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	values := map[string]string{}
	for _, line := range lines {
		if line.key != "" {
			values[line.key] = line.value
		}
	}
	if values["server-port"] != "25565" || values["motd"] != "测试" {
		t.Fatalf("原有配置被破坏：%+v", values)
	}
	if values["enable-rcon"] != "true" || values["rcon.password"] != first.Password {
		t.Fatalf("RCON 配置未写入：%+v", values)
	}
	if values["rcon.port"] != strconv.Itoa(first.Port) {
		t.Fatalf("rcon.port = %q", values["rcon.port"])
	}

	// 二次调用：密码保持不变（否则每次启动都会让上次的 RCON 会话失效）
	second, err := ensureRCONProperties(dir)
	if err != nil {
		t.Fatalf("二次写入失败：%v", err)
	}
	if second.Password != first.Password {
		t.Fatalf("密码被重新生成：%q → %q", first.Password, second.Password)
	}
}

// TestReadRCONConfiguration 只有 enable-rcon=true 且密码非空才算配置好。
func TestReadRCONConfiguration(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		enabled  bool
		port     int
		password string
	}{
		{"完整配置", "enable-rcon=true\nrcon.port=25580\nrcon.password=abc\n", true, 25580, "abc"},
		{"未开启", "enable-rcon=false\nrcon.password=abc\n", false, 0, ""},
		{"缺密码", "enable-rcon=true\n", false, 0, ""},
		{"端口非法回落默认", "enable-rcon=true\nrcon.port=abc\nrcon.password=x\n", true, 25575, "x"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(testCase.content), 0o644); err != nil {
				t.Fatalf("写文件失败：%v", err)
			}
			configuration := readRCONConfiguration(dir)
			if configuration.Enabled != testCase.enabled {
				t.Fatalf("Enabled = %v，期望 %v", configuration.Enabled, testCase.enabled)
			}
			if configuration.Port != testCase.port {
				t.Fatalf("Port = %d，期望 %d", configuration.Port, testCase.port)
			}
			if configuration.Password != testCase.password {
				t.Fatalf("Password = %q，期望 %q", configuration.Password, testCase.password)
			}
		})
	}

	// 文件不存在：零值，不 panic
	if configuration := readRCONConfiguration(t.TempDir()); configuration.Enabled {
		t.Fatalf("没有 properties 时不该认为已启用：%+v", configuration)
	}
}

// TestParsePlayerList 各种 list 文案（含中文/英文、0 人）。
func TestParsePlayerList(t *testing.T) {
	cases := []struct {
		response string
		players  int
		ok       bool
	}{
		{"There are 3 of a max of 20 players online: A, B, C", 3, true},
		{"There are 0 of a max of 20 players online:", 0, true},
		{"There are 1 of a max of 10 players online: Steve", 1, true},
		{"Unknown command", 0, false},
		{"", 0, false},
	}

	for _, testCase := range cases {
		players, ok := parsePlayerList(testCase.response)
		if ok != testCase.ok || (ok && players != testCase.players) {
			t.Fatalf("parsePlayerList(%q) = %d/%v，期望 %d/%v",
				testCase.response, players, ok, testCase.players, testCase.ok)
		}
	}
}
