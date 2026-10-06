//go:build linux

package bindings

import "testing"

func TestParseSwwwQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"单输出", "eDP-1: image: /home/u/img.png\n", "/home/u/img.png"},
		{"多输出取第一行", "eDP-1: image: /a.png\nHDMI-A-1: image: /b.png\n", "/a.png"},
		{"clear 状态忽略", "eDP-1: No images!\n", ""},
		{"混合取有图的行", "eDP-1: No images!\nDP-2: image: /b.jpg\n", "/b.jpg"},
		{"空输出", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseSwwwQuery(tc.in); got != tc.want {
				t.Fatalf("parseSwwwQuery(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsVideoWallpaperFile(t *testing.T) {
	for _, path := range []string{"/a/b.mp4", "V.MOV", "x.webm", "y.mkv"} {
		if !isVideoWallpaperFile(path) {
			t.Errorf("isVideoWallpaperFile(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"/a/b.png", "c.gif", "d.jpg", "e.mp4.bak", ""} {
		if isVideoWallpaperFile(path) {
			t.Errorf("isVideoWallpaperFile(%q) = true, want false", path)
		}
	}
}
