package mcserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// usePlayersTestServer 造一台临时"服务器"（含 server.properties 与空名单）。
func usePlayersTestServer(t *testing.T, id string, whitelistEnabled bool) string {
	t.Helper()

	root := t.TempDir()
	serverRootOverride = &root
	t.Cleanup(func() { serverRootOverride = nil })

	dir := filepath.Join(root, id)
	writeTestFile(t, filepath.Join(dir, "server.properties"),
		"server-port=25565\nwhite-list="+boolText(whitelistEnabled)+"\n")

	return dir
}

func boolText(value bool) string {
	if value {
		return "true"
	}

	return "false"
}

// TestOfflinePlayerUUID 离线 UUID 必须与 Java 的 UUID.nameUUIDFromBytes 一致：
// 版本位 3、变体位 10xx、带连字符的小写十六进制，且区分大小写。
func TestOfflinePlayerUUID(t *testing.T) {
	uuid := OfflinePlayerUUID("Notch")

	if len(uuid) != 36 || strings.Count(uuid, "-") != 4 {
		t.Fatalf("格式不对：%q", uuid)
	}
	if uuid[14] != '3' {
		t.Fatalf("版本位应为 3：%q", uuid)
	}
	if !strings.ContainsRune("89ab", rune(uuid[19])) {
		t.Fatalf("变体位应为 8/9/a/b：%q", uuid)
	}
	if uuid != strings.ToLower(uuid) {
		t.Fatalf("应输出小写：%q", uuid)
	}

	// 稳定且区分大小写
	if OfflinePlayerUUID("Notch") != uuid {
		t.Fatal("同一名字必须得到相同 UUID")
	}
	if OfflinePlayerUUID("notch") == uuid {
		t.Fatal("名字大小写不同应得到不同 UUID")
	}

	// 已知向量：OfflinePlayer:Notch 的 MD5（用于防止算法被改坏）
	if uuid != "b50ad385-829d-3141-a216-7e7d7539ba7f" {
		t.Fatalf("离线 UUID 与预期不符：%q", uuid)
	}
}

// TestGetServerPlayersReadsFiles 读取四个来源：名单文件 + properties + 运行状态。
func TestGetServerPlayersReadsFiles(t *testing.T) {
	id := "players-read"
	dir := usePlayersTestServer(t, id, true)

	writeTestFile(t, filepath.Join(dir, whitelistFileName),
		`[{"uuid":"b50ad385-829d-3141-a216-7e7d7539ba7f","name":"Notch"}]`)
	writeTestFile(t, filepath.Join(dir, opsFileName),
		`[{"uuid":"b50ad385-829d-3141-a216-7e7d7539ba7f","name":"Notch","level":4,"bypassesPlayerLimit":false}]`)
	writeTestFile(t, filepath.Join(dir, bannedFileName),
		`[{"uuid":"00000000-0000-0000-0000-000000000000","name":"Griefer","created":"2026-01-01 00:00:00 +0000","source":"Server","expires":"forever","reason":"破坏"}]`)

	players := GetServerPlayers(id)

	if !players.WhitelistEnabled {
		t.Fatal("white-list=true 应被读到")
	}
	if len(players.Whitelist) != 1 || players.Whitelist[0].Name != "Notch" {
		t.Fatalf("白名单读取不符：%+v", players.Whitelist)
	}
	if len(players.Ops) != 1 || players.Ops[0].Level != 4 {
		t.Fatalf("OP 读取不符：%+v", players.Ops)
	}
	if len(players.Banned) != 1 || players.Banned[0].Reason != "破坏" {
		t.Fatalf("封禁读取不符：%+v", players.Banned)
	}
	if players.Running {
		t.Fatal("测试环境没有运行中的服务器")
	}
	// 未运行时在线列表为空数组（前端直接 map）
	if players.Online == nil || len(players.Online) != 0 {
		t.Fatalf("Online 应为空数组：%+v", players.Online)
	}
}

// TestGetServerPlayersMissingFiles 名单文件缺失/损坏时返回空列表而不是报错。
func TestGetServerPlayersMissingFiles(t *testing.T) {
	id := "players-missing"
	dir := usePlayersTestServer(t, id, false)

	players := GetServerPlayers(id)
	if len(players.Whitelist) != 0 || len(players.Ops) != 0 || len(players.Banned) != 0 {
		t.Fatalf("缺文件时应为空列表：%+v", players)
	}

	writeTestFile(t, filepath.Join(dir, whitelistFileName), "{ 这不是数组 }")
	if list := GetServerPlayers(id).Whitelist; len(list) != 0 {
		t.Fatalf("损坏文件应回落空列表：%+v", list)
	}
}

// TestSaveServerWhitelist 写入：补全离线 UUID、拒绝非法名字、服务器运行中拒绝写文件。
func TestSaveServerWhitelist(t *testing.T) {
	id := "players-save"
	dir := usePlayersTestServer(t, id, true)

	err := SaveServerWhitelist(id, []WhitelistEntry{
		{Name: "Notch"},
		{Name: "  Steve  "},
		{Name: "坏名字!"}, // 非法：应被丢弃
		{Name: "Alex", UUID: "B50AD385829D3141A2167E7D7539BA7F"},
	})
	if err != nil {
		t.Fatalf("写入白名单失败：%v", err)
	}

	data, readErr := os.ReadFile(filepath.Join(dir, whitelistFileName))
	if readErr != nil {
		t.Fatalf("读回失败：%v", readErr)
	}
	var entries []WhitelistEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("写入内容不是合法 JSON：%v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("应写入 3 条（丢弃非法名字）：%+v", entries)
	}
	if entries[0].UUID != OfflinePlayerUUID("Notch") {
		t.Fatalf("未补离线 UUID：%+v", entries[0])
	}
	if entries[1].Name != "Steve" {
		t.Fatalf("名字未去空格：%+v", entries[1])
	}
	// 无连字符的大写 UUID 应被规范化成小写带连字符
	if entries[2].UUID != "b50ad385-829d-3141-a216-7e7d7539ba7f" {
		t.Fatalf("UUID 未规范化：%+v", entries[2])
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatal("写入应以换行结尾（与服务端风格一致）")
	}
}

// TestSaveServerOpsAndBans 默认等级与缺失字段的补全。
func TestSaveServerOpsAndBans(t *testing.T) {
	id := "players-ops"
	dir := usePlayersTestServer(t, id, false)

	if err := SaveServerOps(id, []OpEntry{{Name: "Notch"}, {Name: "Alex", Level: 9}}); err != nil {
		t.Fatalf("写入 OP 失败：%v", err)
	}
	var ops []OpEntry
	data, _ := os.ReadFile(filepath.Join(dir, opsFileName))
	_ = json.Unmarshal(data, &ops)
	if len(ops) != 2 || ops[0].Level != 4 || ops[1].Level != 4 {
		t.Fatalf("等级应夹在 1–4：%+v", ops)
	}

	if err := SaveServerBanned(id, []BannedPlayerEntry{{Name: "Griefer"}}); err != nil {
		t.Fatalf("写入封禁失败：%v", err)
	}
	var banned []BannedPlayerEntry
	data, _ = os.ReadFile(filepath.Join(dir, bannedFileName))
	_ = json.Unmarshal(data, &banned)
	if len(banned) != 1 {
		t.Fatalf("封禁条目数不符：%+v", banned)
	}
	entry := banned[0]
	if entry.Expires != "forever" || entry.Source == "" || entry.Created == "" || entry.Reason == "" {
		t.Fatalf("封禁字段未补全：%+v", entry)
	}
	if entry.UUID != OfflinePlayerUUID("Griefer") {
		t.Fatalf("封禁 UUID 未补全：%+v", entry)
	}

	// 非法名字要报错（OP / 封禁是逐条校验的，不像白名单那样静默丢弃）
	if err := SaveServerOps(id, []OpEntry{{Name: "bad name"}}); err == nil {
		t.Fatal("非法 OP 名字应报错")
	}
}

// TestWritePlayersEntriesRejectsRunningServer 运行中禁止直接改名单文件
// （此时应该走指令，避免与服务端内存状态打架）。
func TestWritePlayersEntriesRejectsRunningServer(t *testing.T) {
	id := "players-running"
	usePlayersTestServer(t, id, false)

	// 直接把状态标成 running，模拟运行中的服务器
	state := Default().ensureState(id)
	state.mu.Lock()
	state.status = StatusRunning
	state.mu.Unlock()
	t.Cleanup(func() {
		state.mu.Lock()
		state.status = StatusStopped
		state.mu.Unlock()
	})

	err := SaveServerWhitelist(id, []WhitelistEntry{{Name: "Notch"}})
	if err == nil {
		t.Fatal("运行中写名单应被拒绝")
	}
	if !strings.Contains(err.Error(), "运行中") {
		t.Fatalf("错误信息应说明原因：%v", err)
	}
}

// TestRunPlayerCommandRequiresRunning 未运行时执行玩家指令应报错（提示去改名单）。
func TestRunPlayerCommandRequiresRunning(t *testing.T) {
	id := "players-command"
	usePlayersTestServer(t, id, false)

	if err := RunPlayerCommand(id, "whitelist add Notch"); err == nil {
		t.Fatal("未运行时应报错")
	}
	if err := RunPlayerCommand(id, "   "); err == nil {
		t.Fatal("空指令应报错")
	}
}

// TestParsePlayerListNames 在线玩家名解析（含 0 人、带空格、无冒号）。
func TestParsePlayerListNames(t *testing.T) {
	cases := []struct {
		response string
		count    int
		names    []string
	}{
		{"There are 2 of a max of 20 players online: Notch, Steve", 2, []string{"Notch", "Steve"}},
		{"There are 0 of a max of 20 players online:", 0, []string{}},
		{"There are 1 of a max of 20 players online: Alex", 1, []string{"Alex"}},
		{"Unknown command", 0, []string{}},
	}

	for _, testCase := range cases {
		count, names := parsePlayerListNames(testCase.response)
		if count != testCase.count || len(names) != len(testCase.names) {
			t.Fatalf("解析 %q → %d/%v，期望 %d/%v",
				testCase.response, count, names, testCase.count, testCase.names)
		}
		for index := range names {
			if names[index] != testCase.names[index] {
				t.Fatalf("第 %d 个名字 = %q，期望 %q", index, names[index], testCase.names[index])
			}
		}
	}
}

// TestSetWhitelistEnabled 白名单开关写进 server.properties。
func TestSetWhitelistEnabled(t *testing.T) {
	id := "players-toggle"
	dir := usePlayersTestServer(t, id, false)

	if err := SetWhitelistEnabled(id, true); err != nil {
		t.Fatalf("开启白名单失败：%v", err)
	}
	if !GetServerPlayers(id).WhitelistEnabled {
		t.Fatal("white-list 未写成 true")
	}
	if err := SetWhitelistEnabled(id, false); err != nil {
		t.Fatalf("关闭白名单失败：%v", err)
	}
	if GetServerPlayers(id).WhitelistEnabled {
		t.Fatal("white-list 未写成 false")
	}
	// 原有键保留
	if values := serverPropertyValues(dir); len(values) == 0 || values[0].Key != "server-port" {
		t.Fatalf("原有配置被破坏：%+v", values)
	}
}
