package auth

import (
	"encoding/json"
	"strings"
	"testing"

	"nekolauncher/internal/config"
)

// withTempSharedAccounts 把全局 AccountStore 的存储目录换成临时目录，
// 并把内存列表替换成给定账号；测试结束只还原内存列表（不读写真实配置）。
func withTempSharedAccounts(t *testing.T, accounts ...*LaunchAccount) {
	t.Helper()
	// 先做内存快照：t.Cleanup 后进先出，这个回调会在「还原存储目录」之前执行，
	// 因此不会去读真实 accounts.yaml
	original := Shared.Current()
	useTempAuthStorage(t)
	t.Cleanup(func() {
		Shared.gate.Lock()
		Shared.current = original
		Shared.gate.Unlock()
	})

	Shared.Reload() // 从临时目录重新开始（全新安装 → 默认离线账号）
	for _, existing := range Shared.Current() {
		Shared.Remove(existing)
	}
	for _, account := range accounts {
		Shared.Add(account)
	}
}

func offlineAccount(name string) *LaunchAccount {
	return &LaunchAccount{
		Type: "offline", DisplayName: name, OfflineName: name, OfflineSkinId: "steve",
	}
}

// TestComponentDisplayAccountKey 读取组件展示账号覆盖：空 id、未配置、配置损坏、
// 值缺失/空白都视为「跟随全局当前账号」。
// 防的回归：配置损坏或按键缺失时 panic / 返回半截值，皮肤与披风页面拿到空键后
// 显示「没有账号」。
func TestComponentDisplayAccountKey(t *testing.T) {
	useTempAuthStorage(t)

	if got := ComponentDisplayAccountKey(""); got != "" {
		t.Fatalf("空组件 id 应返回空串，实际 %q", got)
	}
	if got := ComponentDisplayAccountKey("   "); got != "" {
		t.Fatalf("空白组件 id 应返回空串，实际 %q", got)
	}
	if got := ComponentDisplayAccountKey("skin"); got != "" {
		t.Fatalf("未配置时应返回空串，实际 %q", got)
	}

	if !config.SetValue(componentDisplayConfigKey, "{不是 JSON") {
		t.Fatal("写入损坏配置失败")
	}
	if got := ComponentDisplayAccountKey("skin"); got != "" {
		t.Fatalf("配置损坏应视为未设置覆盖，实际 %q", got)
	}

	mapping := `{"skin":"offline:A","cape":"   ","empty":""}`
	if !config.SetValue(componentDisplayConfigKey, mapping) {
		t.Fatal("写入覆盖配置失败")
	}
	if got := ComponentDisplayAccountKey("skin"); got != "offline:A" {
		t.Fatalf("已配置的组件应返回覆盖键，实际 %q", got)
	}
	for _, component := range []string{"cape", "empty", "missing"} {
		if got := ComponentDisplayAccountKey(component); got != "" {
			t.Fatalf("组件 %q 的空白/缺失值应视为未设置，实际 %q", component, got)
		}
	}
}

// TestSetComponentDisplayAccountKey 写入/清除组件展示账号覆盖，
// 且不清掉其它组件的覆盖。
// 防的回归：每次设置都整体覆盖 map，设置一个组件的账号会把别的组件的选择清掉。
func TestSetComponentDisplayAccountKey(t *testing.T) {
	useTempAuthStorage(t)

	SetComponentDisplayAccountKey("skin", "offline:A")
	SetComponentDisplayAccountKey("cape", "offline:B")
	if got := ComponentDisplayAccountKey("skin"); got != "offline:A" {
		t.Fatalf("skin 覆盖键 = %q", got)
	}
	if got := ComponentDisplayAccountKey("cape"); got != "offline:B" {
		t.Fatalf("设置 cape 不应影响 skin，实际 %q", got)
	}

	// 传空串 = 恢复跟随全局当前账号
	SetComponentDisplayAccountKey("skin", "   ")
	if got := ComponentDisplayAccountKey("skin"); got != "" {
		t.Fatalf("清空后 skin 覆盖键 = %q，期望空串（跟随全局当前账号）", got)
	}
	if got := ComponentDisplayAccountKey("cape"); got != "offline:B" {
		t.Fatalf("清空 skin 不应影响 cape，实际 %q", got)
	}

	stored := config.GetValue(componentDisplayConfigKey)
	var decoded map[string]string
	if err := json.Unmarshal([]byte(stored), &decoded); err != nil {
		t.Fatalf("落盘的覆盖配置不是合法 JSON：%v（%q）", err, stored)
	}
	if _, exists := decoded["skin"]; exists {
		t.Fatalf("空值应当删除键而不是写入空串：%v", decoded)
	}
	if decoded["cape"] != "offline:B" {
		t.Fatalf("落盘结果 = %v", decoded)
	}

	// 已有配置损坏时仍能写入（不 panic、不丢新值）
	if !config.SetValue(componentDisplayConfigKey, "broken") {
		t.Fatal("写入损坏配置失败")
	}
	SetComponentDisplayAccountKey("skin", "offline:A")
	if got := ComponentDisplayAccountKey("skin"); got != "offline:A" {
		t.Fatalf("配置损坏后写入应生效，实际 %q", got)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("空组件 id 应当 panic（调用方必须提供组件名）")
		}
	}()
	SetComponentDisplayAccountKey("   ", "offline:A")
}

// TestResolveComponentDisplayAccount 组件展示账号的解析优先级：
// 覆盖账号存在则用它，否则回落全局当前账号，都没有则 nil。
// 防的回归：覆盖里指定的账号被删掉后组件显示空账号（而不是回落当前账号），
// 以及覆盖失效时把「皮肤页面」绑到错误的账号上。
func TestResolveComponentDisplayAccount(t *testing.T) {
	first := offlineAccount("A")
	second := offlineAccount("B")
	withTempSharedAccounts(t, first, second) // 列表 = [B, A]，当前账号是 B

	if got := Shared.Selected(); got != second {
		t.Fatalf("前置条件不成立：当前账号 = %+v，期望最后一个添加的 B", got)
	}
	if got := ResolveComponentDisplayAccount("skin"); got != second {
		t.Fatalf("未设置覆盖时应跟随全局当前账号，实际 %+v", got)
	}
	if got := ResolveComponentDisplayAccount(""); got != second {
		t.Fatalf("空组件 id 应跟随全局当前账号，实际 %+v", got)
	}

	// 设置为非当前账号：组件应展示它，且不改变全局当前账号
	SetComponentDisplayAccountKey("skin", Shared.GetStableKey(first))
	if got := ResolveComponentDisplayAccount("skin"); got != first {
		t.Fatalf("设置了覆盖时应展示覆盖账号，实际 %+v", got)
	}
	if got := ResolveComponentDisplayAccount("cape"); got != second {
		t.Fatalf("其它组件不应受影响，实际 %+v", got)
	}
	if Shared.Selected() != second {
		t.Fatal("组件展示覆盖不应改变全局当前账号")
	}

	// 覆盖指向已不存在的账号：回落全局当前账号
	SetComponentDisplayAccountKey("skin", "offline:已删除的账号")
	if got := ResolveComponentDisplayAccount("skin"); got != second {
		t.Fatalf("覆盖账号不存在时应回落当前账号，实际 %+v", got)
	}

	// 清空覆盖：回到当前账号
	SetComponentDisplayAccountKey("skin", "")
	if got := ResolveComponentDisplayAccount("skin"); got != second {
		t.Fatalf("清空覆盖后应回到当前账号，实际 %+v", got)
	}
}

// TestResolveComponentDisplayAccountWithoutAccounts 一个账号都没有时必须返回 nil
// （调用方据此展示「请先添加账号」而不是崩溃）。
// 防的回归：空列表时索引越界 panic（账号页面是启动后的默认页面）。
func TestResolveComponentDisplayAccountWithoutAccounts(t *testing.T) {
	withTempSharedAccounts(t)

	if got := Shared.Selected(); got != nil {
		t.Fatalf("前置条件不成立：当前账号 = %+v，期望 nil", got)
	}
	SetComponentDisplayAccountKey("skin", "offline:A")
	if got := ResolveComponentDisplayAccount("skin"); got != nil {
		t.Fatalf("无任何账号时应返回 nil，实际 %+v", got)
	}
	if got := ResolveComponentDisplayAccount(""); got != nil {
		t.Fatalf("无任何账号时应返回 nil，实际 %+v", got)
	}
	if !strings.HasPrefix(ComponentDisplayAccountKey("skin"), "offline:") {
		t.Fatal("覆盖键本身应当已写入（与账号是否存在无关）")
	}
}
