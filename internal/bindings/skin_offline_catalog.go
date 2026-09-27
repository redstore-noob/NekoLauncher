package bindings

// 离线皮肤目录与内置皮肤贴图解析（对应 C# OfflineSkinCatalog）：
// 内置皮肤清单、从客户端 jar 提取贴图、以及纯代码生成的占位皮肤。

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/launch"
	"nekolauncher/internal/tools"
)

// ---- 常量与 HTTP 客户端 ----

const (
	minecraftProfileEndpoint = "https://api.minecraftservices.com/minecraft/profile"
	minecraftSkinEndpoint    = "https://api.minecraftservices.com/minecraft/profile/skins"
)

// skinHTTPClient 皮肤档案请求共用客户端（对应 C# 30 秒超时 / authlib 15 秒超时，统一取 20 秒）。
var skinHTTPClient = &http.Client{Timeout: 20 * time.Second}

// isMojangRetryableStatus 档案服务可重试的临时状态：429（限流）/503（维护）。
func isMojangRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable
}

// retryAfterDuration 解析 Retry-After（秒或 HTTP 日期）得到退避时长；缺省按指数
// 退避（1s→2s…），并统一封顶，避免同步等待过久卡住前端调用。
func retryAfterDuration(resp *http.Response, attempt int) time.Duration {
	const maxDelay = 5 * time.Second
	if resp != nil {
		if value := strings.TrimSpace(resp.Header.Get("Retry-After")); value != "" {
			if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
				return min(time.Duration(seconds)*time.Second, maxDelay)
			}
			if when, err := http.ParseTime(value); err == nil {
				if wait := time.Until(when); wait > 0 {
					return min(wait, maxDelay)
				}
			}
		}
	}
	delay := time.Duration(1<<attempt) * time.Second
	return min(delay, maxDelay)
}

// doMojangRequest 发送档案服务请求；命中限流/临时错误时按退避重试（最多 3 次），
// 缓解 Minecraft 档案服务直接抛出「返回 429」的问题。带请求体时会经 GetBody 重建。
func doMojangRequest(req *http.Request) (*http.Response, error) {
	const maxAttempts = 3
	for attempt := 0; ; attempt++ {
		resp, err := skinHTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		if attempt >= maxAttempts-1 || !isMojangRetryableStatus(resp.StatusCode) {
			return resp, nil
		}
		delay := retryAfterDuration(resp, attempt)
		resp.Body.Close()
		if req.GetBody != nil {
			body, bodyErr := req.GetBody()
			if bodyErr != nil {
				return nil, bodyErr
			}
			req.Body = body
		}
		time.Sleep(delay)
	}
}

// ---- 离线皮肤目录（对应 C# OfflineSkinCatalog.Choices） ----

// OfflineSkinChoice 离线账号可选的一件内置皮肤。
type OfflineSkinChoice struct {
	// Id 皮肤标识（如 "steve"）。
	Id string `json:"id"`
	// DisplayName 显示名称。
	DisplayName string `json:"displayName"`
	// Model 皮肤模型："classic"（宽手臂）或 "slim"（窄手臂）。
	Model string `json:"model"`
	// FallbackText 图片缺失时的占位文字。
	FallbackText string `json:"fallbackText"`
	// Source 解析好的贴图源：本地路径（已包装为 /localfile URL）或 data URI，
	// 前端可直接作为 <img src> 使用；解析失败为空串。
	Source string `json:"source"`
}

// offlineSkinChoices 内置皮肤清单（顺序即展示顺序，与 C# 一致）。
var offlineSkinChoices = []OfflineSkinChoice{
	{"steve", "Steve", "classic", "S", ""},
	{"alex", "Alex", "slim", "A", ""},
	{"noor", "Noor", "slim", "N", ""},
	{"sunny", "Sunny", "classic", "S", ""},
	{"ari", "Ari", "slim", "A", ""},
	{"zuri", "Zuri", "slim", "Z", ""},
	{"makena", "Makena", "classic", "M", ""},
	{"kai", "Kai", "slim", "K", ""},
	{"efe", "Efe", "slim", "E", ""},
}

// GetOfflineSkinCatalog 列出离线皮肤目录可用皮肤（对应 OfflineSkinPickerDialog 的数据源：
// OfflineSkinCatalog.Choices，9 款内置皮肤 + 已解析的贴图源）。
func (a *AccountAPI) GetOfflineSkinCatalog() []OfflineSkinChoice {
	result := make([]OfflineSkinChoice, len(offlineSkinChoices))
	copy(result, offlineSkinChoices)
	for i := range result {
		result[i].Source = resolveOfflineSkinSourceReady(result[i].Id)
	}
	return result
}

// GetOfflineSkinChoice 按 Id 查目录项（忽略大小写）；未知回退第一项（对应 OfflineSkinCatalog.Get）。
func GetOfflineSkinChoice(id string) OfflineSkinChoice {
	for _, choice := range offlineSkinChoices {
		if strings.EqualFold(choice.Id, id) {
			return choice
		}
	}
	return offlineSkinChoices[0]
}

// resolveOfflineSkinSourceReady 解析贴图源并包装为前端可直接使用的 URL。
func resolveOfflineSkinSourceReady(id string) string {
	source := ResolveOfflineSkinSource(id)
	if source == "" {
		return ""
	}
	if strings.HasPrefix(source, "data:") {
		return source
	}
	return LocalFileURL(source)
}

// LocalFilePathPrefix 前缀：标记 OfflineSkinId 中存放的是自定义皮肤文件路径。
// （C# 只支持内置目录 Id；Wails 版扩展支持本地 PNG 路径，复用同一持久化字段。）

// ---- 离线皮肤贴图解析（对应 OfflineSkinCatalog.ResolveTextureSourceAsync） ----

// resolveGate 串行化解压/生成，避免并发写同一缓存文件。
var resolveGate sync.Mutex

// ResolveOfflineSkinSource 解析离线皮肤贴图来源。优先级：
// 缓存 PNG → 客户端 jar 内置贴图 → 程序生成的占位皮肤（data URI）。
func ResolveOfflineSkinSource(id string) string {
	resolveGate.Lock()
	defer resolveGate.Unlock()
	return resolveOfflineSkinSourceLocked(GetOfflineSkinChoice(id))
}

func resolveOfflineSkinSourceLocked(choice OfflineSkinChoice) string {
	cacheDir := offlineSkinCacheDirectory()
	if cacheDir == "" {
		return generatedSkinDataURI(choice)
	}

	// 1) 命中已解压的缓存 PNG
	cachedPath := filepath.Join(cacheDir, choice.Id+".png")
	if tools.FileExists(cachedPath) {
		return cachedPath
	}

	// 2) 从已安装的客户端 jar 中提取（按修改时间从新到旧尝试）
	if extracted := tryExtractFromClientJars(choice, cacheDir); extracted != "" {
		return extracted
	}

	// 3) 回退到程序生成的占位皮肤
	return generatedSkinDataURI(choice)
}

// offlineSkinCacheDirectory 存储目录/appearance-cache/default-skins（对应 C# 缓存子目录）。
func offlineSkinCacheDirectory() string {
	storage := strings.TrimSpace(config.StorageDirectory())
	if storage == "" {
		return ""
	}
	return filepath.Join(storage, "appearance-cache", "default-skins")
}

// clientJarRoots 可能装有客户端 jar 的根目录：先游戏目录，再默认目录（去重）。
func clientJarRoots() []string {
	var roots []string
	if game := strings.TrimSpace(config.GameDirectory()); game != "" {
		roots = append(roots, game)
	}
	if conventional := launch.GetDefaultMinecraftDirectory(); conventional != "" {
		dup := false
		for _, root := range roots {
			if strings.EqualFold(root, conventional) {
				dup = true
				break
			}
		}
		if !dup {
			roots = append(roots, conventional)
		}
	}
	return roots
}

// tryExtractFromClientJars 遍历 versions/**/*.jar（新到旧），
// 提取皮肤内置贴图到缓存目录；成功返回缓存文件完整路径。
func tryExtractFromClientJars(choice OfflineSkinChoice, cacheDir string) string {
	type jarFile struct {
		path    string
		modTime time.Time
	}
	var jars []jarFile
	for _, root := range clientJarRoots() {
		versionsDir := filepath.Join(root, "versions")
		entries, err := os.Stat(versionsDir)
		if err != nil || !entries.IsDir() {
			continue
		}
		_ = filepath.Walk(versionsDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.EqualFold(filepath.Ext(path), ".jar") {
				return nil
			}
			jars = append(jars, jarFile{path, info.ModTime()})
			return nil
		})
	}
	sort.Slice(jars, func(i, j int) bool { return jars[i].modTime.After(jars[j].modTime) })

	for _, jar := range jars {
		if extracted := tryExtractFromJar(choice, jar.path, cacheDir); extracted != "" {
			return extracted
		}
	}
	return ""
}

// tryExtractFromJar 从单个客户端 jar 提取皮肤贴图；找不到或读取失败返回空串。
func tryExtractFromJar(choice OfflineSkinChoice, jarPath, cacheDir string) string {
	archive, err := openSkinJarReader(jarPath)
	if err != nil {
		return ""
	}
	defer archive.Close()

	// 优先取皮肤对应模型的贴图，取不到再试另一个模型（jar 内固定路径：
	// assets/minecraft/textures/entity/player/<model>/<id>.png）
	preferred := "wide"
	if choice.Model == "slim" {
		preferred = "slim"
	}
	other := "wide"
	if preferred == "wide" {
		other = "slim"
	}
	entryPath := fmt.Sprintf("assets/minecraft/textures/entity/player/%s/%s.png", preferred, choice.Id)
	file, err := archive.Open(entryPath)
	if err != nil {
		entryPath = fmt.Sprintf("assets/minecraft/textures/entity/player/%s/%s.png", other, choice.Id)
		file, err = archive.Open(entryPath)
		if err != nil {
			return ""
		}
	}
	defer file.Close()

	targetPath := filepath.Join(cacheDir, choice.Id+".png")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return ""
	}
	out, err := os.CreateTemp(cacheDir, choice.Id+"-*.tmp")
	if err != nil {
		return ""
	}
	tmpName := out.Name()
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		_ = os.Remove(tmpName)
		return ""
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpName)
		return ""
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		_ = os.Remove(tmpName)
		return ""
	}
	return targetPath
}

// openSkinJarReader 打开客户端 jar（zip 归档）。
func openSkinJarReader(jarPath string) (*zip.ReadCloser, error) {
	return zip.OpenReader(jarPath)
}

// ---- 占位皮肤生成（对应 C# CreateGeneratedTextureDataUri 的像素移植） ----

// generatedSkinDataURI 生成占位皮肤（64×64 PNG data URI）。
func generatedSkinDataURI(choice OfflineSkinChoice) string {
	const textureSize = 64
	img := image.NewNRGBA(image.Rect(0, 0, textureSize, textureSize))
	setPx := func(x, y int, color uint32) {
		index := img.PixOffset(x, y)
		img.Pix[index] = byte(color >> 24)
		img.Pix[index+1] = byte(color >> 16)
		img.Pix[index+2] = byte(color >> 8)
		img.Pix[index+3] = byte(color)
	}

	if strings.EqualFold(choice.Id, "steve") {
		for y, row := range steveHeadPixelRows {
			for x := 0; x < len(row); x += 6 {
				color, ok := parseHexColor(row[x : x+6])
				if !ok {
					continue
				}
				setPx(8+x/6, 8+y, color|0xFF000000)
			}
		}
	} else {
		drawGeneratedHead(setPx, skinPalette(choice.Id))
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// skinPalette 占位头像的四个颜色：皮肤底色 / 头发 / 虹膜 / 嘴部阴影。
type skinPalette4 struct{ skin, hair, eye, shadow uint32 }

func skinPalette(id string) skinPalette4 {
	switch strings.ToLower(id) {
	case "alex":
		return skinPalette4{0xD89B74, 0xB45B28, 0x4B792D, 0x9B4B2A}
	case "noor":
		return skinPalette4{0x9A6547, 0x241B1A, 0x4E3828, 0x6F4034}
	case "sunny":
		return skinPalette4{0xBD805B, 0x5A3425, 0x426D91, 0x8A4D3D}
	case "ari":
		return skinPalette4{0xD2A078, 0x32241F, 0x75543A, 0x965F4A}
	case "zuri":
		return skinPalette4{0x82513D, 0x201817, 0x6E4F31, 0x5C332D}
	case "makena":
		return skinPalette4{0x74432F, 0x181414, 0x47321F, 0x4C2927}
	case "kai":
		return skinPalette4{0xC18B64, 0x2A211E, 0x3F6E61, 0x814B3D}
	case "efe":
		return skinPalette4{0x69402F, 0x171313, 0x684D2E, 0x482725}
	default:
		return skinPalette4{0xB98463, 0x39251B, 0x4656A6, 0x875044}
	}
}

// drawGeneratedHead 按调色板绘制简化头部（脸 / 头发 / 眼睛 / 阴影）。
func drawGeneratedHead(setPx func(x, y int, color uint32), p skinPalette4) {
	// 脸部 8×8
	for y := 8; y < 16; y++ {
		for x := 8; x < 16; x++ {
			setPx(x, y, p.skin|0xFF000000)
		}
	}
	// 刘海与两侧鬓角
	for x := 8; x < 16; x++ {
		setPx(x, 8, p.hair|0xFF000000)
		setPx(x, 9, p.hair|0xFF000000)
	}
	for y := 10; y < 13; y++ {
		setPx(8, y, p.hair|0xFF000000)
		setPx(15, y, p.hair|0xFF000000)
	}
	// 眼睛：白底 + 虹膜
	setPx(10, 11, 0xFFF4F5F8)
	setPx(11, 11, p.eye|0xFF000000)
	setPx(12, 11, p.eye|0xFF000000)
	setPx(13, 11, 0xFFF4F5F8)
	// 嘴部阴影
	setPx(11, 13, p.shadow|0xFF000000)
	setPx(12, 13, p.shadow|0xFF000000)
	setPx(11, 14, p.shadow|0xFF000000)
	setPx(12, 14, p.shadow|0xFF000000)
	// 头部背面（与原实现保持一致）
	setPx(40, 8, p.hair|0xFF000000)
	setPx(47, 8, p.hair|0xFF000000)
	setPx(40, 9, p.hair|0xFF000000)
	setPx(47, 9, p.hair|0xFF000000)
}

// steveHeadPixelRows 经典 Steve 头部（脸层 8×8）像素，每 6 个字符一个 RGB 颜色，
// 逐格取自客户端贴图 wide/steve.png（与 C# OfflineSkinCatalog 一致）。
var steveHeadPixelRows = []string{
	"3324113324113F2A153F2A153F2A153F2A153324112B1E0D",
	"2418083324113324113F2A153F2A153324113F2A15332411",
	"2B1E0D9B6349B3795EB7836BB3795EAA72599B6349342512",
	"9B6349AA7259B3795EB3795EAA7259AA7259AA72599B6349",
	"AA7259FFFFFF523D89AA72599B6349523D89FFFFFFAA7259",
	"9B6349AA7259AA72596A40306A4030AA7259AA72599B6349",
	"90593F8F5E3E492510774235774235421D0A8F5E3E815339",
	"94603E815339421D0A492510421D0A4925108153398F5E3E",
}

func parseHexColor(value string) (uint32, bool) {
	var color uint32
	for _, c := range value {
		color <<= 4
		switch {
		case c >= '0' && c <= '9':
			color |= uint32(c - '0')
		case c >= 'A' && c <= 'F':
			color |= uint32(c-'A') + 10
		case c >= 'a' && c <= 'f':
			color |= uint32(c-'a') + 10
		default:
			return 0, false
		}
	}
	return color, true
}
