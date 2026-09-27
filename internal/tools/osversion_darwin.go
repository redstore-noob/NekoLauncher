//go:build darwin

package tools

import (
	"os/exec"
	"strings"
)

// darwinOSVersion 取 macOS 产品版本（"14.5" 这种），供 os.version 规则匹配。
// sw_vers 不可用时返回空串，由调用方回退。
func darwinOSVersion() string {
	output, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}
