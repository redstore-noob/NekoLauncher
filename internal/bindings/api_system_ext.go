package bindings

// SystemAPI 扩展：原生对话框、资源管理器 / 外部打开。
// 与 api_system.go 分离存放，避免与其他改动冲突。

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"nekolauncher/internal/instance"
	"nekolauncher/internal/tools"
)

// ---- PNG 写盘 ----

// pngMagic PNG 文件签名；写入前校验，避免把别的东西存成 .png。
var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

// WritePngFile 把前端（如皮肤编辑器 canvas）导出的 base64 PNG 写入目标路径。
// 接受裸 base64 或 data URI（data:image/png;base64,...）；仅接受 PNG。
func (a *SystemAPI) WritePngFile(path, base64Png string) error {
	target := tools.SanitizeSavePath(filepath.Clean(strings.TrimSpace(path)))
	if target == "" {
		return errors.New("目标路径为空")
	}
	encoded := base64Png
	if index := strings.IndexByte(encoded, ','); index >= 0 && strings.HasPrefix(encoded[:index], "data:") {
		encoded = encoded[index+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("PNG 数据解码失败：%w", err)
	}
	if len(raw) < len(pngMagic) || !bytes.Equal(raw[:len(pngMagic)], pngMagic) {
		return errors.New("数据不是 PNG 图片")
	}

	return os.WriteFile(target, raw, 0o644)
}

// WriteTextFile 把文本内容写入目标路径（UTF-8，覆盖）。供创作工具的"导出输出"等使用。
func (a *SystemAPI) WriteTextFile(path, content string) error {
	target := tools.SanitizeSavePath(filepath.Clean(strings.TrimSpace(path)))
	if target == "" {
		return errors.New("目标路径为空")
	}
	if len(content) > 16<<20 {
		return errors.New("内容过大（上限 16 MB）")
	}

	return os.WriteFile(target, []byte(content), 0o644)
}

// ReadTextFile 读取目标路径的文本内容（UTF-8）。供游戏设置编辑器等使用。
func (a *SystemAPI) ReadTextFile(path string) (string, error) {
	target := filepath.Clean(strings.TrimSpace(path))
	if target == "" {
		return "", errors.New("目标路径为空")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	if len(data) > 16<<20 {
		return "", errors.New("文件过大（上限 16 MB）")
	}
	return string(data), nil
}

// SystemFileEntry 目录条目（供 AI 助手浏览实例文件夹）。
type SystemFileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	IsDir      bool   `json:"isDir"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
}

// ListDirectory 列出目录内容，目录在前、按名称排序。供 AI 助手自主
// 浏览实例目录（崩溃排查、找配置文件等）时使用；只读，不限制根目录，
// 与 ReadTextFile 的暴露面一致。
func (a *SystemAPI) ListDirectory(dir string) ([]SystemFileEntry, error) {
	target := filepath.Clean(strings.TrimSpace(dir))
	if target == "" {
		return nil, errors.New("目标路径为空")
	}
	dirEntries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	entries := make([]SystemFileEntry, 0, len(dirEntries))
	for _, e := range dirEntries {
		info, err := e.Info()
		if err != nil {
			continue // 文件可能在枚举间隙被删除，跳过即可
		}
		modified := ""
		if !info.ModTime().IsZero() {
			modified = info.ModTime().Format("2006-01-02 15:04")
		}
		entries = append(entries, SystemFileEntry{
			Name:       e.Name(),
			Path:       filepath.Join(target, e.Name()),
			IsDir:      e.IsDir(),
			Size:       info.Size(),
			ModifiedAt: modified,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

// ---- 设备标识 ----

var (
	deviceIDOnce   sync.Once
	deviceIDCached string
)

// GetDeviceId 稳定设备码：Windows 读注册表 MachineGuid，其他平台回退主机名+用户名，
// 统一做 SHA-256 摘要后返回十六进制（不暴露原始 GUID）。供前端做"按设备区分"的
// 本地随机种子（如今日运势）等场景；进程内只计算一次。
func (a *SystemAPI) GetDeviceId() string {
	deviceIDOnce.Do(func() {
		deviceIDCached = digestDeviceSeed(rawDeviceSeed())
	})
	return deviceIDCached
}

// ---- 原生对话框 ----

// SelectDirectory 打开目录选择对话框，返回选中目录（取消返回空串）。
func (a *SystemAPI) SelectDirectory(title string) (string, error) {
	return wailsruntime.OpenDirectoryDialog(callCtx(a.ctx), wailsruntime.OpenDialogOptions{
		Title: title,
	})
}

// SelectFile 打开文件选择对话框；filterName/pattern 组成文件类型过滤器，
// pattern 形如 "*.png;*.jpg"（可含多段）。取消返回空串。
func (a *SystemAPI) SelectFile(title, filterName, pattern string) (string, error) {
	opts := wailsruntime.OpenDialogOptions{Title: title}
	if pattern != "" {
		opts.Filters = []wailsruntime.FileFilter{{DisplayName: filterName, Pattern: pattern}}
	}
	return wailsruntime.OpenFileDialog(callCtx(a.ctx), opts)
}

// SaveFile 打开保存文件对话框；defaultName 为默认文件名。取消返回空串。
// 默认名与最终路径都过一遍 tools.SanitizeSavePath：整合包名/版本号等用户输入
// 带 '?' 时系统会拒绝这个名字，先换成 '0' 才不会"点了保存什么都没存下来"。
func (a *SystemAPI) SaveFile(title, defaultName, filterName, pattern string) (string, error) {
	defaultName = tools.SanitizeSaveName(defaultName)
	opts := wailsruntime.OpenDialogOptions{Title: title, DefaultFilename: defaultName}
	if pattern != "" {
		opts.Filters = []wailsruntime.FileFilter{{DisplayName: filterName, Pattern: pattern}}
	}
	destination, err := wailsruntime.SaveFileDialog(callCtx(a.ctx), wailsruntime.SaveDialogOptions{
		Title:           opts.Title,
		DefaultFilename: defaultName,
		Filters:         opts.Filters,
	})
	if err != nil {
		return "", err
	}
	return tools.SanitizeSavePath(destination), nil
}

// ---- 打开资源管理器 / 外部程序 ----

// OpenInExplorer 在系统文件管理器中定位 path（文件或目录）。
// Windows: explorer /select,<path>；macOS: open -R；Linux: xdg-open 其所在目录。
// PORTING_NOTES：Linux 无通用 "选中" 语义，退化为打开父目录。
func (a *SystemAPI) OpenInExplorer(path string) error {
	if path == "" {
		return errEmptyPath
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// 注意 "/select," 与路径分开传参，exec 会做引号转义，含空格路径安全。
		cmd = exec.Command("explorer", "/select,", path)
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	return startDetached(cmd)
}

// startDetached 启动外部程序并回收子进程句柄：只 Start 不 Wait 会一直占着
// 进程句柄（Windows 上尤其明显，长时间运行会累积）。
func startDetached(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// OpenPath 用系统默认程序打开文件或目录（Windows: rundll32 url.dll,FileProtocolHandler，
// 参数经 exec 转义，路径含空格安全；macOS: open；Linux: xdg-open）。
func (a *SystemAPI) OpenPath(path string) error {
	return openWithSystemApp(path)
}

// openWithSystemApp 系统默认程序打开的 OS 分派；OpenPath 与插件的受限打开共用。
func openWithSystemApp(path string) error {
	if path == "" {
		return errEmptyPath
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return startDetached(cmd)
}

// SetClipboard 写入系统剪贴板文本。
func (a *SystemAPI) SetClipboard(text string) error {
	return wailsruntime.ClipboardSetText(a.ctx, text)
}

// ---- 截图墙 ----

// ListScreenshots 汇总全部实例的截图，按修改时间降序返回前 max 张。
// 目录解析复用 instance.ListScreenshots（版本隔离感知），共享目录按内容目录
// 去重只取一次；单个目录读取失败只跳过，不整体报错。
func (a *SystemAPI) ListScreenshots(max int) []instance.ScreenshotInfo {
	if max <= 0 {
		max = 9
	}
	snapshot := instance.CurrentSnapshot()
	if snapshot.IsLoading ||
		strings.TrimSpace(snapshot.ErrorMessage) != "" ||
		strings.TrimSpace(snapshot.MinecraftDirectory) == "" {
		return []instance.ScreenshotInfo{}
	}

	// 共享目录的多个实例只扫一次；归属记到首个（列表序靠前）实例名下
	seen := map[string]bool{}
	var ownerVersions []string
	for _, versionID := range snapshot.VersionIds {
		directory := instance.GameVersionIsolationGetContentDirectory(snapshot, versionID)
		if strings.TrimSpace(directory) == "" {
			continue
		}
		key := directory
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		ownerVersions = append(ownerVersions, versionID)
	}

	shots := make([]instance.ScreenshotInfo, 0)
	for _, versionID := range ownerVersions {
		shots = append(shots, instance.ListScreenshots(versionID)...)
	}

	sort.Slice(shots, func(left, right int) bool {
		return shots[left].ModifiedAt.After(shots[right].ModifiedAt)
	})
	if len(shots) > max {
		shots = shots[:max]
	}
	return shots
}

// ---- 图片查看器：另存为 ----

// SaveFileAs 打开「另存为」对话框，把 sourcePath 的文件原样复制到用户选择
// 的位置（图片查看器使用）。返回最终写入的目标路径；用户取消返回空串。
func (a *SystemAPI) SaveFileAs(sourcePath, defaultName, filterName, pattern string) (string, error) {
	source := filepath.Clean(strings.TrimSpace(sourcePath))
	if source == "" {
		return "", errEmptyPath
	}
	if info, err := os.Stat(source); err != nil || info.IsDir() {
		return "", fmt.Errorf("源文件不存在：%s", source)
	}

	opts := wailsruntime.SaveDialogOptions{
		Title:           "图片另存为",
		DefaultFilename: tools.SanitizeSaveName(defaultName),
	}
	if pattern != "" {
		opts.Filters = []wailsruntime.FileFilter{{DisplayName: filterName, Pattern: pattern}}
	}
	destination, err := wailsruntime.SaveFileDialog(callCtx(a.ctx), opts)
	if err != nil {
		return "", err
	}
	if destination == "" {
		return "", nil
	}
	destination = tools.SanitizeSavePath(destination)

	if err := copyFileContents(source, destination); err != nil {
		return "", err
	}
	return destination, nil
}

// copyFileContents 按字节复制文件内容；目标存在时覆盖。
func copyFileContents(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
