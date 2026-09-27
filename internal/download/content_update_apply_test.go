package download_test

// 落地动作（另存 / 备份并替换）的离线用例。
//
// 这是本功能里唯一会改动用户文件的操作，防的回归是"静默覆盖 / 覆盖失败留下半截文件"：
//   - 替换前必须留下备份，且备份里是**原内容**；
//   - 下载内容与期望哈希不符时必须拒绝替换，原文件一个字节都不能动；
//   - 原文件不存在时也要如实说明（无备份），不能假装备份过。

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekolauncher/internal/download"
)

// contentServer 提供一份固定内容，并返回其 SHA-1。
func contentServer(t *testing.T, content string) (string, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(content))
	}))
	t.Cleanup(server.Close)

	sum := sha1.Sum([]byte(content))
	return server.URL + "/file.jar", hex.EncodeToString(sum[:])
}

func readFileText(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败：%v", path, err)
	}
	return string(raw)
}

// TestApplyContentUpdateBacksUpBeforeReplace 防的回归：
// 替换时直接覆盖原文件（用户丢掉旧版本，而且没有任何回退手段）。
func TestApplyContentUpdateBacksUpBeforeReplace(t *testing.T) {
	url, hash := contentServer(t, "new-content")
	target := filepath.Join(t.TempDir(), "sodium.jar")
	if err := os.WriteFile(target, []byte("old-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := download.ApplyContentUpdate(context.Background(), url, target, hash, nil)
	if err != nil {
		t.Fatalf("替换失败：%v", err)
	}
	if result.BackupPath == "" {
		t.Fatal("替换必须留下备份路径")
	}
	if !strings.Contains(filepath.Base(result.BackupPath), ".bak-") {
		t.Errorf("备份文件名应带时间戳后缀，实际 %q", filepath.Base(result.BackupPath))
	}
	if got := readFileText(t, target); got != "new-content" {
		t.Errorf("目标文件内容 = %q，期望 new-content", got)
	}
	if got := readFileText(t, result.BackupPath); got != "old-content" {
		t.Errorf("备份内容 = %q，期望原内容 old-content", got)
	}
	if result.SHA1 != hash {
		t.Errorf("返回哈希 = %q，期望 %q", result.SHA1, hash)
	}
	if !strings.Contains(result.Message, "备份") {
		t.Errorf("结果说明应提到备份，实际 %q", result.Message)
	}
}

// TestApplyContentUpdateRejectsHashMismatch 防的回归：
// 下载到残缺/被换掉的文件也照样覆盖（游戏直接崩，且原文件已经被毁）。
func TestApplyContentUpdateRejectsHashMismatch(t *testing.T) {
	url, _ := contentServer(t, "tampered-content")
	target := filepath.Join(t.TempDir(), "sodium.jar")
	if err := os.WriteFile(target, []byte("old-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := download.ApplyContentUpdate(context.Background(), url, target,
		"0000000000000000000000000000000000000000", nil)
	if err == nil {
		t.Fatal("哈希不符必须报错")
	}
	if !strings.Contains(err.Error(), "SHA-1") {
		t.Errorf("错误信息应说明哈希校验失败，实际 %v", err)
	}
	if got := readFileText(t, target); got != "old-content" {
		t.Errorf("校验失败后原文件必须原样保留，实际 %q", got)
	}
	// 临时文件不能留下（下次替换会误用）
	entries, _ := os.ReadDir(filepath.Dir(target))
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".nya-update") {
			t.Errorf("失败后不应残留临时文件：%s", entry.Name())
		}
	}
}

// TestApplyContentUpdateWithoutOriginal 防的回归：
// 目标原本不存在时假装做过备份（前端会去展示一个不存在的备份路径）。
func TestApplyContentUpdateWithoutOriginal(t *testing.T) {
	url, hash := contentServer(t, "fresh-content")
	target := filepath.Join(t.TempDir(), "brand-new.jar")

	result, err := download.ApplyContentUpdate(context.Background(), url, target, hash, nil)
	if err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	if result.BackupPath != "" {
		t.Errorf("原本没有文件时不应报备份路径，实际 %q", result.BackupPath)
	}
	if !strings.Contains(result.Message, "无备份") {
		t.Errorf("应如实说明没有备份，实际 %q", result.Message)
	}
	if got := readFileText(t, target); got != "fresh-content" {
		t.Errorf("目标文件内容 = %q", got)
	}
}

// TestDownloadContentUpdateDoesNotTouchOriginal 防的回归：
// 「另存新版本」把原文件也改掉（用户只是想留一份新的，不想动现有文件）。
func TestDownloadContentUpdateDoesNotTouchOriginal(t *testing.T) {
	url, hash := contentServer(t, "saved-copy")
	original := filepath.Join(t.TempDir(), "sodium.jar")
	if err := os.WriteFile(original, []byte("old-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "sodium-1.1.0.jar")

	if _, err := download.DownloadContentUpdate(context.Background(), url, target, hash, nil); err != nil {
		t.Fatalf("另存失败：%v", err)
	}
	if got := readFileText(t, target); got != "saved-copy" {
		t.Errorf("另存文件内容 = %q", got)
	}
	if got := readFileText(t, original); got != "old-content" {
		t.Errorf("原文件不得被改动，实际 %q", got)
	}
}

// TestApplyContentUpdateRejectsEmptyArguments 防的回归：
// 空地址/空目标被当成合法输入，最终在 UI 上表现为"什么都没发生"却报成功。
func TestApplyContentUpdateRejectsEmptyArguments(t *testing.T) {
	target := filepath.Join(t.TempDir(), "x.jar")

	if _, err := download.ApplyContentUpdate(context.Background(), "", target, "", nil); err == nil {
		t.Error("空下载地址必须报错")
	}
	if _, err := download.ApplyContentUpdate(context.Background(), "https://x/y.jar", "  ", "", nil); err == nil {
		t.Error("空目标路径必须报错")
	}
}
