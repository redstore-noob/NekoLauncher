package bindings

import "testing"

// 账户域键必须被 WebView 出入口拒绝：恶意插件可以绕过前端权限门
// 直呼 ConfigAPI.SetValue/GetValue，这条防线必须在 Go 侧。
// 只测守卫判定本身，不落盘（bindings 测试的存储单例指向真实用户目录，
// 换机器跑会污染真实 launcher.yaml）。
func TestGuardAccountDomainKey(t *testing.T) {
	for _, key := range []string{"accounts", "authlibClientToken"} {
		if guardAccountDomainKey(key) {
			t.Errorf("账户域键 %q 应被拒绝", key)
		}
	}
	// 凭据类普通键（代理凭据 / 红石联机 Key）同样拒绝：前端合法入口是
	// GetProxySettings / OnlineAPI.GetSettings 这类"脱敏视图 + 合并保存"绑定
	for _, key := range []string{"proxyPassword", "proxyUsername", "online.redstoneKey"} {
		if guardAccountDomainKey(key) {
			t.Errorf("凭据键 %q 应被拒绝", key)
		}
	}
	for _, key := range []string{"closeAction", "homeWidgetColumns", "plugin:demo:lastTab", ""} {
		if !guardAccountDomainKey(key) {
			t.Errorf("普通键 %q 不应被拒绝", key)
		}
	}
}

// 被拒键在绑定方法里短路返回，不触达配置存储。
func TestConfigAPIAccountDomainKeysShortCircuit(t *testing.T) {
	api := &ConfigAPI{}
	// 这些调用在守卫处直接返回，不会发起任何磁盘读写
	if api.SetValue("accounts", `[]`) {
		t.Fatal("SetValue(accounts) 不应成功")
	}
	if got := api.GetValue("accounts"); got != "" {
		t.Fatalf("GetValue(accounts) 应返回空串，得到 %q", got)
	}
	if api.ClearValue("authlibClientToken") {
		t.Fatal("ClearValue(authlibClientToken) 不应成功")
	}
}
