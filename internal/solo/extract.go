// NekoSolo 安装包（v1/v2/v3）的整合包提取与导入：
//   - ExtractSoloPack 把 exe 载荷里的标准 Modrinth 整合包（modrinth.index.json
//     + overrides/）另存为 .mrpack，可被任意支持 Modrinth 格式的启动器导入；
//   - ImportSoloExe 把 exe 当整合包"吃"进本启动器：载荷转存为临时 .mrpack，
//     由前端走既有的整合包导入流程（建实例 → 装 Loader → 装 mod）。
package solo

import (
	"archive/zip"
	"bytes"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"

	"nekolauncher/internal/config"
)

// soloImportDirectory NekoSolo exe 导入时临时 .mrpack 的落盘目录（启动器存储目录下）。
func soloImportDirectory() string {
	return filepath.Join(config.StorageDirectory(), "solo-import")
}

// isModrinthEntry 判断载荷条目是否属于 Modrinth 整合包本体（相对 v3 载荷而言，
// 即除 manifest.json 与 files/ 之外的全部条目——v3 载荷除这三类外不再有别的；
// 旧 v1/v2 载荷的 minecraft/、jre/ 前缀会被过滤掉）。
func isModrinthEntry(name string) bool {
	if name == "manifest.json" || strings.HasPrefix(name, "files/") {
		return false
	}
	return true
}

// openSoloPayload 打开 NekoSolo 安装包并返回载荷 zip 读取器。v2 在线安装包的
// 载荷在外部地址，这里返回错误提示走安装器。调用方负责 Close。
func openSoloPayload(exePath string) (*zip.Reader, func() error, error) {
	trailer, err := ReadTrailerFromFile(exePath)
	if err != nil {
		return nil, nil, err
	}
	if trailer.V2 {
		return nil, nil, fmt.Errorf("该 NekoSolo 在线安装包的整合包内容托管在远程地址，请运行安装器获取")
	}
	reader, err := OpenPayloadRange(exePath, trailer)
	if err != nil {
		return nil, nil, err
	}
	data, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil {
		return nil, nil, err
	}
	if closeErr != nil {
		return nil, nil, closeErr
	}
	if crc32.ChecksumIEEE(data) != trailer.CRC32 {
		return nil, nil, fmt.Errorf("载荷 CRC 校验失败（安装包可能在传输中损坏）")
	}
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, fmt.Errorf("载荷不是有效 zip（安装包可能已损坏）：%w", err)
	}
	return zipReader, func() error { return nil }, nil
}

// ExtractSoloPack 从 NekoSolo 安装包提取标准 Modrinth 整合包到 outputPath（.mrpack）。
// 提取结果只含 modrinth.index.json 与 overrides/（v1/v2 旧载荷不支持提取）。
func ExtractSoloPack(exePath, outputPath string) error {
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return fmt.Errorf("输出路径不能为空")
	}
	if !strings.EqualFold(filepath.Ext(outputPath), ".mrpack") {
		return fmt.Errorf("输出必须是 .mrpack 文件")
	}
	reader, closePayload, err := openSoloPayload(exePath)
	if err != nil {
		return err
	}
	defer closePayload()

	hasIndex := false
	for _, file := range reader.File {
		if file.Name == "modrinth.index.json" {
			hasIndex = true
			break
		}
	}
	if !hasIndex {
		return fmt.Errorf("该安装包是旧版 NekoSolo 格式（内嵌版本目录），不支持提取为 .mrpack；请用新版启动器重新导出")
	}

	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(output)
	extractErr := func() error {
		for _, file := range reader.File {
			if !isModrinthEntry(file.Name) {
				continue
			}
			if err := copyZipEntry(archive, file); err != nil {
				return fmt.Errorf("复制 %s 失败：%w", file.Name, err)
			}
		}
		return archive.Close()
	}()
	closeErr := output.Close()
	if extractErr != nil {
		_ = os.Remove(outputPath)
		return extractErr
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

// ImportSoloExe 把 NekoSolo 安装包转存为临时 .mrpack（存到启动器存储目录），
// 返回该文件路径，供前端走既有的整合包导入流程。调用方负责在使用完后删除。
func ImportSoloExe(exePath string) (string, error) {
	reader, closePayload, err := openSoloPayload(exePath)
	if err != nil {
		return "", err
	}
	defer closePayload()

	if err := os.MkdirAll(soloImportDirectory(), 0o755); err != nil {
		return "", err
	}
	mrpackPath := filepath.Join(soloImportDirectory(), fmt.Sprintf("solo-import-%s.mrpack", newGUID()))
	output, err := os.OpenFile(mrpackPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	archive := zip.NewWriter(output)
	importErr := func() error {
		for _, file := range reader.File {
			if !isModrinthEntry(file.Name) {
				continue
			}
			if err := copyZipEntry(archive, file); err != nil {
				return fmt.Errorf("复制 %s 失败：%w", file.Name, err)
			}
		}
		return archive.Close()
	}()
	closeErr := output.Close()
	if importErr != nil {
		_ = os.Remove(mrpackPath)
		return "", importErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return mrpackPath, nil
}
