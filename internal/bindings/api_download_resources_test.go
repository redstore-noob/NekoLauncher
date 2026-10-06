package bindings

// 资源搜索绑定（api_download_resources.go）的用例。
//
// 这里只覆盖"绑定层自己负责的事"：CurseForge API Key 的取值（唯一来源是
// 编译期内置值）、资源站清单的可用性判定、以及未配置 Key 时的降级提示。
// 真正的搜索/下载逻辑在 internal/download 里已被 httptest 用例覆盖——
// 绑定层不该重复打网络。
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

// TestEffectiveCurseForgeAPIKeyIsBuiltinOnly 防的回归：
// 生效 Key 偏离了编译期内置值（注入模式下被别的来源盖过、或自带首尾空格）。
// 用户自行配置 Key 的功能已移除，内置值是唯一来源。
func TestEffectiveCurseForgeAPIKeyIsBuiltinOnly(t *testing.T) {
	useTempConfigStorage(t)

	if got := effectiveCurseForgeAPIKey(); got != strings.TrimSpace(builtinCurseForgeAPIKey) {
		t.Fatalf("生效 Key 应等于内置值（未注入时为空串），实际 %q", got)
	}
	// 历史遗留的用户自配 Key（功能移除前写入的）必须不影响生效值。
	if !config.SetValue("curseforgeApiKey", "stale-user-key") {
		t.Fatal("写入遗留 Key 的配置应成功")
	}
	if got := effectiveCurseForgeAPIKey(); got != strings.TrimSpace(builtinCurseForgeAPIKey) {
		t.Fatalf("遗留的用户 Key 不应参与生效判定，实际 %q", got)
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

// TestGetResourceSourcesWithoutKeyMarksCurseForgeUnavailable 防的回归：
// 资源站清单在 Key 未配置时也把 Modrinth 标"可用"（点了才能搜），
// 而 CurseForge 必须标"不可用"（不满足就点进去报错），并带上提示文案。
func TestGetResourceSourcesWithoutKeyMarksCurseForgeUnavailable(t *testing.T) {
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
	if sources[1].Hint == "" {
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
	if !strings.Contains(result.Message, "内置 API Key") {
		t.Fatalf("提示应说明内置 Key 未生效：%q", result.Message)
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
	if !strings.Contains(err.Error(), "内置 API Key") {
		t.Fatalf("错误信息应指向内置 Key 未生效：%v", err)
	}
}
