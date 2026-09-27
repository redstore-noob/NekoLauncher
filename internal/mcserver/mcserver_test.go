package mcserver

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useTempRoot 把服务器根目录指到临时目录，返回根路径。
func useTempRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	serverRootOverride = &root
	t.Cleanup(func() { serverRootOverride = nil })

	return root
}

func TestWriteEula(t *testing.T) {
	dir := t.TempDir()
	if err := writeEula(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "eula.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "eula=true") {
		t.Fatalf("eula.txt 应包含 eula=true：%q", data)
	}
}

func TestSanitizeServerName(t *testing.T) {
	cases := map[string]string{
		"我的服务器":      "我的服务器",
		"  spaced  ": "spaced",
		"a/b\\c:d*e": "a_b_c_d_e",
		"..":         "",
	}
	for input, want := range cases {
		if got := sanitizeServerName(input); got != want {
			t.Errorf("sanitizeServerName(%q) = %q, want %q", input, got, want)
		}
	}
}

// newPropertiesFixture 造一台带 server.properties 的测试服务器。
func newPropertiesFixture(t *testing.T) (root, id, original string) {
	t.Helper()
	root = useTempRoot(t)
	id = "srv"
	if err := os.MkdirAll(filepath.Join(root, id), 0o755); err != nil {
		t.Fatal(err)
	}
	original = "server-port=25565\n# a comment\nmotd=A minecraft server\nmax-players=20\n"
	if err := os.WriteFile(
		filepath.Join(root, id, "server.properties"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	return root, id, original
}

func TestPropertiesRoundTrip(t *testing.T) {
	_, id, _ := newPropertiesFixture(t)

	got, err := GetServerProperties(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("应有 3 条键值，得到 %d：%+v", len(got), got)
	}
	if got[0].Key != "server-port" || got[0].Value != "25565" {
		t.Fatalf("首条不符：%+v", got[0])
	}

	// 修改一个 + 新增一个，注释与其余键应保留
	err = SetServerProperties(id, []Property{
		{Key: "motd", Value: "neko server"},
		{Key: "white-list", Value: "false"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(
		ServerRootDirectory(), id, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"# a comment",
		"motd=neko server",
		"white-list=false",
		"server-port=25565",
		"max-players=20",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("保存后缺少 %q：\n%s", want, text)
		}
	}
}

func TestImportServerWorldRejectsTraversal(t *testing.T) {
	root := useTempRoot(t)
	id := "srv"
	if err := os.MkdirAll(filepath.Join(root, id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, id, "server.properties"),
		[]byte("level-name=world\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	evil := filepath.Join(t.TempDir(), "evil.zip")
	archive, err := os.Create(evil)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	entry, err := writer.Create("../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("x"))
	writer.Close()
	archive.Close()

	if err := ImportServerWorld(id, evil); err == nil {
		t.Fatal("越界路径应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(root, "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("越界文件不应被写出")
	}
}

func TestImportServerWorldExtractsLevelDat(t *testing.T) {
	root := useTempRoot(t)
	id := "srv"
	if err := os.MkdirAll(filepath.Join(root, id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, id, "server.properties"),
		[]byte("level-name=world\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	world := filepath.Join(t.TempDir(), "world.zip")
	archive, err := os.Create(world)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	for _, name := range []string{"world/level.dat", "world/region/r.0.0.mca"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(name)); err != nil {
			t.Fatal(err)
		}
	}
	writer.Close()
	archive.Close()

	if err := ImportServerWorld(id, world); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, id, "world", "region", "r.0.0.mca"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "world/region/r.0.0.mca" {
		t.Fatalf("存档内容损坏：%q", data)
	}
}

func TestCreateServerRequiresEULA(t *testing.T) {
	useTempRoot(t)

	_, err := CreateServer(context.Background(), CreateOptions{
		Name: "test", Core: CoreVanilla, MCVersion: "1.21.4",
		AcceptEULA: false,
	})
	if err == nil {
		t.Fatal("未同意 EULA 应拒绝创建")
	}
}

func TestImportModCoreCheck(t *testing.T) {
	root := useTempRoot(t)
	id := "srv"
	if err := os.MkdirAll(filepath.Join(root, id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveServerConfig(filepath.Join(root, id), &ServerConfig{
		ID: id, Name: id, Core: CoreVanilla,
	}); err != nil {
		t.Fatal(err)
	}

	jar := filepath.Join(t.TempDir(), "m.jar")
	if err := os.WriteFile(jar, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ImportServerMod(id, jar); err == nil {
		t.Fatal("Vanilla 不应允许导入模组")
	}
}
