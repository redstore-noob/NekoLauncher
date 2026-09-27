package auth

import (
	"encoding/json"
	"testing"
)

// TestAuthlibArtifactBothSchemas 官方现行 latest.json 用的是
// download_url + checksums 嵌套结构，早期格式才是顶层 url/sha256。
// 只认后者会让下载地址恒为空，两个源全部失败——皮肤站账号直接启动不了。
func TestAuthlibArtifactBothSchemas(t *testing.T) {
	current := []byte(`{
		"build_number": 56,
		"version": "1.2.8",
		"release_time": "2026-07-11T17:14:13Z",
		"download_url": "https://authlib-injector.yushi.moe/artifact/56/authlib-injector-1.2.8.jar",
		"checksums": {"sha256": "9c7f4343e6c82034958ffb48c14a2cb0c85928be7283103ce17da00c6d5a7b10"}
	}`)

	var artifact AuthlibArtifact
	if err := json.Unmarshal(current, &artifact); err != nil {
		t.Fatalf("解析现行格式失败：%v", err)
	}
	if artifact.Version != "1.2.8" {
		t.Fatalf("version = %q", artifact.Version)
	}
	if got := artifact.ArtifactURL(); got != "https://authlib-injector.yushi.moe/artifact/56/authlib-injector-1.2.8.jar" {
		t.Fatalf("ArtifactURL() = %q", got)
	}
	if got := artifact.ArtifactSha256(); got != "9c7f4343e6c82034958ffb48c14a2cb0c85928be7283103ce17da00c6d5a7b10" {
		t.Fatalf("ArtifactSha256() = %q", got)
	}
	if !hasKnownHash(&artifact) {
		t.Fatal("现行格式应当被认定为有已知哈希")
	}

	legacy := []byte(`{"version":"1.0.0","url":"https://example.com/a.jar","sha1":"abc"}`)
	// 必须用新变量：json.Unmarshal 只覆盖 JSON 里出现的字段，
	// 复用上面的结构体会把现行格式的 download_url 留在里面
	var legacyArtifact AuthlibArtifact
	if err := json.Unmarshal(legacy, &legacyArtifact); err != nil {
		t.Fatalf("解析旧格式失败：%v", err)
	}
	if got := legacyArtifact.ArtifactURL(); got != "https://example.com/a.jar" {
		t.Fatalf("旧格式 ArtifactURL() = %q", got)
	}
	if got := legacyArtifact.ArtifactSha1(); got != "abc" {
		t.Fatalf("旧格式 ArtifactSha1() = %q", got)
	}
}

// TestResolveInjectorDownloadUrl 官方地址映射到 BMCLAPI 镜像时不能把 /artifact
// 拼两遍（镜像目录已经包含了它）。
func TestResolveInjectorDownloadUrl(t *testing.T) {
	got := resolveInjectorDownloadUrl(
		officialLatestJsonUrl,
		"https://authlib-injector.yushi.moe/artifact/56/authlib-injector-1.2.8.jar")
	want := "https://bmclapi2.bangbang93.com/mirrors/authlib-injector/artifact/56/authlib-injector-1.2.8.jar"
	if got != want {
		t.Fatalf("映射结果 = %q，期望 %q", got, want)
	}

	// 非官方源 / 相对地址：原样返回，不做映射
	custom := "https://mirror.example.com/x.jar"
	if got := resolveInjectorDownloadUrl("https://other.example.com/latest.json", custom); got != custom {
		t.Fatalf("非官方源不应改写：%q", got)
	}
	if got := resolveInjectorDownloadUrl(officialLatestJsonUrl, "not-a-url"); got != "not-a-url" {
		t.Fatalf("非绝对地址不应改写：%q", got)
	}
}
