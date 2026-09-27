package bindings

import (
	"context"
	"testing"

	"nekolauncher/internal/online"
)

// TestOnlineAPIProviders 供应商注册表：前端靠这个列表渲染切换器，
// 顺序与 id 都是契约（配置里存的是 id）。
func TestOnlineAPIProviders(t *testing.T) {
	api := &OnlineAPI{}
	api.Startup(context.Background())

	providers := api.ListProviders()
	if len(providers) != 2 {
		t.Fatalf("供应商数量 = %d，期望 2", len(providers))
	}
	if providers[0].ID != string(online.ProviderTerracotta) ||
		providers[1].ID != string(online.ProviderRedstone) {
		t.Fatalf("供应商顺序/ID 不符：%s, %s", providers[0].ID, providers[1].ID)
	}

	for _, provider := range providers {
		if provider.Name == "" || provider.Summary == "" {
			t.Fatalf("%s 缺少名称或简介：%+v", provider.ID, provider)
		}
		if provider.HostNote == "" || provider.JoinNote == "" {
			t.Fatalf("%s 缺少建房/加入说明：%+v", provider.ID, provider)
		}
		if provider.Homepage == "" {
			t.Fatalf("%s 缺少主页地址", provider.ID)
		}
		// 说明文案会被前端原样丢给 t() 查词典，拼接运行期内容会让词典永远查不中
		if provider.Hint == "" {
			t.Fatalf("%s 缺少可用性提示", provider.ID)
		}
	}
}

// TestOnlineAPIStatusAndRuntime 空闲状态下也要有可用的默认值，前端首帧不报错。
func TestOnlineAPIStatusAndRuntime(t *testing.T) {
	api := &OnlineAPI{}
	api.Startup(context.Background())

	// 两家各一份快照，且都带得上供应商标识（前端按 Provider 归位）
	statuses := api.ListStatuses()
	if len(statuses) != 2 {
		t.Fatalf("会话快照数量 = %d，期望 2", len(statuses))
	}
	for index, id := range []string{"terracotta", "redstone"} {
		if statuses[index].Provider != id {
			t.Fatalf("第 %d 份快照的供应商 = %q，期望 %q", index, statuses[index].Provider, id)
		}
		if statuses[index].State != online.StateIdle {
			t.Fatalf("%s 无会话时状态 = %q，期望 idle", id, statuses[index].State)
		}
		if statuses[index].Players == nil {
			t.Fatalf("%s 的成员列表不应为 nil（前端会直接 .map）", id)
		}
	}

	// 单家查询走管理器（绑定层不再单独暴露 GetStatus，前端用 ListStatuses）
	for index, id := range []string{"redstone", "unknown-provider"} {
		status := online.Default().Status(id)
		if status.State != online.StateIdle {
			t.Fatalf("Status(%q) 应为 idle 快照，得到 %+v", id, status)
		}
		if index == 1 && status.Provider != "terracotta" {
			t.Fatalf("未知供应商应归一到陶瓦联机，得到 %q", status.Provider)
		}
	}

	for _, id := range []string{"terracotta", "redstone", "unknown-provider"} {
		runtime := api.GetRuntime(id)
		if runtime.Provider == "" {
			t.Fatalf("GetRuntime(%q) 未回传供应商标识：%+v", id, runtime)
		}
	}
}

// TestOnlineAPIOpenPageRejectsNonHTTP 只允许打开 http(s) 地址，避免把
// rundll32 / open 当成任意命令执行器。
func TestOnlineAPIOpenPageRejectsNonHTTP(t *testing.T) {
	api := &OnlineAPI{}

	for _, target := range []string{"", "   ", "file:///C:/Windows/System32/calc.exe", "calc.exe"} {
		if err := api.OpenPage(target); err == nil {
			t.Fatalf("OpenPage(%q) 应当被拒绝", target)
		}
	}
}

// TestOnlineAPILocalServerOptions 未注入来源时返回空切片而不是 nil
// （前端会直接 .map，nil 会让 React 渲染崩）。
func TestOnlineAPILocalServerOptions(t *testing.T) {
	api := &OnlineAPI{}
	api.Startup(context.Background())

	if servers := api.ListLocalServers(); servers == nil {
		t.Fatal("ListLocalServers 不应返回 nil")
	}
}
