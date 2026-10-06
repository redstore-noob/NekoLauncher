package instance

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeMarkerFile 在 directory 下写入相对路径 marker 的文件（自动建父目录）。
func writeMarkerFile(t *testing.T, directory, marker, content string) {
	t.Helper()
	path := filepath.Join(directory, marker)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入 %s 失败: %v", marker, err)
	}
}

// TestResolveLayout_HMCLMarker 覆盖 HMCL 实例设置的两种文件名：
// 现行 hmclversion.cfg 与老版 .hmclversion.cfg 都应判为 HMCL 版本隔离。
func TestResolveLayout_HMCLMarker(t *testing.T) {
	for _, marker := range []string{"hmclversion.cfg", ".hmclversion.cfg"} {
		t.Run(marker, func(t *testing.T) {
			root := t.TempDir()
			versionDir := filepath.Join(root, "versions", "1.20.1")
			if err := os.MkdirAll(versionDir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeMarkerFile(t, versionDir, marker, "# HMCL")
			writeMarkerFile(t, versionDir, filepath.Join("mods", "some-mod.jar"), "fake")

			layout := ResolveLayout(root, "", "1.20.1", nil, nil)
			if !layout.IsIsolated {
				t.Fatalf("应判定为版本隔离，实际 %+v", layout)
			}
			if layout.Provider != "HMCL" {
				t.Fatalf("Provider 应为 HMCL，实际 %q", layout.Provider)
			}
		})
	}
}

// TestTryResolveExternalInstance_HMCLAndPCL 覆盖外部实例识别链新增的
// HMCL（hmclversion.cfg）与 PCL（PCL/Setup.ini）标记。
func TestTryResolveExternalInstance_HMCLAndPCL(t *testing.T) {
	t.Run("HMCL", func(t *testing.T) {
		instanceDir := filepath.Join(t.TempDir(), "1.20.1")
		if err := os.MkdirAll(instanceDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeMarkerFile(t, instanceDir, "hmclversion.cfg", "# HMCL")
		writeMarkerFile(t, instanceDir, filepath.Join(".minecraft", "options.txt"), "lang:zh_cn")

		layout, ok := TryResolveExternalInstance(instanceDir)
		if !ok {
			t.Fatal("应识别为外部实例")
		}
		if layout.Provider != "HMCL" {
			t.Fatalf("Provider 应为 HMCL，实际 %q", layout.Provider)
		}
	})

	t.Run("PCL", func(t *testing.T) {
		instanceDir := filepath.Join(t.TempDir(), "1.19.2")
		if err := os.MkdirAll(instanceDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeMarkerFile(t, instanceDir, filepath.Join("PCL", "Setup.ini"), "VersionArgumentIndieV2:True")
		writeMarkerFile(t, instanceDir, filepath.Join("mods", "some-mod.jar"), "fake")

		layout, ok := TryResolveExternalInstance(instanceDir)
		if !ok {
			t.Fatal("应识别为外部实例")
		}
		if layout.Provider != "PCL" {
			t.Fatalf("Provider 应为 PCL，实际 %q", layout.Provider)
		}
	})
}

// TestResolveInstallationPath_ClassifiesInvalidRoot 普通文件夹（缺 versions）
// 必须能经 errors.Is 匹配 ErrNotMinecraftRoot——实例扫描据此把它降级为空快照，
// 而不是当作扫描失败上报。
func TestResolveInstallationPath_ClassifiesInvalidRoot(t *testing.T) {
	plain := t.TempDir()

	_, err := ResolveInstallationPath(plain)
	if err == nil {
		t.Fatal("缺 versions 的目录应返回错误")
	}
	if !IsInvalidRootDirectory(err) || !errors.Is(err, ErrNotMinecraftRoot) {
		t.Fatalf("应匹配 ErrNotMinecraftRoot，实际 %v", err)
	}
	// 报错文案保持原样（前端按原文案展示）
	want := "该路径不是有效的 Minecraft 根目录，缺少 versions 文件夹：" + filepath.Clean(plain)
	if err.Error() != want {
		t.Fatalf("错误文案不符：got %q want %q", err.Error(), want)
	}

	// 不存在的目录是另一类错误，不能被降级
	_, err = ResolveInstallationPath(filepath.Join(plain, "not-exist"))
	if err == nil || IsInvalidRootDirectory(err) {
		t.Fatalf("不存在的路径不应匹配 ErrNotMinecraftRoot，实际 %v", err)
	}
}

// TestScan_DegradesPlainFolder 普通文件夹扫描应降级为空快照：
// 无错误、无 ErrorMessage、版本列表为空——实例页显示空态而不是红色报错。
func TestScan_DegradesPlainFolder(t *testing.T) {
	plain := t.TempDir()
	// 里面放点不相干文件，模拟用户随手选的目录
	writeMarkerFile(t, plain, "readme.txt", "hello")

	snapshot, err := scan(plain, EmptyGameInstanceSnapshot())
	if err != nil {
		t.Fatalf("普通目录不应返回错误：%v", err)
	}
	if snapshot.ErrorMessage != "" {
		t.Fatalf("不应携带 ErrorMessage：%q", snapshot.ErrorMessage)
	}
	if snapshot.IsLoading {
		t.Fatal("不应停留在加载态")
	}
	if len(snapshot.VersionIds) != 0 {
		t.Fatalf("版本列表应为空，实际 %v", snapshot.VersionIds)
	}
	if snapshot.MinecraftDirectory == "" {
		t.Fatal("MinecraftDirectory 应保留所选目录")
	}
}
