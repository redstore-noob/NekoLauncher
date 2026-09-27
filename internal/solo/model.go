// NekoSolo 载荷清单与首启标记的数据模型。
package solo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PayloadFormat 载荷/标记格式的当前版本号。
const PayloadFormat = 1

// Manifest 载荷内的 manifest.json：安装器据此展示信息并生成 neko-solo.json。
type Manifest struct {
	Format       int    `json:"format"`
	PackID       string `json:"packId"`
	PackName     string `json:"packName"`
	PackVersion  string `json:"packVersion"`
	Author       string `json:"author,omitempty"`
	Description  string `json:"description,omitempty"`
	MCVersion    string `json:"mcVersion"`
	LoaderName   string `json:"loaderName,omitempty"`
	LoaderVersion string `json:"loaderVersion,omitempty"`
	// VersionID versions/ 下的实例目录名（= 作者导出时选中的版本号）
	VersionID string `json:"versionId"`
	// SimpleMode 安装后启动器默认进入 NekoLauncher-S 模式
	SimpleMode bool `json:"simpleMode"`
	// HasJava 载荷是否携带 jre/（安装器据此写 javaExecutable）
	HasJava bool `json:"hasJava"`
	// IconPath 载荷内图标的条目名（如 "icon.png"），空表示无图标
	IconPath string `json:"iconPath,omitempty"`
	// UpdateLink 作者填的主页/发布页（安装器"检查更新"按钮跳转）
	UpdateLink string `json:"updateLink,omitempty"`
}

// Marker 安装完成后的 neko-solo.json（存储目录下）。启动器首启消费一次
// （Applied 置 true 后不再覆盖用户的后续修改）。
type Marker struct {
	Format int `json:"format"`
	// Applied 启动器是否已把标记内容应用进 launcher.yaml
	Applied bool `json:"applied"`

	PackID      string `json:"packId,omitempty"`
	PackName    string `json:"packName,omitempty"`
	PackVersion string `json:"packVersion,omitempty"`
	Author      string `json:"author,omitempty"`
	Description string `json:"description,omitempty"`

	MCVersion     string `json:"mcVersion,omitempty"`
	LoaderName    string `json:"loaderName,omitempty"`
	LoaderVersion string `json:"loaderVersion,omitempty"`
	VersionID     string `json:"versionId,omitempty"`

	SimpleMode bool `json:"simpleMode"`

	// MinecraftDirectory 安装器写入的绝对路径（存储目录下 minecraft/）
	MinecraftDirectory string `json:"minecraftDirectory,omitempty"`
	// JavaExecutable 捆绑 JRE 的 java.exe 绝对路径；未捆绑时为空
	JavaExecutable string `json:"javaExecutable,omitempty"`

	UpdateLink string `json:"updateLink,omitempty"`
}

// ParseManifest 校验并解析载荷清单：格式版本必须匹配，关键字段不能为空。
func ParseManifest(data []byte) (*Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("manifest.json 不是有效 JSON：%w", err)
	}
	if manifest.Format != PayloadFormat {
		return nil, fmt.Errorf("不支持的 NekoSolo 载荷格式：%d（当前支持 %d）", manifest.Format, PayloadFormat)
	}
	if strings.TrimSpace(manifest.PackName) == "" {
		return nil, fmt.Errorf("manifest.json 缺少 packName")
	}
	if strings.TrimSpace(manifest.VersionID) == "" {
		return nil, fmt.Errorf("manifest.json 缺少 versionId")
	}
	return &manifest, nil
}

// SanitizePackID 从整合包名生成安全的目录名（仅留字母数字与常用符号，限长 48）。
func SanitizePackID(name string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			builder.WriteRune(r)
		case r == ' ':
			builder.WriteRune('-')
		default:
			// 中文等非 ASCII 字符丢弃：目录名保持 ASCII，避免编码踩坑
		}
	}
	result := strings.Trim(builder.String(), "-._")
	if result == "" {
		result = "pack"
	}
	if len(result) > 48 {
		result = result[:48]
	}
	return result
}

// MarkerPath 存储目录下标记文件的路径。
func markerPath(storageDirectory string) string {
	return filepath.Join(storageDirectory, "neko-solo.json")
}

// LoadMarker 读取标记文件；文件不存在返回 (nil, nil)。
func LoadMarker(storageDirectory string) (*Marker, error) {
	data, err := os.ReadFile(markerPath(storageDirectory))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var marker Marker
	if err := json.Unmarshal(data, &marker); err != nil {
		return nil, fmt.Errorf("neko-solo.json 不是有效 JSON：%w", err)
	}
	return &marker, nil
}

// SaveMarker 原子写回标记文件（先写临时文件再改名，避免写一半被读到）。
func SaveMarker(storageDirectory string, marker *Marker) error {
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	finalPath := markerPath(storageDirectory)
	tempPath := finalPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tempPath, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}
