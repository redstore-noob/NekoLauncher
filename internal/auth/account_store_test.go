package auth

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nekolauncher/internal/config"
)

// TestGetStableKeyPerType 账号持久身份键按类型取值：正版用档案 UUID、
// 皮肤站用「角色 UUID@API 根」、离线用玩家名，其余类型回落显示名。
// 防的回归：身份键退化成显示名后，改名/换角色会让同一账号被当成两个账号
// （列表里出现重复项，且旧凭据再也更新不到）。
func TestGetStableKeyPerType(t *testing.T) {
	service := NewAccountStoreService(false)
	cases := []struct {
		name    string
		account *LaunchAccount
		want    string
	}{
		{
			name: "正版按档案 UUID",
			account: &LaunchAccount{Type: "microsoft", DisplayName: "NyaPlayer",
				Microsoft: &MicrosoftAccount{Uuid: "069a79f4", Username: "NyaPlayer"}},
			want: "microsoft:069a79f4",
		},
		{
			name: "皮肤站按角色 UUID@API 根",
			account: &LaunchAccount{Type: "authlib", DisplayName: "NyaPlayer",
				Authlib: &AuthlibCredential{ProfileUuid: "uuid-1", ApiRoot: "https://skin.example/api/"}},
			want: "authlib:uuid-1@https://skin.example/api",
		},
		{
			name:    "离线按玩家名",
			account: &LaunchAccount{Type: "offline", DisplayName: "Steve", OfflineName: "Steve"},
			want:    "offline:Steve",
		},
		{
			name:    "未知类型回落显示名",
			account: &LaunchAccount{Type: "thirdparty", DisplayName: "SomeOne"},
			want:    "thirdparty:SomeOne",
		},
		{
			name:    "正版凭据缺失时回落显示名",
			account: &LaunchAccount{Type: "microsoft", DisplayName: "NyaPlayer"},
			want:    "microsoft:NyaPlayer",
		},
		{
			name:    "显示名也为空时键仍然稳定",
			account: &LaunchAccount{Type: "offline"},
			want:    "offline:",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := service.GetStableKey(testCase.account); got != testCase.want {
				t.Fatalf("GetStableKey() = %q，期望 %q", got, testCase.want)
			}
		})
	}
}

// TestGetStableKeyRejectsNilAccount nil 账号必须 panic（这是包内约定），
// 而不是静默返回一个空键——空键会让「查找/更新/删除」误命中别的账号。
// 防的回归：nil 检查被删掉后，空稳定键让不同账号互相覆盖。
func TestGetStableKeyRejectsNilAccount(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("GetStableKey(nil) 应当 panic")
		}
	}()
	NewAccountStoreService(false).GetStableKey(nil)
}

// TestAuthlibStableKeyIncludesApiRoot 同一角色 UUID 在不同皮肤站是不同账号。
// 防的回归：身份键只取角色 UUID，两个皮肤站的同名角色互相顶掉。
func TestAuthlibStableKeyIncludesApiRoot(t *testing.T) {
	service := NewAccountStoreService(false)
	first := &LaunchAccount{Type: "authlib", DisplayName: "Nya",
		Authlib: &AuthlibCredential{ProfileUuid: "same-uuid", ApiRoot: "https://a.example"}}
	second := &LaunchAccount{Type: "authlib", DisplayName: "Nya",
		Authlib: &AuthlibCredential{ProfileUuid: "same-uuid", ApiRoot: "https://b.example"}}

	if service.GetStableKey(first) == service.GetStableKey(second) {
		t.Fatal("不同皮肤站的同名角色不应共享身份键")
	}
	// API 根尾斜杠不影响身份
	withSlash := &LaunchAccount{Type: "authlib", DisplayName: "Nya",
		Authlib: &AuthlibCredential{ProfileUuid: "same-uuid", ApiRoot: "https://a.example/"}}
	if service.GetStableKey(withSlash) != service.GetStableKey(first) {
		t.Fatal("API 根尾斜杠不应产生新的身份键")
	}
}

// TestUpdateMicrosoftAccountMatchesByStableKeyNotPointer 更新凭据必须按稳定键
// 匹配（大小写不敏感），而不是按指针。
// 防的回归：Wails 会把前端传来的账号对象重新 JSON 解码，引用相等永远不成立——
// 改回指针比较后，前端每次保存凭据都会收到「账号已不存在，可能刚被删除」。
func TestUpdateMicrosoftAccountMatchesByStableKeyNotPointer(t *testing.T) {
	useTempAuthStorage(t)
	service := NewAccountStoreService(false)
	service.Add(&LaunchAccount{
		Type: "microsoft", DisplayName: "NyaPlayer",
		Microsoft: &MicrosoftAccount{Uuid: "UUID-1", Username: "NyaPlayer", AccessToken: "old"},
	})

	// 模拟前端解码出来的等价对象：不同指针、UUID 大小写不同（类型字符串保持后端
	// 写入时的形态——GetStableKey 的 switch 按小写匹配）
	copyFromFrontend := &LaunchAccount{
		Type: "microsoft", DisplayName: "NyaPlayer",
		Microsoft: &MicrosoftAccount{Uuid: "uuid-1", Username: "NyaPlayer"},
	}
	refreshed := &MicrosoftAccount{
		Uuid: "UUID-1", Username: "NyaPlayer", AccessToken: "new-access", RefreshToken: "new-refresh",
	}
	if err := service.UpdateMicrosoftAccount(copyFromFrontend, refreshed); err != nil {
		t.Fatalf("等价对象（同稳定键）更新不应报错：%v", err)
	}
	if copyFromFrontend.Microsoft != refreshed {
		t.Fatal("凭据必须被写入账号对象")
	}
	if len(service.Current()) != 1 {
		t.Fatalf("更新不应改变列表长度：%d", len(service.Current()))
	}

	// 通过稳定键查回来的账号必须仍然是同一个（可被选中/刷新）
	found := service.FindByStableKey("MICROSOFT:uuid-1")
	if found == nil {
		t.Fatal("按稳定键应当能查到刚更新的账号")
	}

	// 澄清一个易踩的坑（当前行为，未改动）：GetStableKey 的 switch 按小写类型匹配，
	// Type 写成 "Microsoft" 会掉到 default 分支、身份退化成显示名，于是同键匹配失败
	// 并报「账号已不存在」。内部调用方都传后端写下的原值（小写），所以当前无碍；
	// 若哪天前端开始改写 Type 的大小写，凭据更新会被静默拒绝。
	capitalized := &LaunchAccount{Type: "Microsoft", DisplayName: "NyaPlayer",
		Microsoft: &MicrosoftAccount{Uuid: "UUID-1", Username: "NyaPlayer"}}
	if service.GetStableKey(capitalized) == service.GetStableKey(copyFromFrontend) {
		t.Fatal("类型段首字母大写会走 GetStableKey 的 default 分支，身份键应当不同")
	}
	if err := service.UpdateMicrosoftAccount(capitalized, refreshed); err == nil {
		t.Fatal("类型段被改写大小写时应当报「账号已不存在」，而不是静默丢弃这次更新")
	}
}

// TestUpdateAccountRejectsUnknownOrWrongType 更新凭据的三种拒绝分支。
// 防的回归：账号在凭据校验的网络往返期间被删除时 panic（调用方在启动 goroutine 上，
// panic 会把整个应用带走），以及把皮肤站凭据写进正版账号。
func TestUpdateAccountRejectsUnknownOrWrongType(t *testing.T) {
	useTempAuthStorage(t)
	service := NewAccountStoreService(false)
	service.Add(&LaunchAccount{
		Type: "offline", DisplayName: "Steve", OfflineName: "Steve", OfflineSkinId: "steve",
	})

	cases := []struct {
		name         string
		account      *LaunchAccount
		wantContains string
	}{
		{name: "账号为 nil", account: nil, wantContains: "不能为空"},
		{
			name:         "账号已不存在",
			account:      &LaunchAccount{Type: "offline", OfflineName: "Ghost"},
			wantContains: "账号已不存在",
		},
		{
			name:         "类型不匹配",
			account:      &LaunchAccount{Type: "offline", OfflineName: "Steve"},
			wantContains: "只能更新账号存储中的正版账号",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := service.UpdateMicrosoftAccount(testCase.account, &MicrosoftAccount{Uuid: "u"}); err == nil {
				t.Fatal("应当报错")
			} else if !strings.Contains(err.Error(), testCase.wantContains) {
				t.Fatalf("错误 = %v，期望包含 %q", err, testCase.wantContains)
			}
		})
	}

	if err := service.UpdateMicrosoftAccount(
		&LaunchAccount{Type: "offline", OfflineName: "Steve"}, nil); err == nil {
		t.Fatal("凭据为 nil 时应当报错")
	}
	if err := service.UpdateAuthlibAccount(nil, &AuthlibCredential{}); err == nil {
		t.Fatal("账号为 nil 时应当报错")
	}
}

// TestUpdateAuthlibAccountSyncsDisplayName 皮肤站账号的显示名跟随角色名。
// 防的回归：同一账号换了角色后列表仍显示旧角色名，用户以为登录到了别的账号。
func TestUpdateAuthlibAccountSyncsDisplayName(t *testing.T) {
	useTempAuthStorage(t)
	service := NewAccountStoreService(false)
	account := &LaunchAccount{
		Type: "authlib", DisplayName: "旧角色",
		Authlib: &AuthlibCredential{ProfileUuid: "uuid-1", ApiRoot: "https://skin.example", ProfileName: "旧角色"},
	}
	service.Add(account)

	if err := service.UpdateAuthlibAccount(account, &AuthlibCredential{
		ProfileUuid: "uuid-1", ApiRoot: "https://skin.example",
		ProfileName: "新角色", AccessToken: "t",
	}); err != nil {
		t.Fatalf("更新皮肤站凭据失败：%v", err)
	}
	if account.DisplayName != "新角色" {
		t.Fatalf("DisplayName = %q，期望跟随角色名更新为 新角色", account.DisplayName)
	}

	// 角色名为空（服务端未返回）时不应把显示名清空
	if err := service.UpdateAuthlibAccount(account, &AuthlibCredential{
		ProfileUuid: "uuid-1", ApiRoot: "https://skin.example", AccessToken: "t2",
	}); err != nil {
		t.Fatalf("更新皮肤站凭据失败：%v", err)
	}
	if account.DisplayName != "新角色" {
		t.Fatalf("DisplayName = %q，角色名为空时不应清空显示名", account.DisplayName)
	}

	// 类型不匹配
	offline := &LaunchAccount{Type: "offline", DisplayName: "Steve", OfflineName: "Steve"}
	service.Add(offline)
	if err := service.UpdateAuthlibAccount(offline, &AuthlibCredential{}); err == nil ||
		!strings.Contains(err.Error(), "只能更新账号存储中的皮肤站账号") {
		t.Fatalf("错误 = %v，期望类型不匹配提示", err)
	}
}

// TestUpdateOfflineSkinBranches 离线皮肤更新的参数校验与类型校验。
// 防的回归：空皮肤 ID 被写入配置，皮肤页面加载时找不到资源。
func TestUpdateOfflineSkinBranches(t *testing.T) {
	useTempAuthStorage(t)
	service := NewAccountStoreService(false)
	account := &LaunchAccount{Type: "offline", DisplayName: "Steve", OfflineName: "Steve", OfflineSkinId: "steve"}
	service.Add(account)

	if err := service.UpdateOfflineSkin(account, "  "); err == nil {
		t.Fatal("空皮肤 ID 应当被拒绝")
	}
	if err := service.UpdateOfflineSkin(nil, "alex"); err == nil {
		t.Fatal("账号为 nil 应当被拒绝")
	}
	if err := service.UpdateOfflineSkin(account, "alex"); err != nil {
		t.Fatalf("更新皮肤失败：%v", err)
	}
	if account.OfflineSkinId != "alex" {
		t.Fatalf("OfflineSkinId = %q，期望 alex", account.OfflineSkinId)
	}

	microsoft := &LaunchAccount{Type: "microsoft", DisplayName: "Nya",
		Microsoft: &MicrosoftAccount{Uuid: "u1"}}
	service.Add(microsoft)
	if err := service.UpdateOfflineSkin(microsoft, "alex"); err == nil ||
		!strings.Contains(err.Error(), "只能更新账号存储中的离线账号") {
		t.Fatalf("错误 = %v，期望类型不匹配提示", err)
	}
}

// TestRemoveLastAccountKeepsEmptyList 删除最后一个账号后列表必须保持为空。
// 防的回归：删空后自动补回默认账号，用户看到「删了又出现」，以为删除失败。
func TestRemoveLastAccountKeepsEmptyList(t *testing.T) {
	useTempAuthStorage(t)
	service := NewAccountStoreService(false)
	account := &LaunchAccount{Type: "offline", DisplayName: "Steve", OfflineName: "Steve", OfflineSkinId: "steve"}
	service.Add(account)
	service.Remove(account)
	if got := service.Current(); len(got) != 0 {
		t.Fatalf("删除最后一个账号后列表 = %+v，期望空列表", got)
	}
	if service.Selected() != nil {
		t.Fatal("空列表时 Selected() 应为 nil")
	}

	// 删除后重新加载仍然是空列表（而不是「全新安装」的默认账号）
	reloaded := NewAccountStoreService(true)
	if got := reloaded.Current(); len(got) != 0 {
		t.Fatalf("重新加载后列表 = %+v，期望保持空列表", got)
	}

	// 同名离线账号查重忽略大小写
	service.Add(&LaunchAccount{Type: "offline", DisplayName: "Alex", OfflineName: "Alex", OfflineSkinId: "steve"})
	if !service.HasOfflineName("alex") {
		t.Fatal("HasOfflineName 应当忽略大小写")
	}
	if service.HasOfflineName("Steve") {
		t.Fatal("已删除的账号不应被查到")
	}
}

// TestMoveToTopSelectAndOnChanged 排序/切换当前账号与变更通知。
// 防的回归：切换账号后没有移到列表首项（Selected 取的是首项，前端显示的还是旧账号），
// 以及已在首项时重复触发 OnChanged（列表被反复重渲染/闪烁）。
func TestMoveToTopSelectAndOnChanged(t *testing.T) {
	useTempAuthStorage(t)
	service := NewAccountStoreService(false)
	changes := 0
	service.OnChanged = func() { changes++ }

	first := &LaunchAccount{Type: "offline", DisplayName: "A", OfflineName: "a", OfflineSkinId: "steve"}
	second := &LaunchAccount{Type: "offline", DisplayName: "B", OfflineName: "b", OfflineSkinId: "steve"}
	service.Add(first)  // +1
	service.Add(second) // +1
	if changes != 2 {
		t.Fatalf("新增两个账号后通知次数 = %d，期望 2", changes)
	}
	if service.Selected() != second {
		t.Fatal("新增的账号应位于列表首项（= 当前账号）")
	}

	service.MoveToTop(first) // +1
	if changes != 3 {
		t.Fatalf("移动后通知次数 = %d，期望 3", changes)
	}
	if service.Selected() != first {
		t.Fatal("移动后首项应为 first")
	}

	// 已经在首项：不应再触发通知
	service.MoveToTop(first)
	service.MoveToTop(&LaunchAccount{Type: "offline", OfflineName: "a"}) // 同键、不同指针
	if changes != 3 {
		t.Fatalf("已在首项时不应触发通知，通知次数 = %d", changes)
	}

	// 按稳定键选中（大小写不敏感）
	if !service.SelectByStableKey("OFFLINE:B") {
		t.Fatal("按稳定键选中应当成功")
	}
	if service.Selected() != second {
		t.Fatal("选中后首项应为 second")
	}
	for _, key := range []string{"", "   ", "offline:不存在", "unknown:key"} {
		if service.SelectByStableKey(key) {
			t.Fatalf("SelectByStableKey(%q) 应当失败", key)
		}
	}

	// 移除不存在的账号不应改变列表
	before := len(service.Current())
	service.Remove(&LaunchAccount{Type: "offline", OfflineName: "ghost"})
	if got := len(service.Current()); got != before {
		t.Fatalf("移除不存在的账号后列表长度 = %d，期望 %d", got, before)
	}
	if service.FindByStableKey("") != nil {
		t.Fatal("空稳定键不应查到任何账号")
	}
}

// TestRaiseChangedRecoversPanic 订阅者 panic 必须被隔离。
// 防的回归：某个页面（订阅者）抛异常后中断账号保存/其它页面的刷新。
func TestRaiseChangedRecoversPanic(t *testing.T) {
	useTempAuthStorage(t)
	service := NewAccountStoreService(false)
	service.OnChanged = func() { panic("订阅者炸了") }

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("OnChanged 的 panic 不应外泄：%v", recovered)
			}
		}()
		service.Add(&LaunchAccount{Type: "offline", DisplayName: "A", OfflineName: "a", OfflineSkinId: "steve"})
	}()

	if got := len(service.Current()); got != 1 {
		t.Fatalf("订阅者 panic 后账号仍应入库，实际列表长度 %d", got)
	}
}

// TestBuildAccountsDropsIncompleteEntries DTO → 账号对象的转换必须丢弃凭据缺失的条目，
// 并补齐默认值。
// 防的回归：缺字段的条目被建出来，启动时选中它 → 拿空令牌启动游戏（进游戏即掉线）。
func TestBuildAccountsDropsIncompleteEntries(t *testing.T) {
	service := NewAccountStoreService(false)
	expiresAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	dtos := []accountDto{
		{Type: "microsoft", Username: "NyaPlayer", Uuid: "uuid-1", AccessToken: "a",
			RefreshToken: "r", XboxUserId: "x", ClientId: "c", ExpiresAt: &expiresAt},
		{Type: "microsoft", Uuid: "uuid-2"},                                            // 缺 Username
		{Type: "authlib", Uuid: "p1", ApiRoot: "https://s.example", Username: "login"}, // 缺 ProfileName
		{Type: "authlib", Uuid: "p2"},                                                  // 缺 ApiRoot
		{Type: "offline", OfflineName: "Player"},                                       // 缺皮肤
		{Type: "offline"},                                                              // 缺名字
		{Type: "thirdparty", Username: "someone"},                                      // 未知类型
	}

	accounts := service.buildAccounts(dtos)
	if len(accounts) != 3 {
		t.Fatalf("转换结果 = %d 条，期望 3 条（丢弃 4 条不完整数据）：%+v", len(accounts), accounts)
	}

	microsoft := accounts[0]
	if microsoft.Type != "microsoft" || microsoft.DisplayName != "NyaPlayer" {
		t.Fatalf("正版账号 = %+v", microsoft)
	}
	if microsoft.Microsoft.AccessToken != "a" || microsoft.Microsoft.RefreshToken != "r" ||
		microsoft.Microsoft.XboxUserId != "x" || microsoft.Microsoft.ClientId != "c" {
		t.Fatalf("正版凭据 = %+v", microsoft.Microsoft)
	}
	if !microsoft.Microsoft.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("ExpiresAt = %v，期望 %v", microsoft.Microsoft.ExpiresAt, expiresAt)
	}

	authlib := accounts[1]
	if authlib.DisplayName != "login" || authlib.Authlib.ProfileName != "" {
		t.Fatalf("皮肤站账号显示名应回落登录名：%+v", authlib)
	}

	offline := accounts[2]
	if offline.DisplayName != "Player" || offline.OfflineName != "Player" ||
		offline.OfflineSkinId != "steve" {
		t.Fatalf("离线账号应补默认皮肤 steve：%+v", offline)
	}

	if got := service.buildAccounts(nil); len(got) != 0 {
		t.Fatalf("空输入应当返回空列表，实际 %+v", got)
	}
}

// TestLoadFromDiskBranches accounts 配置的加载分支：全新安装给默认离线账号、
// 显式空数组保持空、配置损坏回落默认、解密失败则失败关闭（不回落默认账号）。
// 防的回归：解密失败时悄悄补一个默认账号掩盖问题——用户的账号其实已经没了，
// 却以为「只是换了个账号」。
func TestLoadFromDiskBranches(t *testing.T) {
	t.Run("全新安装给默认离线账号", func(t *testing.T) {
		useTempAuthStorage(t)
		accounts := NewAccountStoreService(true).Current()
		if len(accounts) != 1 || accounts[0].Type != "offline" ||
			accounts[0].OfflineName != "Player_01" || accounts[0].OfflineSkinId != "steve" {
			t.Fatalf("默认账号 = %+v，期望 Player_01/steve", accounts)
		}
	})

	t.Run("显式空数组保持空列表", func(t *testing.T) {
		useTempAuthStorage(t)
		if !config.SetValue(AccountsConfigKey, "[]") {
			t.Fatal("写入空账号列表失败")
		}
		if accounts := NewAccountStoreService(true).Current(); len(accounts) != 0 {
			t.Fatalf("空数组应当加载为空列表，实际 %+v", accounts)
		}
	})

	t.Run("配置损坏回落默认账号", func(t *testing.T) {
		useTempAuthStorage(t)
		if !config.SetValue(AccountsConfigKey, "{不是 JSON") {
			t.Fatal("写入损坏配置失败")
		}
		accounts := NewAccountStoreService(true).Current()
		if len(accounts) != 1 || accounts[0].OfflineName != "Player_01" {
			t.Fatalf("配置损坏时应回落默认账号，实际 %+v", accounts)
		}
	})

	t.Run("解密失败时不回落默认账号", func(t *testing.T) {
		useTempAuthStorage(t)
		if !config.SetValue(AccountsConfigKey, EncryptedPrefix+"!!!not-base64!!!") {
			t.Fatal("写入不可解密的账号数据失败")
		}
		if accounts := NewAccountStoreService(true).Current(); len(accounts) != 0 {
			t.Fatalf("解密失败时应当返回空列表（用户需重新登录），实际 %+v", accounts)
		}
	})
}

// TestSaveAndReloadRoundTrip 账号保存 → 重新加载的完整往返，覆盖三种账号类型。
// 防的回归：DTO 字段名/加密前缀不匹配导致重启后账号消失（真实事故：
// DPAPI 分支漏了 nyaenc1: 前缀，accounts.yaml 里全是读不出来的裸 blob）。
func TestSaveAndReloadRoundTrip(t *testing.T) {
	directory := useTempAuthStorage(t)
	service := NewAccountStoreService(false)
	expiresAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)

	service.Add(&LaunchAccount{
		Type: "microsoft", DisplayName: "NyaPlayer",
		Microsoft: &MicrosoftAccount{
			Username: "NyaPlayer", Uuid: "uuid-1", AccessToken: "mc-access",
			RefreshToken: "ms-refresh", XboxUserId: "1234567890123456",
			ClientId: "client-1", ExpiresAt: expiresAt,
		},
	})
	service.Add(&LaunchAccount{
		Type: "authlib", DisplayName: "皮肤站角色",
		Authlib: &AuthlibCredential{
			Username: "nya@example.com", ProfileName: "皮肤站角色", ProfileUuid: "uuid-2",
			AccessToken: "ygg-token", ApiRoot: "https://skin.example/api", ServerName: "小猫皮肤站",
		},
	})
	service.Add(&LaunchAccount{
		Type: "offline", DisplayName: "Steve", OfflineName: "Steve", OfflineSkinId: "alex",
	})

	stored := config.GetValue(AccountsConfigKey)
	if stored == "" {
		t.Fatal("accounts 配置应当已写入临时存储目录")
	}
	if _, err := json.Marshal(stored); err != nil {
		t.Fatalf("配置值不可序列化：%v", err)
	}
	if strings.Contains(stored, "ms-refresh") || strings.Contains(stored, "ygg-token") {
		t.Fatalf("账号令牌不应以明文落盘：%q", stored)
	}

	reloaded := NewAccountStoreService(true).Current()
	if len(reloaded) != 3 {
		t.Fatalf("重新加载得到 %d 条账号，期望 3：%+v", len(reloaded), reloaded)
	}

	// Add 是头插，所以重新加载后的顺序是 offline / authlib / microsoft
	offline := reloaded[0]
	if offline.Type != "offline" || offline.OfflineName != "Steve" || offline.OfflineSkinId != "alex" {
		t.Fatalf("离线账号往返结果 = %+v", offline)
	}
	authlib := reloaded[1]
	if authlib.Type != "authlib" || authlib.DisplayName != "皮肤站角色" ||
		authlib.Authlib.ProfileUuid != "uuid-2" || authlib.Authlib.AccessToken != "ygg-token" ||
		authlib.Authlib.ApiRoot != "https://skin.example/api" ||
		authlib.Authlib.ServerName != "小猫皮肤站" {
		t.Fatalf("皮肤站账号往返结果 = %+v / %+v", authlib, authlib.Authlib)
	}
	microsoft := reloaded[2]
	if microsoft.Type != "microsoft" || microsoft.DisplayName != "NyaPlayer" ||
		microsoft.Microsoft.Uuid != "uuid-1" || microsoft.Microsoft.AccessToken != "mc-access" ||
		microsoft.Microsoft.RefreshToken != "ms-refresh" ||
		microsoft.Microsoft.XboxUserId != "1234567890123456" ||
		microsoft.Microsoft.ClientId != "client-1" {
		t.Fatalf("正版账号往返结果 = %+v / %+v", microsoft, microsoft.Microsoft)
	}
	if !microsoft.Microsoft.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("ExpiresAt = %v，期望 %v", microsoft.Microsoft.ExpiresAt, expiresAt)
	}
	if directory == "" {
		t.Fatal("临时存储目录不应为空")
	}
}

// TestSaveSkipsAccountsWithMissingCredentials 内存中凭据对象缺失的账号不落盘。
// 防的回归：把「没有凭据的账号」降级成错误类型写进配置，污染 accounts.yaml
// 并在下次启动时变成一个无用的空账号。
func TestSaveSkipsAccountsWithMissingCredentials(t *testing.T) {
	useTempAuthStorage(t)
	service := NewAccountStoreService(false)
	service.Add(&LaunchAccount{Type: "microsoft", DisplayName: "缺凭据"})
	service.Add(&LaunchAccount{Type: "authlib", DisplayName: "缺凭据"})
	service.Add(&LaunchAccount{Type: "offline", DisplayName: "缺名字", OfflineName: "   "})

	stored := config.GetValue(AccountsConfigKey)
	if stored == "" {
		t.Fatal("Save 应当写入 accounts 配置")
	}
	if strings.Contains(stored, "缺凭据") || strings.Contains(stored, "缺名字") {
		// 密文里不该出现明文；若出现说明落盘的是未加密内容
		t.Fatalf("accounts 配置里出现了明文字段：%q", stored)
	}

	reloaded := NewAccountStoreService(true).Current()
	if len(reloaded) != 0 {
		t.Fatalf("凭据缺失的账号不应落盘，重新加载得到 %+v", reloaded)
	}
	if len(service.Current()) != 3 {
		t.Fatalf("Save 不应改变内存中的列表，实际 %d 条", len(service.Current()))
	}
}

// TestWithRefreshLockSerializesSameStableKey 同一账号的刷新操作必须串行化，
// 不同账号之间不互相阻塞。
// 防的回归：微软 OAuth 轮换策略下同一 refresh_token 被并发使用两次，
// 第二次必得 invalid_grant，账号被强制下线；以及锁粒度写成全局锁，
// 一个账号刷新卡住时所有账号的刷新一起等待。
func TestWithRefreshLockSerializesSameStableKey(t *testing.T) {
	useTempAuthStorage(t)
	service := NewAccountStoreService(false)
	account := &LaunchAccount{Type: "microsoft", DisplayName: "NyaPlayer",
		Microsoft: &MicrosoftAccount{Uuid: "uuid-1"}}

	var running int32
	var overlaps int32
	var waitGroup sync.WaitGroup
	for index := 0; index < 8; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if err := service.WithRefreshLock(account, func() error {
				if atomic.AddInt32(&running, 1) > 1 {
					atomic.AddInt32(&overlaps, 1)
				}
				time.Sleep(2 * time.Millisecond)
				atomic.AddInt32(&running, -1)
				return nil
			}); err != nil {
				t.Errorf("WithRefreshLock 返回错误：%v", err)
			}
		}()
	}
	waitGroup.Wait()
	if overlaps != 0 {
		t.Fatalf("同一稳定键的回调发生了 %d 次并发重叠（会导致 refresh_token 并发使用）", overlaps)
	}

	// 不同账号之间不应互相阻塞
	other := &LaunchAccount{Type: "microsoft", DisplayName: "Other",
		Microsoft: &MicrosoftAccount{Uuid: "uuid-2"}}
	holding := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = service.WithRefreshLock(account, func() error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding
	acquired := make(chan struct{})
	go func() {
		_ = service.WithRefreshLock(other, func() error {
			close(acquired)
			return nil
		})
	}()
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("不同账号的刷新锁不应互相阻塞")
	}
	close(release)

	// 回调错误必须原样返回
	sentinel := errSentinel{}
	if err := service.WithRefreshLock(account, func() error { return sentinel }); err != sentinel {
		t.Fatalf("WithRefreshLock 错误 = %v，期望原样返回回调错误", err)
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "哨兵错误" }

// TestWithRefreshLockPanicsOnNilArgs 空参数必须 panic（包内约定），
// 而不是静默地不执行回调——静默跳过会让调用方以为凭据已经刷新。
// 防的回归：nil 检查被删掉后刷新逻辑被悄悄跳过。
func TestWithRefreshLockPanicsOnNilArgs(t *testing.T) {
	service := NewAccountStoreService(false)
	account := &LaunchAccount{Type: "offline", OfflineName: "Steve"}

	for _, testCase := range []struct {
		name   string
		action func()
	}{
		{name: "账号为 nil", action: func() { _ = service.WithRefreshLock(nil, func() error { return nil }) }},
		{name: "回调为 nil", action: func() { _ = service.WithRefreshLock(account, nil) }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("应当 panic")
				}
			}()
			testCase.action()
		})
	}
}
