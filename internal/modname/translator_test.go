package modname

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func timeNow() int64 { return time.Now().Unix() }

func TestNormalizeModKey(t *testing.T) {
	cases := map[string]string{
		"sodium-fabric-0.5.8+mc1.20.1.jar":       "sodium",
		"jei-1.20.1-forge-15.3.0.4.jar":          "jei",
		"create-1.20.1-0.5.1.f.jar":              "create",
		"create-fabric-1.20.1-0.5.1f.jar":        "create",
		"appleskin-forge-mc1.20.1-2.5.1.jar":     "appleskin",
		"[1.20.1] NotEnoughAnimations-6.1.2.jar": "notenoughanimations",
		"BetterAdvancements-1.20.1-0.3.0.14.jar": "betteradvancements",
		"YungsApi-1.20-Forge-4.0.4.jar":          "yungsapi",
		"fabric-api-0.92.2+1.20.1.jar":           "fabric-api",
		"plain Mod Name.jar":                     "plain-mod-name",
		"1.20.1-mod.jar":                         "1.20.1-mod", // 纯版本开头：退回词干
	}
	for input, want := range cases {
		if got := NormalizeModKey(input); got != want {
			t.Errorf("NormalizeModKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLookupAndCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	SetCachePath(path)
	t.Cleanup(func() { SetCachePath("") })

	// 空 lookup 不报错。
	if got := Lookup([]string{"anything-1.0.jar"}); len(got) != 0 {
		t.Fatalf("期望空结果，得到 %v", got)
	}

	// 直接写入缓存文件后应能命中（模拟一次成功的搜索结果）。
	// Version 必须是当前规则版本：旧版本条目会被加载器丢弃（负缓存失效机制）。
	seed := map[string]Translation{
		"sodium": {
			Chinese: "钠", English: "Sodium", McmodID: "3533",
			FetchedAt: timeNow(), Version: cacheVersion,
		},
	}
	raw, err := jsonMarshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	// SetCachePath 已把内存缓存置空，重新加载。
	SetCachePath(path)
	got := Lookup([]string{"sodium-fabric-0.5.8.jar"})
	if got["sodium-fabric-0.5.8.jar"] != "钠" {
		t.Fatalf("期望命中“钠”，得到 %v", got)
	}
}
