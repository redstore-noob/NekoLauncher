// Package modname 模组中文名查询服务。
//
// 实现思路与 PCL2 的"模组中文名"一致：把模组文件名规范化成关键词，
// 用它在 MC百科（mcmod.cn）搜索，从结果标题里提取"中文名 (英文名)"，
// 命中后持久缓存到本地，避免重复请求。数据与译名均来自 MC百科，
// 关于页的声明条目不得移除。
//
// 请求策略刻意保守：单请求 5s 超时、并发 1、两次请求间隔 400ms、
// 每次启动最多发起 60 次搜索——MC百科对高频访问有防火墙封禁。
package modname

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/config"
)

// Translation 一条译名缓存记录。Chinese 为空表示查过但没有命中（负缓存）。
type Translation struct {
	Chinese   string `json:"chinese"`
	English   string `json:"english,omitempty"`
	McmodID   string `json:"mcmodId,omitempty"`
	FetchedAt int64  `json:"fetchedAt"`
	// Version 记录写入时的匹配规则版本：规则修正后旧结果（尤其是负缓存）
	// 不可信，读到的旧版本条目一律当作不存在，触发重新搜索。
	Version int `json:"version,omitempty"`
}

// cacheVersion 当前匹配规则版本。修正 pickBest 的分隔符匹配逻辑后 +1，
// 让旧规则下写入的负缓存（如 "fabric-api" 没匹配上 "Fabric API"）自动失效。
const cacheVersion = 2

const (
	// 正缓存 30 天过期（百科译名偶尔修订），负缓存 7 天（新模组可能刚建词条）。
	positiveTTLDays = 30
	negativeTTLDays = 7
	// 每次启动的搜索配额，超出后只走缓存。
	maxSearchesPerRun = 60
	// 磁盘缓存最大条数，超限后整体清空（与内容图标缓存同一轻量策略）。
	maxCacheEntries = 5000
	searchTimeout   = 5 * time.Second
	searchInterval  = 400 * time.Millisecond
)

var (
	mu         sync.Mutex
	cache      map[string]Translation
	dirty      bool
	searches   int
	notifier   func()
	cachePath  string
	httpClient = &http.Client{Timeout: searchTimeout}
)

// SetNotifier 注册缓存更新回调（绑定层用它发 Wails 事件刷新 UI）。
func SetNotifier(fn func()) {
	mu.Lock()
	defer mu.Unlock()
	notifier = fn
}

// SetCachePath 覆盖缓存文件位置（测试注入用）。
func SetCachePath(path string) {
	mu.Lock()
	defer mu.Unlock()
	cachePath = path
	cache = nil
}

func defaultCachePath() string {
	return filepath.Join(config.StorageDirectory(), "modname-cache.json")
}

// Lookup 立即返回已知译名（内存/磁盘缓存），不会发起网络请求。
// 返回 map 以传入的原始文件名（小写化）为 key，仅包含有命中的条目。
func Lookup(fileNames []string) map[string]string {
	loadOnce()
	result := make(map[string]string, len(fileNames))
	mu.Lock()
	defer mu.Unlock()
	now := time.Now().Unix()
	for _, name := range fileNames {
		key := NormalizeModKey(name)
		if key == "" {
			continue
		}
		t, ok := cache[key]
		if !ok || t.Chinese == "" {
			continue
		}
		ttl := int64(positiveTTLDays) * 86400
		if now-t.FetchedAt > ttl {
			continue
		}
		result[name] = t.Chinese
	}
	return result
}

// RefreshAsync 为未命中的文件名异步补查（限流 + 配额），每命中/落盘一批通知一次。
func RefreshAsync(fileNames []string) {
	loadOnce()

	var pending []string
	seen := map[string]bool{}
	mu.Lock()
	now := time.Now().Unix()
	for _, name := range fileNames {
		key := NormalizeModKey(name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		t, ok := cache[key]
		if ok {
			ttl := int64(negativeTTLDays) * 86400
			if t.Chinese != "" {
				ttl = int64(positiveTTLDays) * 86400
			}
			if now-t.FetchedAt < ttl {
				continue
			}
		}
		if searches >= maxSearchesPerRun {
			break
		}
		pending = append(pending, key)
	}
	mu.Unlock()
	if len(pending) == 0 {
		return
	}

	go func() {
		for _, key := range pending {
			mu.Lock()
			if searches >= maxSearchesPerRun {
				mu.Unlock()
				break
			}
			searches++
			mu.Unlock()

			t := searchMcmod(key)
			store(key, t)
			notify()
			time.Sleep(searchInterval)
		}
	}()
}

func store(key string, t Translation) {
	mu.Lock()
	defer mu.Unlock()
	t.FetchedAt = time.Now().Unix()
	t.Version = cacheVersion
	if cache == nil {
		cache = map[string]Translation{}
	}
	cache[key] = t
	dirty = true
	persistLocked()
}

func notify() {
	mu.Lock()
	fn := notifier
	mu.Unlock()
	if fn != nil {
		fn()
	}
}

func loadOnce() {
	mu.Lock()
	defer mu.Unlock()
	if cache != nil {
		return
	}
	path := cachePath
	if path == "" {
		path = defaultCachePath()
	}
	raw, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(raw, &cache)
	}
	// 旧规则版本写入的条目（尤其负缓存）一律丢弃，让它们走重新搜索
	for k, v := range cache {
		if v.Version != cacheVersion {
			delete(cache, k)
		}
	}
	if cache == nil {
		cache = map[string]Translation{}
	}
	if len(cache) > maxCacheEntries {
		cache = map[string]Translation{}
	}
}

func persistLocked() {
	if !dirty {
		return
	}
	path := cachePath
	if path == "" {
		path = defaultCachePath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		log.Printf("modname: 缓存写入失败: %v", err)
		return
	}
	dirty = false
}

// ---- 文件名规范化 ----

var (
	bracketRe    = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`)
	separatorRe  = regexp.MustCompile(`[-_+. ]+`)
	digitRe      = regexp.MustCompile(`[0-9]`)
	loaderTokens = map[string]bool{
		"fabric": true, "forge": true, "neoforge": true, "quilt": true,
		"fb": true, "client": true, "server": true, "all": true,
	}
)

// NormalizeModKey 从模组文件名提取匹配关键词：
// "Sodium-Fabric-0.5.8+mc1.20.1.jar" → "sodium"。
// 规则：剥掉扩展名、括号段与版本号 token；loader 名跳过但不截断
// （fabric-api 仍是有效关键词）；版本号从第一个含数字的 token 开始丢弃。
func NormalizeModKey(fileName string) string {
	s := strings.ToLower(strings.TrimSpace(fileName))
	s = strings.TrimSuffix(s, ".jar")
	s = bracketRe.ReplaceAllString(s, " ")
	tokens := separatorRe.Split(s, -1)
	var keep []string
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if loaderTokens[token] {
			// 首位 loader 名保留（fabric-api），其余位置跳过（sodium-fabric → sodium）。
			if len(keep) == 0 {
				keep = append(keep, token)
			}
			continue
		}
		if digitRe.MatchString(token) {
			break
		}
		keep = append(keep, token)
	}
	if len(keep) == 0 {
		// 兜底：极端命名（如纯版本号）时退回原始词干。
		fallback := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(fileName), ".jar"))
		return fallback
	}
	return strings.Join(keep, "-")
}

// ---- MC百科搜索 ----

var (
	resultRe  = regexp.MustCompile(`href="https?://www\.mcmod\.cn/class/(\d+)\.html"[^>]*>(.*?)</a>`)
	tagRe     = regexp.MustCompile(`<[^>]+>`)
	parenPair = regexp.MustCompile(`^(.*?)\s*[（(](.+)[)）]\s*$`)
	cjkRe     = regexp.MustCompile(`[\p{Han}]`)
)

// searchMcmod 在 MC百科 搜索关键词并挑选最贴合的译名。
// 命中判定：结果英文名（或全标题）按整词包含关键词；多个命中时取
// 英文名最短的一个（与关键词越接近，越不可能是"XX 的附属"类词条）。
func searchMcmod(key string) Translation {
	req, err := http.NewRequest(http.MethodGet, mcmodSearchURL(key), nil)
	if err != nil {
		return Translation{}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) NekoLauncher")
	resp, err := httpClient.Do(req)
	if err != nil {
		return Translation{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Translation{}
	}

	// 页面较大，只读前 512KB 的结果区足够。
	buf := make([]byte, 512*1024)
	n, _ := resp.Body.Read(buf)
	best := pickBest(extractEntries(string(buf[:n])), key)
	if best == nil {
		return Translation{}
	}

	if best.zh != "" {
		return Translation{Chinese: best.zh, English: best.en, McmodID: best.id}
	}
	// 标题是 "English (Subname)" 形态：词条本身没有中文译名。
	return Translation{English: best.en, McmodID: best.id}
}

// entry 一条搜索结果。zh 为空表示词条标题不含中文（无译名）。
type entry struct {
	id    string
	title string
}

// parsed 在 entry.title 基础上拆出的匹配视图。
type parsed struct {
	id string
	// zh 中文名（可能为空），en 英文名（无括号时等于全标题）。
	zh, en string
}

func extractEntries(body string) []parsed {
	var result []parsed
	seen := map[string]bool{}
	for _, m := range resultRe.FindAllStringSubmatch(body, 32) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		title := html.UnescapeString(strings.TrimSpace(tagRe.ReplaceAllString(m[2], "")))
		if title == "" {
			continue
		}
		e := parsed{id: m[1], en: title}
		if pm := parenPair.FindStringSubmatch(title); pm != nil {
			// 常见形态 "钠 (Sodium)"：括号外是中文译名；
			// 也有 "Fabric API（Fabric 支持库）"：中文在括号内，按侧含中文者为准。
			outside, inside := strings.TrimSpace(pm[1]), strings.TrimSpace(pm[2])
			switch {
			case cjkRe.MatchString(outside):
				e.zh, e.en = outside, inside
			case cjkRe.MatchString(inside):
				e.zh, e.en = inside, outside
			}
		} else if cjkRe.MatchString(title) {
			e.zh = title
		}
		result = append(result, e)
	}
	return result
}

func pickBest(entries []parsed, key string) *parsed {
	// 关键词里的分隔符（- _ . 空格）不参与字面匹配：文件名是 "fabric-api"，
	// Modrinth 标题是 "Fabric API"，两者要视为同一个词组。
	// 做法：把 key 按分隔符切段，各段整词转义后用 [^a-z0-9]+ 连接。
	segments := strings.FieldsFunc(key, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == ' ' || r == '+'
	})
	if len(segments) == 0 {
		return nil
	}
	quoted := make([]string, len(segments))
	for i, seg := range segments {
		quoted[i] = regexp.QuoteMeta(seg)
	}
	wordRe, err := regexp.Compile(
		`(?i)(^|[^a-z0-9])` + strings.Join(quoted, `[^a-z0-9]+`) + `([^a-z0-9]|$)`)
	if err != nil {
		return nil
	}
	best := -1
	bestLen := 1 << 30
	for i, e := range entries {
		if wordRe.MatchString(e.en) && len(e.en) < bestLen {
			best = i
			bestLen = len(e.en)
		}
	}
	if best < 0 {
		return nil
	}
	return &entries[best]
}

func mcmodSearchURL(key string) string {
	return fmt.Sprintf("https://search.mcmod.cn/s?key=%s&filter=1", url.QueryEscape(key))
}
