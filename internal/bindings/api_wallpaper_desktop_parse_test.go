package bindings

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWallpaperPathFromValue 各桌面环境输出的取值解析（跨平台可测的纯函数）。
func TestWallpaperPathFromValue(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "wall.png")
	if err := os.WriteFile(existing, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(filepath.Dir(existing), "gone.png")

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"gsettings 带引号", "  '" + existing + "'  ", existing},
		{"file URI", "file://" + filepath.ToSlash(existing), existing},
		{"裸绝对路径", existing, existing},
		{"未设置（none）", "'none'", ""},
		{"KDE 纯色/渐变", "colors:0,0,0", ""},
		{"网络地址", "https://example.com/a.png", ""},
		{"相对路径", "wallpapers/a.png", ""},
		{"已被删除的文件", missing, ""},
		{"空值", "   ", ""},
	}

	for _, item := range cases {
		if got := wallpaperPathFromValue(item.raw); got != item.want {
			t.Errorf("%s：wallpaperPathFromValue(%q) = %q，期望 %q", item.name, item.raw, got, item.want)
		}
	}
}

// TestParseKDEWallpaperConfig Plasma 配置里取第一个可用的壁纸，跳过纯色/失效项。
func TestParseKDEWallpaperConfig(t *testing.T) {
	directory := t.TempDir()
	good := filepath.Join(directory, "kde.png")
	if err := os.WriteFile(good, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	content := "[Container 1][Wallpaper][org.kde.image][General]\n" +
		"Image=colors:0,0,0\n" +
		"FillMode=2\n" +
		"\n[Container 2][Wallpaper][org.kde.image][General]\n" +
		"Image=file://" + filepath.ToSlash(good) + "\n"

	if got := parseKDEWallpaperConfig(content); got != good {
		t.Fatalf("KDE 配置解析 = %q，期望 %q", got, good)
	}
	if got := parseKDEWallpaperConfig("FillMode=2\n"); got != "" {
		t.Fatalf("没有 Image 行时应返回空串，得到 %q", got)
	}
}

// TestParseFehbg feh 的历史命令里取最后一条有效壁纸。
func TestParseFehbg(t *testing.T) {
	directory := t.TempDir()
	old := filepath.Join(directory, "old.png")
	latest := filepath.Join(directory, "latest.png")
	for _, path := range []string{old, latest} {
		if err := os.WriteFile(path, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	content := "#!/bin/sh\n" +
		"feh --no-fehbg --bg-fill '" + old + "'\n" +
		"feh --no-fehbg --bg-scale '" + latest + "'\n"

	if got := parseFehbg(content); got != latest {
		t.Fatalf("feh 配置解析 = %q，期望 %q", got, latest)
	}

	// 最后一条指向已删除的文件时，回退到前一条有效的
	content = "feh --no-fehbg --bg-fill '" + old + "'\n" +
		"feh --no-fehbg --bg-scale '" + filepath.Join(directory, "gone.png") + "'\n"
	if got := parseFehbg(content); got != old {
		t.Fatalf("应回退到仍存在的那条，得到 %q", got)
	}
	if got := parseFehbg("#!/bin/sh\n"); got != "" {
		t.Fatalf("没有 feh 命令时应返回空串，得到 %q", got)
	}
}
