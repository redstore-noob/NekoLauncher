package bindings

// 插件包（.nekoex）读写。
//
// 格式就是一个 zip：包内**根目录**直接放插件本体（plugin.yaml + 入口文件 + icon.png +
// 其它资源），所以解压即得到一个插件目录。也兼容"多套了一层顶层目录"的包——作者常
// 直接 zip 整个文件夹，解压时会剥掉这层。
//
// 安全边界与目录安装一致：不覆盖同名插件、限定规模、逐项校验路径不越出插件目录
// （zip-slip），失败不留残缺目录。

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"nekolauncher/internal/tools"
)

// pluginPackageExtension 插件包扩展名。
const pluginPackageExtension = ".nekoex"

// pluginManifestMaxBytes 清单大小上限，防止包内塞一个超大 plugin.yaml。
const pluginManifestMaxBytes = 1 << 20

// InstallPluginArchive 从 .nekoex 插件包安装，返回插件 id。同名插件已存在时报错。
// 安装/卸载共享 installMu（见 InstallPluginDirectory）。
func (a *PluginAPI) InstallPluginArchive(archivePath string) (string, error) {
	a.installMu.Lock()
	defer a.installMu.Unlock()

	return a.installPluginArchive(archivePath)
}

func (a *PluginAPI) installPluginArchive(archivePath string) (string, error) {
	archive := filepath.Clean(strings.TrimSpace(archivePath))
	if archive == "" || archive == "." {
		return "", errors.New("未选择插件包")
	}
	stat, err := os.Stat(archive)
	if err != nil || stat.IsDir() {
		return "", fmt.Errorf("插件包不可用：%s", archivePath)
	}

	reader, err := zip.OpenReader(archive)
	if err != nil {
		return "", fmt.Errorf("插件包不是有效的 zip：%w", err)
	}
	defer reader.Close()

	prefix, err := pluginArchivePrefix(reader.File)
	if err != nil {
		return "", err
	}
	manifest, err := readManifestFromArchive(reader.File, prefix)
	if err != nil {
		return "", err
	}
	if err := validatePluginID(manifest.ID); err != nil {
		return "", err
	}

	root := a.directory()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(root, manifest.ID)
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("已存在同名插件「%s」，请先卸载", manifest.ID)
	}

	if err := extractPluginArchive(reader.File, prefix, target, manifest); err != nil {
		// 失败不留残缺目录，否则插件页上会出现一个装了一半的插件
		os.RemoveAll(target)
		return "", err
	}
	return manifest.ID, nil
}

// PackagePlugin 把插件源码目录打包成 .nekoex，返回实际写入的路径；
// targetPath 没有 .nekoex 扩展名时自动补上。
func (a *PluginAPI) PackagePlugin(sourceDirectory, targetPath string) (string, error) {
	source := filepath.Clean(strings.TrimSpace(sourceDirectory))
	stat, err := os.Stat(source)
	if err != nil || !stat.IsDir() {
		return "", fmt.Errorf("插件目录不可用：%s", sourceDirectory)
	}
	manifest, err := readPluginManifest(source)
	if err != nil {
		return "", err
	}
	if err := validatePluginID(manifest.ID); err != nil {
		return "", err
	}
	// 入口文件不存在说明目录本身不完整，打出来的包也会是坏的，直接拦下
	entryName := manifest.entryFile()
	if !tools.FileExists(filepath.Join(source, filepath.FromSlash(entryName))) {
		return "", fmt.Errorf("插件目录里找不到入口文件：%s", entryName)
	}

	target := strings.TrimSpace(targetPath)
	if target == "" {
		return "", errors.New("未选择保存位置")
	}
	if !strings.EqualFold(filepath.Ext(target), pluginPackageExtension) {
		target += pluginPackageExtension
	}
	if isSameOrInside(source, target) {
		return "", errors.New("打包目标不能放在插件源码目录内")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}

	file, err := os.Create(target)
	if err != nil {
		return "", err
	}
	writer := zip.NewWriter(file)
	writeErr := writePluginArchive(writer, source)
	if writeErr == nil {
		writeErr = writer.Close()
	} else {
		writer.Close()
	}
	if writeErr == nil {
		writeErr = file.Close()
	} else {
		file.Close()
	}
	if writeErr != nil {
		os.Remove(target)
		return "", writeErr
	}
	return target, nil
}

// pluginArchivePrefix 判断包内插件目录的根前缀：清单在根目录返回 ""；
// 若存在唯一一个顶层目录且清单在其中（直接 zip 整个文件夹），返回 "<目录>/"。
func pluginArchivePrefix(entries []*zip.File) (string, error) {
	names := make(map[string]bool, len(entries))
	topLevel := make(map[string]bool, 4)
	for _, entry := range entries {
		cleaned, err := cleanArchivePath(entry.Name)
		if err != nil || cleaned == "" {
			continue // 非法条目留给解压阶段报错
		}
		names[cleaned] = true
		if index := strings.Index(cleaned, "/"); index > 0 {
			topLevel[cleaned[:index+1]] = true
		}
	}
	if names[pluginManifestName] {
		return "", nil
	}

	candidates := make([]string, 0, len(topLevel))
	for prefix := range topLevel {
		if names[prefix+pluginManifestName] {
			candidates = append(candidates, prefix)
		}
	}
	sort.Strings(candidates)
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return "", errors.New("插件包里找不到 " + pluginManifestName)
	default:
		return "", fmt.Errorf("插件包里有多个目录都含 %s，无法判断以哪个为根", pluginManifestName)
	}
}

// readManifestFromArchive 读取并解析包内清单。
func readManifestFromArchive(entries []*zip.File, prefix string) (*pluginManifest, error) {
	wanted := prefix + pluginManifestName
	for _, entry := range entries {
		cleaned, err := cleanArchivePath(entry.Name)
		if err != nil || cleaned != wanted {
			continue
		}
		if entry.UncompressedSize64 > pluginManifestMaxBytes {
			return nil, errors.New(pluginManifestName + " 过大")
		}
		reader, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败：%w", pluginManifestName, err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(reader, pluginManifestMaxBytes))
		reader.Close()
		if readErr != nil {
			return nil, fmt.Errorf("读取 %s 失败：%w", pluginManifestName, readErr)
		}

		manifest, err := parsePluginManifest(raw)
		if err != nil {
			return nil, err
		}
		return manifest, nil
	}
	return nil, errors.New("插件包里找不到 " + pluginManifestName)
}

// extractPluginArchive 解压插件本体到 target：剥掉前缀、跳过目录与非普通文件、
// 逐项校验路径不越界，并按安装上限限制条目数与解压后体积。
func extractPluginArchive(entries []*zip.File, prefix, target string, manifest *pluginManifest) error {
	targetAbsolute, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	wantedEntry := filepath.ToSlash(manifest.entryFile())

	var totalBytes int64
	var fileCount int
	var entryFound bool

	for _, entry := range entries {
		cleaned, err := cleanArchivePath(entry.Name)
		if err != nil {
			return err
		}
		if prefix != "" {
			if !strings.HasPrefix(cleaned, prefix) {
				continue
			}
			cleaned = strings.TrimPrefix(cleaned, prefix)
		}
		if cleaned == "" {
			continue // 前缀目录自身
		}

		// 每个条目都计数（含目录条目）：只数普通文件的话，一个塞满目录条目的
		// 包能在不触发上限的情况下在磁盘上创建海量目录
		fileCount++
		if fileCount > pluginInstallMaxFiles {
			return fmt.Errorf("插件包内条目数超过上限（%d）", pluginInstallMaxFiles)
		}

		fileInfo := entry.FileInfo()
		destination := filepath.Join(targetAbsolute, filepath.FromSlash(cleaned))
		if !strings.HasPrefix(destination, targetAbsolute+string(filepath.Separator)) {
			return fmt.Errorf("包内路径越出插件目录：%q", entry.Name)
		}
		if fileInfo.IsDir() {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return err
			}
			continue
		}
		if !fileInfo.Mode().IsRegular() {
			continue // 软链/设备等一律跳过，避免解出意外内容
		}

		// zip64 的 UncompressedSize64 可以被伪造成 2^64-1：先按 uint64 与上限比较，
		// 否则 int64() 溢出成负数，下面的累计判断永远不触发。
		if entry.UncompressedSize64 > uint64(pluginInstallMaxBytes) {
			return fmt.Errorf("插件包内文件 %s 解压后体积超过上限（%d MB）", entry.Name, pluginInstallMaxBytes>>20)
		}
		totalBytes += int64(entry.UncompressedSize64)
		if totalBytes > pluginInstallMaxBytes {
			return fmt.Errorf("插件包解压后体积超过上限（%d MB）", pluginInstallMaxBytes>>20)
		}
		if cleaned == wantedEntry {
			entryFound = true
		}
		if err := extractArchiveFile(entry, destination); err != nil {
			return err
		}
	}

	if !entryFound {
		return fmt.Errorf("插件包里找不到入口文件：%s", manifest.entryFile())
	}
	return nil
}

// extractArchiveFile 解压单个文件。压缩包里的权限位没有意义，统一按 0644 落盘。
func extractArchiveFile(entry *zip.File, destination string) error {
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
	// 多读 1 字节：读满上限即说明实际体积超限，不能让 LimitReader 静默截断后报成功。
	written, err := io.Copy(file, io.LimitReader(reader, pluginInstallMaxBytes+1))
	if err != nil {
		file.Close()
		return fmt.Errorf("解压 %s 失败：%w", entry.Name, err)
	}
	if written > pluginInstallMaxBytes {
		file.Close()
		os.Remove(destination)
		return fmt.Errorf("插件包内文件 %s 解压后体积超过上限（%d MB）", entry.Name, pluginInstallMaxBytes>>20)
	}
	return file.Close()
}

// writePluginArchive 把插件目录内容写到 zip 根目录（不额外套一层目录）。
func writePluginArchive(writer *zip.Writer, source string) error {
	return filepath.Walk(source, func(current string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil // 目录由条目路径隐式创建；软链等一律跳过
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		header.Method = zip.Deflate

		entryWriter, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		reader, err := os.Open(current)
		if err != nil {
			return err
		}
		// 不用 defer：Walk 回调里 defer 会把句柄攒到遍历结束才释放
		_, copyErr := io.Copy(entryWriter, reader)
		closeErr := reader.Close()
		if copyErr != nil {
			return fmt.Errorf("写入 %s 失败：%w", relative, copyErr)
		}

		return closeErr
	})
}

// cleanArchivePath 把包内路径归一化为斜杠形式，并挡掉绝对路径、盘符、
// NTFS 备用数据流（ADS）与 ".."。
func cleanArchivePath(name string) (string, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	if normalized == "" {
		return "", errors.New("包内路径为空")
	}
	if strings.HasPrefix(normalized, "/") {
		return "", fmt.Errorf("包内路径不是相对路径：%q", name)
	}
	// zip 里同样可能写出 "C:/..." 这类带盘符的路径
	if len(normalized) >= 2 && normalized[1] == ':' {
		return "", fmt.Errorf("包内路径含盘符：%q", name)
	}
	// 任何位置的 ":" 都拒绝：NTFS 会把 "file:stream" 解析成主文件 file 的
	// 备用数据流——文件名无扩展名，能绕过投递路由的扩展名拦截（见
	// plugin_handler.go），也破坏"插件目录内只放常规文件"的假设
	if strings.ContainsRune(normalized, ':') {
		return "", fmt.Errorf("包内路径含非法字符 \":\"：%q", name)
	}
	cleaned := path.Clean(normalized)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("包内路径越出根目录：%q", name)
	}
	if cleaned == "." {
		return "", nil
	}
	return cleaned, nil
}
