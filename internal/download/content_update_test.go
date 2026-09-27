package download_test

// X-4 更新检测的离线用例：全部走 httptest 假 Modrinth，默认不联网。
//
// 每个用例都写明它防的是哪条回归；核心三条是：
//   - 查不到 ≠ 没有更新（unknown 与 latest 必须分开）；
//   - 批量查询（一次请求带多个哈希），不是一文件一请求；
//   - 网络失败要如实报失败，不能让整个实例页渲染不出来。

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/download"
	"nekolauncher/internal/download/modrinth"
)

// ---------------------------------------------------------------------------
// 夹具
// ---------------------------------------------------------------------------

// testFile 实例目录里一个待检测文件。
type testFile struct {
	relative string
	content  string
}

// buildContentDirectory 造一个实例内容目录（假 jar / zip，不联网）。
func buildContentDirectory(t *testing.T, files []testFile) string {
	t.Helper()
	root := t.TempDir()
	for _, file := range files {
		full := filepath.Join(root, filepath.FromSlash(file.relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(file.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func sha1OfText(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

// modrinthVersionJSON 假 Modrinth 的一个版本对象。
type modrinthVersionJSON struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	VersionNumber string   `json:"version_number"`
	GameVersions  []string `json:"game_versions"`
	Loaders       []string `json:"loaders"`
	DatePublished string   `json:"date_published"`
	Files         []struct {
		URL      string            `json:"url"`
		Filename string            `json:"filename"`
		Size     int64             `json:"size"`
		Hashes   map[string]string `json:"hashes"`
		Primary  bool              `json:"primary"`
	} `json:"files"`
}

func makeVersion(versionID, projectID, versionNumber, sha1Hex, downloadURL string, size int64) modrinthVersionJSON {
	version := modrinthVersionJSON{
		ID:            versionID,
		ProjectID:     projectID,
		VersionNumber: versionNumber,
		GameVersions:  []string{"1.21.1"},
		Loaders:       []string{"fabric"},
		DatePublished: "2026-02-03T10:00:00Z",
	}
	var file struct {
		URL      string            `json:"url"`
		Filename string            `json:"filename"`
		Size     int64             `json:"size"`
		Hashes   map[string]string `json:"hashes"`
		Primary  bool              `json:"primary"`
	}
	file.URL = downloadURL
	file.Filename = filepath.Base(downloadURL)
	file.Size = size
	file.Hashes = map[string]string{"sha1": sha1Hex}
	file.Primary = true
	version.Files = append(version.Files, file)
	return version
}

// fakeModrinth 假 Modrinth 服务器。
// lookup/update 分别回答 /version_files 与 /version_files/update；
// projectNames 回答 /project/{id}（用于项目名回填）。
type fakeModrinth struct {
	// lookup 响应体（按哈希键的版本对象）。
	lookup map[string]modrinthVersionJSON
	// update 响应体（可以是扁平数组，也可以按 project_id 分组）。
	update any
	// projectNames projectID → 项目名。
	projectNames map[string]string

	// lookupStatus / updateStatus 非 0 时直接返回该状态码（模拟 404/500/429）。
	lookupStatus int
	updateStatus int

	// lookupRequests / updateRequests / projectRequests 请求计数。
	lookupRequests  atomic.Int64
	updateRequests  atomic.Int64
	projectRequests atomic.Int64
	// lastLookupHashes / lastUpdateHashes 最近一次请求提交的哈希（批量口径断言用）。
	lastLookupHashes []string
	lastUpdateHashes []string
	mu               sync.Mutex

	// server 假服务器。
	server *httptest.Server
}

func newFakeModrinth(t *testing.T, fake *fakeModrinth) *fakeModrinth {
	t.Helper()
	fake.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var payload struct {
			Hashes []string `json:"hashes"`
		}
		_ = json.Unmarshal(body, &payload)

		switch {
		case strings.HasSuffix(request.URL.Path, "/version_files/update"):
			fake.updateRequests.Add(1)
			fake.mu.Lock()
			fake.lastUpdateHashes = payload.Hashes
			fake.mu.Unlock()
			if fake.updateStatus != 0 {
				writer.WriteHeader(fake.updateStatus)
				return
			}
			answering := map[string]modrinthVersionJSON{}
			collectFromUpdatePayload(fake.update, payload.Hashes, answering)
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(answering)
		case strings.HasSuffix(request.URL.Path, "/version_files"):
			fake.lookupRequests.Add(1)
			fake.mu.Lock()
			fake.lastLookupHashes = payload.Hashes
			fake.mu.Unlock()
			if fake.lookupStatus != 0 {
				writer.WriteHeader(fake.lookupStatus)
				return
			}
			answering := map[string]modrinthVersionJSON{}
			for _, hash := range payload.Hashes {
				if version, ok := fake.lookup[hash]; ok {
					answering[hash] = version
				}
			}
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(answering)
		case strings.Contains(request.URL.Path, "/project/"):
			fake.projectRequests.Add(1)
			projectID := request.URL.Path[strings.LastIndex(request.URL.Path, "/")+1:]
			name, ok := fake.projectNames[projectID]
			if !ok {
				writer.WriteHeader(http.StatusNotFound)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(map[string]string{"title": name, "slug": projectID})
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(fake.server.Close)

	// 假服务器是本包唯一允许的出站目标：镜像一并指过来，避免任何回退请求真的出网。
	modrinth.SetEndpoints(fake.server.URL, fake.server.URL)
	t.Cleanup(modrinth.ResetEndpoints)
	return fake
}

// collectFromUpdatePayload 从 update 夹具里挑出本次请求哈希对应的版本。
// 支持两种形状（扁平数组 / 按 project_id 分组的对象），与真实端点一致。
func collectFromUpdatePayload(payload any, hashes []string, target map[string]modrinthVersionJSON) {
	wanted := map[string]bool{}
	for _, hash := range hashes {
		wanted[hash] = true
	}
	switch typed := payload.(type) {
	case []modrinthVersionJSON:
		for _, version := range typed {
			registerVersion(version, wanted, target)
		}
	case map[string][]modrinthVersionJSON:
		for _, versions := range typed {
			for _, version := range versions {
				registerVersion(version, wanted, target)
			}
		}
	}
}

func registerVersion(version modrinthVersionJSON, wanted map[string]bool, target map[string]modrinthVersionJSON) {
	for _, file := range version.Files {
		hash := strings.ToLower(file.Hashes["sha1"])
		if wanted[hash] {
			target[hash] = version
		}
	}
}

// withResponseHash 把版本对象的文件哈希改成「应答所使用的键哈希」。
// 真实 Modrinth 的 version_files/update 是**按请求里的哈希为键**回答的
// （键是查询用的旧哈希，版本对象里带的却是新文件的哈希），夹具必须照此模拟，
// 否则测的就只是我们自己想象的响应形状。
func withResponseHash(version modrinthVersionJSON, hash string) modrinthVersionJSON {
	version.Files[0].Hashes = map[string]string{"sha1": hash}
	return version
}

// buildMinimalMrpack 造一个最小 .mrpack（含 modrinth.index.json + overrides 内容）。
func buildMinimalMrpack(t *testing.T, name, version string, entries []map[string]any) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "fixture.mrpack")
	file, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	writeEntry := func(entryName, content string) {
		entry, createErr := writer.Create(entryName)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	writeEntry("modrinth.index.json", mrpackIndexJSON(name, version, entries))
	writeEntry("overrides/config/example.toml", "key=1")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return target
}

// ---------------------------------------------------------------------------
// 用例：批量查询与状态判定
// ---------------------------------------------------------------------------

// TestCheckContentUpdatesReportsOnlyKnownUpdates 防的回归：
// 把「Modrinth 上查不到」的文件也标成可更新/已是最新——谎报更新或谎报无需更新。
// 同时核对批量口径：5 个文件去重后 3 个哈希，只需要 2 个请求（反查 1 + 最新 1）。
func TestCheckContentUpdatesReportsOnlyKnownUpdates(t *testing.T) {
	updatableOld := "sodium-old-jar"
	latestJar := "lithium-latest-jar"
	unknownJar := "handmade-mod-jar"

	root := buildContentDirectory(t, []testFile{
		{"mods/sodium.jar", updatableOld},       // 有更新
		{"mods/lithium.jar", latestJar},         // 已是最新
		{"mods/my-own-mod.jar", unknownJar},     // 查不到 = 未知
		{"mods/sodium-copy.jar", updatableOld},  // 与 sodium.jar 同哈希（重复内容）
		{"mods/disabled.jar.disabled", "zzzzz"}, // 禁用态 = 跳过
	})

	fake := newFakeModrinth(t, &fakeModrinth{
		lookup: map[string]modrinthVersionJSON{
			sha1OfText(updatableOld): makeVersion("ver-old", "AABBCC", "1.0.0",
				sha1OfText(updatableOld), "https://cdn.example/sodium-1.0.0.jar", 1024),
			sha1OfText(latestJar): makeVersion("ver-latest", "DDEEFF", "2.0.0",
				sha1OfText(latestJar), "https://cdn.example/lithium-2.0.0.jar", 2048),
		},
		update: []modrinthVersionJSON{
			// 键是「查询用的旧哈希」，值是它对应的最新版本（Modrinth 的实际语义）
			withResponseHash(makeVersion("ver-new", "AABBCC", "1.1.0", "sha1-of-new-sodium",
				"https://cdn.example/sodium-1.1.0.jar", 4096), sha1OfText(updatableOld)),
			withResponseHash(makeVersion("ver-latest", "DDEEFF", "2.0.0", sha1OfText(latestJar),
				"https://cdn.example/lithium-2.0.0.jar", 2048), sha1OfText(latestJar)),
		},
		projectNames: map[string]string{"AABBCC": "Sodium", "DDEEFF": "Lithium"},
	})

	result, err := download.CheckInstanceContentUpdates(
		context.Background(), root, "TestInstance", "1.21.1", "Fabric", nil)
	if err != nil {
		t.Fatalf("检查更新不应报错：%v", err)
	}

	if result.HashRequestCount != 2 {
		t.Errorf("批量口径：5 个文件（3 个唯一哈希）应发 2 个请求，实际 %d", result.HashRequestCount)
	}
	if got := fake.lookupRequests.Load(); got != 1 {
		t.Errorf("反查请求数 = %d，期望 1（批量，不是一文件一请求）", got)
	}
	if got := fake.updateRequests.Load(); got != 1 {
		t.Errorf("最新版本请求数 = %d，期望 1", got)
	}
	if len(fake.lastLookupHashes) != 3 {
		t.Errorf("单次反查提交的哈希数 = %d，期望 3（已去重）", len(fake.lastLookupHashes))
	}
	if result.UpdatableCount != 2 {
		t.Errorf("可更新数量 = %d，期望 2（sodium.jar 与同内容的 sodium-copy.jar 都要报）", result.UpdatableCount)
	}
	if result.LatestCount != 1 {
		t.Errorf("已是最新数量 = %d，期望 1", result.LatestCount)
	}
	if result.UnknownCount != 1 {
		t.Errorf("未知数量 = %d，期望 1（自建 mod 不能被报成已是最新）", result.UnknownCount)
	}
	if result.SkippedCount != 1 {
		t.Errorf("跳过数量 = %d，期望 1（.disabled）", result.SkippedCount)
	}
	if result.DuplicateFileCount != 1 {
		t.Errorf("重复内容文件数 = %d，期望 1", result.DuplicateFileCount)
	}

	byName := map[string]download.ContentUpdateFile{}
	for _, file := range result.Files {
		byName[file.FileName] = file
	}

	sodium := byName["sodium.jar"]
	if sodium.Status != download.ContentUpdateStatusUpdatable {
		t.Fatalf("sodium.jar 状态 = %q，期望 %q", sodium.Status, download.ContentUpdateStatusUpdatable)
	}
	if sodium.ProjectName != "Sodium" {
		t.Errorf("项目名 = %q，期望 Sodium", sodium.ProjectName)
	}
	if sodium.CurrentVersion != "1.0.0" || sodium.LatestVersion != "1.1.0" {
		t.Errorf("版本号 = %q → %q，期望 1.0.0 → 1.1.0", sodium.CurrentVersion, sodium.LatestVersion)
	}
	if sodium.DownloadURL != "https://cdn.example/sodium-1.1.0.jar" {
		t.Errorf("下载地址 = %q", sodium.DownloadURL)
	}
	if sodium.DownloadSizeBytes != 4096 || sodium.DownloadSizeText != "4 KB" {
		// 口径与 models.ModrinthVersionFile.SizeDisplay 一致：>= 1MB 用 MB(1 位小数)，
		// >= 1KB 用整数 KB，其余用 B。
		t.Errorf("下载大小 = %d / %q，期望 4096 / 4 KB", sodium.DownloadSizeBytes, sodium.DownloadSizeText)
	}
	if sodium.ReleaseDate != "2026-02-03" {
		t.Errorf("发布日期 = %q，期望 2026-02-03", sodium.ReleaseDate)
	}
	if !sodium.CompatibleWithInstance {
		t.Error("1.21.1 + Fabric 的版本应判定为兼容当前实例")
	}

	// 同一份内容复制出来的文件必须拿到同样的结论（不能因为是重复文件就漏判）
	if copyEntry := byName["sodium-copy.jar"]; copyEntry.Status != download.ContentUpdateStatusUpdatable {
		t.Errorf("sodium-copy.jar 状态 = %q，期望与同哈希文件一致", copyEntry.Status)
	}

	lithium := byName["lithium.jar"]
	if lithium.Status != download.ContentUpdateStatusLatest {
		t.Errorf("lithium.jar 状态 = %q，期望 %q", lithium.Status, download.ContentUpdateStatusLatest)
	}
	if lithium.LatestVersion != "" {
		t.Errorf("已是最新的条目不应带最新版本号，实际 %q", lithium.LatestVersion)
	}

	own := byName["my-own-mod.jar"]
	if own.Status != download.ContentUpdateStatusUnknown {
		t.Fatalf("自建 mod 状态 = %q，期望 %q", own.Status, download.ContentUpdateStatusUnknown)
	}
	if own.ProjectName != "" || own.CurrentVersion != "" || own.DownloadURL != "" {
		t.Errorf("未知条目不得编造项目名/版本/下载地址：%+v", own)
	}
	if !strings.Contains(own.StatusText, "无法判断") {
		t.Errorf("未知条目的说明应讲清「无法判断」，实际 %q", own.StatusText)
	}

	disabled := byName["disabled.jar"]
	if disabled.Status != download.ContentUpdateStatusSkipped {
		t.Errorf("禁用态文件状态 = %q，期望 %q", disabled.Status, download.ContentUpdateStatusSkipped)
	}
}

// TestCheckContentUpdatesUnknownWhenNoCompatibleVersion 防的回归：
// 反查命中了、但「最新版本」接口因为实例版本/加载器过滤没有返回任何版本时，
// 把它报成「已是最新」——这会把「你在 1.21.1 上没有可用更新」说成「你已经最新」，
// 而真实原因可能是这个项目根本不支持 1.21.1。
func TestCheckContentUpdatesUnknownWhenNoCompatibleVersion(t *testing.T) {
	content := "fabric-mod-for-other-version"
	root := buildContentDirectory(t, []testFile{{"mods/jei.jar", content}})

	newFakeModrinth(t, &fakeModrinth{
		lookup: map[string]modrinthVersionJSON{
			sha1OfText(content): makeVersion("ver-1", "JEIAAA", "15.0.0",
				sha1OfText(content), "https://cdn.example/jei-15.0.0.jar", 512),
		},
		update:       []modrinthVersionJSON{}, // 该实例版本下没有兼容版本
		projectNames: map[string]string{"JEIAAA": "JEI"},
	})

	result, err := download.CheckInstanceContentUpdates(
		context.Background(), root, "TestInstance", "1.21.1", "Fabric", nil)
	if err != nil {
		t.Fatalf("检查更新不应报错：%v", err)
	}
	if result.LatestCount != 0 {
		t.Errorf("无兼容版本时不得计入「已是最新」，实际 %d", result.LatestCount)
	}
	if result.UnknownCount != 1 {
		t.Fatalf("无兼容版本应计为未知，实际 unknown=%d", result.UnknownCount)
	}
	entry := result.Files[0]
	if entry.Status != download.ContentUpdateStatusUnknown {
		t.Errorf("状态 = %q，期望 %q", entry.Status, download.ContentUpdateStatusUnknown)
	}
	if !strings.Contains(entry.StatusText, "1.21.1") {
		t.Errorf("说明里应带上实例版本，实际 %q", entry.StatusText)
	}
}

// TestCheckContentUpdatesUnknownWhenNoFilter 防的回归：
// 没有实例版本/加载器过滤（原版实例）时，接口没回答哈希就该判「已是最新」——
// 这条与上一条互为对照，防止为了修上一条把「没有过滤」的场景也一律判成未知。
func TestCheckContentUpdatesUnknownWhenNoFilter(t *testing.T) {
	content := "resource-pack-content"
	root := buildContentDirectory(t, []testFile{{"resourcepacks/fancy.zip", content}})

	newFakeModrinth(t, &fakeModrinth{
		lookup: map[string]modrinthVersionJSON{
			sha1OfText(content): makeVersion("ver-1", "PACKAA", "3.0.0",
				sha1OfText(content), "https://cdn.example/fancy-3.0.0.zip", 700),
		},
		update:       []modrinthVersionJSON{},
		projectNames: map[string]string{"PACKAA": "Fancy Pack"},
	})

	// 原版实例：没有 MC 版本与加载器过滤（gameVersion 传空）
	result, err := download.CheckInstanceContentUpdates(
		context.Background(), root, "TestInstance", "", "", nil)
	if err != nil {
		t.Fatalf("检查更新不应报错：%v", err)
	}
	if result.LatestCount != 1 || result.Files[0].Status != download.ContentUpdateStatusLatest {
		t.Fatalf("无过滤时应判为已是最新：latest=%d status=%q",
			result.LatestCount, result.Files[0].Status)
	}
	if fake := result.HashRequestCount; fake != 2 {
		t.Errorf("1 个唯一哈希应发 2 个请求（反查 + 最新），实际 %d", fake)
	}
}

// TestCheckContentUpdatesReportsLookupFailure 防的回归：
// Modrinth 反查整批失败（500/超时）时把文件报成「已是最新」或「未知」，
// 让用户以为"检查过了、没问题"；正确行为是如实报「本次检查失败」。
func TestCheckContentUpdatesReportsLookupFailure(t *testing.T) {
	root := buildContentDirectory(t, []testFile{{"mods/whatever.jar", "some-jar"}})

	newFakeModrinth(t, &fakeModrinth{lookup: map[string]modrinthVersionJSON{}, lookupStatus: http.StatusInternalServerError})
	// 重试退避在生产是秒级；测试里把客户端超时压到毫秒级，让重试立即结束，
	// 避免用例为了等退避白耗几秒（重试次数本身只影响时长，不影响结论）。
	modrinth.SetHTTPClient(&http.Client{Timeout: 50 * time.Millisecond})

	result, err := download.CheckInstanceContentUpdates(
		context.Background(), root, "TestInstance", "1.21.1", "Fabric", nil)
	if err != nil {
		t.Fatalf("网络失败不应让整个检查报错（实例页要能照常渲染）：%v", err)
	}
	if result.FailedCount != 1 {
		t.Fatalf("失败数量 = %d，期望 1", result.FailedCount)
	}
	entry := result.Files[0]
	if entry.Status != download.ContentUpdateStatusCheckFailed {
		t.Errorf("状态 = %q，期望 %q", entry.Status, download.ContentUpdateStatusCheckFailed)
	}
	if result.UnknownCount != 0 || result.LatestCount != 0 || result.UpdatableCount != 0 {
		t.Errorf("失败不能算成未知/最新/可更新：%+v", result)
	}
	if len(result.Notices) == 0 {
		t.Error("失败必须留下可读的中文提示")
	}
}

// TestCheckContentUpdatesLookup404MeansUnknown 防的回归：
// Modrinth 对未收录的哈希批量返回 404 时，被当成「网络失败」报错，
// 而不是「查不到 = 未知」——用户看到一片红色失败，其实文件只是自建的。
func TestCheckContentUpdatesLookup404MeansUnknown(t *testing.T) {
	root := buildContentDirectory(t, []testFile{{"mods/private.jar", "private-jar"}})

	newFakeModrinth(t, &fakeModrinth{lookup: map[string]modrinthVersionJSON{}, lookupStatus: http.StatusNotFound})

	result, err := download.CheckInstanceContentUpdates(
		context.Background(), root, "TestInstance", "1.21.1", "Fabric", nil)
	if err != nil {
		t.Fatalf("404 不应让整个检查报错：%v", err)
	}
	if result.UnknownCount != 1 || result.FailedCount != 0 {
		t.Fatalf("404 应计为未知：unknown=%d failed=%d", result.UnknownCount, result.FailedCount)
	}
}

// TestCheckContentUpdatesRejectsEmptyDirectory 防的回归：
// 空内容目录参数被当成合法输入去扫当前工作目录（或在 UI 上表现为"检查了 0 个"）。
func TestCheckContentUpdatesRejectsEmptyDirectory(t *testing.T) {
	if _, err := download.CheckInstanceContentUpdates(
		context.Background(), "  ", "TestInstance", "", "", nil); err == nil {
		t.Error("空内容目录必须报错")
	}
}

// TestCheckContentUpdatesHonoursCancel 防的回归：
// 用户离开实例页/取消检查后 goroutine 还在一路算哈希、发请求。
func TestCheckContentUpdatesHonoursCancel(t *testing.T) {
	root := buildContentDirectory(t, []testFile{{"mods/a.jar", "aaa"}, {"mods/b.jar", "bbb"}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := download.CheckInstanceContentUpdates(ctx, root, "TestInstance", "", "", nil); err == nil {
		t.Error("已取消的上下文必须让检查直接返回错误")
	}
}

// TestCheckContentUpdatesReportsProgressPhases 防的回归：
// 进度回调丢失阶段（UI 上按钮一直转圈却看不到在干什么）。
func TestCheckContentUpdatesReportsProgressPhases(t *testing.T) {
	content := "progress-mod"
	root := buildContentDirectory(t, []testFile{{"mods/x.jar", content}})

	newFakeModrinth(t, &fakeModrinth{
		lookup: map[string]modrinthVersionJSON{
			sha1OfText(content): makeVersion("v1", "PRGAAA", "1.0.0",
				sha1OfText(content), "https://cdn.example/x-1.0.0.jar", 100),
		},
		update:       []modrinthVersionJSON{},
		projectNames: map[string]string{"PRGAAA": "Progress Mod"},
	})

	var phases []string
	_, err := download.CheckInstanceContentUpdates(context.Background(), root, "TestInstance", "", "", func(phase, message string) {
		if strings.TrimSpace(message) == "" {
			t.Errorf("阶段 %s 的提示为空", phase)
		}
		phases = append(phases, phase)
	})
	if err != nil {
		t.Fatalf("检查更新不应报错：%v", err)
	}
	for _, expected := range []string{"hashing", "lookup", "latest", "modpack", "done"} {
		found := false
		for _, phase := range phases {
			if phase == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("缺少进度阶段 %q，实际 %v", expected, phases)
		}
	}
}

// TestCheckContentUpdatesBatchesHashes 防的回归：
// 批量口径被改回「一个文件一个请求」——20 个文件会打出 20+ 个请求，
// 触发 Modrinth 限流并把实例页卡住。
func TestCheckContentUpdatesBatchesHashes(t *testing.T) {
	files := make([]testFile, 0, 20)
	lookup := map[string]modrinthVersionJSON{}
	for index := 0; index < 20; index++ {
		content := "mod-" + string(rune('a'+index))
		files = append(files, testFile{"mods/" + content + ".jar", content})
		lookup[sha1OfText(content)] = makeVersion(
			"v"+content, "PROJ"+content, "1.0.0",
			sha1OfText(content), "https://cdn.example/"+content+".jar", 10)
	}
	root := buildContentDirectory(t, files)

	fake := newFakeModrinth(t, &fakeModrinth{lookup: lookup, update: []modrinthVersionJSON{}})

	result, err := download.CheckInstanceContentUpdates(
		context.Background(), root, "TestInstance", "1.21.1", "Fabric", nil)
	if err != nil {
		t.Fatalf("检查更新不应报错：%v", err)
	}
	if got := fake.lookupRequests.Load(); got != 1 {
		t.Errorf("20 个哈希（< 100）应只需 1 个反查请求，实际 %d", got)
	}
	if result.HashRequestCount != 2 {
		t.Errorf("请求数口径 = %d，期望 2", result.HashRequestCount)
	}
	if result.HashBatchSize != 100 {
		t.Errorf("批量上限 = %d，期望 100", result.HashBatchSize)
	}
}

// ---------------------------------------------------------------------------
// 用例：整合包清单比对
// ---------------------------------------------------------------------------

// mrpackIndexJSON 造一个 mrpack 清单（files 声明 + overrides 里的内容不声明）。
func mrpackIndexJSON(name, version string, entries []map[string]any) string {
	payload := map[string]any{
		"formatVersion": 1,
		"game":          "minecraft",
		"versionId":     version,
		"name":          name,
		"files":         entries,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func indexEntry(path, sha1Hex string) map[string]any {
	return indexEntryWithURL(path, sha1Hex, "https://cdn.example/"+filepath.Base(path))
}

// indexEntryWithURL 同 indexEntry，但自定义下载地址（用例里可指向本地假服务器）。
func indexEntryWithURL(path, sha1Hex, downloadURL string) map[string]any {
	return map[string]any{
		"path":      path,
		"hashes":    map[string]string{"sha1": sha1Hex, "sha512": "ignored"},
		"downloads": []string{downloadURL},
		"fileSize":  1234,
	}
}

// TestVerifyInstanceModpackCountsMissingModifiedCurrent 防的回归：
// 清单比对把「被改动的文件」漏掉（只看文件是否存在），或把缺文件/改文件的数量算错。
func TestVerifyInstanceModpackCountsMissingModifiedCurrent(t *testing.T) {
	root := buildContentDirectory(t, []testFile{
		{"mods/good.jar", "good-content"},
		{"mods/tampered.jar", "modified-content"},
		{"config/settings.toml", "keep=1"},
	})
	index := mrpackIndexJSON("示例整合包", "1.2.3", []map[string]any{
		indexEntry("mods/good.jar", sha1OfText("good-content")),
		indexEntry("mods/tampered.jar", sha1OfText("original-content")),
		indexEntry("mods/deleted.jar", sha1OfText("deleted")),
		indexEntry("config/settings.toml", sha1OfText("keep=1")),
	})
	if err := os.WriteFile(filepath.Join(root, "modrinth.index.json"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}

	result := download.VerifyInstanceModpack(root, nil)
	if !result.Present || result.Format != "modrinth" {
		t.Fatalf("应识别为 Modrinth 清单：%+v", result)
	}
	if result.Status != download.ModpackVerifyIssues {
		t.Errorf("状态 = %q，期望 %q", result.Status, download.ModpackVerifyIssues)
	}
	if result.TotalFiles != 4 {
		t.Errorf("清单文件总数 = %d，期望 4", result.TotalFiles)
	}
	if result.CurrentCount != 2 {
		t.Errorf("一致数量 = %d，期望 2", result.CurrentCount)
	}
	if result.MissingCount != 1 {
		t.Errorf("缺失数量 = %d，期望 1", result.MissingCount)
	}
	if result.ModifiedCount != 1 {
		t.Errorf("被改动数量 = %d，期望 1", result.ModifiedCount)
	}
	if result.PackName != "示例整合包" || result.PackVersion != "1.2.3" {
		t.Errorf("整合包名/版本 = %q / %q", result.PackName, result.PackVersion)
	}
	// 明细里缺失与改动必须排在前面（UI 直接按顺序展示）
	if len(result.Files) != 4 {
		t.Fatalf("明细数量 = %d，期望 4", len(result.Files))
	}
	if result.Files[0].Status != download.ModpackFileMissing {
		t.Errorf("明细第一条应为缺失，实际 %q", result.Files[0].Status)
	}
	if result.Files[1].Status != download.ModpackFileModified {
		t.Errorf("明细第二条应为被改动，实际 %q", result.Files[1].Status)
	}
}

// TestVerifyInstanceModpackOverridesPath 防的回归：
// 清单路径与磁盘路径大小写不一致（或保留了 overrides/ 前缀）时判成"缺失"，
// 把一整包完好文件报成全部丢失。
func TestVerifyInstanceModpackOverridesPath(t *testing.T) {
	root := buildContentDirectory(t, []testFile{
		{"mods/CaseSensitive.jar", "case-content"},
	})
	index := mrpackIndexJSON("大小写整合包", "1.0.0", []map[string]any{
		indexEntry("mods/casesensitive.jar", sha1OfText("case-content")),
	})
	if err := os.WriteFile(filepath.Join(root, "modrinth.index.json"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}

	result := download.VerifyInstanceModpack(root, nil)
	if result.MissingCount != 0 {
		t.Errorf("大小写不一致不应判为缺失：%+v", result)
	}
	if result.CurrentCount != 1 {
		t.Errorf("一致数量 = %d，期望 1", result.CurrentCount)
	}
}

// TestVerifyInstanceModpackCurseForgeIsHonest 防的回归：
// 对 CurseForge 的 manifest.json 假装能校验（报"全部一致"），
// 而它只有 projectID/fileID、没有哈希，离线根本没法验。
func TestVerifyInstanceModpackCurseForgeIsHonest(t *testing.T) {
	root := buildContentDirectory(t, []testFile{{"mods/some.jar", "whatever"}})
	manifest := `{"minecraft":{"version":"1.20.1","modLoaders":[{"id":"forge-47.2.0","primary":true}]},` +
		`"manifestType":"minecraftModpack","manifestVersion":1,"name":"CF 整合包","version":"2.0",` +
		`"files":[{"projectID":238222,"fileID":123456,"required":true}]}`
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	result := download.VerifyInstanceModpack(root, nil)
	if result.Status != download.ModpackVerifyUnsupported {
		t.Fatalf("状态 = %q，期望 %q", result.Status, download.ModpackVerifyUnsupported)
	}
	if result.Format != "curseforge" {
		t.Errorf("格式 = %q，期望 curseforge", result.Format)
	}
	if !strings.Contains(result.StatusText, "API key") {
		t.Errorf("提示里必须说明需要 CurseForge API key，实际 %q", result.StatusText)
	}
	if result.CurrentCount != 0 || result.MissingCount != 0 {
		t.Errorf("CurseForge 清单不得给出任何比对结论：%+v", result)
	}
}

// TestVerifyInstanceModpackNoIndexIsHonest 防的回归：
// 没有清单时假装"没发现问题"，而不是如实说无法比对。
func TestVerifyInstanceModpackNoIndexIsHonest(t *testing.T) {
	root := buildContentDirectory(t, []testFile{{"mods/a.jar", "aaa"}})

	result := download.VerifyInstanceModpack(root, nil)
	if result.Present {
		t.Error("没有清单时 Present 必须为 false")
	}
	if result.Status != download.ModpackVerifyNoIndex {
		t.Errorf("状态 = %q，期望 %q", result.Status, download.ModpackVerifyNoIndex)
	}
	if !strings.Contains(result.StatusText, "无法比对") {
		t.Errorf("提示应说明无法比对，实际 %q", result.StatusText)
	}
}

// TestModpackSnapshotRoundTripThroughInstall 防的回归：
// 安装时不再保存清单快照 → 由于安装器按规范会把清单从实例目录剔除，
// 「整合包有没有被改动」这个问题在安装完成后永远只能回答"没有清单"。
func TestModpackSnapshotRoundTripThroughInstall(t *testing.T) {
	storage := t.TempDir()
	original := config.StorageDirectory()
	if err := config.SetStorageDirectory(storage); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.SetStorageDirectory(original) })

	content := "snapshot-mod"
	// 清单声明的文件同时提供一份可下载的本地副本：验证快照与安装流程共存，
	// 且安装器不需要（也绝不会）去真实 CDN 下载——声明文件在包内已存在时会被跳过，
	// 这里给下载地址只是为了在"文件缺失"的路径上也有确定行为。
	snapshotServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(content))
	}))
	t.Cleanup(snapshotServer.Close)

	mrpackPath := buildMinimalMrpack(t, "快照整合包", "9.9.9", []map[string]any{
		indexEntryWithURL("mods/snapshot.jar", sha1OfText(content), snapshotServer.URL+"/snapshot.jar"),
	})
	target := buildContentDirectory(t, []testFile{{"mods/snapshot.jar", content}})

	result, err := download.InstallModpack(context.Background(), mrpackPath, target, nil)
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	for _, message := range result.Errors {
		if strings.Contains(message, "清单快照") {
			t.Fatalf("清单快照保存失败：%s", message)
		}
	}

	// 清单本身不应该落进实例目录（安装器的既有语义）
	if _, err := os.Stat(filepath.Join(target, "modrinth.index.json")); err == nil {
		t.Error("整合包清单不应出现在实例目录里")
	}

	verified := download.VerifyInstanceModpack(target, nil)
	if !verified.Present {
		t.Fatalf("安装后应能读到清单快照：%+v", verified)
	}
	if verified.Format != "modrinth" {
		t.Errorf("快照格式 = %q，期望 modrinth", verified.Format)
	}
	if verified.PackName != "快照整合包" {
		t.Errorf("快照里的整合包名 = %q", verified.PackName)
	}
	if verified.CurrentCount != 1 || verified.MissingCount != 0 {
		t.Errorf("快照比对结果不符：%+v", verified)
	}
	if !strings.Contains(verified.Source, "快照") {
		t.Errorf("来源说明应指出读的是快照，实际 %q", verified.Source)
	}
}
