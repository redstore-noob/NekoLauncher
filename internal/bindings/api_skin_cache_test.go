package bindings

import (
	"net/http"
	"testing"
	"time"

	"nekolauncher/internal/config"
)

func TestIsMojangRetryableStatus(t *testing.T) {
	if !isMojangRetryableStatus(http.StatusTooManyRequests) {
		t.Fatal("429 应可重试")
	}
	if !isMojangRetryableStatus(http.StatusServiceUnavailable) {
		t.Fatal("503 应可重试")
	}
	if isMojangRetryableStatus(http.StatusOK) || isMojangRetryableStatus(http.StatusUnauthorized) {
		t.Fatal("2xx/401 不应重试")
	}
}

func TestRetryAfterDuration(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "2")
	if got := retryAfterDuration(resp, 0); got != 2*time.Second {
		t.Fatalf("Retry-After=2 应得 2s，实际 %v", got)
	}
	resp.Header.Set("Retry-After", "999")
	if got := retryAfterDuration(resp, 0); got != 5*time.Second {
		t.Fatalf("Retry-After 应封顶 5s，实际 %v", got)
	}
	// 无头时按指数退避
	if got := retryAfterDuration(&http.Response{Header: http.Header{}}, 0); got != time.Second {
		t.Fatalf("首次退避应为 1s，实际 %v", got)
	}
	if got := retryAfterDuration(&http.Response{Header: http.Header{}}, 2); got != 4*time.Second {
		t.Fatalf("第三次退避应为 4s，实际 %v", got)
	}
}

func TestSanitizeCacheKey(t *testing.T) {
	got := sanitizeCacheKey("authlib:uuid@https://api.example.com/")
	if got != "authlib_uuid@https___api.example.com" {
		t.Fatalf("稳定键未正确净化：%s", got)
	}
	if sanitizeCacheKey("...") != "account" {
		t.Fatal("空键应回退 account")
	}
}

func TestSkinTextureCacheRoundTrip(t *testing.T) {
	old := config.StorageDirectory()
	if err := config.SetStorageDirectory(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer config.SetStorageDirectory(old)

	tex := &SkinTexture{
		SkinUri:     "data:image/png;base64,AAAA",
		Model:       "slim",
		CapeUri:     "data:image/png;base64,BBBB",
		DisplayName: "Nya",
		UpdatedAt:   time.Now().Unix(),
	}
	writeSkinTextureCache("microsoft:abc", tex)

	got, ok := readSkinTextureCache("microsoft:abc")
	if !ok {
		t.Fatal("缓存应命中")
	}
	if got.SkinUri != tex.SkinUri || got.Model != "slim" || got.CapeUri != tex.CapeUri {
		t.Fatalf("缓存内容不一致：%+v", got)
	}
	if !got.Cached {
		t.Fatal("命中缓存应标记 Cached=true")
	}
	if !skinTextureCacheFresh(got) {
		t.Fatal("刚写入的缓存应为新鲜")
	}

	got.UpdatedAt = time.Now().Add(-25 * time.Hour).Unix()
	if skinTextureCacheFresh(got) {
		t.Fatal("超过 24 小时的缓存应判定为过期")
	}

	invalidateSkinTextureCache("microsoft:abc")
	if _, ok := readSkinTextureCache("microsoft:abc"); ok {
		t.Fatal("失效后不应再命中")
	}
}
