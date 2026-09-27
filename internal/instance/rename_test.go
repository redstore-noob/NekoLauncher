package instance

// RenameVersion 的行为：目录与 JSON 同步改名、下游 inheritsFrom 引用修补、
// 非法/占用目标的拒绝。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRenameFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(root, filepath.FromSlash(rel));
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("versions/old/old.json", `{"id":"old","inheritsFrom":"1.21.1"}`)
	write("versions/child/child.json", `{"id":"child","inheritsFrom":"old"}`)

	return root
}

func TestRenameVersionRenamesAndPatchesReferences(t *testing.T) {
	root := newRenameFixture(t)

	newID, err := RenameVersion(context.Background(), root, "old", "shiny")
	if err != nil {
		t.Fatalf("改名失败：%v", err)
	}
	if newID != "shiny" {
		t.Errorf("返回的新 id 不符：%q", newID)
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "shiny", "shiny.json")); err != nil {
		t.Errorf("目录与 JSON 应同步改名为 shiny：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "old")); !os.IsNotExist(err) {
		t.Error("原目录应不存在")
	}
	// 下游版本引用旧 id 的 inheritsFrom 应被修补
	childRaw, err := os.ReadFile(filepath.Join(root, "versions", "child", "child.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(childRaw), `"shiny"`) {
		t.Errorf("下游 inheritsFrom 未修补：%s", string(childRaw))
	}
}

func TestRenameVersionSameIdIsNoop(t *testing.T) {
	root := newRenameFixture(t)

	newID, err := RenameVersion(context.Background(), root, "old", "old")
	if err != nil {
		t.Fatalf("同名改名应无副作用：%v", err)
	}
	if newID != "old" {
		t.Errorf("应原样返回：%q", newID)
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "old", "old.json")); err != nil {
		t.Errorf("实例不应被破坏：%v", err)
	}
}

func TestRenameVersionRejectsBadTargets(t *testing.T) {
	root := newRenameFixture(t)

	for _, tc := range [][2]string{
		{"old", "child"},    // 目标已占用
		{"old", "../evil"},  // 路径穿越
		{"old", `a\b`},      // 分隔符
		{"ghost", "ok"},     // 原版本不存在
	} {
		if _, err := RenameVersion(context.Background(), root, tc[0], tc[1]); err == nil {
			t.Errorf("（%q → %q）应被拒绝", tc[0], tc[1])
		}
	}
	// 被拒绝的改名不应留下半成品
	if _, err := os.Stat(filepath.Join(root, "versions", "old", "old.json")); err != nil {
		t.Errorf("原实例不应被破坏：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "evil")); err == nil {
		t.Error("不应出现逃逸目录")
	}
}
