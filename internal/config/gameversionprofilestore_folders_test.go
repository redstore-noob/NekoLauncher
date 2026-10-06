package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 目录列表聚合 / 移除逻辑的回归测试：默认目录与桌面候选是展示期聚合的，
// 用户移除后必须进排除名单压住重聚合，手动加回时名单要解除。
// 断言只做成员判定——测试机真实配置（GameDirectory / 已存目录）会混进
// 聚合视图，整表相等会把用例和外部状态耦死。
func TestFolderAggregationAndRemoval(t *testing.T) {
	isolateConfigStorage(t)
	home := t.TempDir()
	defaultDir := filepath.Join(home, ".minecraft")
	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldLocator := DefaultMinecraftDirectoryLocator
	oldPath := DefaultMinecraftDirectoryPath
	DefaultMinecraftDirectoryLocator = func() string { return defaultDir }
	DefaultMinecraftDirectoryPath = func() string { return defaultDir }
	t.Cleanup(func() {
		DefaultMinecraftDirectoryLocator = oldLocator
		DefaultMinecraftDirectoryPath = oldPath
	})

	if !folderListed(defaultDir) {
		t.Fatalf("默认目录应被聚合进列表，得到 %v", GetFolders())
	}

	// 移除默认目录：允许且要生效
	if !RemoveFolder(defaultDir) {
		t.Fatal("默认目录应允许移除")
	}
	if folderListed(defaultDir) {
		t.Fatalf("移除后默认目录不应再出现在列表里，得到 %v", GetFolders())
	}

	// 手动加回：排除名单解除，目录重新出现
	if !AddFolder(defaultDir) {
		t.Fatal("加回默认目录应成功")
	}
	if !folderListed(defaultDir) {
		t.Fatalf("加回后目录应重新出现，得到 %v", GetFolders())
	}
}

// 用户在启动器外删掉了默认目录（磁盘上已不存在）时，移除也应成功——
// 否则一个死路径会永远留在列表里。
func TestRemoveVanishedDefaultDirectory(t *testing.T) {
	isolateConfigStorage(t)
	home := t.TempDir()
	defaultDir := filepath.Join(home, ".minecraft") // 不创建，保持不存在

	oldLocator := DefaultMinecraftDirectoryLocator
	oldPath := DefaultMinecraftDirectoryPath
	DefaultMinecraftDirectoryLocator = func() string { return defaultDir }
	DefaultMinecraftDirectoryPath = func() string { return defaultDir }
	t.Cleanup(func() {
		DefaultMinecraftDirectoryLocator = oldLocator
		DefaultMinecraftDirectoryPath = oldPath
	})

	// 不存在 → 不会被聚合；移除仍应成功并记入排除名单
	if folderListed(defaultDir) {
		t.Fatalf("不存在的默认目录不应出现，得到 %v", GetFolders())
	}
	if !RemoveFolder(defaultDir) {
		t.Fatal("磁盘上已消失的默认目录应允许移除")
	}
}

// folderListed 判断路径是否出现在聚合列表里（忽略大小写）。
func folderListed(path string) bool {
	for _, folder := range GetFolders() {
		if pathsEqualFold(folder, path) {
			return true
		}
	}
	return false
}

// isolateConfigStorage 把配置存储切到一次性临时目录。
// 不能只靠 TestMain 改 HOME/USERPROFILE：storageDirectory 在包 init 时就
// 已按真实主目录定死（TestMain 晚于 init），不切换的话本文件的写操作会
// 落进用户真实的 launcher.yaml。
func isolateConfigStorage(t *testing.T) {
	t.Helper()
	original := StorageDirectory()
	if err := SetStorageDirectory(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := SetStorageDirectory(original); err != nil {
			t.Fatal(err)
		}
	})
}
