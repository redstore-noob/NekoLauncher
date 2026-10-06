package bindings

// 插件管理 API：插件页的数据源与安装 / 卸载 / 启停操作。
//
// 分工：这里只管磁盘与配置（清单、体积、启停状态），运行时的加载与注册在前端
// （frontend/src/plugin/loader.ts）；页面把两边拼起来展示。资源投递（/plugins/ 路由）
// 见 plugin_handler.go，本文件不涉及 HTTP。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"nekolauncher/internal/config"
)

const (
	// pluginDisabledConfigKey 被停用的插件 id 列表（JSON 数组），存 launcher.yaml。
	pluginDisabledConfigKey = "pluginDisabledIDs"
	// pluginInstallMaxBytes / pluginInstallMaxFiles 安装规模上限：误选一个巨大目录时
	// 给出明确错误，而不是闷头复制到磁盘写满。
	pluginInstallMaxBytes = 256 << 20
	pluginInstallMaxFiles = 20000
	// pluginIDMaxLength 插件 id 长度上限。
	pluginIDMaxLength = 64
)

// pluginManifest 插件清单（plugins/<id>/plugin.yaml）。
// yaml 标签是磁盘格式（作者编辑的文件）；json 标签是 /plugins 索引与
// 编辑绑定的规范化输出（前端只见 JSON，永远不解析 YAML）。
type pluginManifest struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version" json:"version"`
	APIVersion  string `yaml:"api" json:"apiVersion"`
	Description string `yaml:"description,omitempty" json:"description"`
	Author      string `yaml:"author,omitempty" json:"author"`
	Entry       string `yaml:"entry,omitempty" json:"entry"`
	// Icon 图标文件名（相对插件目录），缺省 icon.png。
	Icon string `yaml:"icon,omitempty" json:"icon"`
	// Dev 开发模式：入口是 JSX 源码，由前端运行时编译加载（loader.ts 的 dev 分支）。
	Dev bool `yaml:"dev,omitempty" json:"dev"`
	// Styles 声明的样式文件（相对插件目录、须为 .css）：宿主加载插件时注入到
	// 全局 <style>，可自定义任意控件的样式；卸载/停用时由前端整体移除。
	// 路径合法性（相对路径、不含 ..）由前端注入前再校验一次。
	Styles []string `yaml:"styles,omitempty" json:"styles"`
	// Capabilities 能力声明（storage / launch…）。宿主侧只透传，前端据此决定挂载哪些 API。
	Capabilities map[string]bool `yaml:"capabilities,omitempty" json:"capabilities"`
	// Settings 默认设置：首次加载时由前端种入插件 config（键为插件视角的裸键）。
	Settings map[string]string `yaml:"settings,omitempty" json:"settings"`
	// LaunchTransform 启动贡献（v1：仅 JVM / 游戏参数的**追加**）。
	//
	// 这是「插件 → 启动管线」的唯一通道，刻意做得很窄：
	//   - 只支持追加参数，不支持改主类 / 换 Java / 动 classpath
	//     （那些能力在 launch.MinecraftLaunchTransform 里存在，但开放给插件
	//     等于把"启动一个不是 Minecraft 的进程"变成一行 YAML）；
	//   - 需要 capabilities.launch-transform = true，且前端会在启动前展示
	//     "这个插件想给本次启动加什么"让用户确认。
	LaunchTransform *pluginLaunchTransform `yaml:"launchTransform,omitempty" json:"launchTransform,omitempty"`
}

// pluginLaunchTransform 插件声明的启动贡献。
type pluginLaunchTransform struct {
	// AppendJvmArguments 追加到 JVM 段末尾（主类之前）。支持启动器占位符。
	AppendJvmArguments []string `yaml:"appendJvmArguments,omitempty" json:"appendJvmArguments"`
	// AppendGameArguments 追加到游戏参数末尾。
	AppendGameArguments []string `yaml:"appendGameArguments,omitempty" json:"appendGameArguments"`
}

// pluginEntryName 入口文件缺省名（清单 entry 字段可改写）。
const pluginEntryName = "index.js"

// entryFile 清单声明的入口文件，未声明时用缺省名。
func (m *pluginManifest) entryFile() string {
	if strings.TrimSpace(m.Entry) == "" {
		return pluginEntryName
	}
	return m.Entry
}

// styleFiles 规范化清单声明的样式文件列表：去空白、去空项，只保留指向
// 插件目录内 .css 文件的相对路径（挡掉绝对路径与 ".." 越界写法）。
func (m *pluginManifest) styleFiles() []string {
	files := make([]string, 0, len(m.Styles))
	for _, declared := range m.Styles {
		normalized := filepath.ToSlash(strings.TrimSpace(declared))
		if normalized == "" || !strings.HasSuffix(strings.ToLower(normalized), ".css") {
			continue
		}
		if strings.HasPrefix(normalized, "/") || strings.Contains(normalized, "..") {
			continue
		}
		files = append(files, normalized)
	}
	return files
}

// PluginInfo 插件页展示的一项：清单字段 + 磁盘状态。
// ManifestError 非空表示 plugin.yaml 缺失或解析失败，此时清单字段为空、但目录仍会列出，
// 便于在页面上暴露装坏了的插件。
type PluginInfo struct {
	ID          string
	Name        string
	Version     string
	Author      string
	Description string
	APIVersion  string
	Entry       string
	// IconFile 图标文件名（相对插件目录）；目录里没有图标时为空。
	IconFile   string
	Directory  string
	SizeBytes  int64
	FileCount  int
	ModifiedAt int64 // Unix 秒
	Disabled   bool
	// Dev 开发模式插件（入口为 JSX 源码，运行时编译），插件页可用它打标。
	Dev bool
	// Capabilities 已声明的权限键（只取值为 true 的，按字母排序），插件页展示用。
	Capabilities  []string
	ManifestError string
}

// PluginAPI 插件管理绑定。root 与 disabled 仅在测试时注入；正常使用留空即走
// 默认实现（存储目录 / launcher.yaml）。Startup 注入 ctx 后会顺带启动样式
// 热更新轮询（见 plugin_style_watcher.go）。
type PluginAPI struct {
	ctx      context.Context
	root     string
	disabled pluginDisabledStore
	// usageMu / usageCache 目录体积统计的缓存（见 pluginDirectoryUsage）。
	usageMu    sync.Mutex
	usageCache map[string]pluginUsage
	// installMu 安装/卸载互斥：存在性检查与落盘不是原子的。
	installMu sync.Mutex
}

// Startup 注入 Wails runtime ctx，并启动插件样式热更新轮询。
func (a *PluginAPI) Startup(ctx context.Context) {
	a.ctx = ctx
	go a.watchStyles(ctx)
}

// pluginDisabledStore 停用列表的存取。抽出来是为了让测试不必写用户的 launcher.yaml。
type pluginDisabledStore interface {
	IDs() map[string]bool
	Set(id string, disabled bool)
}

// configDisabledStore 默认实现：停用列表存在 launcher.yaml 的 pluginDisabledConfigKey。
type configDisabledStore struct{}

func (configDisabledStore) IDs() map[string]bool { return pluginDisabledIDs() }

func (configDisabledStore) Set(id string, disabled bool) { setPluginDisabled(id, disabled) }

// directory 插件根目录。
func (a *PluginAPI) directory() string {
	if a.root != "" {
		return a.root
	}
	return pluginRootDirectory()
}

// disabledStore 停用列表存取实现。
func (a *PluginAPI) disabledStore() pluginDisabledStore {
	if a.disabled != nil {
		return a.disabled
	}
	return configDisabledStore{}
}

// EnsurePluginsDirectory 确保插件目录存在并返回路径；安装与"打开目录"前调用。
func (a *PluginAPI) EnsurePluginsDirectory() (string, error) {
	root := a.directory()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}

// ListPlugins 列出插件目录下的全部子目录。目录不存在（还没装过插件）返回空列表。
func (a *PluginAPI) ListPlugins() ([]PluginInfo, error) {
	root := a.directory()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []PluginInfo{}, nil
		}
		return nil, err
	}

	disabled := a.disabledStore().IDs()
	infos := make([]PluginInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		info := PluginInfo{
			ID:        id,
			Directory: filepath.Join(root, id),
			Disabled:  disabled[id],
		}
		info.SizeBytes, info.FileCount, info.ModifiedAt = a.pluginDirectoryUsage(id, info.Directory)

		manifest, manifestErr := readPluginManifest(info.Directory)
		switch {
		case manifestErr != nil:
			info.ManifestError = manifestErr.Error()
		case manifest.ID != id:
			info.ManifestError = fmt.Sprintf("清单里的 id「%s」与目录名不一致", manifest.ID)
		default:
			info.Name = manifest.Name
			info.Version = manifest.Version
			info.Author = manifest.Author
			info.Description = manifest.Description
			info.APIVersion = manifest.APIVersion
			info.Entry = manifest.entryFile()
			info.IconFile = resolvePluginIcon(info.Directory, manifest.Icon)
			info.Dev = manifest.Dev
			for name, enabled := range manifest.Capabilities {
				if enabled {
					info.Capabilities = append(info.Capabilities, name)
				}
			}
			sort.Strings(info.Capabilities)
		}
		infos = append(infos, info)
	}

	sort.Slice(infos, func(left, right int) bool { return infos[left].ID < infos[right].ID })
	return infos, nil
}

// InstallPluginDirectory 把 sourceDirectory 安装为插件目录下的 <清单 id>，返回插件 id。
// 不做覆盖：同名插件已存在、清单不可用、id 不合法或规模超限都会返回错误。
// 分发场景请用 InstallPluginArchive（.nekoex 包）。
//
// 安装/卸载共享 installMu：存在性检查与落盘不是原子的，两个并发安装同名
// 插件会都通过检查、交错写入同一目录（绑定调用可并发，前端 busy 互斥只管 UI 路径）。
func (a *PluginAPI) InstallPluginDirectory(sourceDirectory string) (string, error) {
	a.installMu.Lock()
	defer a.installMu.Unlock()

	return a.installPluginDirectory(sourceDirectory)
}

func (a *PluginAPI) installPluginDirectory(sourceDirectory string) (string, error) {
	source := filepath.Clean(strings.TrimSpace(sourceDirectory))
	if source == "" || source == "." {
		return "", errors.New("未选择插件目录")
	}
	stat, err := os.Stat(source)
	if err != nil || !stat.IsDir() {
		return "", fmt.Errorf("插件目录不可用：%s", sourceDirectory)
	}

	manifest, err := readPluginManifest(source)
	if err != nil {
		return "", err
	}
	if err := validatePluginID(manifest.ID); err != nil {
		return "", err
	}

	root := a.directory()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	if isSameOrInside(root, source) {
		return "", errors.New("所选目录已在插件目录内，无需重复安装")
	}

	target := filepath.Join(root, manifest.ID)
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("已存在同名插件「%s」，请先卸载", manifest.ID)
	}

	if err := copyPluginTree(source, target); err != nil {
		// 半途失败不留残缺目录，否则页面上会出现一个装了一半的插件
		os.RemoveAll(target)
		return "", err
	}
	return manifest.ID, nil
}

// UninstallPlugin 删除插件目录。id 必须是插件目录下的直接子目录名。
// 与安装共享 installMu，避免卸载与安装交错操作同一插件目录。
func (a *PluginAPI) UninstallPlugin(id string) error {
	a.installMu.Lock()
	defer a.installMu.Unlock()

	if err := validatePluginID(id); err != nil {
		return err
	}
	target := filepath.Join(a.directory(), id)
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("插件不存在：%s", id)
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	// 顺手从停用列表里摘掉，避免卸载后残留一条无主记录
	a.disabledStore().Set(id, false)
	// 清掉插件写在 launcher.yaml 的 "<id>:*" 配置键：残留的话重装同名插件
	// 会读到旧值，与"全新安装"预期不符
	config.UpdateInTransaction(func(items map[string]any) bool {
		changed := false
		prefix := id + ":"
		for key := range items {
			if strings.HasPrefix(key, prefix) {
				delete(items, key)
				changed = true
			}
		}
		return changed
	})
	// 卸载后体积缓存里的旧条目失效，直接丢弃
	a.usageMu.Lock()
	delete(a.usageCache, id)
	a.usageMu.Unlock()

	return nil
}

// SetPluginDisabled 记录插件启停状态。这里只写配置，运行时把注册项摘掉/重新加载由
// 前端负责（见 frontend/src/plugin/loader.ts）。
func (a *PluginAPI) SetPluginDisabled(id string, disabled bool) error {
	if err := validatePluginID(id); err != nil {
		return err
	}
	a.disabledStore().Set(id, disabled)
	return nil
}

// ---- 内部工具 ----

// validatePluginID 插件 id 只允许字母、数字、连字符与下划线（不含点，杜绝路径穿越），
// 长度 1..pluginIDMaxLength。
func validatePluginID(id string) error {
	if id == "" {
		return errors.New("插件 id 为空")
	}
	if len(id) > pluginIDMaxLength {
		return fmt.Errorf("插件 id 过长（上限 %d 字符）", pluginIDMaxLength)
	}
	for _, char := range id {
		switch {
		case char >= 'a' && char <= 'z',
			char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9',
			char == '-', char == '_':
		default:
			return fmt.Errorf("插件 id「%s」含有非法字符：%q", id, char)
		}
	}
	return nil
}

// readPluginManifest 读取并解析插件清单；文件缺失或 YAML 非法时返回错误。
// 文案统一用 pluginManifestName 拼，避免改名时漏掉某一处。
func readPluginManifest(directory string) (*pluginManifest, error) {
	raw, err := os.ReadFile(filepath.Join(directory, pluginManifestName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("缺少 " + pluginManifestName)
		}
		return nil, fmt.Errorf("读取 %s 失败：%w", pluginManifestName, err)
	}
	manifest, err := parsePluginManifest(raw)
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

// parsePluginManifest 解析 YAML 清单文本。
func parsePluginManifest(raw []byte) (*pluginManifest, error) {
	var manifest pluginManifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("%s 不是合法 YAML：%w", pluginManifestName, err)
	}
	if manifest.ID == "" {
		return nil, errors.New(pluginManifestName + " 缺少 id")
	}
	return &manifest, nil
}

// writePluginManifestYAML 把清单序列化为 YAML 文本（带末尾换行）。
// 版本字段等必须带引号的约束由调用方校验错误兜底（见 docs/Extensions_Guide.md §4）。
func writePluginManifestYAML(manifest pluginManifest) ([]byte, error) {
	raw, err := yaml.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// resolvePluginIcon 解析插件图标文件名：清单未声明时用缺省 icon.png；文件不存在返回空串。
// 返回值一定是相对路径且不含 ".."，避免指向插件目录之外。
func resolvePluginIcon(directory, declared string) string {
	name := strings.TrimSpace(declared)
	if name == "" {
		name = pluginIconName
	}
	normalized := filepath.ToSlash(name)
	if strings.HasPrefix(normalized, "/") || strings.Contains(normalized, "..") {
		return ""
	}
	info, err := os.Stat(filepath.Join(directory, filepath.FromSlash(name)))
	if err != nil || info.IsDir() {
		return ""
	}
	return normalized
}

// pluginDisabledIDs 读取停用列表；配置损坏时按"都没停用"处理，避免插件全部消失。
func pluginDisabledIDs() map[string]bool {
	disabled := make(map[string]bool)
	raw := config.GetValue(pluginDisabledConfigKey)
	if strings.TrimSpace(raw) == "" {
		return disabled
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return disabled
	}
	for _, id := range ids {
		disabled[id] = true
	}
	return disabled
}

// setPluginDisabled 更新停用列表并落盘。
func setPluginDisabled(id string, disabled bool) {
	ids := pluginDisabledIDs()
	if disabled {
		ids[id] = true
	} else {
		delete(ids, id)
	}
	list := make([]string, 0, len(ids))
	for current := range ids {
		list = append(list, current)
	}
	sort.Strings(list)
	encoded, err := json.Marshal(list)
	if err != nil {
		return
	}
	config.SetValue(pluginDisabledConfigKey, string(encoded))
}

// pluginUsageCacheSize 缓存的插件条目上限：插件数量远小于此，超出即整体失效。
const pluginUsageCacheSize = 256

// pluginUsageCacheTTL 缓存有效期：目录自身的 modtime 只在直接子项增删时变化，
// 子目录深处改文件不会触发失效，加 TTL 兜底（页面上只是体积/时间展示，允许秒级陈旧）。
const pluginUsageCacheTTL = 30 * time.Second

// pluginUsage 缓存的一次目录统计结果；dirModTime + 采集时间用于失效判断。
type pluginUsage struct {
	sizeBytes  int64
	fileCount  int
	modifiedAt int64
	dirModTime int64
	fetchedAt  time.Time
}

// pluginDirectoryUsage 统计目录体积、文件数与最近修改时间（Unix 秒）。
// 结果按插件目录缓存（以目录自身 modtime 失效）：ListPlugins 每次刷新页面
// 都会调用，插件目录可能有几万文件，全量 Walk 不能每次都来一遍。缓存挂在
// PluginAPI 实例上（绑定层调用是并发的，加锁）。
func (a *PluginAPI) pluginDirectoryUsage(id, directory string) (int64, int, int64) {
	dirInfo, err := os.Stat(directory)
	if err != nil {
		return 0, 0, 0
	}
	dirModTime := dirInfo.ModTime().UnixNano()

	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	// 缓存惰性初始化：PluginAPI 常以零值字面量构造（绑定注册 / 测试）
	if a.usageCache == nil {
		a.usageCache = make(map[string]pluginUsage)
	} else if len(a.usageCache) > pluginUsageCacheSize {
		a.usageCache = make(map[string]pluginUsage)
	}
	if cached, ok := a.usageCache[id]; ok && cached.dirModTime == dirModTime &&
		time.Since(cached.fetchedAt) < pluginUsageCacheTTL {
		return cached.sizeBytes, cached.fileCount, cached.modifiedAt
	}

	size, count, newest := walkPluginDirectoryUsage(directory)
	a.usageCache[id] = pluginUsage{
		sizeBytes:  size,
		fileCount:  count,
		modifiedAt: newest,
		dirModTime: dirModTime,
		fetchedAt:  time.Now(),
	}

	return size, count, newest
}

// walkPluginDirectoryUsage 全量 Walk 统计目录体积、文件数与最近修改时间（Unix 秒）。
func walkPluginDirectoryUsage(directory string) (int64, int, int64) {
	var size int64
	var count int
	var newest int64
	_ = filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		size += info.Size()
		count++
		if stamp := info.ModTime().Unix(); stamp > newest {
			newest = stamp
		}
		return nil
	})
	return size, count, newest
}

// isSameOrInside 判断 target 是否就是 root 本身或位于 root 之内。
func isSameOrInside(root, target string) bool {
	rootAbsolute, err1 := filepath.Abs(root)
	targetAbsolute, err2 := filepath.Abs(target)
	if err1 != nil || err2 != nil {
		return false
	}
	if strings.EqualFold(rootAbsolute, targetAbsolute) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(targetAbsolute), strings.ToLower(rootAbsolute)+string(filepath.Separator))
}

// copyPluginTree 递归复制目录，并把规模限制在插件安装上限内。
func copyPluginTree(source, target string) error {
	var bytesCopied int64
	var filesCopied int

	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)

		if info.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		// 符号链接等非常规文件直接跳过，避免复制出意外内容
		if !info.Mode().IsRegular() {
			return nil
		}
		filesCopied++
		if filesCopied > pluginInstallMaxFiles {
			return fmt.Errorf("插件文件数超过上限（%d）", pluginInstallMaxFiles)
		}
		bytesCopied += info.Size()
		if bytesCopied > pluginInstallMaxBytes {
			return fmt.Errorf("插件体积超过上限（%d MB）", pluginInstallMaxBytes>>20)
		}
		return copyPluginFile(path, destination, info.Mode())
	})
}

// copyPluginFile 复制单个文件并保留可执行位。
func copyPluginFile(source, destination string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ---- 插件受限的宿主动作 ----

// OpenPluginPath 用系统默认程序打开**插件自己目录内**的文件或目录。
// "打开"是显式 API 里权力最大的动作（FileProtocolHandler 打开可执行文件
// 约等于启动它），这里把它锁进插件目录，杜绝任意路径的执行原语。
func (a *PluginAPI) OpenPluginPath(pluginID, path string) error {
	if err := validatePluginID(pluginID); err != nil {
		return err
	}
	normalized := filepath.Clean(strings.TrimSpace(path))
	if normalized == "" || normalized == "." {
		return errors.New("路径为空")
	}
	if !isSameOrInside(filepath.Join(a.directory(), pluginID), normalized) {
		return fmt.Errorf("只允许打开插件目录内的文件：%s", path)
	}
	return openWithSystemApp(normalized)
}
