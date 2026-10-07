package instance

// 其它启动器实例扫描：定位常见启动器的实例根目录，识别其中可被
// NekoLauncher 直接接管的实例（MultiMC/Prism/CurseForge/Modrinth/ATLauncher），
// 供「导入其他启动器」一键注册为游戏目录。仅做识别，不移动/复制任何文件。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"nekolauncher/internal/tools"
)

// ImportableInstance 可导入的外部实例（扫描结果）。
type ImportableInstance struct {
	Provider         string `json:"Provider"`         // 启动器名称
	Name             string `json:"Name"`             // 实例名
	Path             string `json:"Path"`             // 注册用路径（实例目录）
	ContentDirectory string `json:"ContentDirectory"` // 解析出的内容目录
	GameVersion      string `json:"GameVersion"`      // 尽力解析的游戏版本（可能为空）
	Registered       bool   `json:"Registered"`       // 是否已在游戏目录列表中
}

// importCandidateRoots 常见启动器的实例根目录（按平台）。
// 目录不存在时会被跳过；同一实例被多个候选根命中时按规范化路径去重。
func importCandidateRoots() []string {
	home, _ := os.UserHomeDir()
	appData := os.Getenv("APPDATA")
	localAppData := os.Getenv("LOCALAPPDATA")

	if runtime.GOOS == "windows" {
		return []string{
			filepath.Join(appData, "PrismLauncher", "instances"),
			filepath.Join(appData, "MultiMC", "instances"),
			filepath.Join(appData, "PolyMC", "instances"),
			filepath.Join(appData, "ATLauncher", "instances"),
			filepath.Join(home, "curseforge", "minecraft", "Instances"),
			filepath.Join(appData, "ModrinthApp", "profiles"),
			filepath.Join(appData, "com.modrinth.theseus", "profiles"),
			filepath.Join(localAppData, "ModrinthApp", "profiles"),
		}
	}

	return []string{
		filepath.Join(home, ".local", "share", "PrismLauncher", "instances"),
		filepath.Join(home, ".var", "app", "org.prismlauncher.PrismLauncher", "data", "PrismLauncher", "instances"),
		filepath.Join(home, "Library", "Application Support", "PrismLauncher", "instances"),
		filepath.Join(home, ".local", "share", "multimc", "instances"),
		filepath.Join(home, ".local", "share", "ATLauncher", "instances"),
		filepath.Join(home, ".var", "app", "com.modrinth.ModrinthApp", "data", "ModrinthApp", "profiles"),
	}
}

// ScanImportableInstances 扫描常见启动器实例目录并返回可导入列表。
// registered 为当前游戏目录列表，用于标记「已注册」。
func ScanImportableInstances(registered []string) []ImportableInstance {
	result := make([]ImportableInstance, 0, 8)
	seen := make(map[string]bool, 8)

	for _, root := range importCandidateRoots() {
		if strings.TrimSpace(root) == "" {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			layout, ok := TryResolveExternalInstance(filepath.Join(root, entry.Name()))
			if !ok {
				continue
			}
			key := strings.ToLower(filepath.Clean(layout.InstanceDirectory))
			if seen[key] {
				continue
			}
			seen[key] = true

			result = append(result, ImportableInstance{
				Provider:         layout.Provider,
				Name:             layout.InstanceId,
				Path:             layout.InstanceDirectory,
				ContentDirectory: layout.ContentDirectory,
				GameVersion:      externalGameVersion(layout),
				Registered:       folderRegistered(layout.InstanceDirectory, registered),
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Provider != result[j].Provider {
			return result[i].Provider < result[j].Provider
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result
}

// externalGameVersion 尽力从实例元数据解析游戏版本：
// MultiMC/Prism 的 mmc-pack.json（net.minecraft 组件）、CurseForge 的
// minecraftinstance.json（gameVersion）、Modrinth App 的 profile.json
// （metadata.versions["net.minecraft"]）；解析失败返回空串。
func externalGameVersion(layout ExternalGameInstanceLayout) string {
	switch {
	case strings.Contains(layout.Provider, "MultiMC"):
		return readMmcPackVersion(filepath.Join(layout.InstanceDirectory, "mmc-pack.json"))
	case strings.Contains(layout.Provider, "Modrinth"):
		if version := readModrinthProfileVersion(filepath.Join(layout.InstanceDirectory, "profile.json")); version != "" {
			return version
		}
		return readCurseForgeVersion(filepath.Join(layout.InstanceDirectory, "minecraftinstance.json"))
	default:
		version := readCurseForgeVersion(filepath.Join(layout.InstanceDirectory, "minecraftinstance.json"))
		if version == "" {
			version = readGenericInstanceVersion(filepath.Join(layout.InstanceDirectory, "instance.json"))
		}
		return version
	}
}

// readModrinthProfileVersion 读取 Modrinth App profile.json 的基础游戏版本。
func readModrinthProfileVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var profile struct {
		Metadata struct {
			Versions map[string]string `json:"versions"`
		} `json:"metadata"`
	}
	if json.Unmarshal(data, &profile) != nil {
		return ""
	}
	return profile.Metadata.Versions["net.minecraft"]
}

// readGenericInstanceVersion 从 ATLauncher 的 instance.json 尽力猜测基础游戏
// 版本（该文件没有统一 schema，尝试常见键名）。
func readGenericInstanceVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var generic map[string]any
	if json.Unmarshal(data, &generic) != nil {
		return ""
	}
	for _, key := range []string{"gameVersion", "minecraftVersion", "baseGameVersion"} {
		if value, ok := generic[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func readMmcPackVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var pack struct {
		Components []struct {
			UID     string `json:"uid"`
			Version string `json:"version"`
		} `json:"components"`
	}
	if json.Unmarshal(data, &pack) != nil {
		return ""
	}
	for _, component := range pack.Components {
		if strings.EqualFold(component.UID, "net.minecraft") {
			return component.Version
		}
	}
	return ""
}

func readCurseForgeVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var meta struct {
		GameVersion string `json:"gameVersion"`
	}
	if json.Unmarshal(data, &meta) != nil {
		return ""
	}
	return meta.GameVersion
}

// folderRegistered 路径是否已在游戏目录列表中。
func folderRegistered(instanceDirectory string, registered []string) bool {
	for _, folder := range registered {
		if tools.PathsEqual(folder, instanceDirectory) {
			return true
		}
	}
	return false
}

// IsImportCandidateInstance 验证 path 是否是导入扫描器探测根下的
// 外部实例目录（供绑定层的注册专用通道使用）。AddProfileFolder 已收紧为
// "对话框批准或已注册目录"，自动探测到的实例路径走这里时必须验明正身：
// 父目录是某个候选探测根，且目录结构确实像外部启动器实例。
func IsImportCandidateInstance(path string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" {
		return false
	}
	if _, ok := TryResolveExternalInstance(cleaned); !ok {
		return false
	}
	parent := filepath.Dir(cleaned)
	for _, root := range importCandidateRoots() {
		if strings.EqualFold(filepath.Clean(root), parent) {
			return true
		}
	}
	return false
}
