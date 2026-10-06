//go:build windows

package network

// Windows 用户级系统代理读取（跟随系统模式）：注册表
// HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings。
// 按站点例外（<local>、通配符列表）不在支持范围——只取全局代理服务器地址。

import (
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows/registry"
)

// systemProxyCacheTTL 系统代理缓存时长：用户在系统设置里改代理后最多
// 延迟这么久生效。逐请求读注册表太浪费，完全静态又感知不到变更。
const systemProxyCacheTTL = 10 * time.Second

var (
	systemProxyMu     sync.Mutex
	systemProxyValue  string
	systemProxyReadAt time.Time
)

// cachedSystemProxy 读取用户级系统代理地址（http://host:port 形式）；
// 未启用或解析不出时返回空串。
func cachedSystemProxy() string {
	systemProxyMu.Lock()
	defer systemProxyMu.Unlock()
	if !systemProxyReadAt.IsZero() && time.Since(systemProxyReadAt) < systemProxyCacheTTL {
		return systemProxyValue
	}
	systemProxyValue = readWindowsSystemProxy()
	systemProxyReadAt = time.Now()
	return systemProxyValue
}

func readWindowsSystemProxy() string {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return ""
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return ""
	}
	return parseWindowsProxyServer(server)
}

// parseWindowsProxyServer 解析 ProxyServer 值的三种形态：
//   - "127.0.0.1:7890"（全协议同一地址）
//   - "http=…;https=…;ftp=…"（分协议列表，优先 https，其次 http）
//   - "<local>;…" 例外列表：只跳过无值的占位项
func parseWindowsProxyServer(server string) string {
	raw := strings.TrimSpace(server)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "=") {
		return withHTTPScheme(raw)
	}
	chosen := ""
	for _, part := range strings.Split(raw, ";") {
		entry := strings.TrimSpace(part)
		kv := strings.SplitN(entry, "=", 2)
		if len(kv) != 2 {
			continue
		}
		scheme := strings.ToLower(strings.TrimSpace(kv[0]))
		value := strings.TrimSpace(kv[1])
		if value == "" {
			continue
		}
		switch {
		case scheme == "https":
			return withHTTPScheme(value)
		case scheme == "http" && chosen == "":
			chosen = value
		}
	}
	return withHTTPScheme(chosen)
}

// withHTTPScheme 给 host:port 形式的地址补 http://（Transport.Proxy 需要
// 带 scheme 的 URL，http 代理对 https 流量走 CONNECT 隧道）。
func withHTTPScheme(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if strings.Contains(address, "://") {
		return address
	}
	return "http://" + address
}
