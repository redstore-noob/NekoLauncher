package modpack

// CurseForge 导出端到端测试：与 exportservice_test.go 同一套驱动方式
// （CollectContent → IncludedPaths → Export → 解包验证），指纹反查用
// 可替换的 CurseForgeFingerprintResolver 桩掉——离线运行，不访问真接口。

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// stubFingerprintResolver 返回一个只认指定指纹的假反查器，并自动在测试结束时还原。
func stubFingerprintResolver(t *testing.T, matches map[uint32]curseForgeFingerprintMatch) {
	t.Helper()
	original := CurseForgeFingerprintResolver
	CurseForgeFingerprintResolver = func(_ context.Context, _ string, prints []uint32) (map[uint32]curseForgeFingerprintMatch, error) {
		result := map[uint32]curseForgeFingerprintMatch{}
		for _, print := range prints {
			if match, ok := matches[print]; ok {
				result[print] = match
			}
		}
		return result, nil
	}
	t.Cleanup(func() { CurseForgeFingerprintResolver = original })
}

// TestExportCurseForgeEndToEnd CurseForge 格式：manifest 声明 + overrides 携带其余内容。
// 防的回归：声明过的 mod 又重复打进 overrides（导入方会收到两份不同来源的同一 mod）、
// modLoaders 的 id 拼法错误（CF 客户端按 id 精确解析加载器）。
func TestExportCurseForgeEndToEnd(t *testing.T) {
	content := newExportFixture(t)
	alphaFingerprint, err := curseForgeFingerprintFile(filepath.Join(content, "mods", "alpha.jar"))
	if err != nil {
		t.Fatalf("计算测试文件指纹失败：%v", err)
	}
	stubFingerprintResolver(t, map[uint32]curseForgeFingerprintMatch{
		alphaFingerprint: {ProjectID: 306612, FileID: 5227279, FileName: "alpha.jar", DisplayName: "Alpha Mod"},
	})

	items := CollectContent(content)
	included := make([]string, 0, len(items))
	for _, item := range items {
		included = append(included, item.RelativePath)
	}

	output := filepath.Join(t.TempDir(), "pack.zip")
	result, err := Export(
		context.Background(),
		ModpackExportOptions{
			Format:           FormatCurseForge,
			PackName:         "CF 整合包",
			PackVersion:      "1.2.3",
			Author:           "tester",
			MinecraftVersion: "1.21.1",
			LoaderName:       "Fabric",
			LoaderVersion:    "0.16.9",
			IncludedPaths:    included,
			CurseForgeAPIKey: "test-key",
		},
		content,
		output,
		nil,
	)
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	if result.DeclaredFiles != 1 {
		t.Errorf("DeclaredFiles = %d，期望 1（只有 alpha.jar 命中）", result.DeclaredFiles)
	}

	entries := zipEntries(t, output)
	manifestRaw, ok := entries["manifest.json"]
	if !ok {
		t.Fatalf("缺少 manifest.json，实际条目：%v", keysOf(entries))
	}
	var manifest struct {
		Minecraft struct {
			Version    string `json:"version"`
			ModLoaders []struct {
				ID      string `json:"id"`
				Primary bool   `json:"primary"`
			} `json:"modLoaders"`
		} `json:"minecraft"`
		ManifestType    string `json:"manifestType"`
		ManifestVersion int    `json:"manifestVersion"`
		Name            string `json:"name"`
		Version         string `json:"version"`
		Files           []struct {
			ProjectID int64 `json:"projectID"`
			FileID    int64 `json:"fileID"`
			Required  bool  `json:"required"`
		} `json:"files"`
		Overrides string `json:"overrides"`
	}
	if err := json.Unmarshal([]byte(manifestRaw), &manifest); err != nil {
		t.Fatalf("manifest.json 不是合法 JSON：%v", err)
	}
	if manifest.ManifestType != "minecraftModpack" || manifest.ManifestVersion != 1 {
		t.Errorf("manifest 类型/版本不符：%+v", manifest)
	}
	if manifest.Minecraft.Version != "1.21.1" {
		t.Errorf("minecraft.version = %q，期望 1.21.1", manifest.Minecraft.Version)
	}
	if len(manifest.Minecraft.ModLoaders) != 1 ||
		manifest.Minecraft.ModLoaders[0].ID != "fabric-0.16.9" ||
		!manifest.Minecraft.ModLoaders[0].Primary {
		t.Errorf("modLoaders 不符：%+v", manifest.Minecraft.ModLoaders)
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("files = %d 条，期望 1 条：%+v", len(manifest.Files), manifest.Files)
	}
	if manifest.Files[0].ProjectID != 306612 || manifest.Files[0].FileID != 5227279 || !manifest.Files[0].Required {
		t.Errorf("files 条目不符：%+v", manifest.Files[0])
	}
	if manifest.Overrides != "overrides" {
		t.Errorf("overrides 字段 = %q，期望 \"overrides\"", manifest.Overrides)
	}

	// 命中的 alpha.jar 只走声明、不进 overrides；未命中的 beta.jar 必须进 overrides
	if _, ok := entries["overrides/mods/alpha.jar"]; ok {
		t.Error("已声明为 CurseForge 文件的 alpha.jar 不应重复进 overrides")
	}
	if got, ok := entries["overrides/mods/beta.jar"]; !ok {
		t.Error("未命中的 beta.jar 应进 overrides")
	} else if got != "fake-jar-beta" {
		t.Error("overrides 内容与源文件不符")
	}
	if !strings.Contains(entries["modlist.html"], "Alpha Mod") {
		t.Error("modlist.html 应包含命中模组的显示名")
	}
}

// TestExportCurseForgeWithoutAPIKey 未配置 Key 时全部 mod 回落 overrides，且导出不失败。
func TestExportCurseForgeWithoutAPIKey(t *testing.T) {
	content := newExportFixture(t)
	items := CollectContent(content)
	included := make([]string, 0, len(items))
	for _, item := range items {
		included = append(included, item.RelativePath)
	}

	output := filepath.Join(t.TempDir(), "pack.zip")
	result, err := Export(
		context.Background(),
		ModpackExportOptions{
			Format:           FormatCurseForge,
			PackName:         "无 Key 整合包",
			PackVersion:      "1.0.0",
			MinecraftVersion: "1.21.1",
			IncludedPaths:    included,
			CurseForgeAPIKey: "",
		},
		content,
		output,
		nil,
	)
	if err != nil {
		t.Fatalf("无 Key 导出不应失败：%v", err)
	}
	if result.DeclaredFiles != 0 {
		t.Errorf("无 Key 时 DeclaredFiles = %d，期望 0", result.DeclaredFiles)
	}
	if len(result.Warnings) == 0 {
		t.Error("无 Key 时应给出警告说明模组直接打包")
	}
	entries := zipEntries(t, output)
	for _, want := range []string{"manifest.json", "overrides/mods/alpha.jar", "overrides/mods/beta.jar"} {
		if _, ok := entries[want]; !ok {
			t.Errorf("缺少 %s", want)
		}
	}
}

// TestFingerprintMurmur2Basics 指纹算法的基本性质：确定性、长度参与混合
// （哈希里混入长度是 murmur2 终结步骤，漏掉会让不同长度内容撞出同一指纹）。
func TestFingerprintMurmur2Basics(t *testing.T) {
	first := fingerprintMurmur2([]byte("fake-jar-alpha"))
	if first != fingerprintMurmur2([]byte("fake-jar-alpha")) {
		t.Fatal("同一输入两次指纹不一致（算法不是确定的）")
	}
	if first == fingerprintMurmur2([]byte("fake-jar-beta")) {
		t.Fatal("不同内容得到相同指纹")
	}
	if first == fingerprintMurmur2([]byte("fake-jar-alpha!")) {
		t.Fatal("不同长度但同前缀的内容得到相同指纹（长度未参与混合）")
	}
	if fingerprintMurmur2(nil) == fingerprintMurmur2([]byte{0}) {
		t.Fatal("空输入与非空输入得到相同指纹")
	}
}

func keysOf(entries map[string]string) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	return keys
}
