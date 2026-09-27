package update

// 更新包下载：把选中的资产下到临时目录，校验后再交给 Apply。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekolauncher/internal/download"
)

// downloadDirectoryName 更新包的落点目录（位于系统临时目录下）。
const downloadDirectoryName = "nekolauncher-update"

// Download 下载版本资产到临时目录，返回落盘路径。
// 复用 download.DownloadFileToPath：断点续传、限速、原子落地都已在那里实现。
func Download(ctx context.Context, asset Asset, progress download.ProgressBytes) (string, error) {
	url := strings.TrimSpace(asset.URL)
	if url == "" {
		return "", fmt.Errorf("下载地址为空")
	}

	name := sanitizeAssetName(asset.Name)
	if name == "" {
		name = "NekoLauncher-update.exe"
	}
	targetDirectory := filepath.Join(os.TempDir(), downloadDirectoryName)
	if err := os.MkdirAll(targetDirectory, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(targetDirectory, name)

	if err := download.DownloadFileToPath(ctx, url, target, progress); err != nil {
		return "", err
	}
	if err := validateReplacement(target); err != nil {
		_ = os.Remove(target)

		return "", fmt.Errorf("下载的文件不可用：%w", err)
	}
	// 资产声明了大小就核对一遍：截断的下载换上去就等于把启动器弄坏
	if asset.Size > 0 {
		if info, err := os.Stat(target); err == nil && info.Size() != asset.Size {
			_ = os.Remove(target)

			return "", fmt.Errorf("下载不完整（期望 %d 字节，实际 %d 字节）", asset.Size, info.Size())
		}
	}

	return target, nil
}

// sanitizeAssetName 只保留文件名部分，挡掉资产名里带路径分隔符的情况
// （GitHub 资产名由发布者填写，启动器不该把它当成路径用）。
func sanitizeAssetName(name string) string {
	value := strings.TrimSpace(name)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\\", "/")
	if index := strings.LastIndex(value, "/"); index >= 0 {
		value = value[index+1:]
	}
	if value == "." || value == ".." {
		return ""
	}

	return value
}
