package bindings

import (
	"path/filepath"
	"testing"
)

// 读类绑定的路径收口：只认已知游戏根目录之内的路径。
//
// 这里钉的是判定本身（纯函数），因为真正的风险是"看起来像在里面、其实在外面"的
// 前缀误判：C:\games2 不该因为字符串以 C:\games 开头就算命中。
func TestInsideAnyRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "minecraft")
	other := filepath.Join(t.TempDir(), "launcher-root")

	cases := []struct {
		name  string
		roots []string
		path  string
		want  bool
	}{
		{"根目录本身", []string{root}, root, true},
		{"根目录之内", []string{root}, filepath.Join(root, "saves", "world"), true},
		{"根目录之外", []string{root}, other, false},
		{"同前缀的兄弟目录不算命中", []string{root}, root + "2", false},
		{"同前缀的兄弟目录之内也不算", []string{root}, filepath.Join(root+"2", "saves"), false},
		{"命中第二个根", []string{root, other}, filepath.Join(other, "instances", "a", "saves"), true},
		{"空路径不命中", []string{root}, "", false},
		{"空根不命中（没配置不等于全允许）", []string{"", "  "}, filepath.Join(root, "saves"), false},
		{"没有任何根时不命中", nil, filepath.Join(root, "saves"), false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := insideAnyRoot(testCase.roots, testCase.path); got != testCase.want {
				t.Fatalf("insideAnyRoot(%v, %q) = %v, want %v",
					testCase.roots, testCase.path, got, testCase.want)
			}
		})
	}
}
