package bindings

// 资源搜索绑定（api_download_resources.go）的用例。
//
// 这里只覆盖"绑定层自己负责的事"：CurseForge API Key 的读写、资源站清单的
// 可用性判定、以及未配置 Key 时的降级提示。真正的搜索/下载逻辑在
// internal/download 里已被 httptest 用例覆盖——绑定层不该重复打网络。
//
// 存储目录必须指向 t.TempDir()：本包其余用例会读写 launcher.yaml，
// 不隔离就会动到用户真实的 %USERPROFILE%\NekoLauncher。

import (
	"strings"
	"testing"

	"nekolauncher/internal/config"
	"nekolauncher/internal/models"
)

// useTempConfigStorage 把启动器存储目录指到临时目录，避免测试污染真实用户配置。
func useTempConfigStorage(t *testing.T) {
	t.Helper()
	original := config.StorageDirectory()
	if err := config.SetStorageDirectory(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.SetStorageDirectory(original) })
}

// TestCurseForgeAPIKeyRoundTrip 防的回归：
// Key 存不进去（用户填了却每次都要重填）、读出来带首尾空格（请求头非法），
// 或者清空后旧值还在（关闭配置却仍在用 Key）。
func TestCurseForgeAPIKeyRoundTrip(t *testing.T) {
	useTempConfigStorage(t)

	api := &DownloadAPI{}
	// 注入模式（-ldflags -X）下初始值是内置 Key，否则为空串。
	if got := effectiveCurseForgeAPIKey(); got != strings.TrimSpace(builtinCurseForgeAPIKey) {
		t.Fatalf("初始应等于内置值（未注入时为空串），实际 %q", got)
	}

	if !api.SaveCurseForgeAPIKey("  $2a$10$abcdef  ") {
		t.Fatal("保存 Key 应返回成功")
	}
	if got := effectiveCurseForgeAPIKey(); got != "$2a$10$abcdef" {
		t.Fatalf("读出的 Key = %q，期望去掉首尾空格", got)
	}
	if got := config.GetValue("curseforgeApiKey"); got != "$2a$10$abcdef" {
		t.Fatalf("配置键 curseforgeApiKey 的值 = %q", got)
	}

	api.SaveCurseForgeAPIKey("")
	if got := effectiveCurseForgeAPIKey(); got != strings.TrimSpace(builtinCurseForgeAPIKey) {
		t.Fatalf("清空用户 Key 后应回到内置值，实际 %q", got)
	}
}

// skipWhenBuiltinKeyInjected 内置 Key 注入的发布构建里不存在"未配置 Key"状态，
// 依赖该状态的降级路径用例没有意义，直接跳过。
func skipWhenBuiltinKeyInjected(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(builtinCurseForgeAPIKey) != "" {
		t.Skip("内置 Key 已注入，未配置 Key 的降级路径不存在")
	}
}

// TestGetResourceSourcesReflectsConfiguredKey 防的回归：
// 资源站清单不看用户是否配了 Key（未配置也标"可用"，点了才报错；
// 或者配置好了仍提示去设置里填 Key）。
func TestGetResourceSourcesReflectsConfiguredKey(t *testing.T) {
	skipWhenBuiltinKeyInjected(t)
	useTempConfigStorage(t)

	api := &DownloadAPI{}
	sources := api.GetResourceSources()
	if len(sources) != 2 {
		t.Fatalf("资源站数量 = %d，期望 2", len(sources))
	}
	if sources[0].ID != models.ResourceSourceModrinth || !sources[0].Available {
		t.Fatalf("Modrinth 应始终可用：%+v", sources[0])
	}
	if sources[1].ID != models.ResourceSourceCurseForge || sources[1].Available {
		t.Fatalf("未配置 Key 时 CurseForge 应标为不可用：%+v", sources[1])
	}
	if sources[1].APIKeyApplyURL == "" {
		t.Fatal("清单里必须带上 Key 申请地址（前端不再硬编码域名）")
	}

	api.SaveCurseForgeAPIKey("key-123")
	configured := api.GetResourceSources()[1]
	if !configured.Available || !configured.APIKeyConfigured {
		t.Fatalf("配置 Key 后 CurseForge 应标为可用：%+v", configured)
	}
	if configured.Hint == "" {
		t.Fatal("Hint 不应为空（界面直接展示）")
	}
}

// TestSearchResourcesWithoutKeyReturnsGuidance 防的回归：
// 没配 Key 时绑定层抛异常（前端弹一个红色报错），而不是返回可读引导。
// 这条用例同时保证"未配置 Key 不会发起任何网络请求"。
func TestSearchResourcesWithoutKeyReturnsGuidance(t *testing.T) {
	skipWhenBuiltinKeyInjected(t)
	useTempConfigStorage(t)

	api := &DownloadAPI{}
	result, err := api.SearchResources(models.ResourceSearchRequest{
		Source:      models.ResourceSourceCurseForge,
		ProjectType: models.ProjectTypeMod,
		Query:       "jei",
	})
	if err != nil {
		t.Fatalf("未配置 Key 不该报错：%v", err)
	}
	if !result.NeedsAPIKey {
		t.Fatal("应标记 NeedsAPIKey")
	}
	if !strings.Contains(result.Message, "CurseForge API Key") {
		t.Fatalf("提示应说明要填 Key：%q", result.Message)
	}
	if result.Hits == nil {
		t.Fatal("Hits 不能为 nil（前端会直接 .map）")
	}
}

// TestListResourceVersionsWithoutKeyReturnsGuidance 防的回归：版本列表路径的降级同上。
func TestListResourceVersionsWithoutKeyReturnsGuidance(t *testing.T) {
	skipWhenBuiltinKeyInjected(t)
	useTempConfigStorage(t)

	api := &DownloadAPI{}
	result, err := api.ListResourceVersions(models.ResourceVersionRequest{
		Source:    models.ResourceSourceCurseForge,
		ProjectID: "238222",
	})
	if err != nil {
		t.Fatalf("未配置 Key 不该报错：%v", err)
	}
	if !result.NeedsAPIKey || result.Message == "" {
		t.Fatalf("应返回可读引导：%+v", result)
	}
	if result.Versions == nil {
		t.Fatal("Versions 不能为 nil（前端会直接 .map）")
	}
}

// TestDownloadResourceVersionWithoutKeyExplainsWhy 防的回归：
// 未配置 Key 时下载报的是"下载失败"这类含糊错误，用户不知道该去配 Key。
func TestDownloadResourceVersionWithoutKeyExplainsWhy(t *testing.T) {
	skipWhenBuiltinKeyInjected(t)
	useTempConfigStorage(t)

	api := &DownloadAPI{}
	_, err := api.DownloadResourceVersion(models.ResourceDownloadRequest{
		Source:           models.ResourceSourceCurseForge,
		ProjectID:        "238222",
		VersionID:        "8965084",
		ContentDirectory: t.TempDir(),
	})
	if err == nil {
		t.Fatal("未配置 Key 时下载必须报错")
	}
	if !strings.Contains(err.Error(), "CurseForge API Key") {
		t.Fatalf("错误信息应指向 Key 配置：%v", err)
	}
}

// TestCurseForgeAPIKeyBuiltinFallback 防的回归：
// 编译期注入的内置 Key（-ldflags -X）没有生效（发布版用户不填 Key 就用不了
// CurseForge），或者生效后盖过了用户自己配置的 Key（用户填的 Key 应优先）。
// 两种运行模式都要成立：普通 go test（builtin 为空）与注入模式（builtin 非空）。
func TestCurseForgeAPIKeyBuiltinFallback(t *testing.T) {
	useTempConfigStorage(t)

	api := &DownloadAPI{}
	if got := effectiveCurseForgeAPIKey(); got != strings.TrimSpace(builtinCurseForgeAPIKey) {
		t.Fatalf("未配置用户 Key 时应回落到内置值：got %q, builtin %q", got, builtinCurseForgeAPIKey)
	}

	if !api.SaveCurseForgeAPIKey("user-key") {
		t.Fatal("保存用户 Key 应返回成功")
	}
	if got := effectiveCurseForgeAPIKey(); got != "user-key" {
		t.Fatalf("用户配置的 Key 应优先于内置值：got %q", got)
	}

	api.SaveCurseForgeAPIKey("")
	if got := effectiveCurseForgeAPIKey(); got != strings.TrimSpace(builtinCurseForgeAPIKey) {
		t.Fatalf("清空用户 Key 后应回到内置值：got %q", got)
	}
}
