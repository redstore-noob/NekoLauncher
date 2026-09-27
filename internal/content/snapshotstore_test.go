package content

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"nekolauncher/internal/config"
)

// useTempStorage 把启动器存储目录指到临时目录，避免测试污染真实用户数据。
func useTempStorage(t *testing.T) {
	t.Helper()
	original := config.StorageDirectory()
	if err := config.SetStorageDirectory(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.SetStorageDirectory(original) })
}

func writeWorldFile(t *testing.T, world, relative, content string) {
	t.Helper()
	full := filepath.Join(world, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestWorld(t *testing.T) string {
	t.Helper()
	world := filepath.Join(t.TempDir(), "MyWorld")
	if err := os.MkdirAll(world, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorldFile(t, world, "level.dat", "level-1")
	writeWorldFile(t, world, "region/r.0.0.mca", "region-1")
	writeWorldFile(t, world, "session.lock", "lock")
	return world
}

func countBlobs(t *testing.T, world string) int {
	t.Helper()
	blobsDir := snapshotBlobsDirectory()
	count := 0
	_ = filepath.WalkDir(blobsDir, func(
		_ string,
		entry os.DirEntry,
		err error,
	) error {
		if err == nil && !entry.IsDir() {
			count++
		}
		return nil
	})
	return count
}

func TestCreateSaveSnapshotCapturesFiles(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)

	snapshot, err := CreateSaveSnapshot(context.Background(), world, "first", "")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.FileCount != 2 {
		t.Fatalf("应捕获 2 个文件（不含 session.lock），得到 %d", snapshot.FileCount)
	}
	if snapshot.AddedFiles != 2 {
		t.Fatalf("首个快照应新增 2 个文件，得到 %d", snapshot.AddedFiles)
	}
	if snapshot.TotalSize != int64(len("level-1")+len("region-1")) {
		t.Fatalf("总大小不符：%d", snapshot.TotalSize)
	}

	list := ListSaveSnapshots(world)
	if len(list) != 1 || list[0].Id != snapshot.Id {
		t.Fatalf("快照列表不符：%+v", list)
	}
	if list[0].Label != "first" {
		t.Fatalf("备注丢失：%q", list[0].Label)
	}
}

func TestSnapshotColorRoundTrip(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)

	// 创建时带颜色 + 非法颜色回退为未标记
	tagged, err := CreateSaveSnapshot(context.Background(), world, "标记", "Danger")
	if err != nil {
		t.Fatal(err)
	}
	if tagged.Color != "danger" {
		t.Fatalf("颜色应归一化为小写：%q", tagged.Color)
	}
	if _, err := CreateSaveSnapshot(context.Background(), world, "", "rainbow"); err != nil {
		t.Fatal(err)
	}

	list := ListSaveSnapshots(world)
	byLabel := map[string]string{}
	for _, snapshot := range list {
		byLabel[snapshot.Label] = snapshot.Color
	}
	if byLabel["标记"] != "danger" {
		t.Fatalf("清单应持久化颜色：%q", byLabel["标记"])
	}
	if byLabel[""] != "" {
		t.Fatalf("非法颜色应回退为未标记：%q", byLabel[""])
	}

	// 更新已有快照颜色
	untagged := list[0]
	if untagged.Label == "标记" {
		untagged = list[1]
	}
	if err := SetSaveSnapshotColor(world, untagged.Id, "success"); err != nil {
		t.Fatal(err)
	}
	after := ListSaveSnapshots(world)
	for _, snapshot := range after {
		if snapshot.Id == untagged.Id && snapshot.Color != "success" {
			t.Fatalf("颜色更新未生效：%q", snapshot.Color)
		}
	}

	// 恢复默认 + 不存在的快照应报错
	if err := SetSaveSnapshotColor(world, untagged.Id, ""); err != nil {
		t.Fatal(err)
	}
	if err := SetSaveSnapshotColor(world, "no-such-id", "danger"); err == nil {
		t.Fatal("不存在的快照应报错")
	}
}

func TestSnapshotDeduplicatesUnchangedFiles(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)

	if _, err := CreateSaveSnapshot(context.Background(), world, "", ""); err != nil {
		t.Fatal(err)
	}
	// 只改一个文件：另一个文件应复用旧块，不重复占盘
	writeWorldFile(t, world, "region/r.0.0.mca", "region-2")
	second, err := CreateSaveSnapshot(context.Background(), world, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.AddedFiles != 1 {
		t.Fatalf("应只有 1 个文件变化，得到 %d", second.AddedFiles)
	}
	if second.AddedSize != int64(len("region-2")) {
		t.Fatalf("新增占盘应只有变化的文件，得到 %d", second.AddedSize)
	}
	if countBlobs(t, world) != 3 {
		t.Fatalf("应共有 3 个数据块（level-1 / region-1 / region-2），得到 %d", countBlobs(t, world))
	}
}

func TestRollbackRestoresSnapshot(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)

	first, err := CreateSaveSnapshot(context.Background(), world, "", "")
	if err != nil {
		t.Fatal(err)
	}
	writeWorldFile(t, world, "region/r.0.0.mca", "region-2")
	writeWorldFile(t, world, "extra/note.txt", "extra")
	if _, err := CreateSaveSnapshot(context.Background(), world, "", ""); err != nil {
		t.Fatal(err)
	}

	safety, err := RollbackSaveSnapshot(context.Background(), world, first.Id)
	if err != nil {
		t.Fatal(err)
	}
	if safety.Reason != snapshotReasonBeforeRollback {
		t.Fatalf("回滚前应创建安全快照，得到 %q", safety.Reason)
	}

	data, err := os.ReadFile(filepath.Join(world, "region", "r.0.0.mca"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "region-1" {
		t.Fatalf("回滚后内容不符：%q", data)
	}
	if _, err := os.Stat(filepath.Join(world, "extra")); !os.IsNotExist(err) {
		t.Fatal("回滚应删除快照中不存在的额外文件")
	}
}

func TestDeleteSnapshotGarbageCollectsBlobs(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)

	first, err := CreateSaveSnapshot(context.Background(), world, "", "")
	if err != nil {
		t.Fatal(err)
	}
	writeWorldFile(t, world, "region/r.0.0.mca", "region-2")
	second, err := CreateSaveSnapshot(context.Background(), world, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := DeleteSaveSnapshot(world, second.Id); err != nil {
		t.Fatal(err)
	}
	list := ListSaveSnapshots(world)
	if len(list) != 1 || list[0].Id != first.Id {
		t.Fatalf("删除后应只剩第一个快照：%+v", list)
	}
	if countBlobs(t, world) != 2 {
		t.Fatalf("孤立数据块应被回收，应剩 2 个，得到 %d", countBlobs(t, world))
	}
}
