// Package modrinth Modrinth API v2 客户端（搜索、版本查询与下载）。
// 移植自 C# NyaLauncher.Core.Download 的 ModrinthSearch / ModrinthVersionApi。
package modrinth

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"nekolauncher/internal/models"
)

// Search 通用搜索（无关键词）。
func Search(ctx context.Context, projectType string, limit int) ([]models.ModrinthProject, error) {
	return SearchFull(ctx, projectType, "", "", limit)
}

// SearchQuery 通用搜索（带关键词）。
func SearchQuery(ctx context.Context, projectType, query string, limit int) ([]models.ModrinthProject, error) {
	return SearchFull(ctx, projectType, query, "", limit)
}

// SearchFull 通用搜索（带关键词 + MC 版本过滤）。
// projectType: mod / modpack / shader / resourcepack；
// gameVersion: 按 MC 版本过滤（可选）；limit: 返回数量上限。
func SearchFull(ctx context.Context, projectType, query, gameVersion string, limit int) ([]models.ModrinthProject, error) {
	facetParts := []string{fmt.Sprintf("%q", "project_type:"+projectType)}
	if strings.TrimSpace(gameVersion) != "" {
		facetParts = append(facetParts, fmt.Sprintf("%q", "versions:"+gameVersion))
	}

	facets := url.QueryEscape(fmt.Sprintf("[[%s]]", strings.Join(facetParts, ",")))
	encodedQuery := url.QueryEscape(query)
	endpoint := fmt.Sprintf("/search?query=%s&facets=%s&limit=%d", encodedQuery, facets, limit)

	var result models.ModrinthSearchResult
	if err := getJSON(ctx, endpoint, &result); err != nil {
		return nil, err
	}
	return result.Hits, nil
}

// SearchWithLoader 带加载器过滤的搜索：Modrinth 的 facets 是"数组的数组"
// （内层 AND、外层 OR），所以 project_type / versions / categories 三项都塞进同一层。
// 服务端装 mod / 插件时靠它把不匹配加载器的项目挡掉——把 Fabric 模组装进 Paper 服务端
// 是这类功能最常见的翻车方式。
func SearchWithLoader(ctx context.Context, projectType, query, gameVersion, loader string, limit int) ([]models.ModrinthProject, error) {
	facets := []string{fmt.Sprintf("%q", "project_type:"+projectType)}
	if strings.TrimSpace(gameVersion) != "" {
		facets = append(facets, fmt.Sprintf("%q", "versions:"+gameVersion))
	}
	if strings.TrimSpace(loader) != "" {
		facets = append(facets, fmt.Sprintf("%q", "categories:"+loader))
	}

	escaped := url.QueryEscape(fmt.Sprintf("[[%s]]", strings.Join(facets, ",")))
	endpoint := fmt.Sprintf("/search?query=%s&facets=%s&limit=%d",
		url.QueryEscape(query), escaped, limit)

	var result models.ModrinthSearchResult
	if err := getJSON(ctx, endpoint, &result); err != nil {
		return nil, err
	}

	return result.Hits, nil
}

// SearchWithLoaderGroup 带"加载器任一匹配"的搜索：Modrinth 的 facets 是
// "数组的数组"（内层 AND、外层 OR）。下载页的 Mod 标签页要的是
// "Fabric 或 Forge 或 Quilt 或 NeoForge 都算 Mod"，也就是加载器之间是 OR——
// 这种情况必须把加载器放进独立的第二层，塞进同一层会变成 AND（永远搜不到结果）。
func SearchWithLoaderGroup(
	ctx context.Context,
	projectType, query, gameVersion string,
	loaders []string,
	limit int,
) ([]models.ModrinthProject, error) {
	primary := []string{fmt.Sprintf("%q", "project_type:"+projectType)}
	if strings.TrimSpace(gameVersion) != "" {
		primary = append(primary, fmt.Sprintf("%q", "versions:"+gameVersion))
	}

	groups := []string{"[" + strings.Join(primary, ",") + "]"}
	if candidates := uniqueNonEmpty(loaders); len(candidates) > 0 {
		quoted := make([]string, 0, len(candidates))
		for _, loader := range candidates {
			quoted = append(quoted, fmt.Sprintf("%q", "categories:"+loader))
		}
		groups = append(groups, "["+strings.Join(quoted, ",")+"]")
	}

	facets := url.QueryEscape("[" + strings.Join(groups, ",") + "]")
	endpoint := fmt.Sprintf("/search?query=%s&facets=%s&limit=%d",
		url.QueryEscape(query), facets, limit)

	var result models.ModrinthSearchResult
	if err := getJSON(ctx, endpoint, &result); err != nil {
		return nil, err
	}
	return result.Hits, nil
}

// uniqueNonEmpty 去重去空（保持原顺序），用于加载器 OR 组。
func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		key := strings.ToLower(trimmed)
		if trimmed == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, trimmed)
	}
	return result
}

// GetMods Mods 列表。
func GetMods(ctx context.Context, limit int) ([]models.ModrinthProject, error) {
	return Search(ctx, "mod", limit)
}

// GetModpacks 整合包列表。
func GetModpacks(ctx context.Context, limit int) ([]models.ModrinthProject, error) {
	return Search(ctx, "modpack", limit)
}

// GetShaders 光影包列表。
func GetShaders(ctx context.Context, limit int) ([]models.ModrinthProject, error) {
	return Search(ctx, "shader", limit)
}

// GetResourcePacks 材质包列表。
func GetResourcePacks(ctx context.Context, limit int) ([]models.ModrinthProject, error) {
	return Search(ctx, "resourcepack", limit)
}
