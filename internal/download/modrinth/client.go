// client.go Modrinth 接口的端点定义与带镜像回退的请求通道。
//
// 为什么单独成文件：官方 api.modrinth.com 在国内网络下经常超时或握手被重置，
// 而同一个根地址此前分别写在 search.go（searchBaseURL）与 version_api.go
// （apiBaseURL）里——加镜像回退时极易只改一处。这里把根地址、回退顺序、
// HTTP 客户端与「可读中文错误」全部收口，业务文件只负责拼路径。
package modrinth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"nekolauncher/internal/logs"
	"nekolauncher/internal/tools"
)

const (
	// OfficialAPIRoot Modrinth 官方 API v2 根地址（无尾部斜杠）。
	OfficialAPIRoot = "https://api.modrinth.com/v2"
	// MirrorAPIRoot 国内镜像 API 根地址（MCIM：https://mod.mcimirror.top）。
	// 实测 /search 与 /project/{id}/version 返回与官方一致（2026-02 手工验证）；
	// 镜像由社区维护、随时可能失效，因此它只是回退，不是主路径。
	MirrorAPIRoot = "https://mod.mcimirror.top/modrinth/v2"
	// SiteURL 项目网页前缀（前端「在浏览器打开项目」用）。
	SiteURL = "https://modrinth.com"
)

// APIHost 官方接口主机名（界面提示文案用，域名只在常量里出现一次）。
func APIHost() string { return hostOf(OfficialAPIRoot) }

// MirrorHost 镜像主机名（空串 = 未配置镜像）。
func MirrorHost() string { return hostOf(MirrorAPIRoot) }

// UsedMirror 最近一次请求是否由镜像回退完成（界面「已自动切换国内镜像」提示用）。
func UsedMirror() bool { return lastUsedMirror.Load() }

// requestTimeout 单次请求的超时预算。搜索是交互式操作：官方挂起时不能
// 让用户对着转圈按钮等满共享客户端的 15 秒，8 秒足够正常的搜索/版本查询完成，
// 超时后还有镜像与一次重试两个机会。
var requestTimeout = 8 * time.Second

// maxResponseBytes 响应体上限（32 MB）：版本列表（含 changelog）在热门项目上
// 可达数百 KB，正常不会接近这个量级；设上限只是防止异常响应把内存撑爆。
const maxResponseBytes = 32 << 20

var (
	// apiRoot 主用根地址。生产环境不变，仅测试/live 检查通过 SetEndpoints 覆盖。
	apiRoot = OfficialAPIRoot
	// mirrorRoot 回退根地址；空串 = 不回退。
	mirrorRoot = MirrorAPIRoot
	// httpClient 出站客户端。默认共享客户端（15s 超时 + 统一 User-Agent）。
	httpClient = tools.SharedHTTPClient
	// preferMirror 最近一次是靠镜像成功的。一旦为真，后续请求先试镜像：
	// 国内直连官方往往要等满整个超时才失败，每次都先等一遍会让搜索固定 8 秒起步。
	preferMirror atomic.Bool
	// lastUsedMirror 最近一次请求是否走了镜像（供界面提示，不参与调度）。
	lastUsedMirror atomic.Bool
)

// SetEndpoints 覆盖主用/回退根地址（测试注入 httptest 地址、或用户自建镜像时用）。
// mirror 传空串表示禁用回退。
func SetEndpoints(official, mirror string) {
	apiRoot = strings.TrimRight(strings.TrimSpace(official), "/")
	mirrorRoot = strings.TrimRight(strings.TrimSpace(mirror), "/")
	preferMirror.Store(false)
	lastUsedMirror.Store(false)
}

// SetHTTPClient 覆盖出站客户端（测试注入 httptest 客户端用）。
func SetHTTPClient(client *http.Client) {
	if client != nil {
		httpClient = client
	}
}

// ResetEndpoints 恢复默认端点、默认客户端与首选地址记忆。
func ResetEndpoints() {
	apiRoot = OfficialAPIRoot
	mirrorRoot = MirrorAPIRoot
	httpClient = tools.SharedHTTPClient
	preferMirror.Store(false)
	lastUsedMirror.Store(false)
}

// getJSON 带镜像回退的 GET + JSON 解析。成功时 target 才被写入。
//
// 顺序：首选地址 → 另一个地址（镜像）→ 首选地址再重试一次。
// 只有「值得重试」的失败（超时、连接被重置、5xx、429）才会走后面的步骤；
// 4xx 与响应格式错误是确定性失败，换地址或重试都不会变好，直接报错。
func getJSON(ctx context.Context, path string, target any) error {
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

	firstErr := getJSONOnce(ctx, first, target)
	if firstErr == nil {
		recordSuccess(first == mirror)
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	var secondErr error
	if second != "" && isRetryable(firstErr) {
		secondErr = getJSONOnce(ctx, second, target)
		if secondErr == nil {
			recordSuccess(second == mirror)
			logs.Write("INFO", "Modrinth 接口主地址失败，已回退备用地址："+path)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	// 两个地址都没成功：再给首选地址一次机会（限流与瞬时抖动居多），
	// 仍失败才向上抛可读错误。
	if isRetryable(firstErr) {
		if retryErr := getJSONOnce(ctx, first, target); retryErr == nil {
			recordSuccess(first == mirror)
			return nil
		}
	}
	return describeFailure(firstErr, secondErr, mirror != "")
}

// recordSuccess 记录本次是否靠镜像成功（同时更新界面提示用的状态）。
func recordSuccess(usedMirror bool) {
	preferMirror.Store(usedMirror)
	lastUsedMirror.Store(usedMirror)
}

// getJSONOnce 单次请求。读到完整响应体后再解析：失败时不会把半截数据写进 target，
// 回退/重试才能安全复用同一个 target。
func getJSONOnce(ctx context.Context, endpoint string, target any) error {
	attemptCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, "GET", endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &statusError{StatusCode: resp.StatusCode}
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

// statusError 非 2xx 响应；带状态码便于判断是否值得回退/重试。
type statusError struct {
	StatusCode int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("接口返回 HTTP %d", e.StatusCode)
}

// decodeError 响应体不是预期 JSON。确定性失败：镜像与重试都救不了，
// 但保留 Unwrap 以便上层继续用 errors.As 识别 json.SyntaxError 等具体原因
// （version_api.go 就靠这个把「格式异常」与「网络失败」分开处理）。
type decodeError struct {
	err error
}

func (e *decodeError) Error() string { return "响应解析失败：" + e.err.Error() }
func (e *decodeError) Unwrap() error { return e.err }

// isRetryable 这次失败是否值得换地址/重试。
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
		// 4xx（404 项目不存在、400 参数不对）重试不会变好；408/429 与 5xx 例外
		return status.StatusCode == http.StatusRequestTimeout ||
			status.StatusCode == http.StatusTooManyRequests ||
			status.StatusCode >= 500
	}
	// 连接被拒、DNS、TLS 重置、超时等：换地址或重试有意义
	return true
}

// describeFailure 拼出用户能看懂的中文错误：说清试过哪些地址、原始原因是什么、
// 下一步该做什么，而不是把 http.Client 的英文错误直接丢到界面上。
func describeFailure(primaryErr, mirrorErr error, mirrorTried bool) error {
	if mirrorTried && mirrorErr != nil {
		return fmt.Errorf(
			"Modrinth 接口访问失败：官方（%s）与国内镜像（%s）均未返回数据。"+
				"请检查网络或代理后重试（原始错误：%w；镜像错误：%v）",
			hostOf(OfficialAPIRoot), hostOf(MirrorAPIRoot), primaryErr, mirrorErr)
	}
	return fmt.Errorf(
		"Modrinth 接口访问失败（%s）：%w。请检查网络或代理后重试",
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
