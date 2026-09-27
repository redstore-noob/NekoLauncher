package solo

// 清单解析、packId 清洗与标记文件读写的单元测试。
// TestMain 把 USERPROFILE 指到一次性临时目录，日志与默认存储目录都不会碰真实用户数据。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "nekolauncher-solo-test-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("USERPROFILE", home)
	_ = os.Setenv("HOME", home)

	code := m.Run()

	_ = os.RemoveAll(home)
	os.Exit(code)
}

func TestSanitizePackID(t *testing.T) {
	cases := map[string]string{
		"我的究极生存包":     "pack",
		"Sky Odyssey 2": "Sky-Odyssey-2",
		"!!中文!!":        "pack",
		"  --__a.b__--": "a.b",
	}
	for input, expected := range cases {
		if got := SanitizePackID(input); got != expected {
			t.Errorf("SanitizePackID(%q) = %q，期望 %q", input, got, expected)
		}
	}
	long := SanitizePackID(string(make([]byte, 0)) + "aaaaaaaaaa bbbbbbbbbb cccccccccc dddddddddd eeeeeeeeee ffffffffff")
	if len(long) > 48 {
		t.Errorf("超长 packId 未截断：%d", len(long))
	}
}

func TestParseManifest(t *testing.T) {
	data := []byte(`{
		"format": 1,
		"packId": "demo",
		"packName": "Demo Pack",
		"packVersion": "1.0.0",
		"mcVersion": "1.20.1",
		"versionId": "1.20.1-forge-47.2.0",
		"simpleMode": true,
		"hasJava": true
	}`)
	manifest, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest 失败：%v", err)
	}
	if manifest.PackName != "Demo Pack" || manifest.VersionID != "1.20.1-forge-47.2.0" || !manifest.SimpleMode || !manifest.HasJava {
		t.Fatalf("清单字段不符：%+v", manifest)
	}

	badFormat := []byte(`{"format": 99, "packName": "x", "versionId": "y"}`)
	if _, err := ParseManifest(badFormat); err == nil {
		t.Fatal("未知格式版本应被拒绝")
	}
	if _, err := ParseManifest([]byte(`{"format": 1, "packName": "x"}`)); err == nil {
		t.Fatal("缺少 versionId 应被拒绝")
	}
	if _, err := ParseManifest([]byte("not-json")); err == nil {
		t.Fatal("非 JSON 应被拒绝")
	}
}

func TestMarkerSaveLoad(t *testing.T) {
	storage := t.TempDir()
	if _, err := LoadMarker(storage); err != nil {
		t.Fatalf("无标记时应返回 nil 而非错误：%v", err)
	}

	marker := &Marker{
		Format:             PayloadFormat,
		PackName:           "Demo Pack",
		PackVersion:        "1.0.0",
		VersionID:          "1.20.1",
		SimpleMode:         true,
		MinecraftDirectory: filepath.Join(storage, "minecraft"),
		JavaExecutable:     filepath.Join(storage, "runtime", "jre", "bin", "java.exe"),
	}
	if err := SaveMarker(storage, marker); err != nil {
		t.Fatalf("SaveMarker 失败：%v", err)
	}
	loaded, err := LoadMarker(storage)
	if err != nil {
		t.Fatalf("LoadMarker 失败：%v", err)
	}
	if loaded == nil || loaded.PackName != marker.PackName || loaded.VersionID != marker.VersionID ||
		loaded.SimpleMode != marker.SimpleMode || loaded.MinecraftDirectory != marker.MinecraftDirectory {
		t.Fatalf("标记读写不一致：%+v", loaded)
	}
	if _, err := os.Stat(filepath.Join(storage, "neko-solo.json")); err != nil {
		t.Fatalf("标记文件不在预期位置：%v", err)
	}
}
