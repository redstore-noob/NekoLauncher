package content

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// ResolveDuplicateMods 会指导"动用户的哪些文件"，选错了等于让用户删掉想留的那个。
func TestResolveDuplicateModsKeepNewest(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "sodium-1.0.0.jar",
		`{"id":"sodium","name":"Sodium","version":"1.0.0"}`)
	writeFabricMod(t, directory, "sodium-2.0.0.jar",
		`{"id":"sodium","name":"Sodium","version":"2.0.0"}`)

	toDisable, err := ResolveDuplicateMods(directory, true)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(toDisable) != 1 {
		t.Fatalf("应禁用 1 个，实际 %v", toDisable)
	}
	if filepath.Base(toDisable[0]) != "sodium-1.0.0.jar" {
		t.Errorf("应禁用旧版，实际禁用了 %q", filepath.Base(toDisable[0]))
	}
}

func TestResolveDuplicateModsKeepAlphabetical(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "sodium-a.jar",
		`{"id":"sodium","name":"Sodium","version":"9.0.0"}`)
	writeFabricMod(t, directory, "sodium-z.jar",
		`{"id":"sodium","name":"Sodium","version":"1.0.0"}`)

	// keepNewest=false：按文件名保留最后的（z），即使它的版本号更低
	toDisable, err := ResolveDuplicateMods(directory, false)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(toDisable) != 1 || filepath.Base(toDisable[0]) != "sodium-a.jar" {
		t.Errorf("应禁用 sodium-a.jar，实际 %v", toDisable)
	}
}

func TestResolveDuplicateModsPrefersParseableVersion(t *testing.T) {
	directory := modsDir(t)
	// 一个版本号读不出（空），一个正常：有版本号的应当胜出
	writeFabricMod(t, directory, "mod-unknown.jar",
		`{"id":"mod","name":"Mod"}`)
	writeFabricMod(t, directory, "mod-1.5.0.jar",
		`{"id":"mod","name":"Mod","version":"1.5.0"}`)

	toDisable, err := ResolveDuplicateMods(directory, true)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(toDisable) != 1 || filepath.Base(toDisable[0]) != "mod-unknown.jar" {
		t.Errorf("应保留有版本号的那个，实际 %v", toDisable)
	}
}

func TestResolveDuplicateModsNoDuplicatesIsEmpty(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "a.jar", `{"id":"a","name":"A","version":"1.0.0"}`)
	writeFabricMod(t, directory, "b.jar", `{"id":"b","name":"B","version":"1.0.0"}`)

	toDisable, err := ResolveDuplicateMods(directory, true)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(toDisable) != 0 {
		t.Errorf("没有重复时不该动任何文件，实际 %v", toDisable)
	}
}

func TestResolveDuplicateModsIgnoresUnidentified(t *testing.T) {
	directory := modsDir(t)
	// 两个都读不出 mod id（坏 jar）：不能凭"看起来一样"就去禁用文件
	if err := os.WriteFile(filepath.Join(directory, "junk1.jar"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "junk2.jar"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	toDisable, err := ResolveDuplicateMods(directory, true)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(toDisable) != 0 {
		t.Errorf("读不出 id 的 jar 不能被当成重复项处理，实际 %v", toDisable)
	}
}

func TestResolveDuplicateModsHandlesThreeCopies(t *testing.T) {
	directory := modsDir(t)
	for _, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
		writeFabricMod(t, directory, "mod-"+version+".jar",
			`{"id":"mod","name":"Mod","version":"`+version+`"}`)
	}

	toDisable, err := ResolveDuplicateMods(directory, true)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(toDisable) != 2 {
		t.Fatalf("三份副本应禁用两份，实际 %v", toDisable)
	}
	sort.Strings(toDisable)
	// 保留 3.0.0，禁用 1.0.0 与 2.0.0
	if filepath.Base(toDisable[0]) != "mod-1.0.0.jar" ||
		filepath.Base(toDisable[1]) != "mod-2.0.0.jar" {
		t.Errorf("禁用清单 = %v", toDisable)
	}
}

func TestResolveDuplicateModsHandlesMultipleIDs(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "a1.jar", `{"id":"a","name":"A","version":"1.0.0"}`)
	writeFabricMod(t, directory, "a2.jar", `{"id":"a","name":"A","version":"2.0.0"}`)
	writeFabricMod(t, directory, "b1.jar", `{"id":"b","name":"B","version":"1.0.0"}`)
	writeFabricMod(t, directory, "b2.jar", `{"id":"b","name":"B","version":"2.0.0"}`)

	toDisable, err := ResolveDuplicateMods(directory, true)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(toDisable) != 2 {
		t.Fatalf("两个 id 各禁一个，实际 %v", toDisable)
	}
	for _, path := range toDisable {
		base := filepath.Base(path)
		if base != "a1.jar" && base != "b1.jar" {
			t.Errorf("禁用了错误的新版：%s", base)
		}
	}
}

func TestResolveDuplicateModsReturnsAbsolutePaths(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "c1.jar", `{"id":"c","name":"C","version":"1.0.0"}`)
	writeFabricMod(t, directory, "c2.jar", `{"id":"c","name":"C","version":"2.0.0"}`)

	toDisable, err := ResolveDuplicateMods(directory, true)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	// 调用方要直接拿去 rename，必须是可直接使用的绝对路径
	for _, path := range toDisable {
		if !filepath.IsAbs(path) {
			t.Errorf("返回了非绝对路径：%q", path)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("返回的路径不存在：%q", path)
		}
	}
}
