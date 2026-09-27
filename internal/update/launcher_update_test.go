package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestCompareVersions 版本比较：数字段、预发布后缀、v 前缀与不可解析值。
func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.1.0", "1.0.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.0", "1.0.0", 0},
		{"1.0.0", "1.0", 0},
		// 预发布 < 正式版
		{"1.0.0", "1.0.0-preview4", 1},
		{"1.0.0-preview4", "1.0.0", -1},
		// 同核心的预发布按字母+数字比较（preview9 < preview10，别按字符串排）
		{"1.0.0-preview10", "1.0.0-preview9", 1},
		{"1.0.0-preview2", "1.0.0-preview10", -1},
		{"1.0.0-beta", "1.0.0-preview", -1},
		// 无法解析的标记（重建分支的 Nya_Rebuild）按 0.0.0 处理，不 panic
		{"Nya_Rebuild", "1.0.0", -1},
		{"Nya_Rebuild", "Nya_Rebuild", 0},
		{"", "", 0},
	}

	for _, item := range cases {
		got := CompareVersions(item.a, item.b)
		if (got > 0) != (item.want > 0) || (got < 0) != (item.want < 0) {
			t.Errorf("CompareVersions(%q, %q) = %d，期望符号与 %d 一致", item.a, item.b, got, item.want)
		}
	}
}

// TestPickLatestReleaseSkipsDraftsAndPrereleases 挑最新版本时跳过草稿，
// 且按需过滤预发布（不依赖接口返回顺序）。
func TestPickLatestReleaseSkipsDraftsAndPrereleases(t *testing.T) {
	releases := []release{
		{TagName: "v1.2.0-preview1", Prerelease: true, PublishedAt: time.Now()},
		{TagName: "v9.9.9", Draft: true, PublishedAt: time.Now()},
		{TagName: "v1.1.0", PublishedAt: time.Now().Add(-time.Hour)},
		{TagName: "v1.0.0", PublishedAt: time.Now().Add(-2 * time.Hour)},
	}

	stable := pickLatestRelease(releases, false)
	if stable == nil || stable.TagName != "v1.1.0" {
		t.Fatalf("仅稳定版应挑到 v1.1.0，得到 %+v", stable)
	}

	withPre := pickLatestRelease(releases, true)
	if withPre == nil || withPre.TagName != "v1.2.0-preview1" {
		t.Fatalf("含预发布应挑到 v1.2.0-preview1，得到 %+v", withPre)
	}

	// 全是草稿时返回 nil
	if got := pickLatestRelease([]release{{TagName: "v1.0.0", Draft: true}}, true); got != nil {
		t.Fatalf("草稿不该被选中：%+v", got)
	}
}

// TestCheckAgainstFakeGitHub 端到端（假 GitHub）：查到新版本、选中平台资产并给出结论。
func TestCheckAgainstFakeGitHub(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("User-Agent") == "" {
			t.Errorf("请求缺少 User-Agent（GitHub 会拒绝）")
		}
		payload := []map[string]any{
			{
				"tag_name":     "v1.0.0-preview5",
				"name":         "preview5",
				"body":         "修了一堆东西",
				"prerelease":   true,
				"draft":        false,
				"html_url":     "https://example.com/releases/v1.0.0-preview5",
				"published_at": time.Now().Format(time.RFC3339),
				"assets": []map[string]any{
					{"name": "NekoLauncher.exe", "browser_download_url": "https://example.com/NekoLauncher.exe", "size": 6 << 20},
					{"name": "NekoLauncher-windows-amd64-portable.zip", "browser_download_url": "https://example.com/portable.zip", "size": 7 << 20},
				},
			},
			{
				"tag_name":   "v0.9.0",
				"name":       "stable",
				"draft":      false,
				"prerelease": false,
				"assets":     []map[string]any{},
			},
		}
		_ = json.NewEncoder(writer).Encode(payload)
	}))
	defer server.Close()

	restore := releasesAPI
	releasesAPI = server.URL
	t.Cleanup(func() { releasesAPI = restore })

	result, err := Check(context.Background(), "1.0.0-preview4", true)
	if err != nil {
		t.Fatalf("检查更新失败：%v", err)
	}
	if result.LatestVersion != "1.0.0-preview5" || !result.UpdateAvailable || !result.Prerelease {
		t.Fatalf("检查结果不符：%+v", result)
	}
	if result.Notes != "修了一堆东西" || result.PublishedAt == 0 {
		t.Fatalf("版本说明/时间没有带上：%+v", result)
	}
	if runtime.GOOS == "windows" {
		if result.Asset == nil || result.Asset.Name != "NekoLauncher.exe" {
			t.Fatalf("Windows 上应选中裸 exe 资产（可直接替换）：%+v", result.Asset)
		}
		if !result.CanSelfUpdate {
			t.Fatal("Windows 应支持自动替换")
		}
	} else {
		// 其它平台不自动替换：必须给出可读的手动指引
		if result.Asset != nil {
			t.Fatalf("非 Windows 不该选中自动替换资产：%+v", result.Asset)
		}
		if result.ManualHint == "" {
			t.Fatal("非 Windows 应给出手动下载提示")
		}
	}

	// 不看预发布时只剩稳定版 v0.9.0：比当前的 1.0.0-preview4 还旧，不该提示更新
	// （稳定版与预发布版的先后关系正是版本比较里最容易写错的地方）
	stableOnly, err := Check(context.Background(), "1.0.0-preview4", false)
	if err != nil {
		t.Fatalf("仅稳定版检查失败：%v", err)
	}
	if stableOnly.LatestVersion != "0.9.0" || stableOnly.UpdateAvailable {
		t.Fatalf("0.9.0 旧于 1.0.0-preview4，不该提示更新：%+v", stableOnly)
	}

	// 已经是同一个版本时也不提示
	upToDate, err := Check(context.Background(), "1.0.0-preview5", true)
	if err != nil {
		t.Fatalf("检查失败：%v", err)
	}
	if upToDate.UpdateAvailable {
		t.Fatalf("已是最新版本却提示更新：%+v", upToDate)
	}
}

// TestCheckErrors 空仓库、限流与网络错误都要给出可读信息，而不是把原始错误丢给用户。
func TestCheckErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		payload string
		want    string
	}{
		{"没有版本", http.StatusOK, `[]`, "还没有发布任何版本"},
		{"限流", http.StatusForbidden, `{"message":"rate limit"}`, "限流"},
		{"仓库不存在", http.StatusNotFound, `{}`, "版本页不存在"},
	}

	for _, item := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(item.status)
			_, _ = writer.Write([]byte(item.payload))
		}))

		restore := releasesAPI
		releasesAPI = server.URL

		_, err := Check(context.Background(), "1.0.0", true)
		server.Close()
		releasesAPI = restore

		if err == nil || !strings.Contains(err.Error(), item.want) {
			t.Errorf("%s：错误信息里应包含 %q，得到 %v", item.name, item.want, err)
		}
	}
}

// TestSanitizeAssetName 资产名里的路径分隔符要被剥掉（不能当成路径用）。
func TestSanitizeAssetName(t *testing.T) {
	cases := map[string]string{
		"NekoLauncher.exe":             "NekoLauncher.exe",
		"../../evil.exe":               "evil.exe",
		`..\\windows\\system32\\a.exe`: "a.exe",
		"   ":                          "",
		"..":                           "",
	}

	for input, want := range cases {
		if got := sanitizeAssetName(input); got != want {
			t.Errorf("sanitizeAssetName(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// TestValidateReplacement 替换前的粗筛：目录、空文件、过小文件都要被拒。
func TestValidateReplacement(t *testing.T) {
	directory := t.TempDir()

	if err := validateReplacement(directory); err == nil {
		t.Error("目录不该通过校验")
	}
	tiny := filepath.Join(directory, "tiny.exe")
	if err := os.WriteFile(tiny, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateReplacement(tiny); err == nil {
		t.Error("过小文件不该通过校验")
	}
	big := filepath.Join(directory, "big.exe")
	if err := os.WriteFile(big, make([]byte, minimumExecutableBytes+1), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateReplacement(big); err != nil {
		t.Errorf("正常大小文件应通过：%v", err)
	}
	if err := validateReplacement(filepath.Join(directory, "missing.exe")); err == nil {
		t.Error("不存在的文件不该通过校验")
	}
}

// TestApplyReplacesExecutable 非 Windows 上 Apply 必须明确拒绝自动更新
// （返回 ErrManualUpdateRequired，由前端引导手动下载）；
// Windows 的就地替换流程在 launcher_update_windows_test.go 里验证。
func TestApplyReplacesExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 替换流程见 launcher_update_windows_test.go")
	}

	if _, err := Apply("whatever.exe"); err != ErrManualUpdateRequired {
		t.Fatalf("非 Windows 应返回 ErrManualUpdateRequired，得到 %v", err)
	}
}

// TestDownloadRejectsTruncatedAsset 大小不符的下载必须被删掉并报错：
// 把截断的 exe 换上去等于把启动器弄坏。
func TestDownloadRejectsTruncatedAsset(t *testing.T) {
	payload := make([]byte, minimumExecutableBytes+100)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	asset := Asset{Name: "NekoLauncher.exe", URL: server.URL + "/NekoLauncher.exe", Size: int64(len(payload)) + 1}
	if _, err := Download(context.Background(), asset, nil); err == nil {
		t.Fatal("大小不符时应报错")
	}

	// 大小一致时正常落盘
	asset.Size = int64(len(payload))
	path, err := Download(context.Background(), asset, nil)
	if err != nil {
		t.Fatalf("下载失败：%v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if info, err := os.Stat(path); err != nil || info.Size() != int64(len(payload)) {
		t.Fatalf("落盘文件不符：%v / %+v", err, info)
	}
}
