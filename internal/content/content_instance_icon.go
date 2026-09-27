package content

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"nekolauncher/internal/tools"
)

// ---------------------------------------------------------------------------
// 实例目录图标探测
// ---------------------------------------------------------------------------

// findInstanceIcon 探测实例目录自带图标：固定候选名 → 元数据 JSON 引用 → instance.cfg iconKey。
func findInstanceIcon(instanceDirectory, launcherRoot string) string {
	direct := findFirstExisting(
		instanceDirectory,
		"icon.png",
		"icon.jpg",
		"instance.png",
		"logo.png",
		"profile.png",
		filepath.Join("PCL", "Logo.png"),
		filepath.Join("PCL", "Logo.jpg"),
		filepath.Join("minecraft", "icon.png"),
		filepath.Join(".minecraft", "icon.png"))
	if direct != "" {
		return direct
	}

	for _, metadataName := range []string{"minecraftinstance.json", "profile.json", "instance.json"} {
		metadataPath := filepath.Join(instanceDirectory, metadataName)
		if referenced := readReferencedIcon(metadataPath, instanceDirectory); referenced != "" {
			return referenced
		}
	}

	return readCfgIconKey(instanceDirectory, launcherRoot)
}

// readCfgIconKey MultiMC 系的 instance.cfg 里 iconKey 指向启动器 icons/ 目录下的图标。
func readCfgIconKey(instanceDirectory, launcherRoot string) string {
	cfgPath := filepath.Join(instanceDirectory, "instance.cfg")
	if !tools.FileExists(cfgPath) || strings.TrimSpace(launcherRoot) == "" {
		return ""
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return ""
	}
	iconKey := ""
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(strings.TrimRight(line, "\r"), "=", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "iconKey") {
			iconKey = strings.TrimSpace(parts[1])
			break
		}
	}
	if strings.TrimSpace(iconKey) == "" || strings.EqualFold(iconKey, "default") {
		return ""
	}
	return findFirstExisting(filepath.Join(launcherRoot, "icons"), iconKey+".png", iconKey+".jpg")
}

// readReferencedIcon 读取第三方启动器实例元数据 JSON 中引用的图标路径。
func readReferencedIcon(metadataPath, instanceDirectory string) string {
	if !tools.FileExists(metadataPath) {
		return ""
	}
	if info, err := os.Stat(metadataPath); err != nil || info.Size() > maximumMetadataBytes {
		return ""
	}
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return ""
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return ""
	}
	return findReferencedIcon(document, instanceDirectory)
}

// findReferencedIcon 深度优先搜索 JSON 树里的图标属性（URL 或本地路径）。
func findReferencedIcon(element any, instanceDirectory string) string {
	switch value := element.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := value[key]
			if text, ok := child.(string); ok && isIconProperty(key) {
				if resolved := resolveIconValue(text, instanceDirectory); resolved != "" {
					return resolved
				}
			}
			if nested := findReferencedIcon(child, instanceDirectory); nested != "" {
				return nested
			}
		}
	case []any:
		for _, child := range value {
			if nested := findReferencedIcon(child, instanceDirectory); nested != "" {
				return nested
			}
		}
	}
	return ""
}

// resolveIconValue 把图标属性值解析为可用的图标引用：https URL / 存在的本地路径。
func resolveIconValue(value, instanceDirectory string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme == "https" {
		return value
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme == "file" {
		local := parsed.Path
		if runtime.GOOS == "windows" && strings.HasPrefix(local, "/") {
			local = local[1:]
		}
		if tools.FileExists(local) {
			return mustAbsPath(local)
		}
	}
	candidate := value
	if !filepath.IsAbs(value) {
		candidate = filepath.Join(instanceDirectory, filepath.FromSlash(value))
	}
	if tools.FileExists(candidate) {
		return mustAbsPath(candidate)
	}
	return ""
}

// isIconProperty 图标属性名匹配表。
func isIconProperty(name string) bool {
	switch strings.ToLower(name) {
	case "icon", "iconurl", "iconpath", "icon_path",
		"profileimagepath", "profile_image_path",
		"logo", "logourl", "imageurl":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// 加载器识别
// ---------------------------------------------------------------------------

// detectLoaderFromMetadata 从版本 JSON 与 mmc-pack.json 的文本特征识别加载器
// （NeoForge > Fabric > Quilt > Forge > 原版）。版本 JSON 只读前 1 MB。
func detectLoaderFromMetadata(minecraftDirectory, instanceDirectory, versionID string) string {
	var signals strings.Builder
	signals.WriteString(versionID)
	versionJSON := filepath.Join(minecraftDirectory, "versions", versionID, versionID+".json")
	if tools.FileExists(versionJSON) {
		if data, err := os.ReadFile(versionJSON); err == nil {
			if len(data) > 1024*1024 {
				data = data[:1024*1024]
			}
			// 只追加实际读到的内容
			signals.Write(data)
		}
	}
	packPath := filepath.Join(instanceDirectory, "mmc-pack.json")
	if tools.FileExists(packPath) {
		if data, err := os.ReadFile(packPath); err == nil {
			signals.Write(data)
		}
	}
	return matchLoaderName(signals.String())
}

// matchLoaderName 文本特征匹配加载器名。
func matchLoaderName(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "neoforge"):
		return "NeoForge"
	case strings.Contains(lower, "fabric"):
		return "Fabric"
	case strings.Contains(lower, "quilt"):
		return "Quilt"
	case strings.Contains(lower, "forge"):
		return "Forge"
	default:
		return "原版"
	}
}

// loaderGlyph 回退字形统一使用 Material 图标字形（Core 只存字符串，由 UI 层渲染为图标）。
func loaderGlyph(string) string { return "material:Apps" }

// ---------------------------------------------------------------------------
// 通用文件系统辅助
// ---------------------------------------------------------------------------

// findFirstExisting 返回第一个存在的相对路径候选（相对 root 的完整路径）。
func findFirstExisting(root string, relativePaths ...string) string {
	for _, relativePath := range relativePaths {
		candidate := filepath.Join(root, relativePath)
		if tools.FileExists(candidate) {
			return mustAbsPath(candidate)
		}
	}
	return ""
}

// enumerateFiles 枚举目录下以指定后缀结尾的文件（目录不存在返回空）。
func enumerateFiles(directory, suffix string) []string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), suffix) {
			result = append(result, filepath.Join(directory, entry.Name()))
		}
	}
	return result
}

// enumerateArchivesAndDirectories 枚举目录下的 *.zip、*.zip.disabled 与子目录。
// 禁用态（.disabled 后缀）必须一起枚举：只列 *.zip 会让"关掉一个资源包"
// 变成不可逆操作——条目从列表里消失，再也没法在界面上打开它。
func enumerateArchivesAndDirectories(directory string) []string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if entry.IsDir() || strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".zip.disabled") {
			result = append(result, filepath.Join(directory, entry.Name()))
		}
	}
	return result
}

func sortEntries(entries []GameContentEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
