package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMinecraftLanguageCode(t *testing.T) {
	cases := map[string]string{
		"zh-CN":       "zh_cn",
		"zh-Hans":     "zh_cn",
		"zh-Hant-TW":  "zh_tw",
		"zh-TW":       "zh_tw",
		"zh-HK":       "zh_hk",
		"zh":          "zh_cn",
		"en-US":       "en_us",
		"en":          "en_us",
		"en-GB":       "en_gb",
		"en-XX":       "en_us", // MC 没有的区域回落默认
		"ja-JP":       "ja_jp",
		"ja":          "ja_jp",
		"ko-KR":       "ko_kr",
		"pt-BR":       "pt_br",
		"pt-PT":       "pt_pt",
		"pt":          "pt_br",
		"zh_CN.UTF-8": "zh_cn", // 宽容解析：zh_CN + 编码后缀也能识别
		"xx-YY":       "", // 完全未知的语言
		"":            "",
		"nds-DE":      "", // MC 没有的语言
		"ru_RU":       "ru_ru",
		"fr":          "fr_fr",
		"de-AT":       "de_at",
		"zh-hans-cn":  "zh_cn",
		"es-MX":       "es_mx",
		"es-AR":       "es_ar",
		"zh-hant-mo":  "zh_hk",
	}
	for locale, want := range cases {
		if got := minecraftLanguageCode(locale); got != want {
			t.Errorf("minecraftLanguageCode(%q) = %q, want %q", locale, got, want)
		}
	}
}

func TestUpsertOptionsKey(t *testing.T) {
	// 空内容 → 只写入该键
	if got := upsertOptionsKey("", "lang", "zh_cn"); got != "lang:zh_cn\n" {
		t.Errorf("empty content upsert = %q", got)
	}
	// 已有内容 → 追加到末尾，不破坏原行
	base := "version:4157\nmouseSensitivity:1.0\n"
	got := upsertOptionsKey(base, "lang", "zh_cn")
	if !strings.Contains(got, "version:4157\nmouseSensitivity:1.0\nlang:zh_cn\n") {
		t.Errorf("append upsert = %q", got)
	}
	// 已有同名键 → 原地替换
	got = upsertOptionsKey("lang:en_us\nfov:90.0\n", "lang", "zh_cn")
	if got != "lang:zh_cn\nfov:90.0\n" {
		t.Errorf("replace upsert = %q", got)
	}
}

func TestHasOptionsKey(t *testing.T) {
	if !hasOptionsKey("lang:en_us\n", "lang") {
		t.Error("lang key should be found")
	}
	if hasOptionsKey("language:xx\n", "lang") {
		t.Error("prefix collision should not match")
	}
	if hasOptionsKey("", "lang") {
		t.Error("empty content has no keys")
	}
}

func TestSyncInstanceLanguageWritesForNewInstance(t *testing.T) {
	dir := t.TempDir()
	// 保存/恢复全局钩子
	origResolver := IsolatedGameDirectoryResolver
	t.Cleanup(func() { IsolatedGameDirectoryResolver = origResolver })
	IsolatedGameDirectoryResolver = func(minecraftDirectory, sourcePath, versionId string) string {
		return dir
	}

	origLocale := readSystemLocaleForTest
	t.Cleanup(func() { readSystemLocaleForTest = origLocale })
	readSystemLocaleForTest = func() string { return "zh-CN" }

	s := &GameLaunchService{}
	snap := GameInstanceSnapshot{MinecraftDirectory: dir, SelectedVersionId: "1.20.1"}
	s.syncInstanceLanguage(snap, "1.20.1")

	content, err := os.ReadFile(filepath.Join(dir, "options.txt"))
	if err != nil {
		t.Fatalf("options.txt not written: %v", err)
	}
	if !hasOptionsKey(string(content), "lang") || !strings.Contains(string(content), "lang:zh_cn") {
		t.Errorf("options.txt missing lang:zh_cn, got %q", content)
	}

	// 已有 lang 的实例不被覆盖
	if err := os.WriteFile(filepath.Join(dir, "options.txt"), []byte("lang:en_us\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.syncInstanceLanguage(snap, "1.20.1")
	content, _ = os.ReadFile(filepath.Join(dir, "options.txt"))
	if string(content) != "lang:en_us\n" {
		t.Errorf("existing lang overwritten: %q", content)
	}
}
