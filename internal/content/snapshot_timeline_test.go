package content

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 本文件复用 snapshotstore_test.go 的既有助手：
//   - useTempStorage：隔离存储目录
//   - newTestWorld / writeWorldFile：造世界目录

// ---------------------------------------------------------------------------
// 自动快照来源判定 —— 新增自动来源必须登记，否则永不淘汰
// ---------------------------------------------------------------------------

func TestIsAutomaticSnapshotReason(t *testing.T) {
	cases := []struct {
		reason string
		want   bool
	}{
		{snapshotReasonManual, false},
		{snapshotReasonBeforeRollback, true},
		{snapshotReasonBeforeLaunch, true},
		{"", false},
		{"something-else", false},
	}
	for _, testCase := range cases {
		if got := isAutomaticSnapshotReason(testCase.reason); got != testCase.want {
			t.Errorf("isAutomaticSnapshotReason(%q) = %v，期望 %v",
				testCase.reason, got, testCase.want)
		}
	}
}

// setWorldFileTime 把世界目录里所有文件的修改时间推到指定时刻。
func setWorldFileTime(t *testing.T, world string, moment time.Time) {
	t.Helper()

	err := filepath.WalkDir(world, func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return nil
		}
		return os.Chtimes(path, moment, moment)
	})
	if err != nil {
		t.Fatalf("设置文件时间失败：%v", err)
	}
}

func listWorldManifests(t *testing.T, world string) []saveSnapshotManifest {
	t.Helper()

	normalized, err := normalizeSnapshotDirectory(world)
	if err != nil {
		t.Fatalf("规范化目录失败：%v", err)
	}
	manifests, err := loadSnapshotManifests(snapshotRepoDirectory(normalized))
	if err != nil {
		t.Fatalf("读取清单失败：%v", err)
	}
	return manifests
}

// ---------------------------------------------------------------------------
// 变更检测：连续启动不留重复还原点
// ---------------------------------------------------------------------------

func TestShouldCreateLaunchSnapshotOnFirstRun(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)

	// 从没快照过：第一次启动必须留一个还原点
	if !shouldCreateLaunchSnapshot(world) {
		t.Error("首次启动应创建还原点")
	}
}

func TestShouldNotCreateLaunchSnapshotWhenUnchanged(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)

	// 文件时间推到过去，而快照是刚刚创建的 → 没有新变化
	setWorldFileTime(t, world, time.Now().Add(-2*time.Hour))
	if _, err := CreateSaveSnapshot(context.Background(), world, "", ""); err != nil {
		t.Fatalf("创建快照失败：%v", err)
	}

	// 存档没变过：连续启动不该再堆一个一模一样的还原点
	if shouldCreateLaunchSnapshot(world) {
		t.Error("内容未变化时不该重复创建还原点")
	}
}

func TestShouldCreateLaunchSnapshotAfterChange(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)
	setWorldFileTime(t, world, time.Now().Add(-2*time.Hour))

	if _, err := CreateSaveSnapshot(context.Background(), world, "", ""); err != nil {
		t.Fatalf("创建快照失败：%v", err)
	}

	// 玩家又玩了一会儿：文件比快照新
	writeWorldFile(t, world, "region/r.0.1.mca", "region-new")

	if !shouldCreateLaunchSnapshot(world) {
		t.Error("内容变化后应创建还原点")
	}
}

// TestShouldCreateLaunchSnapshotOnCoarseTimestampFilesystem 防的回归：CI（Linux）
// 上这条曾一直红。粗粒度时间戳的文件系统（ext3/HFS+ 秒级、FAT 2 秒级）会把刚
// 写出的文件 mtime 截断到整秒，于是"快照之后紧接着发生的改动"看上去比快照还旧；
// 一旦拿快照的墙钟时间（纳秒）当基准，这次改动就被判成"没变"，还原点没了。
// 这里显式把新文件的 mtime 截断到整秒，模拟这种文件系统。
func TestShouldCreateLaunchSnapshotOnCoarseTimestampFilesystem(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)
	setWorldFileTime(t, world, time.Now().Add(-2*time.Hour))

	if _, err := CreateSaveSnapshot(context.Background(), world, "", ""); err != nil {
		t.Fatalf("创建快照失败：%v", err)
	}

	path := filepath.Join(world, "region", "r.0.1.mca")
	writeWorldFile(t, world, "region/r.0.1.mca", "region-new")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	floored := info.ModTime().Truncate(time.Second)
	if err := os.Chtimes(path, floored, floored); err != nil {
		t.Fatal(err)
	}

	if !shouldCreateLaunchSnapshot(world) {
		t.Error("秒级时间戳的改动同样算变化，不能因为文件系统粒度粗就漏掉还原点")
	}
}

// TestShouldIgnoreSessionLockWhenDetectingChange 防的回归：session.lock 由游戏
// 每次启动重写，且不参与快照内容。把它算成"内容变化"，连续启动就会每次多留一个
// 内容完全相同的还原点——正是"不留重复还原点"要避免的。
func TestShouldIgnoreSessionLockWhenDetectingChange(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)
	setWorldFileTime(t, world, time.Now().Add(-2*time.Hour))

	if _, err := CreateSaveSnapshot(context.Background(), world, "", ""); err != nil {
		t.Fatalf("创建快照失败：%v", err)
	}

	// 游戏启动了一次：只重写了会话锁
	writeWorldFile(t, world, "session.lock", "lock-2")

	if shouldCreateLaunchSnapshot(world) {
		t.Error("仅 session.lock 变化（不参与快照内容）时不该新增还原点")
	}
}

// ---------------------------------------------------------------------------
// CreateLaunchSnapshot
// ---------------------------------------------------------------------------

func TestCreateLaunchSnapshotMarksReason(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)

	snapshot, created, err := CreateLaunchSnapshot(context.Background(), world)
	if err != nil {
		t.Fatalf("创建失败：%v", err)
	}
	if !created {
		t.Fatal("首次启动应真的创建了快照")
	}
	// 来源必须是 before-launch：否则会被当成手动快照而永不自动清理
	if snapshot.Reason != snapshotReasonBeforeLaunch {
		t.Errorf("来源 = %q，期望 %q", snapshot.Reason, snapshotReasonBeforeLaunch)
	}
	if snapshot.Label != snapshotLaunchLabel {
		t.Errorf("备注 = %q，期望 %q", snapshot.Label, snapshotLaunchLabel)
	}

	// 落盘的清单也要带上来源（重启后仍能正确淘汰）
	manifests := listWorldManifests(t, world)
	if len(manifests) != 1 {
		t.Fatalf("落盘清单数 = %d，期望 1", len(manifests))
	}
	if manifests[0].Reason != snapshotReasonBeforeLaunch {
		t.Errorf("落盘来源 = %q，期望 %q", manifests[0].Reason, snapshotReasonBeforeLaunch)
	}
}

func TestCreateLaunchSnapshotSkipsWhenUnchanged(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)
	setWorldFileTime(t, world, time.Now().Add(-2*time.Hour))

	_, created, err := CreateLaunchSnapshot(context.Background(), world)
	if err != nil {
		t.Fatalf("第一次创建失败：%v", err)
	}
	if !created {
		t.Fatal("第一次应创建")
	}

	// 第二次：存档没动过
	_, createdAgain, err := CreateLaunchSnapshot(context.Background(), world)
	if err != nil {
		t.Fatalf("第二次创建失败：%v", err)
	}
	if createdAgain {
		t.Error("内容未变化时第二次不该再创建")
	}

	// 确认磁盘上确实只有一个
	if manifests := listWorldManifests(t, world); len(manifests) != 1 {
		t.Errorf("快照数 = %d，期望 1（连续启动不该堆重复还原点）", len(manifests))
	}
}

func TestCreateLaunchSnapshotEmptyDirectoryIsNoop(t *testing.T) {
	useTempStorage(t)

	snapshot, created, err := CreateLaunchSnapshot(context.Background(), "")
	if err != nil {
		t.Fatalf("空路径不该报错：%v", err)
	}
	if created {
		t.Error("空路径不该创建快照")
	}
	if snapshot.Id != "" {
		t.Errorf("应返回零值快照，实际 %+v", snapshot)
	}
}

// ---------------------------------------------------------------------------
// 淘汰：自动快照上限必须覆盖 before-launch
// ---------------------------------------------------------------------------

func TestLaunchSnapshotsArePrunedByAutoLimit(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)
	setWorldFileTime(t, world, time.Now().Add(-1000*time.Hour))

	created := 0
	for index := 0; index < snapshotMaxAutoSnapshots+5; index++ {
		// 每次改内容，保证"有变化"而不被跳过
		writeWorldFile(t, world, "level.dat", "seed-"+itoa(index))
		// 时间推到过去，让变更检测认为"存档在这之后被改过"
		setWorldFileTime(t, world, time.Now().Add(
			-time.Duration(900-index)*time.Hour))

		_, wasCreated, err := CreateLaunchSnapshot(context.Background(), world)
		if err != nil {
			t.Fatalf("第 %d 次创建失败：%v", index, err)
		}
		if wasCreated {
			created++
		}
	}
	if created == 0 {
		t.Fatal("一次都没创建成功，测试前提不成立")
	}

	manifests := listWorldManifests(t, world)
	automatic := 0
	for _, manifest := range manifests {
		if isAutomaticSnapshotReason(manifest.Reason) {
			automatic++
		}
	}
	// 这是本功能最关键的一条：若不把 before-launch 算进自动上限，
	// 这个数字会一直涨到 created，磁盘会被无声写满。
	if automatic > snapshotMaxAutoSnapshots {
		t.Errorf("自动快照数 = %d，超过上限 %d（before-launch 没被计入淘汰）",
			automatic, snapshotMaxAutoSnapshots)
	}
	if len(manifests) == 0 {
		t.Error("淘汰不该把所有快照都清掉")
	}
}

// ---------------------------------------------------------------------------
// 与手动快照共存
// ---------------------------------------------------------------------------

func TestManualSnapshotsSurviveAutomaticPruning(t *testing.T) {
	useTempStorage(t)
	world := newTestWorld(t)
	setWorldFileTime(t, world, time.Now().Add(-1000*time.Hour))

	// 用户手动打了个标记（带备注 → 受保护）
	manual, err := CreateSaveSnapshot(context.Background(), world, "重要进度", "")
	if err != nil {
		t.Fatalf("创建手动快照失败：%v", err)
	}

	// 再堆一堆自动快照
	for index := 0; index < snapshotMaxAutoSnapshots+3; index++ {
		writeWorldFile(t, world, "level.dat", "v"+itoa(index))
		setWorldFileTime(t, world, time.Now().Add(
			-time.Duration(800-index)*time.Hour))
		if _, _, err := CreateLaunchSnapshot(context.Background(), world); err != nil {
			t.Fatalf("创建启动快照失败：%v", err)
		}
	}

	manifests := listWorldManifests(t, world)
	found := false
	for _, manifest := range manifests {
		if manifest.Id == manual.Id {
			found = true
		}
	}
	if !found {
		t.Error("带用户标记的手动快照被自动淘汰了（应当永不删除）")
	}
}
