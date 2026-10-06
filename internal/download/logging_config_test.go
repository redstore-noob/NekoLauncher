package download

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// loggingMetadataFixture 原版版本 JSON 里的 logging 段（取自真实 1.16.5 描述）。
func loggingMetadataFixture(url, sha1Value string, size int) *VersionJSON {
	payload := `{
  "id": "1.16.5",
  "logging": {
    "client": {
      "argument": "-Dlog4j.configurationFile=${path}",
      "file": {"id": "client-1.12.xml", "sha1": "` + sha1Value + `", "size": ` +
		strconv.Itoa(size) + `, "url": "` + url + `"},
      "type": "log4j2-xml"
    }
  }
}`
	var metadata VersionJSON
	if err := json.Unmarshal([]byte(payload), &metadata); err != nil {
		panic(err)
	}

	return &metadata
}

// TestEnsureLoggingConfigFromMetadataURL 扁平化实例的修复路径：本地没有 logging 段，
// 凭 clientVersion 取回原版描述后补下配置文件。
func TestEnsureLoggingConfigFromMetadataURL(t *testing.T) {
	content := []byte(`<Configuration status="WARN"/>`)
	sum := sha1.Sum(content)
	checksum := hex.EncodeToString(sum[:])

	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/client-1.12.xml") {
			_, _ = writer.Write(content)

			return
		}
		// 版本描述：logging 指向同一台测试服务器
		payload := `{"id":"1.21.1","logging":{"client":{"argument":"-Dlog4j.configurationFile=${path}",
		  "file":{"id":"client-1.12.xml","sha1":"` + checksum + `","size":` + strconv.Itoa(len(content)) +
			`,"url":"` + "https://" + request.Host + `/client-1.12.xml"}}}}`
		_, _ = writer.Write([]byte(payload))
	}))
	defer server.Close()

	restoreClient := longDownloadClient
	longDownloadClient = server.Client()
	t.Cleanup(func() { longDownloadClient = restoreClient })

	root := t.TempDir()
	downloaded, err := EnsureLoggingConfigFromMetadataURL(t.Context(), root, server.URL+"/1.21.1.json", nil)
	if err != nil {
		t.Fatalf("修复路径失败：%v", err)
	}
	if !downloaded {
		t.Fatal("应当补下日志配置")
	}
	if _, err := os.Stat(filepath.Join(root, "assets", "log_configs", "client-1.12.xml")); err != nil {
		t.Fatalf("配置文件没有落盘：%v", err)
	}

	// 元数据地址为空 / 取不到 / 没有 logging 段：静默返回，不报错也不下载
	if downloaded, err := EnsureLoggingConfigFromMetadataURL(t.Context(), root, "", nil); err != nil || downloaded {
		t.Fatalf("空地址应为空操作，得到 (%v, %v)", downloaded, err)
	}
	if downloaded, err := EnsureLoggingConfigFromMetadataURL(t.Context(), root, server.URL+"/missing.json", nil); err != nil || downloaded {
		t.Fatalf("元数据取不到时应静默跳过，得到 (%v, %v)", downloaded, err)
	}
}

// TestClientVersionMarkerAndLoggingProbe 扁平化标记与 logging 段的探测。
func TestClientVersionMarkerAndLoggingProbe(t *testing.T) {
	flattened := []byte(`{"id":"fabric-loader-0.19.5-1.21.1","clientVersion":"1.21.1"}`)
	if got := ClientVersionOf(flattened); got != "1.21.1" {
		t.Fatalf("clientVersion = %q", got)
	}
	if MetadataHasLogging(flattened) {
		t.Fatal("扁平化实例的 JSON 里不该探测到 logging 段")
	}
	if got := ClientVersionOf([]byte(`{"id":"1.16.5"}`)); got != "" {
		t.Fatalf("没有标记时应为空，得到 %q", got)
	}
	if !MetadataHasLogging([]byte(
		`{"logging":{"client":{"argument":"-Dlog4j.configurationFile=${path}","file":{"id":"client-1.12.xml","url":"https://example.com/x"}}}}`)) {
		t.Fatal("有 logging 段时应探测到")
	}
}

// TestLoggingConfigPlan 计划：文件落在 assets/log_configs/<id>，带上官方校验值。
func TestLoggingConfigPlan(t *testing.T) {
	metadata := loggingMetadataFixture("https://example.com/client-1.12.xml", "abc", 888)
	root := t.TempDir()

	plan := createLoggingPlan(metadata, root)
	if plan == nil {
		t.Fatal("应当生成日志配置下载条目")
	}
	want := filepath.Join(root, "assets", "log_configs", "client-1.12.xml")
	if plan.targetPath != want {
		t.Fatalf("落盘路径 = %q，期望 %q", plan.targetPath, want)
	}
	if plan.url != "https://example.com/client-1.12.xml" || plan.sha1 != "abc" {
		t.Fatalf("下载条目不符：%+v", plan)
	}
	if got := LoggingConfigPath(root, metadata); got != want {
		t.Fatalf("LoggingConfigPath = %q，期望 %q", got, want)
	}
	if got := LoggingConfigArgument(metadata); got != "-Dlog4j.configurationFile=${path}" {
		t.Fatalf("LoggingConfigArgument = %q", got)
	}
}

// TestLoggingConfigPlanWithoutSection 没有 logging 段时不做任何事。
func TestLoggingConfigPlanWithoutSection(t *testing.T) {
	var metadata VersionJSON
	if err := json.Unmarshal([]byte(`{"id":"1.16.5"}`), &metadata); err != nil {
		t.Fatalf("解析失败：%v", err)
	}

	if plan := createLoggingPlan(&metadata, t.TempDir()); plan != nil {
		t.Fatalf("没有 logging 段时不该生成下载条目：%+v", plan)
	}
	if got := LoggingConfigPath(t.TempDir(), &metadata); got != "" {
		t.Fatalf("没有 logging 段时路径应为空，得到 %q", got)
	}

	downloaded, err := EnsureLoggingConfig(t.Context(), t.TempDir(), &metadata, nil)
	if err != nil || downloaded {
		t.Fatalf("没有 logging 段时应为空操作，得到 (%v, %v)", downloaded, err)
	}
}

// TestEnsureLoggingConfigDownloads 缺失时下载；已就位时跳过；内容被改坏时重新下载。
func TestEnsureLoggingConfigDownloads(t *testing.T) {
	content := []byte(`<Configuration status="WARN"><Appenders/></Configuration>`)
	sum := sha1.Sum(content)
	checksum := hex.EncodeToString(sum[:])

	requests := 0
	// 下载走的是 HTTPS 校验 + 长时下载客户端：这里换成测试服务器自己的客户端
	// （自带自签证书），既不放松生产校验，也不依赖外网
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		_, _ = writer.Write(content)
	}))
	defer server.Close()

	restoreClient := longDownloadClient
	longDownloadClient = server.Client()
	t.Cleanup(func() { longDownloadClient = restoreClient })

	metadata := loggingMetadataFixture(server.URL+"/client-1.12.xml", checksum, len(content))
	root := t.TempDir()

	var messages []string
	status := func(message string) { messages = append(messages, message) }

	downloaded, err := EnsureLoggingConfig(t.Context(), root, metadata, status)
	if err != nil {
		t.Fatalf("补全日志配置失败：%v", err)
	}
	if !downloaded || requests != 1 {
		t.Fatalf("首次应下载一次，得到 downloaded=%v requests=%d", downloaded, requests)
	}

	target := filepath.Join(root, "assets", "log_configs", "client-1.12.xml")
	saved, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("日志配置没有落盘：%v", err)
	}
	if string(saved) != string(content) {
		t.Fatalf("落盘内容不符：%s", saved)
	}

	// 第二次：已经就位，不该再下载
	downloaded, err = EnsureLoggingConfig(t.Context(), root, metadata, status)
	if err != nil {
		t.Fatalf("二次校验失败：%v", err)
	}
	if downloaded || requests != 1 {
		t.Fatalf("已就位时不该重新下载，得到 downloaded=%v requests=%d", downloaded, requests)
	}

	// 内容被改坏：校验值不符 → 重新下载覆盖，并给出可读提示
	if err := os.WriteFile(target, []byte("坏了"), 0o644); err != nil {
		t.Fatalf("改坏文件失败：%v", err)
	}
	downloaded, err = EnsureLoggingConfig(t.Context(), root, metadata, status)
	if err != nil {
		t.Fatalf("第三次补全失败：%v", err)
	}
	if !downloaded || requests != 2 {
		t.Fatalf("校验值不符时应重新下载，得到 downloaded=%v requests=%d", downloaded, requests)
	}
	if len(messages) == 0 || !strings.Contains(strings.Join(messages, "|"), "校验值不符") {
		t.Fatalf("应给出「校验值不符」的提示，实际：%v", messages)
	}
}
