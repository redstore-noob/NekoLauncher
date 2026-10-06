package mcserver

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/logs"
)

// 服务器备份：热备份（运行中）+ 冷备份（已停止）+ 保留策略 + 定时任务。
//
// 与「导出 NekoSer」（ExportServer）的区别：那个是给用户拿走/迁移用的整包，
// 运行中一律拒绝；这里是**给同一台服务器做时间点副本**，运行中也要能备份——
// 否则玩家一整天在线，等于一天没有备份。热备份靠服务端的 save-off / save-all flush
// 把世界刷盘并暂停写入，压缩期间不产生半写状态。
const (
	backupDirectoryName = "backups"
	// backupMaxTotalBytes 单个备份包上限（8GB）：超过就中止，避免把磁盘写满。
	backupMaxTotalBytes = 8 << 30
	// backupFlushDelay save-all flush 之后等世界落盘的宽限时间。
	backupFlushDelay = 1500 * time.Millisecond
	// backupDefaultKeepCount 默认保留份数。
	backupDefaultKeepCount = 5
	// backupMinimumIntervalHours 自动备份的最小间隔。
	backupMinimumIntervalHours = 1
	// backupDefaultIntervalHours 默认自动备份间隔。
	backupDefaultIntervalHours = 6
)

// BackupInfo 一份备份的元信息。
type BackupInfo struct {
	Name      string `json:"Name"`
	Path      string `json:"Path"`
	SizeBytes int64  `json:"SizeBytes"`
	CreatedAt int64  `json:"CreatedAt"`
	// Hot 是否在服务器运行中做的热备份。
	Hot bool `json:"Hot"`
}

// backupsDirectory 某服务器的备份目录。
func backupsDirectory(id string) string {
	return filepath.Join(serverDirectory(id), backupDirectoryName)
}

// backupPrefix 备份文件名前缀（解析 Hot 标记用）。
const (
	backupHotPrefix  = "hot-"
	backupColdPrefix = "cold-"
)

// CreateServerBackup 为服务器创建一份备份，返回备份信息。
//
// 运行中：先 save-off + save-all flush（RCON 优先，退回 stdin），压缩结束后 save-on；
// 已停止：直接压缩。压缩内容为服务器目录下除 backups/ 与 logs/ 之外的全部文件。
func CreateServerBackup(ctx context.Context, id string) (BackupInfo, error) {
	if err := validateID(id); err != nil {
		return BackupInfo{}, err
	}
	dir := serverDirectory(id)
	if _, err := os.Stat(dir); err != nil {
		return BackupInfo{}, fmt.Errorf("服务器目录不存在：%w", err)
	}

	running := Default().IsRunning(id)
	if running {
		// save-off 成功后立刻登记恢复：后面 flush / 压缩任何一步失败都不能把
		// 世界留在 save-off——此前 defer 在 flush 成功之后才注册，大世界
		// save-all flush 超过 RCON 超时就会让自动保存被永久关闭（静默丢档）。
		if err := setWorldSaving(id, false); err != nil {
			// 拿不到 save-off 也不硬失败：至少 save-all flush 一次再压，
			// 只是退化成"可能包含半写区块"的备份，日志里说明清楚。
			logs.Write("WARN", fmt.Sprintf(
				"服务器 %s 无法暂停世界写入（%v），本次热备份可能包含未完全落盘的区块。", id, err))
		} else {
			defer func() {
				if err := setWorldSaving(id, true); err != nil {
					logs.Write("WARN", fmt.Sprintf("服务器 %s 恢复世界写入失败：%v", id, err))
				}
			}()
		}
		if err := flushWorld(id); err != nil {
			return BackupInfo{}, fmt.Errorf("刷新世界失败：%w", err)
		}
		time.Sleep(backupFlushDelay)
	}

	directory := backupsDirectory(id)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return BackupInfo{}, err
	}

	prefix := backupColdPrefix
	if running {
		prefix = backupHotPrefix
	}
	// 文件名只到秒：同一秒内连做两次（例如"恢复前的安全备份"紧跟在用户手动备份之后）
	// 会撞名，直接 rename 会把前一份覆盖掉——那正是恢复时的回退点。撞名就加序号。
	name := filepath.Base(uniqueBackupPath(directory,
		fmt.Sprintf("%s%s.zip", prefix, time.Now().Format("20060102-150405"))))
	target := filepath.Join(directory, name)
	temporary := target + ".tmp"

	written, err := writeServerArchive(ctx, dir, temporary)
	if err != nil {
		_ = os.Remove(temporary)

		return BackupInfo{}, err
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)

		return BackupInfo{}, err
	}

	info, statErr := os.Stat(target)
	size := written
	if statErr == nil {
		size = info.Size()
	}

	// 备份完成后按保留策略清理（失败只记日志：备份本身已经成功）
	if _, pruneErr := PruneServerBackups(id); pruneErr != nil {
		logs.Write("WARN", fmt.Sprintf("清理旧备份失败：%v", pruneErr))
	}

	return BackupInfo{
		Name:      name,
		Path:      target,
		SizeBytes: size,
		CreatedAt: time.Now().Unix(),
		Hot:       running,
	}, nil
}

// writeServerArchive 把服务器目录（除 backups/ 与 logs/）打成 zip，返回写入字节数。
func writeServerArchive(ctx context.Context, dir, target string) (int64, error) {
	file, err := os.Create(target)
	if err != nil {
		return 0, err
	}
	writer := zip.NewWriter(file)

	var written int64
	walkErr := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		if relative == "." {
			return nil
		}
		// 备份目录自身不能进包（会递归），日志目录体积大且无保留价值
		top := strings.Split(filepath.ToSlash(relative), "/")[0]
		if top == backupDirectoryName || top == "logs" {
			if entry.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}
		if entry.IsDir() {
			return nil
		}
		// 压缩到一半的临时文件不备份
		if strings.HasSuffix(entry.Name(), ".tmp") || strings.HasSuffix(entry.Name(), ".nya-download") {
			return nil
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		header, headerErr := zip.FileInfoHeader(info)
		if headerErr != nil {
			return headerErr
		}
		header.Name = filepath.ToSlash(relative)
		header.Method = zip.Deflate

		entryWriter, createErr := writer.CreateHeader(header)
		if createErr != nil {
			return createErr
		}

		source, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		copied, copyErr := io.Copy(entryWriter, source)
		_ = source.Close()
		if copyErr != nil {
			return copyErr
		}
		written += copied
		if written > backupMaxTotalBytes {
			return fmt.Errorf("备份体积超过上限（%d GB），已中止", backupMaxTotalBytes>>30)
		}

		return nil
	})
	if walkErr != nil {
		_ = writer.Close()
		_ = file.Close()

		return written, walkErr
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()

		return written, err
	}
	if err := file.Close(); err != nil {
		return written, err
	}

	return written, nil
}

// ListServerBackups 列出全部备份（新→旧）。
func ListServerBackups(id string) []BackupInfo {
	if err := validateID(id); err != nil {
		return []BackupInfo{}
	}
	directory := backupsDirectory(id)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return []BackupInfo{}
	}

	result := make([]BackupInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".zip") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		result = append(result, BackupInfo{
			Name:      entry.Name(),
			Path:      filepath.Join(directory, entry.Name()),
			SizeBytes: info.Size(),
			CreatedAt: info.ModTime().Unix(),
			Hot:       strings.HasPrefix(entry.Name(), backupHotPrefix),
		})
	}
	sort.Slice(result, func(left, right int) bool {
		// 文件名带时间戳，字典序即时间序；同秒时用创建时间兜底
		if result[left].Name == result[right].Name {
			return result[left].CreatedAt > result[right].CreatedAt
		}

		return result[left].Name > result[right].Name
	})

	return result
}

// DeleteServerBackup 删除一份备份。
func DeleteServerBackup(id, name string) error {
	if err := validateID(id); err != nil {
		return err
	}
	safe, err := safeBackupName(name)
	if err != nil {
		return err
	}
	target := filepath.Join(backupsDirectory(id), safe)
	if _, statErr := os.Stat(target); statErr != nil {
		return fmt.Errorf("备份不存在：%s", safe)
	}

	return os.Remove(target)
}

// uniqueBackupPath 返回一个不会覆盖既有备份的落点：
// 撞名时依次尝试 <名字>-1.zip、-2.zip……
func uniqueBackupPath(directory, name string) string {
	base := strings.TrimSuffix(name, ".zip")
	candidate := name
	for index := 1; ; index++ {
		path := filepath.Join(directory, candidate)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		if index > 1000 {
			// 极端情况下（表被塞满/权限异常）不再空转，交给 rename 报错
			return path
		}
		candidate = fmt.Sprintf("%s-%d.zip", base, index)
	}
}

// safeBackupName 校验备份文件名（防目录穿越；只接受 backups/ 下的单层文件）。
func safeBackupName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", errors.New("备份名为空")
	}
	if trimmed != filepath.Base(trimmed) || strings.ContainsAny(trimmed, `/\`) {
		return "", errors.New("备份名不合法")
	}
	if !strings.HasSuffix(strings.ToLower(trimmed), ".zip") {
		return "", errors.New("备份名不合法")
	}

	return trimmed, nil
}

// PruneServerBackups 按保留策略清理旧备份，返回删除数量。
// 保留策略：份数上限（backupKeepCount，0 = 不限）与天数上限（backupKeepDays，0 = 不限）同时生效。
func PruneServerBackups(id string) (int, error) {
	backups := ListServerBackups(id)
	if len(backups) == 0 {
		return 0, nil
	}

	keepCount := BackupKeepCount()
	keepDays := BackupKeepDays()
	deadline := time.Now().AddDate(0, 0, -keepDays)

	removed := 0
	for index, backup := range backups {
		overCount := keepCount > 0 && index >= keepCount
		overAge := keepDays > 0 && time.Unix(backup.CreatedAt, 0).Before(deadline)
		if !overCount && !overAge {
			continue
		}
		// 最新的那一份永远保留：全是旧备份时也不能清空
		if index == 0 {
			continue
		}
		if err := os.Remove(backup.Path); err != nil {
			return removed, err
		}
		removed++
	}

	return removed, nil
}

// RestoreServerBackup 用指定备份覆盖服务器目录（要求服务器已停止）。
//
// 覆盖前会先自动做一份冷备份：恢复本身也可能选错文件，没有回退点就太危险。
func RestoreServerBackup(ctx context.Context, id, name string) error {
	if err := validateID(id); err != nil {
		return err
	}
	if Default().IsRunning(id) {
		return errors.New("请先停止服务器再恢复备份")
	}
	safe, err := safeBackupName(name)
	if err != nil {
		return err
	}
	dir := serverDirectory(id)
	archive := filepath.Join(backupsDirectory(id), safe)
	if _, statErr := os.Stat(archive); statErr != nil {
		return fmt.Errorf("备份不存在：%s", safe)
	}

	// 恢复前自动留一份"当前状态"，避免选错备份之后无法回头
	if _, backupErr := CreateServerBackup(ctx, id); backupErr != nil {
		return fmt.Errorf("恢复前的安全备份失败（已中止）：%w", backupErr)
	}

	reader, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("打开备份失败：%w", err)
	}
	defer reader.Close()

	for _, entry := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		target, joinErr := safeArchiveTarget(dir, entry.Name)
		if joinErr != nil {
			return joinErr
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}

			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		source, openErr := entry.Open()
		if openErr != nil {
			return openErr
		}
		destination, createErr := os.Create(target)
		if createErr != nil {
			_ = source.Close()

			return createErr
		}
		_, copyErr := io.Copy(destination, source)
		_ = source.Close()
		_ = destination.Close()
		if copyErr != nil {
			return copyErr
		}
	}

	return nil
}

// safeArchiveTarget 把 zip 内的相对路径解析到服务器目录内（挡掉 ../ 与绝对路径）。
func safeArchiveTarget(root, name string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("备份内包含越界路径：%s", name)
	}
	target := filepath.Join(root, cleaned)
	if !strings.HasPrefix(target, filepath.Clean(root)+string(filepath.Separator)) {
		return "", fmt.Errorf("备份内包含越界路径：%s", name)
	}

	return target, nil
}

// ---- 服务端指令（热备份用；RCON 优先，退回 stdin） ----

// setWorldSaving 开关世界写入（save-off / save-on）。
func setWorldSaving(id string, enabled bool) error {
	command := "save-off"
	if enabled {
		command = "save-on"
	}

	return sendServerCommandAny(id, command)
}

// flushWorld 让服务端把世界刷盘（save-all flush）。
func flushWorld(id string) error {
	return sendServerCommandAny(id, "save-all flush")
}

// sendServerCommandAny 优先 RCON，未启用或失败时退回 stdin。
func sendServerCommandAny(id, command string) error {
	if _, err := Default().RunRCONCommand(id, command); err == nil {
		return nil
	}

	return Default().SendServerCommand(id, command)
}

// ---- 保留策略与定时任务设置 ----

// BackupSettings 备份策略（前端一个面板展示，一次读写）。
type BackupSettings struct {
	// Enabled 是否开启自动备份
	Enabled bool `json:"Enabled"`
	// IntervalHours 自动备份间隔（小时，最小 1）
	IntervalHours int `json:"IntervalHours"`
	// KeepCount 保留份数（0 = 不限）
	KeepCount int `json:"KeepCount"`
	// KeepDays 保留天数（0 = 不限）
	KeepDays int `json:"KeepDays"`
}

// CurrentBackupSettings 读取当前备份策略。
func CurrentBackupSettings() BackupSettings {
	return BackupSettings{
		Enabled:       BackupAutoEnabled(),
		IntervalHours: BackupIntervalHours(),
		KeepCount:     BackupKeepCount(),
		KeepDays:      BackupKeepDays(),
	}
}

// SaveBackupSettings 保存备份策略。
func SaveBackupSettings(settings BackupSettings) {
	SaveBackupAutoEnabled(settings.Enabled)
	SaveBackupIntervalHours(settings.IntervalHours)
	SaveBackupKeepCount(settings.KeepCount)
	SaveBackupKeepDays(settings.KeepDays)
}

// BackupKeepCount 保留份数（0 = 不限）。
func BackupKeepCount() int {
	value := strings.TrimSpace(config.GetValue("mcserverBackupKeepCount"))
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		return backupDefaultKeepCount
	}

	return count
}

// SaveBackupKeepCount 保存保留份数。
func SaveBackupKeepCount(count int) {
	if count < 0 {
		count = 0
	}
	config.SetValue("mcserverBackupKeepCount", strconv.Itoa(count))
}

// BackupKeepDays 保留天数（0 = 不限）。
func BackupKeepDays() int {
	value := strings.TrimSpace(config.GetValue("mcserverBackupKeepDays"))
	days, err := strconv.Atoi(value)
	if err != nil || days < 0 {
		return 0
	}

	return days
}

// SaveBackupKeepDays 保存保留天数。
func SaveBackupKeepDays(days int) {
	if days < 0 {
		days = 0
	}
	config.SetValue("mcserverBackupKeepDays", strconv.Itoa(days))
}

// BackupAutoEnabled 是否开启自动备份。
func BackupAutoEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(config.GetValue("mcserverBackupEnabled")), "true")
}

// SaveBackupAutoEnabled 保存自动备份开关。
func SaveBackupAutoEnabled(enabled bool) {
	config.SetValue("mcserverBackupEnabled", strconv.FormatBool(enabled))
}

// BackupIntervalHours 自动备份间隔（小时）。
func BackupIntervalHours() int {
	value := strings.TrimSpace(config.GetValue("mcserverBackupIntervalHours"))
	hours, err := strconv.Atoi(value)
	if err != nil || hours < backupMinimumIntervalHours {
		return backupDefaultIntervalHours
	}

	return hours
}

// SaveBackupIntervalHours 保存自动备份间隔。
func SaveBackupIntervalHours(hours int) {
	if hours < backupMinimumIntervalHours {
		hours = backupMinimumIntervalHours
	}
	config.SetValue("mcserverBackupIntervalHours", strconv.Itoa(hours))
}

// StartBackupScheduler 启动自动备份巡检（应用启动时调用一次）。
//
// 每 10 分钟看一遍：开启自动备份时，对"已到间隔"的服务器做热/冷备份。
// 判断依据是最后一份备份的创建时间——启动器重启不会重复备份，也不会漏。
func StartBackupScheduler(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runScheduledBackups(ctx)
			}
		}
	}()
}

// runScheduledBackups 巡检一轮（导出便于测试）。
func runScheduledBackups(ctx context.Context) {
	if !BackupAutoEnabled() {
		return
	}
	interval := time.Duration(BackupIntervalHours()) * time.Hour

	for _, server := range ListServerInfos() {
		if err := ctx.Err(); err != nil {
			return
		}
		backups := ListServerBackups(server.ID)
		if len(backups) > 0 && time.Since(time.Unix(backups[0].CreatedAt, 0)) < interval {
			continue
		}
		if _, err := CreateServerBackup(ctx, server.ID); err != nil {
			logs.Write("WARN", fmt.Sprintf("自动备份 %s 失败：%v", server.Name, err))
			Default().appendConsole(server.ID, fmt.Sprintf("[%s] 自动备份失败：%v", time.Now().Format("15:04:05"), err))
		}
	}
}
