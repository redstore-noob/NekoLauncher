package mcserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useContentTestServer 造一台临时服务器（mods/ 与 plugins/ 各放几个文件）。
func useContentTestServer(t *testing.T, id string) string {
	t.Helper()

	root := t.TempDir()
	serverRootOverride = &root
	t.Cleanup(func() { serverRootOverride = nil })

	dir := filepath.Join(root, id)
	writeTestFile(t, filepath.Join(dir, "mods", "sodium.jar"), "mod-a")
	writeTestFile(t, filepath.Join(dir, "mods", "lithium.jar.disabled"), "mod-b")
	writeTestFile(t, filepath.Join(dir, "mods", "readme.txt"), "not-a-jar")
	writeTestFile(t, filepath.Join(dir, "plugins", "essentials.jar"), "plugin-a")

	return dir
}

// TestServerContentKindForCore 核心 → 目录类型 / Modrinth 加载器的映射。
func TestServerContentKindForCore(t *testing.T) {
	cases := []struct {
		core   string
		kind   string
		loader string
		fails  bool
	}{
		{core: CoreFabric, kind: ContentKindMods, loader: "fabric"},
		{core: CoreNeoForge, kind: ContentKindMods, loader: "neoforge"},
		{core: CorePaper, kind: ContentKindPlugins, loader: "paper"},
		{core: CoreVanilla, fails: true},
		{core: "unknown", fails: true},
	}

	for _, testCase := range cases {
		kind, err := ServerContentKindForCore(testCase.core)
		if testCase.fails {
			if err == nil {
				t.Fatalf("%s 应当报错", testCase.core)
			}

			continue
		}
		if err != nil {
			t.Fatalf("%s 解析失败：%v", testCase.core, err)
		}
		if kind != testCase.kind {
			t.Fatalf("%s → %s，期望 %s", testCase.core, kind, testCase.kind)
		}
		if loader := ModrinthLoaderForCore(testCase.core); loader != testCase.loader {
			t.Fatalf("%s 加载器 = %s，期望 %s", testCase.core, loader, testCase.loader)
		}
	}
}

// TestListServerContent 只列 jar，识别 .disabled，展示名干净。
func TestListServerContent(t *testing.T) {
	id := "content-list"
	useContentTestServer(t, id)

	mods := ListServerContent(id, ContentKindMods)
	if len(mods) != 2 {
		t.Fatalf("应只列出两个 jar（忽略 txt）：%+v", mods)
	}
	// 排序后 lithium 在前
	if mods[0].Name != "lithium" || mods[0].Enabled {
		t.Fatalf("停用条目解析不符：%+v", mods[0])
	}
	if mods[0].FileName != "lithium.jar.disabled" {
		t.Fatalf("磁盘文件名应保留 .disabled：%+v", mods[0])
	}
	if mods[1].Name != "sodium" || !mods[1].Enabled {
		t.Fatalf("启用条目解析不符：%+v", mods[1])
	}

	plugins := ListServerContent(id, ContentKindPlugins)
	if len(plugins) != 1 || plugins[0].Name != "essentials" {
		t.Fatalf("插件列表不符：%+v", plugins)
	}

	// 未知类型 / 不存在的服务器 → 空数组而不是 panic
	if list := ListServerContent(id, "unknown"); len(list) != 0 {
		t.Fatalf("未知类型应返回空：%+v", list)
	}
	if list := ListServerContent("missing-server", ContentKindMods); len(list) != 0 {
		t.Fatalf("目录不存在应返回空：%+v", list)
	}
}

// TestSetServerContentEnabled 启停靠改名，幂等且不覆盖同名文件。
func TestSetServerContentEnabled(t *testing.T) {
	id := "content-toggle"
	dir := useContentTestServer(t, id)

	// 停用
	if err := SetServerContentEnabled(id, ContentKindMods, "sodium.jar", false); err != nil {
		t.Fatalf("停用失败：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mods", "sodium.jar.disabled")); err != nil {
		t.Fatalf("停用后文件名不对：%v", err)
	}
	// 再次停用：幂等
	if err := SetServerContentEnabled(id, ContentKindMods, "sodium.jar", false); err != nil {
		t.Fatalf("重复停用应幂等：%v", err)
	}

	// 启用
	if err := SetServerContentEnabled(id, ContentKindMods, "lithium.jar.disabled", true); err != nil {
		t.Fatalf("启用失败：%v", err)
	}
	if list := ListServerContent(id, ContentKindMods); len(list) != 2 {
		t.Fatalf("启用后条目数不对：%+v", list)
	}

	// 目标名已被占用时拒绝（否则会覆盖掉另一个文件）
	writeTestFile(t, filepath.Join(dir, "mods", "sodium.jar.disabled"), "again")
	writeTestFile(t, filepath.Join(dir, "mods", "sodium.jar"), "occupied")
	if err := SetServerContentEnabled(id, ContentKindMods, "sodium.jar.disabled", true); err == nil {
		t.Fatal("目标名已存在时应拒绝启用")
	}

	// 文件名越界要挡住
	for _, bad := range []string{"../server.properties", `..\x.jar`, "sub/x.jar", ""} {
		if err := SetServerContentEnabled(id, ContentKindMods, bad, false); err == nil {
			t.Fatalf("非法文件名应被拒绝：%q", bad)
		}
	}
}

// TestDeleteServerContent 删除只接受单层文件名，不存在时报错。
func TestDeleteServerContent(t *testing.T) {
	id := "content-delete"
	useContentTestServer(t, id)

	if err := DeleteServerContent(id, ContentKindMods, "sodium.jar"); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	if list := ListServerContent(id, ContentKindMods); len(list) != 1 {
		t.Fatalf("删除后应剩 1 个：%+v", list)
	}
	if err := DeleteServerContent(id, ContentKindMods, "sodium.jar"); err == nil {
		t.Fatal("删除不存在的文件应报错")
	}
	if err := DeleteServerContent(id, ContentKindMods, "../server.properties"); err == nil {
		t.Fatal("越界文件名应被拒绝")
	}
}

// TestInstallServerContentFromURL 从真实 HTTP 下载：落盘内容正确、同名自动加序号。
func TestInstallServerContentFromURL(t *testing.T) {
	id := "content-install"
	dir := useContentTestServer(t, id)

	payload := "fake-jar-bytes"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(payload))
	}))
	defer server.Close()

	installed, err := InstallServerContentFromURL(context.Background(), id, ContentKindMods,
		server.URL+"/ferritecore-1.0.jar", "ferritecore-1.0.jar")
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	if installed != "ferritecore-1.0.jar" {
		t.Fatalf("文件名 = %q", installed)
	}
	data, readErr := os.ReadFile(filepath.Join(dir, "mods", installed))
	if readErr != nil || string(data) != payload {
		t.Fatalf("落盘内容不符：%v / %q", readErr, data)
	}

	// 同名再装一次：不能覆盖，自动加序号
	second, err := InstallServerContentFromURL(context.Background(), id, ContentKindMods,
		server.URL+"/ferritecore-1.0.jar", "ferritecore-1.0.jar")
	if err != nil {
		t.Fatalf("二次安装失败：%v", err)
	}
	if second != "ferritecore-1.0-1.jar" {
		t.Fatalf("同名应加序号，得到 %q", second)
	}
	if list := ListServerContent(id, ContentKindMods); len(list) != 4 {
		t.Fatalf("两次安装后应有 4 个 jar（含原有 2 个）：%+v", list)
	}

	// 非 jar 拒绝
	if _, err := InstallServerContentFromURL(context.Background(), id, ContentKindMods,
		server.URL+"/x.zip", "x.zip"); err == nil {
		t.Fatal("非 jar 应被拒绝")
	}
	// 空地址拒绝
	if _, err := InstallServerContentFromURL(context.Background(), id, ContentKindMods, "", "x.jar"); err == nil {
		t.Fatal("空地址应被拒绝")
	}
	// 越界文件名拒绝
	if _, err := InstallServerContentFromURL(context.Background(), id, ContentKindMods,
		server.URL+"/x.jar", "../x.jar"); err == nil {
		t.Fatal("越界文件名应被拒绝")
	}
}

// TestInstallServerContentFiltersByLoader 端到端（真连 Modrinth）：
// Fabric 服务端搜出来的项目必须真的支持 fabric，且版本列表按 fabric+MC 版本过滤。
// 默认跳过（需要网络），设置 NEKO_LIVE_MODRINTH=1 才跑。
func TestInstallServerContentFiltersByLoader(t *testing.T) {
	if strings.TrimSpace(os.Getenv("NEKO_LIVE_MODRINTH")) == "" {
		t.Skip("未设置 NEKO_LIVE_MODRINTH，跳过 Modrinth 在线校验")
	}

	ctx := context.Background()
	projects, err := SearchServerContent(ctx, "sodium", "1.21.1", CoreFabric, 5)
	if err != nil {
		t.Fatalf("搜索失败：%v", err)
	}
	if len(projects) == 0 {
		t.Fatal("应当搜到结果")
	}
	versions, err := ListServerContentVersions(ctx, projects[0].ProjectID, "1.21.1", CoreFabric)
	if err != nil {
		t.Fatalf("取版本失败：%v", err)
	}
	if len(versions) == 0 {
		t.Fatal("应当有适配 1.21.1 + fabric 的版本")
	}
	for _, version := range versions {
		matched := false
		for _, loader := range version.Loaders {
			if loader == "fabric" {
				matched = true
			}
		}
		if !matched {
			t.Fatalf("版本 %s 不含 fabric 加载器：%+v", version.VersionNumber, version.Loaders)
		}
	}
}
