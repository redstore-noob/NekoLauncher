package bindings

// 资源包草稿的写入 / 读取 / 清除：不应污染真实存储目录。

import (
	"encoding/base64"
	"testing"

	"nekolauncher/internal/config"
)

func TestResourcePackDraftRoundTrip(t *testing.T) {
	old := config.StorageDirectory()
	if err := config.SetStorageDirectory(t.TempDir()); err != nil {
		t.Fatalf("设置存储目录失败：%v", err)
	}
	defer config.SetStorageDirectory(old)

	api := &SystemAPI{}

	// 初始无草稿：Found=false 且不报错
	if draft, err := api.LoadResourcePackDraft(); err != nil || draft.Found {
		t.Fatalf("空草稿应 Found=false：%+v %v", draft, err)
	}

	files := []ResourcePackFile{
		{Path: "pack.mcmeta", Kind: "text", Text: `{"pack":{"pack_format":34}}`},
		{
			Path:      "assets/minecraft/textures/block/a.png",
			Kind:      "png",
			PngBase64: base64.StdEncoding.EncodeToString(writePngFixture),
		},
		{Path: "assets/minecraft/sounds/a.ogg", Kind: "binary", Base64: "AAEC"},
	}
	if err := api.SaveResourcePackDraft(ResourcePackDraft{
		Name:    "我的资源包",
		SavedAt: "2026-09-20T12:00:00Z",
		Files:   files,
	}); err != nil {
		t.Fatalf("保存草稿失败：%v", err)
	}

	draft, err := api.LoadResourcePackDraft()
	if err != nil {
		t.Fatalf("读取草稿失败：%v", err)
	}
	if !draft.Found || draft.Name != "我的资源包" || len(draft.Files) != 3 {
		t.Fatalf("草稿内容不符：%+v", draft)
	}
	if draft.Files[1].PngBase64 != files[1].PngBase64 {
		t.Error("PNG 载荷未原样还原")
	}

	if err := api.ClearResourcePackDraft(); err != nil {
		t.Fatalf("清除草稿失败：%v", err)
	}
	if again, err := api.LoadResourcePackDraft(); err != nil || again.Found {
		t.Fatalf("清除后应无草稿：%+v %v", again, err)
	}
}

func TestSaveResourcePackDraftRejectsOversize(t *testing.T) {
	old := config.StorageDirectory()
	if err := config.SetStorageDirectory(t.TempDir()); err != nil {
		t.Fatalf("设置存储目录失败：%v", err)
	}
	defer config.SetStorageDirectory(old)

	api := &SystemAPI{}
	big := make([]ResourcePackFile, resourcePackMaxFiles+1)
	for index := range big {
		big[index] = ResourcePackFile{Path: "a.txt", Kind: "text", Text: "x"}
	}
	if err := api.SaveResourcePackDraft(ResourcePackDraft{Files: big}); err == nil {
		t.Error("超过文件数上限应被拒绝")
	}
}
