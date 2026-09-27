package modpack

// CurseForge 整合包导出（manifest.json 格式，manifestVersion 1）。
//
// 与 Modrinth 直链声明的思路一致：能确定归属的 mod 声明为 files 条目
// （projectID + fileID，由 CurseForge 客户端自行下载），其余内容进 overrides/。
// 归属判定用 CurseForge 官方指纹接口：对 jar 原始字节计算 murmur2（种子 1），
// 批量提交 /fingerprints/4409602 精确匹配——不做文件名猜测，避免张冠李戴。
// CurseForge 独占之外的情况（Modrinth 独占 mod）在指纹接口查不到，
// 自动回落进 overrides/ 并给出警告，整合包仍然可用。

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math/bits"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"nekolauncher/internal/tools"
)

// curseForgeFingerprintsURL 指纹批量查询端点（4409602 = Minecraft 游戏 ID）。
const curseForgeFingerprintsURL = "https://api.curseforge.com/v1/fingerprints/4409602"

// curseForgeFingerprintChunkSize 单次提交的指纹数上限（保守取值，官方文档未给硬限）。
const curseForgeFingerprintChunkSize = 256

// curseForgeFileRef manifest.json files 条目。
type curseForgeFileRef struct {
	ProjectID int64 `json:"projectID"`
	FileID    int64 `json:"fileID"`
	Required  bool  `json:"required"`
}

// curseForgeFingerprintMatch 指纹命中的 CurseForge 文件信息。
type curseForgeFingerprintMatch struct {
	ProjectID   int64
	FileID      int64
	FileName    string
	DisplayName string
}

// CurseForgeFingerprintResolver 指纹批量反查（测试可替换；nil 视为全部未命中）。
var CurseForgeFingerprintResolver = resolveCurseForgeFingerprints

// resolveCurseForgeModRefs 为选中的 mod 计算 CurseForge files 声明。
// 返回 包内相对路径 → files 条目、命中明细（供 modlist.html 使用）与错误；
// 可恢复的失败（无 Key / 查询失败 / 指纹缺失）不返回错误，回落 overrides 并记入警告。
func resolveCurseForgeModRefs(
	ctx context.Context,
	options ModpackExportOptions,
	contentDirectory string,
	selected []ModpackContentItem,
	warnings *[]string,
	progress func(ModpackExportProgress),
) (map[string]curseForgeFileRef, []curseForgeFingerprintMatch, error) {
	result := map[string]curseForgeFileRef{}
	var matched []curseForgeFingerprintMatch
	apiKey := strings.TrimSpace(options.CurseForgeAPIKey)
	if apiKey == "" {
		*warnings = append(*warnings,
			"未配置 CurseForge API Key（设置 → 下载），模组将直接打包进整合包而不是声明为 CurseForge 文件。")
		return result, matched, nil
	}

	// 只声明启用状态的 mod（*.jar）；禁用态（*.jar.disabled）进 overrides，
	// manifest 没有表达"已禁用"的字段，声明出去会在导入后被意外启用。
	type modCandidate struct {
		archivePath string
		fingerprint uint32
	}
	var candidates []modCandidate
	for _, item := range selected {
		if item.Category != CategoryMods || item.IsDirectory {
			continue
		}
		if strings.HasSuffix(strings.ToLower(item.RelativePath), ".disabled") {
			continue
		}
		print, err := curseForgeFingerprintFile(
			filepath.Join(contentDirectory, filepath.FromSlash(item.RelativePath)))
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("计算指纹失败（%s）：%v", item.RelativePath, err))
			continue
		}
		candidates = append(candidates, modCandidate{item.RelativePath, print})
	}
	if len(candidates) == 0 {
		return result, matched, nil
	}

	prints := make([]uint32, 0, len(candidates))
	for _, candidate := range candidates {
		prints = append(prints, candidate.fingerprint)
	}
	if progress != nil {
		progress(ModpackExportProgress{"正在解析 CurseForge 文件指纹", 0, len(prints)})
	}
	resolver := CurseForgeFingerprintResolver
	if resolver == nil {
		*warnings = append(*warnings, "本次未能向 CurseForge 查询文件指纹，模组将直接打包进整合包。")
		return result, matched, nil
	}
	matches, err := resolver(ctx, apiKey, prints)
	if err != nil {
		if ctx.Err() != nil {
			return result, matched, ctx.Err()
		}
		*warnings = append(*warnings, fmt.Sprintf("查询 CurseForge 指纹失败，模组将直接打包进整合包：%v", err))
		return result, matched, nil
	}
	for _, candidate := range candidates {
		match, ok := matches[candidate.fingerprint]
		if !ok {
			continue
		}
		result[strings.ToLower(filepath.ToSlash(candidate.archivePath))] = curseForgeFileRef{
			ProjectID: match.ProjectID,
			FileID:    match.FileID,
			Required:  true,
		}
		matched = append(matched, match)
	}
	undeclared := len(candidates) - len(result)
	if undeclared > 0 {
		*warnings = append(*warnings, fmt.Sprintf(
			"%d 个 mod 未能匹配到 CurseForge 文件（Modrinth 独占或已下架），它们将直接打包进整合包。", undeclared))
	}
	sort.Slice(matched, func(i, j int) bool {
		return strings.ToLower(matched[i].DisplayName) < strings.ToLower(matched[j].DisplayName)
	})
	return result, matched, nil
}

// resolveCurseForgeFingerprints 调用官方指纹接口批量精确匹配（默认实现）。
func resolveCurseForgeFingerprints(
	ctx context.Context,
	apiKey string,
	fingerprints []uint32,
) (map[uint32]curseForgeFingerprintMatch, error) {
	result := map[uint32]curseForgeFingerprintMatch{}
	for start := 0; start < len(fingerprints); start += curseForgeFingerprintChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + curseForgeFingerprintChunkSize
		if end > len(fingerprints) {
			end = len(fingerprints)
		}
		payload, err := json.Marshal(map[string]any{"fingerprints": fingerprints[start:end]})
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost,
			curseForgeFingerprintsURL, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("x-api-key", apiKey)
		response, err := tools.SharedHTTPClient.Do(request)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("CurseForge 指纹查询失败（HTTP %d）", response.StatusCode)
		}
		var parsed struct {
			Data struct {
				ExactFingerprints []struct {
					ID              int64  `json:"id"`
					ModID           int64  `json:"modId"`
					FileName        string `json:"fileName"`
					DisplayName     string `json:"displayName"`
					FileFingerprint int64  `json:"fileFingerprint"`
				} `json:"exactFingerprints"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, err
		}
		for _, file := range parsed.Data.ExactFingerprints {
			result[uint32(file.FileFingerprint)] = curseForgeFingerprintMatch{
				ProjectID:   file.ModID,
				FileID:      file.ID,
				FileName:    file.FileName,
				DisplayName: file.DisplayName,
			}
		}
	}
	return result, nil
}

// curseForgeFingerprintFile 读取文件并计算 CurseForge 指纹。
func curseForgeFingerprintFile(path string) (uint32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return fingerprintMurmur2(data), nil
}

// fingerprintMurmur2 CurseForge 指纹算法：murmur2 x86_32，种子固定为 1，
// 输入为文件原始字节（与官方启动器/社区实现的指纹完全一致）。
func fingerprintMurmur2(data []byte) uint32 {
	const (
		constant1 = 0xcc9e2d51
		constant2 = 0x1b873593
	)
	length := len(data)
	hash := uint32(1)
	blocks := length / 4
	for i := 0; i < blocks; i++ {
		k := binary.LittleEndian.Uint32(data[i*4:])
		k *= constant1
		k = bits.RotateLeft32(k, 15)
		k *= constant2
		hash ^= k
		hash = bits.RotateLeft32(hash, 13)
		hash = hash*5 + 0xe6546b64
	}
	var k uint32
	tail := data[blocks*4:]
	switch len(tail) {
	case 3:
		k ^= uint32(tail[2]) << 16
		fallthrough
	case 2:
		k ^= uint32(tail[1]) << 8
		fallthrough
	case 1:
		k ^= uint32(tail[0])
		k *= constant1
		k = bits.RotateLeft32(k, 15)
		k *= constant2
		hash ^= k
	}
	hash ^= uint32(length)
	hash ^= hash >> 16
	hash *= 0x85ebca6b
	hash ^= hash >> 13
	hash *= 0xc2b2ae35
	hash ^= hash >> 16
	return hash
}

// ---------------------------------------------------------------------------
// manifest.json / modlist.html 构建
// ---------------------------------------------------------------------------

// curseForgeManifest manifest.json 根对象。
type curseForgeManifest struct {
	Minecraft       curseForgeMinecraft  `json:"minecraft"`
	ManifestType    string               `json:"manifestType"`
	ManifestVersion int                  `json:"manifestVersion"`
	Name            string               `json:"name"`
	Version         string               `json:"version"`
	Author          string               `json:"author,omitempty"`
	Files           []curseForgeFileRef  `json:"files"`
	Overrides       string               `json:"overrides"`
}

type curseForgeMinecraft struct {
	Version    string                `json:"version"`
	ModLoaders []curseForgeModLoader `json:"modLoaders"`
}

type curseForgeModLoader struct {
	ID      string `json:"id"`
	Primary bool   `json:"primary"`
}

// buildCurseForgeManifest 组装 manifest.json。
func buildCurseForgeManifest(
	options ModpackExportOptions,
	packVersion string,
	fileRefs map[string]curseForgeFileRef,
) curseForgeManifest {
	refs := make([]curseForgeFileRef, 0, len(fileRefs))
	for _, ref := range fileRefs {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ProjectID != refs[j].ProjectID {
			return refs[i].ProjectID < refs[j].ProjectID
		}
		return refs[i].FileID < refs[j].FileID
	})
	manifest := curseForgeManifest{
		Minecraft: curseForgeMinecraft{
			Version:    options.MinecraftVersion,
			ModLoaders: []curseForgeModLoader{},
		},
		ManifestType:    "minecraftModpack",
		ManifestVersion: 1,
		Name:            options.PackName,
		Version:         packVersion,
		Author:          options.Author,
		Files:           refs,
		Overrides:       "overrides",
	}
	if loaderID := mapCurseForgeLoaderID(options.LoaderName, options.LoaderVersion); loaderID != "" {
		manifest.Minecraft.ModLoaders = append(manifest.Minecraft.ModLoaders, curseForgeModLoader{
			ID:      loaderID,
			Primary: true,
		})
	}
	return manifest
}

// mapCurseForgeLoaderID 加载器名 + 版本 → manifest 的 modLoader id（如 "fabric-0.16.9"）。
// 加载器版本缺失时返回空串（与其它格式一致：缺版本就不声明加载器）。
func mapCurseForgeLoaderID(loaderName, loaderVersion string) string {
	version := strings.TrimSpace(loaderVersion)
	if version == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(loaderName)) {
	case "fabric":
		return "fabric-" + version
	case "forge":
		return "forge-" + version
	case "neoforge":
		return "neoforge-" + version
	case "quilt":
		return "quilt-" + version
	default:
		return ""
	}
}

// buildCurseForgeModlist 组装 modlist.html（纯展示文件，CurseForge 客户端不依赖）。
func buildCurseForgeModlist(matched []curseForgeFingerprintMatch) string {
	var builder strings.Builder
	builder.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"UTF-8\">\n<title>Mod List</title>\n</head>\n<body>\n<ul>\n")
	for _, match := range matched {
		name := match.DisplayName
		if strings.TrimSpace(name) == "" {
			name = match.FileName
		}
		fmt.Fprintf(&builder, "<li>%s</li>\n", html.EscapeString(name))
	}
	builder.WriteString("</ul>\n</body>\n</html>\n")
	return builder.String()
}
