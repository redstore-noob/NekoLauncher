//go:build linux

package network

// Linux「跟随系统」代理读取：GNOME 系（GNOME / Cinnamon / MATE / Unity /
// Pop!_OS 等大多数量发型桌面）把系统代理存在 dconf 的
// org.gnome.system.proxy 下，用 gsettings 读。KDE 的 kioslaverc 与
// XFCE 无系统级代理概念（浏览器各管各的），暂不支持，回落环境变量。
//
// 只在 mode=manual 时返回地址；ignore-hosts 例外列表不展开——启动器
// 请求的都是外网域名，本地例外场景几乎不存在。

import (
	"os/exec"
	"strings"
	"sync"
	"time"
)

// systemProxyCacheTTL 与 Windows 版同样的缓存节奏：改系统设置后最多
// 延迟这么久生效。
const systemProxyCacheTTL = 10 * time.Second

var (
	systemProxyMu     sync.Mutex
	systemProxyValue  string
	systemProxyReadAt time.Time
)

// cachedSystemProxy 读 GNOME 手动代理地址（http://host:port 形式）；
// 未启用或 gsettings 不可用返回空串，由调用方回落环境变量。
func cachedSystemProxy() string {
	systemProxyMu.Lock()
	defer systemProxyMu.Unlock()
	if !systemProxyReadAt.IsZero() && time.Since(systemProxyReadAt) < systemProxyCacheTTL {
		return systemProxyValue
	}
	systemProxyValue = readGnomeSystemProxy()
	systemProxyReadAt = time.Now()
	return systemProxyValue
}

// readGnomeSystemProxy 依次探测 socks / https / http 三种协议键，
// 优先 socks5（gsettings 里 socks 有 host 无 scheme，需补 socks5://）。
func readGnomeSystemProxy() string {
	if mode := gsettingsGet("org.gnome.system.proxy", "mode"); strings.TrimSpace(mode) != "manual" {
		return ""
	}

	if address := gnomeProxyAddress("org.gnome.system.proxy.https"); address != "" {
		return address
	}
	if address := gnomeProxyAddress("org.gnome.system.proxy.http"); address != "" {
		return address
	}
	if host := gsettingsGet("org.gnome.system.proxy.socks", "host"); strings.TrimSpace(host) != "" {
		port := strings.TrimSpace(gsettingsGet("org.gnome.system.proxy.socks", "port"))
		if port != "" && port != "0" {
			return "socks5://" + strings.TrimSpace(host) + ":" + port
		}
	}
	return ""
}

// gnomeProxyAddress 读某个协议键（http / https）的 host+port，
// 拼成带 scheme 的地址；host 为空或 port 为 0 视为未配置。
func gnomeProxyAddress(schema string) string {
	host := strings.TrimSpace(gsettingsGet(schema, "host"))
	if host == "" {
		return ""
	}
	port := strings.TrimSpace(gsettingsGet(schema, "port"))
	if port == "" || port == "0" {
		return ""
	}
	if strings.HasPrefix(host, "://") || strings.Contains(host, "://") {
		return host + ":" + port
	}
	return "http://" + host + ":" + port
}

// gsettingsGet 调 gsettings 读一个键，失败返回空串。
func gsettingsGet(schema, key string) string {
	output, err := exec.Command("gsettings", "get", schema, key).Output()
	if err != nil {
		return ""
	}
	// 输出形如 "'manual'"，去掉外层引号
	value := strings.TrimSpace(string(output))
	value = strings.Trim(value, "'\"")
	return value
}
