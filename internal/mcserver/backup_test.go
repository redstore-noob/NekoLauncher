package mcserver

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// useBackupTestServer 在临时根目录里造一台"服务器"（若干文件 + 一个 backups 目录）。
func useBackupTestServer(t *testing.T, id string) string {
	t.Helper()

	root := t.TempDir()
	serverRootOverride = &root
	t.Cleanup(func() { serverRootOverride = nil })

	dir := filepath.Join(root, id)
	writeTestFile(t, filepath.Join(dir, "server.properties"), "server-port=25565\nenable-rcon=true\n")
	writeTestFile(t, filepath.Join(dir, "server.jar"), "fake-jar")
	writeTestFile(t, filepath.Join(dir, "world", "level.dat"), "level-data")
	writeTestFile(t, filepath.Join(dir, "world", "region", "r.0.0.mca"), "region")
	writeTestFile(t, filepath.Join(dir, "logs", "latest.log"), "log-line\n")
	writeTestFile(t, filepath.Join(dir, "backups", "keep-me.txt"), "not-a-backup")

	return dir
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
}

// TestCreateServerBackupCold 冷备份：包含世界与配置，排除 backups/ 与 logs/。
func TestCreateServerBackupCold(t *testing.T) {
	id := "backup-cold"
	dir := useBackupTestServer(t, id)

	info, err := CreateServerBackup(context.Background(), id)
	if err != nil {
		t.Fatalf("冷备份失败：%v", err)
	}
	if info.Hot {
		t.Fatal("服务器没在跑，不应标记为热备份")
	}
	if !strings.HasPrefix(info.Name, backupColdPrefix) {
		t.Fatalf("文件名前缀不符：%s", info.Name)
	}
	if info.SizeBytes <= 0 {
		t.Fatalf("备份体积异常：%d", info.SizeBytes)
	}

	names := archiveEntries(t, info.Path)
	for _, want := range []string{"server.properties", "world/level.dat", "world/region/r.0.0.mca", "server.jar"} {
		if !names[want] {
			t.Errorf("备份里缺少 %s（实际：%v）", want, keysOf(names))
		}
	}
	// 排除项：备份自身目录（防递归）与日志
	for _, unwanted := range []string{"backups/keep-me.txt", "logs/latest.log"} {
		if names[unwanted] {
			t.Errorf("备份里不应包含 %s", unwanted)
		}
	}
	if !strings.HasPrefix(info.Path, filepath.Join(dir, backupDirectoryName)) {
		t.Fatalf("备份落点不对：%s", info.Path)
	}
}

// TestListAndDeleteServerBackup 列表按新→旧排序，删除只接受 backups 下的合法文件名。
func TestListAndDeleteServerBackup(t *testing.T) {
	id := "backup-list"
	useBackupTestServer(t, id)

	// 造两份假备份：文件名带时间戳，字典序即时间序
	directory := backupsDirectory(id)
	writeTestFile(t, filepath.Join(directory, backupColdPrefix+"20260101-000000.zip"), "older")
	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(directory, backupHotPrefix+"20260202-000000.zip"), "newer")

	backups := ListServerBackups(id)
	if len(backups) != 2 {
		t.Fatalf("备份数量 = %d，期望 2", len(backups))
	}
	if !backups[0].Hot || backups[0].Name != backupHotPrefix+"20260202-000000.zip" {
		t.Fatalf("最新的一份应排在最前且是热备份：%+v", backups[0])
	}
	if backups[1].Hot {
		t.Fatalf("第二份不应是热备份：%+v", backups[1])
	}

	// 目录穿越必须被拒
	for _, bad := range []string{"../server.properties", `..\server.properties`, "sub/x.zip", "nope.txt", ""} {
		if _, err := safeBackupName(bad); err == nil {
			t.Fatalf("非法备份名应被拒绝：%q", bad)
		}
	}
	if err := DeleteServerBackup(id, backups[0].Name); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	if remaining := ListServerBackups(id); len(remaining) != 1 {
		t.Fatalf("删除后应剩 1 份，实际 %d", len(remaining))
	}
	if err := DeleteServerBackup(id, "missing.zip"); err == nil {
		t.Fatal("删除不存在的备份应报错")
	}
}

// TestPruneServerBackups 保留策略：份数上限生效，且最新一份永不被删。
func TestPruneServerBackups(t *testing.T) {
	id := "backup-prune"
	useBackupTestServer(t, id)
	SaveBackupKeepCount(2)
	SaveBackupKeepDays(0)
	t.Cleanup(func() {
		SaveBackupKeepCount(backupDefaultKeepCount)
		SaveBackupKeepDays(0)
	})

	directory := backupsDirectory(id)
	for _, stamp := range []string{"20260101-000000", "20260102-000000", "20260103-000000", "20260104-000000"} {
		writeTestFile(t, filepath.Join(directory, backupColdPrefix+stamp+".zip"), stamp)
	}

	removed, err := PruneServerBackups(id)
	if err != nil {
		t.Fatalf("清理失败：%v", err)
	}
	if removed != 2 {
		t.Fatalf("删除数量 = %d，期望 2", removed)
	}
	remaining := ListServerBackups(id)
	if len(remaining) != 2 {
		t.Fatalf("应保留 2 份，实际 %d", len(remaining))
	}
	if remaining[0].Name != backupColdPrefix+"20260104-000000.zip" {
		t.Fatalf("最新的备份被删掉了：%+v", remaining)
	}
}

// TestPruneKeepsNewestWhenAllExpired 天数策略把全部备份判为过期时，最新一份仍要保留。
func TestPruneKeepsNewestWhenAllExpired(t *testing.T) {
	id := "backup-prune-old"
	useBackupTestServer(t, id)
	SaveBackupKeepCount(0)
	SaveBackupKeepDays(1)
	t.Cleanup(func() {
		SaveBackupKeepCount(backupDefaultKeepCount)
		SaveBackupKeepDays(0)
	})

	directory := backupsDirectory(id)
	old := filepath.Join(directory, backupColdPrefix+"20200101-000000.zip")
	writeTestFile(t, old, "ancient")
	past := time.Now().AddDate(0, 0, -30)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatalf("改时间失败：%v", err)
	}

	if _, err := PruneServerBackups(id); err != nil {
		t.Fatalf("清理失败：%v", err)
	}
	if remaining := ListServerBackups(id); len(remaining) != 1 {
		t.Fatalf("最新一份必须保留，实际剩 %d 份", len(remaining))
	}
}

// TestRestoreServerBackup 恢复：文件被还原、原状态自动留了一份安全备份。
func TestRestoreServerBackup(t *testing.T) {
	id := "backup-restore"
	dir := useBackupTestServer(t, id)

	created, err := CreateServerBackup(context.Background(), id)
	if err != nil {
		t.Fatalf("备份失败：%v", err)
	}
	// 先确认这份备份里确实有配置文件与存档（否则后面"没还原"会是备份本身的问题）
	if names := archiveEntries(t, created.Path); !names["server.properties"] || !names["world/level.dat"] {
		t.Fatalf("备份内容不完整：%v", keysOf(names))
	}

	// 破坏现场：改配置、删世界文件
	writeTestFile(t, filepath.Join(dir, "server.properties"), "server-port=1\n")
	if err := os.Remove(filepath.Join(dir, "world", "level.dat")); err != nil {
		t.Fatalf("删除文件失败：%v", err)
	}

	if err := RestoreServerBackup(context.Background(), id, created.Name); err != nil {
		t.Fatalf("恢复失败：%v", err)
	}
	t.Logf("恢复后目录内容：%v", directoryEntries(t, dir))

	data, readErr := os.ReadFile(filepath.Join(dir, "server.properties"))
	if readErr != nil {
		t.Fatalf("读回配置失败：%v", readErr)
	}
	if !strings.Contains(string(data), "server-port=25565") {
		t.Fatalf("配置未被还原：%q", data)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "world", "level.dat")); statErr != nil {
		t.Fatalf("世界文件未被还原：%v", statErr)
	}

	// 恢复前自动留的安全备份（加上用户自己那份，共 2 份）
	if backups := ListServerBackups(id); len(backups) < 2 {
		t.Fatalf("恢复前应自动留一份安全备份，实际 %d 份", len(backups))
	}
}

// TestSafeArchiveTarget 备份内路径越界要被挡住。
func TestSafeArchiveTarget(t *testing.T) {
	root := t.TempDir()

	if _, err := safeArchiveTarget(root, "../evil.txt"); err == nil {
		t.Fatal("../ 越界应被拒绝")
	}
	if _, err := safeArchiveTarget(root, "world/level.dat"); err != nil {
		t.Fatalf("正常路径应通过：%v", err)
	}

	target, err := safeArchiveTarget(root, "world/region/r.0.0.mca")
	if err != nil {
		t.Fatalf("正常路径应通过：%v", err)
	}
	if !strings.HasPrefix(target, filepath.Clean(root)+string(filepath.Separator)) {
		t.Fatalf("解析结果不在服务器目录内：%s", target)
	}
}

// TestBackupSettingsRoundTrip 设置读写与夹紧。
func TestBackupSettingsRoundTrip(t *testing.T) {
	SaveBackupAutoEnabled(true)
	if !BackupAutoEnabled() {
		t.Fatal("自动备份开关未生效")
	}
	SaveBackupAutoEnabled(false)
	if BackupAutoEnabled() {
		t.Fatal("自动备份开关未关闭")
	}

	SaveBackupIntervalHours(0) // 低于下限 → 夹到下限
	if got := BackupIntervalHours(); got != backupMinimumIntervalHours {
		t.Fatalf("间隔应夹到下限 %d，得到 %d", backupMinimumIntervalHours, got)
	}
	SaveBackupIntervalHours(12)
	if got := BackupIntervalHours(); got != 12 {
		t.Fatalf("间隔 = %d，期望 12", got)
	}

	SaveBackupKeepCount(-3)
	if got := BackupKeepCount(); got != 0 {
		t.Fatalf("负数份数应归 0，得到 %d", got)
	}
	SaveBackupKeepCount(3)
	if got := BackupKeepCount(); got != 3 {
		t.Fatalf("份数 = %d，期望 3", got)
	}
	SaveBackupKeepCount(backupDefaultKeepCount)
}

// TestRunScheduledBackupsDisabled 关掉自动备份时巡检不做任何事。
func TestRunScheduledBackupsDisabled(t *testing.T) {
	id := "backup-scheduler"
	useBackupTestServer(t, id)
	SaveBackupAutoEnabled(false)

	runScheduledBackups(context.Background())

	if backups := ListServerBackups(id); len(backups) != 0 {
		t.Fatalf("关闭自动备份时不该产生备份，实际 %d 份", len(backups))
	}
}

// archiveEntries 读出 zip 内的条目名集合。
func archiveEntries(t *testing.T, path string) map[string]bool {
	t.Helper()

	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("打开备份失败：%v", err)
	}
	defer reader.Close()

	names := map[string]bool{}
	for _, entry := range reader.File {
		names[entry.Name] = true
	}

	return names
}

func keysOf(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}

	return keys
}

// directoryEntries 列出目录下的相对路径（诊断用）。
func directoryEntries(t *testing.T, root string) []string {
	t.Helper()

	var result []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr == nil {
			result = append(result, filepath.ToSlash(relative))
		}

		return nil
	})

	return result
}
