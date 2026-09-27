package content

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// newSaveFixture 造一个存档目录（含 region 子目录与导出时应丢弃的 session.lock）。
func newSaveFixture(t *testing.T) (savesDirectory, worldDirectory string) {
	t.Helper()
	savesDirectory = t.TempDir()
	worldDirectory = filepath.Join(savesDirectory, "MyWorld")
	writeFile := func(rel, content string) {
		full := filepath.Join(worldDirectory, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("level.dat", "level-data")
	writeFile("region/r.0.0.mca", "region-data")
	writeFile("session.lock", "lock")
	return savesDirectory, worldDirectory
}

func TestExportSaveDropsSessionLock(t *testing.T) {
	_, worldDirectory := newSaveFixture(t)
	target := filepath.Join(t.TempDir(), "out.zip")

	if _, err := ExportSave(context.Background(), worldDirectory, target); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(target)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	names := map[string]bool{}
	for _, entry := range reader.File {
		names[entry.Name] = true
	}
	if !names["MyWorld/level.dat"] {
		t.Fatalf("存档包缺少 level.dat：%v", names)
	}
	if names["MyWorld/session.lock"] {
		t.Fatal("session.lock 不应被打包")
	}
}

func TestImportSaveRoundTrip(t *testing.T) {
	savesDirectory, worldDirectory := newSaveFixture(t)
	archive := filepath.Join(t.TempDir(), "MyWorld.zip")
	if _, err := ExportSave(context.Background(), worldDirectory, archive); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(worldDirectory); err != nil {
		t.Fatal(err)
	}

	imported, err := ImportSave(context.Background(), archive, savesDirectory)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(imported, "region", "r.0.0.mca"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "region-data" {
		t.Fatalf("导入内容损坏：%q", data)
	}
}

func TestImportSaveAvoidsOverwrite(t *testing.T) {
	savesDirectory, worldDirectory := newSaveFixture(t)
	archive := filepath.Join(t.TempDir(), "MyWorld.zip")
	if _, err := ExportSave(context.Background(), worldDirectory, archive); err != nil {
		t.Fatal(err)
	}

	imported, err := ImportSave(context.Background(), archive, savesDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(imported) != "MyWorld-1" {
		t.Fatalf("同名存档应追加序号，得到：%s", filepath.Base(imported))
	}
	if _, err := os.Stat(filepath.Join(worldDirectory, "level.dat")); err != nil {
		t.Fatalf("原有存档被破坏：%v", err)
	}
}

func TestImportSaveRejectsTraversal(t *testing.T) {
	savesDirectory := t.TempDir()
	archive := filepath.Join(t.TempDir(), "evil.zip")

	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := ImportSave(context.Background(), archive, savesDirectory); err == nil {
		t.Fatal("越界路径应被拒绝")
	}
}
