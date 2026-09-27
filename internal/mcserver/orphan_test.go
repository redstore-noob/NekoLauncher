package mcserver

import (
	"strings"
	"testing"
)

// TestContainsPathSegment 路径必须是完整一段：兄弟目录（s1 / s10）不能互相命中，
// 目录内部的子路径要命中，完全无关的路径不能命中。
func TestContainsPathSegment(t *testing.T) {
	dir := `C:\Users\me\AppData\Roaming\NekoLauncher\mc-servers\s1`

	cases := []struct {
		name    string
		cmdline string
		want    bool
	}{
		{
			name:    "路径作为独立一段（后跟分隔符）",
			cmdline: `"C:\Program Files\Java\bin\java.exe" -jar C:\Users\me\AppData\Roaming\NekoLauncher\mc-servers\s1\server.jar nogui`,
			want:    true,
		},
		{
			name:    "路径出现在末尾",
			cmdline: `java -Duser.dir=C:\Users\me\AppData\Roaming\NekoLauncher\mc-servers\s1`,
			want:    true,
		},
		{
			name:    "子目录同样算命中",
			cmdline: `java -jar C:\Users\me\AppData\Roaming\NekoLauncher\mc-servers\s1\plugins\x.jar`,
			want:    true,
		},
		{
			name:    "兄弟服务器 s10 不能命中 s1",
			cmdline: `java -jar C:\Users\me\AppData\Roaming\NekoLauncher\mc-servers\s10\server.jar nogui`,
			want:    false,
		},
		{
			name:    "同名前缀的另一目录不能命中",
			cmdline: `java -jar C:\Users\me\AppData\Roaming\NekoLauncher\mc-servers\s1-backup\server.jar`,
			want:    false,
		},
		{
			name:    "完全无关的进程",
			cmdline: `notepad.exe C:\Users\me\notes.txt`,
			want:    false,
		},
		{
			name:    "路径在引号之间（后跟引号）",
			cmdline: `java -jar "C:\Users\me\AppData\Roaming\NekoLauncher\mc-servers\s1"`,
			want:    true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := containsPathSegment(testCase.cmdline, dir); got != testCase.want {
				t.Fatalf("containsPathSegment(%q) = %v，期望 %v", testCase.cmdline, got, testCase.want)
			}
		})
	}
}

func TestContainsPathSegmentCaseInsensitive(t *testing.T) {
	dir := `C:\SERVERS\My-Server`
	cmdline := `java -jar c:\servers\my-server\server.jar`

	if !containsPathSegment(strings.ToUpper(cmdline), dir) {
		t.Fatal("Windows 路径大小写不敏感，应当命中")
	}
	if containsPathSegment(cmdline, "") {
		t.Fatal("空目录不应命中任何进程")
	}
}
