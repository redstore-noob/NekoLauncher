package modrinth

// Modrinth 客户端的离线用例：全部指向 httptest 假服务器，一条都不打真实网络。
//
// 覆盖三件容易悄悄坏掉的事：
//  1. facets / 过滤参数的拼法（拼错不会报错，只会静默搜出不相干的结果）；
//  2. 官方失败时的镜像回退与"确定性失败不重试"（多试一次只是让用户多等一倍）；
//  3. 最终抛给界面的错误必须是可读中文（把 http.Client 的英文错误直接甩出去
//     等于让用户自己猜）。

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

// searchPayload 搜索结果夹具（字段名与 Modrinth v2 一致）。
const searchPayload = `{"hits":[
  {"project_id":"AANobbMI","title":"Sodium","description":"现代渲染优化","icon_url":"https://cdn.modrinth.com/x.png",
   "project_type":"mod","downloads":1500,"follows":42,"slug":"sodium","date_created":"2024-06-13T08:00:00Z"}
],"offset":0,"limit":10,"total_hits":1}`

// versionsPayload 版本列表夹具：一个正式版（带 sha1）与一个测试版。
const versionsPayload = `[
 {"id":"RncWhTxD","project_id":"AANobbMI","name":"Sodium 0.5.11","version_number":"mc1.21-0.5.11",
  "version_type":"release","game_versions":["1.21","1.21.1"],"loaders":["fabric"],
  "date_published":"2024-06-13T08:00:00Z",
  "files":[{"url":"https://cdn.modrinth.com/data/x.jar","filename":"sodium.jar","size":1024,"primary":true,
            "hashes":{"sha1":"AABBCC","sha512":"DD"}}]},
 {"id":"Beta0001","project_id":"AANobbMI","name":"","version_number":"mc1.20-0.4.0",
  "version_type":"beta","game_versions":["1.20.1"],"loaders":["fabric"],"date_published":"2023-01-02T00:00:00Z",
  "files":[]}
]`

// versionPayload 单版本夹具（下载路径靠它拿到文件地址）。
const versionPayload = `{"id":"RncWhTxD","project_id":"AANobbMI","name":"Sodium 0.5.11",
 "version_number":"mc1.21-0.5.11","version_type":"release","game_versions":["1.21.1"],"loaders":["fabric"],
 "date_published":"2024-06-13T08:00:00Z",
 "files":[{"url":"https://cdn.modrinth.com/data/x.jar","filename":"sodium.jar","size":2048,"primary":true,
           "hashes":{"sha1":"AABBCC"}}]}`

// stubEndpoint 假 Modrinth 端点：记录命中次数与最近一次的路径/查询参数，
// 用来断言"到底打了官方还是镜像、打了几次"。
type stubEndpoint struct {
	server *httptest.Server

	mu    sync.Mutex
	hits  int
	paths []string
	query []url.Values
}

func newStubEndpoint(t *testing.T, status int, body string) *stubEndpoint {
	t.Helper()
	stub := &stubEndpoint{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.hits++
		stub.paths = append(stub.paths, r.URL.Path)
		stub.query = append(stub.query, r.URL.Query())
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

// TestSearchWithLoaderSendsFiltersAndParsesHits 防的回归：
// facets 拼错（少了 project_type/versions/categories，或把 AND 关系拆成两层）
// 会静默搜出不相干的资源——尤其是把 Forge 模组装进 Fabric 实例。
func TestSearchWithLoaderSendsFiltersAndParsesHits(t *testing.T) {
	official := newStubEndpoint(t, http.StatusOK, searchPayload)
	useStubEndpoints(t, official, nil)

	hits, err := SearchWithLoader(context.Background(), "mod", "sodium", "1.21.1", "fabric", 15)
	if err != nil {
		t.Fatalf("搜索不应失败：%v", err)
	}
	if len(hits) != 1 || hits[0].ProjectID != "AANobbMI" {
		t.Fatalf("解析结果不对：%+v", hits)
	}

	if path := official.lastPath(t); path != "/search" {
		t.Fatalf("路径 = %q，期望 /search", path)
	}
	query := official.lastQuery(t)
	if got := query.Get("query"); got != "sodium" {
		t.Fatalf("query = %q，期望 sodium", got)
	}
	if got := query.Get("limit"); got != "15" {
		t.Fatalf("limit = %q，期望 15", got)
	}
	facets := query.Get("facets")
	for _, want := range []string{`"project_type:mod"`, `"versions:1.21.1"`, `"categories:fabric"`} {
		if !strings.Contains(facets, want) {
			t.Fatalf("facets = %q，缺少 %s", facets, want)
		}
	}
	// 三个 facet 必须在同一层（内层 AND），否则 Modrinth 会按 OR 处理
	if strings.Contains(facets, "],[") {
		t.Fatalf("facets 出现了内层分组，语义会从 AND 变成 OR：%q", facets)
	}
}

// TestSearchFallsBackToMirrorWhenOfficialFails 防的回归：
// 官方 5xx 之后不再尝试镜像（国内网络下等于"搜索永远失败"），
// 或者回退成功却不记录 UsedMirror（界面无法提示"已走镜像"）。
func TestSearchFallsBackToMirrorWhenOfficialFails(t *testing.T) {
	official := newStubEndpoint(t, http.StatusInternalServerError, "")
	mirror := newStubEndpoint(t, http.StatusOK, searchPayload)
	useStubEndpoints(t, official, mirror)

	hits, err := SearchQuery(context.Background(), "mod", "sodium", 10)
	if err != nil {
		t.Fatalf("镜像可用时不该失败：%v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("镜像返回的结果没有被解析：%+v", hits)
	}
	if !UsedMirror() {
		t.Fatal("回退成功却没有记录 UsedMirror，界面无法提示已切换镜像")
	}
	if got := official.count(); got != 1 {
		t.Fatalf("官方请求次数 = %d，期望 1（成功后不该再打）", got)
	}
	if got := mirror.count(); got != 1 {
		t.Fatalf("镜像请求次数 = %d，期望 1", got)
	}
}

// TestSearchPrefersMirrorAfterFallbackSuccess 防的回归：
// 回退成功后下一次仍然先等官方超时——国内网络下每次搜索都会固定慢一个超时周期。
func TestSearchPrefersMirrorAfterFallbackSuccess(t *testing.T) {
	official := newStubEndpoint(t, http.StatusInternalServerError, "")
	mirror := newStubEndpoint(t, http.StatusOK, searchPayload)
	useStubEndpoints(t, official, mirror)

	if _, err := SearchQuery(context.Background(), "mod", "sodium", 10); err != nil {
		t.Fatalf("第一次搜索应靠镜像成功：%v", err)
	}
	if _, err := SearchQuery(context.Background(), "mod", "lithium", 10); err != nil {
		t.Fatalf("第二次搜索应靠镜像成功：%v", err)
	}

	if got := official.count(); got != 1 {
		t.Fatalf("官方请求次数 = %d，期望 1（第二次应直接走镜像，不再等官方超时）", got)
	}
	if got := mirror.count(); got != 2 {
		t.Fatalf("镜像请求次数 = %d，期望 2", got)
	}
}

// TestSearchDoesNotFallbackOnNotFound 防的回归：
// 404（项目不存在/参数不对）这种确定性失败也去回退与重试——
// 白白把一次失败放大成三次请求，界面还要多等两个超时。
func TestSearchDoesNotFallbackOnNotFound(t *testing.T) {
	official := newStubEndpoint(t, http.StatusNotFound, `{"error":"not_found"}`)
	mirror := newStubEndpoint(t, http.StatusOK, searchPayload)
	useStubEndpoints(t, official, mirror)

	if _, err := SearchQuery(context.Background(), "mod", "不存在的项目", 10); err == nil {
		t.Fatal("404 必须报错，不能伪装成空结果")
	}
	if got := official.count(); got != 1 {
		t.Fatalf("官方请求次数 = %d，期望 1（确定性失败不该重试）", got)
	}
	if got := mirror.count(); got != 0 {
		t.Fatalf("镜像请求次数 = %d，期望 0（确定性失败不该回退）", got)
	}
}

// TestSearchErrorMentionsBothEndpointsAndNetwork 防的回归：
// 两个地址都失败时把原始英文错误直接抛给用户，用户不知道该检查什么。
func TestSearchErrorMentionsBothEndpointsAndNetwork(t *testing.T) {
	official := newStubEndpoint(t, http.StatusInternalServerError, "")
	mirror := newStubEndpoint(t, http.StatusBadGateway, "")
	useStubEndpoints(t, official, mirror)

	_, err := SearchQuery(context.Background(), "mod", "sodium", 10)
	if err == nil {
		t.Fatal("两个地址都失败时必须返回错误")
	}
	message := err.Error()
	for _, want := range []string{"Modrinth", "国内镜像", "api.modrinth.com", "mod.mcimirror.top", "网络"} {
		if !strings.Contains(message, want) {
			t.Fatalf("错误信息缺少 %q，用户无法判断问题：%s", want, message)
		}
	}
}

// TestSearchWithoutMirrorReportsOfficialOnlyHint 防的回归：
// 没配置镜像时错误里却写"镜像也失败了"，把人引去查一个根本不存在的地址。
func TestSearchWithoutMirrorReportsOfficialOnlyHint(t *testing.T) {
	official := newStubEndpoint(t, http.StatusInternalServerError, "")
	useStubEndpoints(t, official, nil)

	_, err := SearchQuery(context.Background(), "mod", "sodium", 10)
	if err == nil {
		t.Fatal("官方失败且无镜像时必须返回错误")
	}
	if strings.Contains(err.Error(), "镜像") {
		t.Fatalf("未配置镜像却提示镜像失败：%s", err.Error())
	}
	if !strings.Contains(err.Error(), "api.modrinth.com") {
		t.Fatalf("错误信息应写明打的是哪个地址：%s", err.Error())
	}
}

// TestGetVersionsSendsFiltersAndParses 防的回归：
// game_versions / loaders 的 JSON 数组被拼成裸字符串（Modrinth 需要 ["1.21.1"]），
// 服务端会直接忽略过滤条件，把不兼容的版本列出来。
func TestGetVersionsSendsFiltersAndParses(t *testing.T) {
	official := newStubEndpoint(t, http.StatusOK, versionsPayload)
	useStubEndpoints(t, official, nil)

	versions, err := GetVersions(context.Background(), "AANobbMI", []string{"1.21.1"}, []string{"fabric"})
	if err != nil {
		t.Fatalf("查询版本不应失败：%v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("版本条数 = %d，期望 2", len(versions))
	}
	if versions[0].VersionType != "release" || versions[1].VersionType != "beta" {
		t.Fatalf("version_type 未解析：%+v", versions)
	}
	if versions[1].DisplayName() != "mc1.20-0.4.0" {
		t.Fatalf("空 name 应回退版本号：%q", versions[1].DisplayName())
	}

	if path := official.lastPath(t); path != "/project/AANobbMI/version" {
		t.Fatalf("路径 = %q", path)
	}
	query := official.lastQuery(t)
	if got := query.Get("game_versions"); got != `["1.21.1"]` {
		t.Fatalf("game_versions = %q，期望 [\"1.21.1\"]", got)
	}
	if got := query.Get("loaders"); got != `["fabric"]` {
		t.Fatalf("loaders = %q，期望 [\"fabric\"]", got)
	}
}

// TestGetVersionsRejectsEmptyProjectID 防的回归：空 projectID 也发请求，
// 打出一个 /project//version 的无效地址（服务端 404，界面却显示"没有版本"）。
func TestGetVersionsRejectsEmptyProjectID(t *testing.T) {
	official := newStubEndpoint(t, http.StatusOK, versionsPayload)
	useStubEndpoints(t, official, nil)

	if _, err := GetVersions(context.Background(), "   ", nil, nil); err == nil {
		t.Fatal("空 projectID 必须报错")
	}
	if got := official.count(); got != 0 {
		t.Fatalf("空 projectID 不该发请求，实际发了 %d 次", got)
	}
}

// TestGetVersionsParseFailureReturnsEmptyList 防的回归：
// 响应格式异常被当成"网络失败"上抛（调用方据此报错），
// 或者因为是格式异常就反复重试——两者都会让用户看到假的故障提示。
func TestGetVersionsParseFailureReturnsEmptyList(t *testing.T) {
	official := newStubEndpoint(t, http.StatusOK, "这不是 JSON")
	mirror := newStubEndpoint(t, http.StatusOK, versionsPayload)
	useStubEndpoints(t, official, mirror)

	versions, err := GetVersions(context.Background(), "AANobbMI", nil, nil)
	if err != nil {
		t.Fatalf("格式异常按既有约定返回空列表，不该上抛：%v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("期望空列表，得到 %+v", versions)
	}
	if got := official.count(); got != 1 {
		t.Fatalf("官方请求次数 = %d，期望 1（解析失败是确定性失败，不该重试）", got)
	}
	if got := mirror.count(); got != 0 {
		t.Fatalf("镜像请求次数 = %d，期望 0（解析失败不该回退）", got)
	}
}

// TestGetSupportedGameVersionsSortsNumerically 防的回归：
// 支持的 MC 版本按字符串排序（1.9.4 排在 1.10.2 之后），下拉框顺序错乱。
func TestGetSupportedGameVersionsSortsNumerically(t *testing.T) {
	official := newStubEndpoint(t, http.StatusOK, versionsPayload)
	useStubEndpoints(t, official, nil)

	versions, err := GetSupportedGameVersions(context.Background(), "AANobbMI")
	if err != nil {
		t.Fatalf("查询支持版本不应失败：%v", err)
	}
	want := []string{"1.21.1", "1.21", "1.20.1"}
	if len(versions) != len(want) {
		t.Fatalf("支持版本 = %v，期望 %v", versions, want)
	}
	for index := range want {
		if versions[index] != want[index] {
			t.Fatalf("支持版本 = %v，期望 %v", versions, want)
		}
	}
}

// TestGetVersionReadsPrimaryFileAndSHA1 防的回归：
// 下载路径按版本 ID 反查文件时拿不到主文件或哈希（哈希是将来校验下载完整性的依据）。
func TestGetVersionReadsPrimaryFileAndSHA1(t *testing.T) {
	official := newStubEndpoint(t, http.StatusOK, versionPayload)
	useStubEndpoints(t, official, nil)

	version, err := GetVersion(context.Background(), "RncWhTxD")
	if err != nil {
		t.Fatalf("查询单个版本不应失败：%v", err)
	}
	if path := official.lastPath(t); path != "/version/RncWhTxD" {
		t.Fatalf("路径 = %q", path)
	}
	file := version.PrimaryFile()
	if file == nil {
		t.Fatal("应能取到主文件")
	}
	if file.Filename != "sodium.jar" || file.URL == "" {
		t.Fatalf("主文件信息不对：%+v", file)
	}
	if got := file.SHA1(); got != "aabbcc" {
		t.Fatalf("SHA1 = %q，期望小写 aabbcc", got)
	}
	if got := version.VersionTypeDisplay(); got != "正式版" {
		t.Fatalf("发布类型展示 = %q，期望 正式版", got)
	}
}

// TestGetVersionRejectsEmptyVersionID 防的回归：空版本 ID 也发请求。
func TestGetVersionRejectsEmptyVersionID(t *testing.T) {
	official := newStubEndpoint(t, http.StatusOK, versionPayload)
	useStubEndpoints(t, official, nil)

	if _, err := GetVersion(context.Background(), ""); err == nil {
		t.Fatal("空 versionID 必须报错")
	}
	if got := official.count(); got != 0 {
		t.Fatalf("空 versionID 不该发请求，实际发了 %d 次", got)
	}
}

// TestHostHelpersDeriveFromConstants 防的回归：界面提示里的主机名被另写一份
// 字符串常量，改了根地址却忘了改提示，用户按提示排查的是错误域名。
func TestHostHelpersDeriveFromConstants(t *testing.T) {
	if got := APIHost(); got != "api.modrinth.com" {
		t.Fatalf("APIHost = %q", got)
	}
	if got := MirrorHost(); got != "mod.mcimirror.top" {
		t.Fatalf("MirrorHost = %q", got)
	}
	if !strings.Contains(OfficialAPIRoot, APIHost()) || !strings.Contains(MirrorAPIRoot, MirrorHost()) {
		t.Fatal("提示用主机名必须来自根地址常量")
	}
}
