// 存档的导出与删除操作。移植自 NyaLauncher.Core/Content/GameSaveService.cs。
// 存档在磁盘上是一个目录，导出会将该目录打包为 .zip（丢弃会话锁文件）。
// 同目录的历史备份（{存档名}-backup_*.zip）仍是本导出格式，可直接导入。
package content

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"nekolauncher/internal/tools"
)

// sessionLockName 导出时丢弃的会话锁文件名。
const sessionLockName = "session.lock"

// ExportSave 将指定存档目录打包到目标 .zip 路径。目标已存在时覆盖。
// 对应 C# ExportAsync（C# 失败返回 null；Go 版改为返回 error，见 PORTING_NOTES.md）。
func ExportSave(ctx context.Context, saveDirectory, destinationZipPath string) (string, error) {
	if strings.TrimSpace(saveDirectory) == "" ||
		strings.TrimSpace(destinationZipPath) == "" {
		return "", fmt.Errorf("参数无效")
	}
	info, err := os.Stat(saveDirectory)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("存档目录不存在：%s", saveDirectory)
	}

	temporary := destinationZipPath + ".nya-pack"
	defer func() { tools.RemoveFileIfExists(temporary) }()

	normalized := trimEndingSep(mustAbsPath(saveDirectory))
	saveName := filepath.Base(normalized)
	parent := filepath.Dir(destinationZipPath)
	if strings.TrimSpace(parent) == "" {
		return "", fmt.Errorf("目标路径无效：%s", destinationZipPath)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}

	if err := createSaveArchive(ctx, normalized, saveName, temporary); err != nil {
		return "", err
	}
	if err := os.Rename(temporary, destinationZipPath); err != nil {
		// Windows 上 os.Rename 不覆盖已存在文件；失败时改为流式覆盖写入
		//（大存档可达数百 MB，不能整文件读入内存）
		if err := overwriteByCopy(temporary, destinationZipPath); err != nil {
			return "", err
		}
	}
	return destinationZipPath, nil
}

// overwriteByCopy 把 source 流式复制到 target（覆盖已存在的 target）。
func overwriteByCopy(source, target string) error {
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	targetFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(targetFile, sourceFile)
	closeErr := targetFile.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// ImportSave 把存档压缩包解压到 savesDirectory，返回落盘的存档目录路径。
// 压缩包可以是 ExportSave 的产物（包内顶层为存档目录名），也可以
// 是直接压缩存档内容的扁平包；同名目录已存在时自动追加 -N 避免覆盖。
func ImportSave(ctx context.Context, archiveZipPath, savesDirectory string) (string, error) {
	if strings.TrimSpace(archiveZipPath) == "" ||
		strings.TrimSpace(savesDirectory) == "" {
		return "", fmt.Errorf("参数无效")
	}
	info, err := os.Stat(archiveZipPath)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("存档压缩包不存在：%s", archiveZipPath)
	}
	if err := os.MkdirAll(savesDirectory, 0o755); err != nil {
		return "", err
	}

	reader, err := zip.OpenReader(archiveZipPath)
	if err != nil {
		return "", fmt.Errorf("不是有效的存档压缩包：%w", err)
	}
	defer reader.Close()

	staging, err := os.MkdirTemp(savesDirectory, ".nya-import-")
	if err != nil {
		return "", err
	}
	// 成功时根目录会先改名出暂存区，这里只清理剩下的空壳
	defer func() { _ = os.RemoveAll(staging) }()

	stagingAbsolute, err := filepath.Abs(staging)
	if err != nil {
		return "", err
	}
	if err := extractSaveArchive(ctx, reader.File, stagingAbsolute); err != nil {
		return "", err
	}

	root := staging
	if entries, err := os.ReadDir(staging); err == nil &&
		len(entries) == 1 && entries[0].IsDir() {
		root = filepath.Join(staging, entries[0].Name())
	}

	name := filepath.Base(root)
	if root == staging {
		// 扁平包：以压缩包文件名作为存档目录名
		name = strings.TrimSuffix(
			filepath.Base(archiveZipPath),
			filepath.Ext(archiveZipPath),
		)
	}
	name = sanitizeSaveName(name)
	if name == "" {
		name = "imported-world"
	}

	destination := uniqueSavePath(savesDirectory, name)
	if err := os.Rename(root, destination); err != nil {
		return "", err
	}
	return destination, nil
}

// sanitizeSaveName 替换存档目录名里的路径分隔符与 Windows 非法字符，防止越界。
func sanitizeSaveName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, strings.TrimSpace(name))

	return strings.Trim(name, ". ")
}

// uniqueSavePath 在 parent 下为 name 找一个不冲突的目录路径（已存在则追加 -N）。
func uniqueSavePath(parent, name string) string {
	candidate := filepath.Join(parent, name)
	for index := 1; ; index++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = filepath.Join(parent, fmt.Sprintf("%s-%d", name, index))
	}
}

// extractSaveArchive 把压缩包条目解压到 target（调用方保证为绝对路径）。
func extractSaveArchive(ctx context.Context, entries []*zip.File, target string) error {
	prefix := target + string(filepath.Separator)

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		cleaned, err := cleanSaveArchivePath(entry.Name)
		if err != nil {
			return err
		}
		if cleaned == "" {
			continue
		}
		destination := filepath.Join(target, filepath.FromSlash(cleaned))
		if destination != target && !strings.HasPrefix(destination, prefix) {
			return fmt.Errorf("压缩包内路径越出目标目录：%q", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return err
			}
			continue
		}
		if !entry.FileInfo().Mode().IsRegular() {
			continue
		}
		if err := extractSaveEntry(entry, destination); err != nil {
			return err
		}
	}
	return nil
}

// cleanSaveArchivePath 归一化包内路径为斜杠形式，并挡掉绝对路径、盘符与 ".."。
func cleanSaveArchivePath(name string) (string, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	if normalized == "" {
		return "", nil
	}
	if strings.HasPrefix(normalized, "/") {
		return "", fmt.Errorf("压缩包内路径不是相对路径：%q", name)
	}
	// zip 里同样可能写出 "C:/..." 这类带盘符的路径
	if len(normalized) >= 2 && normalized[1] == ':' {
		return "", fmt.Errorf("压缩包内路径含盘符：%q", name)
	}
	cleaned := path.Clean(normalized)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("压缩包内路径越出根目录：%q", name)
	}
	if cleaned == "." {
		return "", nil
	}
	return cleaned, nil
}

// extractSaveEntry 解压单个文件（统一按 0644 落盘）。
func extractSaveEntry(entry *zip.File, destination string) error {
	reader, err := entry.Open()
	if err != nil {
		return fmt.Errorf("读取 %s 失败：%w", entry.Name, err)
	}
	defer reader.Close()

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, reader); err != nil {
		file.Close()
		return fmt.Errorf("解压 %s 失败：%w", entry.Name, err)
	}
	return file.Close()
}

// DeleteSave 递归删除存档目录；目录已不存在视为成功。
func DeleteSave(saveDirectory string) error {
	if strings.TrimSpace(saveDirectory) == "" {
		return fmt.Errorf("参数无效")
	}
	normalized := mustAbsPath(saveDirectory)
	if info, err := os.Stat(normalized); err != nil || !info.IsDir() {
		return nil
	}
	return os.RemoveAll(normalized)
}

// createSaveArchive 递归打包存档目录到 archivePath（先写临时文件由调用方改名）。
func createSaveArchive(ctx context.Context, sourceDirectory, rootEntryName, archivePath string) (err error) {
	file, err := os.OpenFile(archivePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	archive := zip.NewWriter(file)
	// 中央目录在 Close 时才写：磁盘写满/配额不足只在这里暴露。错误必须带出去，
	// 否则调用方会把截断的 zip 改名成目标文件、报成功。
	defer func() {
		if closeErr := archive.Close(); err == nil {
			err = closeErr
		}
	}()

	return filepath.WalkDir(sourceDirectory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(sourceDirectory, path)
		if err != nil {
			return err
		}
		if strings.EqualFold(filepath.Base(path), sessionLockName) {
			return nil
		}
		// ZIP 规范要求条目名使用 '/'：Windows 分隔符会被非 Windows 工具解包成损坏文件名
		entryName := rootEntryName + "/" + filepath.ToSlash(relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = entryName
		header.Method = zip.Deflate
		entryWriter, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		// 不用 defer：WalkDir 回调里 defer 会把句柄攒到遍历结束才释放
		if _, err := io.Copy(entryWriter, source); err != nil {
			source.Close()

			return err
		}

		return source.Close()
	})
}

