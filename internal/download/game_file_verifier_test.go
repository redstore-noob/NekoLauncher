package download

// 游戏文件校验器的链式遍历。
//
// 背景：载荷不再下发客户端 jar 本体（见 internal/solo/export.go），补全路径
// 因此必须覆盖 inheritsFrom 之外、由 jar 字段引用的旁支版本——只沿 inheritsFrom
// 走会漏掉它，玩家装完就启动不了。

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeVerifierFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败 %s：%v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件失败 %s：%v", path, err)
	}
}

type verifierRoundTripFunc func(*http.Request) (*http.Response, error)

func (f verifierRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// stubVersionManifest 用一份固定的版本清单替换下载客户端，让测试不联网。
// 清单刻意留空：getMetadataURL 查不到任何版本 → 校验器不会真的发起下载，
// 但"发现客户端文件缺失"的状态回调照常发出，正好用来断言遍历到了哪些版本。
func stubVersionManifest(t *testing.T) {
	t.Helper()
	original := longDownloadClient
	t.Cleanup(func() { longDownloadClient = original })
	longDownloadClient = &http.Client{
		Transport: verifierRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`{"latest":{"release":"1.20.1","snapshot":"1.20.1"},"versions":[]}`)),
				Request: req,
			}, nil
		}),
	}
}

func TestVerifyAndRepairFollowsJarProvider(t *testing.T) {
	stubVersionManifest(t)

	root := t.TempDir()
	// 模拟载荷安装后的状态：三份描述文件都在，客户端 jar 本体都没有
	writeVerifierFile(t, root, "versions/MyPack/MyPack.json",
		`{"id":"MyPack","inheritsFrom":"1.20.1","jar":"1.7.10"}`)
	writeVerifierFile(t, root, "versions/1.20.1/1.20.1.json",
		`{"id":"1.20.1","downloads":{"client":{"url":"https://piston-data.mojang.com/v1/objects/aa/client.jar","sha1":"aa","size":1}}}`)
	writeVerifierFile(t, root, "versions/1.7.10/1.7.10.json",
		`{"id":"1.7.10","downloads":{"client":{"url":"https://piston-data.mojang.com/v1/objects/bb/client.jar","sha1":"bb","size":1}}}`)
	// 谁都没引用它：不该被遍历到
	writeVerifierFile(t, root, "versions/1.6.4/1.6.4.json",
		`{"id":"1.6.4","downloads":{"client":{"url":"https://piston-data.mojang.com/v1/objects/cc/client.jar","sha1":"cc","size":1}}}`)

	var messages []string
	if _, err := (&GameFileVerifier{}).VerifyAndRepair(
		context.Background(), root, "MyPack", false, func(message string) {
			messages = append(messages, message)
		}); err != nil {
		t.Fatalf("校验并补全失败：%v", err)
	}

	joined := strings.Join(messages, "\n")
	// 1.20.1 来自 inheritsFrom，1.7.10 只由 jar 字段引用
	for _, want := range []string{"1.20.1", "1.7.10"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("未遍历到版本 %s 的客户端 JAR，状态回调：\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "1.6.4") {
		t.Fatalf("未被引用的版本不应被遍历，状态回调：\n%s", joined)
	}
}
