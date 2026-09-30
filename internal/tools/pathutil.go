// Package tools 全项目共享的路径、HTTP 客户端、JSON 辅助方法。
package tools

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"nekolauncher/internal/info"
)

// SharedHTTPClient 项目共享的 HTTP 客户端（15 秒超时，统一 User-Agent）。
// 用于版本清单、Modrinth 搜索等轻量 GET 请求。
// 需要无限超时的下载场景（安装器）使用独立实例。
// 包装器 base 留空：RoundTrip 时才解析 http.DefaultTransport——代理设置
// （network.ApplyProxySettings）整体替换全局 Transport 后这里立即生效。
var SharedHTTPClient = createSharedHTTPClient()

func createSharedHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &userAgentTransport{
			base: nil,
			ua:   "NekoLauncher/" + info.Version(),
		},
	}
}

type userAgentTransport struct {
	base http.RoundTripper
	ua   string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	r := req.Clone(req.Context())
	r.Header.Set("User-Agent", t.ua)
	return base.RoundTrip(r)
}

// UserHomeDir 用户主目录；不可用时返回空串，调用方需自行回落。
func UserHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// PathsEqual 规范化后比较两个路径是否相等（去掉尾部目录分隔符、展开为完整路径）。
// Windows 下忽略大小写。任一为空时按"不相等"处理。
func PathsEqual(left, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return false
	}
	absLeft, err := filepath.Abs(left)
	if err != nil {
		return false
	}
	absRight, err := filepath.Abs(right)
	if err != nil {
		return false
	}
	absLeft = filepath.Clean(absLeft)
	absRight = filepath.Clean(absRight)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(absLeft, absRight)
	}
	return absLeft == absRight
}
