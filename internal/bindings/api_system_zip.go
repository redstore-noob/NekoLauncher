package bindings

// SystemAPI 扩展：资源包工程读写（创作中心 · 资源包制作器）。
//
// 前端把整个资源包当作"一组 包内相对路径 → 文件内容"的工程来编辑：
//   - PNG 贴图在画布上产出（base64 PNG）；
//   - 清单 / 模型 / blockstate / lang 等文本直接给明文；
//   - ogg / ttf 等二进制原样给 base64，导出时逐字节还原。
//
// ImportResourcePack 支持从 .zip 或整个资源包目录导入（可选剥离单层顶层目录），
// 按扩展名 + UTF-8 / NUL 探测把每个文件分类为 png / text / binary。
// ExportResourcePack 把文件集原子地打包为 .zip：写临时暂存目录 → 打 zip → 清理，
// 失败不留残缺产物。贴图校验与 WritePngFile 一致（仅 PNG）。

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"nekolauncher/internal/tools"
)

// ResourcePackFile 资源包内单个文件。导出时三选一：PngBase64 / Base64 / Text；
// 导入时会额外填充 Kind（"png" / "text" / "binary"），导出忽略该字段。
// （字段名遵循 Wails 绑定的帕斯卡约定，不加 json 标签。）
type ResourcePackFile struct {
	// Path 包内相对路径（正斜杠），如 assets/minecraft/textures/block/stone.png。
	Path string
	// Kind 文件类型标注：png / text / binary（仅导入结果有意义）。
	Kind string
	// PngBase64 PNG 内容（裸 base64 或 data URI）。
	PngBase64 string
	// Base64 任意二进制内容（裸 base64 或 data URI），用于 ogg / ttf 等原样透传。
	Base64 string
	// Text 文本内容（pack.mcmeta / 模型 JSON / lang 等清单）。
	Text string
}

// ResourcePackProject 导入结果：一个可编辑的资源包工程。
type ResourcePackProject struct {
	// Name 建议的包名（zip 去扩展名 / 目录名）。
	Name string
	// Root 导入来源的绝对路径。
	Root string
	// Files 包内全部文件（已按路径排序）。
	Files []ResourcePackFile
}

const (
	// resourcePackMaxFiles 打包 / 导入的文件数上限。
	resourcePackMaxFiles = 5000
	// resourcePackMaxPngBytes 单张贴图上限（8 MB，够 512x512 还有余）。
	resourcePackMaxPngBytes = 8 << 20
	// resourcePackMaxTextBytes 单个文本上限（清单 / 模型远小于此）。
	resourcePackMaxTextBytes = 2 << 20
	// resourcePackMaxBinaryBytes 单个二进制文件上限（音效等）。
	resourcePackMaxBinaryBytes = 32 << 20
	// resourcePackMaxTotalBytes 解包后总体积上限。
	resourcePackMaxTotalBytes = 256 << 20
	// resourcePackExtension 导出包扩展名。
	resourcePackExtension = ".zip"
	// resourcePackMetaName 资源包清单固定文件名（用于识别顶层目录）。
	resourcePackMetaName = "pack.mcmeta"
)

// resourcePackTextExtensions 明确按文本处理的扩展名（其它按内容探测）。
var resourcePackTextExtensions = map[string]bool{
	".json":       true,
	".mcmeta":     true,
	".txt":        true,
	".md":         true,
	".properties": true,
	".lang":       true,
	".cfg":        true,
	".toml":       true,
	".yml":        true,
	".yaml":       true,
	".xml":        true,
	".svg":        true,
	".mcfunction": true,
	".fsh":        true,
	".vsh":        true,
	".glsl":       true,
	".csv":        true,
	".tsv":        true,
	".ini":        true,
	".html":       true,
	".css":        true,
	".js":         true,
	".ts":         true,
}

// ExportResourcePack 把文件集打包为 .zip 资源包，返回实际写入的路径；
// targetPath 没有 .zip 扩展名时自动补上。文件内容在内存中校验后落暂存目录，
// 打包复用 writePluginArchive（同包通用：walk 目录 → zip 根）。
func (a *SystemAPI) ExportResourcePack(targetPath string, files []ResourcePackFile) (string, error) {
	if len(files) == 0 {
		return "", errors.New("资源包里还没有任何文件")
	}
	if len(files) > resourcePackMaxFiles {
		return "", fmt.Errorf("文件数超过上限（%d）", resourcePackMaxFiles)
	}
	target := tools.SanitizeSavePath(strings.TrimSpace(targetPath))
	if target == "" {
		return "", errors.New("未选择保存位置")
	}
	if !strings.EqualFold(filepath.Ext(target), resourcePackExtension) {
		target += resourcePackExtension
	}

	seen := make(map[string]bool, len(files))
	payloads := make(map[string][]byte, len(files))
	var total int64
	for _, item := range files {
		cleaned, err := cleanArchivePath(item.Path)
		if err != nil || cleaned == "" || strings.HasSuffix(cleaned, "/") {
			return "", fmt.Errorf("包内路径不合法：%q", item.Path)
		}
		// 去重按大小写无关：资源包在 Windows/macOS 上是不区分大小写的文件系统，
		// "A.png" 与 "a.png" 同时存在会互相覆盖，解包结果不确定
		lowered := strings.ToLower(cleaned)
		if seen[lowered] {
			return "", fmt.Errorf("包内路径重复：%s", cleaned)
		}
		seen[lowered] = true

		switch {
		case strings.TrimSpace(item.PngBase64) != "":
			data, err := decodeResourcePackPng(item.PngBase64)
			if err != nil {
				return "", fmt.Errorf("%s：%w", cleaned, err)
			}
			total += int64(len(data))
			payloads[cleaned] = data
		case strings.TrimSpace(item.Base64) != "":
			data, err := decodeResourcePackBase64(item.Base64)
			if err != nil {
				return "", fmt.Errorf("%s：%w", cleaned, err)
			}
			if len(data) > resourcePackMaxBinaryBytes {
				return "", fmt.Errorf("%s 超过单个二进制上限（%d MB）", cleaned, resourcePackMaxBinaryBytes>>20)
			}
			total += int64(len(data))
			payloads[cleaned] = data
		case item.Kind == "text" || item.Text != "":
			data := []byte(item.Text)
			if len(data) > resourcePackMaxTextBytes {
				return "", fmt.Errorf("%s 文本内容超过上限（%d MB）", cleaned, resourcePackMaxTextBytes>>20)
			}
			total += int64(len(data))
			payloads[cleaned] = data
		default:
			return "", fmt.Errorf("%s 缺少文件内容", cleaned)
		}
		if total > resourcePackMaxTotalBytes {
			return "", fmt.Errorf("资源包总体积超过上限（%d MB）", resourcePackMaxTotalBytes>>20)
		}
	}

	staging, err := os.MkdirTemp("", "nekolauncher-rp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	stagingAbsolute, err := filepath.Abs(staging)
	if err != nil {
		return "", err
	}
	for name, data := range payloads {
		destination := filepath.Join(stagingAbsolute, filepath.FromSlash(name))
		if !strings.HasPrefix(destination, stagingAbsolute+string(filepath.Separator)) {
			return "", fmt.Errorf("包内路径越出暂存目录：%q", name)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(destination, data, 0o644); err != nil {
			return "", err
		}
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	file, err := os.Create(target)
	if err != nil {
		return "", err
	}
	writer := zip.NewWriter(file)
	writeErr := writePluginArchive(writer, stagingAbsolute)
	if writeErr == nil {
		writeErr = writer.Close()
	} else {
		writer.Close()
	}
	if writeErr == nil {
		writeErr = file.Close()
	} else {
		file.Close()
	}
	if writeErr != nil {
		os.Remove(target)
		return "", writeErr
	}
	return target, nil
}

// ImportResourcePack 读取 .zip 或资源包目录为一个可编辑工程。zip 若把资源包
// 套在唯一一层顶层目录里（直接压缩文件夹），会自动剥掉该层。
func (a *SystemAPI) ImportResourcePack(sourcePath string) (ResourcePackProject, error) {
	source := filepath.Clean(strings.TrimSpace(sourcePath))
	if source == "" || source == "." {
		return ResourcePackProject{}, errors.New("未选择资源包")
	}
	info, err := os.Stat(source)
	if err != nil {
		return ResourcePackProject{}, fmt.Errorf("资源包不可用：%s", sourcePath)
	}
	if info.IsDir() {
		return importResourcePackDirectory(source)
	}
	return importResourcePackZip(source)
}

// importResourcePackDirectory 从磁盘目录导入。
func importResourcePackDirectory(root string) (ResourcePackProject, error) {
	project := ResourcePackProject{Name: filepath.Base(root), Root: root}
	var total int64
	err := filepath.Walk(root, func(current string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		if len(project.Files) >= resourcePackMaxFiles {
			return fmt.Errorf("文件数超过上限（%d）", resourcePackMaxFiles)
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		limit := int64(resourcePackMaxBinaryBytes)
		if strings.HasSuffix(strings.ToLower(relative), ".png") {
			limit = resourcePackMaxPngBytes
		}
		data, err := readFileLimited(current, limit)
		if err != nil {
			return fmt.Errorf("%s：%w", relative, err)
		}
		total += int64(len(data))
		if total > resourcePackMaxTotalBytes {
			return fmt.Errorf("资源包总体积超过上限（%d MB）", resourcePackMaxTotalBytes>>20)
		}
		project.Files = append(project.Files, buildImportedFile(relative, data))
		return nil
	})
	if err != nil {
		return ResourcePackProject{}, err
	}
	sortResourcePackFiles(project.Files)
	return project, nil
}

// importResourcePackZip 从 .zip 导入；顶层目录检测复用资源包清单名。
func importResourcePackZip(source string) (ResourcePackProject, error) {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return ResourcePackProject{}, fmt.Errorf("资源包不是有效的 zip：%w", err)
	}
	defer reader.Close()

	prefix := resourcePackArchivePrefix(reader.File)
	project := ResourcePackProject{
		Name: strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)),
		Root: source,
	}
	var total int64
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() || !entry.FileInfo().Mode().IsRegular() {
			continue
		}
		cleaned, err := cleanArchivePath(entry.Name)
		if err != nil || cleaned == "" {
			return ResourcePackProject{}, fmt.Errorf("包内路径不合法：%q", entry.Name)
		}
		if prefix != "" {
			if !strings.HasPrefix(cleaned, prefix) {
				continue
			}
			cleaned = strings.TrimPrefix(cleaned, prefix)
		}
		if cleaned == "" {
			continue
		}
		if len(project.Files) >= resourcePackMaxFiles {
			return ResourcePackProject{}, fmt.Errorf("文件数超过上限（%d）", resourcePackMaxFiles)
		}
		// 上限按扩展名区分，与目录导入分支保持一致：低于导出时的 8 MB PNG 校验，
		// 会导致导入成功的工程在导出时被自己的校验拒绝
		limit := int64(resourcePackMaxBinaryBytes)
		if strings.HasSuffix(strings.ToLower(cleaned), ".png") {
			limit = resourcePackMaxPngBytes
		}
		data, err := readZipEntryLimited(entry, limit)
		if err != nil {
			return ResourcePackProject{}, fmt.Errorf("%s：%w", cleaned, err)
		}
		total += int64(len(data))
		if total > resourcePackMaxTotalBytes {
			return ResourcePackProject{}, fmt.Errorf("资源包总体积超过上限（%d MB）", resourcePackMaxTotalBytes>>20)
		}
		project.Files = append(project.Files, buildImportedFile(cleaned, data))
	}
	sortResourcePackFiles(project.Files)
	return project, nil
}

// resourcePackArchivePrefix 判断 zip 内资源包的根前缀：清单在根目录返回 ""；
// 若资源包被套在唯一一层顶层目录里（顶层目录内才有 pack.mcmeta），返回 "<目录>/"。
// 找不到清单时不剥离（允许导入结构不那么规范的包）。
func resourcePackArchivePrefix(entries []*zip.File) string {
	names := make(map[string]bool, len(entries))
	topLevel := make(map[string]bool, 4)
	for _, entry := range entries {
		cleaned, err := cleanArchivePath(entry.Name)
		if err != nil || cleaned == "" {
			continue
		}
		names[cleaned] = true
		if index := strings.Index(cleaned, "/"); index > 0 {
			topLevel[cleaned[:index+1]] = true
		}
	}
	if names[resourcePackMetaName] {
		return ""
	}

	var candidate string
	found := 0
	for prefix := range topLevel {
		if names[prefix+resourcePackMetaName] {
			candidate = prefix
			found++
		}
	}
	if found == 1 {
		return candidate
	}
	return ""
}

// buildImportedFile 按扩展名与内容把原始字节分类为 png / text / binary。
func buildImportedFile(name string, data []byte) ResourcePackFile {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".png") && bytes.HasPrefix(data, pngMagic) {
		return ResourcePackFile{
			Path:      name,
			Kind:      "png",
			PngBase64: base64.StdEncoding.EncodeToString(data),
		}
	}
	if resourcePackTextExtensions[strings.ToLower(filepath.Ext(name))] ||
		(utf8.Valid(data) && !looksBinary(data)) {
		return ResourcePackFile{Path: name, Kind: "text", Text: string(data)}
	}
	return ResourcePackFile{
		Path:   name,
		Kind:   "binary",
		Base64: base64.StdEncoding.EncodeToString(data),
	}
}

// looksBinary 在前 8KB 里找 NUL 字节，作为二进制文件的快速判据。
func looksBinary(data []byte) bool {
	const sniff = 8000
	if len(data) > sniff {
		data = data[:sniff]
	}
	return bytes.IndexByte(data, 0) >= 0
}

// sortResourcePackFiles 稳定按路径排序（正斜杠字典序），让前端树形展示可预期。
func sortResourcePackFiles(files []ResourcePackFile) {
	sort.SliceStable(files, func(left, right int) bool {
		return files[left].Path < files[right].Path
	})
}

// readFileLimited 读取文件并在读取前用大小做限额预判。
func readFileLimited(target string, limit int64) ([]byte, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("文件超过上限（%d MB）", limit>>20)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("文件超过上限（%d MB）", limit>>20)
	}
	return data, nil
}

// readZipEntryLimited 读取单个 zip 条目，按自报大小预判 + LimitReader 兜底。
func readZipEntryLimited(entry *zip.File, limit int64) ([]byte, error) {
	if entry.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("文件超过上限（%d MB）", limit>>20)
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("文件超过上限（%d MB）", limit>>20)
	}
	return data, nil
}

// decodeResourcePackPng 解码并校验 PNG 载荷（与 WritePngFile 相同的格式约束）。
func decodeResourcePackPng(encoded string) ([]byte, error) {
	raw, err := decodeResourcePackBase64(encoded)
	if err != nil {
		return nil, err
	}
	if len(raw) < len(pngMagic) || !bytes.Equal(raw[:len(pngMagic)], pngMagic) {
		return nil, errors.New("数据不是 PNG 图片")
	}
	if len(raw) > resourcePackMaxPngBytes {
		return nil, fmt.Errorf("贴图超过上限（%d MB）", resourcePackMaxPngBytes>>20)
	}
	return raw, nil
}

// decodeResourcePackBase64 解码裸 base64 / data URI 载荷。
func decodeResourcePackBase64(encoded string) ([]byte, error) {
	if index := strings.IndexByte(encoded, ','); index >= 0 && strings.HasPrefix(encoded[:index], "data:") {
		encoded = encoded[index+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("base64 解码失败：%w", err)
	}
	return raw, nil
}
