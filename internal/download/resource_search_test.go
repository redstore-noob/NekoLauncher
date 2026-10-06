package download

// 资源搜索服务（resource_search.go）的离线用例：httptest 假资源站 + 注入的
// 地址解析，一条都不打真实网络，也不碰用户真实数据（下载目标一律 t.TempDir()）。
//
// 盯住三件最容易悄悄坏掉的事：
//  1. CurseForge 未配置 Key 的降级必须是"可读引导"而不是错误；
//  2. 版本匹配判定（游戏版本/加载器）错判会让用户把不兼容的 Mod 装进实例；
//  3. 下载必须走 SourceProvider 的主源 + 回退源，主源失败还能落到回退源。

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nekolauncher/internal/download/curseforge"
	"nekolauncher/internal/download/modrinth"
	"nekolauncher/internal/models"
)

// ---- 夹具 ----

const resourceSearchPayload = `{"hits":[
 {"project_id":"AANobbMI","title":"Sodium","description":"渲染优化","icon_url":"https://cdn.modrinth.com/x.png",
  "project_type":"mod","downloads":1500,"follows":42,"slug":"sodium","date_created":"2024-06-13T08:00:00Z"}
],"total_hits":1}`

const resourceVersionsPayload = `[
 {"id":"Match001","project_id":"AANobbMI","name":"Sodium 0.6.0","version_number":"mc1.21.1-0.6.0",
  "version_type":"release","game_versions":["1.21.1"],"loaders":["fabric"],
  "date_published":"2024-06-13T08:00:00Z","changelog":"修复若干问题",
  "files":[{"url":"https://cdn.modrinth.com/data/sodium.jar","filename":"sodium.jar","size":1024,"primary":true,
            "hashes":{"sha1":"ABC"}}]},
 {"id":"Old00001","project_id":"AANobbMI","name":"Sodium 0.4.0","version_number":"mc1.19.2-0.4.0",
  "version_type":"beta","game_versions":["1.19.2"],"loaders":["forge"],
  "date_published":"2022-01-02T00:00:00Z",
  "files":[{"url":"https://cdn.modrinth.com/data/sodium-old.jar","filename":"sodium-old.jar","size":2048,"primary":true}]}
]`

const resourceVersionPayload = `{"id":"Match001","project_id":"AANobbMI","name":"Sodium 0.6.0",
 "version_number":"mc1.21.1-0.6.0","version_type":"release","game_versions":["1.21.1"],"loaders":["fabric"],
 "date_published":"2024-06-13T08:00:00Z",
 "files":[{"url":"CDN_URL","filename":"sodium.jar","size":1024,"primary":true,"hashes":{"sha1":"ABC"}}]}`

const curseForgeSearchPayload = `{"data":[{
 "id":238222,"gameId":432,"name":"Just Enough Items","slug":"jei",
 "links":{"websiteUrl":"https://www.curseforge.com/minecraft/mc-mods/jei"},
 "summary":"查看物品配方","downloadCount":1500000,"classId":6,
 "authors":[{"id":1,"name":"mezz"}],
 "logo":{"thumbnailUrl":"https://media.forgecdn.net/t.png","url":"https://media.forgecdn.net/f.png"},
 "dateModified":"2024-06-13T08:00:00.000Z","allowModDistribution":true}],
 "pagination":{"index":0,"pageSize":20,"resultCount":1,"totalCount":377}}`

const curseForgeFilesPayload = `{"data":[{
 "id":8965084,"modId":238222,"displayName":"JEI for NeoForge","fileName":"jei.jar",
 "releaseType":1,"fileDate":"2024-06-13T08:00:00.000Z","fileLength":2048,
 "downloadUrl":"CDN_URL","gameVersions":["1.21.1","NeoForge"],
 "sortableGameVersions":[{"gameVersionName":"NeoForge","gameVersionTypeId":68441},
                         {"gameVersionName":"1.21.1","gameVersionTypeId":77784}],
 "dependencies":[],"hashes":[{"value":"abc","algo":1}],"isAvailable":true}],
 "pagination":{"index":0,"pageSize":50,"resultCount":1,"totalCount":1}}`

// curseForgeSingleFileNoURLPayload 单个文件响应，且作者关闭了 API 分发
// （downloadUrl 为 null，必须再去问 download-url 端点）。
const curseForgeSingleFileNoURLPayload = `{"data":{
 "id":8965084,"modId":238222,"displayName":"JEI for NeoForge","fileName":"jei.jar",
 "releaseType":1,"fileDate":"2024-06-13T08:00:00.000Z","fileLength":2048,
 "downloadUrl":null,"gameVersions":["1.21.1","NeoForge"],
 "sortableGameVersions":[{"gameVersionName":"NeoForge","gameVersionTypeId":68441}],
 "dependencies":[],"hashes":[{"value":"abc","algo":1}],"isAvailable":true}}`

// ---- 假服务器 ----

// resourceServer 假资源站服务器：记录请求路径与查询参数，按 responder 返回内容。
// 同一个实例既能当 API 端点也能当文件 CDN（路径不同即可）。
type resourceServer struct {
	server *httptest.Server

	mu       sync.Mutex
	requests []string
	queries  []url.Values
}

func newResourceServer(t *testing.T, respond func(path string) (int, string)) *resourceServer {
	t.Helper()
	stub := &resourceServer{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.requests = append(stub.requests, r.URL.Path)
		stub.queries = append(stub.queries, r.URL.Query())
		stub.mu.Unlock()

		status, body := respond(r.URL.Path)
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

// lastQuery 最近一次请求的查询参数。
func (s *resourceServer) lastQuery(t *testing.T) url.Values {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queries) == 0 {
		t.Fatal("假服务器没有收到任何请求")
	}
	return s.queries[len(s.queries)-1]
}

func (s *resourceServer) baseURL() string { return s.server.URL }

func (s *resourceServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *resourceServer) countPath(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, request := range s.requests {
		if request == path {
			total++
		}
	}
	return total
}

// useModrinthStub 把 Modrinth 客户端指向假服务器（不配置镜像）。
func useModrinthStub(t *testing.T, stub *resourceServer) {
	t.Helper()
	modrinth.SetEndpoints(stub.baseURL(), "")
	modrinth.SetHTTPClient(stub.server.Client())
	t.Cleanup(modrinth.ResetEndpoints)
}

// useCurseForgeStub 把 CurseForge 客户端指向假服务器（不配置镜像）。
func useCurseForgeStub(t *testing.T, stub *resourceServer) {
	t.Helper()
	curseforge.SetEndpoints(stub.baseURL(), "")
	curseforge.SetHTTPClient(stub.server.Client())
	t.Cleanup(curseforge.ResetEndpoints)
}

// swapResourceResolver 注入假的地址解析。
// 真实 CDN（cdn.modrinth.com / edge.forgecdn.net）在单测里不可达，而
// "主源失败后确实尝试了回退源"这件事必须有测试盯着。
func swapResourceResolver(t *testing.T, resolve func(string) (string, string)) {
	t.Helper()
	original := resolveResourceURL
	resolveResourceURL = resolve
	t.Cleanup(func() { resolveResourceURL = original })
}

// ---- 匹配判定 ----

// TestMatchResourceVersionsMarksAndSorts 防的回归：
//   - 匹配判定漏判游戏版本或加载器（把 1.19.2/Forge 的版本标成"匹配当前实例"）；
//   - 匹配项没有排到前面（用户一眼看到的是不兼容的旧版本）；
//   - 不匹配时不给原因（界面只能显示一个干巴巴的"不匹配"）。
func TestMatchResourceVersionsMarksAndSorts(t *testing.T) {
	versions := []models.ResourceVersion{
		{VersionID: "old", GameVersions: []string{"1.19.2"}, Loaders: []string{"forge"}},
		{VersionID: "match", GameVersions: []string{"1.21.1"}, Loaders: []string{"fabric"}},
		{VersionID: "other", GameVersions: []string{"1.20.1"}, Loaders: []string{"fabric"}},
	}

	matched := MatchResourceVersions(versions, "1.21.1", "fabric")
	if matched[0].VersionID != "match" {
		t.Fatalf("匹配项应排在最前，实际顺序：%s/%s/%s",
			matched[0].VersionID, matched[1].VersionID, matched[2].VersionID)
	}
	if !matched[0].MatchesInstance || matched[0].MatchNote != "" {
		t.Fatalf("匹配项不该带不匹配原因：%+v", matched[0])
	}
	if matched[1].VersionID != "old" || matched[2].VersionID != "other" {
		// 同组内保持原有顺序（接口已是新→旧）
		t.Fatalf("不匹配项之间应保持原顺序，实际：%s/%s", matched[1].VersionID, matched[2].VersionID)
	}
	if matched[1].MatchesInstance {
		t.Fatal("1.19.2/forge 的版本不该被标成匹配")
	}
	if !strings.Contains(matched[1].MatchNote, "1.19.2") || !strings.Contains(matched[1].MatchNote, "Forge") {
		t.Fatalf("不匹配原因应写清版本与加载器：%q", matched[1].MatchNote)
	}
}

// TestMatchResourceVersionsWithoutInstanceFilters 防的回归：
// 没传游戏版本/加载器时把所有版本都标成不匹配（等于"什么都装不了"）。
func TestMatchResourceVersionsWithoutInstanceFilters(t *testing.T) {
	versions := []models.ResourceVersion{
		{VersionID: "a", GameVersions: []string{"1.19.2"}, Loaders: []string{"forge"}},
		{VersionID: "b", GameVersions: nil, Loaders: nil},
	}
	for _, version := range MatchResourceVersions(versions, "", "") {
		if !version.MatchesInstance {
			t.Fatalf("没有实例过滤条件时不该判为不匹配：%+v", version)
		}
	}
}

// TestMatchResourceVersionsTreatsMinecraftLoaderAsWildcard 防的回归：
// 材质包/光影包的 loaders 是 ["minecraft"]，被当成"不支持 Fabric"而全员不匹配
// （用户会以为这个材质包装不了）。
func TestMatchResourceVersionsTreatsMinecraftLoaderAsWildcard(t *testing.T) {
	versions := []models.ResourceVersion{
		{VersionID: "pack", GameVersions: []string{"1.21.1"}, Loaders: []string{"minecraft"}},
		{VersionID: "noLoaderField", GameVersions: []string{"1.21.1"}},
	}
	for _, version := range MatchResourceVersions(versions, "1.21.1", "fabric") {
		if !version.MatchesInstance {
			t.Fatalf("minecraft / 空加载器应当作任意加载器兼容：%+v", version)
		}
	}
}

// ---- 资源站清单与降级 ----

// TestListResourceSourcesReportsAvailability 防的回归：
// 未配置 Key 时 CurseForge 被标成"可用"（用户点了才报错），
// 或者配置后仍标"不可用"（引导永远不消失）。
func TestListResourceSourcesReportsAvailability(t *testing.T) {
	sources := ListResourceSources("")
	if len(sources) != 2 {
		t.Fatalf("资源站数量 = %d，期望 2", len(sources))
	}
	if sources[0].ID != models.ResourceSourceModrinth || !sources[0].Available {
		t.Fatalf("Modrinth 应无需 Key 且可用：%+v", sources[0])
	}
	if sources[0].APIHost == "" || sources[0].MirrorHost == "" {
		t.Fatalf("资源站信息必须带上域名（前端不再抄一份）：%+v", sources[0])
	}

	curseForge := sources[1]
	if curseForge.ID != models.ResourceSourceCurseForge {
		t.Fatalf("第二个资源站应是 CurseForge：%+v", curseForge)
	}
	if !curseForge.RequiresAPIKey || curseForge.Available || curseForge.APIKeyConfigured {
		t.Fatalf("未配置 Key 时应为「需要 Key 且不可用」：%+v", curseForge)
	}
	if !strings.Contains(curseForge.Hint, "内置 API Key") || curseForge.Hint == "" {
		t.Fatalf("引导里必须说明内置 Key 未生效：%+v", curseForge)
	}

	configured := ListResourceSources("some-key")[1]
	if !configured.Available || !configured.APIKeyConfigured {
		t.Fatalf("配置 Key 后应标记为可用：%+v", configured)
	}
}

// TestSearchResourcesCurseForgeWithoutKeyReturnsHint 防的回归：
// 没配 Key 就抛错（下载页看起来像坏了），或者悄悄回落到 Modrinth
// （用户以为在看 CurseForge 的结果）。
func TestSearchResourcesCurseForgeWithoutKeyReturnsHint(t *testing.T) {
	result, err := SearchResources(context.Background(), models.ResourceSearchRequest{
		Source:      models.ResourceSourceCurseForge,
		ProjectType: models.ProjectTypeMod,
		Query:       "jei",
	}, "")
	if err != nil {
		t.Fatalf("未配置 Key 不该返回错误（界面要能正常显示引导）：%v", err)
	}
	if !result.NeedsAPIKey {
		t.Fatal("应标记 NeedsAPIKey，界面据此显示提示")
	}
	if !strings.Contains(result.Message, "内置 API Key") {
		t.Fatalf("提示信息应说明内置 CurseForge API Key 未生效：%q", result.Message)
	}
	if result.Source != models.ResourceSourceCurseForge {
		t.Fatalf("降级后仍应报告真实数据源，实际 %q", result.Source)
	}
	if result.Hits == nil {
		t.Fatal("Hits 不能为 nil（前端会直接 .map）")
	}
}

// TestListResourceVersionsCurseForgeWithoutKeyReturnsHint 防的回归：同上，版本列表路径。
func TestListResourceVersionsCurseForgeWithoutKeyReturnsHint(t *testing.T) {
	result, err := ListResourceVersions(context.Background(), models.ResourceVersionRequest{
		Source:    models.ResourceSourceCurseForge,
		ProjectID: "238222",
	}, "")
	if err != nil {
		t.Fatalf("未配置 Key 不该返回错误：%v", err)
	}
	if !result.NeedsAPIKey || result.Message == "" {
		t.Fatalf("应返回可读引导：%+v", result)
	}
	if result.Versions == nil {
		t.Fatal("Versions 不能为 nil（前端会直接 .map）")
	}
}

// TestDownloadResourceVersionCurseForgeWithoutKeyReturnsHint 防的回归：
// 下载路径的降级提示与搜索路径不一致（一个说"没配 Key"，一个说"下载失败"）。
func TestDownloadResourceVersionCurseForgeWithoutKeyReturnsHint(t *testing.T) {
	_, err := DownloadResourceVersion(context.Background(), models.ResourceDownloadRequest{
		Source:           models.ResourceSourceCurseForge,
		ProjectID:        "238222",
		VersionID:        "8965084",
		ContentDirectory: t.TempDir(),
	}, "", nil)
	if err == nil {
		t.Fatal("未配置 Key 时下载必须报错（不能静默什么都不做）")
	}
	if !strings.Contains(err.Error(), "内置 API Key") {
		t.Fatalf("错误信息应指向内置 API Key 未生效：%v", err)
	}
}

// ---- 转换（展示串必须来自 Go 侧的展示语义） ----

// TestSearchResourcesModrinthConvertsDisplayFields 防的回归：
// 统一模型里的展示串没接上 models 里已被单测锁定的方法（前端只好自己再格式化一遍），
// 以及 PageURL 拼错、类型没归一化。
func TestSearchResourcesModrinthConvertsDisplayFields(t *testing.T) {
	stub := newResourceServer(t, func(path string) (int, string) {
		if path != "/search" {
			return http.StatusNotFound, ""
		}
		return http.StatusOK, resourceSearchPayload
	})
	useModrinthStub(t, stub)

	result, err := SearchResources(context.Background(), models.ResourceSearchRequest{
		Source:      models.ResourceSourceModrinth,
		ProjectType: models.ProjectTypeMod,
		Query:       "sodium",
		Limit:       10,
	}, "")
	if err != nil {
		t.Fatalf("搜索不应失败：%v", err)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("结果条数 = %d", len(result.Hits))
	}
	hit := result.Hits[0]
	if hit.DownloadsDisplay != "1.5K 下载" {
		t.Fatalf("DownloadsDisplay = %q（应复用 models 的格式化语义）", hit.DownloadsDisplay)
	}
	if hit.FollowsDisplay != "42 ⭐" {
		t.Fatalf("FollowsDisplay = %q", hit.FollowsDisplay)
	}
	if hit.TypeDisplay != "Mod" || hit.TypeIcon != "⬜" {
		t.Fatalf("类型展示 = %q / %q", hit.TypeDisplay, hit.TypeIcon)
	}
	if hit.PageURL != "https://modrinth.com/project/sodium" {
		t.Fatalf("PageURL = %q", hit.PageURL)
	}
	if hit.DateDisplay != "2024-06-13" {
		t.Fatalf("DateDisplay = %q", hit.DateDisplay)
	}
	if result.UsedMirror {
		t.Fatal("没走镜像却标了 UsedMirror")
	}
}

// TestSearchResourcesCurseForgeConvertsHits 防的回归：
// CurseForge 结果里的作者/图标/下载量展示与 Modrinth 不一致（同一张列表两种样式），
// 以及给 CurseForge 硬凑一个"关注数"。
func TestSearchResourcesCurseForgeConvertsHits(t *testing.T) {
	stub := newResourceServer(t, func(path string) (int, string) {
		if path != "/mods/search" {
			return http.StatusNotFound, ""
		}
		return http.StatusOK, curseForgeSearchPayload
	})
	useCurseForgeStub(t, stub)

	result, err := SearchResources(context.Background(), models.ResourceSearchRequest{
		Source:      models.ResourceSourceCurseForge,
		ProjectType: models.ProjectTypeMod,
		Query:       "jei",
	}, "key")
	if err != nil {
		t.Fatalf("搜索不应失败：%v", err)
	}
	if result.Total != 377 {
		t.Fatalf("总数 = %d，期望取接口的 totalCount", result.Total)
	}
	hit := result.Hits[0]
	if hit.ProjectID != "238222" || hit.Title != "Just Enough Items" {
		t.Fatalf("基本字段不对：%+v", hit)
	}
	if hit.Author != "mezz" {
		t.Fatalf("Author = %q", hit.Author)
	}
	if hit.DownloadsDisplay != "1.5M 下载" {
		t.Fatalf("DownloadsDisplay = %q", hit.DownloadsDisplay)
	}
	if hit.FollowsDisplay != "" {
		t.Fatalf("CurseForge 没有关注数，应为空串（不是假的 0）：%q", hit.FollowsDisplay)
	}
	if hit.TypeDisplay != "Mod" {
		t.Fatalf("TypeDisplay = %q", hit.TypeDisplay)
	}
	if hit.Source != models.ResourceSourceCurseForge {
		t.Fatalf("Source = %q", hit.Source)
	}
}

// TestListResourceVersionsModrinthMarksMatches 防的回归：
// 版本列表没有带上"匹配当前实例"的标记与展示串（界面无法提示"这个版本可以用"）。
func TestListResourceVersionsModrinthMarksMatches(t *testing.T) {
	stub := newResourceServer(t, func(path string) (int, string) {
		if path != "/project/AANobbMI/version" {
			return http.StatusNotFound, ""
		}
		return http.StatusOK, resourceVersionsPayload
	})
	useModrinthStub(t, stub)

	result, err := ListResourceVersions(context.Background(), models.ResourceVersionRequest{
		Source:      models.ResourceSourceModrinth,
		ProjectID:   "AANobbMI",
		GameVersion: "1.21.1",
		Loader:      "fabric",
	}, "")
	if err != nil {
		t.Fatalf("查询版本不应失败：%v", err)
	}
	if len(result.Versions) != 2 || result.MatchedCount != 1 {
		t.Fatalf("版本/匹配数 = %d/%d，期望 2/1", len(result.Versions), result.MatchedCount)
	}
	matched := result.Versions[0]
	if !matched.MatchesInstance || matched.VersionID != "Match001" {
		t.Fatalf("匹配版本应排在最前：%+v", matched)
	}
	if matched.DisplayName != "Sodium 0.6.0" || matched.LoaderDisplay != "Fabric" {
		t.Fatalf("展示串没接上 models 的方法：%+v", matched)
	}
	if matched.FileName != "sodium.jar" || matched.FileSizeDisplay != "1 KB" {
		t.Fatalf("主文件信息不对：%+v", matched)
	}
	if matched.ReleaseType != "release" || matched.ReleaseTypeDisplay != "正式版" {
		t.Fatalf("发布类型不对：%+v", matched)
	}
	if matched.SHA1 != "abc" {
		t.Fatalf("SHA1 = %q，期望小写", matched.SHA1)
	}
	if !matched.DownloadAllowed {
		t.Fatal("Modrinth 文件应允许下载")
	}
	if !strings.Contains(result.Versions[1].MatchNote, "1.19.2") {
		t.Fatalf("不匹配版本应说明原因：%q", result.Versions[1].MatchNote)
	}
}

// TestListResourceVersionsRejectsEmptyProjectID 防的回归：空项目 ID 也发请求。
func TestListResourceVersionsRejectsEmptyProjectID(t *testing.T) {
	stub := newResourceServer(t, func(string) (int, string) { return http.StatusOK, resourceVersionsPayload })
	useModrinthStub(t, stub)

	if _, err := ListResourceVersions(context.Background(), models.ResourceVersionRequest{
		Source: models.ResourceSourceModrinth,
	}, ""); err == nil {
		t.Fatal("空项目 ID 必须报错")
	}
	if got := stub.count(); got != 0 {
		t.Fatalf("空项目 ID 不该发请求，实际 %d 次", got)
	}
}

// ---- 下载 ----

// TestDownloadResourceVersionInstallsIntoInstanceDirectory 防的回归：
// 文件没有落到 <内容目录>/<子目录>/<文件名>（装到别处游戏读不到），
// 或者进度回调没有上报（界面进度条永远 0%）。
func TestDownloadResourceVersionInstallsIntoInstanceDirectory(t *testing.T) {
	contentDirectory := t.TempDir()
	payload := []byte("mod-bytes")

	api := newResourceServer(t, func(path string) (int, string) {
		if path != "/version/Match001" {
			return http.StatusNotFound, ""
		}
		return http.StatusOK, strings.ReplaceAll(resourceVersionPayload, "CDN_URL", "https://cdn.example/mod.jar")
	})
	useModrinthStub(t, api)
	files := newResourceServer(t, func(string) (int, string) { return http.StatusOK, string(payload) })
	swapResourceResolver(t, func(rawURL string) (string, string) {
		if rawURL != "https://cdn.example/mod.jar" {
			t.Fatalf("解析的地址不对：%q", rawURL)
		}
		return files.baseURL() + "/mod.jar", ""
	})

	var downloaded, total int64
	result, err := DownloadResourceVersion(context.Background(), models.ResourceDownloadRequest{
		Source:           models.ResourceSourceModrinth,
		ProjectID:        "AANobbMI",
		VersionID:        "Match001",
		ContentDirectory: contentDirectory,
	}, "", func(d, tt int64) { downloaded, total = d, tt })
	if err != nil {
		t.Fatalf("下载不应失败：%v", err)
	}

	wantPath := filepath.Join(contentDirectory, "mods", "sodium.jar")
	if result.SavedPath != wantPath {
		t.Fatalf("保存路径 = %q，期望 %q", result.SavedPath, wantPath)
	}
	written, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("文件没有落盘：%v", err)
	}
	if string(written) != string(payload) {
		t.Fatalf("文件内容不对：%q", written)
	}
	if result.FileName != "sodium.jar" || result.UsedFallback {
		t.Fatalf("结果字段不对：%+v", result)
	}
	if downloaded != int64(len(payload)) || total != int64(len(payload)) {
		t.Fatalf("进度回调 = %d/%d，期望 %d/%d", downloaded, total, len(payload), len(payload))
	}
}

// TestDownloadResourceVersionUsesSubDirectoryFromRequest 防的回归：
// 光影包/材质包被装进 mods（游戏里找不到），或者子目录被当成绝对路径使用。
func TestDownloadResourceVersionUsesSubDirectoryFromRequest(t *testing.T) {
	contentDirectory := t.TempDir()

	api := newResourceServer(t, func(path string) (int, string) {
		return http.StatusOK, strings.ReplaceAll(resourceVersionPayload, "CDN_URL", "https://cdn.example/pack.zip")
	})
	useModrinthStub(t, api)
	files := newResourceServer(t, func(string) (int, string) { return http.StatusOK, "pack-bytes" })
	swapResourceResolver(t, func(string) (string, string) { return files.baseURL() + "/pack.zip", "" })

	result, err := DownloadResourceVersion(context.Background(), models.ResourceDownloadRequest{
		Source:           models.ResourceSourceModrinth,
		ProjectID:        "AANobbMI",
		VersionID:        "Match001",
		ContentDirectory: contentDirectory,
		SubDirectory:     "shaderpacks",
	}, "", nil)
	if err != nil {
		t.Fatalf("下载不应失败：%v", err)
	}
	if want := filepath.Join(contentDirectory, "shaderpacks", "sodium.jar"); result.SavedPath != want {
		t.Fatalf("保存路径 = %q，期望 %q", result.SavedPath, want)
	}
}

// TestDownloadResourceVersionRejectsMismatchedProject 防的回归：
// 版本 ID 与项目 ID 不匹配时照下不误（前端状态串了就会把别的项目的文件
// 装进用户实例，且完全无声）。
func TestDownloadResourceVersionRejectsMismatchedProject(t *testing.T) {
	api := newResourceServer(t, func(path string) (int, string) {
		return http.StatusOK, strings.ReplaceAll(resourceVersionPayload, "CDN_URL", "https://cdn.example/mod.jar")
	})
	useModrinthStub(t, api)
	swapResourceResolver(t, func(string) (string, string) {
		t.Fatal("项目不匹配时不该解析下载地址")
		return "", ""
	})

	_, err := DownloadResourceVersion(context.Background(), models.ResourceDownloadRequest{
		Source:           models.ResourceSourceModrinth,
		ProjectID:        "别的项目",
		VersionID:        "Match001",
		ContentDirectory: t.TempDir(),
	}, "", nil)
	if err == nil {
		t.Fatal("项目与版本不匹配必须拒绝下载")
	}
	if !strings.Contains(err.Error(), "不属于项目") {
		t.Fatalf("错误信息应说明项目不匹配：%v", err)
	}
}

// TestDownloadResourceVersionFallsBackToFallbackSource 防的回归：
// 主源失败就直接报错（回退源形同虚设），或者回退成功后仍标记为"未回退"。
func TestDownloadResourceVersionFallsBackToFallbackSource(t *testing.T) {
	contentDirectory := t.TempDir()
	payload := []byte("from-fallback")

	api := newResourceServer(t, func(path string) (int, string) {
		return http.StatusOK, strings.ReplaceAll(resourceVersionPayload, "CDN_URL", "https://cdn.example/mod.jar")
	})
	useModrinthStub(t, api)
	primary := newResourceServer(t, func(string) (int, string) { return http.StatusInternalServerError, "" })
	fallback := newResourceServer(t, func(string) (int, string) { return http.StatusOK, string(payload) })
	swapResourceResolver(t, func(string) (string, string) {
		return primary.baseURL() + "/mod.jar", fallback.baseURL() + "/mod.jar"
	})

	result, err := DownloadResourceVersion(context.Background(), models.ResourceDownloadRequest{
		Source:           models.ResourceSourceModrinth,
		ProjectID:        "AANobbMI",
		VersionID:        "Match001",
		ContentDirectory: contentDirectory,
	}, "", nil)
	if err != nil {
		t.Fatalf("回退源可用时不该失败：%v", err)
	}
	if !result.UsedFallback {
		t.Fatal("实际用了回退源却没有标记 UsedFallback（界面无法提示）")
	}
	if result.SourceURL != fallback.baseURL()+"/mod.jar" {
		t.Fatalf("SourceURL = %q，应为回退源地址", result.SourceURL)
	}
	written, err := os.ReadFile(filepath.Join(contentDirectory, "mods", "sodium.jar"))
	if err != nil {
		t.Fatalf("回退下载的文件没有落盘：%v", err)
	}
	if string(written) != string(payload) {
		t.Fatalf("文件内容不对：%q", written)
	}
}

// TestDownloadResourceVersionReportsBothFailures 防的回归：
// 主源与回退源都失败时只报一句"下载失败"，用户不知道该查哪个地址。
func TestDownloadResourceVersionReportsBothFailures(t *testing.T) {
	api := newResourceServer(t, func(path string) (int, string) {
		return http.StatusOK, strings.ReplaceAll(resourceVersionPayload, "CDN_URL", "https://cdn.example/mod.jar")
	})
	useModrinthStub(t, api)
	primary := newResourceServer(t, func(string) (int, string) { return http.StatusInternalServerError, "" })
	fallback := newResourceServer(t, func(string) (int, string) { return http.StatusInternalServerError, "" })
	swapResourceResolver(t, func(string) (string, string) {
		return primary.baseURL() + "/mod.jar", fallback.baseURL() + "/mod.jar"
	})

	_, err := DownloadResourceVersion(context.Background(), models.ResourceDownloadRequest{
		Source:           models.ResourceSourceModrinth,
		ProjectID:        "AANobbMI",
		VersionID:        "Match001",
		ContentDirectory: t.TempDir(),
	}, "", nil)
	if err == nil {
		t.Fatal("两个源都失败必须报错")
	}
	for _, want := range []string{"主源", "回退源"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误信息缺少 %q：%v", want, err)
		}
	}
}

// TestDownloadResourceVersionCurseForgeResolvesMissingDownloadURL 防的回归：
// /files 里 downloadUrl 为空时直接判死（作者其实允许分发，只是要再问一次
// download-url 端点），或者拿到了地址却不用。
func TestDownloadResourceVersionCurseForgeResolvesMissingDownloadURL(t *testing.T) {
	contentDirectory := t.TempDir()
	payload := []byte("jei-bytes")

	api := newResourceServer(t, func(path string) (int, string) {
		switch path {
		case "/mods/238222/files/8965084":
			return http.StatusOK, curseForgeSingleFileNoURLPayload
		case "/mods/238222/files/8965084/download-url":
			return http.StatusOK, `{"data":"https://edge.forgecdn.net/files/jei.jar"}`
		default:
			return http.StatusNotFound, ""
		}
	})
	useCurseForgeStub(t, api)
	files := newResourceServer(t, func(string) (int, string) { return http.StatusOK, string(payload) })
	swapResourceResolver(t, func(rawURL string) (string, string) {
		if rawURL != "https://edge.forgecdn.net/files/jei.jar" {
			t.Fatalf("应使用 download-url 端点解析出的地址，实际 %q", rawURL)
		}
		return files.baseURL() + "/jei.jar", ""
	})

	result, err := DownloadResourceVersion(context.Background(), models.ResourceDownloadRequest{
		Source:           models.ResourceSourceCurseForge,
		ProjectID:        "238222",
		VersionID:        "8965084",
		ContentDirectory: contentDirectory,
	}, "key", nil)
	if err != nil {
		t.Fatalf("下载不应失败：%v", err)
	}
	if want := filepath.Join(contentDirectory, "mods", "jei.jar"); result.SavedPath != want {
		t.Fatalf("保存路径 = %q，期望 %q", result.SavedPath, want)
	}
	if api.countPath("/mods/238222/files/8965084/download-url") != 1 {
		t.Fatal("downloadUrl 为空时必须问一次 download-url 端点")
	}
}

// TestDownloadResourceVersionCurseForgeRefusesUndistributableFile 防的回归：
// 作者禁止第三方分发时，错误提示写成"网络失败"（用户会一直重试而不是去网页下载）。
func TestDownloadResourceVersionCurseForgeRefusesUndistributableFile(t *testing.T) {
	api := newResourceServer(t, func(path string) (int, string) {
		switch path {
		case "/mods/238222/files/8965084":
			return http.StatusOK, `{"data":{"id":8965084,"modId":238222,"displayName":"JEI","fileName":"jei.jar",
			  "fileLength":2048,"downloadUrl":null,"gameVersions":["1.21.1"],"sortableGameVersions":[],"dependencies":[],"hashes":[]}}`
		default:
			return http.StatusForbidden, ""
		}
	})
	useCurseForgeStub(t, api)
	swapResourceResolver(t, func(string) (string, string) {
		t.Fatal("没有可用地址时不该进入下载")
		return "", ""
	})

	_, err := DownloadResourceVersion(context.Background(), models.ResourceDownloadRequest{
		Source:           models.ResourceSourceCurseForge,
		ProjectID:        "238222",
		VersionID:        "8965084",
		ContentDirectory: t.TempDir(),
	}, "key", nil)
	if err == nil {
		t.Fatal("作者禁止分发时必须报错")
	}
	if !strings.Contains(err.Error(), "不允许第三方下载") {
		t.Fatalf("错误信息应说明原因与替代做法：%v", err)
	}
}

// TestDownloadResourceVersionRejectsEmptyContentDirectory 防的回归：
// 目标目录为空时也照下（文件会落到当前工作目录，属于"写坏用户环境"）。
func TestDownloadResourceVersionRejectsEmptyContentDirectory(t *testing.T) {
	if _, err := DownloadResourceVersion(context.Background(), models.ResourceDownloadRequest{
		Source:    models.ResourceSourceModrinth,
		ProjectID: "AANobbMI",
		VersionID: "Match001",
	}, "", nil); err == nil {
		t.Fatal("内容目录为空必须报错")
	}
	if _, err := DownloadResourceVersion(context.Background(), models.ResourceDownloadRequest{
		Source:           models.ResourceSourceModrinth,
		ProjectID:        "AANobbMI",
		ContentDirectory: t.TempDir(),
	}, "", nil); err == nil {
		t.Fatal("版本 ID 为空必须报错")
	}
}

// TestResolveResourceURLGoesThroughSourceProvider 防的回归：
// 资源下载自己另写一套地址解析、绕开 DownloadSourceProvider——
// 那样设置里的下载源/回退源对资源下载就完全失效了。
func TestResolveResourceURLGoesThroughSourceProvider(t *testing.T) {
	originalActive := SourceProvider.Active()
	originalFallback := SourceProvider.Fallback()
	t.Cleanup(func() {
		SourceProvider.SetActive(originalActive)
		SourceProvider.SetFallback(originalFallback)
	})
	SourceProvider.SetActive(DownloadSources.Bmcl)
	official := DownloadSources.Official
	SourceProvider.SetFallback(&official)

	primary, fallback := resolveResourceURL("https://libraries.minecraft.net/net/example/lib.jar")
	if !strings.HasPrefix(primary, DownloadSources.Bmcl.Maven) {
		t.Fatalf("主用地址没有被活跃源（BMCL）改写：%q", primary)
	}
	if fallback != "https://libraries.minecraft.net/net/example/lib.jar" {
		t.Fatalf("回退地址应回到官方源：%q", fallback)
	}

	// 内容 CDN（Modrinth / CurseForge）目前没有国内镜像，解析结果应保持不变；
	// 哪天补上镜像，这条断言会失败——那时请连同文档一起更新，别默默改语义。
	cdnPrimary, cdnFallback := resolveResourceURL("https://cdn.modrinth.com/data/x.jar")
	if cdnPrimary != "https://cdn.modrinth.com/data/x.jar" || cdnFallback != cdnPrimary {
		t.Fatalf("内容 CDN 解析应保持不变，实际 %q / %q", cdnPrimary, cdnFallback)
	}
}

// TestNormalizeResourceSourceDefaultsToModrinth 防的回归：
// 未知/空资源站被当成 CurseForge（没有 Key，界面空白）而不是默认的 Modrinth。
func TestNormalizeResourceSourceDefaultsToModrinth(t *testing.T) {
	for _, input := range []string{"", "  ", "unknown", "MODRINTH"} {
		if got := normalizeResourceSource(input); got != models.ResourceSourceModrinth {
			t.Fatalf("normalizeResourceSource(%q) = %q，期望 modrinth", input, got)
		}
	}
	if got := normalizeResourceSource("CurseForge"); got != models.ResourceSourceCurseForge {
		t.Fatalf("大小写不同的 CurseForge 也应识别，实际 %q", got)
	}
}

// TestSearchResourcesModrinthUsesLoaderOrGroup 防的回归：
// 下载页 Mod 标签的"任意 Mod 加载器"过滤被写成内层 AND
// （[[project_type:mod, categories:fabric, categories:forge]]），
// 语义从"任一加载器"变成"同时是 Fabric 与 Forge"，结果永远是空列表。
func TestSearchResourcesModrinthUsesLoaderOrGroup(t *testing.T) {
	stub := newResourceServer(t, func(path string) (int, string) {
		return http.StatusOK, resourceSearchPayload
	})
	useModrinthStub(t, stub)

	if _, err := SearchResources(context.Background(), models.ResourceSearchRequest{
		Source:      models.ResourceSourceModrinth,
		ProjectType: models.ProjectTypeMod,
		Query:       "sodium",
		Loaders:     []string{"Fabric", "forge", "neoforge", "quilt"},
		Limit:       100,
	}, ""); err != nil {
		t.Fatalf("搜索不应失败：%v", err)
	}

	facets := stub.lastQuery(t).Get("facets")
	if got := stub.lastQuery(t).Get("limit"); got != "100" {
		t.Fatalf("limit = %q，下载页标签一次要 100 条", got)
	}
	// 期望形状：[[project_type:mod],["categories:fabric","categories:forge",...]]
	if !strings.HasPrefix(facets, `[["project_type:mod"],[`) {
		t.Fatalf("facets 形状不对（加载器必须是独立的 OR 层）：%q", facets)
	}
	for _, want := range []string{`"categories:fabric"`, `"categories:forge"`, `"categories:neoforge"`, `"categories:quilt"`} {
		if !strings.Contains(facets, want) {
			t.Fatalf("facets = %q，缺少 %s", facets, want)
		}
	}
	if strings.Contains(facets, `"project_type:mod","categories`) {
		t.Fatalf("加载器与 project_type 被塞进同一层，语义变成 AND：%q", facets)
	}
}

// TestClampResourceLimit 防的回归：条数上限收敛写错（把下载页要的 100 条砍到 40，
// 或者放任前端传 1000 条把一页塞爆）。
func TestClampResourceLimit(t *testing.T) {
	cases := map[int]int{
		0:    resourceSearchDefaultLimit,
		-5:   resourceSearchDefaultLimit,
		10:   10,
		100:  100,
		5000: resourceSearchMaxLimit,
	}
	for input, want := range cases {
		if got := clampResourceLimit(input); got != want {
			t.Fatalf("clampResourceLimit(%d) = %d，期望 %d", input, got, want)
		}
	}
}

// TestHostOfURL 防的回归：提示文案里的主机名解析不出来（写成整条 URL 或空串）。
func TestHostOfURL(t *testing.T) {
	cases := map[string]string{
		"https://cdn.modrinth.com/data/x.jar": "cdn.modrinth.com",
		"http://example.com:8080/a":           "example.com:8080",
		"https://edge.forgecdn.net":           "edge.forgecdn.net",
		"":                                    "",
	}
	for input, want := range cases {
		if got := hostOfURL(input); got != want {
			t.Fatalf("hostOfURL(%q) = %q，期望 %q", input, got, want)
		}
	}
}
