// 实例自定义图标的持久化存储。移植自 NyaLauncher.Core/Content/CustomInstanceIconStore.cs。
// 图标按「游戏目录 + 版本 id」哈希存放在启动器存储目录的 instance-icons/custom 下，
// 不污染 Minecraft 游戏目录。ResolveInstanceVisual 优先读取这里的图标。
package content

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"nekolauncher/internal/config"
)

// allowedIconExtensions 允许的自定义图标扩展名。
var allowedIconExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".bmp": true, ".gif": true,
}

// maximumIconBytes 自定义图标大小上限（8 MB）。
const maximumIconBytes = 8 * 1024 * 1024

// GetCustomIconPath 解析自定义图标路径；未设置或文件已丢失时返回空串。
// 写入侧保留原始扩展名，因此查找时按 hash.* 通配匹配，而不是假设固定文件名。
func GetCustomIconPath(minecraftDirectory, versionID string) string {
	if strings.TrimSpace(minecraftDirectory) == "" || strings.TrimSpace(versionID) == "" {
		return ""
	}

	hash := computeInstanceIconHash(minecraftDirectory, versionID)
	directory := filepath.Join(config.StorageDirectory(), "instance-icons", "custom")
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		return ""
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return ""
	}
	var matches []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, hash+".") && !entry.IsDir() {
			matches = append(matches, filepath.Join(directory, name))
		}
	}
	if len(matches) == 0 {
		return ""
	}
	// 与 C# Directory.GetFiles(hash + ".*") 后按 Ordinal 排序取第一个一致
	sort.Strings(matches)
	return matches[0]
}

// SetCustomIcon 为指定实例设置自定义图标：校验扩展名与大小后复制到存储目录。
// 成功返回存储路径；参数无效、文件不可读或磁盘写入失败返回 error。
func SetCustomIcon(minecraftDirectory, versionID, sourcePath string) (string, error) {
	if strings.TrimSpace(minecraftDirectory) == "" ||
		strings.TrimSpace(versionID) == "" ||
		strings.TrimSpace(sourcePath) == "" {
		return "", errors.New("参数无效")
	}

	extension := strings.ToLower(filepath.Ext(sourcePath))
	if !allowedIconExtensions[extension] {
		return "", errors.New("不支持的图标格式")
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumIconBytes {
		return "", errors.New("图标文件为空或超过大小限制")
	}

	hash := computeInstanceIconHash(minecraftDirectory, versionID)
	directory := filepath.Join(config.StorageDirectory(), "instance-icons", "custom")
	target := filepath.Join(directory, hash+extension)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	removeIconVariants(directory, hash)
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", err
	}
	return target, nil
}

// RemoveCustomIcon 清除指定实例的自定义图标；存在并删除成功返回 true。
func RemoveCustomIcon(minecraftDirectory, versionID string) bool {
	if strings.TrimSpace(minecraftDirectory) == "" || strings.TrimSpace(versionID) == "" {
		return false
	}

	hash := computeInstanceIconHash(minecraftDirectory, versionID)
	directory := filepath.Join(config.StorageDirectory(), "instance-icons", "custom")
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		return false
	}
	return removeIconVariants(directory, hash)
}

// removeIconVariants 删除同一 hash 的所有扩展名变体；任一删除成功返回 true。
func removeIconVariants(directory, hash string) bool {
	removed := false
	entries, err := os.ReadDir(directory)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, hash+".") {
			continue
		}
		if err := os.Remove(filepath.Join(directory, name)); err == nil {
			removed = true
		}
	}
	return removed
}

// computeInstanceIconHash 计算图标存储键：规范化游戏目录 + \0 + 版本 id 的 SHA-256 小写十六进制。
func computeInstanceIconHash(minecraftDirectory, versionID string) string {
	key := normalizeIconPath(minecraftDirectory) + "\x00" + versionID
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// normalizeIconPath 展开完整路径并去掉尾部分隔符。
// 仅 Windows 文件系统大小写不敏感；Linux 下两个大小写不同的目录是不同实例。
func normalizeIconPath(path string) string {
	normalized := trimEndingSep(mustAbsPath(path))
	if runtime.GOOS == "windows" {
		return strings.ToLower(normalized)
	}
	return normalized
}

func mustAbsPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func trimEndingSep(path string) string {
	return strings.TrimRight(path, `\/`)
}
