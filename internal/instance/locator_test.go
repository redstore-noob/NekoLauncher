package instance

import (
	"os"
	"path/filepath"
	"testing"
)

// makeVersionDir 在 versions/ 下造一个版本目录，files 映射相对文件名 → 内容（nil 表示空文件）。
func makeVersionDir(t *testing.T, versionsDir, id string, jsonContent string, withJar bool, extraFiles map[string]string) {
	t.Helper()
	dir := filepath.Join(versionsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建版本目录失败: %v", err)
	}
	if jsonContent != "" {
		if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(jsonContent), 0o644); err != nil {
			t.Fatalf("写入版本 json 失败: %v", err)
		}
	} else {
		if err := os.WriteFile(filepath.Join(dir, id+".json"), nil, 0o644); err != nil {
			t.Fatalf("写入空版本 json 失败: %v", err)
		}
	}
	if withJar {
		if err := os.WriteFile(filepath.Join(dir, id+".jar"), []byte("fake jar"), 0o644); err != nil {
			t.Fatalf("写入 jar 失败: %v", err)
		}
	}
	for name, content := range extraFiles {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("创建子目录失败: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("写入 %s 失败: %v", name, err)
		}
	}
}

const vanillaJSON = `{"id":"1.20.1","type":"release","releaseTime":"2023-06-12T13:25:51+00:00"}`
const inheritsJSON = `{"id":"forge-1.20.1","inheritsFrom":"1.20.1"}`

// TestGetInstalledVersionIds_FilterInvalid 覆盖扫描有效性判定：
// 完整版本与合法 inheritsFrom 版本应出现，残缺/损坏/孤儿版本应被过滤。
func TestGetInstalledVersionIds_FilterInvalid(t *testing.T) {
	root := t.TempDir()
	versionsDir := filepath.Join(root, "versions")
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// ✅ 完整版本：json + jar
	makeVersionDir(t, versionsDir, "1.20.1", vanillaJSON, true, nil)

	// ✅ Loader 版本：json 声明 inheritsFrom，无 jar（jar 由父版本提供）
	makeVersionDir(t, versionsDir, "forge-1.20.1", inheritsJSON, false, nil)

	// ❌ 下载中断残缺目录：只有 json，无 jar 也无 inheritsFrom
	makeVersionDir(t, versionsDir, "broken-jsononly", `{"id":"broken-jsononly"}`, false, nil)

	// ❌ json 损坏：无法解析
	makeVersionDir(t, versionsDir, "corrupt-json", `{ this is not json `, true, nil)

	// ❌ json 为空文件
	makeVersionDir(t, versionsDir, "empty-json", "", true, nil)

	// ❌ 孤儿 Loader 实例：声明 inheritsFrom 但父版本目录不存在
	makeVersionDir(t, versionsDir, "orphan-loader", `{"id":"orphan-loader","inheritsFrom":"1.19.4"}`, false, nil)

	// ❌ 目录里没有 json
	if err := os.MkdirAll(filepath.Join(versionsDir, "no-json"), 0o755); err != nil {
		t.Fatal(err)
	}

	ids := GetInstalledVersionIds(root)

	want := map[string]bool{"1.20.1": true, "forge-1.20.1": true}
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	for id := range want {
		if !got[id] {
			t.Errorf("有效版本 %s 未被扫描出来，得到 %v", id, ids)
		}
	}
	for id := range got {
		if !want[id] {
			t.Errorf("无效版本 %s 不应出现在列表中，得到 %v", id, ids)
		}
	}
	if len(ids) != len(want) {
		t.Errorf("期望 %d 个版本，实际 %d 个：%v", len(want), len(ids), ids)
	}

	// 排序：忽略大小写降序
	for i := 1; i < len(ids); i++ {
		if ids[i-1] < ids[i] && !equalFoldStr(ids[i-1], ids[i]) {
			t.Errorf("排序错误：期望降序，实际 %v", ids)
		}
	}
}

// TestGetInstalledVersionIds_InheritsFromChain 验证两层 inheritsFrom 链
// （vanilla ← optifine-姿势的中间层 ← loader）以及链断裂时的过滤。
func TestGetInstalledVersionIds_InheritsFromChain(t *testing.T) {
	root := t.TempDir()
	versionsDir := filepath.Join(root, "versions")
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	makeVersionDir(t, versionsDir, "1.20.1", vanillaJSON, true, nil)
	// 中间层：无 jar，继承 1.20.1
	makeVersionDir(t, versionsDir, "middle", `{"id":"middle","inheritsFrom":"1.20.1"}`, false, nil)
	// 顶层：无 jar，继承 middle → 链可达，应有效
	makeVersionDir(t, versionsDir, "top", `{"id":"top","inheritsFrom":"middle"}`, false, nil)
	// 断链：继承到不存在的 middle2
	makeVersionDir(t, versionsDir, "broken-chain", `{"id":"broken-chain","inheritsFrom":"middle2"}`, false, nil)
	// 环：a 继承 b，b 继承 a
	makeVersionDir(t, versionsDir, "loop-a", `{"id":"loop-a","inheritsFrom":"loop-b"}`, false, nil)
	makeVersionDir(t, versionsDir, "loop-b", `{"id":"loop-b","inheritsFrom":"loop-a"}`, false, nil)

	got := map[string]bool{}
	for _, id := range GetInstalledVersionIds(root) {
		got[id] = true
	}
	for _, id := range []string{"1.20.1", "middle", "top"} {
		if !got[id] {
			t.Errorf("有效版本 %s 未被扫描出来：%v", id, got)
		}
	}
	for _, id := range []string{"broken-chain", "loop-a", "loop-b"} {
		if got[id] {
			t.Errorf("无效版本 %s 不应出现在列表中", id)
		}
	}
}

// TestGetInstalledVersionIds_NoVersionsDir 无 versions 目录时返回空列表。
func TestGetInstalledVersionIds_NoVersionsDir(t *testing.T) {
	ids := GetInstalledVersionIds(t.TempDir())
	if len(ids) != 0 {
		t.Errorf("期望空列表，实际 %v", ids)
	}
}

func equalFoldStr(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
