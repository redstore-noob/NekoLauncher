// 版本重命名：目录改名 + inheritsFrom/jar 引用修补 + 实例配置迁移。
// 移植自 NyaLauncher.Core/Launch/GameVersionRenameService.cs。
package instance

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"nekolauncher/internal/config"
	"nekolauncher/internal/tools"
)

// tempSuffix 临时文件后缀：原子写入与仅大小写改名共用。
const renameTempSuffix = ".nya-rename"

// renameReservedDeviceNames Windows 保留设备名：作为目录名会让目录改名报出难懂的 IO 错误。
var renameReservedDeviceNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// jsonPatch 单个版本 JSON 的待写入变更。
type jsonPatch struct {
	FilePath string
	Document map[string]any
}

// backupEntry 已写入文件的原内容快照；originalText 为 nil 表示该文件原本不存在。
type backupEntry struct {
	FilePath     string
	OriginalText *string
}

// RenameVersion 重命名版本文件夹，并同步修补所有引用该版本 ID 的
// inheritsFrom 与 jar 字段（对应 C# RenameAsync；返回实际生效的新版本 ID）。
func RenameVersion(ctx context.Context, minecraftDirectory, oldVersionID, requestedVersionID string) (string, error) {
	newVersionID, err := validateVersionID(requestedVersionID)
	if err != nil {
		return "", err
	}
	if oldVersionID == newVersionID {
		return oldVersionID, nil
	}

	versionsDirectory := filepath.Join(mustAbs(minecraftDirectory), "versions")
	sourceDirectory, err := resolveContainedDirectory(versionsDirectory, oldVersionID)
	if err != nil {
		return "", err
	}
	targetDirectory, err := resolveContainedDirectory(versionsDirectory, newVersionID)
	if err != nil {
		return "", err
	}
	if err := validateRenameTargets(sourceDirectory, targetDirectory, newVersionID); err != nil {
		return "", err
	}

	sourceJSONPath := filepath.Join(sourceDirectory, oldVersionID+".json")
	if !tools.FileExists(sourceJSONPath) {
		return "", fmt.Errorf("原版本 JSON 不存在：%s", sourceJSONPath)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	// 先算好所有要改的 JSON 再动目录：避免目录已改名后发现无法修补的半成品状态
	mutations, err := readMutations(versionsDirectory, sourceJSONPath, targetDirectory, oldVersionID, newVersionID, ctx)
	if err != nil {
		return "", err
	}

	if err := moveVersionDirectory(sourceDirectory, targetDirectory, versionsDirectory); err != nil {
		return "", err
	}
	if err := applyJSONMutations(ctx, minecraftDirectory, mutations, sourceDirectory, targetDirectory, oldVersionID, newVersionID); err != nil {
		// JSON 修补失败：尽力把目录改名也回滚，然后抛出原始异常
		tryRollbackDirectory(targetDirectory, sourceDirectory, oldVersionID, newVersionID)
		return "", err
	}
	return newVersionID, nil
}

// validateRenameTargets 改名目标校验：原目录存在、新名字未被占用（同名目录除外，支持仅大小写改名）。
func validateRenameTargets(sourceDirectory, targetDirectory, newVersionID string) error {
	if info, err := os.Stat(sourceDirectory); err != nil || !info.IsDir() {
		return fmt.Errorf("原版本文件夹不存在：%s", sourceDirectory)
	}

	targetOccupied := pathExists(targetDirectory)
	// 只改大小写时目标目录当然"存在"（就是自己）：这里必须用折叠大小写的比较，
	// tools.PathsEqual 在非 Windows 上是区分大小写的，会把这种情况下判成"名字已存在"，
	// 于是 Linux/macOS 上永远改不了只差大小写的版本名。
	if targetOccupied && !renameSameTarget(sourceDirectory, targetDirectory) {
		return fmt.Errorf("版本名称“%s”已存在。", newVersionID)
	}
	return nil
}

// renameSameTarget 两个路径是否指向同一个目标（忽略大小写与末尾分隔符）。
func renameSameTarget(source, target string) bool {
	normalized := func(path string) string {
		trimmed := strings.TrimRight(filepath.Clean(path), `\/`)

		return strings.ToLower(trimmed)
	}

	return normalized(source) == normalized(target)
}

// applyJSONMutations 改名成功后修补 json/jar 文件名与全部引用，最后迁移实例配置。
// 任何一步失败都会先还原已写入的 JSON 内容，再由外层回滚目录改名。
func applyJSONMutations(
	ctx context.Context,
	minecraftDirectory string,
	patches []jsonPatch,
	sourceDirectory string,
	targetDirectory string,
	oldVersionID string,
	newVersionID string,
) error {
	renamedJSONPath := filepath.Join(targetDirectory, newVersionID+".json")
	sourceJSONPath := filepath.Join(sourceDirectory, oldVersionID+".json")

	// 逐个写入兄弟版本 JSON 前先快照原内容：中途失败（取消/文件被锁）时
	// 逐一还原，否则目录虽然回滚，已写进兄弟版本的 inheritsFrom/jar 会
	// 永久指向不存在的版本名。
	var backups []backupEntry
	for _, patch := range patches {
		if err := ctx.Err(); err != nil {
			restoreBackups(backups)
			return err
		}
		destination := patch.FilePath
		if samePathOnDisk(patch.FilePath, sourceJSONPath) {
			// 目录已先一步改名：源 JSON 此刻位于 targetDirectory 下且仍是旧文件名。
			// 修补内容必须写到旧文件名上，否则末尾 os.Rename(旧名→新名) 会用
			// 未修补的原始 JSON（id 仍为旧值）覆盖掉刚写入的补丁。
			destination = filepath.Join(targetDirectory, oldVersionID+".json")
		}
		if text, err := os.ReadFile(destination); err == nil {
			content := string(text)
			backups = append(backups, backupEntry{destination, &content})
		} else {
			backups = append(backups, backupEntry{destination, nil})
		}
		if err := writeJSONAtomically(destination, patch.Document); err != nil {
			restoreBackups(backups)
			return err
		}
	}

	config.MigrateRenamedVersion(minecraftDirectory, oldVersionID, newVersionID, sourceDirectory, targetDirectory)

	// 最后重命名版本自身的 json/jar：失败同样走上面的备份还原与目录回滚
	if err := os.Rename(filepath.Join(targetDirectory, oldVersionID+".json"), renamedJSONPath); err != nil {
		restoreBackups(backups)
		return err
	}
	sourceJarPath := filepath.Join(targetDirectory, oldVersionID+".jar")
	if tools.FileExists(sourceJarPath) {
		if err := os.Rename(sourceJarPath, filepath.Join(targetDirectory, newVersionID+".jar")); err != nil {
			restoreBackups(backups)
			return err
		}
	}
	return nil
}

// samePathOnDisk Windows 上按大小写不敏感比较路径，其余平台区分大小写。
func samePathOnDisk(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

// restoreBackups 还原已写入的文件内容；还原失败不影响目录级回滚。
func restoreBackups(backups []backupEntry) {
	for _, backup := range backups {
		if backup.OriginalText == nil {
			_ = os.Remove(backup.FilePath)
			continue
		}
		_ = os.WriteFile(backup.FilePath, []byte(*backup.OriginalText), 0o644)
	}
}

// readMutations 扫描 versions 下所有版本 JSON，找出需要修补的变更：
// 被改名版本的 id 字段、指向旧名的 inheritsFrom 与 jar 字段。
func readMutations(
	versionsDirectory string,
	renamedJSONPath string,
	targetDirectory string,
	oldVersionID string,
	newVersionID string,
	ctx context.Context,
) ([]jsonPatch, error) {
	var patches []jsonPatch
	entries, err := os.ReadDir(versionsDirectory)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		directoryName := entry.Name()
		jsonPath := filepath.Join(versionsDirectory, directoryName, directoryName+".json")
		if !tools.FileExists(jsonPath) {
			continue
		}

		root, err := tryParseJSONObject(jsonPath)
		if err != nil || root == nil {
			// 解析版本 JSON；损坏或非对象的内容跳过，不阻断整个重命名。
			continue
		}

		if patch := collectJSONChanges(root, jsonPath, renamedJSONPath, oldVersionID, newVersionID); patch != nil {
			patches = append(patches, *patch)
		}
	}

	// 目标版本 JSON 必须至少产生一条变更（id 字段），否则说明读取失败。
	// 同样用折叠大小写的比较：只改大小写时 patch.FilePath 与 renamedJSONPath
	// 只在大小写上不同，PathsEqual 在 Linux/macOS 上会判成"没匹配上"，
	// 于是改名被误报成"无法读取所选版本 JSON"。
	renamedPatched := false
	for _, patch := range patches {
		if renameSameTarget(patch.FilePath, renamedJSONPath) {
			renamedPatched = true
			break
		}
	}
	if !renamedPatched {
		return nil, errors.New("无法读取所选版本 JSON。")
	}
	return patches, nil
}

// tryParseJSONObject 解析版本 JSON；损坏或非对象的内容返回 nil。
func tryParseJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	return root, nil
}

// collectJSONChanges 计算单个版本 JSON 的字段变更，无变更时返回 nil。
// 引用比对忽略大小写：Linux 文件系统大小写敏感，漏改大小写不同的引用
// 会让子版本在重命名后找不到父版本 JSON。
func collectJSONChanges(root map[string]any, jsonPath, renamedJSONPath, oldVersionID, newVersionID string) *jsonPatch {
	modified := false

	// 判定"该 JSON 就是重命名后的那份"必须忽略大小写：
	// tools.PathsEqual 在 Linux（大小写敏感文件系统）上对仅大小写不同的
	// 路径返回 false，会导致仅大小写改名时 id 补丁不被收集。
	// 两个路径都由 versions 目录 + 版本 ID 拼出，EqualFold 即可。
	if strings.EqualFold(filepath.Clean(jsonPath), filepath.Clean(renamedJSONPath)) {
		root["id"] = newVersionID
		modified = true
	}

	if inheritsFrom, ok := root["inheritsFrom"].(string); ok &&
		strings.EqualFold(inheritsFrom, oldVersionID) {
		root["inheritsFrom"] = newVersionID
		modified = true
	}

	if jarField, ok := root["jar"].(string); ok &&
		strings.EqualFold(jarField, oldVersionID) {
		root["jar"] = newVersionID
		modified = true
	}

	if !modified {
		return nil
	}
	return &jsonPatch{FilePath: jsonPath, Document: root}
}

// validateVersionID 校验新版本 ID：非空、不含路径分隔符与文件系统非法字符、
// 不是 Windows 保留设备名、不以点或空格结尾。
func validateVersionID(requested string) (string, error) {
	trimmed := strings.TrimSpace(requested)
	illegal := trimmed == "" || trimmed == "." || trimmed == ".." ||
		strings.HasSuffix(trimmed, ".") || strings.HasSuffix(trimmed, " ") ||
		strings.ContainsAny(trimmed, invalidFileNameChars()) ||
		strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") ||
		renameReservedDeviceNames[strings.ToUpper(trimmed)]
	if illegal {
		return "", errors.New("版本名称为空或包含文件系统不允许的字符。")
	}
	return trimmed, nil
}

// invalidFileNameChars 返回文件系统禁止字符（对应 C# Path.GetInvalidFileNameChars 的
// Windows 常用子集：路径分隔符与控制字符；Windows 下额外包含盘符冒号等）。
func invalidFileNameChars() string {
	chars := `<>:"|?*`
	for i := 0; i < 32; i++ {
		chars += string(rune(i))
	}
	if runtime.GOOS != "windows" {
		// 非 Windows 只保留控制字符（分隔符单独判断）
		chars = ""
		for i := 0; i < 32; i++ {
			chars += string(rune(i))
		}
	}
	return chars
}

// resolveContainedDirectory 解析版本目录并确保结果仍位于 versions 文件夹内（防路径逃逸）。
func resolveContainedDirectory(versionsDirectory, versionID string) (string, error) {
	root := trimEndingSeparator(mustAbs(versionsDirectory))
	candidate := mustAbs(filepath.Join(root, versionID))
	separator := string(os.PathSeparator)
	if candidate != root && !strings.HasPrefix(candidate, root+separator) {
		return "", errors.New("版本路径超出 versions 文件夹。")
	}
	return candidate, nil
}

// writeJSONAtomically 先写临时文件再覆盖移动，避免中途失败留下半写状态的 JSON。
// 输出保留两空格缩进，便于用户后续手工查看/编辑。
func writeJSONAtomically(path string, document map[string]any) error {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporaryPath := path + renameTempSuffix
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := os.WriteFile(temporaryPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

// moveVersionDirectory 改名版本目录。仅大小写不同的改名在不区分大小写的文件系统上
// 需要经过一个临时中间名。
func moveVersionDirectory(sourceDirectory, targetDirectory, versionsDirectory string) error {
	caseOnlyRename := tools.PathsEqual(sourceDirectory, targetDirectory)
	if !caseOnlyRename {
		return os.Rename(sourceDirectory, targetDirectory)
	}

	intermediate := filepath.Join(versionsDirectory, fmt.Sprintf(".nya-rename-%s", newGUID()))
	if err := os.Rename(sourceDirectory, intermediate); err != nil {
		return err
	}
	if err := os.Rename(intermediate, targetDirectory); err != nil {
		// 第二步失败：把目录挪回原名，尽量恢复原状
		if info, err := os.Stat(intermediate); err == nil && info.IsDir() {
			if _, err := os.Stat(sourceDirectory); err != nil {
				_ = os.Rename(intermediate, sourceDirectory)
			}
		}
		return err
	}
	return nil
}

// tryRollbackDirectory 尽力回滚目录改名与 json/jar 文件名。回滚失败时保留原异常：
// 磁盘上的文件仍可人工恢复。
func tryRollbackDirectory(targetDirectory, sourceDirectory, oldVersionID, newVersionID string) {
	moveBackIfRenamed(targetDirectory, newVersionID, oldVersionID, ".json")
	moveBackIfRenamed(targetDirectory, newVersionID, oldVersionID, ".jar")
	if info, err := os.Stat(targetDirectory); err == nil && info.IsDir() {
		if _, err := os.Stat(sourceDirectory); err != nil {
			_ = os.Rename(targetDirectory, sourceDirectory)
		}
	}
}

// moveBackIfRenamed 回滚单个文件名（新名 → 旧名），仅在新名存在且旧名缺失时执行。
func moveBackIfRenamed(directory, newVersionID, oldVersionID, extension string) {
	renamedPath := filepath.Join(directory, newVersionID+extension)
	originalPath := filepath.Join(directory, oldVersionID+extension)
	if tools.FileExists(renamedPath) && !pathExists(originalPath) {
		_ = os.Rename(renamedPath, originalPath)
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// newGUID 生成不带连字符的小写 GUID（对应 C# Guid.NewGuid().ToString("N")）。
func newGUID() string {
	b := make([]byte, 16)
	_, _ = crand.Read(b)
	return fmt.Sprintf("%x", b)
}
