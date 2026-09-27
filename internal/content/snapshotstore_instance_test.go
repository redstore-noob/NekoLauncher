package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nekolauncher/internal/config"
)

// newInstanceDirectory 造一个最小实例游戏目录：mods/ 是快照对象，
// libraries/assets/logs 是应被跳过的目录。
func newInstanceDirectory(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "MyInstance")
	writeWorldFile(t, directory, "mods/optimization.jar", "mod-1")
	writeWorldFile(t, directory, "config/options.txt", "fov:1")
	writeWorldFile(t, directory, "libraries/net/minecraft/x.jar", "library")
	writeWorldFile(t, directory, "assets/indexes/objects.json", "asset")
	writeWorldFile(t, directory, "logs/latest.log", "log")
	writeWorldFile(t, directory, "versions/1.20.1/natives/native.dll", "native")
	return directory
}

// TestInstanceSnapshotSkipsIgnoredDirectories 实例快照只捕获用户内容与配置，
// libraries/assets/logs 与启动时重新解压的 natives 不进清单。
func TestInstanceSnapshotSkipsIgnoredDirectories(t *testing.T) {
	useTempStorage(t)
	instance := newInstanceDirectory(t)

	snapshot, err := CreateInstanceSnapshot(context.Background(), instance, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.FileCount != 2 {
		t.Fatalf("应只捕获 2 个文件（mods + config），得到 %d", snapshot.FileCount)
	}

	manifest, err := loadSnapshotManifest(snapshotRepoDirectory(instance), snapshot.Id)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Kind != string(snapshotKindInstance) {
		t.Fatalf("清单应记录实例类型，得到 %q", manifest.Kind)
	}
	for _, file := range manifest.Files {
		if snapshotIgnores(snapshotKindInstance, file.Path, false) {
			t.Fatalf("清单不应包含被忽略的路径：%s", file.Path)
		}
	}
}

// TestInstanceRollbackPreservesIgnoredDirectories 实例回滚采用"整目录换名"，
// 被快照跳过的目录必须原样保留在新目录里。
func TestInstanceRollbackPreservesIgnoredDirectories(t *testing.T) {
	useTempStorage(t)
	instance := newInstanceDirectory(t)

	first, err := CreateInstanceSnapshot(context.Background(), instance, "", "")
	if err != nil {
		t.Fatal(err)
	}
	writeWorldFile(t, instance, "mods/optimization.jar", "mod-2")
	writeWorldFile(t, instance, "libraries/net/minecraft/x.jar", "library-updated")

	if _, err := RollbackInstanceSnapshot(context.Background(), instance, first.Id); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(instance, "mods", "optimization.jar"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "mod-1" {
		t.Fatalf("回滚后 mod 内容不符：%q", data)
	}
	// 被忽略的目录：回滚后原样保留（包括快照之后新增/修改的内容）
	library, err := os.ReadFile(filepath.Join(instance, "libraries", "net", "minecraft", "x.jar"))
	if err != nil {
		t.Fatalf("被忽略的 libraries 目录在回滚后丢失：%v", err)
	}
	if string(library) != "library-updated" {
		t.Fatalf("libraries 内容不应被回滚影响：%q", library)
	}
	if _, err := os.Stat(filepath.Join(instance, "config", "options.txt")); err != nil {
		t.Fatalf("回滚后清单内文件丢失：%v", err)
	}

	// 临时/备份目录不应残留
	entries, err := os.ReadDir(filepath.Dir(instance))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Base(instance) != entry.Name() {
			t.Fatalf("回滚后不应留下临时目录或残骸：%s", entry.Name())
		}
	}
}

// TestInstanceRollbackKeepsSafetySnapshotOnFailure 恢复失败时目标目录不被改动，
// 且回滚前安全快照可用。
func TestInstanceRollbackFailureKeepsInstanceIntact(t *testing.T) {
	useTempStorage(t)
	instance := newInstanceDirectory(t)

	first, err := CreateInstanceSnapshot(context.Background(), instance, "", "")
	if err != nil {
		t.Fatal(err)
	}
	writeWorldFile(t, instance, "mods/optimization.jar", "mod-2")

	// 删掉数据块制造"恢复中途失败"
	manifest, err := loadSnapshotManifest(snapshotRepoDirectory(instance), first.Id)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range manifest.Files {
		_ = os.Remove(snapshotBlobPath(snapshotBlobsDirectory(), file.Hash))
	}

	safety, err := RollbackInstanceSnapshot(context.Background(), instance, first.Id)
	if err == nil {
		t.Fatal("数据块缺失时应报错")
	}
	if safety.Id == "" {
		t.Fatal("失败时也应返回回滚前安全快照")
	}
	data, err := os.ReadFile(filepath.Join(instance, "mods", "optimization.jar"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "mod-2" {
		t.Fatalf("回滚失败后实例不该被改动，实际内容 %q", data)
	}
}

// TestGlobalBlobsSharedAcrossTargets 内容寻址库全局共享：两个目标引用相同内容
// 只占一份盘；删除其中一边的快照时，被另一边引用的数据块不会被 GC。
func TestGlobalBlobsSharedAcrossTargets(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)
	instance := newInstanceDirectory(t)
	// 与世界文件相同内容：应命中同一数据块
	writeWorldFile(t, instance, "mods/duplicate.bin", "region-1")

	if _, err := CreateSaveSnapshot(context.Background(), world, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateInstanceSnapshot(context.Background(), instance, "", ""); err != nil {
		t.Fatal(err)
	}
	if blobs := countBlobs(t, world); blobs != 4 {
		t.Fatalf("两个目标应共享数据块（level-1/region-1/config/mod-1），得到 %d", blobs)
	}

	list := ListSaveSnapshots(world)
	if err := DeleteSaveSnapshot(world, list[0].Id); err != nil {
		t.Fatal(err)
	}
	// level-1 块只被世界快照引用，随删除被 GC（正确）；共享的 region-1 块必须留下
	sharedSum := sha256.Sum256([]byte("region-1"))
	sharedHash := hex.EncodeToString(sharedSum[:])
	if _, err := os.Stat(snapshotBlobPath(snapshotBlobsDirectory(), sharedHash)); err != nil {
		t.Fatalf("被另一目标引用的共享数据块不应被回收：%v", err)
	}
	if blobs := countBlobs(t, world); blobs != 3 {
		t.Fatalf("GC 后应剩 3 个数据块（level-1 被回收），得到 %d", blobs)
	}
}

// TestLegacySnapshotStoreMigration 旧版 save-snapshots 布局在首次操作时自动迁移：
// 清单搬到新仓库、blob 并入全局库、旧根目录清空。
func TestLegacySnapshotStoreMigration(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)

	// 手工搭建旧版布局：一个含清单与 blob 的 repo
	content := []byte("level-1")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	// 旧版 repo 键与现役派生方式一致（同一世界路径 → 同一键）
	repo := filepath.Join(config.StorageDirectory(), "save-snapshots", snapshotRepoKey(world))
	manifests := filepath.Join(repo, "manifests")
	blobs := filepath.Join(repo, "blobs", hash[:2])
	for _, directory := range []string{manifests, blobs} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(blobs, hash), content, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := saveSnapshotManifest{
		Id:         "20260101-000000.000",
		CreatedAt:  time.Now(),
		Reason:     "manual",
		Kind:       "save",
		FileCount:  1,
		TotalSize:  int64(len(content)),
		AddedSize:  int64(len(content)),
		AddedFiles: 1,
		WorldName:  "MyWorld",
		Files:      []SaveSnapshotFile{{Path: "level.dat", Hash: hash, Size: int64(len(content))}},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifests, manifest.Id+".json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}

	list := ListSaveSnapshots(world)
	if len(list) != 1 || list[0].Id != manifest.Id {
		t.Fatalf("迁移后旧快照应可见：%+v", list)
	}
	if _, err := os.Stat(filepath.Join(config.StorageDirectory(), "save-snapshots")); !os.IsNotExist(err) {
		t.Fatal("迁移后旧根目录应被清理")
	}
	if countBlobs(t, world) != 1 {
		t.Fatalf("旧 blob 应并入全局库，得到 %d", countBlobs(t, world))
	}

	// 迁移后的快照应可回滚
	if _, err := RollbackSaveSnapshot(context.Background(), world, manifest.Id); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(world, "level.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "level-1" {
		t.Fatalf("回滚后内容不符：%q", data)
	}
}
