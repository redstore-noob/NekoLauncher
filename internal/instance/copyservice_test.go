package instance

// 实例复制端到端测试：真实目录 + 真实配置存储（经 TestMain 隔离到临时目录）。
// 防的回归：副本 id 没补丁（启动器扫描不到/游戏按旧名找会话目录）、
// 副本的 inheritsFrom 被错误改写（应保持指向原父版本）、档案没克隆。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekolauncher/internal/config"
)

// newCopyFixture 造一个可复制的实例目录：
//   - versions/src/（vanilla 版本，含 json + jar + 嵌套资源）
//   - versions/child/（loader 版本，inheritsFrom 指向 src）
func newCopyFixture(t *testing.T) string {
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

	write("versions/src/src.json", `{"id":"src","assets":"1.0","minecraftArguments":"--demo"}`)
	write("versions/src/src.jar", "fake-client-jar")
	write("versions/src/libraries/extra/lwjgl.jar", "fake-library")
	write("versions/child/child.json", `{"id":"child","inheritsFrom":"src"}`)
	return root
}

// switchTempStorage 把配置存储切到临时目录（档案断言的前置条件）。
func switchTempStorage(t *testing.T) {
	t.Helper()
	original := config.StorageDirectory()
	if err := config.SetStorageDirectory(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.SetStorageDirectory(original) })
}

func readJSONFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败：%v", path, err)
	}
	return string(data)
}

// TestCopyVersionEndToEnd 复制全流程：目录克隆 + id 补丁 + 文件改名 + 档案克隆。
func TestCopyVersionEndToEnd(t *testing.T) {
	switchTempStorage(t)
	root := newCopyFixture(t)

	// 给源实例存一份可辨认的档案（复制后应原样克隆到副本名下）
	profile := config.NewGameVersionProfile()
	profile.MinecraftDirectory = root
	profile.VersionId = "src"
	profile.MaximumMemoryMb = 8192
	profile.AdditionalJvmArguments = []string{"-XX:+UseG1GC"}
	if !config.Save(profile) {
		t.Fatal("保存源实例档案失败")
	}

	newID, err := CopyVersion(context.Background(), root, "src", "src-copy")
	if err != nil {
		t.Fatalf("复制失败：%v", err)
	}
	if newID != "src-copy" {
		t.Fatalf("返回的新版本 ID = %q，期望 src-copy", newID)
	}

	// 目录与文件：副本 json/jar 已改名，嵌套资源完整拷贝
	copyJSON := filepath.Join(root, "versions", "src-copy", "src-copy.json")
	if _, err := os.Stat(copyJSON); err != nil {
		t.Fatalf("副本版本 JSON 未改名（应在 versions/src-copy/src-copy.json）：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "src-copy", "src.json")); !os.IsNotExist(err) {
		t.Error("副本目录里不应残留旧文件名的版本 JSON")
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "src-copy", "src-copy.jar")); err != nil {
		t.Error("副本客户端 jar 未改名")
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "src-copy", "libraries", "extra", "lwjgl.jar")); err != nil {
		t.Error("嵌套依赖目录没有被完整复制")
	}

	// 副本 JSON：id 必须是新名字（否则游戏按旧 id 找会话目录），其余字段原样
	document := readJSONFile(t, copyJSON)
	if !strings.Contains(document, `"id": "src-copy"`) {
		t.Errorf("副本 JSON 的 id 没有补丁：%s", document)
	}
	if !strings.Contains(document, "minecraftArguments") {
		t.Error("副本 JSON 丢失了原有字段")
	}

	// 副本的 inheritsFrom 引用必须保持原样指向 src（原版本没有被改名，
	// 兄弟版本与副本都不应被触碰）
	childDocument := readJSONFile(t, filepath.Join(root, "versions", "child", "child.json"))
	if childDocument != `{"id":"child","inheritsFrom":"src"}` {
		t.Errorf("兄弟版本 JSON 被误改：%s", childDocument)
	}

	// 原实例完全不受影响
	originalJSON := readJSONFile(t, filepath.Join(root, "versions", "src", "src.json"))
	if originalJSON != `{"id":"src","assets":"1.0","minecraftArguments":"--demo"}` {
		t.Errorf("原版本 JSON 被改动：%s", originalJSON)
	}

	// 档案克隆：副本继承内存/JVM 设置，且指向副本自己的版本 ID
	cloned := config.Get(root, "src-copy")
	if cloned.MaximumMemoryMb != 8192 {
		t.Errorf("副本档案内存 = %d，期望克隆自源实例的 8192", cloned.MaximumMemoryMb)
	}
	if len(cloned.AdditionalJvmArguments) != 1 || cloned.AdditionalJvmArguments[0] != "-XX:+UseG1GC" {
		t.Errorf("副本档案 JVM 参数未克隆：%v", cloned.AdditionalJvmArguments)
	}
	if cloned.VersionId != "src-copy" {
		t.Errorf("副本档案 VersionId = %q，期望 src-copy", cloned.VersionId)
	}
}

// TestCopyVersionRejectsInvalidTargets 非法目标：同名、已存在、非法字符、外部实例。
func TestCopyVersionRejectsInvalidTargets(t *testing.T) {
	switchTempStorage(t)
	root := newCopyFixture(t)

	if _, err := CopyVersion(context.Background(), root, "src", "SRC"); err == nil {
		t.Error("与原版本仅大小写不同也应视为同名并拒绝")
	}
	if _, err := CopyVersion(context.Background(), root, "src", "child"); err == nil {
		t.Error("目标版本已存在时应拒绝")
	}
	if _, err := CopyVersion(context.Background(), root, "src", "a/b"); err == nil {
		t.Error("包含路径分隔符的名字应拒绝")
	}
	if _, err := CopyVersion(context.Background(), root, "不存在", "whatever"); err == nil {
		t.Error("原版本不存在时应拒绝")
	}

	// 失败的复制不能留下半成品目录
	if _, err := os.Stat(filepath.Join(root, "versions", "a")); !os.IsNotExist(err) {
		t.Error("被拒绝的复制不应创建目标目录")
	}
}
