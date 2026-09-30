package curseforge

// 指纹反查与项目详情：内容版本管理（content_version_options）用 murmur2 指纹
// 把"Modrinth 未收录"的本地文件识别为 CurseForge 项目。官方 API 没有 SHA-1
// 反查端点，指纹（murmur2 x86_32，种子 1）是唯一精确匹配手段——与导出整合包
// 时反查 projectID/fileID 的算法完全一致（internal/modpack/curseforge_export.go
// 内嵌了一份同样的实现，用于对清单条目做匹配）。

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"nekolauncher/internal/models"
	"nekolauncher/internal/tools"
)

// fingerprintsPath 官方批量指纹反查端点（gameId 固定为 Minecraft）。
func fingerprintsPath(gameID int) string {
	return fmt.Sprintf("/fingerprints/%d", gameID)
}

// fingerprintChunkSize 单次指纹批量上限（官方文档值）。
const fingerprintChunkSize = 1024

// FingerprintMatch 指纹精确命中的单个文件。
type FingerprintMatch struct {
	ProjectID   int64
	FileID      int64
	FileName    string
	DisplayName string
}

// FingerprintMurmur2 CurseForge 指纹算法：murmur2 x86_32，种子固定为 1，
// 输入为文件原始字节（与官方启动器 / modpack 导出的指纹实现一致）。
func FingerprintMurmur2(data []byte) uint32 {
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
		k = (k << 15) | (k >> 17) // bits.RotateLeft32(k, 15)
		k *= constant2

		hash ^= k
		hash = (hash << 13) | (hash >> 19) // bits.RotateLeft32(hash, 13)
		hash = hash*5 + 0xe6546b64
	}

	tail := data[blocks*4:]
	var k uint32
	switch len(tail) & 3 {
	case 3:
		k ^= uint32(tail[2]) << 16
		fallthrough
	case 2:
		k ^= uint32(tail[1]) << 8
		fallthrough
	case 1:
		k ^= uint32(tail[0])
		k *= constant1
		k = (k << 15) | (k >> 17)
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

// FingerprintFile 读取本地文件并计算 CurseForge 指纹。
func FingerprintFile(path string) (uint32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return FingerprintMurmur2(data), nil
}

// Fingerprints 批量精确反查指纹（自动分块，单块上限见 fingerprintChunkSize）。
// 未命中的指纹不会出现在返回的 map 里。
func Fingerprints(ctx context.Context, apiKey string, fingerprints []uint32) (map[uint32]FingerprintMatch, error) {
	result := map[uint32]FingerprintMatch{}
	for start := 0; start < len(fingerprints); start += fingerprintChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + fingerprintChunkSize
		if end > len(fingerprints) {
			end = len(fingerprints)
		}
		payload, err := json.Marshal(map[string]any{"fingerprints": fingerprints[start:end]})
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost,
			apiRoot+fingerprintsPath(models.CurseForgeMinecraftGameID), strings.NewReader(string(payload)))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		if key := strings.TrimSpace(apiKey); key != "" {
			request.Header.Set("x-api-key", key)
		}
		response, err := tools.SharedHTTPClient.Do(request)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
		_ = response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, &statusError{StatusCode: response.StatusCode, APIKeySent: strings.TrimSpace(apiKey) != ""}
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
			return nil, &decodeError{err: err}
		}
		for _, file := range parsed.Data.ExactFingerprints {
			result[uint32(file.FileFingerprint)] = FingerprintMatch{
				ProjectID:   file.ModID,
				FileID:      file.ID,
				FileName:    file.FileName,
				DisplayName: file.DisplayName,
			}
		}
	}
	return result, nil
}

// GetMod 查询项目详情（取名称与主页链接用）。
func GetMod(ctx context.Context, apiKey, modID string) (*models.CurseForgeProject, error) {
	id, err := parsePositiveID("项目 ID", modID)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data models.CurseForgeProject `json:"data"`
	}
	if err := getJSON(ctx, "/mods/"+strconv.Itoa(id), apiKey, &result); err != nil {
		return nil, err
	}
	return &result.Data, nil
}
