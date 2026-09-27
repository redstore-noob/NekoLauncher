package modrinth_test

// 批量哈希查询的客户端用例（离线，全部指向 httptest 假服务器）。
//
// 防的回归：
//  1. 分批上限失效——几百个哈希塞进一个请求（请求体过大、失败重试代价高）；
//  2. 空响应被当成解析失败（会把"未收录"报成"检查失败"）；
//  3. 429 不重试（Modrinth 限流时整批检查直接失败）；
//  4. 响应是「按 project_id 分组的对象」形状时解析不出来（真实端点会这么答）。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nekolauncher/internal/download/modrinth"
)

// startFakeAPI 起一个假 Modrinth 并把客户端指过去（镜像一并指过来，保证完全离线）。
func startFakeAPI(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	modrinth.SetEndpoints(server.URL, server.URL)
	t.Cleanup(modrinth.ResetEndpoints)
	return server
}

// TestVersionFilesLookupBatchesAtHundred 防的回归：分批上限被去掉或改大，
// 500 个哈希一次全发（服务端拒绝、重试代价高），或分批被写成一哈希一请求。
func TestVersionFilesLookupBatchesAtHundred(t *testing.T) {
	var batches [][]string
	var mu sync.Mutex

	startFakeAPI(t, func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var payload struct {
			Hashes    []string `json:"hashes"`
			Algorithm string   `json:"algorithm"`
		}
		_ = json.Unmarshal(body, &payload)
		if payload.Algorithm != "sha1" {
			t.Errorf("哈希算法应为 sha1，实际 %q", payload.Algorithm)
		}
		mu.Lock()
		batches = append(batches, payload.Hashes)
		mu.Unlock()
		_, _ = writer.Write([]byte("{}"))
	})

	hashes := make([]string, 250)
	for index := range hashes {
		hashes[index] = strings.Repeat("a", 39) + string(rune('0'+index%10))
		// 保证唯一：用下标拼进哈希文本
		hashes[index] = strings.Repeat("0", 40-len(itoa(index))) + itoa(index)
	}

	if _, err := modrinth.GetVersionFilesByHashes(context.Background(), hashes); err != nil {
		t.Fatalf("批量查询失败：%v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(batches) != 3 {
		t.Fatalf("250 个哈希应按 100 分批成 3 个请求，实际 %d 个", len(batches))
	}
	if len(batches[0]) != 100 || len(batches[1]) != 100 || len(batches[2]) != 50 {
		t.Errorf("分批大小 = %d/%d/%d，期望 100/100/50",
			len(batches[0]), len(batches[1]), len(batches[2]))
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// TestVersionFilesLookupEmptyResponseIsNotAnError 防的回归：
// 空响应体（未收录任何哈希时服务端的正常应答）被当成解析失败，
// UI 上表现为「检查失败」而不是「未知」。
func TestVersionFilesLookupEmptyResponseIsNotAnError(t *testing.T) {
	startFakeAPI(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("{}"))
	})

	matches, err := modrinth.GetVersionFilesByHashes(context.Background(), []string{"deadbeef"})
	if err != nil {
		t.Fatalf("空响应不应报错：%v", err)
	}
	if len(matches) != 0 {
		t.Errorf("未收录时应返回空结果，实际 %d 条", len(matches))
	}
}

// TestVersionFilesLookupRetriesOnRateLimit 防的回归：
// 429 直接放弃（用户点一次检查更新就因为限流整批失败），
// 以及重试时不尊重 Retry-After。
func TestVersionFilesLookupRetriesOnRateLimit(t *testing.T) {
	var attempts atomic.Int64

	startFakeAPI(t, func(writer http.ResponseWriter, request *http.Request) {
		if attempts.Add(1) == 1 {
			writer.Header().Set("Retry-After", "0")
			writer.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"abc":{"id":"v1","project_id":"P","version_number":"1.0.0","files":[]}}`))
	})

	matches, err := modrinth.GetVersionFilesByHashes(context.Background(), []string{"abc"})
	if err != nil {
		t.Fatalf("429 后重试应成功：%v", err)
	}
	if attempts.Load() < 2 {
		t.Errorf("应至少重试一次，实际请求 %d 次", attempts.Load())
	}
	if match, ok := matches["abc"]; !ok || match.VersionID != "v1" {
		t.Errorf("重试成功后应拿到结果，实际 %+v", matches)
	}
}

// TestVersionFilesLookupForbiddenIsNotRetried 防的回归：
// 403/400 这类确定性失败被当成"抖动"反复重试，用户白等十几秒才看到错误。
func TestVersionFilesLookupForbiddenIsNotRetried(t *testing.T) {
	var attempts atomic.Int64

	startFakeAPI(t, func(writer http.ResponseWriter, request *http.Request) {
		attempts.Add(1)
		writer.WriteHeader(http.StatusForbidden)
	})

	if _, err := modrinth.GetVersionFilesByHashes(context.Background(), []string{"abc"}); err == nil {
		t.Fatal("403 必须报错（不能静默当成查不到）")
	}
	if attempts.Load() != 1 {
		t.Errorf("403 不应重试，实际请求 %d 次", attempts.Load())
	}
}

// TestLatestVersionsAcceptsProjectGroupedResponse 防的回归：
// 真实端点返回「按 project_id 分组的对象」时解析不出来，
// 所有文件都被判成「未知/已是最新」——这是最容易蒙混过关的一类错。
func TestLatestVersionsAcceptsProjectGroupedResponse(t *testing.T) {
	startFakeAPI(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		// 形状：{ "<project_id>": [version, ...] }
		_, _ = writer.Write([]byte(`{
			"P7dR8mSH": [{
				"id": "newver",
				"project_id": "P7dR8mSH",
				"version_number": "1.2.3",
				"game_versions": ["1.21.1"],
				"loaders": ["fabric"],
				"date_published": "2026-01-02T03:04:05Z",
				"files": [{
					"url": "https://cdn.example/fabric-api-1.2.3.jar",
					"filename": "fabric-api-1.2.3.jar",
					"size": 555,
					"hashes": {"sha1": "hash-of-new"},
					"primary": true
				}]
			}]
		}`))
	})

	updates, err := modrinth.GetLatestVersionFilesByHashes(context.Background(),
		[]string{"hash-of-new"}, []string{"1.21.1"}, []string{"fabric"})
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	update, ok := updates["hash-of-new"]
	if !ok {
		t.Fatalf("分组形状的响应必须能解析出结果，实际 %+v", updates)
	}
	if update.VersionNumber != "1.2.3" || update.File.Filename != "fabric-api-1.2.3.jar" {
		t.Errorf("解析结果不符：%+v", update)
	}
	if update.File.Size != 555 {
		t.Errorf("文件大小 = %d，期望 555", update.File.Size)
	}
}

// TestLatestVersionsAcceptsHashKeyedResponse 防的回归：
// 端点以「请求里的哈希为键」回答（键是旧哈希、版本对象带的是新文件哈希）时，
// 结果全部对不上号 → 明明有更新却一个都报不出来。
func TestLatestVersionsAcceptsHashKeyedResponse(t *testing.T) {
	startFakeAPI(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"oldhash": {
				"id": "newver",
				"project_id": "P",
				"version_number": "2.0.0",
				"game_versions": ["1.21.1"],
				"loaders": ["fabric"],
				"files": [{
					"url": "https://cdn.example/mod-2.0.0.jar",
					"filename": "mod-2.0.0.jar",
					"size": 100,
					"hashes": {"sha1": "brand-new-hash"},
					"primary": true
				}]
			}
		}`))
	})

	updates, err := modrinth.GetLatestVersionFilesByHashes(context.Background(), []string{"oldhash"}, nil, nil)
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	if _, ok := updates["oldhash"]; !ok {
		t.Fatalf("以哈希为键的响应必须能解析出结果，实际 %+v", updates)
	}
}

// TestLatestVersionsOmitFiltersWhenEmpty 防的回归：
// 没有实例版本/加载器时仍然发送空数组过滤（game_versions: [] 在 Modrinth 语义里
// 等于「什么都不匹配」），结果一律"查不到更新"。
func TestLatestVersionsOmitFiltersWhenEmpty(t *testing.T) {
	var body map[string]any
	startFakeAPI(t, func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(raw, &body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("[]"))
	})

	if _, err := modrinth.GetLatestVersionFilesByHashes(context.Background(), []string{"h"}, nil, nil); err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	if _, exists := body["game_versions"]; exists {
		t.Error("空 gameVersions 不应作为过滤参数发送")
	}
	if _, exists := body["loaders"]; exists {
		t.Error("空 loaders 不应作为过滤参数发送")
	}
	if algorithm, _ := body["algorithm"].(string); algorithm != "sha1" {
		t.Errorf("algorithm = %q，期望 sha1", algorithm)
	}
}

// TestGetProjectNameRejectsEmptyID 防的回归：空 projectID 拼出 /project/ 打到
// 服务端（多一次无意义请求，还可能命中一个不相关的端点）。
func TestGetProjectNameRejectsEmptyID(t *testing.T) {
	startFakeAPI(t, func(writer http.ResponseWriter, request *http.Request) {
		t.Errorf("空 projectID 不应发起请求，实际请求 %s", request.URL.Path)
	})
	if _, err := modrinth.GetProjectName(context.Background(), "   "); err == nil {
		t.Error("空 projectID 必须报错")
	}
}

// TestVersionFileLookupTimeoutIsReadable 防的回归：
// 连接超时把 Go 的英文错误直接抛给界面，用户不知道发生了什么。
func TestVersionFileLookupTimeoutIsReadable(t *testing.T) {
	startFakeAPI(t, func(writer http.ResponseWriter, request *http.Request) {
		time.Sleep(200 * time.Millisecond)
	})
	// 单次请求预算被 requestTimeout 收紧到 1 秒以内时，客户端超时应当被翻译成中文
	modrinth.SetHTTPClient(&http.Client{Timeout: 20 * time.Millisecond})
	t.Cleanup(func() { modrinth.ResetEndpoints() })

	_, err := modrinth.GetVersionFilesByHashes(context.Background(), []string{"abc"})
	if err == nil {
		t.Fatal("超时必须报错")
	}
	if !strings.Contains(err.Error(), "Modrinth") {
		t.Errorf("错误信息应说明是 Modrinth 访问失败，实际 %v", err)
	}
}
