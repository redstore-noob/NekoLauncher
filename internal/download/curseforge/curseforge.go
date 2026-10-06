// Package curseforge CurseForge API v1 客户端（搜索、版本/文件查询与下载地址解析）。
//
// 与 modrinth 包对称：官方接口 + 国内镜像双地址、可注入端点与 HTTP 客户端
// （所有单测离线跑），以及可读中文错误。这里没有复用 modrinth 的内部实现是因为
// 两者的鉴权方式不同（CurseForge 需要 x-api-key 请求头），共享内部函数会把
// "带不带 Key"这件事藏进参数里，反而更难看出问题。
package curseforge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"nekolauncher/internal/logs"
	"nekolauncher/internal/models"
	"nekolauncher/internal/tools"
)

const (
	// OfficialAPIRoot CurseForge 官方 API v1 根地址（无尾部斜杠）。
	OfficialAPIRoot = "https://api.curseforge.com/v1"
	// MirrorAPIRoot 国内镜像根地址（MCIM：https://mod.mcimirror.top）。
	// 实测 /mods/search 与 /mods/{id}/files 可用，且镜像侧自带 Key；
	// 社区维护、可能失效，因此只作回退。
	MirrorAPIRoot = "https://mod.mcimirror.top/curseforge/v1"
	// SiteURL 站点主页。
	SiteURL = "https://www.curseforge.com"
	// APIKeyHint 内置 Key 未生效时的中文引导（界面直接展示）。Key 唯一来源是
	// 编译期内置值，不提供用户自配入口（见 bindings/builtin_keys.go）。
	APIKeyHint = "CurseForge 搜索需要内置 API Key，当前构建未包含：请使用官方发布版，" +
		"或在 Modrinth 里搜索同类资源。"
)

// requestTimeout 单次请求超时；与 modrinth 客户端保持一致的口径。
var requestTimeout = 8 * time.Second

// maxResponseBytes 响应体上限（32 MB）。
const maxResponseBytes = 32 << 20

var (
	// apiRoot 主用根地址（生产不变，测试用 SetEndpoints 覆盖）。
	apiRoot = OfficialAPIRoot
	// mirrorRoot 回退根地址；空串 = 不回退。
	mirrorRoot = MirrorAPIRoot
	// httpClient 出站客户端。
	httpClient = tools.SharedHTTPClient
	// preferMirror 最近一次靠镜像成功，之后优先走镜像（理由同 modrinth 客户端）。
	preferMirror atomic.Bool
	// lastUsedMirror 最近一次请求是否走了镜像（供界面提示）。
	lastUsedMirror atomic.Bool
)

// SetEndpoints 覆盖主用/回退根地址（测试注入 httptest 地址用）；mirror 空串 = 禁用回退。
func SetEndpoints(official, mirror string) {
	apiRoot = strings.TrimRight(strings.TrimSpace(official), "/")
	mirrorRoot = strings.TrimRight(strings.TrimSpace(mirror), "/")
	preferMirror.Store(false)
	lastUsedMirror.Store(false)
}

// SetHTTPClient 覆盖出站客户端（测试注入）。
func SetHTTPClient(client *http.Client) {
	if client != nil {
		httpClient = client
	}
}

// ResetEndpoints 恢复默认端点、客户端与状态记忆。
func ResetEndpoints() {
	apiRoot = OfficialAPIRoot
	mirrorRoot = MirrorAPIRoot
	httpClient = tools.SharedHTTPClient
	preferMirror.Store(false)
	lastUsedMirror.Store(false)
}

// UsedMirror 最近一次请求是否由镜像回退完成（界面"已自动切换国内镜像"提示用）。
func UsedMirror() bool { return lastUsedMirror.Load() }

// APIHost 官方接口主机名（界面提示文案用，域名只在常量里出现一次）。
func APIHost() string { return hostOf(OfficialAPIRoot) }

// MirrorHost 镜像主机名（空串 = 未配置镜像）。
func MirrorHost() string { return hostOf(MirrorAPIRoot) }

// SearchOptions 搜索参数。
type SearchOptions struct {
	// Query 关键词；空串 = 浏览热门。
	Query string
	// ClassID CurseForge 分类 ID（models.CurseForgeClass*）。
	ClassID int
	// GameVersion 按 MC 版本过滤；空串 = 不过滤。
	GameVersion string
	// LoaderType CurseForge 加载器枚举（models.CurseForgeLoader*）；0 = 不过滤。
	LoaderType int
	// Limit 返回条数上限（1..50）。
	Limit int
}

// Search 搜索项目。apiKey 为空时官方接口会返回 403，调用方应先做"未配置 Key"的降级，
// 不要把 403 直接甩给用户。
func Search(ctx context.Context, apiKey string, options SearchOptions) (models.CurseForgeSearchResult, error) {
	var result models.CurseForgeSearchResult

	limit := options.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50 // CurseForge 单页上限就是 50，传更大只会被拒绝
	}

	query := url.Values{}
	query.Set("gameId", strconv.Itoa(models.CurseForgeMinecraftGameID))
	query.Set("pageSize", strconv.Itoa(limit))
	query.Set("sortField", strconv.Itoa(models.CurseForgeSortPopularity))
	query.Set("sortOrder", "desc")
	if trimmed := strings.TrimSpace(options.Query); trimmed != "" {
		query.Set("searchFilter", trimmed)
	}
	if options.ClassID > 0 {
		query.Set("classId", strconv.Itoa(options.ClassID))
	}
	if trimmed := strings.TrimSpace(options.GameVersion); trimmed != "" {
		query.Set("gameVersion", trimmed)
	}
	if options.LoaderType > 0 {
		query.Set("modLoaderType", strconv.Itoa(options.LoaderType))
	}

	if err := getJSON(ctx, "/mods/search?"+query.Encode(), apiKey, &result); err != nil {
		return models.CurseForgeSearchResult{}, err
	}
	if result.Data == nil {
		result.Data = []models.CurseForgeProject{}
	}
	if result.Pagination.TotalCount == 0 {
		result.Pagination.TotalCount = len(result.Data)
	}
	return result, nil
}

// GetFiles 列出项目的文件，可按 MC 版本与加载器过滤（服务端过滤，避免把
// 上百个历史文件全拉回来再在本地筛）。
func GetFiles(ctx context.Context, apiKey, modID, gameVersion string, loaderType, limit int) ([]models.CurseForgeFile, error) {
	id, err := parsePositiveID("项目 ID", modID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 50 {
		limit = 50
	}

	query := url.Values{}
	query.Set("pageSize", strconv.Itoa(limit))
	if trimmed := strings.TrimSpace(gameVersion); trimmed != "" {
		query.Set("gameVersion", trimmed)
	}
	if loaderType > 0 {
		query.Set("modLoaderType", strconv.Itoa(loaderType))
	}

	var result models.CurseForgeFilesResult
	path := fmt.Sprintf("/mods/%d/files?%s", id, query.Encode())
	if err := getJSON(ctx, path, apiKey, &result); err != nil {
		return nil, err
	}
	if result.Data == nil {
		return []models.CurseForgeFile{}, nil
	}
	return result.Data, nil
}

// GetFile 查询单个文件（下载前按版本 ID 反查文件名与地址）。
func GetFile(ctx context.Context, apiKey, modID, fileID string) (*models.CurseForgeFile, error) {
	projectID, err := parsePositiveID("项目 ID", modID)
	if err != nil {
		return nil, err
	}
	versionID, err := parsePositiveID("文件 ID", fileID)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data models.CurseForgeFile `json:"data"`
	}
	path := fmt.Sprintf("/mods/%d/files/%d", projectID, versionID)
	if err := getJSON(ctx, path, apiKey, &result); err != nil {
		return nil, err
	}
	return &result.Data, nil
}

// ResolveDownloadURL 请求官方"下载地址"端点。
//
// 为什么还要单独问一次：/files 响应里的 downloadUrl 在作者禁止第三方分发时为 null，
// 这时官方会返回 403（或 data 为空），必须把它当成"不能下载"而不是"网络错误"。
func ResolveDownloadURL(ctx context.Context, apiKey, modID, fileID string) (string, error) {
	projectID, err := parsePositiveID("项目 ID", modID)
	if err != nil {
		return "", err
	}
	versionID, err := parsePositiveID("文件 ID", fileID)
	if err != nil {
		return "", err
	}

	var result struct {
		Data string `json:"data"`
	}
	path := fmt.Sprintf("/mods/%d/files/%d/download-url", projectID, versionID)
	if err := getJSON(ctx, path, apiKey, &result); err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Data), nil
}

// ---- HTTP 通道 ----

// getJSON 带镜像回退的 GET + JSON 解析（策略与 modrinth 客户端一致：
// 首选地址 → 另一地址 → 首选地址再试一次；只有值得重试的失败才继续下一步）。
func getJSON(ctx context.Context, path, apiKey string, target any) error {
	primary := apiRoot + path
	mirror := ""
	if strings.TrimSpace(mirrorRoot) != "" {
		if candidate := mirrorRoot + path; !strings.EqualFold(candidate, primary) {
			mirror = candidate
		}
	}

	first, second := primary, mirror
	if mirror != "" && preferMirror.Load() {
		first, second = mirror, primary
	}

	firstErr := getJSONOnce(ctx, first, apiKey, target)
	if firstErr == nil {
		recordSuccess(first == mirror)
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	var secondErr error
	if second != "" && isRetryable(firstErr) {
		secondErr = getJSONOnce(ctx, second, apiKey, target)
		if secondErr == nil {
			recordSuccess(second == mirror)
			logs.Write("INFO", "CurseForge 接口主地址失败，已回退备用地址："+path)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	if isRetryable(firstErr) {
		if retryErr := getJSONOnce(ctx, first, apiKey, target); retryErr == nil {
			recordSuccess(first == mirror)
			return nil
		}
	}
	return describeFailure(firstErr, secondErr, mirror != "", apiKey)
}

// recordSuccess 记录本次是否走镜像。
func recordSuccess(usedMirror bool) {
	preferMirror.Store(usedMirror)
	lastUsedMirror.Store(usedMirror)
}

// getJSONOnce 单次请求；读到完整响应体后再解析，失败不会污染 target。
func getJSONOnce(ctx context.Context, endpoint, apiKey string, target any) error {
	attemptCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, "GET", endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if key := strings.TrimSpace(apiKey); key != "" {
		req.Header.Set("x-api-key", key)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &statusError{StatusCode: resp.StatusCode, APIKeySent: strings.TrimSpace(apiKey) != ""}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return &decodeError{err: err}
	}
	return nil
}

// statusError 非 2xx 响应。
type statusError struct {
	StatusCode int
	// APIKeySent 本次请求是否带了 Key：403 时用来区分"没配 Key"与"Key 无效"。
	APIKeySent bool
}

func (e *statusError) Error() string {
	return fmt.Sprintf("接口返回 HTTP %d", e.StatusCode)
}

// decodeError 响应体不是预期 JSON（确定性失败）。
type decodeError struct{ err error }

func (e *decodeError) Error() string { return "响应解析失败：" + e.err.Error() }
func (e *decodeError) Unwrap() error { return e.err }

// isRetryable 是否值得换地址/重试。
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var decode *decodeError
	if errors.As(err, &decode) {
		return false
	}
	var status *statusError
	if errors.As(err, &status) {
		return status.StatusCode == http.StatusRequestTimeout ||
			status.StatusCode == http.StatusTooManyRequests ||
			status.StatusCode >= 500
	}
	return true
}

// describeFailure 拼出可读中文错误：403 单独说明 Key 的问题（这是鉴权失败时
// 最常见的现象，直接把 "HTTP 403" 丢给用户等于没说）。
func describeFailure(primaryErr, mirrorErr error, mirrorTried bool, apiKey string) error {
	var status *statusError
	if errors.As(primaryErr, &status) && (status.StatusCode == http.StatusForbidden || status.StatusCode == http.StatusUnauthorized) {
		if strings.TrimSpace(apiKey) == "" {
			return fmt.Errorf("CurseForge 接口需要内置 API Key，当前构建未包含。%s", APIKeyHint)
		}
		return fmt.Errorf("CurseForge 拒绝了这次请求（HTTP %d）：内置 API Key 可能无效或已触发限流，请稍后重试",
			status.StatusCode)
	}

	if mirrorTried && mirrorErr != nil {
		return fmt.Errorf(
			"CurseForge 接口访问失败：官方（%s）与国内镜像（%s）均未返回数据。"+
				"请检查网络或代理后重试（原始错误：%w；镜像错误：%v）",
			hostOf(OfficialAPIRoot), hostOf(MirrorAPIRoot), primaryErr, mirrorErr)
	}
	return fmt.Errorf(
		"CurseForge 接口访问失败（%s）：%w。请检查网络或代理后重试",
		hostOf(OfficialAPIRoot), primaryErr)
}

// hostOf 取出根地址的主机名（提示文案用；域名只在常量里出现一次）。
func hostOf(root string) string {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(root), "https://"), "http://")
	if index := strings.IndexAny(trimmed, "/?#"); index >= 0 {
		return trimmed[:index]
	}
	return trimmed
}

// parsePositiveID 解析 CurseForge 的数字 ID；非法值给出中文错误而不是发一个必然 404 的请求。
func parsePositiveID(label, raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s 不是有效的数字：%q", label, raw)
	}
	return value, nil
}
