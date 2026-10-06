package content

import (
	"archive/zip"
	"bytes"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/tools"
)

// ---------------------------------------------------------------------------
// JSON 字段读取
// ---------------------------------------------------------------------------

// readPeople authors / contributors / authorList：字符串、对象（键为作者名）与对象数组三种形态都兼容。
func readPeople(root map[string]any, propertyName string) string {
	authors, ok := root[propertyName]
	if !ok {
		return "未提供"
	}
	switch value := authors.(type) {
	case string:
		if value == "" {
			return "未提供"
		}
		return value
	case map[string]any:
		names := make([]string, 0, len(value))
		for key := range value {
			names = append(names, key)
		}
		sort.Strings(names)
		return strings.Join(names, "、")
	case []any:
		var names []string
		for _, item := range value {
			switch entry := item.(type) {
			case string:
				if strings.TrimSpace(entry) != "" {
					names = append(names, entry)
				}
			case map[string]any:
				if name := readJSONString(entry, "name"); strings.TrimSpace(name) != "" {
					names = append(names, name)
				}
			}
		}
		if len(names) == 0 {
			return "未提供"
		}
		return strings.Join(names, "、")
	}
	return "未提供"
}

// readDescription description：字符串、组件对象（text/translate）或其他 JSON 值的原始文本。
func readDescription(root map[string]any, propertyName string) string {
	description, ok := root[propertyName]
	if !ok {
		return ""
	}
	switch value := description.(type) {
	case string:
		return value
	case map[string]any:
		if text := readJSONString(value, "text"); text != "" {
			return text
		}
		if translate := readJSONString(value, "translate"); translate != "" {
			return translate
		}
		serialized, _ := json.Marshal(value)
		return string(serialized)
	default:
		serialized, _ := json.Marshal(value)
		return string(serialized)
	}
}

// readIconProperty icon 字段：字符串或 { "尺寸": "路径" } 映射（取数值最大的尺寸）。
func readIconProperty(root map[string]any, propertyName string) string {
	icon, ok := root[propertyName]
	if !ok {
		return ""
	}
	switch value := icon.(type) {
	case string:
		return value
	case map[string]any:
		type sizePath struct {
			size int
			path string
		}
		var candidates []sizePath
		for key, entry := range value {
			size := 0
			if parsed, err := atoiSafe(key); err == nil {
				size = parsed
			}
			if text, ok := entry.(string); ok && strings.TrimSpace(text) != "" {
				candidates = append(candidates, sizePath{size, text})
			}
		}
		if len(candidates) == 0 {
			return ""
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].size > candidates[j].size })
		return candidates[0].path
	}
	return ""
}

func atoiSafe(value string) (int, error) {
	result := 0
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("not a number")
		}
		result = result*10 + int(ch-'0')
	}
	return result, nil
}

// readJSONString 读取 JSON 对象中字符串类型的字段，缺失或类型不符返回空串。
func readJSONString(root map[string]any, propertyName string) string {
	if root == nil {
		return ""
	}
	if value, ok := root[propertyName].(string); ok {
		return value
	}
	return ""
}

// readJSONBool 读取 JSON 对象中布尔类型的字段，缺失或类型不符返回 false。
// 仅用于"缺省即 false"的语义（如 quilt 依赖的 optional 标志）；
// 语义上"缺省为 true"的字段不要用它，否则会把默认值读反。
func readJSONBool(root map[string]any, propertyName string) bool {
	if root == nil {
		return false
	}
	if value, ok := root[propertyName].(bool); ok {
		return value
	}
	return false
}

// ---------------------------------------------------------------------------
// 压缩包条目读取
// ---------------------------------------------------------------------------

// readEntryJSON 流式读取压缩包内的 JSON 条目。
// 限制实际读取字节数，避免恶意构造的条目撑爆内存。
func readEntryJSON(entry *zip.File) (any, error) {
	if entry.UncompressedSize64 > maximumMetadataBytes {
		return nil, fmt.Errorf("Metadata entry is too large.")
	}
	stream, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	limited, err := copyWithLimit(stream, maximumMetadataBytes)
	if err != nil {
		return nil, err
	}
	var document any
	if err := json.Unmarshal(limited, &document); err != nil {
		return nil, err
	}
	return document, nil
}

// readEntryObject 读取压缩包内的 JSON 条目并要求顶层为对象。
func readEntryObject(entry *zip.File) (map[string]any, error) {
	document, err := readEntryJSON(entry)
	if err != nil {
		return nil, err
	}
	obj, ok := document.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("metadata entry is not an object")
	}
	return obj, nil
}

// copyWithLimit 复制最多 limit 字节；超出即报错（而不是静默截断导致解析出错）。
func copyWithLimit(source io.Reader, limit int64) ([]byte, error) {
	var buffer bytes.Buffer
	if _, err := io.CopyN(&buffer, source, limit+1); err != nil && err != io.EOF {
		return nil, err
	}
	if int64(buffer.Len()) > limit {
		return nil, fmt.Errorf("Metadata entry is too large.")
	}
	return buffer.Bytes(), nil
}

// readEntryText 读取压缩包条目的 UTF-8 文本（带大小上限）。
func readEntryText(entry *zip.File) (string, error) {
	if entry.UncompressedSize64 > maximumMetadataBytes {
		return "", fmt.Errorf("Metadata entry is too large.")
	}
	stream, err := entry.Open()
	if err != nil {
		return "", err
	}
	defer stream.Close()
	limited, err := copyWithLimit(stream, maximumMetadataBytes)
	if err != nil {
		return "", err
	}
	return string(limited), nil
}

// tomlValuePattern 用正则从 mods.toml 文本中提取顶层 key = "value"
// （含三引号与单双引号字面量）。使用时按 key 转义后动态编译。

// readTomlValue 提取 mods.toml 顶层字符串值。
func readTomlValue(text, key string) string {
	pattern := regexp.MustCompile(fmt.Sprintf(`(?im)^\s*%s\s*=\s*(?:"""([\s\S]*?)"""|'''([\s\S]*?)'''|"([^"]*)"|'([^']*)')`, regexp.QuoteMeta(key)))
	match := pattern.FindStringSubmatch(text)
	if match == nil {
		return ""
	}
	for _, group := range match[1:] {
		if group != "" {
			return strings.TrimSpace(group)
		}
	}
	return ""
}

// readManifestValue 读取 JAR 内 MANIFEST.MF 的指定键（处理续行："\r\n " 前缀拼接）。
func readManifestValue(archive *zip.Reader, key string) string {
	manifest := findEntry(archive, "META-INF/MANIFEST.MF")
	if manifest == nil {
		return ""
	}
	text, err := readEntryText(manifest)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n ", ""), "\n")
	prefix := key + ":"
	for _, line := range lines {
		if len(line) >= len(prefix) && strings.EqualFold(line[:len(prefix)], prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return ""
}

func isTemplateValue(value string) bool {
	return strings.TrimSpace(value) != "" && strings.Contains(value, "${")
}

// findEntry 按忽略大小写的完整路径查找压缩包条目。
func findEntry(archive *zip.Reader, entryPath string) *zip.File {
	normalized := strings.TrimPrefix(strings.ReplaceAll(entryPath, "\\", "/"), "/")
	for i := range archive.File {
		if strings.EqualFold(archive.File[i].Name, normalized) {
			return archive.File[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 图标提取与磁盘缓存
// ---------------------------------------------------------------------------

// extractIcon 把压缩包内的图标条目提取到 content-icons 磁盘缓存并返回路径。
// 缓存按（文件路径, 大小, mtime, 条目, 条目大小）哈希命名，文件更新后自然产生新键。
func extractIcon(sourcePath string, entry *zip.File) string {
	if entry.UncompressedSize64 <= 0 || entry.UncompressedSize64 > maximumZipIconBytes {
		return ""
	}
	extension := strings.ToLower(filepath.Ext(entry.Name))
	switch extension {
	case ".png", ".jpg", ".jpeg", ".webp":
	default:
		return ""
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return ""
	}
	cacheKey := fmt.Sprintf("%s|%d|%d|%s|%d",
		sourcePath, info.Size(), info.ModTime().UnixNano(), entry.Name, entry.UncompressedSize64)
	sum := sha256.Sum256([]byte(cacheKey))
	hash := hex.EncodeToString(sum[:])
	cacheDirectory := filepath.Join(config.StorageDirectory(), "content-icons")
	output := filepath.Join(cacheDirectory, hash+extension)
	if tools.FileExists(output) {
		return output
	}

	if err := os.MkdirAll(cacheDirectory, 0o755); err != nil {
		return ""
	}
	pruneIconCache(cacheDirectory)
	// 先写临时文件再改名：避免并发请求/中途失败留下半个图标文件
	temporary := fmt.Sprintf("%s.%s.tmp", output, newContentGUID())
	if err := writeEntryToFile(entry, temporary); err != nil {
		_ = os.Remove(temporary)
		return ""
	}
	if _, err := os.Stat(output); err == nil {
		// 并发竞争：别的线程/进程刚写出同一图标
		_ = os.Remove(temporary)
		return output
	}
	if err := os.Rename(temporary, output); err != nil {
		_ = os.Remove(temporary)
		return ""
	}
	return output
}

// writeEntryToFile 将压缩包条目内容写到目标文件（临时文件，独占创建）。
func writeEntryToFile(entry *zip.File, destination string) error {
	stream, err := entry.Open()
	if err != nil {
		return err
	}
	defer stream.Close()
	target, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer target.Close()
	_, err = io.Copy(target, stream)
	return err
}

// CopyFileIntoDirectory 把外部文件复制到指定目录（拖拽安装用）。
// 只接受 .jar / .zip，目标重名时返回错误而不是静默覆盖。
func CopyFileIntoDirectory(sourcePath, destinationDir string) (string, error) {
	ext := strings.ToLower(filepath.Ext(sourcePath))
	if ext != ".jar" && ext != ".zip" {
		return "", fmt.Errorf("不支持的文件类型: %s", ext)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("不支持目录")
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(destinationDir, filepath.Base(sourcePath))
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("目标已存在同名文件: %s", filepath.Base(target))
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", err
	}

	return target, nil
}

// pruneIconCache 图标缓存按 (路径, 大小, mtime, 条目) 哈希命名，重下/改名的模组会不断
// 产生新键；超过上限时删除最旧的一批，避免缓存无限增长。
func pruneIconCache(cacheDirectory string) {
	entries, err := os.ReadDir(cacheDirectory)
	if err != nil || len(entries) <= maximumIconCacheFiles {
		return
	}
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	files := make([]fileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{filepath.Join(cacheDirectory, entry.Name()), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	for _, file := range files {
		// 被占用则留给下次
		_ = os.Remove(file.path)
		remaining, err := os.ReadDir(cacheDirectory)
		if err != nil || len(remaining) <= maximumIconCacheFiles/2 {
			break
		}
	}
}

// newContentGUID 生成不带连字符的小写 GUID。
func newContentGUID() string {
	b := make([]byte, 16)
	_, _ = crand.Read(b)
	return fmt.Sprintf("%x", b)
}
