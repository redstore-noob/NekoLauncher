package curseforge

// CurseForge 客户端的离线用例：全部指向 httptest 假服务器，一条都不打真实网络。
//
// 重点盯三件事：
//  1. 鉴权与过滤参数（x-api-key 漏发 = 官方一律 403；gameId/classId 写错 = 搜出别的游戏）；
//  2. 官方失败时的镜像回退，以及"确定性失败（403/404/格式错）不回退"；
//  3. 错误必须是可读中文——把 403 原样抛出去，用户根本不知道要配 API Key。

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// searchPayload 搜索响应夹具（字段名与 CurseForge v1 一致，含可空 classId）。
const searchPayload = `{"data":[{
  "id":238222,"gameId":432,"name":"Just Enough Items","slug":"jei",
  "links":{"websiteUrl":"https://www.curseforge.com/minecraft/mc-mods/jei"},
  "summary":"查看物品配方","downloadCount":1500000,"classId":6,
  "authors":[{"id":1,"name":"mezz","url":"https://www.curseforge.com/members/mezz"}],
  "logo":{"title":"jei.png","thumbnailUrl":"https://media.forgecdn.net/t.png","url":"https://media.forgecdn.net/f.png"},
  "dateModified":"2024-06-13T08:00:00.000Z","allowModDistribution":true,"latestFiles":[]
}],"pagination":{"index":0,"pageSize":20,"resultCount":1,"totalCount":377}}`

// filesPayload 文件列表夹具：NeoForge 文件（sortableGameVersions 里带加载器）。
const filesPayload = `{"data":[{
  "id":8965084,"modId":238222,"displayName":"30.38.0.228 for NeoForge 26.2","fileName":"jei.jar",
  "releaseType":2,"fileDate":"2026-09-24T16:00:36.760Z","fileLength":2302732,"downloadCount":0,
  "downloadUrl":"https://edge.forgecdn.net/files/8965/84/jei.jar",
  "gameVersions":["Client","NeoForge","Server","1.21.1"],
  "sortableGameVersions":[
    {"gameVersionName":"Client","gameVersionTypeId":75208},
    {"gameVersionName":"NeoForge","gameVersionTypeId":68441},
    {"gameVersionName":"1.21.1","gameVersionTypeId":77784}],
  "dependencies":[{"modId":1700987,"relationType":3}],
  "hashes":[{"value":"44B5B014F16ED568682E4C2AAB8486484EC2128A","algo":1},{"value":"d8dd","algo":2}],
  "isAvailable":true
}],"pagination":{"index":0,"pageSize":50,"resultCount":1,"totalCount":1}}`

// stubEndpoint 假 CurseForge 端点：记录命中次数、路径、查询参数与请求头。
type stubEndpoint struct {
	server *httptest.Server

	mu      sync.Mutex
	hits    int
	paths   []string
	query   []url.Values
	apiKeys []string
}

func newStubEndpoint(t *testing.T, status int, body string) *stubEndpoint {
	t.Helper()
	stub := &stubEndpoint{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.hits++
		stub.paths = append(stub.paths, r.URL.Path)
		stub.query = append(stub.query, r.URL.Query())
		stub.apiKeys = append(stub.apiKeys, r.Header.Get("x-api-key"))
		stub.mu.Unlock()

		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *stubEndpoint) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

func (s *stubEndpoint) lastPath(t *testing.T) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.paths) == 0 {
		t.Fatal("假服务器没有收到任何请求")
	}
	return s.paths[len(s.paths)-1]
}

func (s *stubEndpoint) lastQuery(t *testing.T) url.Values {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.query) == 0 {
		t.Fatal("假服务器没有收到任何请求")
	}
	return s.query[len(s.query)-1]
}

func (s *stubEndpoint) lastAPIKey(t *testing.T) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.apiKeys) == 0 {
		t.Fatal("假服务器没有收到任何请求")
	}
	return s.apiKeys[len(s.apiKeys)-1]
}

// useStubEndpoints 把客户端指向假服务器；mirror 传 nil 表示不配置镜像。
func useStubEndpoints(t *testing.T, official, mirror *stubEndpoint) {
	t.Helper()
	officialURL, mirrorURL := "", ""
	client := http.DefaultClient
	if official != nil {
		officialURL = official.server.URL
		client = official.server.Client()
	}
	if mirror != nil {
		mirrorURL = mirror.server.URL
		client = mirror.server.Client()
	}
	SetEndpoints(officialURL, mirrorURL)
	SetHTTPClient(client)
	t.Cleanup(ResetEndpoints)
}

// TestSearchSendsAPIKeyAndMinecraftFilters 防的回归：
// x-api-key 没带上（官方一律 403）、gameId 写错（搜到别的游戏的内容）、
// classId / modLoaderType / gameVersion 过滤漏传（把不兼容的文件列出来）。
func TestSearchSendsAPIKeyAndMinecraftFilters(t *testing.T) {
	official := newStubEndpoint(t, http.StatusOK, searchPayload)
	useStubEndpoints(t, official, nil)

	result, err := Search(context.Background(), "  test-key  ", SearchOptions{
		Query:       "jei",
		ClassID:     6,
		GameVersion: "1.21.1",
		LoaderType:  6,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("搜索不应失败：%v", err)
	}
	if len(result.Data) != 1 || result.Data[0].ID != 238222 {
		t.Fatalf("解析结果不对：%+v", result.Data)
	}
	if result.Pagination.TotalCount != 377 {
		t.Fatalf("分页总数 = %d，期望 377", result.Pagination.TotalCount)
	}
	if got := official.lastAPIKey(t); got != "test-key" {
		t.Fatalf("x-api-key = %q，期望去掉首尾空格后的 test-key", got)
	}

	query := official.lastQuery(t)
	for key, want := range map[string]string{
		"gameId":        "432",
		"classId":       "6",
		"modLoaderType": "6",
		"gameVersion":   "1.21.1",
		"searchFilter":  "jei",
		"pageSize":      "10",
		"sortField":     "2",
		"sortOrder":     "desc",
	} {
		if got := query.Get(key); got != want {
			t.Fatalf("%s = %q，期望 %q", key, got, want)
		}
	}
	if path := official.lastPath(t); path != "/mods/search" {
		t.Fatalf("路径 = %q", path)
	}
}

// TestSearchClampsPageSize 防的回归：pageSize 超过官方的 50 上限，
// 官方直接返回 400，用户看到的是"搜索失败"而不是结果。
func TestSearchClampsPageSize(t *testing.T) {
	official := newStubEndpoint(t, http.StatusOK, searchPayload)
	useStubEndpoints(t, official, nil)

	if _, err := Search(context.Background(), "key", SearchOptions{Limit: 500}); err != nil {
		t.Fatalf("搜索不应失败：%v", err)
	}
	if got := official.lastQuery(t).Get("pageSize"); got != "50" {
		t.Fatalf("pageSize = %q，期望被收敛到 50", got)
	}

	if _, err := Search(context.Background(), "key", SearchOptions{Limit: -3}); err != nil {
		t.Fatalf("搜索不应失败：%v", err)
	}
	if got := official.lastQuery(t).Get("pageSize"); got != "20" {
		t.Fatalf("pageSize = %q，期望回落到默认 20", got)
	}
}

// TestSearchFallsBackToMirrorOnServerError 防的回归：
// 官方 5xx 之后不再尝试镜像——国内网络下等于 CurseForge 整个不可用。
func TestSearchFallsBackToMirrorOnServerError(t *testing.T) {
	official := newStubEndpoint(t, http.StatusInternalServerError, "")
	mirror := newStubEndpoint(t, http.StatusOK, searchPayload)
	useStubEndpoints(t, official, mirror)

	result, err := Search(context.Background(), "key", SearchOptions{Query: "jei", ClassID: 6})
	if err != nil {
		t.Fatalf("镜像可用时不该失败：%v", err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("镜像结果没有被解析：%+v", result.Data)
	}
	if !UsedMirror() {
		t.Fatal("回退成功却没有记录 UsedMirror")
	}
	if got := mirror.lastAPIKey(t); got != "key" {
		t.Fatalf("回退请求也必须带上 Key（镜像会校验格式），实际 %q", got)
	}
}

// TestForbiddenReportsAPIKeyHintWithoutFallback 防的回归：
// 403（Key 无效/没配）被当成网络抖动去回退与重试，
// 以及把 "HTTP 403" 直接抛给用户、不告诉他去哪里配 Key。
func TestForbiddenReportsAPIKeyHintWithoutFallback(t *testing.T) {
	official := newStubEndpoint(t, http.StatusForbidden, "")
	mirror := newStubEndpoint(t, http.StatusOK, searchPayload)
	useStubEndpoints(t, official, mirror)

	_, err := Search(context.Background(), "bad-key", SearchOptions{Query: "jei"})
	if err == nil {
		t.Fatal("403 必须报错")
	}
	if got := official.count(); got != 1 {
		t.Fatalf("官方请求次数 = %d，期望 1（鉴权失败不该重试）", got)
	}
	if got := mirror.count(); got != 0 {
		t.Fatalf("镜像请求次数 = %d，期望 0（不该偷偷绕过用户的 Key）", got)
	}
	if !strings.Contains(err.Error(), "API Key") || !strings.Contains(err.Error(), APIKeyApplyURL) {
		t.Fatalf("错误信息应说明 Key 有问题并给出申请地址：%s", err.Error())
	}
}

// TestForbiddenWithoutKeyExplainsHowToConfigure 防的回归：
// 完全没带 Key 时提示语写成"Key 无效"（用户以为配错了，其实根本没配）。
func TestForbiddenWithoutKeyExplainsHowToConfigure(t *testing.T) {
	official := newStubEndpoint(t, http.StatusUnauthorized, "")
	useStubEndpoints(t, official, nil)

	_, err := Search(context.Background(), "", SearchOptions{Query: "jei"})
	if err == nil {
		t.Fatal("401 必须报错")
	}
	if !strings.Contains(err.Error(), "需要 API Key") {
		t.Fatalf("未配置 Key 时提示应说明「需要先在设置里填写」：%s", err.Error())
	}
}

// TestSearchErrorMentionsBothEndpoints 防的回归：两个地址都失败时只报原始英文错误。
func TestSearchErrorMentionsBothEndpoints(t *testing.T) {
	official := newStubEndpoint(t, http.StatusInternalServerError, "")
	mirror := newStubEndpoint(t, http.StatusBadGateway, "")
	useStubEndpoints(t, official, mirror)

	_, err := Search(context.Background(), "key", SearchOptions{Query: "jei"})
	if err == nil {
		t.Fatal("两个地址都失败时必须报错")
	}
	for _, want := range []string{"CurseForge", "国内镜像", "api.curseforge.com", "mod.mcimirror.top", "网络"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误信息缺少 %q：%s", want, err.Error())
		}
	}
}

// TestGetFilesSendsFiltersAndParsesLoaders 防的回归：
// 文件列表漏传 gameVersion/modLoaderType（列出不兼容的历史文件），
// 或者 sortableGameVersions 里的加载器被当成游戏版本解析。
func TestGetFilesSendsFiltersAndParsesLoaders(t *testing.T) {
	official := newStubEndpoint(t, http.StatusOK, filesPayload)
	useStubEndpoints(t, official, nil)

	files, err := GetFiles(context.Background(), "key", "238222", "1.21.1", 6, 50)
	if err != nil {
		t.Fatalf("查询文件不应失败：%v", err)
	}
	if len(files) != 1 {
		t.Fatalf("文件条数 = %d", len(files))
	}
	file := files[0]
	if got := file.LoaderDisplay(); got != "NeoForge" {
		t.Fatalf("LoaderDisplay = %q，期望 NeoForge", got)
	}
	if got := file.MinecraftVersions(); len(got) != 1 || got[0] != "1.21.1" {
		t.Fatalf("MC 版本 = %v，期望只有 1.21.1（客户端/服务端/加载器标记要剔除）", got)
	}
	if got := file.SHA1(); got != "44b5b014f16ed568682e4c2aab8486484ec2128a" {
		t.Fatalf("SHA1 = %q，期望小写哈希", got)
	}
	if got := file.ReleaseTypeDisplay(); got != "测试版" {
		t.Fatalf("发布类型 = %q，期望 测试版", got)
	}

	if path := official.lastPath(t); path != "/mods/238222/files" {
		t.Fatalf("路径 = %q", path)
	}
	query := official.lastQuery(t)
	if query.Get("gameVersion") != "1.21.1" || query.Get("modLoaderType") != "6" {
		t.Fatalf("过滤参数丢失：%v", query)
	}
}

// TestGetFileAndResolveDownloadURL 防的回归：
// 单文件响应 / 下载地址响应外层的 data 包装没解析（拿到的是零值，下载必失败）。
func TestGetFileAndResolveDownloadURL(t *testing.T) {
	official := newStubEndpoint(t, http.StatusOK, `{"data":`+singleFilePayload+`}`)
	useStubEndpoints(t, official, nil)

	file, err := GetFile(context.Background(), "key", "238222", "8965084")
	if err != nil {
		t.Fatalf("查询单个文件不应失败：%v", err)
	}
	if file.FileName != "jei.jar" || file.DownloadURL == nil {
		t.Fatalf("文件信息不对：%+v", file)
	}
	if path := official.lastPath(t); path != "/mods/238222/files/8965084" {
		t.Fatalf("路径 = %q", path)
	}

	officialWithURL := newStubEndpoint(t, http.StatusOK, `{"data":"https://edge.forgecdn.net/files/8965/84/jei.jar"}`)
	useStubEndpoints(t, officialWithURL, nil)

	resolved, err := ResolveDownloadURL(context.Background(), "key", "238222", "8965084")
	if err != nil {
		t.Fatalf("解析下载地址不应失败：%v", err)
	}
	if resolved != "https://edge.forgecdn.net/files/8965/84/jei.jar" {
		t.Fatalf("下载地址 = %q", resolved)
	}
	if path := officialWithURL.lastPath(t); path != "/mods/238222/files/8965084/download-url" {
		t.Fatalf("路径 = %q", path)
	}
}

// singleFilePayload 单个文件夹具（GetFile 用）。
const singleFilePayload = `{
  "id":8965084,"modId":238222,"displayName":"30.38.0.228 for NeoForge 26.2","fileName":"jei.jar",
  "releaseType":2,"fileDate":"2026-09-24T16:00:36.760Z","fileLength":2302732,
  "downloadUrl":"https://edge.forgecdn.net/files/8965/84/jei.jar",
  "gameVersions":["1.21.1"],"sortableGameVersions":[],"dependencies":[],"hashes":[],"isAvailable":true}`

// TestInvalidNumericIDsFailWithoutRequest 防的回归：
// 非数字的项目/文件 ID 也去发请求，打出一个必然 404 的地址，
// 用户看到的是"网络错误"而不是"参数不对"。
func TestInvalidNumericIDsFailWithoutRequest(t *testing.T) {
	official := newStubEndpoint(t, http.StatusOK, filesPayload)
	useStubEndpoints(t, official, nil)

	if _, err := GetFiles(context.Background(), "key", "sodium", "", 0, 10); err == nil {
		t.Fatal("非数字项目 ID 必须报错")
	}
	if _, err := GetFile(context.Background(), "key", "238222", "abc"); err == nil {
		t.Fatal("非数字文件 ID 必须报错")
	}
	if _, err := ResolveDownloadURL(context.Background(), "key", "-1", "1"); err == nil {
		t.Fatal("非正数项目 ID 必须报错")
	}
	if got := official.count(); got != 0 {
		t.Fatalf("非法 ID 不该发请求，实际发了 %d 次", got)
	}
}

// TestDecodeFailureIsNotRetriedNorMirrored 防的回归：
// 响应格式异常（例如被网关换成了 HTML）也算"值得重试"，
// 把一次确定性失败放大成三次请求。
func TestDecodeFailureIsNotRetriedNorMirrored(t *testing.T) {
	official := newStubEndpoint(t, http.StatusOK, "<html>502 Bad Gateway</html>")
	mirror := newStubEndpoint(t, http.StatusOK, searchPayload)
	useStubEndpoints(t, official, mirror)

	if _, err := Search(context.Background(), "key", SearchOptions{Query: "jei"}); err == nil {
		t.Fatal("响应不是 JSON 时必须报错")
	}
	if got := official.count(); got != 1 {
		t.Fatalf("官方请求次数 = %d，期望 1", got)
	}
	if got := mirror.count(); got != 0 {
		t.Fatalf("镜像请求次数 = %d，期望 0", got)
	}
}

// TestHostHelpersDeriveFromConstants 防的回归：界面提示里的主机名另写一份常量，
// 改了根地址忘了改提示。
func TestHostHelpersDeriveFromConstants(t *testing.T) {
	if got := APIHost(); got != "api.curseforge.com" {
		t.Fatalf("APIHost = %q", got)
	}
	if got := MirrorHost(); got != "mod.mcimirror.top" {
		t.Fatalf("MirrorHost = %q", got)
	}
	if !strings.Contains(APIKeyHint, APIKeyApplyURL) {
		t.Fatal("未配置 Key 的引导里必须包含申请地址")
	}
}
