package download

// CurseForge CDN 鉴权与失败翻译。
//
// 背景（官方 2026-06-10 公告《Introducing API Key Authentication for CurseForge
// File Downloads》）：CDN 直链 https://edge.forgecdn.net/files/{id}/{subId}/{name}
// 开始支持 x-api-key 请求头（或 ?api-key= 查询参数）；当时可选，2026-07-16 起强制，
// 不带 Key 的请求会返回 401。公告同时要求各启动器"优雅处理 401，给出可执行提示"。
//
// 本启动器此前只在问 API（api.curseforge.com）时带 Key，真正去 CDN 取文件的那一步
// 只发了 Range —— 也就是"解析地址时带了 Key、下载时没带"。这里补上，并把
// CurseForge 的鉴权/下架类失败翻译成人能看懂的提示（裸 401/404 对用户毫无指导意义）。

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"nekolauncher/internal/download/curseforge"
)

// CurseForgeCDNKeyHook 提供当前生效的 CurseForge API Key。
//
// 为什么用钩子而不是直接读配置：内置 Key 是编译期变量，由
// `-ldflags -X nekolauncher/internal/bindings.builtinCurseForgeAPIKey=...`
// 写进 bindings 包，download 包读不到它。由 bindings 在启动时把
// effectiveCurseForgeAPIKey 挂上来，两条来源（用户配置 / 内置）才都能生效。
var CurseForgeCDNKeyHook func() string

// curseForgeCDNKey 当前生效的 CurseForge API Key（未配置或未接线时为空串）。
func curseForgeCDNKey() string {
	if CurseForgeCDNKeyHook == nil {
		return ""
	}
	return strings.TrimSpace(CurseForgeCDNKeyHook())
}

// isCurseForgeCDNHost 该地址是否指向 CurseForge CDN（forgecdn.net 系：
// edge / mediafilez 等同族主机）。
func isCurseForgeCDNHost(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "forgecdn.net" || strings.HasSuffix(host, ".forgecdn.net")
}

// isCurseForgePublicDownloadURL 是否为已下线的 CurseForge 公开下载端点
// （www.curseforge.com/api/v1/mods/{id}/files/{fileID}/download）。
// 该端点现被 Cloudflare 拦截，未配 Key 时只会返回 404/403。
func isCurseForgePublicDownloadURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	if !strings.EqualFold(parsed.Hostname(), "www.curseforge.com") {
		return false
	}
	return strings.Contains(parsed.Path, "/api/v1/mods/") &&
		strings.HasSuffix(strings.ToLower(parsed.Path), "/download")
}

// isCurseForgeDownloadURL 是否为 CurseForge 的下载地址（CDN 直链或公开端点）。
func isCurseForgeDownloadURL(rawURL string) bool {
	return isCurseForgeCDNHost(rawURL) || isCurseForgePublicDownloadURL(rawURL)
}

// applyCurseForgeCDNAuth 给 CurseForge CDN 请求补上 API Key。
// 只对 forgecdn.net 系主机加头：把 Key 发给无关主机是没必要的凭据外泄。
func applyCurseForgeCDNAuth(req *http.Request) {
	if req == nil || req.URL == nil || !isCurseForgeCDNHost(req.URL.String()) {
		return
	}
	if key := curseForgeCDNKey(); key != "" {
		req.Header.Set("x-api-key", key)
	}
}

// curseForgeKeyHint 统一的 Key 引导文案（复用 curseforge 包里的常量，
// 保证与设置页/搜索页展示的说明一致）。
func curseForgeKeyHint() string {
	return curseforge.APIKeyHint
}

// curseForgeDownloadError 把 CurseForge 下载失败翻译成可执行的提示。
// 返回 nil 表示这条错误不需要特殊解释，交由调用方原有逻辑处理。
//
// 必须放在"双域名兜底"之后调用：edge 的 404 先换 mediafilez 重试，
// 域名都试过仍然失败才是真的要向用户解释。
func curseForgeDownloadError(err error, rawURL string) error {
	if err == nil || !isCurseForgeDownloadURL(rawURL) {
		return nil
	}
	keyConfigured := curseForgeCDNKey() != ""

	switch {
	case isHTTPStatus(err, http.StatusUnauthorized):
		// 公告里点名的场景：CDN 强制鉴权后没带/带错 Key 都是 401。
		if !keyConfigured {
			return fmt.Errorf("CurseForge 下载被拒（401）：CDN 直链现已要求鉴权，%s", curseForgeKeyHint())
		}
		return fmt.Errorf("CurseForge 下载被拒（401）：当前 API Key 无效或已失效，%s", curseForgeKeyHint())

	case isHTTPStatus(err, http.StatusForbidden):
		if !keyConfigured {
			return fmt.Errorf("CurseForge 下载被拒（403）：%s", curseForgeKeyHint())
		}
		// 带了 Key 仍然 403：多半是作者关闭了第三方分发，而不是我们的 Key 有问题。
		return fmt.Errorf("CurseForge 下载被拒（403）：作者可能已禁止第三方分发，或该 Key 无权访问此文件。")

	case isHTTPStatus(err, http.StatusNotFound):
		// 公开下载端点已下线：裸 404 会让人以为"文件不存在"，
		// 实际是"没配 Key，走了已经死掉的通道"。
		if isCurseForgePublicDownloadURL(rawURL) && !keyConfigured {
			return fmt.Errorf("CurseForge 公开下载端点已不可用（404）：%s", curseForgeKeyHint())
		}
		if isCurseForgeCDNHost(rawURL) {
			return fmt.Errorf("CurseForge CDN 上找不到该文件（404）：可能已被作者删除，或作者禁止第三方分发。")
		}
	}
	return nil
}
