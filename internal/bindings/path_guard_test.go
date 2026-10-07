package bindings

import (
	"os"
	"path/filepath"
	"testing"

	"nekolauncher/internal/config"
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

// SystemAPI 读类绑定（ReadTextFile / ListDirectory）必须收口：不设限等于
// 给同 WebView 的插件开了任意文件读取——accounts.yaml 与密钥文件都能翻走。
func TestGuardReadablePathRejectsOutsideRoots(t *testing.T) {
	outside := t.TempDir()
	if err := guardReadablePath(filepath.Join(outside, "notes.txt")); err == nil {
		t.Fatal("收口根之外的路径应被拒绝")
	}
	// 启动器存储目录（accounts.yaml / 密钥 / 日志的家）即便被用户误配为
	// 游戏根也必须拒绝
	if storage := config.DefaultStorageDirectory(); storage != "" {
		if err := guardReadablePath(filepath.Join(storage, "accounts.yaml")); err == nil {
			t.Fatal("存储目录内的路径应被拒绝")
		}
	}
}

// 读根的设置守卫：只认"已注册目录之间的切换"或"用户对话框选中的目录"。
// 否则插件可静默 AddProfileFolder("C:\\Users\\x") 把读收口扩大到全盘。
func TestGuardSettableRoot(t *testing.T) {
	arbitrary := t.TempDir()
	if err := guardSettableRoot(arbitrary); err == nil {
		t.Fatal("未经对话框批准、也未注册的目录应被拒绝")
	}

	// 对话框批准后：目录本身与子目录（导入扫描注册场景）都放行
	approveDialogDirectory(arbitrary)
	if err := guardSettableRoot(arbitrary); err != nil {
		t.Fatalf("对话框批准的目录应放行：%v", err)
	}
	if err := guardSettableRoot(filepath.Join(arbitrary, "some-instance")); err != nil {
		t.Fatalf("批准目录的子目录应放行（导入扫描注册场景）：%v", err)
	}

	// 已注册目录之间的切换始终放行
	registered := t.TempDir()
	if !config.AddFolder(registered) {
		t.Skip("无法注册临时游戏目录，跳过")
	}
	defer config.RemoveFolder(registered)
	if err := guardSettableRoot(registered); err != nil {
		t.Fatalf("已注册目录的切换应放行：%v", err)
	}
}

func TestGuardReadablePathAllowsInsideRoots(t *testing.T) {
	root := t.TempDir()
	if !config.AddFolder(root) {
		t.Skip("无法注册临时游戏目录，跳过")
	}
	defer config.RemoveFolder(root)

	target := filepath.Join(root, "options.txt")
	if err := os.WriteFile(target, []byte("version:1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := guardReadablePath(target); err != nil {
		t.Fatalf("游戏根内的路径不应被拒绝：%v", err)
	}

	api := &SystemAPI{}
	if _, err := api.ReadTextFile(filepath.Join(root, "..", "..", "some-secret.txt")); err == nil {
		t.Error("借 .. 逃出游戏根的读取应被拒绝")
	}
	if _, err := api.ReadTextFile(target); err != nil {
		t.Errorf("正常读取游戏根内文件失败：%v", err)
	}
}
