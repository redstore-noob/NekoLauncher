// Package modname 模组中文名查询服务。
//
// 两个数据源，按成本从低到高依次尝试：
//  1. SCL 社区译名数据集（gitee 静态文件，SCL 启动器同款）：key 经
//     base64url 编码取单文件，命中即得中文名，无反爬风险；
//  2. MC百科（mcmod.cn）搜索兜底：把模组文件名规范化成关键词，
//     从结果标题里提取"中文名 (英文名)"，覆盖数据集未收录的模组。
//
// 命中后持久缓存到本地，避免重复请求。数据与译名均源自 MC百科，
// 关于页的声明条目不得移除。
//
// 请求策略刻意保守：百科搜索单请求 5s 超时、并发 1、两次请求间隔 3s
// （间隔过短会触发百科反爬的"无结果页"软封锁，检测到后冷却 15 分钟）、
// 每次启动最多发起 60 次搜索。
package modname

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"math/rand"
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
	// Source 译名来源（scl = 社区数据集；空串 = MC百科 搜索）。
	Source string `json:"source,omitempty"`
	// Version 记录写入时的匹配规则版本：规则修正后旧结果（尤其是负缓存）
	// 不可信，读到的旧版本条目一律当作不存在，触发重新搜索。
	Version int `json:"version,omitempty"`
}

// 译名来源标识。
const TranslationSourceSCL = "scl"

// cacheVersion 当前匹配规则版本。本轮修复（结果条目上限、优先带中文名的
// 命中、封锁页不再写入负缓存）后 +1，让历史负缓存全部失效重查。
// 4：版本号判定改为"以数字开头"（modid 带数字的不再被截断），key 变了。
const cacheVersion = 4

const (
	// 正缓存 30 天过期（百科译名偶尔修订），负缓存 7 天（新模组可能刚建词条）。
	positiveTTLDays = 30
	negativeTTLDays = 7
	// 每次启动的搜索配额，超出后只走缓存。
	maxSearchesPerRun = 60
	// 磁盘缓存最大条数，超限后整体清空（与内容图标缓存同一轻量策略）。
	maxCacheEntries = 5000
	searchTimeout   = 5 * time.Second
	// 实测：固定 400ms 连发两三次就被百科反爬软封锁（返回无结果页）。
	// 用完整浏览器请求头（Chrome UA + Referer + Accept-Language）加
	// 4~7s 随机间隔后连续 8 次零封锁；固定 3s 间隔在 IP 状态差时仍会被封。
	searchIntervalMin = 4 * time.Second
	searchIntervalMax = 7 * time.Second
	// SCL 数据集是静态文件，礼貌性小间隔即可。
	sclInterval     = 300 * time.Millisecond
	// MC百科的反爬在连续请求后返回"无结果页"（软封锁）。检测到后暂停搜索
	// cooldown 时间再试；连续 3 次软封锁则本轮启动放弃补查。
	blockedCooldown = 15 * time.Minute
	maxBlockedStreak = 3
)

var (
	mu         sync.Mutex
	cache      map[string]Translation
	dirty      bool
	searches   int
	blockedUntil time.Time
	blockedStreak int
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
		for i := 0; i < len(pending); i++ {
			key := pending[i]
			mu.Lock()
			if searches >= maxSearchesPerRun || blockedStreak >= maxBlockedStreak {
				mu.Unlock()
				return
			}
			wait := time.Until(blockedUntil)
			mu.Unlock()
			if wait > 0 {
				// 冷却中：睡满再继续（软封锁不是错误答案，等得起）
				time.Sleep(wait + time.Second)
			}
			mu.Lock()
			searches++
			mu.Unlock()

			// 先查 SCL 社区维护的译名数据集（静态文件，无反爬）：key 用
			// base64url 编码后取单文件。命中就直接用（SCL 启动器同款方案），
			// 未收录（404）再走 MC百科 搜索兜底。
			if t, found := searchSCLDataset(key); found {
				store(key, t)
				notify()
				time.Sleep(sclInterval)
				continue
			}

			t, blocked := searchMcmod(key)
			if blocked {
				// 软封锁：不写缓存（空页不是百科的答案），冷却后原地重试
				// 同一个 key；连续 maxBlockedStreak 次才放弃本轮。
				mu.Lock()
				blockedStreak++
				blockedUntil = time.Now().Add(blockedCooldown)
				giveUp := blockedStreak >= maxBlockedStreak
				mu.Unlock()
				log.Printf("modname: MC百科疑似限流，冷却 %s 后重试（第 %d 次）",
					blockedCooldown, blockedStreak)
				if giveUp {
					return
				}
				i-- // 重试当前 key
				continue
			}
			// 只有拿到真实答案（命中或确认未命中）才落缓存。
			store(key, t)
			mu.Lock()
			blockedStreak = 0
			mu.Unlock()
			notify()
			time.Sleep(jitterInterval())
		}
	}()
}

// jitterInterval 两次百科搜索之间的随机间隔（4~7s）：
// 固定节奏更容易被反爬识别。
func jitterInterval() time.Duration {
	return searchIntervalMin + time.Duration(rand.Int63n(int64(searchIntervalMax-searchIntervalMin)))
}

// ---- SCL 社区译名数据集（gitee 静态文件，SCL 启动器同款数据源） ----

const sclDatasetBase = "https://gitee.com/SteveXMH/scl-data/raw/master/mcmod/cname/"

// searchSCLDataset 按规范化 key 查询社区译名数据集。
// 数据集的文件名是 base64url(modid)，modid 是粘连词（"refinedstorage"）；
// 我们的 key 可能带分隔符（来自 jar 显示名 "Refined Storage" →
// "refined-storage"），所以两个变体都试。
// 第二个返回值：true = 数据集收录。
func searchSCLDataset(key string) (Translation, bool) {
	for _, variant := range sclKeyVariants(key) {
		if t, found := fetchSCLFile(variant); found {
			return t, true
		}
	}
	return Translation{}, false
}

// sclKeyVariants 生成数据集查询用的 key 变体：原样 + 去掉分隔符的粘连形态。
func sclKeyVariants(key string) []string {
	variants := []string{key}
	glued := strings.Map(func(r rune) rune {
		switch r {
		case '-', '_', '.', ' ', '+':
			return -1
		}
		return r
	}, key)
	if glued != key && glued != "" {
		variants = append(variants, glued)
	}
	return variants
}

func fetchSCLFile(key string) (Translation, bool) {
	req, err := http.NewRequest(http.MethodGet,
		sclDatasetBase+base64.URLEncoding.EncodeToString([]byte(key)), nil)
	if err != nil {
		return Translation{}, false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 NekoLauncher")
	resp, err := httpClient.Do(req)
	if err != nil {
		// 网络问题不算"未收录"，交由百科兜底
		return Translation{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Translation{}, false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return Translation{}, false
	}
	chinese := strings.TrimSpace(string(raw))
	if chinese == "" {
		return Translation{}, false
	}
	return Translation{Chinese: chinese, Source: TranslationSourceSCL}, true
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
	// 版本号 token：以数字开头（"1.20.1"、"0.5.8"），或 mc/v 前缀接数字
	// （"mc1.20.1"、"v2"）。只是"包含"数字的不算（"appliedenergistics2"、
	// "ae2thing"——这类 modid 本身带数字，截掉就查不到了）。
	versionTokenRe = regexp.MustCompile(`^(mc)?v?[0-9]`)
	loaderTokens = map[string]bool{
		"fabric": true, "forge": true, "neoforge": true, "quilt": true,
		"fb": true, "client": true, "server": true, "all": true,
	}
)

// NormalizeModKey 从模组文件名提取匹配关键词：
// "Sodium-Fabric-0.5.8+mc1.20.1.jar" → "sodium"。
// 规则：剥掉扩展名、括号段与版本号 token；loader 名跳过但不截断
// （fabric-api 仍是有效关键词）；版本号从第一个以数字开头的 token 起丢弃。
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
		if versionTokenRe.MatchString(token) {
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
	// 尾缀复数/数字："ironchests"→"ironchest"，"appliedenergistics1"→"appliedenergistics"
	trailingSuffixRe = regexp.MustCompile(`s?[0-9]+$`)
)

// searchMcmod 在 MC百科 搜索关键词并挑选最贴合的译名。
// 第二个返回值表示"页面被反爬软封锁"（无结果区）：调用方不应把这种
// 响应当成"未命中"写入负缓存。
// 关键词是粘连词且带数字（"appliedenergistics2"，来自文件名兜底）时，
// 主查询没命中会再试一次"数字前插空格"的变体（百科按词匹配，
// "Applied Energistics 2" 搜不出来 glued 形态的主词条）。
func searchMcmod(key string) (Translation, bool) {
	t, blocked := fetchMcmodQuery(key)
	if blocked || t.Chinese != "" {
		return t, blocked
	}
	if variant := digitSplitVariant(key); variant != "" && variant != key {
		return fetchMcmodQuery(variant)
	}
	return t, false
}

// digitSplitVariant "appliedenergistics2" → "appliedenergistics 2"；
// 无粘连数字时返回空串。
func digitSplitVariant(key string) string {
	var b strings.Builder
	prevDigit := false
	wroteSpace := false
	for _, r := range key {
		isDigit := r >= '0' && r <= '9'
		if isDigit && !prevDigit && b.Len() > 0 {
			b.WriteByte(' ')
			wroteSpace = true
		}
		b.WriteRune(r)
		prevDigit = isDigit
	}
	if !wroteSpace {
		return ""
	}
	return b.String()
}

// fetchMcmodQuery 发起一次百科搜索请求并解析结果。
func fetchMcmodQuery(key string) (Translation, bool) {
	req, err := http.NewRequest(http.MethodGet, mcmodSearchURL(key), nil)
	if err != nil {
		return Translation{}, true // 无法发请求时按软封锁处理，不写负缓存
	}
	// 完整浏览器请求头（UA/Referer/Accept-Language）：百科反爬按
	// 请求特征软封锁，裸 UA + 短间隔几乎必封，这组头实测稳定。
	req.Header.Set("User-Agent",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Referer", "https://www.mcmod.cn/")
	resp, err := httpClient.Do(req)
	if err != nil {
		return Translation{}, true
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Translation{}, true
	}

	// 只读前 1MB 足够，但必须循环读到 EOF：单次 Read 可能只返回一个分片，
	// 曾经导致结果区被截断、后半部分词条永远不被考虑。
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil && len(raw) == 0 {
		return Translation{}, true
	}
	body := string(raw)

	// 软封锁识别：结果区标记一个都没有。百科真正"无结果"的页面
	// 仍会带 "找到约 0 条结果" 的统计行。
	if !strings.Contains(body, "找到约") && !strings.Contains(body, "search-result") {
		return Translation{}, true
	}

	best := pickBest(extractEntries(body), key)
	if best == nil {
		// 正常页面但没匹配上：可信的"未命中"，允许负缓存。
		return Translation{}, false
	}

	if best.zh != "" {
		return Translation{Chinese: best.zh, English: best.en, McmodID: best.id}, false
	}
	// 标题是 "English (Subname)" 形态：词条本身没有中文译名。
	return Translation{English: best.en, McmodID: best.id}, false
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
	// 每条结果有两个词条链接（标题 + "地址：www.mcmod.cn/class/…" 文本链接），
	// 上限取 128 个链接 ≈ 64 条结果；真实页面的主词条（如 "钠 (Sodium)"）
	// 常排在第 20 位以后，此前 32 的上限让它们永远进不了候选。
	for _, m := range resultRe.FindAllStringSubmatch(body, 128) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		title := html.UnescapeString(strings.TrimSpace(tagRe.ReplaceAllString(m[2], "")))
		if title == "" {
			continue
		}
		// "地址：www.mcmod.cn/class/xxxx.html" 文本链接不是标题，跳过；
		// 它占掉去重名额会让相邻真正的标题链接（同 id）被误判为重复。
		if strings.Contains(title, "mcmod.cn/class/") {
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
		// 词条英文名常见粘连与带尾缀数字的形态：
		// "MouseWheelie"、"ClothConfigAPI"、"AppliedEnergistics1"。
		// 因此段之间允许零个或多个非字母数字字符，最后一段末尾
		// 容忍复数 s 与数字尾缀。
		quoted[i] = regexp.QuoteMeta(seg)
	}
	joined := quoted[0]
	for _, q := range quoted[1:] {
		joined += `[^a-z0-9]*` + q
	}
	wordRe, err := regexp.Compile(
		`(?i)(^|[^a-z0-9])` + joined + `s?[0-9]*([^a-z0-9]|$)`)
	if err != nil {
		return nil
	}
	// 等价比较视图：只留字母数字、小写、去尾缀（复数 s / 数字），
	// "fabric-api" == "Fabric API"，"applied-energistics" == "AppliedEnergistics1"。
	equivalent := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToLower(s) {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			}
		}
		return trailingSuffixRe.ReplaceAllString(b.String(), "")
	}
	keyEq := equivalent(key)
	best := -1
	// 优先级：0 = 英文与关键词等价且有中文名（主词条，如 "钠 (Sodium)"）；
	// 1 = 等价但无中文名；2 = 含关键词且有中文名；3 = 含关键词。
	bestRank, bestLen := 4, 1<<30
	for i, e := range entries {
		if !wordRe.MatchString(e.en) {
			continue
		}
		rank := 3
		if equivalent(e.en) == keyEq {
			if e.zh != "" {
				rank = 0
			} else {
				rank = 1
			}
		} else if e.zh != "" {
			rank = 2
		}
		if rank < bestRank || (rank == bestRank && len(e.en) < bestLen) {
			best, bestRank, bestLen = i, rank, len(e.en)
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
