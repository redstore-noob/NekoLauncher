// NekoSolo 安装包导出：把启动器本体 + 捆绑 Java（可选）+ 整合包（版本文件与
// 内容）打成载荷 zip，再拼接到安装器模板（stub）尾部并写尾标。
// 布局约定（载荷 zip 内的顶层目录）：
//
//	manifest.json          元数据（model.Manifest）
//	icon.png               可选，整合包图标（安装器界面展示）
//	files/                 解压到安装根目录（NekoLauncher.exe、portable.flag）
//	minecraft/versions/…   解压到 <数据目录>/minecraft/versions/…
//	                       内容直接进 versions/<id>/，配合首启显式开启的版本隔离，
//	                       开箱即是"隔离实例"，且首启补齐缺失的 libraries/assets
//	jre/…                  可选，解压到 <数据目录>/runtime/jre/
package solo

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"

	"nekolauncher/internal/config"
	"nekolauncher/internal/modpack"
)

// maxInheritDepth 版本 inheritsFrom 父链的最大回溯深度（与实例扫描一致）。
const maxInheritDepth = 3

// launcherExecutable 启动器本体路径的获取入口。抽成变量仅为测试注入
// 小体积的假 exe（真实导出走 os.Executable，即正在运行的启动器）。
var launcherExecutable = os.Executable

// javaExcludedTopDirs 打包 JRE 时跳过的顶层目录（jmods 仅 JDK 有；demo/man 非运行必需）。
var javaExcludedTopDirs = map[string]bool{"jmods": true, "demo": true, "man": true}

// javaExcludedFiles 打包 JRE 时跳过的文件（源码包与压缩包说明）。
var javaExcludedFiles = map[string]bool{"src.zip": true, "javafx-src.zip": true}

// SoloExportOptions NekoSolo 安装包导出参数（字段扁平化，便于生成 Wails 绑定）。
type SoloExportOptions struct {
	PackName    string
	PackVersion string
	Author      string
	UpdateLink  string
	Description string
	IconPngPath string
	// MinecraftVersion 主游戏版本；LoaderName/LoaderVersion 仅作展示
	MinecraftVersion string
	LoaderName       string
	LoaderVersion    string
	// IncludedPaths 勾选打包的内容条目（相对内容目录的路径）
	IncludedPaths []string
	// ContentDirectory 实例内容目录（隔离实例即 versions/<id>，共享实例为游戏根）
	ContentDirectory string
	// VersionDirectory 作者实例的 versions/<id> 目录（承载版本 json/jar）
	VersionDirectory string
	// VersionID versions/ 下的实例目录名；空串时取 VersionDirectory 的目录名
	VersionID string
	// BundleJava 是否捆绑当前首选 Java 运行时
	BundleJava bool
	// SimpleMode 安装后启动器默认进入 NekoLauncher-S 模式
	SimpleMode bool
}

// ExportSolo 导出 NekoSolo 安装包到 outputPath（.exe）。
// progress 为可选回调，阶段名即中文描述，经 modpack:exportProgress 事件推送。
func ExportSolo(
	ctx context.Context,
	options SoloExportOptions,
	outputPath string,
	progress func(modpack.ModpackExportProgress),
) (modpack.ModpackExportResult, error) {
	var result modpack.ModpackExportResult
	if strings.TrimSpace(outputPath) == "" {
		return result, fmt.Errorf("输出路径不能为空")
	}
	if !strings.EqualFold(filepath.Ext(outputPath), ".exe") {
		return result, fmt.Errorf("NekoSolo 安装包必须是 .exe 文件")
	}
	if strings.TrimSpace(options.PackName) == "" {
		return result, fmt.Errorf("整合包名称不能为空")
	}
	if strings.TrimSpace(options.MinecraftVersion) == "" {
		return result, fmt.Errorf("无法确定 Minecraft 版本")
	}
	versionID := strings.TrimSpace(options.VersionID)
	if versionID == "" {
		versionID = filepath.Base(strings.TrimRight(filepath.ToSlash(options.VersionDirectory), "/"))
	}
	if versionID == "" || versionID == "." || versionID == "/" {
		return result, fmt.Errorf("无法确定要打包的版本目录")
	}
	versionJSONPath := filepath.Join(options.VersionDirectory, versionID+".json")
	if info, err := os.Stat(versionJSONPath); err != nil || info.IsDir() {
		return result, fmt.Errorf("版本目录缺少 %s.json：%s", versionID, versionJSONPath)
	}
	if info, err := os.Stat(options.ContentDirectory); err != nil || !info.IsDir() {
		return result, fmt.Errorf("实例内容目录不存在：%s", options.ContentDirectory)
	}
	emitProgress(progress, "正在定位安装器模板", 0, 1)
	stubPath, err := FindStubTemplate()
	if err != nil {
		return result, err
	}
	launcherExe, err := launcherExecutable()
	if err != nil {
		return result, fmt.Errorf("无法定位启动器本体：%w", err)
	}

	// ---- 收集待写入载荷的文件 ----
	emitProgress(progress, "正在收集版本文件", 0, 1)
	versionsRoot := filepath.Dir(options.VersionDirectory)
	versionEntries, err := collectVersionChain(versionsRoot, versionID)
	if err != nil {
		return result, err
	}

	emitProgress(progress, "正在收集整合包内容", 0, 1)
	contentEntries, warnings, err := collectContentEntries(options, versionID)
	if err != nil {
		return result, err
	}

	javaEntries := []payloadEntry{}
	if options.BundleJava {
		emitProgress(progress, "正在收集 Java 运行时", 0, 1)
		javaEntries, err = collectJavaEntries(primaryJavaHome())
		if err != nil {
			return result, err
		}
		if len(javaEntries) == 0 {
			warnings = append(warnings, "未配置首选 Java，跳过捆绑：玩家首次启动时需自行安装 Java。")
		}
	}

	// ---- 写载荷 ----
	emitProgress(progress, "正在写入载荷", 0, 1)
	temporaryPath := fmt.Sprintf("%s.%s.nekosolo-tmp", outputPath, newGUID())
	defer func() { _ = os.Remove(temporaryPath) }()
	payloadTemp := temporaryPath + ".payload"
	defer func() { _ = os.Remove(payloadTemp) }()

	manifest := Manifest{
		Format:        PayloadFormat,
		PackID:        SanitizePackID(options.PackName),
		PackName:      options.PackName,
		PackVersion:   options.PackVersion,
		Author:        options.Author,
		Description:   options.Description,
		MCVersion:     options.MinecraftVersion,
		LoaderName:    options.LoaderName,
		LoaderVersion: options.LoaderVersion,
		VersionID:     versionID,
		SimpleMode:    options.SimpleMode,
		HasJava:       len(javaEntries) > 0,
		UpdateLink:    options.UpdateLink,
	}
	if options.IconPngPath != "" {
		if _, err := os.Stat(options.IconPngPath); err == nil {
			manifest.IconPath = "icon.png"
		}
	}

	payloadLength, payloadCRC, err := writePayload(ctx, payloadTemp, manifest, options, launcherExe,
		versionEntries, contentEntries, javaEntries, progress)
	if err != nil {
		return result, err
	}

	// ---- 组装最终 exe：stub + payload + trailer ----
	stubInfo, err := os.Stat(stubPath)
	if err != nil {
		return result, err
	}
	if err := assembleInstaller(stubPath, stubInfo.Size(), payloadTemp, payloadLength, payloadCRC, temporaryPath); err != nil {
		return result, err
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return result, err
	}

	emitProgress(progress, "完成", 1, 1)
	return modpack.ModpackExportResult{
		OutputPath:    outputPath,
		DeclaredFiles: len(versionEntries),
		OverrideFiles: len(contentEntries),
		Warnings:      warnings,
	}, nil
}

// payloadEntry 待写入载荷的单个文件：载荷内路径 + 源文件绝对路径。
type payloadEntry struct {
	archivePath string
	sourcePath  string
}

// collectVersionChain 收集版本目录及其 inheritsFrom/jar 依赖链的文件
// （json + jar），载荷路径以 minecraft/versions/ 为根。根版本缺失 json 报错，
// 依赖链缺失报错（这样的版本装上也无法被扫描识别）。
func collectVersionChain(versionsRoot, versionID string) ([]payloadEntry, error) {
	entries := []payloadEntry{}
	seen := map[string]bool{}
	type chainItem struct {
		id    string
		depth int
	}
	queue := []chainItem{{strings.TrimSpace(versionID), 0}}
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		key := strings.ToLower(item.id)
		if item.id == "" || seen[key] {
			continue
		}
		seen[key] = true

		versionDirectory := filepath.Join(versionsRoot, item.id)
		jsonPath := filepath.Join(versionDirectory, item.id+".json")
		if info, err := os.Stat(jsonPath); err != nil || info.IsDir() {
			return nil, fmt.Errorf("版本描述文件不存在：%s", jsonPath)
		}
		entries = append(entries, payloadEntry{
			archivePath: "minecraft/versions/" + item.id + "/" + item.id + ".json",
			sourcePath:  jsonPath,
		})
		jarPath := filepath.Join(versionDirectory, item.id+".jar")
		if _, err := os.Stat(jarPath); err == nil {
			entries = append(entries, payloadEntry{
				archivePath: "minecraft/versions/" + item.id + "/" + item.id + ".jar",
				sourcePath:  jarPath,
			})
		}

		var meta struct {
			InheritsFrom string `json:"inheritsFrom"`
			Jar          string `json:"jar"`
		}
		raw, err := os.ReadFile(jsonPath)
		if err != nil {
			return nil, fmt.Errorf("读取版本描述文件失败：%w", err)
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("版本描述文件不是有效 JSON（%s）：%w", item.id, err)
		}
		// jar 字段：客户端 jar 借用另一版本的文件，只拷那一个 jar
		if jar := strings.TrimSpace(meta.Jar); jar != "" && !strings.EqualFold(jar, item.id) && !seen[strings.ToLower(jar)] {
			borrowed := filepath.Join(versionsRoot, jar, jar+".jar")
			if _, err := os.Stat(borrowed); err != nil {
				return nil, fmt.Errorf("版本 %s 声明的客户端 jar 不存在：%s", item.id, borrowed)
			}
			entries = append(entries, payloadEntry{
				archivePath: "minecraft/versions/" + jar + "/" + jar + ".jar",
				sourcePath:  borrowed,
			})
			seen[strings.ToLower(jar)] = true
		}
		// inheritsFrom：整个父版本目录（json + jar）都要带上
		if parent := strings.TrimSpace(meta.InheritsFrom); parent != "" && !strings.EqualFold(parent, item.id) {
			if item.depth >= maxInheritDepth {
				return nil, fmt.Errorf("版本 %s 的 inheritsFrom 链过深（>%d）", item.id, maxInheritDepth)
			}
			queue = append(queue, chainItem{parent, item.depth + 1})
		}
	}
	return entries, nil
}

// collectContentEntries 按 IncludedPaths 过滤并展开实例内容，
// 载荷路径为 minecraft/versions/<versionID>/<相对路径>。重复条目去重。
func collectContentEntries(options SoloExportOptions, versionID string) ([]payloadEntry, []string, error) {
	included := map[string]bool{}
	for _, path := range options.IncludedPaths {
		included[strings.ToLower(filepath.ToSlash(path))] = true
	}
	all := modpack.CollectContent(options.ContentDirectory)

	unique := map[string]bool{}
	entries := []payloadEntry{}
	var warnings []string
	for _, item := range all {
		if !included[strings.ToLower(item.RelativePath)] {
			continue
		}
		source := filepath.Join(options.ContentDirectory, filepath.FromSlash(item.RelativePath))
		if item.IsDirectory {
			for _, file := range enumerateRecursively(source) {
				relative, err := filepath.Rel(options.ContentDirectory, file)
				if err != nil {
					continue
				}
				addContentEntry(&entries, unique, versionID, relative, file)
			}
		} else {
			if _, err := os.Stat(source); err != nil {
				warnings = append(warnings, fmt.Sprintf("跳过 %s：%v", item.RelativePath, err))
				continue
			}
			addContentEntry(&entries, unique, versionID, item.RelativePath, source)
		}
	}
	return entries, warnings, nil
}

func addContentEntry(entries *[]payloadEntry, unique map[string]bool, versionID, relative, source string) {
	key := "minecraft/versions/" + versionID + "/" + filepath.ToSlash(relative)
	normalized := strings.ToLower(key)
	if unique[normalized] {
		return
	}
	unique[normalized] = true
	*entries = append(*entries, payloadEntry{archivePath: key, sourcePath: source})
}

// enumerateRecursively 递归枚举目录下全部文件；不可读子树静默跳过。
func enumerateRecursively(directory string) []string {
	var result []string
	walkSafe(directory, &result)
	return result
}

func walkSafe(directory string, result *[]string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			walkSafe(path, result)
			continue
		}
		*result = append(*result, path)
	}
}

// primaryJavaHome 当前首选 Java 的主目录（java.exe 上两级：…/bin/java.exe → …）。
// 未配置或层级异常时返回空串。
func primaryJavaHome() string {
	items := config.GetJavaPaths()
	if len(items) == 0 || strings.TrimSpace(items[0].JavaPath) == "" {
		return ""
	}
	bin := filepath.Dir(filepath.FromSlash(items[0].JavaPath))
	home := filepath.Dir(bin)
	if strings.EqualFold(filepath.Base(bin), "bin") && home != bin {
		return home
	}
	// 路径不符合 <home>/bin/java.exe 布局：把它本身当主目录兜底
	return bin
}

// collectJavaEntries 打包 Java 主目录（跳过 jmods/demo/man 与源码包）。
// javaHome 为空或不可读时返回空列表（由调用方记 warning）。
func collectJavaEntries(javaHome string) ([]payloadEntry, error) {
	if strings.TrimSpace(javaHome) == "" {
		return []payloadEntry{}, nil
	}
	info, err := os.Stat(javaHome)
	if err != nil || !info.IsDir() {
		return []payloadEntry{}, nil
	}
	entries := []payloadEntry{}
	err = filepath.WalkDir(javaHome, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // 不可读子树跳过，不中断打包
		}
		if d.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(javaHome, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if javaExcludedTopDirs[parts[0]] {
			return nil
		}
		if javaExcludedFiles[parts[len(parts)-1]] {
			return nil
		}
		entries = append(entries, payloadEntry{
			archivePath: "jre/" + filepath.ToSlash(relative),
			sourcePath:  path,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// writePayload 把全部条目写成载荷 zip；返回载荷长度与 CRC32。
func writePayload(
	ctx context.Context,
	payloadPath string,
	manifest Manifest,
	options SoloExportOptions,
	launcherExe string,
	versionEntries, contentEntries, javaEntries []payloadEntry,
	progress func(modpack.ModpackExportProgress),
) (int64, uint32, error) {
	file, err := os.OpenFile(payloadPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	hasher := crc32.NewIEEE()
	counter := &countingWriter{inner: file}
	archive := zip.NewWriter(io.MultiWriter(counter, hasher))

	writeErr := func() error {
		manifestJSON, err := marshalSoloJSON(manifest)
		if err != nil {
			return err
		}
		if err := writeTextEntry(archive, "manifest.json", manifestJSON); err != nil {
			return err
		}
		if manifest.IconPath != "" {
			if err := copyFileEntry(archive, manifest.IconPath, options.IconPngPath); err != nil {
				return err
			}
		}
		// 启动器本体 + 便携标记：安装器整体解到安装根目录。
		// 载荷内固定命名 NekoLauncher.exe——安装器按此固定名注册与拉起启动器
		if err := copyFileEntry(archive, "files/NekoLauncher.exe", launcherExe); err != nil {
			return err
		}
		if err := writeTextEntry(archive, "files/portable.flag", ""); err != nil {
			return err
		}

		total := len(versionEntries) + len(contentEntries) + len(javaEntries)
		written := 0
		for _, group := range []struct {
			label  string
			entries []payloadEntry
		}{
			{"正在写入版本文件", versionEntries},
			{"正在写入整合包内容", contentEntries},
			{"正在写入 Java 运行时", javaEntries},
		} {
			for _, entry := range group.entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				emitProgress(progress, group.label, written, total)
				if err := copyFileEntry(archive, entry.archivePath, entry.sourcePath); err != nil {
					return fmt.Errorf("写入 %s 失败：%w", entry.archivePath, err)
				}
				written++
			}
		}
		return nil
	}()
	if writeErr != nil {
		return 0, 0, writeErr
	}
	// 中央目录在 Close 时才写：磁盘满/配额不足只在这里暴露
	if err := archive.Close(); err != nil {
		return 0, 0, err
	}
	return counter.count, hasher.Sum32(), nil
}

// assembleInstaller 流式拼接 stub + 载荷 + 尾标到 outputPath。
func assembleInstaller(stubPath string, stubSize int64, payloadPath string, payloadLength int64, payloadCRC uint32, outputPath string) error {
	stub, err := os.Open(stubPath)
	if err != nil {
		return err
	}
	defer stub.Close()
	payload, err := os.Open(payloadPath)
	if err != nil {
		return err
	}
	defer payload.Close()
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer output.Close()

	if _, err := io.Copy(output, stub); err != nil {
		return err
	}
	if _, err := io.Copy(output, payload); err != nil {
		return err
	}
	if err := AppendTrailer(output, stubSize, payloadLength, payloadCRC); err != nil {
		return err
	}
	return output.Sync()
}

// marshalSoloJSON 两空格缩进、不转义 HTML 字符的 JSON。
func marshalSoloJSON(value any) (string, error) {
	var buffer strings.Builder
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimRight(buffer.String(), "\n"), nil
}

func writeTextEntry(archive *zip.Writer, name, content string) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.WriteString(writer, content)
	return err
}

func copyFileEntry(archive *zip.Writer, name, sourcePath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = name
	header.Method = zip.Deflate
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, source)
	return err
}

// countingWriter 统计写入字节数（载荷长度供尾标使用）。
type countingWriter struct {
	inner io.Writer
	count int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.inner.Write(p)
	w.count += int64(n)
	return n, err
}

func emitProgress(progress func(modpack.ModpackExportProgress), phase string, current, total int) {
	if progress != nil {
		progress(modpack.ModpackExportProgress{Phase: phase, Current: current, Total: total})
	}
}

func newGUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
