package bindings

// DeleteInstance 的行为：正常删除、路径穿越拒绝、目标缺失、不影响邻居。

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func newGameDirectoryFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("versions/KeepMe/KeepMe.json", `{"id":"KeepMe"}`)
	write("versions/MyPack/MyPack.json", `{"id":"MyPack"}`)
	write("versions/MyPack/mods/mod.jar", "fake")

	return root
}

func TestDeleteInstanceRemovesOnlyTarget(t *testing.T) {
	api := &InstanceAPI{}
	root := newGameDirectoryFixture(t)

	if err := api.DeleteInstance("MyPack", root); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "MyPack")); !os.IsNotExist(err) {
		t.Error("实例目录应已被删除")
	}
	// 邻居与 versions 根必须完好
	if _, err := os.Stat(filepath.Join(root, "versions", "KeepMe")); err != nil {
		t.Errorf("邻居实例被误删：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "versions")); err != nil {
		t.Errorf("versions 根目录被误删：%v", err)
	}
}

func TestDeleteInstanceRejectsUnsafeIds(t *testing.T) {
	api := &InstanceAPI{}
	root := newGameDirectoryFixture(t)

	for _, id := range []string{"..", ".", "a/b", `a\b`} {
		if err := api.DeleteInstance(id, root); err == nil {
			t.Errorf("不安全 id %q 应被拒绝", id)
		}
	}
	if err := api.DeleteInstance("", root); err == nil {
		t.Error("空 id 应被拒绝")
	}
	if err := api.DeleteInstance("MyPack", ""); err == nil {
		t.Error("空游戏目录应被拒绝")
	}
	// 上述尝试都不应碰掉真实实例
	if _, err := os.Stat(filepath.Join(root, "versions", "MyPack")); err != nil {
		t.Errorf("被拒绝的删除不应影响实例：%v", err)
	}
}

// 幂等：删除不存在的实例视为成功（列表里的幽灵条目点击删除必须能清掉）。
func TestDeleteInstanceMissingTarget(t *testing.T) {
	api := &InstanceAPI{}
	root := newGameDirectoryFixture(t)

	if err := api.DeleteInstance("ghost", root); err != nil {
		t.Errorf("不存在的实例应视为删除成功，得到：%v", err)
	}
}

// 回归：目录名尾部带空格/点时，常规路径 API 会误报不存在，导致清不掉。
func TestDeleteInstanceRemovesTrailingDotName(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("尾部点/空格是 Windows 路径归一化行为")
	}
	api := &InstanceAPI{}
	root := t.TempDir()
	target := filepath.Join(root, "versions", "1.20.1 ")
	// 常规路径 API 创建/写入这种目录名会被 Windows 剥掉尾部空格，必须走 \\?\
	extTarget := extendedPath(target)
	if err := os.MkdirAll(extTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extTarget, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 常规路径 Stat 必须误报不存在，否则本测试没有针对性
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Skipf("此系统未复现尾部空格的 Stat 误报（err=%v）", err)
	}

	if err := api.DeleteInstance("1.20.1 ", root); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	if _, err := os.Stat(extendedPath(target)); !os.IsNotExist(err) {
		t.Error("尾部带空格的实例目录应被删除")
	}
}

// 回归：只读文件与超过 MAX_PATH 的深层嵌套不应造成残留。
func TestDeleteInstanceRemovesReadOnlyAndLongPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("只读位与 MAX_PATH 均为 Windows 行为")
	}
	api := &InstanceAPI{}
	root := t.TempDir()
	target := filepath.Join(root, "versions", "StuckPack")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	// 只读文件 + 只读目录
	roFile := filepath.Join(target, "mods", "ro.jar")
	if err := os.MkdirAll(filepath.Dir(roFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(roFile, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(roFile), 0o555); err != nil {
		t.Fatal(err)
	}

	// 超过 260 字符的深层路径
	deep := filepath.Join(target, "libraries")
	for i := 0; i < 24; i++ {
		deep = filepath.Join(deep, "verylongsegmentname-without-spaces-0000000000")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if len(deep) <= 260 {
		t.Skipf("临时目录本身太短，构造不出长路径（%d）", len(deep))
	}
	if err := os.WriteFile(filepath.Join(deep, "a.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := api.DeleteInstance("StuckPack", root); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("只读/长路径实例目录应被完整删除，残留：%v", err)
	}
}
