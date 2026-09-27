package bindings

// 资源包工程读写：导出（png / text / base64 三通道）与导入（目录 / zip、顶层目录剥离）。

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestExportResourcePackChannels(t *testing.T) {
	api := &SystemAPI{}
	target := filepath.Join(t.TempDir(), "pack") // 无扩展名：应自动补 .zip

	pngB64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(writePngFixture)
	binary := []byte{0x4F, 0x67, 0x67, 0x53, 0x00, 0x02, 0x00} // 含 NUL 的伪 ogg
	output, err := api.ExportResourcePack(target, []ResourcePackFile{
		{Path: "pack.mcmeta", Text: `{"pack":{"pack_format":34,"description":"x"}}`},
		{Path: "assets/minecraft/textures/block/a.png", PngBase64: pngB64},
		{Path: "assets/minecraft/sounds/a.ogg", Base64: base64.StdEncoding.EncodeToString(binary)},
		{Path: "empty.txt", Kind: "text", Text: ""},
	})
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	if filepath.Ext(output) != resourcePackExtension {
		t.Fatalf("应自动补 .zip：%s", output)
	}

	reader, err := zip.OpenReader(output)
	if err != nil {
		t.Fatalf("打开导出包失败：%v", err)
	}
	defer reader.Close()

	found := map[string][]byte{}
	for _, entry := range reader.File {
		raw, err := readZipEntryLimited(entry, resourcePackMaxBinaryBytes)
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", entry.Name, err)
		}
		found[entry.Name] = raw
	}
	if !bytes.Equal(found["assets/minecraft/textures/block/a.png"], writePngFixture) {
		t.Error("PNG 通道内容不符")
	}
	if !bytes.Equal(found["assets/minecraft/sounds/a.ogg"], binary) {
		t.Error("二进制通道内容不符")
	}
	if len(found["pack.mcmeta"]) == 0 {
		t.Error("文本通道内容缺失")
	}
	if data, ok := found["empty.txt"]; !ok || len(data) != 0 {
		t.Error("空文本文件应作为零字节文件写出")
	}
}

func TestExportResourcePackRejects(t *testing.T) {
	api := &SystemAPI{}
	base := t.TempDir()

	cases := []struct {
		name  string
		files []ResourcePackFile
	}{
		{"空文件集", nil},
		{"重复路径", []ResourcePackFile{
			{Path: "a.txt", Text: "1"},
			{Path: "a.txt", Text: "2"},
		}},
		{"路径越界", []ResourcePackFile{{Path: "../evil.txt", Text: "x"}}},
		{"缺少内容", []ResourcePackFile{{Path: "a.txt"}}},
		{"非 PNG 冒充", []ResourcePackFile{{Path: "a.png", PngBase64: base64.StdEncoding.EncodeToString([]byte("nope"))}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := api.ExportResourcePack(filepath.Join(base, "out.zip"), tc.files); err == nil {
				t.Error("应当报错")
			}
		})
	}
}

func TestImportResourcePackDirectory(t *testing.T) {
	api := &SystemAPI{}
	root := t.TempDir()

	writeFixture(t, filepath.Join(root, "pack.mcmeta"), []byte(`{"pack":{"pack_format":34}}`))
	writeFixture(t, filepath.Join(root, "assets/minecraft/lang/zh_cn.json"), []byte(`{"a":"b"}`))
	writeFixture(t, filepath.Join(root, "assets/minecraft/textures/block/stone.png"), writePngFixture)
	writeFixture(t, filepath.Join(root, "assets/minecraft/sounds/tick.ogg"), []byte{1, 2, 0, 3})

	project, err := api.ImportResourcePack(root)
	if err != nil {
		t.Fatalf("导入目录失败：%v", err)
	}
	if project.Name == "" || project.Root != root {
		t.Errorf("工程元信息不符：%+v", project)
	}
	kinds := map[string]string{}
	for _, file := range project.Files {
		kinds[file.Path] = file.Kind
	}
	want := map[string]string{
		"pack.mcmeta":                               "text",
		"assets/minecraft/lang/zh_cn.json":          "text",
		"assets/minecraft/textures/block/stone.png": "png",
		"assets/minecraft/sounds/tick.ogg":          "binary",
	}
	for path, kind := range want {
		if kinds[path] != kind {
			t.Errorf("%s 类型应为 %s，实际 %s", path, kind, kinds[path])
		}
	}
}

func TestImportResourcePackZipStripsTopDirectory(t *testing.T) {
	api := &SystemAPI{}
	archive := filepath.Join(t.TempDir(), "fancy.zip")

	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entries := map[string][]byte{
		"fancy/pack.mcmeta":                 []byte(`{"pack":{"pack_format":34}}`),
		"fancy/pack.png":                    writePngFixture,
		"fancy/assets/minecraft/lang/x.txt": []byte("hello"),
	}
	for name, data := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	project, err := api.ImportResourcePack(archive)
	if err != nil {
		t.Fatalf("导入 zip 失败：%v", err)
	}
	if project.Name != "fancy" {
		t.Errorf("包名应为 fancy，实际 %s", project.Name)
	}
	paths := map[string]bool{}
	for _, f := range project.Files {
		paths[f.Path] = true
		if len(f.Path) >= 6 && f.Path[:6] == "fancy/" {
			t.Errorf("顶层目录未剥离：%s", f.Path)
		}
	}
	for _, want := range []string{"pack.mcmeta", "pack.png", "assets/minecraft/lang/x.txt"} {
		if !paths[want] {
			t.Errorf("缺少条目 %s", want)
		}
	}
}

// writeFixture 写一个小文件（自动建父目录）。
func writeFixture(t *testing.T, target string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
