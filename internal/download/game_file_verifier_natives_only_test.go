package download

import (
	"os"
	"path/filepath"
	"testing"
)

// TestVerifyLibrariesSkipsNativesOnlyMainArtifact 校验侧与启动侧同口径：
// natives-only 库没有主 jar（安装侧也从不下载），不应被判缺失，
// 否则 VerifyAndRepair 的修复计划永远缺一项、修复永远失败。
func TestVerifyLibrariesSkipsNativesOnlyMainArtifact(t *testing.T) {
	root := t.TempDir()
	nativesJar := filepath.Join(root, "libraries", "org", "lwjgl", "lwjgl",
		"lwjgl-platform", "2.9.4-beta-1", "lwjgl-platform-2.9.4-beta-1-natives-windows.jar")
	if err := os.MkdirAll(filepath.Dir(nativesJar), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nativesJar, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}

	library := libraryJSON{
		Name:    "org.lwjgl.lwjgl:lwjgl-platform:2.9.4-beta-1",
		Natives: map[string]string{"windows": "natives-windows"},
	}
	if missing := verifyLibraries(root, []libraryJSON{library}, false); len(missing) != 0 {
		t.Fatalf("natives-only 库不应报缺失，实际缺失：%v", missing)
	}
}

// TestVerifyLibrariesStillRequiresOrdinaryArtifact 确认跳过逻辑没有扩大到普通库。
func TestVerifyLibrariesStillRequiresOrdinaryArtifact(t *testing.T) {
	root := t.TempDir()
	library := libraryJSON{
		Name: "com.mojang:brigadier:1.0.18",
		Downloads: &libraryDownloadsJSON{
			Artifact: &downloadInfoJSON{Path: "com/mojang/brigadier/1.0.18/brigadier-1.0.18.jar"},
		},
	}
	missing := verifyLibraries(root, []libraryJSON{library}, false)
	if len(missing) != 1 {
		t.Fatalf("普通库缺主构件应报 1 项缺失，实际 %d 项", len(missing))
	}
}
