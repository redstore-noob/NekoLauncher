package content

import (
	"context"
	"testing"
)

// TestRewindSummaryEmptyStorage 没有任何仓库时返回零值汇总（预算仍是常量）。
func TestRewindSummaryEmptyStorage(t *testing.T) {
	useTempStorage(t)

	summary := ComputeRewindSummary()
	if summary.SnapshotCount != 0 {
		t.Fatalf("空存储的快照数应为 0，得到 %d", summary.SnapshotCount)
	}
	if summary.BlobBytes != 0 {
		t.Fatalf("空存储的数据块占用应为 0，得到 %d", summary.BlobBytes)
	}
	if summary.BudgetBytes != snapshotMaxTotalBytes {
		t.Fatalf("预算应等于 snapshotMaxTotalBytes，得到 %d", summary.BudgetBytes)
	}
	if !summary.LastSnapshotAt.IsZero() {
		t.Fatalf("空存储的最近快照时间应为零值，得到 %v", summary.LastSnapshotAt)
	}
	if len(summary.RecentColors) != 0 {
		t.Fatalf("空存储不应有迷你时间线，得到 %v", summary.RecentColors)
	}
}

// TestRewindSummaryCountsSnapshots 建两个存档各拍快照后，汇总应跨仓库统计。
func TestRewindSummaryCountsSnapshots(t *testing.T) {
	useTempStorage(t)

	worldA := newTestWorld(t)
	worldB := newTestWorld(t)

	if _, err := CreateSaveSnapshot(context.Background(), worldA, "开荒", "success"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateSaveSnapshot(context.Background(), worldB, "", "danger"); err != nil {
		t.Fatal(err)
	}

	summary := ComputeRewindSummary()
	if summary.SnapshotCount != 2 {
		t.Fatalf("两个仓库各一快照，汇总应为 2，得到 %d", summary.SnapshotCount)
	}
	if summary.BlobBytes <= 0 {
		t.Fatalf("有快照后数据块占用应大于 0，得到 %d", summary.BlobBytes)
	}
	if summary.LastSnapshotAt.IsZero() {
		t.Fatal("有快照后最近快照时间不应为零值")
	}
	if len(summary.RecentColors) != 2 {
		t.Fatalf("迷你时间线应有两个条目，得到 %v", summary.RecentColors)
	}
	// 新的在前：第一项是最后一次快照的标记色
	if summary.RecentColors[0] != "danger" {
		t.Fatalf("迷你时间线新的在前，第一项应为 danger，得到 %q", summary.RecentColors[0])
	}
}
