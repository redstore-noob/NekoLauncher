package bindings

// scene.pkg 读取:Wallpaper Engine 场景壁纸把全部资源(场景描述、模型、材质、
// 纹理、粒子、着色器、音频)打包进 scene.pkg。
//
// PKGV 布局(按实际壁纸包 + RePKG 逆向结论对照确认):
//
//	字符串 = int32 字节长度 + UTF-8 内容
//	头部   = 字符串 magic("PKGV" + 4 位版本号)+ int32 条目总数
//	条目   = 字符串 路径 + int32 数据偏移 + int32 数据长度
//	         长度为 0 表示目录,后接 int32 子项数量与子项条目
//	偏移   = 相对**数据区起点**(即目录表结束位置),不是绝对文件偏移!
//	         绝对位置 = 目录表长度 + 偏移。旧实现按绝对偏移读取,恰好靠
//	         PNG 签名扫描在错位数据里"歪打正着"找到内嵌图,本实现修正。
//
// 本文件提供两类能力:
//  1. weScenePackageImage:静态提取——挑包内最大的一张贴图作为背景兜底图(带缓存);
//  2. wePkgReader:随机访问——按条目路径读取任意资源,供 /wescene 路由做完整渲染。

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"nekolauncher/internal/config"
	"nekolauncher/internal/tools"
)

const (
	// wePkgMagicPrefix pkg 头部 magic 前缀,后接 4 位版本号(如 PKGV0022)。
	wePkgMagicPrefix = "PKGV"
	// wePkgHeadLimit 解析目录表时读取的头部上限:目录表本身只有几 KB,
	// 留足余量同时避免为解析目录而读入整个包。
	wePkgHeadLimit = 4 << 20
	// wePkgEntryLimit 目录条目数上限,防御损坏文件导致的越界扩张。
	wePkgEntryLimit = 1 << 17
	// wePkgScanLimit 单次提取最多读取的条目字节数,避免超大包把启动拖住。
	wePkgScanLimit = 128 << 20
	// wePkgMinEntryBytes 小于此长度的条目装不下能铺满屏的图,跳过以减少读盘;
	// 阈值刻意压低——纯色原画压成 PNG 可能只有几十 KB,宁可多扫也不漏掉原画。
	wePkgMinEntryBytes = 4 << 10
	// wePkgDominanceRatio 主体占优倍数:最大贴图面积需达到次大者的该倍数,才认定它
	// 就是整幅原画(见 extractWEPkgWallpaperImage 末尾的判定)。
	// 原值 2 要求 best 面积 >= runnerUp * 2(即 best 是次大的 2 倍)
	// 这对分层场景(人物、背景面积接近)过于严格,很多壁纸会被拒绝。
	// 降低到 1 后变成 best 面积 >= runnerUp * 1,即只要 best 不小于次大即可。
	wePkgDominanceRatio = 1
)

// pngSignature / jpegSignature 内嵌图片的起始特征。
var (
	pngSignature  = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	jpegSignature = []byte{0xFF, 0xD8, 0xFF}
	// pngEndChunk IEND 块类型;JPEG 结束标记见 jpegEndMarker。
	pngEndChunk    = []byte("IEND")
	jpegEndMarker  = []byte{0xFF, 0xD9}
	weTexExtension = ".tex"
)

// wePkgAuxiliaryNames 辅助贴图的路径片段:遮罩、法线、粗糙度等,都不是画面本身,
// 参与选图只会挑中更大却无意义的贴图。
var wePkgAuxiliaryNames = []string{
	"mask", "normal", "specular", "_spec", "rough", "metallic", "metal",
	"height", "displace", "gloss", "opacity", "alpha", "noise", "blur",
	"distort", "flow", "gradient_mask", "_ao", "ao_", "depth", "_uv", "uv_",
}

// wePkgAuxiliaryDirs 辅助目录:模型/音频/脚本/特效等,定义上不含画面贴图。
var wePkgAuxiliaryDirs = []string{
	"models/", "sounds/", "sound/", "particles/", "shaders/",
	"scripts/", "effects/", "masks/", "utilities/",
}

// wePkgBackgroundNameHints 指向"整幅底图"的路径片段。分层场景里只有背景层覆盖整块
// 画布,其余层各占一角;面积占比之外再看名字,可避免把分层场景误判成单张原画。
// 刻意不收 "bg" 这类过短片段——它会命中 bgm 等无关名字。
var wePkgBackgroundNameHints = []string{
	"background", "backdrop", "wallpaper", "illust", "背景", "原画", "壁纸",
}

// wePkgEntry scene.pkg 目录表中的一个条目。
type wePkgEntry struct {
	Path   string
	Offset int64
	Length int64
}

// wePkgImage 从包内提取出的候选图片。
type wePkgImage struct {
	Data   []byte
	Ext    string
	Width  int
	Height int
}

// Area 像素面积,用于在候选之间比较清晰度。
func (i *wePkgImage) Area() int { return i.Width * i.Height }

// wePkgCursor 顺序读取 pkg 头部的小游标。
type wePkgCursor struct {
	data []byte
	pos  int
}

func (c *wePkgCursor) readString() (string, bool) {
	if c.pos+4 > len(c.data) {
		return "", false
	}
	length := int(int32(binary.LittleEndian.Uint32(c.data[c.pos:])))
	c.pos += 4
	if length < 0 || length > len(c.data)-c.pos {
		return "", false
	}
	value := string(c.data[c.pos : c.pos+length])
	c.pos += length
	return value, true
}

func (c *wePkgCursor) readInt64() (int64, bool) {
	if c.pos+4 > len(c.data) {
		return 0, false
	}
	value := int64(int32(binary.LittleEndian.Uint32(c.data[c.pos:])))
	c.pos += 4
	return value, true
}

// readWEPkgTOC 解析 pkg 头部,返回目录条目与数据区起点(目录表结束位置)。
// 条目 Offset 均为相对数据区的偏移,读取时需加上返回的第二个值。
func readWEPkgTOC(data []byte) ([]wePkgEntry, int64, error) {
	cursor := &wePkgCursor{data: data}
	magic, ok := cursor.readString()
	if !ok || !strings.HasPrefix(magic, wePkgMagicPrefix) {
		return nil, 0, fmt.Errorf("不是 scene.pkg:magic=%q", magic)
	}
	total, ok := cursor.readInt64()
	if !ok || total <= 0 || total > wePkgEntryLimit {
		return nil, 0, fmt.Errorf("scene.pkg 条目数异常:%d", total)
	}

	entries := make([]wePkgEntry, 0, total)
	for int64(len(entries)) < total {
		before := cursor.pos
		if !readWEPkgEntry(cursor, &entries, total) || cursor.pos == before {
			break
		}
	}
	if len(entries) == 0 {
		return nil, 0, fmt.Errorf("scene.pkg 目录表为空")
	}
	return entries, int64(cursor.pos), nil
}

// readWEPkgEntry 读取一个条目;目录条目会连同其子条目一并展开写入 entries。
func readWEPkgEntry(cursor *wePkgCursor, entries *[]wePkgEntry, total int64) bool {
	if int64(len(*entries)) >= total {
		return false
	}
	path, ok := cursor.readString()
	if !ok {
		return false
	}
	offset, ok1 := cursor.readInt64()
	length, ok2 := cursor.readInt64()
	if !ok1 || !ok2 {
		return false
	}
	*entries = append(*entries, wePkgEntry{Path: path, Offset: offset, Length: length})
	if length != 0 {
		return true
	}
	// 目录:后接子项数量,再递归读取子项
	childCount, ok := cursor.readInt64()
	if !ok || childCount < 0 || childCount > wePkgEntryLimit {
		return false
	}
	for i := int64(0); i < childCount; i++ {
		if !readWEPkgEntry(cursor, entries, total) {
			return false
		}
	}
	return true
}

// wePkgIsAuxiliaryPath 路径是否属于辅助贴图/辅助目录。
func wePkgIsAuxiliaryPath(path string) bool {
	lower := strings.ToLower(path)
	for _, dir := range wePkgAuxiliaryDirs {
		if strings.Contains(lower, dir) {
			return true
		}
	}
	for _, name := range wePkgAuxiliaryNames {
		if strings.Contains(lower, name) {
			return true
		}
	}
	return false
}

// wePkgIsBackgroundName 路径是否像是覆盖整块画布的底图。
func wePkgIsBackgroundName(path string) bool {
	lower := strings.ToLower(path)
	for _, hint := range wePkgBackgroundNameHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// wePkgImageCandidates 从目录条目里筛出可能承载画面的贴图条目:跳过目录、辅助贴图、
// 过小条目,以及相对偏移换算成绝对位置后越界的条目;按长度降序排列,
// 让大贴图优先被读到。
func wePkgImageCandidates(entries []wePkgEntry, tocEnd, fileSize int64) []wePkgEntry {
	candidates := make([]wePkgEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Length < wePkgMinEntryBytes {
			continue
		}
		// 偏移相对数据区:换算绝对位置并校验边界
		if entry.Offset < 0 || tocEnd+entry.Offset+entry.Length > fileSize {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Path))
		if ext != weTexExtension && !isImagePath(entry.Path) {
			continue
		}
		if wePkgIsAuxiliaryPath(entry.Path) {
			continue
		}
		candidates = append(candidates, entry)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Length > candidates[j].Length })
	return candidates
}

// findEmbeddedImage 在条目字节里找出第一张内嵌 PNG/JPEG,截到图片自身的结束标记。
// 截尾只影响文件尾部残留,即使标记缺失也只是多带一段无关字节,不影响解码。
func findEmbeddedImage(data []byte) (wePkgImage, bool) {
	best := wePkgImage{}
	found := false

	if start := bytes.Index(data, pngSignature); start >= 0 {
		payload := data[start:]
		if end := bytes.Index(payload, pngEndChunk); end >= 0 && end+8 <= len(payload) {
			payload = payload[:end+8] // IEND 后的 4 字节 CRC;数据可能在块中途截断
		}
		if width, height, ok := imageDimensions(payload); ok {
			best, found = wePkgImage{Data: payload, Ext: ".png", Width: width, Height: height}, true
		}
	}
	// JPEG 的熵编码数据里 0xFF 必然被转义或作为标记首字节,末尾标记不会误命中
	if start := bytes.Index(data, jpegSignature); start >= 0 {
		payload := data[start:]
		if end := bytes.Index(payload[3:], jpegEndMarker); end >= 0 {
			payload = payload[:3+end+2]
		}
		if width, height, ok := imageDimensions(payload); ok {
			if !found || width*height > best.Area() {
				best, found = wePkgImage{Data: payload, Ext: ".jpg", Width: width, Height: height}, true
			}
		}
	}
	return best, found
}

// imageDimensions 用注册的解码器读出图片尺寸,顺带验证这段字节确实是完整图片。
func imageDimensions(data []byte) (int, int, bool) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

// weImageFileArea 读本地图片文件的像素面积;文件缺失、损坏或非图片时返回 0。
func weImageFileArea(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return 0
	}
	return config.Width * config.Height
}

// extractWEPkgWallpaperImage 从 scene.pkg 中提取分辨率最高的可用贴图;
// 包内没有可用图片时返回 nil。优先走 TEX 正规解码,失败再退回签名扫描。
func extractWEPkgWallpaperImage(pkgPath string) (*wePkgImage, error) {
	reader, err := wePkgOpen(pkgPath)
	if err != nil {
		return nil, err
	}

	scanned := int64(0)
	var best *wePkgImage
	runnerUpArea := 0
	for _, entry := range wePkgImageCandidates(reader.entries, reader.dataStart, reader.size) {
		if scanned+entry.Length > wePkgScanLimit {
			break
		}
		payload, err := reader.readEntry(&entry)
		if err != nil {
			continue
		}
		scanned += entry.Length

		candidate := weExtractImageFromEntry(payload)
		if candidate == nil || candidate.Area() == 0 {
			continue
		}
		if best == nil || candidate.Area() > best.Area() {
			if best != nil {
				runnerUpArea = best.Area()
			}
			best = candidate
		} else if candidate.Area() > runnerUpArea {
			runnerUpArea = candidate.Area()
		}
	}
	if best == nil {
		return nil, nil
	}
	// 判定这张贴图是否就是整幅画面,两个信号取或:面积明显大于其余候选(单张原画),
	// 或名字明确指向底图。分层场景(人物、头发、飘带各自一张,面积相当)两个信号都不满足,
	// 这时宁可回退预览图,也不要拿画面的一角铺满屏。
	// 临时禁用判断,接受所有提取的贴图
	_ = runnerUpArea
	_ = wePkgDominanceRatio
	// if best.Area() < runnerUpArea*wePkgDominanceRatio && !wePkgIsBackgroundName(bestPath) {
	// 	return nil, nil
	// }
	return best, nil
}

// weExtractImageFromEntry 从单个条目字节里解出候选图片:TEX 走正规解码,
// 其余(或 TEX 解码失败)退回 PNG/JPEG 签名扫描。
func weExtractImageFromEntry(payload []byte) *wePkgImage {
	if len(payload) > 18 && string(payload[0:4]) == "TEXV" {
		if decoded, err := weDecodeTex(payload); err == nil {
			ext := ""
			switch decoded.Mime {
			case "image/png":
				ext = ".png"
			case "image/jpeg":
				ext = ".jpg"
			}
			if ext != "" {
				return &wePkgImage{Data: decoded.ImageData, Ext: ext, Width: decoded.Width, Height: decoded.Height}
			}
		}
	}
	if image, ok := findEmbeddedImage(payload); ok {
		return &image
	}
	return nil
}

// weScenePackageImage 返回 scene.pkg 提取结果的缓存文件路径与其像素面积;
// 无包、解析失败或包内无可用图片时返回空路径与 0。
//
// 提取结果按 包路径+修改时间+大小 缓存到存储目录:前端每 15 秒轮询一次当前壁纸,
// 每次重扫几十 MB 的包会把轮询拖住,故命中缓存就直接返回,不碰包内容。
func weScenePackageImage(projectDir string) (string, int) {
	pkgPath := filepath.Join(projectDir, "scene.pkg")
	info, err := os.Stat(pkgPath)
	if err != nil || info.IsDir() {
		return "", 0
	}

	// 缓存扩展名取决于包内图片格式,两种都探一遍即可命中,无需先解包
	cacheBase := filepath.Join(config.StorageDirectory(), "cache", "wallpaper-engine",
		wePkgCacheKey(pkgPath, info))
	for _, ext := range []string{".png", ".jpg"} {
		if cached := cacheBase + ext; tools.FileExists(cached) {
			return cached, weImageFileArea(cached)
		}
	}

	image, err := extractWEPkgWallpaperImage(pkgPath)
	if err != nil || image == nil || image.Area() == 0 {
		return "", 0
	}

	cachePath := cacheBase + image.Ext
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", 0
	}
	tmp := cachePath + ".tmp"
	if err := os.WriteFile(tmp, image.Data, 0o644); err != nil {
		return "", 0
	}
	if err := os.Rename(tmp, cachePath); err != nil {
		os.Remove(tmp)
		return "", 0
	}
	return cachePath, image.Area()
}

// wePkgCacheKey 缓存文件名:包路径与修改时间/大小的摘要,包一变缓存即失效。
func wePkgCacheKey(pkgPath string, info os.FileInfo) string {
	digest := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", strings.ToLower(pkgPath), info.ModTime().UnixNano(), info.Size())))
	return hex.EncodeToString(digest[:])
}

// ---- 随机访问读取器:供 /wescene 资源路由使用 ----

// wePkgReader 解析后的 scene.pkg:目录表一次解析,之后按路径读取任意条目。
// 不持有文件句柄(Windows 上常开句柄会挡住目录清理与 WE 重写包),
// readEntry 时短暂打开文件读取。
type wePkgReader struct {
	path      string
	entries   []wePkgEntry
	byPath    map[string]*wePkgEntry // 小写路径 → 条目
	dataStart int64                  // 数据区起点(目录表结束位置)
	size      int64
}

// wePkgOpen 解析 pkg 目录表。
func wePkgOpen(path string) (*wePkgReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	headSize := info.Size()
	if headSize > wePkgHeadLimit {
		headSize = wePkgHeadLimit
	}
	head := make([]byte, headSize)
	if _, err := file.ReadAt(head, 0); err != nil && headSize > 0 {
		file.Close()
		return nil, err
	}
	entries, tocEnd, err := readWEPkgTOC(head)
	file.Close()
	if err != nil {
		return nil, err
	}

	reader := &wePkgReader{
		path:      path,
		entries:   entries,
		byPath:    make(map[string]*wePkgEntry, len(entries)),
		dataStart: tocEnd,
		size:      info.Size(),
	}
	for i := range reader.entries {
		entry := &reader.entries[i]
		if entry.Length == 0 {
			continue // 目录项
		}
		key := strings.ToLower(entry.Path)
		if _, exists := reader.byPath[key]; !exists {
			reader.byPath[key] = entry
		}
	}
	return reader, nil
}

// lookup 按路径查找条目(大小写与斜杠方向不敏感)。
func (p *wePkgReader) lookup(path string) *wePkgEntry {
	return p.byPath[strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(path, "/"), "\\", "/"))]
}

// readEntry 读取条目内容(偏移相对数据区,这里换算成绝对位置)。
func (p *wePkgReader) readEntry(entry *wePkgEntry) ([]byte, error) {
	start := p.dataStart + entry.Offset
	if start < 0 || start+entry.Length > p.size {
		return nil, fmt.Errorf("条目越界:%s", entry.Path)
	}
	file, err := os.Open(p.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	buffer := make([]byte, entry.Length)
	if _, err := file.ReadAt(buffer, start); err != nil {
		return nil, err
	}
	return buffer, nil
}

// ---- 解析结果缓存:壁纸轮询与资源请求都反复打开同一个包,读目录表很便宜但
// 没必要每次都做;按"路径+修改时间"保留最近几个包的解析结果(无句柄,可长期持有) ----

type wePkgCacheEntry struct {
	reader *wePkgReader
	modKey string
}

var (
	wePkgCacheMu  sync.Mutex
	wePkgCache    = map[string]wePkgCacheEntry{}
	wePkgCacheMax = 4
)

// wePkgOpenCached 打开 pkg,优先复用缓存解析结果(修改时间变了自动重开)。
func wePkgOpenCached(path string) (*wePkgReader, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	modKey := fmt.Sprintf("%d|%d", info.ModTime().UnixNano(), info.Size())

	wePkgCacheMu.Lock()
	defer wePkgCacheMu.Unlock()
	if cached, ok := wePkgCache[path]; ok && cached.modKey == modKey {
		return cached.reader, nil
	}
	reader, err := wePkgOpen(path)
	if err != nil {
		return nil, err
	}
	// 容量控制:简单淘汰最早插入的(壁纸场景里同屏最多两三个包)
	if len(wePkgCache) >= wePkgCacheMax {
		for key := range wePkgCache {
			delete(wePkgCache, key)
			break
		}
	}
	wePkgCache[path] = wePkgCacheEntry{reader: reader, modKey: modKey}
	return reader, nil
}
