// 版本列表服务：按核心拉取支持的 MC 版本与服务端版本（Paper 构建 /
// Fabric loader / NeoForge 全版本 / Vanilla 即 MC 版本本身），带内存缓存。
package mcserver

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// cachedStrings 缓存条目。
type cachedStrings struct {
	items   []string
	expires time.Time
}

// itoa 小整数转字符串。保留这个薄包装只是为了不改调用点的可读性，
// 实现直接走 strconv（此前手写的十进制转换对负数不成立，纯属重复造轮子）。
func itoa(n int) string { return strconv.Itoa(n) }

var versionCache sync.Map // key -> cachedStrings

// cachedStringsFetch 带缓存的拉取。
func cachedStringsFetch(key string, ttl time.Duration, fetch func() ([]string, error)) ([]string, error) {
	if v, ok := versionCache.Load(key); ok {
		if entry, ok := v.(cachedStrings); ok && time.Now().Before(entry.expires) {
			return entry.items, nil
		}
	}
	items, err := fetch()
	if err != nil {
		return nil, err
	}
	versionCache.Store(key, cachedStrings{items: items, expires: time.Now().Add(ttl)})

	return items, nil
}

// mojangReleases 官方清单里的全部正式版（新→旧），作为各核心 MC 版本的统一顺序。
func mojangReleases(ctx context.Context) ([]string, error) {
	return cachedStringsFetch("mojang-releases", 6*time.Hour, func() ([]string, error) {
		var manifest struct {
			Versions []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"versions"`
		}
		if err := httpGetJSON(ctx,
			"https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &manifest); err != nil {
			return nil, err
		}
		releases := make([]string, 0, len(manifest.Versions))
		for _, v := range manifest.Versions {
			if v.Type == "release" {
				releases = append(releases, v.ID)
			}
		}

		return releases, nil
	})
}

// paperSupportedSet Paper 支持的 MC 版本集合（fill v3 projects/paper 的扁平版本）。
func paperSupportedSet(ctx context.Context) (map[string]bool, error) {
	set, err := cachedStringsFetch("paper-supported", 6*time.Hour, func() ([]string, error) {
		var project struct {
			Versions map[string][]string `json:"versions"`
		}
		if err := httpGetJSON(ctx,
			"https://fill.papermc.io/v3/projects/paper", &project); err != nil {
			return nil, err
		}
		flat := make([]string, 0, 128)
		for _, versions := range project.Versions {
			flat = append(flat, versions...)
		}

		return flat, nil
	})
	if err != nil {
		return nil, err
	}
	set2 := make(map[string]bool, len(set))
	for _, v := range set {
		set2[v] = true
	}

	return set2, nil
}

// fabricSupportedSet Fabric 支持的稳定 MC 版本集合。
func fabricSupportedSet(ctx context.Context) (map[string]bool, error) {
	set, err := cachedStringsFetch("fabric-supported", 6*time.Hour, func() ([]string, error) {
		var games []struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		}
		if err := httpGetJSON(ctx,
			"https://meta.fabricmc.net/v2/versions/game", &games); err != nil {
			return nil, err
		}
		stable := make([]string, 0, len(games))
		for _, g := range games {
			if g.Stable {
				stable = append(stable, g.Version)
			}
		}

		return stable, nil
	})
	if err != nil {
		return nil, err
	}
	set2 := make(map[string]bool, len(set))
	for _, v := range set {
		set2[v] = true
	}

	return set2, nil
}

// neoforgeVersionPrefix MC 版本 → NeoForge maven 版本前缀。
// 老版本号是 "1.<major>.<minor>"（对应 NeoForge "21.4.x"），26 起改成
// 日期式的 "<major>.<minor>"（对应 NeoForge "26.1.x"）——两者都只需去掉
// 可能存在的 "1." 前缀再拼一个点。
func neoforgeVersionPrefix(mcVersion string) (string, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(mcVersion), "1.")
	if trimmed == "" || strings.HasPrefix(trimmed, ".") {
		return "", false
	}

	return trimmed + ".", true
}

// neoforgeSupportedSet 由 NeoForge maven 版本推导其支持的 MC 版本集合
// （"21.4.57" → "1.21.4"；日期式 "26.1.2" → "26.1"）。
func neoforgeSupportedSet(ctx context.Context) (map[string]bool, error) {
	set, err := cachedStringsFetch("neoforge-supported", 6*time.Hour, func() ([]string, error) {
		var versions struct {
			Versions []string `json:"versions"`
		}
		if err := httpGetJSON(ctx,
			"https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge",
			&versions); err != nil {
			return nil, err
		}
		mcs := make([]string, 0, 32)
		seen := map[string]bool{}
		for _, v := range versions.Versions {
			// NeoForge "21.4.57" → MC "1.21.4"；26 起的日期式版本
			//（"26.1.2" → MC "26.1"）不再补 "1." 前缀
			parts := strings.Split(v, ".")
			if len(parts) < 2 {
				continue
			}
			mc := parts[0] + "." + parts[1]
			if major, convErr := strconv.Atoi(parts[0]); convErr != nil || major < 26 {
				mc = "1." + mc
			}
			if !seen[mc] {
				seen[mc] = true
				mcs = append(mcs, mc)
			}
		}

		return mcs, nil
	})
	if err != nil {
		return nil, err
	}
	set2 := make(map[string]bool, len(set))
	for _, v := range set {
		set2[v] = true
	}

	return set2, nil
}

// ListServerMCVersions 某核心支持的 MC 版本（新→旧，仅正式版）。
func ListServerMCVersions(ctx context.Context, core string) ([]string, error) {
	releases, err := mojangReleases(ctx)
	if err != nil {
		return nil, err
	}
	var supported func(ctx context.Context) (map[string]bool, error)
	switch core {
	case CoreVanilla:
		return releases, nil
	case CorePaper:
		supported = paperSupportedSet
	case CoreFabric:
		supported = fabricSupportedSet
	case CoreNeoForge:
		supported = neoforgeSupportedSet
	default:
		return nil, errUnknownCore(core)
	}
	set, err := supported(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, 64)
	for _, v := range releases {
		if set[v] {
			result = append(result, v)
		}
	}

	return result, nil
}

func errUnknownCore(core string) error {
	return &coreError{core: core}
}

type coreError struct{ core string }

func (e *coreError) Error() string { return "不支持的服务器核心：" + e.core }

// ListCoreVersions 某核心在指定 MC 版本下的全部服务端版本（新→旧）。
func ListCoreVersions(ctx context.Context, core, mcVersion string) ([]string, error) {
	key := "core-versions:" + core + ":" + mcVersion

	return cachedStringsFetch(key, 30*time.Minute, func() ([]string, error) {
		switch core {
		case CoreVanilla:
			return []string{mcVersion}, nil

		case CorePaper:
			var builds []struct {
				ID int `json:"id"`
			}
			if err := httpGetJSON(ctx,
				"https://fill.papermc.io/v3/projects/paper/versions/"+mcVersion+"/builds",
				&builds); err != nil {
				return nil, err
			}
			ids := make([]string, 0, len(builds))
			for _, build := range builds { // API 返回新→旧
				ids = append(ids, itoa(build.ID))
			}

			return ids, nil

		case CoreFabric:
			var loaders []struct {
				Loader struct {
					Version string `json:"version"`
				} `json:"loader"`
			}
			if err := httpGetJSON(ctx,
				"https://meta.fabricmc.net/v2/versions/loader/"+mcVersion, &loaders); err != nil {
				return nil, err
			}
			versions := make([]string, 0, len(loaders))
			for _, loader := range loaders {
				versions = append(versions, loader.Loader.Version)
			}

			return versions, nil

		case CoreNeoForge:
			prefix, ok := neoforgeVersionPrefix(mcVersion)
			if !ok {
				return nil, &coreError{core: mcVersion}
			}
			var versions struct {
				Versions []string `json:"versions"`
			}
			if err := httpGetJSON(ctx,
				"https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge",
				&versions); err != nil {
				return nil, err
			}
			matched := make([]string, 0, 16)
			for _, v := range versions.Versions {
				if strings.HasPrefix(v, prefix) {
					matched = append(matched, v)
				}
			}
			// maven 升序 → 反转为新→旧
			for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
				matched[i], matched[j] = matched[j], matched[i]
			}

			return matched, nil
		}

		return nil, &coreError{core: core}
	})
}
