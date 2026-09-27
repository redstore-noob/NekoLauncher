//go:build windows

package bindings

// 稳定设备标识（Windows）：注册表 MachineGuid 是系统安装时生成、重装前不变的
// 机器级 GUID，作为"设备码"语义最贴近。这里只返回其 SHA-256 摘要（截断 16 字节），
// 不把原始 GUID 暴露到前端。

import (
	"crypto/sha256"
	"encoding/hex"
	"os"

	"golang.org/x/sys/windows/registry"
)

// rawDeviceSeed 取设备原始标识串：MachineGuid，读取失败回退主机名+用户名。
func rawDeviceSeed() string {
	if key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Cryptography`,
		registry.QUERY_VALUE|registry.WOW64_64KEY,
	); err == nil {
		defer key.Close()
		if value, _, err := key.GetStringValue("MachineGuid"); err == nil && value != "" {
			return value
		}
	}
	seed := ""
	if host, err := os.Hostname(); err == nil {
		seed = host
	}
	return seed + "|" + os.Getenv("USERNAME")
}

// digestDeviceSeed 摘要为稳定十六进制设备码（与平台无关的统一出口见 api_system_ext.go）。
func digestDeviceSeed(raw string) string {
	sum := sha256.Sum256([]byte("nekolauncher-device:" + raw))
	return hex.EncodeToString(sum[:16])
}
