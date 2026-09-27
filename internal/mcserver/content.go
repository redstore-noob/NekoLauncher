package mcserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"nekolauncher/internal/download"
	"nekolauncher/internal/download/modrinth"
	"nekolauncher/internal/models"
)

// 服务端内容管理：mods 与 plugins 的列表 / 启停 / 删除 / 从 Modrinth 安装。
//
// 与客户端内容下载的分工：搜索与版本过滤复用 `internal/download/modrinth`
// （同一套 API、同一套缓存语义），下载复用 `download.DownloadFileToPath`
// （天然带上全局限速、断点续传与瞬时失败重试）。
// 区别只在"放到哪个目录、按哪个加载器过滤"。
const (
	// ContentKindMods Fabric / NeoForge 服务端的 mods 目录。
	ContentKindMods = "mods"
	// ContentKindPlugins Paper 服务端的 plugins 目录。
	ContentKindPlugins = "plugins"

	// serverContentDisabledSuffix 停用态后缀（服务端只加载 .jar，改名即停用）。
	serverContentDisabledSuffix = ".disabled"
)

// ServerContentEntry 服务端内容目录里的一个条目。
type ServerContentEntry struct {
	// Name 展示名（去掉 .jar 与 .disabled）
	Name string `json:"Name"`
	// FileName 磁盘上的真实文件名（启停/删除都以它为准）
	FileName string `json:"FileName"`
	// SizeBytes 体积
	SizeBytes int64 `json:"SizeBytes"`
	// Enabled 是否启用（未加 .disabled）
	Enabled bool `json:"Enabled"`
	// Path 绝对路径
	Path string `json:"Path"`
}

// ServerContentKindForCore 服务器核心对应的内容目录类型。
// 原版没有 mods/plugins 概念；Paper 系是插件，Fabric/NeoForge 是模组。
func ServerContentKindForCore(core string) (string, error) {
	switch core {
	case CoreFabric, CoreNeoForge:
		return ContentKindMods, nil
	case CorePaper:
		return ContentKindPlugins, nil
	case CoreVanilla:
		return "", errors.New("原版服务端不支持模组或插件")
	default:
		return "", fmt.Errorf("未知的服务器核心：%s", core)
	}
}

// ModrinthLoaderForCore 服务器核心对应的 Modrinth 加载器名（搜索与版本过滤用）。
func ModrinthLoaderForCore(core string) string {
	switch core {
	case CoreFabric:
		return "fabric"
	case CoreNeoForge:
		return "neoforge"
	case CorePaper:
		// Paper 插件在 Modrinth 上通常标 paper/bukkit/spigot，主用 paper
		return "paper"
	default:
		return ""
	}
}

// serverContentDirectory 解析内容目录（只接受两个已知子目录，防目录穿越）。
func serverContentDirectory(id, kind string) (string, error) {
	if err := validateID(id); err != nil {
		return "", err
	}
	if kind != ContentKindMods && kind != ContentKindPlugins {
		return "", fmt.Errorf("未知的内容类型：%s", kind)
	}

	return filepath.Join(serverDirectory(id), kind), nil
}

// ListServerContent 列出内容目录里的 jar（含 .disabled）。
func ListServerContent(id, kind string) []ServerContentEntry {
	directory, err := serverContentDirectory(id, kind)
	if err != nil {
		return []ServerContentEntry{}
	}
	entries, readErr := os.ReadDir(directory)
	if readErr != nil {
		return []ServerContentEntry{}
	}

	result := make([]ServerContentEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".jar") && !strings.HasSuffix(lower, ".jar"+serverContentDisabledSuffix) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		result = append(result, ServerContentEntry{
			Name:      displayContentName(name),
			FileName:  name,
			SizeBytes: info.Size(),
			Enabled:   !strings.HasSuffix(lower, serverContentDisabledSuffix),
			Path:      filepath.Join(directory, name),
		})
	}
	sort.Slice(result, func(left, right int) bool {
		return strings.ToLower(result[left].Name) < strings.ToLower(result[right].Name)
	})

	return result
}

// displayContentName 去掉 .jar 与 .disabled，得到干净的展示名。
func displayContentName(fileName string) string {
	name := fileName
	if strings.HasSuffix(strings.ToLower(name), serverContentDisabledSuffix) {
		name = name[:len(name)-len(serverContentDisabledSuffix)]
	}
	if strings.HasSuffix(strings.ToLower(name), ".jar") {
		name = name[:len(name)-len(".jar")]
	}

	return name
}

// SetServerContentEnabled 启用/停用：靠 .disabled 后缀改名（服务端只加载 .jar）。
func SetServerContentEnabled(id, kind, fileName string, enabled bool) error {
	directory, err := serverContentDirectory(id, kind)
	if err != nil {
		return err
	}
	safe, err := safeContentFileName(fileName)
	if err != nil {
		return err
	}
	source := filepath.Join(directory, safe)
	lower := strings.ToLower(safe)
	disabled := strings.HasSuffix(lower, serverContentDisabledSuffix)

	// 传入的名字可能是"当前形态"也可能是"目标形态"（界面刷新前后各一次调用），
	// 所以两种形态都要认：找不到 source 时先看对面形态是否已经满足要求。
	if _, statErr := os.Stat(source); statErr != nil {
		alternate := safe + serverContentDisabledSuffix
		if disabled {
			alternate = safe[:len(safe)-len(serverContentDisabledSuffix)]
		}
		if _, alternateErr := os.Stat(filepath.Join(directory, alternate)); alternateErr == nil {
			// 对面形态存在：说明已经处于目标状态（或名字传的是另一种形态），幂等返回
			if enabled == !disabled {
				return nil
			}

			return os.Rename(filepath.Join(directory, alternate), source)
		}

		return fmt.Errorf("文件不存在：%s", safe)
	}

	switch {
	case enabled && disabled:
		target := safe[:len(safe)-len(serverContentDisabledSuffix)]
		if _, statErr := os.Stat(filepath.Join(directory, target)); statErr == nil {
			return fmt.Errorf("同名文件已存在：%s", target)
		}

		return os.Rename(source, filepath.Join(directory, target))
	case !enabled && !disabled:
		return os.Rename(source, source+serverContentDisabledSuffix)
	default:
		return nil // 已是目标状态，幂等
	}
}

// DeleteServerContent 删除一个内容文件。
func DeleteServerContent(id, kind, fileName string) error {
	directory, err := serverContentDirectory(id, kind)
	if err != nil {
		return err
	}
	safe, err := safeContentFileName(fileName)
	if err != nil {
		return err
	}
	target := filepath.Join(directory, safe)
	if _, statErr := os.Stat(target); statErr != nil {
		return fmt.Errorf("文件不存在：%s", safe)
	}

	return os.Remove(target)
}

// safeContentFileName 只接受单层文件名（防 ../ 与绝对路径）。
func safeContentFileName(fileName string) (string, error) {
	trimmed := strings.TrimSpace(fileName)
	if trimmed == "" {
		return "", errors.New("文件名为空")
	}
	if trimmed != filepath.Base(trimmed) || strings.ContainsAny(trimmed, `/\`) {
		return "", errors.New("文件名不合法")
	}

	return trimmed, nil
}

// InstallServerContentFromURL 下载一个 jar 到内容目录，返回实际写入的文件名。
//
// 同名不覆盖：已存在时自动加 -1/-2 后缀并把最终文件名返回给调用方
// （静默覆盖会让"装完发现还是旧版本"变成一个查不出来的问题）。
// 服务器运行中也可以安装，文件在下次重启后生效。
func InstallServerContentFromURL(ctx context.Context, id, kind, downloadURL, fileName string) (string, error) {
	directory, err := serverContentDirectory(id, kind)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(downloadURL) == "" {
		return "", errors.New("下载地址为空")
	}
	safe, err := safeContentFileName(fileName)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(safe), ".jar") {
		// Modrinth 上偶尔主文件是 zip（插件包），服务端只认 jar
		return "", fmt.Errorf("只支持安装 .jar 文件（收到 %s）", safe)
	}
	if mkdirErr := os.MkdirAll(directory, 0o755); mkdirErr != nil {
		return "", mkdirErr
	}

	target := uniqueContentPath(directory, safe)
	if err := download.DownloadFileToPath(ctx, downloadURL, target, nil); err != nil {
		return "", err
	}

	return filepath.Base(target), nil
}

// uniqueContentPath 撞名时依次尝试 <名字>-1.jar、-2.jar……
func uniqueContentPath(directory, fileName string) string {
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	extension := filepath.Ext(fileName)
	candidate := fileName

	for index := 1; index <= 1000; index++ {
		path := filepath.Join(directory, candidate)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		candidate = fmt.Sprintf("%s-%d%s", base, index, extension)
	}

	return filepath.Join(directory, candidate)
}

// ---- Modrinth 搜索与安装 ----

// SearchServerContent 在 Modrinth 上搜索服务端内容。
// 按服务器核心对应的加载器 + MC 版本过滤，避免把客户端模组装到服务端。
func SearchServerContent(ctx context.Context, query, gameVersion, core string, limit int) ([]models.ModrinthProject, error) {
	loader := ModrinthLoaderForCore(core)
	if loader == "" {
		return nil, errors.New("该服务器核心不支持从 Modrinth 安装内容")
	}
	if limit <= 0 || limit > 40 {
		limit = 20
	}
	// Modrinth 上插件与模组同属 mod 类型，靠 categories 里的加载器名区分：
	// 不带上这个 facet 就会搜出"能装但服务端根本不加载"的东西。
	return modrinth.SearchWithLoader(ctx, "mod", strings.TrimSpace(query),
		strings.TrimSpace(gameVersion), loader, limit)
}

// ListServerContentVersions 列出某个项目在指定 MC 版本 + 加载器下可用的版本（新→旧由 API 决定）。
func ListServerContentVersions(ctx context.Context, projectID, gameVersion, core string) ([]models.ModrinthVersion, error) {
	loader := ModrinthLoaderForCore(core)
	if loader == "" {
		return nil, errors.New("该服务器核心不支持从 Modrinth 安装内容")
	}

	return modrinth.GetVersions(ctx, projectID, []string{gameVersion}, []string{loader})
}

// InstallServerContentFromModrinth 取该项目在当前 MC 版本 + 加载器下的最新版本并安装。
// 返回实际写入的文件名与安装的版本号。
func InstallServerContentFromModrinth(ctx context.Context, id, kind, projectID, gameVersion, core string) (string, string, error) {
	versions, err := ListServerContentVersions(ctx, projectID, gameVersion, core)
	if err != nil {
		return "", "", err
	}
	if len(versions) == 0 {
		return "", "", fmt.Errorf("没有找到适配 %s / %s 的版本", gameVersion, ModrinthLoaderForCore(core))
	}
	version := versions[0]
	file := version.PrimaryFile()
	if file == nil || strings.TrimSpace(file.URL) == "" {
		return "", "", errors.New("该版本没有可下载的主文件")
	}

	installed, err := InstallServerContentFromURL(ctx, id, kind, file.URL, file.Filename)
	if err != nil {
		return "", "", err
	}

	return installed, version.VersionNumber, nil
}
