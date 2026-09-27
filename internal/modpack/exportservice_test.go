package modpack

// 端到端导出测试：按创作中心整合包弹窗的真实调用方式（CollectContent 取
// 清单 → 勾选 IncludedPaths → Export 落盘）驱动导出服务，产出真实的
// .mrpack / .zip 后解开压缩包验证结构。离线运行：ResolveModrinthLinks
// 置 false（弹窗在线时的直链解析走网络，失败自动回退 overrides，属后端
// 既有行为，不在本测试范围）。

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newExportFixture 造一个实例内容目录：mods / config / resourcepacks / saves。
func newExportFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(root, filepath.FromSlash(rel));
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("mods/alpha.jar", "fake-jar-alpha")
	write("mods/beta.jar", "fake-jar-beta")
	write("config/main.cfg", "key=value")
	write("resourcepacks/fancy.zip", "fake-resourcepack")
	write("saves/world/level.dat", "fake-level")

	return root
}

func zipEntries(t *testing.T, archivePath string) map[string]string {
	t.Helper()
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("打开压缩包失败：%v", err)
	}
	defer reader.Close()

	entries := map[string]string{}
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("读取条目 %s 失败：%v", file.Name, err)
		}
		buf := make([]byte, file.UncompressedSize64)
		if _, err := readFull(rc, buf); err != nil {
			t.Fatalf("读取内容 %s 失败：%v", file.Name, err)
		}
		rc.Close()
		entries[file.Name] = string(buf)
	}

	return entries
}

func readFull(rc interface{ Read([]byte) (int, error) }, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := rc.Read(buf[total:])
		total += n
		if err != nil {
			if total == len(buf) {
				return total, nil
			}
			return total, err
		}
	}

	return total, nil
}

// TestExportModrinthEndToEnd Modrinth 格式：index 声明 + overrides 携带内容。
func TestExportModrinthEndToEnd(t *testing.T) {
	content := newExportFixture(t)
	items := CollectContent(content)
	if len(items) == 0 {
		t.Fatal("内容清单为空")
	}
	// 弹窗默认全选
	included := make([]string, 0, len(items))
	for _, item := range items {
		included = append(included, item.RelativePath)
	}

	output := filepath.Join(t.TempDir(), "pack.mrpack")
	result, err := Export(
		context.Background(),
		ModpackExportOptions{
			Format:            FormatModrinth,
			PackName:          "测试整合包",
			PackVersion:       "2.1.0",
			Author:            "tester",
			MinecraftVersion:  "1.21.1",
			LoaderName:        "Fabric",
			LoaderVersion:     "0.16.9",
			IncludedPaths:     included,
			ResolveModrinthLinks: false,
		},
		content,
		output,
		nil,
	)
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	if result.OutputPath == "" {
		t.Fatal("结果缺少输出路径")
	}

	entries := zipEntries(t, output)
	indexRaw, ok := entries["modrinth.index.json"]
	if !ok {
		t.Fatal("缺少 modrinth.index.json")
	}
	var index struct {
		FormatVersion int               `json:"formatVersion"`
		Name          string            `json:"name"`
		VersionID     string            `json:"versionId"`
		Dependencies  map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(indexRaw), &index); err != nil {
		t.Fatalf("index 不是合法 JSON：%v", err)
	}
	if index.Name != "测试整合包" || index.VersionID != "2.1.0" {
		t.Errorf("元数据不符：%+v", index)
	}
	if index.Dependencies["minecraft"] != "1.21.1" || index.Dependencies["fabric-loader"] != "0.16.9" {
		t.Errorf("依赖不符：%v", index.Dependencies)
	}
	for _, want := range []string{
		"overrides/mods/alpha.jar",
		"overrides/mods/beta.jar",
		"overrides/config/main.cfg",
		"overrides/saves/world/level.dat",
	} {
		if _, ok := entries[want]; !ok {
			t.Errorf("缺少 %s", want)
		}
	}
	if entries["overrides/mods/alpha.jar"] != "fake-jar-alpha" {
		t.Error("overrides 内容与源文件不符")
	}
}

// TestExportMultiMcEndToEnd MultiMC 格式：mmc-pack.json + instance.cfg + overrides。
func TestExportMultiMcEndToEnd(t *testing.T) {
	content := newExportFixture(t)
	items := CollectContent(content)
	// 模拟用户只勾选 mods 与 config（不勾存档）
	included := []string{}
	for _, item := range items {
		if item.Category == CategoryMods || item.Category == CategoryConfig {
			included = append(included, item.RelativePath)
		}
	}

	output := filepath.Join(t.TempDir(), "pack.zip")
	_, err := Export(
		context.Background(),
		ModpackExportOptions{
			Format:           FormatMultiMc,
			PackName:         "MC整合包",
			PackVersion:      "1.0.0",
			MinecraftVersion: "1.20.4",
			IncludedPaths:    included,
		},
		content,
		output,
		func(progress ModpackExportProgress) {
			if progress.Phase == "" {
				t.Error("进度回调缺少阶段名")
			}
		},
	)
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}

	entries := zipEntries(t, output)
	componentsJSON, ok := entries["mmc-pack.json"]
	if !ok {
		t.Fatal("缺少 mmc-pack.json")
	}
	// MultiMC 规范：MC/加载器版本声明在 mmc-pack.json 的 components 里
	if !strings.Contains(componentsJSON, "1.20.4") {
		t.Errorf("mmc-pack.json 未包含 MC 版本：%s", componentsJSON)
	}
	cfg, ok := entries["instance.cfg"]
	if !ok {
		t.Fatal("缺少 instance.cfg")
	}
	if !strings.Contains(cfg, "name=MC整合包") {
		t.Errorf("instance.cfg 未包含整合包名：%s", cfg)
	}
	// MultiMC/Prism 的游戏文件放在 .minecraft/ 下（overrides/ 是 mrpack 的约定）
	if _, ok := entries[".minecraft/mods/alpha.jar"]; !ok {
		t.Error("勾选的 mods 应进 .minecraft/")
	}
	if _, ok := entries[".minecraft/saves/world/level.dat"]; ok {
		t.Error("未勾选的 saves 不应进包")
	}
}

// TestExportRejectsEmptySelection 一个都不勾：后端必须报错（弹窗据此禁用按钮）。
func TestExportRejectsEmptySelection(t *testing.T) {
	content := newExportFixture(t)
	output := filepath.Join(t.TempDir(), "pack.mrpack")
	_, err := Export(
		context.Background(),
		ModpackExportOptions{
			PackName:         "空包",
			MinecraftVersion: "1.21.1",
			IncludedPaths:    nil,
		},
		content,
		output,
		nil,
	)
	if err == nil {
		t.Fatal("空勾选应报错")
	}
}

// TestExportProfileRoundtrip 档案存取往返（弹窗打开时读、导出成功后写）。
func TestExportProfileRoundtrip(t *testing.T) {
	versionDir := t.TempDir()
	loaded := LoadProfile(versionDir)
	if loaded.PackName != nil {
		t.Errorf("空档案应为零值，实际 %+v", loaded)
	}

	name := "往返档案"
	version := "3.2.1"
	format := int(FormatMultiMc)
	resolveLinks := true
	excluded := []string{"saves", "logs/latest.log"}
	if !SaveProfile(versionDir, ModpackExportProfile{
		PackName:             &name,
		PackVersion:          &version,
		Format:               &format,
		ResolveModrinthLinks: &resolveLinks,
		ExcludedPaths:        &excluded,
	}) {
		t.Fatal("保存档案失败")
	}

	if _, err := os.Stat(filepath.Join(versionDir, "nya-pack.json")); err != nil {
		t.Fatalf("档案未落盘：%v", err)
	}
	roundtrip := LoadProfile(versionDir)
	if roundtrip.PackName == nil || *roundtrip.PackName != name {
		t.Errorf("packName 往返不符：%+v", roundtrip)
	}
	if roundtrip.ExcludedPaths == nil || len(*roundtrip.ExcludedPaths) != 2 {
		t.Errorf("excludedPaths 往返不符：%+v", roundtrip)
	}
}
