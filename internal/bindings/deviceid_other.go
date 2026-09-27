//go:build !windows

package bindings

// 非 Windows 平台的设备标识：无注册表可读，回退主机名+用户名摘要。

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// rawDeviceSeed 取设备原始标识串（非 Windows 回退路径）。
func rawDeviceSeed() string {
	seed := ""
	if host, err := os.Hostname(); err == nil {
		seed = host
	}
	return seed + "|" + os.Getenv("USER") + "|" + os.Getenv("USERNAME")
}

// digestDeviceSeed 摘要为稳定十六进制设备码。
func digestDeviceSeed(raw string) string {
	sum := sha256.Sum256([]byte("nekolauncher-device:" + raw))
	return hex.EncodeToString(sum[:16])
}
