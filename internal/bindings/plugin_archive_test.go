package bindings

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeZip 把 name → content 写成一个 zip 文件（按名字排序保证可复现）。
func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建 zip 失败：%v", err)
	}
	writer := zip.NewWriter(file)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entryWriter, err := writer.Create(name)
		if err != nil {
			t.Fatalf("写入条目 %s 失败：%v", name, err)
		}
		if _, err := entryWriter.Write([]byte(entries[name])); err != nil {
			t.Fatalf("写入条目 %s 失败：%v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭 zip 失败：%v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("关闭文件失败：%v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败：%v", path, err)
	}
	return string(content)
}

// TestPackageThenInstallRoundTrip 打包再安装应还原出同样的文件，且包内根目录就是插件本体
// （没有多套一层目录）。
func TestPackageThenInstallRoundTrip(t *testing.T) {
	api, root := newPluginAPI(t, nil)
	source := newPluginFixture(t, t.TempDir(), "demo", "演示")

	archivePath, err := api.PackagePlugin(source, filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatalf("打包失败：%v", err)
	}
	if filepath.Ext(archivePath) != pluginPackageExtension {
		t.Errorf("应自动补上 %s 扩展名，实际 %q", pluginPackageExtension, archivePath)
	}

	// 包内条目应在根目录（可直接解压成插件目录）
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("打开插件包失败：%v", err)
	}
	names := make([]string, 0, len(reader.File))
	for _, entry := range reader.File {
		names = append(names, entry.Name)
	}
	reader.Close()
	sort.Strings(names)
	for _, expected := range []string{pluginManifestName, pluginIconName, "index.js", "assets/logo.png"} {
		if !containsString(names, expected) {
			t.Errorf("包内应含根级条目 %s，实际 %v", expected, names)
		}
	}

	id, err := api.InstallPluginArchive(archivePath)
	if err != nil {
		t.Fatalf("从插件包安装失败：%v", err)
	}
	if id != "demo" {
		t.Errorf("应以清单 id 命名，实际 %q", id)
	}
	installed := filepath.Join(root, "demo")
	if content := readFile(t, filepath.Join(installed, pluginManifestName)); !strings.Contains(content, `"id":"demo"`) {
		t.Errorf("清单内容不一致：%s", content)
	}
	if content := readFile(t, filepath.Join(installed, pluginIconName)); content != "fake-icon-bytes" {
		t.Errorf("图标内容不一致：%s", content)
	}
	if content := readFile(t, filepath.Join(installed, "assets", "logo.png")); content != "fake-png-bytes" {
		t.Errorf("子目录文件内容不一致：%s", content)
	}
}

// TestInstallPluginArchiveAcceptsWrappedDirectory 兼容"直接 zip 整个文件夹"的包：
// 包内只有一层顶层目录时剥掉它。
func TestInstallPluginArchiveAcceptsWrappedDirectory(t *testing.T) {
	api, root := newPluginAPI(t, nil)
	archivePath := filepath.Join(t.TempDir(), "wrapped.nekoex")
	writeZip(t, archivePath, map[string]string{
		"demo/plugin.yaml": `{"id":"demo","name":"包了一层","version":"1.0.0","api":"1","entry":"index.js"}`,
		"demo/index.js":  "export default () => {};",
		"demo/icon.png":  "icon-bytes",
		"demo/sub/a.txt": "nested",
		"readme.txt":     "包外的无关文件",
	})

	id, err := api.InstallPluginArchive(archivePath)
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	if id != "demo" {
		t.Errorf("id 应为 demo，实际 %q", id)
	}
	if content := readFile(t, filepath.Join(root, "demo", pluginManifestName)); !strings.Contains(content, "包了一层") {
		t.Errorf("清单未落到插件根目录：%s", content)
	}
	if content := readFile(t, filepath.Join(root, "demo", "sub", "a.txt")); content != "nested" {
		t.Errorf("子目录未解压：%s", content)
	}
	// 前缀目录之外的文件应被忽略，不能跑到插件目录里
	if _, err := os.Stat(filepath.Join(root, "demo", "readme.txt")); !os.IsNotExist(err) {
		t.Errorf("前缀之外的条目不应被解压，实际 err=%v", err)
	}
}

// TestInstallPluginArchiveRejectsZipSlip 包内路径越出插件目录时必须拒绝，且不留残骸。
func TestInstallPluginArchiveRejectsZipSlip(t *testing.T) {
	api, root := newPluginAPI(t, nil)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Dir(root)
	archivePath := filepath.Join(t.TempDir(), "evil.nekoex")
	writeZip(t, archivePath, map[string]string{
		"plugin.yaml":        `{"id":"evil","name":"evil","version":"1.0.0","api":"1","entry":"index.js"}`,
		"index.js":         "export default () => {};",
		"../escaped.txt":   "越界内容",
		"/absolute.txt":    "绝对路径",
		"C:/windows/x.txt": "盘符路径",
	})

	if _, err := api.InstallPluginArchive(archivePath); err == nil {
		t.Fatal("含越界路径的插件包应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(workspace, "escaped.txt")); !os.IsNotExist(err) {
		t.Errorf("越界文件不应被写出，实际 err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "evil")); !os.IsNotExist(err) {
		t.Errorf("失败的安装不应留下残缺目录，实际 err=%v", err)
	}
}

// TestInstallPluginArchiveRejectsBrokenPackages 缺清单 / 缺入口 / id 非法 / 同名已存在。
func TestInstallPluginArchiveRejectsBrokenPackages(t *testing.T) {
	api, root := newPluginAPI(t, nil)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	newPluginFixture(t, root, "existing", "已存在")

	cases := []struct {
		name        string
		entries     map[string]string
		wantMessage string
	}{
		{
			name:        "缺清单",
			entries:     map[string]string{"index.js": "x"},
			wantMessage: "找不到 " + pluginManifestName,
		},
		{
			name: "清单非法",
			entries: map[string]string{
				pluginManifestName: "{ nope", "index.js": "x",
			},
			wantMessage: "不是合法 YAML",
		},
		{
			name: "id 非法",
			entries: map[string]string{
				pluginManifestName: `{"id":"../evil","api":"1"}`, "index.js": "x",
			},
			wantMessage: "非法字符",
		},
		{
			name: "缺入口文件",
			entries: map[string]string{
				pluginManifestName: `{"id":"noentry","api":"1","entry":"index.js"}`,
				"other.js":         "x",
			},
			wantMessage: "找不到入口文件",
		},
		{
			name: "同名插件已存在",
			entries: map[string]string{
				pluginManifestName: `{"id":"existing","api":"1"}`, "index.js": "x",
			},
			wantMessage: "已存在同名插件",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "case.nekoex")
			writeZip(t, archivePath, testCase.entries)
			_, err := api.InstallPluginArchive(archivePath)
			if err == nil {
				t.Fatal("应返回错误")
			}
			if !strings.Contains(err.Error(), testCase.wantMessage) {
				t.Errorf("错误信息应包含 %q，实际 %q", testCase.wantMessage, err.Error())
			}
		})
	}
}

// TestInstallPluginArchiveRejectsNonArchive 不是 zip 的文件要给出明确错误。
func TestInstallPluginArchiveRejectsNonArchive(t *testing.T) {
	api, _ := newPluginAPI(t, nil)
	plainPath := filepath.Join(t.TempDir(), "not-a-zip.nekoex")
	writeFile(t, plainPath, "这不是压缩包")

	if _, err := api.InstallPluginArchive(plainPath); err == nil ||
		!strings.Contains(err.Error(), "不是有效的 zip") {
		t.Errorf("应提示不是有效 zip，实际 %v", err)
	}
	if _, err := api.InstallPluginArchive(filepath.Join(t.TempDir(), "absent.nekoex")); err == nil {
		t.Error("不存在的插件包应报错")
	}
}

// TestListPluginsResolvesIcon 图标解析：缺省 icon.png、清单可改写、越界路径不接受。
func TestListPluginsResolvesIcon(t *testing.T) {
	api, root := newPluginAPI(t, nil)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1) 缺省 icon.png
	newPluginFixture(t, root, "withicon", "带图标")

	// 2) 清单指定其它图标文件
	custom := filepath.Join(root, "customicon")
	if err := os.MkdirAll(filepath.Join(custom, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(custom, pluginManifestName),
		`{"id":"customicon","name":"自定义图标","version":"1.0.0","api":"1","icon":"assets/pic.png"}`)
	writeFile(t, filepath.Join(custom, "index.js"), "export default () => {};")
	writeFile(t, filepath.Join(custom, "assets", "pic.png"), "pic")

	// 3) 没有图标文件
	noicon := filepath.Join(root, "noicon")
	if err := os.MkdirAll(noicon, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(noicon, pluginManifestName), `{"id":"noicon","api":"1"}`)
	writeFile(t, filepath.Join(noicon, "index.js"), "x")

	// 4) 清单把图标指向目录之外
	evil := filepath.Join(root, "evilicon")
	if err := os.MkdirAll(evil, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(evil, pluginManifestName), `{"id":"evilicon","api":"1","icon":"../../secret.png"}`)
	writeFile(t, filepath.Join(evil, "index.js"), "x")

	infos, err := api.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins 失败：%v", err)
	}
	byID := make(map[string]PluginInfo, len(infos))
	for _, info := range infos {
		byID[info.ID] = info
	}

	if byID["withicon"].IconFile != pluginIconName {
		t.Errorf("应解析出缺省图标 %s，实际 %q", pluginIconName, byID["withicon"].IconFile)
	}
	if byID["customicon"].IconFile != "assets/pic.png" {
		t.Errorf("应使用清单指定的图标，实际 %q", byID["customicon"].IconFile)
	}
	if byID["noicon"].IconFile != "" {
		t.Errorf("没有图标文件时应为空，实际 %q", byID["noicon"].IconFile)
	}
	if byID["evilicon"].IconFile != "" {
		t.Errorf("越界图标路径应被拒绝，实际 %q", byID["evilicon"].IconFile)
	}
	if byID["withicon"].Entry != "index.js" {
		t.Errorf("入口应回落到缺省 index.js，实际 %q", byID["withicon"].Entry)
	}
}

// TestPackagePluginValidations 打包的各类拒绝场景。
func TestPackagePluginValidations(t *testing.T) {
	api, _ := newPluginAPI(t, nil)
	target := filepath.Join(t.TempDir(), "out.nekoex")

	// 缺清单
	emptyDirectory := t.TempDir()
	if _, err := api.PackagePlugin(emptyDirectory, target); err == nil ||
		!strings.Contains(err.Error(), "缺少 "+pluginManifestName) {
		t.Errorf("缺清单应报错，实际 %v", err)
	}

	// 缺入口文件
	source := newPluginFixture(t, t.TempDir(), "demo", "演示")
	if err := os.Remove(filepath.Join(source, "index.js")); err != nil {
		t.Fatal(err)
	}
	if _, err := api.PackagePlugin(source, target); err == nil ||
		!strings.Contains(err.Error(), "找不到入口文件") {
		t.Errorf("缺入口应报错，实际 %v", err)
	}

	// 打包目标落在源码目录内
	if _, err := api.PackagePlugin(source, filepath.Join(source, "demo.nekoex")); err == nil {
		t.Error("目标写在源码目录内应被拒绝")
	}

	// 目录不存在
	if _, err := api.PackagePlugin(filepath.Join(t.TempDir(), "absent"), target); err == nil {
		t.Error("目录不存在应报错")
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
