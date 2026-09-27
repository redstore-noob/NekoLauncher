package download

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/info"
)

// DownloadSource 一个完整的下载源定义，包含所有需要替换的基础 URL。
type DownloadSource struct {
	// Name 显示名称，如 "Official" 或 "BMCL"。
	Name string
	// LauncherMeta 版本清单完整 URL（version_manifest_v2.json）。
	LauncherMeta string
	// Meta piston-meta / launchermeta 基础域名（无尾部斜杠）。
	Meta string
	// Libraries libraries 基础域名（无尾部斜杠）。
	Libraries string
	// Resources 资源文件基础域名（无尾部斜杠）。
	Resources string
	// Maven Maven 仓库基础域名（用于 Forge/NeoForge 的 Maven 坐标解析，无尾部斜杠）。
	Maven string
}

// DownloadSources 内置下载源定义。
var DownloadSources = struct {
	Official DownloadSource
	Bmcl     DownloadSource
}{
	Official: DownloadSource{
		Name:         "Official",
		LauncherMeta: "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json",
		Meta:         "https://piston-meta.mojang.com",
		Libraries:    "https://libraries.minecraft.net",
		Resources:    "https://resources.download.minecraft.net",
		Maven:        "https://libraries.minecraft.net",
	},
	Bmcl: DownloadSource{
		Name:         "BMCL",
		LauncherMeta: "https://bmclapi2.bangbang93.com/mc/game/version_manifest_v2.json",
		Meta:         "https://bmclapi2.bangbang93.com",
		Libraries:    "https://bmclapi2.bangbang93.com/maven",
		Resources:    "https://bmclapi2.bangbang93.com/assets",
		Maven:        "https://bmclapi2.bangbang93.com/maven",
	},
}

// AllDownloadSources 所有可用下载源。
var AllDownloadSources = []DownloadSource{DownloadSources.Official, DownloadSources.Bmcl}

// downloadSourceProvider 下载源提供器。Core 侧的全局单例，前端通过
// SetActive / SetFallback 控制源选择，所有下载逻辑通过本类获取 URL。
type downloadSourceProvider struct {
	mu       sync.RWMutex
	active   DownloadSource
	fallback *DownloadSource
}

// SourceProvider 下载源提供器单例（对应 C# 静态类 DownloadSourceProvider）。
var SourceProvider = newDownloadSourceProvider()

func newDownloadSourceProvider() *downloadSourceProvider {
	bmcl := DownloadSources.Bmcl
	return &downloadSourceProvider{active: bmcl, fallback: &bmcl}
}

// Active 当前活跃下载源。默认 BMCL（国内网络友好）。
func (p *downloadSourceProvider) Active() DownloadSource {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.active
}

// SetActive 设置活跃下载源。
func (p *downloadSourceProvider) SetActive(source DownloadSource) {
	p.mu.Lock()
	p.active = source
	p.mu.Unlock()
}

// Fallback 自动回退源。当 Active 请求失败时自动尝试；nil 则不回退。
func (p *downloadSourceProvider) Fallback() *DownloadSource {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.fallback == nil {
		return nil
	}
	copySource := *p.fallback
	return &copySource
}

// SetFallback 设置自动回退源（可传 nil 禁用）。
func (p *downloadSourceProvider) SetFallback(source *DownloadSource) {
	p.mu.Lock()
	if source == nil {
		p.fallback = nil
	} else {
		copySource := *source
		p.fallback = &copySource
	}
	p.mu.Unlock()
}

// Resolve 将 Official 源的 URL 替换为活跃源对应地址。
// 如果 Active 就是 Official 则原样返回。
func (p *downloadSourceProvider) Resolve(officialURL string) string {
	if strings.TrimSpace(officialURL) == "" || p.Active().Name == DownloadSources.Official.Name {
		return officialURL
	}
	return replaceBaseURL(officialURL, p.Active())
}

// ResolveFallback 获取 Fallback 源对应的 URL。无 Fallback 时返回 nil。
func (p *downloadSourceProvider) ResolveFallback(officialURL string) *string {
	fb := p.Fallback()
	if fb == nil || strings.TrimSpace(officialURL) == "" {
		return nil
	}
	replaced := replaceBaseURL(officialURL, *fb)
	return &replaced
}

// ---- 带回退的 HTTP 请求 ----

// longDownloadClient 长时下载/请求共用的独立 HTTP 客户端。
// 不设整体超时（对应 C# Timeout.InfiniteTimeSpan）：超时预算由各调用方
// 通过 context 精确控制；绝不复用 tools.SharedHTTPClient 的 15s 超时。
// 包装器 base 留空（RoundTrip 时解析 DefaultTransport），代理设置替换
// 全局 Transport 后无需重建客户端即生效。
var longDownloadClient = createLongDownloadClient()

func createLongDownloadClient() *http.Client {
	return &http.Client{
		Timeout: 0, // 无限超时，由 context 控制
		Transport: &userAgentRoundTripper{
			base: nil,
			ua:   "NekoLauncher/" + info.Version(),
		},
	}
}

type userAgentRoundTripper struct {
	base http.RoundTripper
	ua   string
}

func (t *userAgentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	r := req.Clone(req.Context())
	r.Header.Set("User-Agent", t.ua)
	return base.RoundTrip(r)
}

// defaultRequestTimeout 默认请求超时：调用方未显式给超时时使用（与旧 HttpClient.Timeout 一致）。
const defaultRequestTimeout = 30 * time.Second

// withTimeout 按调用方超时创建链接取消 ctx；未显式给超时时套用默认 30 秒，
// 保证关掉客户端整体超时后无超时调用仍不会无限挂起。
func withTimeout(ctx context.Context, timeout *time.Duration) (context.Context, context.CancelFunc) {
	effective := defaultRequestTimeout
	if timeout != nil {
		effective = *timeout
	}
	return context.WithTimeout(ctx, effective)
}

// GetString GET 请求，自动应用活跃源；失败时回退到回退源。
func (p *downloadSourceProvider) GetString(ctx context.Context, officialURL string, timeout *time.Duration) (string, error) {
	primaryURL, err := validateHTTPSURL(p.Resolve(officialURL))
	if err != nil {
		return "", err
	}
	fallbackURL := validateHTTPSURLOrNil(p.ResolveFallback(officialURL))

	// 用户取消判定基于外层 ctx：内层 ctx 携带请求超时预算，
	// 主源超时属于"应回退"的失败而非用户取消——否则最常见的
	// "主源挂起直至超时"场景永远不会触发回退。
	parentCtx := ctx
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", primaryURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := longDownloadClient.Do(req)
	if err == nil {
		body, readErr := readAllString(resp)
		if readErr == nil {
			return body, nil
		}
		err = readErr
	}

	if isFallbackEligible(fallbackURL, primaryURL, parentCtx) {
		// 回退请求给独立的完整超时预算：主源耗掉的等待时间不该挤占回退。
		// 但必须挂在调用方的 ctx 上（不是 WithoutCancel）——否则回退期间
		// 用户点取消/关窗口都中断不了，只能等超时跑满。
		fbCtx, fbCancel := withTimeout(parentCtx, timeout)
		defer fbCancel()
		fbReq, fbErr := http.NewRequestWithContext(fbCtx, "GET", *fallbackURL, nil)
		if fbErr != nil {
			return "", fbErr
		}
		fbResp, fbErr := longDownloadClient.Do(fbReq)
		if fbErr != nil {
			return "", fbErr
		}
		return readAllString(fbResp)
	}
	return "", err
}

// GetBytes GET 请求（字节），自动应用活跃源；失败时回退到回退源。
func (p *downloadSourceProvider) GetBytes(ctx context.Context, officialURL string, timeout *time.Duration) ([]byte, error) {
	primaryURL, err := validateHTTPSURL(p.Resolve(officialURL))
	if err != nil {
		return nil, err
	}
	fallbackURL := validateHTTPSURLOrNil(p.ResolveFallback(officialURL))

	// 与 GetString 相同：取消判定用外层 ctx，避免主源超时被当成用户取消
	parentCtx := ctx
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", primaryURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := longDownloadClient.Do(req)
	if err == nil {
		body, readErr := readAllBytes(resp)
		if readErr == nil {
			return body, nil
		}
		err = readErr
	}

	if isFallbackEligible(fallbackURL, primaryURL, parentCtx) {
		fbCtx, fbCancel := withTimeout(parentCtx, timeout)
		defer fbCancel()
		fbReq, fbErr := http.NewRequestWithContext(fbCtx, "GET", *fallbackURL, nil)
		if fbErr != nil {
			return nil, fbErr
		}
		fbResp, fbErr := longDownloadClient.Do(fbReq)
		if fbErr != nil {
			return nil, fbErr
		}
		return readAllBytes(fbResp)
	}
	return nil, err
}

// isFallbackEligible 回退是否值得尝试：用户未取消、存在回退源，且回退地址与主地址不同
// （回退源与活跃源解析出同一 URL 时重试只是把超时时间翻倍）。
func isFallbackEligible(fallbackURL *string, primaryURL string, ctx context.Context) bool {
	return ctx.Err() == nil &&
		fallbackURL != nil &&
		!strings.EqualFold(*fallbackURL, primaryURL)
}

// MeasureLatency 测量一个下载源的响应延迟：HEAD 版本清单地址，返回往返毫秒；
// 失败或超时（5 秒）返回 -1。供设置页"测速"功能使用。
func (p *downloadSourceProvider) MeasureLatency(ctx context.Context, source DownloadSource) int {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, "HEAD", source.LauncherMeta, nil)
	if err != nil {
		return -1
	}
	resp, err := longDownloadClient.Do(req)
	if err != nil {
		return -1
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return -1
	}
	elapsed := time.Since(started).Milliseconds()
	if elapsed < 1 {
		return 1
	}
	return int(elapsed)
}

// ---- URL 处理 ----

// validateHTTPSURL 所有出站请求统一强制 HTTPS（与各安装器的直连下载保持同一安全基线）。
func validateHTTPSURL(rawURL string) (string, error) {
	if !strings.HasPrefix(rawURL, "https://") && !strings.HasPrefix(rawURL, "HTTPS://") {
		return "", fmt.Errorf("下载地址不是有效的 HTTPS URL：%s", rawURL)
	}
	return rawURL, nil
}

func validateHTTPSURLOrNil(rawURL *string) *string {
	if rawURL == nil {
		return nil
	}
	if _, err := validateHTTPSURL(*rawURL); err != nil {
		return nil
	}
	return rawURL
}

// replaceBaseURL 将一个指向 Official 各域名的 URL 替换为目标源对应域名。
// 识别的域名：piston-meta.mojang.com, piston-data.mojang.com,
//
//	launchermeta.mojang.com, libraries.minecraft.net,
//	resources.download.minecraft.net, files.minecraftforge.net,
//	maven.minecraftforge.net, maven.neoforged.net,
//	meta.fabricmc.net, maven.fabricmc.net。
//
// Quilt 域名（meta/maven.quiltmc.org）无镜像，原样直连。
func replaceBaseURL(rawURL string, target DownloadSource) string {
	lower := strings.ToLower(rawURL)

	// Quilt：BMCLAPI 未镜像 Quilt（实测 meta 与 maven 路径均 404），改写只会产生
	// 一次必然失败的请求再回退官方，因此 Quilt 域名一律直连官方源。
	if strings.Contains(lower, "meta.quiltmc.org") || strings.Contains(lower, "maven.quiltmc.org") {
		return rawURL
	}

	// 先处理有特殊路径映射的域名
	if strings.Contains(lower, "libraries.minecraft.net") {
		return strings.Replace(rawURL, "https://libraries.minecraft.net", target.Libraries, 1)
	}
	if strings.Contains(lower, "resources.download.minecraft.net") {
		return strings.Replace(rawURL, "https://resources.download.minecraft.net", target.Resources, 1)
	}
	// Forge 官网（promotions_slim.json 版本列表）：BMCL 镜像为 {Maven}/net/minecraftforge/...（实测可用）
	if strings.Contains(lower, "files.minecraftforge.net") {
		return strings.Replace(rawURL, "https://files.minecraftforge.net", target.Maven, 1)
	}
	// Forge Maven
	if strings.Contains(lower, "maven.minecraftforge.net") {
		return strings.Replace(rawURL, "https://maven.minecraftforge.net", target.Maven, 1)
	}
	// NeoForge Maven：BMCL 镜像会去掉 /releases/ 路径段（实测 maven/releases/... 返回 404），
	// 必须先处理带路径的特例，再回退到域名级替换。
	if strings.Contains(lower, "maven.neoforged.net/releases/net/neoforged") {
		return strings.Replace(rawURL,
			"https://maven.neoforged.net/releases/net/neoforged",
			target.Maven+"/net/neoforged", 1)
	}
	if strings.Contains(lower, "maven.neoforged.net") {
		return strings.Replace(rawURL, "https://maven.neoforged.net", target.Maven, 1)
	}
	// Fabric meta（版本列表与 profile JSON 的镜像为 {Meta}/fabric-meta）
	if strings.Contains(lower, "meta.fabricmc.net") {
		return strings.Replace(rawURL, "https://meta.fabricmc.net", target.Meta+"/fabric-meta", 1)
	}
	// Fabric Maven（库文件）
	if strings.Contains(lower, "maven.fabricmc.net") {
		return strings.Replace(rawURL, "https://maven.fabricmc.net", target.Maven, 1)
	}
	// 通用 Meta 域名
	if strings.Contains(lower, "piston-meta.mojang.com") {
		return strings.Replace(rawURL, "https://piston-meta.mojang.com", target.Meta, 1)
	}
	if strings.Contains(lower, "piston-data.mojang.com") {
		return strings.Replace(rawURL, "https://piston-data.mojang.com", target.Meta, 1)
	}
	if strings.Contains(lower, "launchermeta.mojang.com") {
		return strings.Replace(rawURL, "https://launchermeta.mojang.com", target.Meta, 1)
	}
	return rawURL
}
