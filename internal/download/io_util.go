package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"nekolauncher/internal/logs"
)

// httpGetString GET 文本（专用客户端），非 2xx 报 httpStatusError。
func httpGetString(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	return readAllString(resp)
}

// readAllLimited 限长读取：超过 limit 字节时返回错误而不是截断后继续。
// 用于把压缩包里的清单文件读进内存前设一道独立上限（解压阶段的体积防护
// 发生在这之后，挡不住 deflate 全零条目把内存撑爆）。
func readAllLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("清单文件超过体积上限（%d MB）", limit>>20)
	}
	return data, nil
}

// jsonUnmarshalStrict JSON 解析（独立封装便于统一错误信息）。
func jsonUnmarshalStrict(data []byte, target any) error {
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("JSON 解析失败：%w", err)
	}
	return nil
}

// logsWrite 写一条日志（失败不影响主流程）。
func logsWrite(message string) {
	_ = logs.Write("DEBUG", message)
}

// readAllString 读取响应体为字符串；非 2xx 状态码按 httpStatusError 处理
// （对应 C# GetStringAsync 的 EnsureSuccess 语义）。
func readAllString(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &httpStatusError{StatusCode: resp.StatusCode}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// readAllBytes 读取响应体为字节；非 2xx 状态码按 httpStatusError 处理。
func readAllBytes(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &httpStatusError{StatusCode: resp.StatusCode}
	}
	return io.ReadAll(resp.Body)
}

// httpStatusError 非 2xx 响应错误（对应 C# HttpRequestException.StatusCode）。
type httpStatusError struct {
	StatusCode int
}

func (e *httpStatusError) Error() string {
	return "HTTP " + http.StatusText(e.StatusCode)
}

// isHTTPStatusError 判断错误是否为非 2xx 状态错误。
func isHTTPStatusError(err error) bool {
	var statusErr *httpStatusError
	return errors.As(err, &statusErr)
}

// isHTTPStatus 判断错误链中是否包含指定状态码的 HTTP 状态错误。
func isHTTPStatus(err error, statusCode int) bool {
	var statusErr *httpStatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == statusCode
}

// forgeCDNFallbackURL CurseForge CDN 双域名兜底：downloadUrl 给的是
// edge.forgecdn.net，但该域名的 GET 在部分网络下直接 404（HEAD 才 302 跳转
// 到 mediafilez，实测 2026-09）；mediafilez.forgecdn.net 与它路径同构且可达。
// 输入是 edge 域名时返回换好域名的地址，其余返回空串（无需兜底）。
func forgeCDNFallbackURL(downloadURL string) string {
	if strings.Contains(downloadURL, "://edge.forgecdn.net/") {
		return strings.Replace(downloadURL, "://edge.forgecdn.net/", "://mediafilez.forgecdn.net/", 1)
	}
	return ""
}

// rangeMismatchError 断点续传的临时文件与远端内容不一致（416 且长度对不上）。
// 断点信息已被丢弃，错误本身按瞬时失败处理：重试一次即可从零完整下载。
type rangeMismatchError struct{}

func (e *rangeMismatchError) Error() string {
	return "断点信息与远端文件不一致，已重置下载。"
}

// sanitizeSegment 替换文件名中的非法字符（跨平台基础集，对应 C# Path.GetInvalidFileNameChars 的常见子集）。
func sanitizeSegment(name string) string {
	replacer := strings.NewReplacer(
		"<", "_", ">", "_", ":", "_", "\"", "_", "/", "_", "\\", "_",
		"|", "_", "?", "_", "*", "_",
	)
	return replacer.Replace(name)
}

// containsInvalidFileNameChars 版本 ID 等是否包含文件系统非法字符。
func containsInvalidFileNameChars(s string) bool {
	return strings.ContainsAny(s, "<>:\"/\\|?*")
}
