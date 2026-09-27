// version_files_api.go Modrinth 的「按哈希反查版本」批量接口。
//
// 与 version_api.go（按 projectID 查版本列表）互补：更新检测手里只有磁盘上的
// 文件，没有 projectID，必须先用 SHA-1 反查出「这个文件属于哪个项目的哪个版本」，
// 再问 Modrinth「这些哈希现在的最新版本是哪个」。
//
// 两个端点都支持一次提交多个哈希（本项目按 versionFileLookupBatchSize 分批），
// 一次检查 N 个 Mod 只需要 2 * ceil(N/100) 个请求，而不是每个文件 2 个。
//
// 请求通道复用 client.go 的 apiRoot / mirrorRoot 回退与 statusError 判定；
// 测试用 SetEndpoints(httptest地址, "") 指向假服务器即可完全离线。
package modrinth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"nekolauncher/internal/logs"
)

// versionFileLookupBatchSize 单次请求提交的哈希数上限。
// Modrinth 对 version_files 的请求体没有公开的硬上限，但一次塞几百个哈希
// 会让请求体过大、失败后重试代价高；100 与常见的批量查询实现一致。
const versionFileLookupBatchSize = 100

// VersionFileLookupBatchSize 批量查询的单批哈希数上限（对外只读，供上层
// 预估请求数与向用户说明口径）。
const VersionFileLookupBatchSize = versionFileLookupBatchSize

// VersionFileMatch 一个文件哈希在 Modrinth 上的归属（项目 + 版本 + 文件本身）。
type VersionFileMatch struct {
	// ProjectID 所属项目 ID。
	ProjectID string
	// VersionID 哈希命中的版本 ID。
	VersionID string
	// VersionNumber 哈希命中的版本号（如 "1.2.3"）。
	VersionNumber string
	// GameVersions 该版本支持的 Minecraft 版本。
	GameVersions []string
	// Loaders 该版本支持的加载器（材质包/光影包通常为空）。
	Loaders []string
	// DatePublished 发布日期（RFC3339 原文）。
	DatePublished string
	// File 与哈希对应的那个文件（URL / 文件名 / 大小）。
	File VersionFileInfo
}

// VersionFileInfo 一个可下载文件的基本信息。
type VersionFileInfo struct {
	URL      string
	Filename string
	Size     int64
}

// VersionFileUpdate 批量「取最新版本」接口对一个哈希的回答。
type VersionFileUpdate struct {
	// Hash 本次查询所用的 SHA-1。
	Hash string
	// VersionID 当前最新（对查询条件兼容的）版本 ID。
	VersionID string
	// ProjectID 所属项目 ID。
	ProjectID string
	// VersionNumber 最新版本号。
	VersionNumber string
	// GameVersions 最新版本支持的 Minecraft 版本。
	GameVersions []string
	// Loaders 最新版本支持的加载器。
	Loaders []string
	// DatePublished 最新版本发布日期。
	DatePublished string
	// File 最新版本中与最新哈希对应的文件。
	File VersionFileInfo
}

// GetVersionFilesByHashes 批量按 SHA-1 反查文件归属。
// 返回的 map 只包含「库里查得到」的哈希；查不到的哈希不会出现在结果里
// （调用方必须把缺席当作"未知"，而不是"没有更新"）。
// 404 表示这批哈希一个都没收录，按"全部查不到"处理（返回空 map、不报错）；
// 其余网络/服务端错误原样上抛。
func GetVersionFilesByHashes(ctx context.Context, hashes []string) (map[string]VersionFileMatch, error) {
	result := make(map[string]VersionFileMatch)
	unique := uniqueHashes(hashes)
	for start := 0; start < len(unique); start += versionFileLookupBatchSize {
		batch := unique[start:minInt(start+versionFileLookupBatchSize, len(unique))]
		payload, err := postJSONWithFallback(ctx, "/version_files", map[string]any{
			"hashes":    batch,
			"algorithm": "sha1",
		})
		if err != nil {
			var status *statusError
			if errorsAs(err, &status) && status.StatusCode == http.StatusNotFound {
				logs.Write("INFO", "Modrinth 未收录本次检查的文件哈希（version_files 返回 404）")
				continue
			}
			return nil, err
		}

		var decoded map[string]versionFileMatchPayload
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return nil, fmt.Errorf("解析 Modrinth 文件哈希查询结果失败：%w", err)
		}
		for hash, entry := range decoded {
			result[strings.ToLower(hash)] = entry.toMatch(hash)
		}
	}
	return result, nil
}

// GetLatestVersionFilesByHashes 批量查询这些哈希对应的最新版本。
// 返回的 map 只包含「确实存在可用更新版本」的哈希：
//   - 版本已是该哈希的最新版本 → 不在结果里（调用方按"已是最新"处理）；
//   - 查询条件（gameVersions / loaders）下没有任何兼容版本 → 同样不在结果里。
//
// 因此调用方必须把「反查命中但本接口缺席」当成"未知/无兼容版本"，
// 不能直接报"没有更新"（见 download.checkInstanceContentUpdates 的分支）。
func GetLatestVersionFilesByHashes(
	ctx context.Context,
	hashes, gameVersions, loaders []string,
) (map[string]VersionFileUpdate, error) {
	result := make(map[string]VersionFileUpdate)
	unique := uniqueHashes(hashes)
	for start := 0; start < len(unique); start += versionFileLookupBatchSize {
		batch := unique[start:minInt(start+versionFileLookupBatchSize, len(unique))]
		body := map[string]any{
			"hashes":    batch,
			"algorithm": "sha1",
		}
		// 空数组与"不传"语义不同：不传 = 不过滤；传空数组 = 什么都不匹配。
		// 只有确实要过滤时才带上这两个键。
		if len(gameVersions) > 0 {
			body["game_versions"] = dedupeStrings(gameVersions)
		}
		if len(loaders) > 0 {
			body["loaders"] = dedupeStrings(loaders)
		}

		payload, err := postJSONWithFallback(ctx, "/version_files/update", body)
		if err != nil {
			var status *statusError
			if errorsAs(err, &status) && status.StatusCode == http.StatusNotFound {
				continue
			}
			return nil, err
		}
		updates, err := decodeUpdatePayload(payload)
		if err != nil {
			return nil, err
		}
		for hash, update := range updates {
			result[strings.ToLower(hash)] = update
		}
	}
	return result, nil
}

// GetProjectName 查询项目名（`/project/{id}` 的 title，回退 slug）。
// version_files 的响应只给 project_id，项目名要单独查；同一个项目在调用方
// 只应查一次（见 download.fillProjectNames）。
func GetProjectName(ctx context.Context, projectID string) (string, error) {
	trimmed := strings.TrimSpace(projectID)
	if trimmed == "" {
		return "", fmt.Errorf("projectID 不能为空")
	}
	var project struct {
		Title string `json:"title"`
		Slug  string `json:"slug"`
	}
	if err := getJSON(ctx, "/project/"+trimmed, &project); err != nil {
		return "", err
	}
	if strings.TrimSpace(project.Title) != "" {
		return project.Title, nil
	}
	return project.Slug, nil
}

// ---- 响应解析 ----

type versionFileMatchPayload struct {
	ID            string                   `json:"id"`
	ProjectID     string                   `json:"project_id"`
	VersionNumber string                   `json:"version_number"`
	GameVersions  []string                 `json:"game_versions"`
	Loaders       []string                 `json:"loaders"`
	DatePublished string                   `json:"date_published"`
	Files         []versionFileInfoPayload `json:"files"`
}

func (p versionFileMatchPayload) toMatch(hash string) VersionFileMatch {
	return VersionFileMatch{
		ProjectID:     p.ProjectID,
		VersionID:     p.ID,
		VersionNumber: p.VersionNumber,
		GameVersions:  p.GameVersions,
		Loaders:       p.Loaders,
		DatePublished: p.DatePublished,
		File:          pickFile(p.Files, hash),
	}
}

type versionFileInfoPayload struct {
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Size     int64             `json:"size"`
	Hashes   map[string]string `json:"hashes"`
	Primary  bool              `json:"primary"`
}

// pickFile 在与哈希对应的版本里挑出这个哈希所指的文件：
// 优先哈希精确匹配（一个版本可能挂了多个文件），匹配不到再退 primary / 第一个。
func pickFile(files []versionFileInfoPayload, hash string) VersionFileInfo {
	wanted := strings.ToLower(hash)
	for _, file := range files {
		if strings.EqualFold(file.Hashes["sha1"], wanted) {
			return VersionFileInfo{URL: file.URL, Filename: file.Filename, Size: file.Size}
		}
	}
	for _, file := range files {
		if file.Primary {
			return VersionFileInfo{URL: file.URL, Filename: file.Filename, Size: file.Size}
		}
	}
	if len(files) > 0 {
		return VersionFileInfo{
			URL:      files[0].URL,
			Filename: files[0].Filename,
			Size:     files[0].Size,
		}
	}
	return VersionFileInfo{}
}

// decodeUpdatePayload 解析 version_files/update 的响应。
// 该端点根据是否带 game_versions/loaders 会返回两种形状：
//   - 扁平数组 []version；
//   - 以 project_id 为键的对象 { "<project_id>": [version, ...] }。
//
// 两种都要认（只认一种会让另一条调用路径变成静默的"查不到"）。
func decodeUpdatePayload(payload []byte) (map[string]VersionFileUpdate, error) {
	result := map[string]VersionFileUpdate{}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return result, nil
	}

	switch trimmed[0] {
	case '{':
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return nil, fmt.Errorf("解析 Modrinth 最新版本查询结果失败：%w", err)
		}
		for key, value := range raw {
			// 键是 project_id → 值是版本数组
			if versions, ok := decodeVersionArray(value); ok {
				collectUpdates(result, versions)
				continue
			}
			// 键是哈希 → 值是单个版本对象（用键登记，不依赖版本对象里的哈希字段）
			var version versionFileMatchPayload
			if err := json.Unmarshal(value, &version); err != nil {
				return nil, fmt.Errorf("解析 Modrinth 最新版本查询结果失败（键 %s）：%w", key, err)
			}
			collectUpdateForHash(result, strings.ToLower(key), version)
		}
	case '[':
		var versions []versionFileMatchPayload
		if err := json.Unmarshal(trimmed, &versions); err != nil {
			return nil, fmt.Errorf("解析 Modrinth 最新版本查询结果失败：%w", err)
		}
		collectUpdates(result, versions)
	default:
		return nil, fmt.Errorf("Modrinth 最新版本查询返回了非预期的内容：%s", snippet(trimmed))
	}
	return result, nil
}

// decodeVersionArray 值是否为版本数组（project_id 分组形状）。
func decodeVersionArray(value json.RawMessage) ([]versionFileMatchPayload, bool) {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, false
	}
	var versions []versionFileMatchPayload
	if err := json.Unmarshal(trimmed, &versions); err != nil {
		return nil, false
	}
	return versions, true
}

// collectUpdates 把一批版本按其文件哈希登记为「该哈希的最新版本」。
func collectUpdates(target map[string]VersionFileUpdate, versions []versionFileMatchPayload) {
	for _, version := range versions {
		for _, file := range version.Files {
			hash := strings.ToLower(strings.TrimSpace(file.Hashes["sha1"]))
			if hash == "" {
				// 扁平数组形状下没有哈希就无从对应，上层按「未知」处理；
				// 键为哈希的形状里键本身就是答案，走 collectUpdateForHash。
				continue
			}
			target[hash] = updateFrom(version, file)
		}
	}
}

// collectUpdateForHash 用给定的哈希登记一个版本（键为哈希的响应形状）。
func collectUpdateForHash(target map[string]VersionFileUpdate, hash string, version versionFileMatchPayload) {
	if strings.TrimSpace(hash) == "" {
		return
	}
	update := updateFrom(version, pickFilePayload(version.Files, hash))
	update.Hash = hash
	target[hash] = update
}

// updateFrom 把版本 + 文件组装成查询结果。
func updateFrom(version versionFileMatchPayload, file versionFileInfoPayload) VersionFileUpdate {
	return VersionFileUpdate{
		Hash:          strings.ToLower(strings.TrimSpace(file.Hashes["sha1"])),
		VersionID:     version.ID,
		ProjectID:     version.ProjectID,
		VersionNumber: version.VersionNumber,
		GameVersions:  version.GameVersions,
		Loaders:       version.Loaders,
		DatePublished: version.DatePublished,
		File: VersionFileInfo{
			URL:      file.URL,
			Filename: file.Filename,
			Size:     file.Size,
		},
	}
}

// pickFilePayload 优先哈希精确匹配，其次 primary，最后第一个（与 pickFile 同口径）。
func pickFilePayload(files []versionFileInfoPayload, hash string) versionFileInfoPayload {
	for _, file := range files {
		if strings.EqualFold(file.Hashes["sha1"], hash) {
			return file
		}
	}
	for _, file := range files {
		if file.Primary {
			return file
		}
	}
	if len(files) > 0 {
		return files[0]
	}
	return versionFileInfoPayload{}
}

// ---- HTTP 通道 ----

// postJSONWithFallback 与 client.go 的 getJSON 同策略的 JSON POST：
// 首选地址 → 备用地址 → 首选地址再试一次；只有「值得重试」的失败才继续下一步。
func postJSONWithFallback(ctx context.Context, path string, body any) ([]byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	primary := apiRoot + path
	mirror := ""
	if strings.TrimSpace(mirrorRoot) != "" {
		if candidate := mirrorRoot + path; !strings.EqualFold(candidate, primary) {
			mirror = candidate
		}
	}
	first, second := primary, mirror
	if mirror != "" && preferMirror.Load() {
		first, second = mirror, primary
	}

	payload, firstErr := postJSONOnce(ctx, first, encoded)
	if firstErr == nil {
		preferMirror.Store(first == mirror)
		return payload, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var secondErr error
	if second != "" && isRetryable(firstErr) {
		var mirrored []byte
		mirrored, secondErr = postJSONOnce(ctx, second, encoded)
		if secondErr == nil {
			preferMirror.Store(second == mirror)
			logs.Write("INFO", "Modrinth 接口主地址失败，已回退备用地址："+path)
			return mirrored, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	if isRetryable(firstErr) {
		if retried, retryErr := postJSONOnce(ctx, first, encoded); retryErr == nil {
			preferMirror.Store(first == mirror)
			return retried, nil
		}
	}
	return nil, describeFailure(firstErr, secondErr, mirror != "")
}

// postJSONOnce 单次 POST；只负责发请求、读响应体与保留状态码语义。
func postJSONOnce(ctx context.Context, endpoint string, body []byte) ([]byte, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &statusError{StatusCode: response.StatusCode}
	}
	return io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
}

// errorsAs 取出错误链里的状态错误（没有则返回 nil）。
func errorsAs(err error, target **statusError) bool {
	return errors.As(err, target)
}

// ---- 小工具 ----

// uniqueHashes 去重、去空、统一小写。
func uniqueHashes(hashes []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		normalized := strings.ToLower(strings.TrimSpace(hash))
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	return result
}

// dedupeStrings 去重（保持原顺序），用于 game_versions / loaders 过滤参数。
func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	return result
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func snippet(payload []byte) string {
	const limit = 80
	if len(payload) <= limit {
		return string(payload)
	}
	return string(payload[:limit]) + "…"
}
