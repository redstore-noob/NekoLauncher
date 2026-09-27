package auth

import (
	"encoding/json"
	"strings"

	"nekolauncher/internal/config"
)

// ConfigKey 组件展示账号覆盖在 accounts.yaml 中的键。
const componentDisplayConfigKey = "componentDisplayAccounts"

// ResolveComponentDisplayAccount 解析组件应展示的账号：优先取覆盖账号（存在时），
// 否则回退全局当前账号；无任何账号时返回 nil。
// 组件展示覆盖允许单个组件（如皮肤与披风）独立展示指定的正版账号，
// 而不是永远跟随全局当前账号。
func ResolveComponentDisplayAccount(componentId string) *LaunchAccount {
	key := ComponentDisplayAccountKey(componentId)
	if key == "" {
		return Shared.Selected()
	}
	account := Shared.FindByStableKey(key)
	if account != nil {
		return account
	}
	return Shared.Selected()
}

// ComponentDisplayAccountKey 读取组件展示账号的稳定键；空串表示跟随全局当前账号。
func ComponentDisplayAccountKey(componentId string) string {
	if strings.TrimSpace(componentId) == "" {
		return ""
	}
	var mapping map[string]string
	value := config.GetValue(componentDisplayConfigKey)
	if strings.TrimSpace(value) == "" {
		return ""
	}
	if err := json.Unmarshal([]byte(value), &mapping); err != nil {
		// 配置损坏时视为未设置覆盖
		return ""
	}
	key := mapping[componentId]
	if strings.TrimSpace(key) == "" {
		return ""
	}
	return key
}

// SetComponentDisplayAccountKey 设置组件展示的账号（传空串恢复跟随全局当前账号）。
func SetComponentDisplayAccountKey(componentId, accountKey string) {
	if strings.TrimSpace(componentId) == "" {
		panic("componentId 不能为空")
	}
	mapping := loadComponentDisplayMap()
	if strings.TrimSpace(accountKey) == "" {
		delete(mapping, componentId)
	} else {
		mapping[componentId] = accountKey
	}
	if data, err := json.Marshal(mapping); err == nil {
		config.SetValue(componentDisplayConfigKey, string(data))
	}
}

func loadComponentDisplayMap() map[string]string {
	mapping := map[string]string{}
	value := config.GetValue(componentDisplayConfigKey)
	if strings.TrimSpace(value) != "" {
		_ = json.Unmarshal([]byte(value), &mapping)
	}
	return mapping
}
