package content

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSnapshotCatchesSameSizeSameMTimeEdit 变更检测必须看内容：同一文件系统时间
// 粒度内、大小又恰好不变的改动，以前会被"mtime + size 没变"判成未修改而漏掉。
func TestSnapshotCatchesSameSizeSameMTimeEdit(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)

	first, err := CreateSaveSnapshot(context.Background(), world, "", "")
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(world, "region", "r.0.0.mca")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	// 内容改成同样长度，并把 mtime 复原成原值——只有读内容才能发现这次改动
	if err := os.WriteFile(target, []byte("region-9"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	second, err := CreateSaveSnapshot(context.Background(), world, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.AddedFiles != 1 {
		t.Fatalf("同尺寸同 mtime 的改写应被识别为 1 个变化文件，得到 %d", second.AddedFiles)
	}

	// 回滚到第一个快照后内容要回到旧值，说明新内容确实进了仓库
	if _, err := RollbackSaveSnapshot(context.Background(), world, first.Id); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "region-1" {
		t.Fatalf("回滚后内容不符：%q", data)
	}
}

// TestRollbackSwapsWorldAtomically 回滚走"先恢复到临时目录再整体替换"：
// 成功后临时目录/备份目录都不该留在存档旁边。
func TestRollbackSwapsWorldAtomically(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)

	first, err := CreateSaveSnapshot(context.Background(), world, "", "")
	if err != nil {
		t.Fatal(err)
	}
	writeWorldFile(t, world, "region/r.0.0.mca", "region-2")

	if _, err := RollbackSaveSnapshot(context.Background(), world, first.Id); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(world, "region", "r.0.0.mca"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "region-1" {
		t.Fatalf("回滚后内容不符：%q", data)
	}

	entries, err := os.ReadDir(filepath.Dir(world))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "nya-restore") || strings.Contains(entry.Name(), "nya-rollback-backup") {
			t.Fatalf("回滚后不该留下临时目录：%s", entry.Name())
		}
	}
}

// TestRollbackFailureKeepsWorldIntact 恢复阶段失败时世界目录必须原封不动
// （临时目录里失败 = 世界没被碰过）。
func TestRollbackFailureKeepsWorldIntact(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)

	first, err := CreateSaveSnapshot(context.Background(), world, "", "")
	if err != nil {
		t.Fatal(err)
	}
	writeWorldFile(t, world, "region/r.0.0.mca", "region-2")

	// 把快照要用的数据块删掉，制造"恢复中途失败"
	repo := snapshotRepoDirectory(world)
	target, err := loadSnapshotManifest(repo, first.Id)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range target.Files {
		_ = os.Remove(snapshotBlobPath(snapshotBlobsDirectory(), file.Hash))
	}

	if _, err := RollbackSaveSnapshot(context.Background(), world, first.Id); err == nil {
		t.Fatal("数据块缺失时应报错")
	}

	data, err := os.ReadFile(filepath.Join(world, "region", "r.0.0.mca"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "region-2" {
		t.Fatalf("回滚失败后世界不该被改动，实际内容 %q", data)
	}
	if _, err := os.Stat(filepath.Join(world, "level.dat")); err != nil {
		t.Fatalf("回滚失败后世界文件丢失：%v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(world))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "nya-restore") || strings.Contains(entry.Name(), "nya-rollback-backup") {
			t.Fatalf("失败后不该留下临时目录：%s", entry.Name())
		}
	}
}

// TestPruneKeepsRecentAutoSnapshots 自动安全快照按数量上限淘汰最旧的。
func TestPruneKeepsRecentAutoSnapshots(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)
	repo := snapshotRepoDirectory(world)
	manifestsDir := filepath.Join(repo, snapshotManifestDirName)
	if err := os.MkdirAll(manifestsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 造 1 + 上限 个自动快照（时间递增，id 递增）
	created := make([]string, 0, snapshotMaxAutoSnapshots+1)
	for index := 0; index <= snapshotMaxAutoSnapshots; index++ {
		manifest := saveSnapshotManifest{
			Id:        "auto-" + pad2(index),
			CreatedAt: time.Now().Add(time.Duration(index) * time.Second),
			Reason:    snapshotReasonBeforeRollback,
			AddedSize: 1,
		}
		if err := writeSnapshotManifest(repo, manifest); err != nil {
			t.Fatal(err)
		}
		created = append(created, manifest.Id)
	}

	if err := pruneSnapshotsLocked(repo, ""); err != nil {
		t.Fatal(err)
	}

	remaining := map[string]bool{}
	for _, manifest := range loadManifestsForTest(t, repo) {
		remaining[manifest.Id] = true
	}
	if len(remaining) != snapshotMaxAutoSnapshots {
		t.Fatalf("应只保留 %d 个自动快照，实际 %d：%v", snapshotMaxAutoSnapshots, len(remaining), remaining)
	}
	if remaining[created[0]] {
		t.Fatalf("最旧的自动快照应被淘汰：%v", remaining)
	}
	if !remaining[created[len(created)-1]] {
		t.Fatalf("最新的自动快照不该被淘汰：%v", remaining)
	}

	// 刚创建的那个（keepId）即使超出数量上限也不能被自己触发的清理删掉
	if err := writeSnapshotManifest(repo, saveSnapshotManifest{
		Id:        "just-created",
		CreatedAt: time.Now().Add(time.Hour),
		Reason:    snapshotReasonBeforeRollback,
		AddedSize: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := pruneSnapshotsLocked(repo, "just-created"); err != nil {
		t.Fatal(err)
	}
	for _, manifest := range loadManifestsForTest(t, repo) {
		if manifest.Id == "just-created" {
			return
		}
	}
	t.Fatal("刚创建的快照被自己触发的清理删掉了")
}

// TestPruneNeverDeletesTaggedSnapshots 带用户标记的快照永不自动删除，
// 体积超预算时优先淘汰无标记的。
func TestPruneNeverDeletesTaggedSnapshots(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)
	repo := snapshotRepoDirectory(world)
	manifestsDir := filepath.Join(repo, snapshotManifestDirName)
	if err := os.MkdirAll(manifestsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 三个快照的 AddedSize 之和超过预算；只有中间那个带标记
	huge := snapshotMaxTotalBytes/2 + 1
	for index, spec := range []struct {
		id      string
		label   string
		added   int64
		created time.Time
	}{
		{"oldest", "", huge, time.Now().Add(-3 * time.Hour)},
		{"tagged", "别删我", huge, time.Now().Add(-2 * time.Hour)},
		{"newest", "", huge, time.Now().Add(-time.Hour)},
	} {
		manifest := saveSnapshotManifest{
			Id:        spec.id,
			CreatedAt: spec.created,
			Label:     spec.label,
			AddedSize: spec.added,
			Reason:    snapshotReasonManual,
		}
		if err := writeSnapshotManifest(repo, manifest); err != nil {
			t.Fatal(err)
		}
		_ = index
	}

	if err := pruneSnapshotsLocked(repo, ""); err != nil {
		t.Fatal(err)
	}

	remaining := map[string]bool{}
	for _, manifest := range loadManifestsForTest(t, repo) {
		remaining[manifest.Id] = true
	}
	if !remaining["tagged"] {
		t.Fatalf("带标记的快照被删了：%v", remaining)
	}
	if remaining["oldest"] {
		t.Fatalf("超预算时应从最旧的无标记快照开始淘汰：%v", remaining)
	}
}

// TestNewSaveSnapshotIDSuffixSortsNumerically 冲突后缀补零，字符串排序与时间顺序一致。
func TestNewSaveSnapshotIDSuffixSortsNumerically(t *testing.T) {
	directory := t.TempDir()
	fixed := time.Date(2026, 9, 24, 22, 30, 0, 0, time.Local)

	first := newSnapshotIDAt(directory, fixed)
	if err := os.WriteFile(filepath.Join(directory, first+".json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := newSnapshotIDAt(directory, fixed)
	if second == first {
		t.Fatal("重名时应换一个 id")
	}
	if !strings.HasPrefix(second, first) {
		t.Fatalf("冲突 id 应基于同一时间戳：%q / %q", first, second)
	}
	if !strings.HasSuffix(second, "-01") {
		t.Fatalf("冲突后缀应补零，得到 %q", second)
	}
	if second <= first {
		t.Fatalf("补零后字符串序应与时间序一致：%q 应大于 %q", second, first)
	}

	// 一路造到 -10：补零后 "…-10" 仍排在 "…-02" 之后
	for index := 1; index <= 10; index++ {
		id := newSnapshotIDAt(directory, fixed)
		if err := os.WriteFile(filepath.Join(directory, id+".json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tenth := first + "-10"
	if _, err := os.Stat(filepath.Join(directory, tenth+".json")); err != nil {
		t.Fatalf("第 10 个冲突 id 应是 %q：%v", tenth, err)
	}
	if tenth <= first+"-02" {
		t.Fatalf("补零后 -10 不该排在 -02 前面：%q vs %q", tenth, first+"-02")
	}
}

// loadManifestsForTest 读仓库里的全部清单（测试用）。
func loadManifestsForTest(t *testing.T, repo string) []saveSnapshotManifest {
	t.Helper()

	manifests, err := loadSnapshotManifests(repo)
	if err != nil {
		t.Fatal(err)
	}

	return manifests
}

// pad2 两位补零（测试里造递增 id 用）。
func pad2(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}

	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}
