// 服务器目录的文件管理：列表、文本读写（内置编辑器）、存档/模组/插件导入。
package mcserver

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// FileEntry 文件列表项。
type FileEntry struct {
	Name    string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

const textFileLimit = 2 << 20 // 文本读取上限 2MB

// resolveServerPath 把相对路径安全解析到服务器目录内（拒绝越界）。
func resolveServerPath(id, relative string) (string, error) {
	if err := validateID(id); err != nil {
		return "", err
	}
	root := serverDirectory(id)
	cleaned := path.Clean(strings.ReplaceAll(relative, "\\", "/"))
	if cleaned == "." {
		return root, nil
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.HasPrefix(cleaned, "/") {
		return "", errors.New("路径越出服务器目录")
	}
	target := filepath.Join(root, filepath.FromSlash(cleaned))
	if !strings.HasPrefix(strings.ToLower(target), strings.ToLower(root+string(os.PathSeparator))) {
		return "", errors.New("路径越出服务器目录")
	}

	return target, nil
}

// ListFiles 列出目录（文件夹在前，名称排序）。
func ListFiles(id, relative string) ([]FileEntry, error) {
	target, err := resolveServerPath(id, relative)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	result := make([]FileEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, FileEntry{
			Name:    entry.Name(),
			IsDir:   entry.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	// 文件夹优先，其余按名称
	for i := 1; i < len(result); i++ {
		for j := i; j > 0; j-- {
			if result[j].IsDir == result[j-1].IsDir &&
				result[j].Name < result[j-1].Name {
				result[j], result[j-1] = result[j-1], result[j]

				continue
			}
			if result[j].IsDir && !result[j-1].IsDir {
				result[j], result[j-1] = result[j-1], result[j]
			}
		}
	}

	return result, nil
}

// ReadServerTextFile 读取文本文件（限制大小，供内置编辑器）。
func ReadServerTextFile(id, relative string) (string, error) {
	target, err := resolveServerPath(id, relative)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", errors.New("目标是目录")
	}
	if info.Size() > textFileLimit {
		return "", errors.New("文件过大（>2MB），请在服务器目录中用外部编辑器打开")
	}

	data, err := os.ReadFile(target)

	return string(data), err
}

// WriteServerTextFile 写回文本文件。
func WriteServerTextFile(id, relative, content string) error {
	target, err := resolveServerPath(id, relative)
	if err != nil {
		return err
	}

	return os.WriteFile(target, []byte(content), 0o644)
}

// worldDirName 从 server.properties 读取 level-name（缺省 world）。
func worldDirName(id string) string {
	properties, err := GetServerProperties(id)
	if err != nil {
		return "world"
	}
	for _, property := range properties {
		if property.Key == "level-name" && strings.TrimSpace(property.Value) != "" {
			return strings.TrimSpace(property.Value)
		}
	}

	return "world"
}

// coreSupportsMods Fabric/NeoForge 用 mods 目录；Paper 用 plugins；Vanilla 无。
func coreSupportsMods(core string) bool {
	return core == CoreFabric || core == CoreNeoForge
}

// ImportServerWorld 导入存档压缩包（zip，顶层为 level.dat 或单层目录）。
func ImportServerWorld(id, zipPath string) error {
	if Default().IsRunning(id) {
		return errors.New("服务器运行中，请先停止再导入存档")
	}
	if err := validateID(id); err != nil {
		return err
	}
	worldDir := filepath.Join(serverDirectory(id), worldDirName(id))
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		return err
	}

	return extractZipRoot(zipPath, worldDir)
}

// extractZipRoot 解压 zip 到目标目录；顶层只有一个目录时下钻一层（ExportSave 习惯）。
func extractZipRoot(zipPath, target string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("不是有效的压缩包：%w", err)
	}
	defer reader.Close()

	// 先全部清洗校验（拦截 ../ 越界），再判断是否需要下钻单层根目录
	type safeEntry struct {
		name string
		file *zip.File
	}
	entries := make([]safeEntry, 0, len(reader.File))
	for _, file := range reader.File {
		cleaned, err := cleanZipPath(file.Name)
		if err != nil {
			return err
		}
		if cleaned == "" {
			continue
		}
		entries = append(entries, safeEntry{name: cleaned, file: file})
	}

	root := ""
	if len(entries) > 0 {
		first := strings.SplitN(entries[0].name, "/", 2)[0]
		only := true
		nested := false
		for _, entry := range entries {
			if strings.HasPrefix(entry.name, first+"/") {
				nested = true
			}
			if entry.name != first && !strings.HasPrefix(entry.name, first+"/") {
				only = false

				break
			}
		}
		// 只有"所有条目都在 first/ 之下"才下钻。不能靠名字判断首条目是不是
		// 目录：zip 里显式的 "world/" 目录条目被 path.Clean 去掉尾斜杠后
		// 与同名文件无法区分，看有没有真正的嵌套条目才可靠。
		if only && first != "" && nested {
			root = first + "/"
		}
	}

	for _, entry := range entries {
		rel := strings.TrimPrefix(entry.name, root)
		if rel == "" {
			continue
		}
		dest := filepath.Join(target, filepath.FromSlash(rel))
		if entry.file.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}

			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := copyZipEntry(entry.file, dest); err != nil {
			return err
		}
	}

	return nil
}

// cleanZipPath 归一化压缩包内路径并拦截越界。
func cleanZipPath(name string) (string, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	if normalized == "" || strings.HasPrefix(normalized, "/") {
		return "", errors.New("压缩包内路径非法")
	}
	cleaned := path.Clean(normalized)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("压缩包内路径越出目标目录")
	}
	if cleaned == "." {
		return "", nil
	}

	return cleaned, nil
}

// copyZipEntry 解压单个文件。
func copyZipEntry(file *zip.File, dest string) error {
	source, err := file.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, source)

	return err
}

// ImportServerMod 复制 mod jar 到 mods/（Fabric/NeoForge）。
func ImportServerMod(id, srcJarPath string) error {
	cfg, err := loadServerConfig(serverDirectory(id))
	if err != nil {
		return err
	}
	if !coreSupportsMods(cfg.Core) {
		return fmt.Errorf("核心 %s 不支持模组（仅 Fabric/NeoForge）", cfg.Core)
	}

	return copyIntoServerDir(id, srcJarPath, "mods")
}

// ImportServerPlugin 复制插件 jar 到 plugins/（Paper）。
func ImportServerPlugin(id, srcJarPath string) error {
	cfg, err := loadServerConfig(serverDirectory(id))
	if err != nil {
		return err
	}
	if cfg.Core != CorePaper {
		return fmt.Errorf("核心 %s 不支持插件（仅 Paper）", cfg.Core)
	}

	return copyIntoServerDir(id, srcJarPath, "plugins")
}

// copyIntoServerDir 复制文件到服务器子目录（不存在则创建；重名覆盖）。
func copyIntoServerDir(id, srcPath, sub string) error {
	if Default().IsRunning(id) {
		return errors.New("服务器运行中，请先停止再导入")
	}
	if err := validateID(id); err != nil {
		return err
	}
	source, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer source.Close()

	destDir := filepath.Join(serverDirectory(id), sub)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	dest, err := os.Create(filepath.Join(destDir, filepath.Base(srcPath)))
	if err != nil {
		return err
	}
	defer dest.Close()

	_, err = io.Copy(dest, source)

	return err
}
