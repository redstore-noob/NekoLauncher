package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/logs"
)

// accountDto 账号持久化 DTO；JSON 字段名与 C# 默认序列化名（PascalCase）一致，
// 序列化后作为字符串存进 accounts.yaml。
type accountDto struct {
	Type          string     `json:"Type,omitempty"`
	Username      string     `json:"Username,omitempty"`
	Uuid          string     `json:"Uuid,omitempty"`
	AccessToken   string     `json:"AccessToken,omitempty"`
	RefreshToken  string     `json:"RefreshToken,omitempty"`
	XboxUserId    string     `json:"XboxUserId,omitempty"`
	ClientId      string     `json:"ClientId,omitempty"`
	ExpiresAt     *time.Time `json:"ExpiresAt,omitempty"`
	ProfileName   string     `json:"ProfileName,omitempty"`
	ApiRoot       string     `json:"ApiRoot,omitempty"`
	ServerName    string     `json:"ServerName,omitempty"`
	OfflineName   string     `json:"OfflineName,omitempty"`
	OfflineSkinId string     `json:"OfflineSkinId,omitempty"`
}

// AccountsConfigKey 账号列表在 accounts.yaml 中的键。
const AccountsConfigKey = "accounts"

// AccountStoreService 账号存储的可实例化实现：所有页面共享 AccountStore 门面持有的
// 默认实例；需要隔离状态的场景（单元测试、多存储目录）可直接构造本类。
type AccountStoreService struct {
	gate sync.RWMutex
	// current 内存中的权威账号列表（对应 C# ObservableCollection）。
	current []*LaunchAccount
	// refreshGates 以账号稳定键为粒度的刷新锁。
	refreshGates sync.Map
	// loadOnDemand 首次访问时才从 accounts.yaml 加载（见 ensureLoaded）。
	loadOnDemand bool
	// loaded 是否已经尝试过加载（含"确实没有配置"的情况）。
	loaded bool
	// OnChanged 账号列表发生变化（增/删/排序）时触发。
	// C# 为多订阅者事件，Go 移植为单回调字段（语义偏离见 PORTING_NOTES.md）。
	OnChanged func()
}

// NewAccountStoreService 构造账号存储。
//
// loadFromDisk 为 true 时**首次访问**才加载 accounts.yaml，而不是在构造时立刻加载：
// 包级单例 `Shared` 是在 import 阶段构造的，若那时就读盘，任何引入本包的测试
// （甚至 `go test ./internal/auth/` 自己）都会在 TestMain 之前读一遍用户真实的
// accounts.yaml，并因此往真实日志目录写文件——测试隔离无从下手。
func NewAccountStoreService(loadFromDisk bool) *AccountStoreService {
	return &AccountStoreService{loadOnDemand: loadFromDisk}
}

// ensureLoaded 按需加载一次；已加载或本实例不要求加载时直接返回。
func (s *AccountStoreService) ensureLoaded() {
	s.gate.Lock()
	if s.loaded || !s.loadOnDemand {
		s.gate.Unlock()

		return
	}
	// 先置位再加载：loadFromDisk 内部会读 gate（同 goroutine 重入 RWMutex 会死锁），
	// 而且并发首次访问时也只能有一个执行加载。
	s.loaded = true
	loaded := s.loadFromDisk()
	s.current = loaded
	s.gate.Unlock()
}

// Selected 列表首项是所有页面和组件共享的当前账号。
func (s *AccountStoreService) Selected() *LaunchAccount {
	s.ensureLoaded()

	s.gate.RLock()
	defer s.gate.RUnlock()
	if len(s.current) == 0 {
		return nil
	}
	return s.current[0]
}

// Current 返回账号列表快照。
func (s *AccountStoreService) Current() []*LaunchAccount {
	s.ensureLoaded()

	s.gate.RLock()
	defer s.gate.RUnlock()
	out := make([]*LaunchAccount, len(s.current))
	copy(out, s.current)
	return out
}

// sameIdentity 判断两个账号是否同一身份（按持久身份键比较）。
// Wails 会把前端传入的对象 JSON 解码成新的结构体，引用相等永远不成立，
// 因此这里必须用稳定键而不是指针比较。
func (s *AccountStoreService) sameIdentity(a, b *LaunchAccount) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.EqualFold(s.GetStableKey(a), s.GetStableKey(b))
}

// contains 判断列表中是否存在同一账号（按持久身份键比较）。
func (s *AccountStoreService) contains(account *LaunchAccount) bool {
	for _, candidate := range s.current {
		if s.sameIdentity(candidate, account) {
			return true
		}
	}
	return false
}

// Add 新增账号并置于列表首项。
func (s *AccountStoreService) Add(account *LaunchAccount) {
	s.ensureLoaded()
	if account == nil {
		panic("account 不能为空")
	}
	s.gate.Lock()
	s.current = append([]*LaunchAccount{account}, s.current...)
	s.gate.Unlock()
	s.Save()
	s.raiseChanged()
}

// Remove 移除账号。注意：删除后允许列表为空，不再自动补充默认账号，
// 否则用户删除最后一个账号时"删了又出现"，看起来像删除失败。
func (s *AccountStoreService) Remove(account *LaunchAccount) {
	s.ensureLoaded()
	s.gate.Lock()
	for index, candidate := range s.current {
		if s.sameIdentity(candidate, account) {
			s.current = append(s.current[:index], s.current[index+1:]...)
			break
		}
	}
	s.gate.Unlock()
	s.Save()
	s.raiseChanged()
}

// MoveToTop 把指定账号设为默认（移到列表顶部）。
func (s *AccountStoreService) MoveToTop(account *LaunchAccount) {
	s.ensureLoaded()
	if account == nil {
		panic("account 不能为空")
	}
	s.gate.Lock()
	if len(s.current) > 0 && s.sameIdentity(s.current[0], account) {
		s.gate.Unlock()
		return
	}
	var matched *LaunchAccount
	for index, candidate := range s.current {
		if s.sameIdentity(candidate, account) {
			matched = candidate
			s.current = append(s.current[:index], s.current[index+1:]...)
			break
		}
	}
	if matched != nil {
		s.current = append([]*LaunchAccount{matched}, s.current...)
	}
	s.gate.Unlock()
	if matched != nil {
		s.Save()
		s.raiseChanged()
	}
}

// GetStableKey 账号的持久身份键。
func (s *AccountStoreService) GetStableKey(account *LaunchAccount) string {
	if account == nil {
		panic("account 不能为空")
	}
	var identity string
	switch account.Type {
	case "microsoft":
		if account.Microsoft != nil {
			identity = account.Microsoft.Uuid
		}
	case "authlib":
		// 皮肤站身份含 API 根：同一角色 UUID 在不同皮肤站是不同账号
		if account.Authlib != nil {
			identity = fmt.Sprintf("%s@%s", account.Authlib.ProfileUuid,
				strings.TrimSuffix(account.Authlib.ApiRoot, "/"))
		}
	case "offline":
		identity = account.OfflineName
	default:
		identity = account.DisplayName
	}
	if strings.TrimSpace(identity) == "" {
		identity = account.DisplayName
	}
	return account.Type + ":" + identity
}

// SelectByStableKey 通过持久身份切换当前账号；成功后该账号会移动到列表首项。
func (s *AccountStoreService) SelectByStableKey(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	account := s.FindByStableKey(key)
	if account == nil {
		return false
	}
	s.MoveToTop(account)
	return true
}

// FindByStableKey 通过持久身份查找账号（忽略大小写）。
func (s *AccountStoreService) FindByStableKey(key string) *LaunchAccount {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	for _, candidate := range s.Current() {
		if strings.EqualFold(s.GetStableKey(candidate), key) {
			return candidate
		}
	}
	return nil
}

// Reload 在配置存储目录切换后，从新的 accounts.yaml 重新载入账号。
func (s *AccountStoreService) Reload() {
	loaded := s.loadFromDisk()
	s.gate.Lock()
	s.current = loaded
	s.gate.Unlock()
	s.raiseChanged()
}

// UpdateMicrosoftAccount 更新正版账号凭据（经由存储以保证落盘与 Changed 通知）。
// 账号已被删除时返回错误而不是 panic：调用方多在启动/刷新 goroutine 上，
// 用户在网络往返期间删掉账号不该把整个应用带走。
func (s *AccountStoreService) UpdateMicrosoftAccount(account *LaunchAccount, microsoft *MicrosoftAccount) error {
	s.ensureLoaded()
	if account == nil || microsoft == nil {
		return errors.New("账号与凭据不能为空")
	}
	s.gate.Lock()
	if !s.contains(account) {
		s.gate.Unlock()

		return errors.New("账号已不存在，可能刚被删除")
	}
	if !strings.EqualFold(account.Type, "microsoft") {
		s.gate.Unlock()

		return errors.New("只能更新账号存储中的正版账号")
	}
	account.Microsoft = microsoft
	s.gate.Unlock()
	s.Save()
	s.raiseChanged()

	return nil
}

// UpdateAuthlibAccount 更新皮肤站账号凭据；显示名跟随角色名，
// 保证重新登录换角色后列表同步更新。
func (s *AccountStoreService) UpdateAuthlibAccount(account *LaunchAccount, credential *AuthlibCredential) error {
	s.ensureLoaded()
	if account == nil || credential == nil {
		return errors.New("账号与凭据不能为空")
	}
	s.gate.Lock()
	if !s.contains(account) {
		s.gate.Unlock()

		return errors.New("账号已不存在，可能刚被删除")
	}
	if !strings.EqualFold(account.Type, "authlib") {
		s.gate.Unlock()

		return errors.New("只能更新账号存储中的皮肤站账号")
	}
	account.Authlib = credential
	if strings.TrimSpace(credential.ProfileName) != "" {
		account.DisplayName = credential.ProfileName
	}
	s.gate.Unlock()
	s.Save()
	s.raiseChanged()

	return nil
}

// UpdateOfflineSkin 更新离线账号皮肤。
func (s *AccountStoreService) UpdateOfflineSkin(account *LaunchAccount, skinID string) error {
	s.ensureLoaded()
	if account == nil || strings.TrimSpace(skinID) == "" {
		return errors.New("账号与皮肤不能为空")
	}
	s.gate.Lock()
	if !s.contains(account) {
		s.gate.Unlock()

		return errors.New("账号已不存在，可能刚被删除")
	}
	if !strings.EqualFold(account.Type, "offline") {
		s.gate.Unlock()

		return errors.New("只能更新账号存储中的离线账号")
	}
	account.OfflineSkinId = skinID
	s.gate.Unlock()
	s.Save()
	s.raiseChanged()

	return nil
}

// HasOfflineName 是否已存在同名离线账号（忽略大小写）。
func (s *AccountStoreService) HasOfflineName(name string) bool {
	s.ensureLoaded()
	for _, candidate := range s.Current() {
		if candidate.Type == "offline" &&
			strings.EqualFold(candidate.OfflineName, name) {
			return true
		}
	}
	return false
}

// WithRefreshLock 以账号稳定键为粒度串行化「刷新凭据」操作。微软 OAuth 轮换策略下，
// 同一 refresh_token 被并发使用两次时第二次必得 invalid_grant（账号被强制下线）。
// 启动校验与皮肤/档案服务等不同组件都会触发刷新，必须使用同一实例的这把锁；
// 进入锁后回调内应重新读取 account.Microsoft 判断过期，
// 因为等待锁的期间可能已有并发的刷新完成并写入了轮换后的令牌。
func (s *AccountStoreService) WithRefreshLock(account *LaunchAccount, action func() error) error {
	s.ensureLoaded()
	if account == nil || action == nil {
		panic("参数不能为空")
	}
	key := s.GetStableKey(account)
	gateAny, _ := s.refreshGates.LoadOrStore(key, &sync.Mutex{})
	gate := gateAny.(*sync.Mutex)
	gate.Lock()
	defer gate.Unlock()
	return action()
}

// Save 把当前列表写回 accounts.yaml。
//
// DTO 快照在 gate 读锁内构建：Update* 系列是在写锁内原地改 account 的字段，
// 不持锁读会和并发更新交错，落盘的可能是半新半旧的凭据——微软的
// refresh_token 轮换策略下，写回旧令牌会让账号被强制下线。
// 注意：加锁期间不能调用 Current()（它自己会 RLock），所以先取列表、
// 持锁构建完 DTO 再落盘。
func (s *AccountStoreService) Save() {
	s.ensureLoaded()
	snapshot := s.Current()
	dtos := make([]accountDto, 0, len(snapshot))

	s.gate.RLock()
	for _, account := range snapshot {
		switch {
		case account.Type == "microsoft" && account.Microsoft != nil:
			ms := account.Microsoft
			expiresAt := ms.ExpiresAt
			dtos = append(dtos, accountDto{
				Type:         "microsoft",
				Username:     ms.Username,
				Uuid:         ms.Uuid,
				AccessToken:  ms.AccessToken,
				RefreshToken: ms.RefreshToken,
				XboxUserId:   ms.XboxUserId,
				ClientId:     ms.ClientId,
				ExpiresAt:    &expiresAt,
			})
		case account.Type == "authlib" && account.Authlib != nil:
			authlib := account.Authlib
			dtos = append(dtos, accountDto{
				Type:        "authlib",
				Username:    authlib.Username,
				ProfileName: authlib.ProfileName,
				Uuid:        authlib.ProfileUuid,
				AccessToken: authlib.AccessToken,
				ApiRoot:     authlib.ApiRoot,
				ServerName:  authlib.ServerName,
			})
		case account.Type == "offline" && account.OfflineName != "":
			dtos = append(dtos, accountDto{
				Type:          "offline",
				OfflineName:   account.OfflineName,
				OfflineSkinId: account.OfflineSkinId,
			})
		default:
			// 凭据对象缺失的账号（内存中的异常状态）不落盘：
			// 宁可丢掉这条，也不降级成错误类型污染配置
		}
	}
	s.gate.RUnlock()

	data, err := json.Marshal(dtos)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return
	}
	// Windows 上以 DPAPI 加密落盘；加密失败（密钥文件不可写等）回落明文——
	// 宁可明文也不能丢账号。但必须记 ERROR：明文里是刷新令牌，
	// 用户至少要在启动日志里能看到这件事。
	stored := Protect(string(data))
	if stored == "" {
		logs.Write("ERROR", "账号加密不可用，已按明文写入 accounts.yaml；"+
			"请检查存储目录权限与 account.secret.key（Windows 为 DPAPI 可用性）后重新登录账号。")
		stored = string(data)
	}
	// 写盘失败必须喊出来：磁盘满/文件被占用时内存与磁盘从此分叉——
	// 界面照常显示"添加成功"，重启后账号消失（或删除的账号复活）。
	if !config.SetValue(AccountsConfigKey, stored) {
		logs.Write("ERROR", "账号数据写入磁盘失败，本次变更在重启后会丢失；请检查磁盘空间与存储目录权限")
	}
}

// raiseChanged 触发 OnChanged 回调；回调异常通过 recover 隔离，
// 防止一个页面出错中断其它页面。
func (s *AccountStoreService) raiseChanged() {
	handler := s.OnChanged
	if handler == nil {
		return
	}
	func() {
		defer func() {
			if err := recover(); err != nil {
				logs.Write("ERROR", fmt.Sprintf("AccountStore OnChanged 订阅者异常：%v", err))
			}
		}()
		handler()
	}()
}

// loadFromDisk 从 accounts.yaml 加载账号列表。
func (s *AccountStoreService) loadFromDisk() []*LaunchAccount {
	accounts := make([]*LaunchAccount, 0)
	stored := config.GetValue(AccountsConfigKey)
	jsonBody := stored
	if strings.HasPrefix(stored, EncryptedPrefix) {
		jsonBody = Unprotect(stored)
		if jsonBody == "" {
			// 解密失败（换机/换 Windows 用户等）：凭据已不可恢复。
			// 直接返回空列表，让用户重新登录；不回落默认账号，避免掩盖问题。
			logs.Write("ERROR", "账号数据解密失败，已跳过加载，需要重新登录账号")
			return accounts
		}
	} else if legacy := Unprotect(stored); legacy != "" {
		// 旧格式（没有 nyaenc1: 前缀的裸密文）：解出来继续用，下次保存会自动写成新格式。
		// 少了这一条，老用户升级后账号会静默消失（见 Unprotect 的注释）。
		logs.Write("INFO", "检测到旧格式账号数据，已解密加载；下次保存会写入新格式")
		jsonBody = legacy
	}

	// 只要保存过 accounts（包括空数组 []），就按内容加载，保持用户的选择（允许空列表）。
	if strings.TrimSpace(jsonBody) != "" {
		var dtos []accountDto
		if err := json.Unmarshal([]byte(jsonBody), &dtos); err == nil {
			return s.buildAccounts(dtos)
		}
		// 值层损坏（能读到内容但解析失败）：备份原始值供人工恢复，按空列表
		// 处理而不是假装全新安装——此前静默回落默认离线账号 Player_01，
		// 用户的所有账号"凭空消失"且毫无提示，后续 Save 还会覆盖掉损坏
		// 原值，彻底无法恢复。备份的是加密原文（stored），不落明文凭据。
		backupKey := AccountsConfigKey + ".corrupted"
		if backupErr := config.SetValue(backupKey, stored); backupErr {
			logs.Write("ERROR", "账号数据损坏（无法解析），原值已备份到配置键 "+backupKey+
				"；本次按空账号列表启动，需要重新登录账号。可凭备份尝试恢复。")
		} else {
			logs.Write("ERROR", "账号数据损坏（无法解析），且备份写入失败；本次按空账号列表启动，需要重新登录账号。")
		}
		return accounts
	}

	// 全新安装（从未保存过账号）：提供一个默认离线账号，保证首次打开即可启动。
	accounts = append(accounts, &LaunchAccount{
		Type:          "offline",
		DisplayName:   "Player_01",
		OfflineName:   "Player_01",
		OfflineSkinId: "steve",
	})
	return accounts
}

// buildAccounts 把 DTO 列表转换为账号对象；凭据字段缺失的条目按对应类型的要求丢弃。
// 抽取自 loadFromDisk 主体，便于单独测试。
func (s *AccountStoreService) buildAccounts(dtos []accountDto) []*LaunchAccount {
	accounts := make([]*LaunchAccount, 0, len(dtos))
	for index := range dtos {
		dto := &dtos[index]
		switch {
		case dto.Type == "microsoft" && strings.TrimSpace(dto.Username) != "":
			expiresAt := time.Time{}
			if dto.ExpiresAt != nil {
				expiresAt = *dto.ExpiresAt
			}
			accounts = append(accounts, &LaunchAccount{
				Type:        "microsoft",
				DisplayName: dto.Username,
				Microsoft: &MicrosoftAccount{
					Username:     dto.Username,
					Uuid:         dto.Uuid,
					AccessToken:  dto.AccessToken,
					RefreshToken: dto.RefreshToken,
					XboxUserId:   dto.XboxUserId,
					ClientId:     dto.ClientId,
					ExpiresAt:    expiresAt,
				},
			})
		case dto.Type == "authlib" &&
			strings.TrimSpace(dto.Uuid) != "" &&
			strings.TrimSpace(dto.ApiRoot) != "":
			displayName := dto.ProfileName
			if displayName == "" {
				displayName = dto.Username
			}
			accounts = append(accounts, &LaunchAccount{
				Type:        "authlib",
				DisplayName: displayName,
				Authlib: &AuthlibCredential{
					Username:    dto.Username,
					ProfileName: dto.ProfileName,
					ProfileUuid: dto.Uuid,
					AccessToken: dto.AccessToken,
					ApiRoot:     dto.ApiRoot,
					ServerName:  dto.ServerName,
				},
			})
		case dto.Type == "offline" && strings.TrimSpace(dto.OfflineName) != "":
			skinId := dto.OfflineSkinId
			if strings.TrimSpace(skinId) == "" {
				skinId = "steve"
			}
			accounts = append(accounts, &LaunchAccount{
				Type:          "offline",
				DisplayName:   dto.OfflineName,
				OfflineName:   dto.OfflineName,
				OfflineSkinId: skinId,
			})
		}
	}
	return accounts
}

// ---------------------------------------------------------------------------
// AccountStore 静态门面：Shared 是全进程共享的默认实例。
// ---------------------------------------------------------------------------

// Shared 全进程共享的默认实例；首个访问者触发一次从 accounts.yaml 的加载。
var Shared = NewAccountStoreService(true)

// AccountStore 门面函数集（对应 C# 静态类 AccountStore），
// 语义与 Shared 实例上的同名方法一致。

// AccountStoreCurrent 全部账号。
func AccountStoreCurrent() []*LaunchAccount { return Shared.Current() }

// AccountStoreSelected 当前选中账号（无则 nil）。
func AccountStoreSelected() *LaunchAccount { return Shared.Selected() }

// AccountStoreAdd 新增账号并置于列表首项。
func AccountStoreAdd(account *LaunchAccount) { Shared.Add(account) }

// AccountStoreRemove 移除账号。
func AccountStoreRemove(account *LaunchAccount) { Shared.Remove(account) }

// AccountStoreMoveToTop 把指定账号设为默认（移到列表顶部）。
func AccountStoreMoveToTop(account *LaunchAccount) { Shared.MoveToTop(account) }

// AccountStoreGetStableKey 账号的持久身份键。
func AccountStoreGetStableKey(a *LaunchAccount) string { return Shared.GetStableKey(a) }

// AccountStoreSelectByStableKey 通过持久身份切换当前账号。
func AccountStoreSelectByStableKey(key string) bool { return Shared.SelectByStableKey(key) }

// AccountStoreFindByStableKey 通过持久身份查找账号。
func AccountStoreFindByStableKey(key string) *LaunchAccount { return Shared.FindByStableKey(key) }

// AccountStoreReload 从 accounts.yaml 重新载入账号。
func AccountStoreReload() { Shared.Reload() }

// AccountStoreSave 把当前列表写回 accounts.yaml。
func AccountStoreSave() { Shared.Save() }

// AccountStoreHasOfflineName 是否已存在同名离线账号。
func AccountStoreHasOfflineName(name string) bool { return Shared.HasOfflineName(name) }

// AccountStoreUpdateMicrosoftAccount 更新正版账号凭据并持久化。
func AccountStoreUpdateMicrosoftAccount(account *LaunchAccount, ms *MicrosoftAccount) error {
	return Shared.UpdateMicrosoftAccount(account, ms)
}

// AccountStoreUpdateAuthlibAccount 更新皮肤站账号凭据并持久化。
func AccountStoreUpdateAuthlibAccount(account *LaunchAccount, credential *AuthlibCredential) error {
	return Shared.UpdateAuthlibAccount(account, credential)
}

// AccountStoreUpdateOfflineSkin 更新离线账号皮肤。
func AccountStoreUpdateOfflineSkin(account *LaunchAccount, skinId string) error {
	return Shared.UpdateOfflineSkin(account, skinId)
}
