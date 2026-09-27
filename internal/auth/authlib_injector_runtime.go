package auth

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nekolauncher/internal/logs"
)

// 注入器 latest.json 的官方源与 BMCLAPI 镜像。
const (
	officialLatestJsonUrl = "https://authlib-injector.yushi.moe/artifact/latest.json"
	mirrorLatestJsonUrl   = "https://bmclapi2.bangbang93.com/mirrors/authlib-injector/artifact/latest.json"
)

// injectorClient 注入器下载专用客户端：2 分钟整体超时（jar 体积小但网络可能慢），
// 统一 User-Agent。
var injectorClient = &http.Client{Timeout: 2 * time.Minute}

// AuthlibArtifact latest.json 的产物描述。
//
// 官方现行格式（build_number / version / download_url / checksums{sha256,sha1}）：
//
//	{"build_number":56,"version":"1.2.8","download_url":"…/authlib-injector-1.2.8.jar",
//	 "checksums":{"sha256":"…"}}
//
// 早期格式把 url / sha256 / sha1 放在顶层。两种都认（取值一律走下面的访问器，
// 新格式优先）——只按旧格式解析会让 Url 恒为空，两个下载源全部失败，
// 皮肤站账号在全新安装上根本启动不了。
type AuthlibArtifact struct {
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
	Checksums   struct {
		Sha256 string `json:"sha256"`
		Sha1   string `json:"sha1"`
	} `json:"checksums"`

	// 旧格式字段（保留兼容）
	Url    string `json:"url"`
	Sha256 string `json:"sha256"`
	Sha1   string `json:"sha1"`
}

// ArtifactURL 产物下载地址（新格式优先）。
func (a *AuthlibArtifact) ArtifactURL() string {
	if value := strings.TrimSpace(a.DownloadURL); value != "" {
		return value
	}

	return strings.TrimSpace(a.Url)
}

// ArtifactSha256 产物 SHA-256（新格式优先）。
func (a *AuthlibArtifact) ArtifactSha256() string {
	if value := strings.TrimSpace(a.Checksums.Sha256); value != "" {
		return value
	}

	return strings.TrimSpace(a.Sha256)
}

// ArtifactSha1 产物 SHA-1（新格式优先）。
func (a *AuthlibArtifact) ArtifactSha1() string {
	if value := strings.TrimSpace(a.Checksums.Sha1); value != "" {
		return value
	}

	return strings.TrimSpace(a.Sha1)
}

// EnsureInjector 确保 minecraftDirectory 下存在可用的 authlib-injector 注入器 jar，
// 返回其绝对路径。官方源优先，BMCLAPI 镜像兜底；已缓存且哈希一致时直接复用，
// 全源失败时回退到任意已存在的旧版 jar（外置登录多数场景下仍可用）。
func EnsureInjector(ctx context.Context, minecraftDirectory string, log func(string)) (string, error) {
	if strings.TrimSpace(minecraftDirectory) == "" {
		return "", errors.New("minecraftDirectory 不能为空")
	}
	installDirectory := filepath.Join(minecraftDirectory, "authlib-injector")
	if err := os.MkdirAll(installDirectory, 0o755); err != nil {
		return "", err
	}

	var lastError error
	for _, latestJsonUrl := range []string{officialLatestJsonUrl, mirrorLatestJsonUrl} {
		result, err := ensureFromSource(ctx, latestJsonUrl, installDirectory, log)
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		lastError = err
		logLog(log, fmt.Sprintf("注入器下载源 %s 不可用：%v", hostOf(latestJsonUrl), err))
	}

	// 全部下载源失败：回退到本地任意旧版注入器（外置登录多数场景下仍可用）
	entries, _ := os.ReadDir(installDirectory)
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasPrefix(name, "authlib-injector-") && strings.HasSuffix(name, ".jar") {
			logLog(log, fmt.Sprintf("警告：无法获取最新注入器，使用本地已有的 %s。", name))
			return filepath.Join(installDirectory, name), nil
		}
	}

	return "", fmt.Errorf("获取 authlib-injector 注入器失败：%v", lastError)
}

func ensureFromSource(
	ctx context.Context,
	latestJsonUrl, installDirectory string,
	log func(string),
) (string, error) {
	artifact, err := fetchLatestArtifact(ctx, latestJsonUrl)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(artifact.ArtifactURL()) == "" || strings.TrimSpace(artifact.Version) == "" {
		return "", fmt.Errorf("latest.json 缺少下载地址或 version 字段。")
	}

	targetPath := filepath.Join(installDirectory, fmt.Sprintf("authlib-injector-%s.jar", artifact.Version))

	if _, statErr := os.Stat(targetPath); statErr == nil {
		if !hasKnownHash(artifact) {
			logLog(log, fmt.Sprintf("authlib-injector %s 已就绪。", artifact.Version))
			return targetPath, nil
		}
		if matches, hashErr := matchesHash(targetPath, artifact); hashErr == nil && matches {
			logLog(log, fmt.Sprintf("authlib-injector %s 已就绪。", artifact.Version))
			return targetPath, nil
		}
		// 缓存损坏（校验失败）：删除后重新下载
		logLog(log, "本地注入器缓存校验失败，正在重新下载。")
		_ = os.Remove(targetPath)
	}

	downloadUrl := resolveInjectorDownloadUrl(latestJsonUrl, artifact.ArtifactURL())
	logLog(log, fmt.Sprintf("正在下载 authlib-injector %s…", artifact.Version))
	if err := downloadInjectorFile(ctx, downloadUrl, targetPath); err != nil {
		return "", err
	}

	if hasKnownHash(artifact) {
		matches, hashErr := matchesHash(targetPath, artifact)
		if hashErr != nil || !matches {
			_ = os.Remove(targetPath)
			return "", fmt.Errorf("注入器哈希校验失败。")
		}
	}

	logLog(log, "authlib-injector 下载完成。")
	return targetPath, nil
}

func fetchLatestArtifact(ctx context.Context, latestJsonUrl string) (*AuthlibArtifact, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestJsonUrl, nil)
	if err != nil {
		return nil, err
	}
	response, err := injectorClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("latest.json 请求失败（HTTP %d）", response.StatusCode)
	}
	var artifact AuthlibArtifact
	if err := json.NewDecoder(response.Body).Decode(&artifact); err != nil {
		return nil, fmt.Errorf("latest.json 解析结果为空：%w", err)
	}
	return &artifact, nil
}

// resolveInjectorDownloadUrl 把官方源的产物地址映射到对应镜像（BMCLAPI 的
// authlib-injector 镜像与官方目录结构一致，仅替换主机与前缀）。
//
// 官方 download_url 形如 https://authlib-injector.yushi.moe/artifact/56/xxx.jar，
// 镜像目录是 https://bmclapi2.bangbang93.com/mirrors/authlib-injector/，
// 所以正确的拼接是「镜像目录 + artifact/56/xxx.jar」——早先按 LastIndex 截出来的
// mirrorBase 已经带上了 /artifact，再拼原始 path 会得到 /artifact/artifact/56/…。
func resolveInjectorDownloadUrl(latestJsonUrl, artifactUrl string) string {
	if latestJsonUrl != officialLatestJsonUrl ||
		!(strings.HasPrefix(artifactUrl, "http://") || strings.HasPrefix(artifactUrl, "https://")) {
		return artifactUrl
	}

	// 镜像根（latest.json 所在目录的上一级）：…/mirrors/authlib-injector/
	mirrorRoot := mirrorLatestJsonUrl[:strings.LastIndex(mirrorLatestJsonUrl, "/")+1]
	mirrorRoot = mirrorRoot[:strings.LastIndex(strings.TrimSuffix(mirrorRoot, "/"), "/")+1]

	path := artifactUrl
	if index := strings.Index(path[8:], "/"); index >= 0 {
		path = path[8+index+1:]
	} else {
		return artifactUrl
	}

	return mirrorRoot + path
}

func hasKnownHash(artifact *AuthlibArtifact) bool {
	return artifact.ArtifactSha256() != "" || artifact.ArtifactSha1() != ""
}

func matchesHash(filePath string, artifact *AuthlibArtifact) (bool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, err
	}
	if expected := artifact.ArtifactSha256(); expected != "" {
		sum := sha256.Sum256(data)
		return hashMatches(hex.EncodeToString(sum[:]), expected), nil
	}
	if expected := artifact.ArtifactSha1(); expected != "" {
		sum := sha1.Sum(data)
		return hashMatches(hex.EncodeToString(sum[:]), expected), nil
	}
	return false, nil
}

func hashMatches(actual, expected string) bool {
	return strings.EqualFold(actual, strings.TrimSpace(expected))
}

// downloadInjectorFile 流式下载到 .download 临时文件后原子替换。
func downloadInjectorFile(ctx context.Context, url, targetPath string) error {
	temporaryPath := targetPath + ".download"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := injectorClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("下载失败（HTTP %d）", response.StatusCode)
	}

	target, err := os.Create(temporaryPath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, response.Body)
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(temporaryPath)
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}

	return os.Rename(temporaryPath, targetPath)
}

func logLog(log func(string), message string) {
	if log != nil {
		log(message)
	}
	// 与 C# 行为一致：注入器下载日志只进启动日志回调，不额外写文件
	logs.Write("LAUNCH", message)
}
