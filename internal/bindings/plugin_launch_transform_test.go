package bindings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePlugin 在 root 下造一个插件目录（含 plugin.yaml）。
func writePlugin(t *testing.T, root, id, manifest string) {
	t.Helper()

	directory := filepath.Join(root, id)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("建插件目录失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("写清单失败：%v", err)
	}
}

func newPluginAPIForTest(t *testing.T) (*PluginAPI, string) {
	t.Helper()

	root := t.TempDir()
	return &PluginAPI{root: root, disabled: staticDisabledStore{}}, root
}

// staticDisabledStore 测试用停用列表。
type staticDisabledStore struct{ ids map[string]bool }

func (s staticDisabledStore) IDs() map[string]bool { return s.ids }
func (s staticDisabledStore) Set(string, bool)     {}

// ---------------------------------------------------------------------------
// 基本转换
// ---------------------------------------------------------------------------

func TestLaunchTransformEmptyWhenNoPlugins(t *testing.T) {
	api, _ := newPluginAPIForTest(t)

	transform, warnings := api.launchTransform()
	if transform == nil {
		t.Fatal("必须返回非 nil 变换（nil 会被启动管线当成未注入）")
	}
	if len(transform.AppendJvmArguments) != 0 || len(transform.AppendGameArguments) != 0 {
		t.Errorf("没有插件时不应有参数：%+v", transform)
	}
	if len(warnings) != 0 {
		t.Errorf("没有插件时不应有警告：%v", warnings)
	}
}

func TestLaunchTransformCollectsDeclaredArguments(t *testing.T) {
	api, root := newPluginAPIForTest(t)
	writePlugin(t, root, "demo", `
id: demo
name: 演示
version: "1.0.0"
api: "1"
capabilities:
  launch-transform: true
launchTransform:
  appendJvmArguments:
    - "-Ddemo.enabled=true"
  appendGameArguments:
    - "--demo-flag"
`)

	transform, warnings := api.launchTransform()
	if len(warnings) != 0 {
		t.Fatalf("不应有警告：%v", warnings)
	}
	if len(transform.AppendJvmArguments) != 1 ||
		transform.AppendJvmArguments[0] != "-Ddemo.enabled=true" {
		t.Errorf("JVM 参数 = %v", transform.AppendJvmArguments)
	}
	if len(transform.AppendGameArguments) != 1 ||
		transform.AppendGameArguments[0] != "--demo-flag" {
		t.Errorf("游戏参数 = %v", transform.AppendGameArguments)
	}
}

func TestLaunchTransformDeterministicOrder(t *testing.T) {
	api, root := newPluginAPIForTest(t)
	for _, id := range []string{"zeta", "alpha", "mid"} {
		writePlugin(t, root, id, `
id: `+id+`
name: `+id+`
version: "1.0.0"
api: "1"
capabilities:
  launch-transform: true
launchTransform:
  appendJvmArguments:
    - "-D`+id+`=1"
`)
	}

	first, _ := api.launchTransform()
	second, _ := api.launchTransform()
	if len(first.AppendJvmArguments) != 3 {
		t.Fatalf("参数数 = %d，期望 3", len(first.AppendJvmArguments))
	}
	// 两次调用必须得到同一顺序（用户重启启动器不该看到参数顺序漂移）
	for index := range first.AppendJvmArguments {
		if first.AppendJvmArguments[index] != second.AppendJvmArguments[index] {
			t.Fatalf("顺序不稳定：%v vs %v",
				first.AppendJvmArguments, second.AppendJvmArguments)
		}
	}
	// os.ReadDir 已按名排序：alpha, mid, zeta
	want := []string{"-Dalpha=1", "-Dmid=1", "-Dzeta=1"}
	for index := range want {
		if first.AppendJvmArguments[index] != want[index] {
			t.Errorf("顺序 = %v，期望 %v", first.AppendJvmArguments, want)
		}
	}
}

// ---------------------------------------------------------------------------
// 权限：声明制契约
// ---------------------------------------------------------------------------

func TestLaunchTransformRequiresCapability(t *testing.T) {
	api, root := newPluginAPIForTest(t)
	writePlugin(t, root, "sneaky", `
id: sneaky
name: 越权
version: "1.0.0"
api: "1"
launchTransform:
  appendJvmArguments:
    - "-Dmalicious=true"
`)

	transform, warnings := api.launchTransform()
	if len(transform.AppendJvmArguments) != 0 {
		t.Fatalf("未声明权限却生效了：%v", transform.AppendJvmArguments)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "launch-transform") {
		t.Fatalf("必须给出缺权限的明确警告，实际：%v", warnings)
	}
}

func TestLaunchTransformCapabilityFalseIsIgnored(t *testing.T) {
	api, root := newPluginAPIForTest(t)
	writePlugin(t, root, "off", `
id: off
name: 显式关闭
version: "1.0.0"
api: "1"
capabilities:
  launch-transform: false
launchTransform:
  appendJvmArguments:
    - "-Dnope=true"
`)

	transform, _ := api.launchTransform()
	if len(transform.AppendJvmArguments) != 0 {
		t.Errorf("capabilities 显式为 false 时必须忽略：%v", transform.AppendJvmArguments)
	}
}

func TestLaunchTransformSkipsDisabledPlugins(t *testing.T) {
	api, root := newPluginAPIForTest(t)
	writePlugin(t, root, "disabled-one", `
id: disabled-one
name: 已停用
version: "1.0.0"
api: "1"
capabilities:
  launch-transform: true
launchTransform:
  appendJvmArguments:
    - "-DshouldNotAppear=true"
`)
	// 让这个插件处于停用状态
	api.disabled = staticDisabledStore{ids: map[string]bool{"disabled-one": true}}

	transform, _ := api.launchTransform()
	if len(transform.AppendJvmArguments) != 0 {
		t.Errorf("停用的插件不应贡献参数：%v", transform.AppendJvmArguments)
	}
}

// ---------------------------------------------------------------------------
// 参数体检
// ---------------------------------------------------------------------------

func TestLaunchTransformDropsEmptyAndInvalidArguments(t *testing.T) {
	api, root := newPluginAPIForTest(t)
	writePlugin(t, root, "sloppy", "id: sloppy\n"+
		"name: 粗心\n"+
		"version: \"1.0.0\"\n"+
		"api: \"1\"\n"+
		"capabilities:\n"+
		"  launch-transform: true\n"+
		"launchTransform:\n"+
		"  appendJvmArguments:\n"+
		"    - \"-Dgood=true\"\n"+
		"    - \"\"\n"+
		"    - \"   \"\n"+
		"    - \"-Dnull\\0byte=true\"\n")

	transform, warnings := api.launchTransform()
	if len(transform.AppendJvmArguments) != 1 ||
		transform.AppendJvmArguments[0] != "-Dgood=true" {
		t.Fatalf("非法项未被过滤：%v", transform.AppendJvmArguments)
	}
	if len(warnings) != 3 {
		t.Errorf("每个被丢弃的项都要有警告，实际 %d 条：%v", len(warnings), warnings)
	}
}

func TestLaunchTransformTrimsArguments(t *testing.T) {
	api, root := newPluginAPIForTest(t)
	writePlugin(t, root, "spaces", `
id: spaces
name: 空格
version: "1.0.0"
api: "1"
capabilities:
  launch-transform: true
launchTransform:
  appendJvmArguments:
    - "  -Dtrimmed=true  "
`)

	transform, _ := api.launchTransform()
	if len(transform.AppendJvmArguments) != 1 ||
		transform.AppendJvmArguments[0] != "-Dtrimmed=true" {
		t.Errorf("参数未被 trim：%q", transform.AppendJvmArguments)
	}
}

// ---------------------------------------------------------------------------
// 健壮性：坏插件不能拖垮启动
// ---------------------------------------------------------------------------

func TestLaunchTransformSurvivesBrokenManifest(t *testing.T) {
	api, root := newPluginAPIForTest(t)
	// 坏 YAML
	writePlugin(t, root, "broken", "id: broken\n  bad: [unclosed\n")
	// 正常插件
	writePlugin(t, root, "good", `
id: good
name: 正常
version: "1.0.0"
api: "1"
capabilities:
  launch-transform: true
launchTransform:
  appendJvmArguments:
    - "-Dgood=true"
`)

	transform, _ := api.launchTransform()
	if len(transform.AppendJvmArguments) != 1 ||
		transform.AppendJvmArguments[0] != "-Dgood=true" {
		t.Fatalf("坏插件影响了正常插件：%v", transform.AppendJvmArguments)
	}
}

func TestLaunchTransformMissingDirectoryIsNotAnError(t *testing.T) {
	api := &PluginAPI{
		root:     filepath.Join(t.TempDir(), "does-not-exist"),
		disabled: staticDisabledStore{},
	}

	transform, warnings := api.launchTransform()
	if transform == nil {
		t.Fatal("目录不存在也必须返回可用变换")
	}
	if len(warnings) != 0 {
		t.Errorf("目录不存在不该产生警告：%v", warnings)
	}
}

func TestLaunchTransformIgnoresFilesInPluginRoot(t *testing.T) {
	api, root := newPluginAPIForTest(t)
	// 插件根目录下的散落文件不应被当成插件
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}

	transform, warnings := api.launchTransform()
	if len(transform.AppendJvmArguments) != 0 || len(warnings) != 0 {
		t.Errorf("散落文件被误当成插件：%+v / %v", transform, warnings)
	}
}
