package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 内容落点与实例名的路径约束。
//
// 这两处都不是"理论风险"：
//   - DownloadFileToInstance 同时被插件 API（downloadResource）使用，subDirectory
//     曾经原样进 filepath.Join，传 "..\\.." 就把下载的文件写到实例目录之外；
//   - ModLoaderInstaller 的 Forge / NeoForge 分支在扁平化之前就用实例名拼
//     versions/<名称>，而当时只有 Fabric 分支经 baseInstaller.Install 间接受检。
//
// 下面把它们钉死：越界一律拒绝，且必须在动任何文件之前就拒绝。

func TestDownloadFileToInstanceRejectsEscapingSubDirectory(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "escaped")

	cases := []string{"..", "../..", "..\\..", "mods/../..", "mods\\..\\..\\.."}
	for _, subDirectory := range cases {
		_, err := DownloadFileToInstance(
			context.Background(),
			"http://127.0.0.1:1/never-fetched.jar",
			"a.jar",
			root,
			subDirectory,
			func(int64, int64) {},
		)
		if err == nil || !strings.Contains(err.Error(), "越界") {
			t.Fatalf("子目录 %q 应被拒绝，实际 err=%v", subDirectory, err)
		}
	}

	// 越界必须在发起下载之前拒绝：既没写出文件，也没在实例目录外建目录
	if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
		t.Fatalf("越界路径不该被创建：%v", statErr)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatalf("读取内容目录失败：%v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("越界请求不该在内容目录里留下任何东西：%+v", entries)
	}
}

func TestDownloadFileToInstanceKeepsFileInsideContentDirectory(t *testing.T) {
	payload := []byte("fake mod payload")

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", "16")
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	root := t.TempDir()

	for _, testCase := range []struct {
		name         string
		subDirectory string
		wantRelative string
	}{
		{"内容子目录", "mods", filepath.Join("mods", "sodium.jar")},
		{"子目录为空时落在内容目录根", "", "sodium.jar"},
		{"原生分隔符", "shaderpacks\\extra", filepath.Join("shaderpacks", "extra", "sodium.jar")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			saved, err := DownloadFileToInstance(
				context.Background(),
				server.URL+"/sodium.jar",
				"sodium.jar",
				root,
				testCase.subDirectory,
				func(int64, int64) {},
			)
			if err != nil {
				t.Fatalf("正常子目录不该失败：%v", err)
			}

			want := filepath.Join(root, testCase.wantRelative)
			if saved != want {
				t.Fatalf("落点不符：want %s, got %s", want, saved)
			}
			if !containedPath(root, saved) {
				t.Fatalf("落点跑出内容目录：%s", saved)
			}
			if content, readErr := os.ReadFile(want); readErr != nil || string(content) != string(payload) {
				t.Fatalf("文件内容不符：%v / %q", readErr, content)
			}
		})
	}
}

// TestSafeCombineStillRejectsModpackTraversal 是重构 safeCombine 后的回归钉子：
// 整合包条目的穿越防护与文案都不能变（越界条目要被跳过而不是让导入崩掉）。
func TestSafeCombineStillRejectsModpackTraversal(t *testing.T) {
	root := t.TempDir()

	for _, relative := range []string{"../evil.txt", "a/../../evil.txt", "/etc/passwd", "..\\evil.txt"} {
		if _, err := safeCombine(root, relative); err == nil {
			t.Fatalf("整合包条目 %q 应被拒绝", relative)
		}
	}

	good, err := safeCombine(root, "mods/sodium.jar")
	if err != nil {
		t.Fatalf("正常条目不该失败：%v", err)
	}
	if good != filepath.Join(root, "mods", "sodium.jar") {
		t.Fatalf("拼接结果不符：%s", good)
	}
}

// TestModLoaderInstallRejectsUnsafeInstanceName 实例名会变成 versions/<名称> 目录：
// 不安全的名字必须在安装开始前（联网、落盘之前）就拒绝。
func TestModLoaderInstallRejectsUnsafeInstanceName(t *testing.T) {
	root := t.TempDir()
	installer := &ModLoaderInstaller{}

	for _, instanceName := range []string{"..", "..\\..\\evil", "../evil", "a/b", "c:d"} {
		err := installer.Install(
			context.Background(),
			ModLoaderVersion{RequiresInstallerExtraction: true},
			instanceName,
			root,
			"1.21.1",
			nil,
		)
		if err == nil || !strings.Contains(err.Error(), "不安全字符") {
			t.Fatalf("实例名 %q 应被拒绝，实际 err=%v", instanceName, err)
		}
	}

	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatalf("读取游戏目录失败：%v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("被拒绝的实例名不该创建任何东西：%+v", entries)
	}
}
