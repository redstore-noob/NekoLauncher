package download

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

// edge.forgecdn.net 的 GET 在部分网络下直接 404（HEAD 才 302），下载引擎必须
// 能换到路径同构的 mediafilez.forgecdn.net 重下；非 edge 域名不做兜底。
func TestForgeCDNFallbackURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "edge 域名换 mediafilez",
			url:  "https://edge.forgecdn.net/files/8945/86/All%20the%20Mods%2010-8.2.zip",
			want: "https://mediafilez.forgecdn.net/files/8945/86/All%20the%20Mods%2010-8.2.zip",
		},
		{name: "mediafilez 不再兜底", url: "https://mediafilez.forgecdn.net/files/1/2/a.jar", want: ""},
		{name: "modrinth 不兜底", url: "https://cdn.modrinth.com/data/A/b.jar", want: ""},
		{name: "mojang 不兜底", url: "https://piston-data.mojang.com/x/y", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := forgeCDNFallbackURL(tt.url); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// 404（edge CDN 实测行为）必须触发兜底，403/500 不触发（语义不同）。
func TestIsHTTPStatusTriggersForgeCDNSwap(t *testing.T) {
	notFound := &httpStatusError{StatusCode: http.StatusNotFound}
	if !isHTTPStatus(notFound, http.StatusNotFound) {
		t.Fatal("404 应命中兜底条件")
	}
	wrapped := fmt.Errorf("下载失败：%w", errors.Join(fmt.Errorf("x"), notFound))
	if !isHTTPStatus(wrapped, http.StatusNotFound) {
		t.Fatal("包装后的 404 也应命中兜底条件")
	}
	if isHTTPStatus(&httpStatusError{StatusCode: http.StatusForbidden}, http.StatusNotFound) {
		t.Fatal("403 不应命中 404 兜底条件")
	}
	if isHTTPStatus(nil, http.StatusNotFound) || isHTTPStatus(errors.New("x"), http.StatusNotFound) {
		t.Fatal("非状态错误不应命中兜底条件")
	}
}
