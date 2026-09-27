package bindings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// memoryDisabledStore 内存版停用列表：测试绝不写用户真实的 launcher.yaml。
type memoryDisabledStore struct{ ids map[string]bool }

func newMemoryDisabledStore(initial ...string) *memoryDisabledStore {
	store := &memoryDisabledStore{ids: make(map[string]bool, len(initial))}
	for _, id := range initial {
		store.ids[id] = true
	}
	return store
}

func (m *memoryDisabledStore) IDs() map[string]bool { return m.ids }

func (m *memoryDisabledStore) Set(id string, disabled bool) {
	if disabled {
		m.ids[id] = true
		return
	}
	delete(m.ids, id)
}

// newPluginFixture 构造一个可用的插件源目录（含嵌套子目录与若干文件）。
func newPluginFixture(t *testing.T, parent, id, name string) string {
	t.Helper()
	directory := filepath.Join(parent, id)
	if err := os.MkdirAll(filepath.Join(directory, "assets"), 0o755); err != nil {
		t.Fatalf("创建插件源目录失败：%v", err)
	}
	writeFile(t, filepath.Join(directory, pluginManifestName),
		fmt.Sprintf(`{"id":%q,"name":%q,"version":"1.0.0","api":"1","author":"tester","description":"desc","entry":"index.js"}`, id, name))
	writeFile(t, filepath.Join(directory, "index.js"), "export default () => {};")
	writeFile(t, filepath.Join(directory, "assets", "logo.png"), "fake-png-bytes")
	writeFile(t, filepath.Join(directory, pluginIconName), "fake-icon-bytes")
	return directory
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入 %s 失败：%v", path, err)
	}
}

// newPluginAPI 构造注入了临时目录与内存停用列表的 API。
func newPluginAPI(t *testing.T, store *memoryDisabledStore) (*PluginAPI, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "plugins")
	if store == nil {
		store = newMemoryDisabledStore()
	}
	return &PluginAPI{root: root, disabled: store}, root
}

// TestListPluginsReportsManifestProblems 清单缺失/非法/id 不符都要在列表里体现出来，
// 而不是把目录静默藏起来——插件页要靠这个告诉用户"装坏了"。
func TestListPluginsReportsManifestProblems(t *testing.T) {
	api, root := newPluginAPI(t, nil)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("创建插件目录失败：%v", err)
	}

	newPluginFixture(t, root, "good", "好的插件")

	if err := os.MkdirAll(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "broken", pluginManifestName), "{ not json")

	if err := os.MkdirAll(filepath.Join(root, "missing"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(root, "mismatch"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "mismatch", pluginManifestName), `{"id":"other","api":"1"}`)

	// 插件目录根下的散落文件应被忽略
	writeFile(t, filepath.Join(root, "readme.txt"), "not a plugin")

	infos, err := api.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins 失败：%v", err)
	}
	if len(infos) != 4 {
		t.Fatalf("应列出 4 个目录，实际 %d：%+v", len(infos), infos)
	}

	byID := make(map[string]PluginInfo, len(infos))
	for _, info := range infos {
		byID[info.ID] = info
	}
	for index, id := range []string{"broken", "good", "mismatch", "missing"} {
		if infos[index].ID != id {
			t.Errorf("第 %d 项应为 %s（按 id 排序），实际 %s", index, id, infos[index].ID)
		}
	}

	good := byID["good"]
	if good.ManifestError != "" {
		t.Errorf("合法插件不应有清单错误：%s", good.ManifestError)
	}
	if good.Name != "好的插件" || good.Version != "1.0.0" || good.Author != "tester" ||
		good.APIVersion != "1" || good.Entry != "index.js" || good.Description != "desc" {
		t.Errorf("清单字段未正确填充：%+v", good)
	}
	if good.SizeBytes <= 0 || good.FileCount < 3 || good.ModifiedAt <= 0 {
		t.Errorf("磁盘统计异常：size=%d files=%d mtime=%d", good.SizeBytes, good.FileCount, good.ModifiedAt)
	}
	if !strings.Contains(good.Directory, "good") {
		t.Errorf("应带上目录路径，实际 %q", good.Directory)
	}

	if !strings.Contains(byID["broken"].ManifestError, "不是合法 YAML") {
		t.Errorf("非法 JSON 应给出明确错误，实际 %q", byID["broken"].ManifestError)
	}
	if !strings.Contains(byID["missing"].ManifestError, "缺少 plugin.yaml") {
		t.Errorf("缺清单应给出明确错误，实际 %q", byID["missing"].ManifestError)
	}
	if !strings.Contains(byID["mismatch"].ManifestError, "与目录名不一致") {
		t.Errorf("id 不符应给出明确错误，实际 %q", byID["mismatch"].ManifestError)
	}
}

// TestListPluginsMarksDisabled 停用状态来自注入的存储。
func TestListPluginsMarksDisabled(t *testing.T) {
	api, root := newPluginAPI(t, newMemoryDisabledStore("good"))
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	newPluginFixture(t, root, "good", "被停用的")
	newPluginFixture(t, root, "other", "正常的")

	infos, err := api.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins 失败：%v", err)
	}
	byID := make(map[string]PluginInfo)
	for _, info := range infos {
		byID[info.ID] = info
	}
	if !byID["good"].Disabled {
		t.Error("good 应标记为已停用")
	}
	if byID["other"].Disabled {
		t.Error("other 不应被标记为停用")
	}
}

// TestListPluginsWithoutDirectory 还没装过插件时返回空列表而不是错误。
func TestListPluginsWithoutDirectory(t *testing.T) {
	api, _ := newPluginAPI(t, nil)

	infos, err := api.ListPlugins()
	if err != nil {
		t.Fatalf("目录不存在不应报错：%v", err)
	}
	if len(infos) != 0 {
		t.Errorf("应返回空列表，实际 %+v", infos)
	}
}

// TestInstallPluginCopiesWholeTree 安装要把整棵目录（含子目录）复制到位，并按清单 id 命名。
func TestInstallPluginCopiesWholeTree(t *testing.T) {
	api, root := newPluginAPI(t, nil)
	source := newPluginFixture(t, t.TempDir(), "demo-source", "演示")

	id, err := api.InstallPluginDirectory(source)
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	if id != "demo-source" {
		t.Errorf("应以清单 id 命名，实际 %q", id)
	}

	installed := filepath.Join(root, "demo-source")
	for _, relative := range []string{pluginManifestName, "index.js", filepath.Join("assets", "logo.png")} {
		if _, err := os.Stat(filepath.Join(installed, relative)); err != nil {
			t.Errorf("安装后缺少文件 %s：%v", relative, err)
		}
	}
	content, err := os.ReadFile(filepath.Join(installed, "assets", "logo.png"))
	if err != nil || string(content) != "fake-png-bytes" {
		t.Errorf("子目录文件内容未正确复制：%q %v", content, err)
	}

	// 安装后应能被列表读到，且清单正常
	infos, err := api.ListPlugins()
	if err != nil || len(infos) != 1 || infos[0].ManifestError != "" {
		t.Errorf("安装结果应可正常列出：%+v %v", infos, err)
	}
}

// TestInstallPluginRejections 安装的各类拒绝场景。
func TestInstallPluginRejections(t *testing.T) {
	api, root := newPluginAPI(t, nil)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	newPluginFixture(t, root, "existing", "已存在")

	cases := []struct {
		name        string
		source      func(t *testing.T) string
		wantMessage string
	}{
		{
			name: "缺少清单",
			source: func(t *testing.T) string {
				directory := filepath.Join(t.TempDir(), "nomanifest")
				if err := os.MkdirAll(directory, 0o755); err != nil {
					t.Fatal(err)
				}
				return directory
			},
			wantMessage: "缺少 plugin.yaml",
		},
		{
			name: "清单非法",
			source: func(t *testing.T) string {
				directory := filepath.Join(t.TempDir(), "badjson")
				if err := os.MkdirAll(directory, 0o755); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(directory, pluginManifestName), "{ nope")
				return directory
			},
			wantMessage: "不是合法 YAML",
		},
		{
			name: "id 含路径分隔符",
			source: func(t *testing.T) string {
				directory := filepath.Join(t.TempDir(), "badid")
				if err := os.MkdirAll(directory, 0o755); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(directory, pluginManifestName), `{"id":"../escape","api":"1"}`)
				return directory
			},
			wantMessage: "非法字符",
		},
		{
			name: "同名插件已存在",
			source: func(t *testing.T) string {
				return newPluginFixture(t, t.TempDir(), "existing", "重复")
			},
			wantMessage: "已存在同名插件",
		},
		{
			name: "源目录已在插件目录内",
			source: func(t *testing.T) string {
				return filepath.Join(root, "existing")
			},
			wantMessage: "已在插件目录内",
		},
		{
			name: "源目录不存在",
			source: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "absent")
			},
			wantMessage: "插件目录不可用",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := api.InstallPluginDirectory(testCase.source(t))
			if err == nil {
				t.Fatal("应返回错误")
			}
			if !strings.Contains(err.Error(), testCase.wantMessage) {
				t.Errorf("错误信息应包含 %q，实际 %q", testCase.wantMessage, err.Error())
			}
		})
	}
}

// TestUninstallPluginRemovesDirectoryAndFlag 卸载要删掉目录，并顺手清掉停用记录。
func TestUninstallPluginRemovesDirectoryAndFlag(t *testing.T) {
	store := newMemoryDisabledStore()
	api, root := newPluginAPI(t, store)
	source := newPluginFixture(t, t.TempDir(), "demo", "演示")

	if _, err := api.InstallPluginDirectory(source); err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	if err := api.SetPluginDisabled("demo", true); err != nil {
		t.Fatalf("停用失败：%v", err)
	}
	if !store.IDs()["demo"] {
		t.Fatal("停用状态未写入")
	}

	if err := api.UninstallPlugin("demo"); err != nil {
		t.Fatalf("卸载失败：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "demo")); !os.IsNotExist(err) {
		t.Errorf("插件目录应已删除，实际 err=%v", err)
	}
	if store.IDs()["demo"] {
		t.Error("卸载后不应残留停用记录")
	}
}

// TestUninstallPluginRejectsEscape 卸载的 id 必须限定在插件目录内。
func TestUninstallPluginRejectsEscape(t *testing.T) {
	api, root := newPluginAPI(t, nil)
	outside := filepath.Join(filepath.Dir(root), "precious")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"../precious", "..", "", "a/b", "a\\b"} {
		if err := api.UninstallPlugin(id); err == nil {
			t.Errorf("id %q 应被拒绝", id)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("插件目录外的目录不应被删除：%v", err)
	}
}

// TestSetPluginDisabledValidatesID 启停同样要校验 id。
func TestSetPluginDisabledValidatesID(t *testing.T) {
	api, _ := newPluginAPI(t, nil)

	if err := api.SetPluginDisabled("../x", true); err == nil {
		t.Error("非法 id 应被拒绝")
	}
	if err := api.SetPluginDisabled("fine-ID_1", true); err != nil {
		t.Errorf("合法 id 不应报错：%v", err)
	}
}

// TestPluginIndexIncludesDisabledField 发现接口要带上 disabled 字段供前端跳过加载。
// 具体内容取决于本机配置，这里只确认字段存在且是数组（避免测试依赖运行环境）。
func TestPluginIndexIncludesDisabledField(t *testing.T) {
	root, _ := pluginTestFixture(t)
	response := doRequest(newPluginHandler(root), pluginRoutePrefix)

	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("响应不是合法 YAML：%v", err)
	}
	if _, ok := payload["disabled"].([]any); !ok {
		t.Errorf("响应应包含 disabled 数组，实际 %v", payload)
	}
	if plugins, ok := payload["plugins"].([]any); !ok || len(plugins) != 1 {
		t.Errorf("响应应包含 plugins 数组且只含 demo，实际 %v", payload["plugins"])
	}
}

// TestSavePluginManifest 图形化编辑保存：合法修改落盘、改 id 与空名称被拒。
func TestSavePluginManifest(t *testing.T) {
	api, root := newPluginAPI(t, nil)
	directory := newPluginFixture(t, root, "good", "好的插件")

	// 正常修改：name/version 落盘，其余字段保持完整
	updated := `{"id":"good","name":"改名了","version":"2.0.0","apiVersion":"1","author":"tester","description":"desc","entry":"index.js","icon":"icon.png"}`
	if err := api.SavePluginManifest(directory, updated); err != nil {
		t.Fatalf("保存清单失败：%v", err)
	}
	saved, err := readPluginManifest(directory)
	if err != nil {
		t.Fatalf("重新读取清单失败：%v", err)
	}
	if saved.Name != "改名了" || saved.Version != "2.0.0" || saved.Entry != "index.js" {
		t.Errorf("清单内容未正确落盘：%+v", saved)
	}

	// 改 id：与磁盘既有清单不一致，必须拒绝
	renamed := `{"id":"other","name":"改名了","version":"2.0.0","apiVersion":"1"}`
	if err := api.SavePluginManifest(directory, renamed); err == nil {
		t.Error("修改插件 id 应被拒绝")
	}

	// 空名称：拒绝
	unnamed := `{"id":"good","name":"","version":"2.0.0","apiVersion":"1"}`
	if err := api.SavePluginManifest(directory, unnamed); err == nil {
		t.Error("空插件名称应被拒绝")
	}

	// 目录不存在：拒绝
	if err := api.SavePluginManifest(filepath.Join(root, "missing"), updated); err == nil {
		t.Error("不存在的插件目录应被拒绝")
	}
}

// TestCreatePluginScaffold 从零创建骨架：文件齐全、重复目录与非法 id 被拒。
func TestCreatePluginScaffold(t *testing.T) {
	api, root := newPluginAPI(t, nil)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := `{"id":"fresh","name":"新插件","version":"0.1.0","apiVersion":"1","author":"me","entry":"index.js"}`
	target, err := api.CreatePluginScaffold(root, manifest, "export default () => {};")
	if err != nil {
		t.Fatalf("创建骨架失败：%v", err)
	}
	if target != filepath.Join(root, "fresh") {
		t.Errorf("返回目录不符：%s", target)
	}
	saved, err := readPluginManifest(target)
	if err != nil {
		t.Fatalf("清单不可读：%v", err)
	}
	if saved.Name != "新插件" || saved.APIVersion != "1" {
		t.Errorf("清单内容不符：%+v", saved)
	}
	if raw, err := os.ReadFile(filepath.Join(target, "index.js")); err != nil || len(raw) == 0 {
		t.Errorf("入口文件未写入：%v", err)
	}
	if info, err := os.Stat(filepath.Join(target, "assets")); err != nil || !info.IsDir() {
		t.Error("assets 目录未创建")
	}

	// 重复创建：拒绝
	if _, err := api.CreatePluginScaffold(root, manifest, "x"); err == nil {
		t.Error("重复目录应被拒绝")
	}

	// 非法 id：拒绝
	bad := `{"id":"../evil","name":"evil","version":"1","apiVersion":"1"}`
	if _, err := api.CreatePluginScaffold(root, bad, "x"); err == nil {
		t.Error("非法 id 应被拒绝")
	}

	// 空名称：拒绝
	unnamed := `{"id":"ok2","name":"","version":"1","apiVersion":"1"}`
	if _, err := api.CreatePluginScaffold(root, unnamed, "x"); err == nil {
		t.Error("空名称应被拒绝")
	}
}

// TestListPluginsSurfacesCapabilities 清单声明的权限键（只取 true 并排序）
// 应暴露到 PluginInfo，供插件页展示。
func TestListPluginsSurfacesCapabilities(t *testing.T) {
	store := newMemoryDisabledStore()
	api, _ := newPluginAPI(t, store)
	source := newPluginFixture(t, t.TempDir(), "demo", "演示")
	writeFile(t, filepath.Join(source, pluginManifestName),
		`{"id":"demo","name":"演示","version":"1.0.0","api":"1","capabilities":{"storage":true,"launch":true,"notifications":false}}`)
	if _, err := api.InstallPluginDirectory(source); err != nil {
		t.Fatalf("安装失败：%v", err)
	}

	list, err := api.ListPlugins()
	if err != nil {
		t.Fatalf("列出失败：%v", err)
	}
	if len(list) != 1 {
		t.Fatalf("应列出 1 个插件：%+v", list)
	}
	got := list[0].Capabilities
	if len(got) != 2 || got[0] != "launch" || got[1] != "storage" {
		t.Fatalf("权限应只含取值为 true 的键并排序，实际 %v", got)
	}
}
