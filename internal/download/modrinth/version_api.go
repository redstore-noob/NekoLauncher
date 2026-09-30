package modrinth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"nekolauncher/internal/logs"
	"nekolauncher/internal/models"
)

// GetVersions 获取指定项目的版本列表，可按 MC 版本和 Loader 过滤。
func GetVersions(ctx context.Context, projectID string, gameVersions, loaders []string) ([]models.ModrinthVersion, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("projectID 不能为空")
	}

	endpoint := fmt.Sprintf("/project/%s/version", url.PathEscape(projectID))
	var params []string
	if len(gameVersions) > 0 {
		quoted := make([]string, len(gameVersions))
		for i, v := range gameVersions {
			quoted[i] = strconv.Quote(v)
		}
		params = append(params, "game_versions="+url.QueryEscape("["+strings.Join(quoted, ",")+"]"))
	}
	if len(loaders) > 0 {
		quoted := make([]string, len(loaders))
		for i, l := range loaders {
			quoted[i] = strconv.Quote(l)
		}
		params = append(params, "loaders="+url.QueryEscape("["+strings.Join(quoted, ",")+"]"))
	}
	if len(params) > 0 {
		endpoint += "?" + strings.Join(params, "&")
	}

	var versions []models.ModrinthVersion
	if err := getJSON(ctx, endpoint, &versions); err != nil {
		// 响应格式异常不应伪装成"没有版本"：解析失败留下日志并返回空列表
		//（保持对调用方的兼容行为，与 C# 一致）；其余错误原样上抛
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
			logs.Write("WARN", fmt.Sprintf("Modrinth 版本响应解析失败（%s）: %v", projectID, err))
			return []models.ModrinthVersion{}, nil
		}
		return nil, err
	}
	if versions == nil {
		versions = []models.ModrinthVersion{}
	}
	return versions, nil
}

// CompareVersionStrings 按分段数值比较 MC 版本号（如 1.10.2 > 1.9.4），
// 替代会产生错误顺序的字符串比较。
func CompareVersionStrings(a, b string) int {
	aParts := strings.FieldsFunc(a, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
	bParts := strings.FieldsFunc(b, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
	length := len(aParts)
	if len(bParts) > length {
		length = len(bParts)
	}
	for i := 0; i < length; i++ {
		aPart := "0"
		if i < len(aParts) {
			aPart = aParts[i]
		}
		bPart := "0"
		if i < len(bParts) {
			bPart = bParts[i]
		}
		aNum, aErr := strconv.Atoi(aPart)
		bNum, bErr := strconv.Atoi(bPart)
		if aErr == nil && bErr == nil {
			if aNum != bNum {
				if aNum < bNum {
					return -1
				}
				return 1
			}
		} else if aPart != bPart {
			return strings.Compare(aPart, bPart)
		}
	}
	return 0
}

// GetVersionsForCombo 获取指定项目在指定 MC 版本 + Loader 下的可用 Mod 版本列表。
func GetVersionsForCombo(ctx context.Context, projectID, gameVersion, loader string) ([]models.ModrinthVersion, error) {
	return GetVersions(ctx, projectID, []string{gameVersion}, []string{loader})
}

// GetVersion 获取单个版本详情。
//
// 为什么需要：下载只能由后端决定文件地址——前端传来的 URL 既不可信
// （WebView 里可以塞任何地址），也需要与"用户看到的那一行版本"严格对应；
// 按版本 ID 反查官方数据后取主文件，才是权威来源。
func GetVersion(ctx context.Context, versionID string) (*models.ModrinthVersion, error) {
	if strings.TrimSpace(versionID) == "" {
		return nil, fmt.Errorf("versionID 不能为空")
	}

	var version models.ModrinthVersion
	if err := getJSON(ctx, "/version/"+url.PathEscape(versionID), &version); err != nil {
		return nil, err
	}
	return &version, nil
}
