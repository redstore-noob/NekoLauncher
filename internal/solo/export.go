// NekoSolo 安装包导出（v3：Modrinth 整合包壳）：
//
//	最终 exe = 安装器模板（stub，C# WPF）+ 载荷 zip + 32 字节尾标（"NKSOLO3\x03"）。
//	载荷 zip = 标准 .mrpack 结构（modrinth.index.json + overrides/）
//	           + manifest.json 元数据 + files/（NekoLauncher.exe、portable.flag）。
//
// 与 v1/v2 的区别：载荷里不再有 versions/ 描述文件与 jre/ 运行时——
//   - mods 优先在 modrinth.index.json 里声明直链（复用 modpack.Export 的
//     SHA1→Modrinth 匹配），找不到对应文件的 mod 与 config/saves/options.txt
//     保留在 overrides/ 随包分发；
//   - Minecraft 本体、libraries/assets 由启动器首启按常规流程联网补全；
//   - Java 运行时改由启动器首启按 MC 版本联网下载（不再捆绑分发）。
//   补全流程由安装器写下的 neko-solo.json（pendingPayload 字段）触发，
//   见 marker.go / completion.go。
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

	"nekolauncher/internal/modpack"
	"nekolauncher/internal/tools"
)

// launcherExecutable 启动器本体路径的获取入口。抽成变量仅为测试注入
// 小体积的假 exe（真实导出走 os.Executable，即正在运行的启动器）。
var launcherExecutable = os.Executable

// SoloExportOptions NekoSolo 安装包导出参数（字段扁平化，便于生成 Wails 绑定）。
type SoloExportOptions struct {
	PackName    string
	PackVersion string
	Author      string
	UpdateLink  string
	Description string
	IconPngPath string
	// MinecraftVersion 主游戏版本；LoaderName/LoaderVersion 同时写进
	// modrinth.index.json 的 dependencies（玩家首启按它装 Loader）。
	MinecraftVersion string
	LoaderName       string
	LoaderVersion    string
	// IncludedPaths 勾选打包的内容条目（相对内容目录的路径）
	IncludedPaths []string
	// ContentDirectory 实例内容目录（隔离实例即 versions/<id>，共享实例为游戏根）
	ContentDirectory string
	// VersionDirectory 作者实例的 versions/<id> 目录。v3 载荷不再携带版本
	// 描述文件，仅在 VersionID 为空时用它的目录名兜底实例名。
	VersionDirectory string
	// VersionID 安装后的实例目录名；空串时取 VersionDirectory 的目录名
	VersionID string
	// SimpleMode 安装后启动器默认进入 NekoLauncher-S 模式
	SimpleMode bool
	// RemoteDistribution 在线安装包（尾标 v2）：载荷 zip 不打进 exe，
	// 而是生成独立的 zip 供作者上传（如 GitHub Releases），安装器安装时下载。
	RemoteDistribution bool
	// PayloadURL 在线安装包的载荷下载地址（必须 https；通常为
	// https://github.com/<owner>/<repo>/releases/download/<tag>/<file>）
	PayloadURL string
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
	// 输出路径来自用户输入（整合包名/版本号拼出来的文件名）：带 '?' 时 Windows
	// 会拒绝创建文件，统一换成 '0'（mrpack 与临时文件都从它派生，一并受益）。
	outputPath = tools.SanitizeSavePath(outputPath)
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
	if options.RemoteDistribution && !strings.HasPrefix(strings.TrimSpace(options.PayloadURL), "https://") {
		return result, fmt.Errorf("在线安装包需要 https:// 开头的载荷下载地址（如 GitHub Releases 资产直链）")
	}
	versionID := strings.TrimSpace(options.VersionID)
	if versionID == "" {
		versionID = filepath.Base(strings.TrimRight(filepath.ToSlash(options.VersionDirectory), "/"))
	}
	if versionID == "" || versionID == "." || versionID == "/" {
		return result, fmt.Errorf("无法确定要打包的版本目录")
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

	// ---- 第一步：按 Modrinth 格式打包内容（mod 直链匹配 + overrides 回填） ----
	temporaryPath := fmt.Sprintf("%s.%s.nekosolo-tmp", outputPath, newGUID())
	defer func() { _ = os.Remove(temporaryPath) }()
	mrpackPath := temporaryPath + ".mrpack"
	defer func() { _ = os.Remove(mrpackPath) }()
	payloadTemp := temporaryPath + ".payload"
	defer func() { _ = os.Remove(payloadTemp) }()

	emitProgress(progress, "正在打包整合包内容", 0, 1)
	mrpackOptions := modpack.ModpackExportOptions{
		Format:               modpack.FormatModrinth,
		PackName:             options.PackName,
		PackVersion:          options.PackVersion,
		Author:               options.Author,
		UpdateLink:           options.UpdateLink,
		Description:          options.Description,
		IconPngPath:          options.IconPngPath,
		MinecraftVersion:     options.MinecraftVersion,
		LoaderName:           options.LoaderName,
		LoaderVersion:        options.LoaderVersion,
		IncludedPaths:        options.IncludedPaths,
		ResolveModrinthLinks: true,
	}
	mrpackResult, err := modpack.Export(ctx, mrpackOptions, options.ContentDirectory, mrpackPath,
		func(p modpack.ModpackExportProgress) {
			p.Phase = "正在打包整合包内容：" + p.Phase
			emitProgress(progress, p.Phase, p.Current, p.Total)
		})
	if err != nil {
		return result, err
	}
	warnings := mrpackResult.Warnings

	// ---- 第二步：mrpack + manifest + files/ 组装成 v3 载荷 ----
	emitProgress(progress, "正在写入载荷", 0, 1)
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
		UpdateLink:    options.UpdateLink,
	}
	// 图标由 modpack.Export 写进 overrides/icon.png（标准 mrpack 的图标位置），
	// 安装器与导入流程都按该路径展示。
	if options.IconPngPath != "" {
		if _, err := os.Stat(options.IconPngPath); err == nil {
			manifest.IconPath = "overrides/icon.png"
		}
	}

	payloadLength, payloadCRC, err := writePayloadV3(payloadTemp, manifest, mrpackPath, launcherExe)
	if err != nil {
		return result, err
	}

	// ---- 第三步：拼接最终 exe ----
	stubInfo, err := os.Stat(stubPath)
	if err != nil {
		return result, err
	}
	if options.RemoteDistribution {
		// 在线安装包：exe = stub + 远程清单 JSON + v2 尾标（体积只有几 MB）；
		// 载荷 zip（v3 布局）保留在输出目录旁，由作者上传到 PayloadURL。
		payloadPath, payloadSize, err := assembleRemoteInstaller(stubPath, stubInfo.Size(), payloadTemp,
			payloadLength, payloadCRC, manifest, options, temporaryPath, outputPath)
		if err != nil {
			return result, err
		}
		warnings = append(warnings,
			"在线安装包：请把 "+payloadPath+" 上传到 PayloadURL 指向的位置（如 GitHub Releases 资产），玩家安装时将从此地址下载。")
		emitProgress(progress, "完成", 1, 1)

		return modpack.ModpackExportResult{
			OutputPath:       outputPath,
			DeclaredFiles:    mrpackResult.DeclaredFiles,
			OverrideFiles:    mrpackResult.OverrideFiles,
			Warnings:         warnings,
			PayloadPath:      payloadPath,
			PayloadSizeBytes: payloadSize,
		}, nil
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
		DeclaredFiles: mrpackResult.DeclaredFiles,
		OverrideFiles: mrpackResult.OverrideFiles,
		Warnings:      warnings,
	}, nil
}

// writePayloadV3 把 mrpack 全部条目 + manifest.json + files/ 写成 v3 载荷 zip；
// 返回载荷长度与 CRC32。条目逐个流式复制，mrpack 只读不改动。
func writePayloadV3(payloadPath string, manifest Manifest, mrpackPath, launcherExe string) (int64, uint32, error) {
	mrpack, err := zip.OpenReader(mrpackPath)
	if err != nil {
		return 0, 0, fmt.Errorf("读取整合包载荷失败：%w", err)
	}
	defer mrpack.Close()

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
		// 启动器本体 + 便携标记：安装器整体解到安装根目录。
		// 载荷内固定命名 NekoLauncher.exe——安装器按此固定名注册与拉起启动器
		if err := copyFileEntry(archive, "files/NekoLauncher.exe", launcherExe); err != nil {
			return err
		}
		if err := writeTextEntry(archive, "files/portable.flag", ""); err != nil {
			return err
		}
		// mrpack 条目原样复制（modrinth.index.json + overrides/…）
		for _, entry := range mrpack.File {
			if err := copyZipEntry(archive, entry); err != nil {
				return fmt.Errorf("复制 %s 失败：%w", entry.Name, err)
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

// copyZipEntry 从源 zip 复制单个条目到目标 zip（保持压缩方法，流式拷贝数据）。
func copyZipEntry(archive *zip.Writer, source *zip.File) error {
	reader, err := source.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	writer, err := archive.CreateHeader(&zip.FileHeader{
		Name:   source.Name,
		Method: source.Method,
	})
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, reader)
	return err
}

// assembleRemoteInstaller 生成在线安装包：stub + 远程清单 JSON + v2 尾标。
// 载荷 zip（payloadTemp，v3 布局）被移动到 outputPath 同目录下的
// "<包名>-payload.zip"，由作者上传到 GitHub Releases 等托管地址。返回载荷 zip 路径与大小。
func assembleRemoteInstaller(
	stubPath string,
	stubSize int64,
	payloadTemp string,
	payloadLength int64,
	payloadCRC uint32,
	manifest Manifest,
	options SoloExportOptions,
	temporaryPath, outputPath string,
) (string, int64, error) {
	url := strings.TrimSpace(options.PayloadURL)
	if !strings.HasPrefix(url, "https://") {
		return "", 0, fmt.Errorf("在线安装包的载荷下载地址必须是 https:// 开头的 URL")
	}
	remote := RemoteManifest{
		Manifest:     manifest,
		PayloadURL:   url,
		PayloadSize:  payloadLength,
		PayloadCRC32: payloadCRC,
	}
	manifestJSON, err := marshalSoloJSON(remote)
	if err != nil {
		return "", 0, err
	}

	stub, err := os.Open(stubPath)
	if err != nil {
		return "", 0, err
	}
	defer stub.Close()
	output, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", 0, err
	}
	if _, err := io.Copy(output, stub); err != nil {
		output.Close()
		return "", 0, err
	}
	manifestBytes := []byte(manifestJSON)
	manifestOffset := stubSize
	if _, err := output.Write(manifestBytes); err != nil {
		output.Close()
		return "", 0, err
	}
	if err := AppendTrailerV2(output, manifestOffset, int64(len(manifestBytes)), crc32.ChecksumIEEE(manifestBytes)); err != nil {
		output.Close()
		return "", 0, err
	}
	if err := output.Sync(); err != nil {
		output.Close()
		return "", 0, err
	}
	if err := output.Close(); err != nil {
		return "", 0, err
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return "", 0, err
	}

	// 载荷 zip 移动到输出目录旁（扩展名 .zip 便于直接作为 Release 资产上传）
	payloadPath := strings.TrimSuffix(outputPath, ".exe") + "-payload.zip"
	if err := os.Rename(payloadTemp, payloadPath); err != nil {
		return "", 0, err
	}
	return payloadPath, payloadLength, nil
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
	if err := AppendTrailerV3(output, stubSize, payloadLength, payloadCRC); err != nil {
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
