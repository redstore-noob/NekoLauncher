// 安装器模板（stub）的动态下载兜底：
// 本地四处都找不到模板时（从 Release 直接下载启动器的用户），从启动器
// 自己的 GitHub Releases 拉取随版本发布的 NekoSolo.Installer.exe 资产，
// 落到存储目录 tools/NekoSolo/，此后 FindStubTemplate 即可命中。
package solo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/update"
)

// stubAssetName Releases 里安装器模板的资产名（CI 打包产物，与本地候选名一致）。
const stubAssetName = "NekoSolo.Installer.exe"

// stubDownloadTimeout 下载模板的超时：模板约 10MB，国内到 GitHub 放宽到 5 分钟。
const stubDownloadTimeout = 5 * time.Minute

// githubRelease GitHub Releases 列表的最小模型（只取需要的字段）。
type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

// DownloadStubTemplate 在线获取安装器模板：列出启动器仓库的最新 release，
// 找到模板资产后下载到 <存储目录>/tools/NekoSolo/。返回落盘路径。
// 已存在同名文件时直接返回（调用方先经 FindStubTemplate 确认缺失才会走到这里，
// 但文件存在而损坏的极端情况由导出时的拼装失败自然暴露）。
func DownloadStubTemplate(ctx context.Context) (string, error) {
	release, err := latestStubRelease(ctx)
	if err != nil {
		return "", err
	}
	var assetURL string
	var assetSize int64
	for _, asset := range release.Assets {
		if asset.Name == stubAssetName {
			assetURL, assetSize = asset.URL, asset.Size
			break
		}
	}
	if assetURL == "" {
		return "", fmt.Errorf(
			"启动器 %s 的 Release 里没有 %s 资产；请到 %s 手动下载并放到启动器目录的 tools/NekoSolo/ 下",
			release.TagName, stubAssetName, update.ReleasePageURL)
	}

	destination := filepath.Join(config.StorageDirectory(), "tools", "NekoSolo", stubAssetName)
	if err := downloadToFile(ctx, assetURL, destination, assetSize); err != nil {
		return "", err
	}
	return destination, nil
}

// latestStubRelease 取最新的非草稿 release（预发布也可用：模板与启动器版本
// 未必同步，宁可给旧一点的模板也不给失败）。
func latestStubRelease(ctx context.Context) (*githubRelease, error) {
	api := "https://api.github.com/repos/" + update.ReleasesRepo + "/releases?per_page=10"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("查询启动器 Release 失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("查询启动器 Release 失败：HTTP %d", resp.StatusCode)
	}
	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("解析 Release 列表失败：%w", err)
	}
	for i := range releases {
		if releases[i].Draft {
			continue
		}
		return &releases[i], nil
	}
	return nil, fmt.Errorf("启动器仓库还没有可用的 Release")
}

// downloadToFile 流式下载到 destination（先写临时文件再改名），校验长度。
func downloadToFile(ctx context.Context, url, destination string, expectedSize int64) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: stubDownloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载安装器模板失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载安装器模板失败：HTTP %d", resp.StatusCode)
	}

	temp := destination + ".tmp"
	file, err := os.Create(temp)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("下载安装器模板失败：%w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(temp)
		return closeErr
	}
	if expectedSize > 0 && written != expectedSize {
		_ = os.Remove(temp)
		return fmt.Errorf("下载不完整：得到 %d 字节，应为 %d 字节", written, expectedSize)
	}
	// 模板是 PE 可执行文件，起手就是 "MZ"：顺手做个最基础的完整性检查
	if written < 2 {
		_ = os.Remove(temp)
		return fmt.Errorf("下载的模板文件为空")
	}
	if err := os.Rename(temp, destination); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}
