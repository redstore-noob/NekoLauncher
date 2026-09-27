package online

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// memoryStore settingStore 的内存实现：测试不碰真实配置目录。
type memoryStore struct {
	mu     sync.Mutex
	values map[string]string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{values: map[string]string{}}
}

func (s *memoryStore) Get(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.values[key]
}

func (s *memoryStore) Set(key, value string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.values[key] = value

	return true
}

func TestRoomCodeVariants(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantBody string
		valid    bool
	}{
		{name: "标准形态", input: "U/YNZE-U61D-2206-HXRG", wantBody: "YNZEU61D2206HXRG", valid: true},
		{name: "缺少前缀", input: "YNZE-U61D-2206-HXRG", wantBody: "YNZEU61D2206HXRG", valid: true},
		{name: "小写与空格", input: " ynze u61d 2206 hxrg ", wantBody: "YNZEU61D2206HXRG", valid: true},
		{name: "全角连字符", input: "YNZE－U61D－2206－HXRG", wantBody: "YNZEU61D2206HXRG", valid: true},
		{name: "含易混字符", input: "U/IOIO-IOIO-IOIO-IOIO", wantBody: "IOIOIOIOIOIOIOIO", valid: false},
		{name: "位数不足", input: "U/ABCD-EFGH", wantBody: "", valid: false},
		{name: "空输入", input: "   ", wantBody: "", valid: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := RoomCodeBody(testCase.input); got != testCase.wantBody {
				t.Fatalf("RoomCodeBody(%q) = %q，期望 %q", testCase.input, got, testCase.wantBody)
			}
			if got := RoomCodeLooksValid(testCase.input); got != testCase.valid {
				t.Fatalf("RoomCodeLooksValid(%q) = %v，期望 %v", testCase.input, got, testCase.valid)
			}
		})
	}
}

func TestRoomCodeVariantsOrder(t *testing.T) {
	variants := RoomCodeVariants("ynze-u61d-2206-hxrg")
	want := []string{"U/YNZE-U61D-2206-HXRG", "YNZE-U61D-2206-HXRG"}

	if len(variants) != len(want) {
		t.Fatalf("候选数量 = %d，期望 %d（%v）", len(variants), len(want), variants)
	}
	for index := range want {
		if variants[index] != want[index] {
			t.Fatalf("候选[%d] = %q，期望 %q", index, variants[index], want[index])
		}
	}
}

func TestRoomCodeVariantsFallback(t *testing.T) {
	// 无法解析时回退成"原样输入"，交给陶瓦进程自己判断
	variants := RoomCodeVariants("  custom-room  ")
	if len(variants) != 1 || variants[0] != "custom-room" {
		t.Fatalf("回退候选 = %v，期望 [custom-room]", variants)
	}
	if variants := RoomCodeVariants("   "); variants != nil {
		t.Fatalf("空输入应返回 nil，得到 %v", variants)
	}
}

func TestFormatRoomCodeForDisplay(t *testing.T) {
	if got := FormatRoomCodeForDisplay("ynzeu61d2206hxrg"); got != "U/ YNZE-U61D-2206-HXRG" && got != "U/YNZE-U61D-2206-HXRG" {
		t.Fatalf("FormatRoomCodeForDisplay = %q", got)
	}
	// 已带前缀时不应重复添加
	if got := FormatRoomCodeForDisplay("U/YNZE-U61D-2206-HXRG"); got != "U/YNZE-U61D-2206-HXRG" {
		t.Fatalf("已带前缀时被改写为 %q", got)
	}
	// 无法解析时原样返回
	if got := FormatRoomCodeForDisplay("随便写的"); got != "随便写的" {
		t.Fatalf("无法解析时应原样返回，得到 %q", got)
	}
}

func TestNormalizeTargetAddress(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "127.0.0.1:25565", want: "127.0.0.1:25565"},
		{input: "localhost", want: "localhost:25565"},
		{input: " 1.2.3.4:12345 ", want: "1.2.3.4:12345"},
		{input: ":25570", want: "127.0.0.1:25570"},
		{input: "127.0.0.1：25566", want: "127.0.0.1:25566"},
		{input: "", wantErr: true},
		{input: "127.0.0.1:0", wantErr: true},
		{input: "127.0.0.1:70000", wantErr: true},
		{input: "127.0.0.1:abc", wantErr: true},
	}

	for _, testCase := range cases {
		got, err := NormalizeTargetAddress(testCase.input)
		if testCase.wantErr {
			if err == nil {
				t.Fatalf("NormalizeTargetAddress(%q) 期望报错，得到 %q", testCase.input, got)
			}

			continue
		}
		if err != nil {
			t.Fatalf("NormalizeTargetAddress(%q) 报错：%v", testCase.input, err)
		}
		if got != testCase.want {
			t.Fatalf("NormalizeTargetAddress(%q) = %q，期望 %q", testCase.input, got, testCase.want)
		}
	}
}

func TestSplitAndDisplayAddress(t *testing.T) {
	host, port := SplitHostPort("122.51.108.96:12345")
	if host != "122.51.108.96" || port != 12345 {
		t.Fatalf("SplitHostPort = %s/%d", host, port)
	}

	// 缺端口时补默认端口
	host, port = SplitHostPort("example.com")
	if host != "example.com" || port != 25565 {
		t.Fatalf("SplitHostPort(缺端口) = %s/%d", host, port)
	}

	if got := DisplayAddress("122.51.108.96:25565"); got != "122.51.108.96" {
		t.Fatalf("默认端口应省略，得到 %q", got)
	}
	if got := DisplayAddress("122.51.108.96:12345"); got != "122.51.108.96:12345" {
		t.Fatalf("非默认端口应保留，得到 %q", got)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	store := newMemoryStore()

	defaults := loadSettings(store)
	if defaults.Provider != string(ProviderTerracotta) {
		t.Fatalf("默认供应商 = %q", defaults.Provider)
	}
	if defaults.RedstoneRelay != DefaultRelayAddress {
		t.Fatalf("默认中继 = %q", defaults.RedstoneRelay)
	}
	if defaults.MaxPlayers != DefaultMaxPlayers {
		t.Fatalf("默认并发 = %d", defaults.MaxPlayers)
	}
	if defaults.Target != DefaultTargetAddress {
		t.Fatalf("默认目标 = %q", defaults.Target)
	}

	saved := Settings{
		Provider:       string(ProviderRedstone),
		Player:         "猫娘",
		TerracottaPath: "/tmp/terracotta",
		RedstoneRelay:  "relay.example.com:3000",
		RedstoneKey:    "abcdefghijklmnopqrst",
		Target:         "127.0.0.1:25570",
		ServerID:       "srv-1",
		MaxPlayers:     999,
	}
	if err := saveSettings(store, saved); err != nil {
		t.Fatalf("saveSettings 报错：%v", err)
	}

	loaded := loadSettings(store)
	if loaded.Provider != string(ProviderRedstone) || loaded.Player != "猫娘" ||
		loaded.RedstoneRelay != "relay.example.com:3000" || loaded.Target != "127.0.0.1:25570" ||
		loaded.ServerID != "srv-1" || loaded.RedstoneKey != "abcdefghijklmnopqrst" {
		t.Fatalf("设置回读不一致：%+v", loaded)
	}
	// 并发上限被夹到 50，避免一条命令拉起几百个协程
	if loaded.MaxPlayers != 50 {
		t.Fatalf("并发上限应被夹紧到 50，得到 %d", loaded.MaxPlayers)
	}
}

func TestNormalizeProviderID(t *testing.T) {
	if got := NormalizeProviderID("redstone"); got != ProviderRedstone {
		t.Fatalf("NormalizeProviderID(redstone) = %q", got)
	}
	if got := NormalizeProviderID(" terracotta "); got != ProviderTerracotta {
		t.Fatalf("NormalizeProviderID = %q", got)
	}
	if got := NormalizeProviderID("whatever"); got != ProviderTerracotta {
		t.Fatalf("未知供应商应回落到陶瓦，得到 %q", got)
	}
}

func TestGenerateAPIKey(t *testing.T) {
	first, err := generateAPIKey(20)
	if err != nil {
		t.Fatalf("generateAPIKey 报错：%v", err)
	}
	if len(first) != 20 {
		t.Fatalf("密钥长度 = %d，期望 20", len(first))
	}

	second, err := generateAPIKey(20)
	if err != nil {
		t.Fatalf("generateAPIKey 报错：%v", err)
	}
	if first == second {
		t.Fatalf("两次生成的密钥相同：%s", first)
	}
}

func TestReadTerracottaPortFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "port.json")

	if got := readTerracottaPortFile(path); got != 0 {
		t.Fatalf("文件不存在时应返回 0，得到 %d", got)
	}

	if err := os.WriteFile(path, []byte(`{"port":`), 0o600); err != nil {
		t.Fatalf("写入测试文件失败：%v", err)
	}
	if got := readTerracottaPortFile(path); got != 0 {
		// 进程是"先写 .tmp 再 rename"，读到半截只可能是外部干扰，必须当成未就绪
		t.Fatalf("半截 JSON 应返回 0，得到 %d", got)
	}

	if err := os.WriteFile(path, []byte(`{"port":41234}`), 0o600); err != nil {
		t.Fatalf("写入测试文件失败：%v", err)
	}
	if got := readTerracottaPortFile(path); got != 41234 {
		t.Fatalf("端口解析 = %d，期望 41234", got)
	}

	if err := os.WriteFile(path, []byte(`{"port":70000}`), 0o600); err != nil {
		t.Fatalf("写入测试文件失败：%v", err)
	}
	if got := readTerracottaPortFile(path); got != 0 {
		t.Fatalf("越界端口应返回 0，得到 %d", got)
	}
}

func TestTerracottaExceptionText(t *testing.T) {
	if got := terracottaExceptionText(nil); got == "" {
		t.Fatal("nil 也应给出可读文案")
	}

	for kind := 0; kind <= 5; kind++ {
		value := kind
		if got := terracottaExceptionText(&value); got == "" {
			t.Fatalf("错误码 %d 没有文案", kind)
		}
	}

	unknown := 99
	if got := terracottaExceptionText(&unknown); got == "" {
		t.Fatal("未知错误码也应给出可读文案")
	}
}

func TestTerracottaDifficultyIgnored(t *testing.T) {
	// 难度只是陶瓦内部的连通性评估，不进状态快照（提示文案必须是固定文案，
	// 否则前端没法查词典），这里只确认它不会让状态映射出错。
	provider := newTerracottaProvider(newMemoryStore())

	provider.applyState(terracottaStatePayload{
		State:      "guest-starting",
		Difficulty: "TOUGH",
	})

	if status := provider.Status(); status.State != StateStarting || status.Tip == "" {
		t.Fatalf("难度字段不应影响状态映射：%+v", status)
	}
}
