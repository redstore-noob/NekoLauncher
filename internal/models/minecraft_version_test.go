package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestMinecraftVersionTypeDisplay 防的回归：版本类型中文名错位/丢失，
// 前端版本列表里 release 与 snapshot 显示成同一串（用户分不清正式版与快照）。
func TestMinecraftVersionTypeDisplay(t *testing.T) {
	cases := []struct {
		name        string
		versionType string
		want        string
	}{
		{name: "release", versionType: "release", want: "正式版"},
		{name: "snapshot", versionType: "snapshot", want: "快照版"},
		{name: "old_beta", versionType: "old_beta", want: "经典 Beta"},
		{name: "old_alpha", versionType: "old_alpha", want: "经典 Alpha"},
		{name: "空类型", versionType: "", want: ""},
		// 未知类型原样透出，方便发现 Mojang 新增类型；不能被吞成某个已知名字
		{name: "未知类型原样透出", versionType: "april_fools", want: "april_fools"},
		// 大小写敏感：类型串来自 API，不做归一化，改动归一化逻辑要同步这里的期望
		{name: "大小写敏感", versionType: "Release", want: "Release"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			version := MinecraftVersion{Type: tc.versionType}
			if got := version.TypeDisplay(); got != tc.want {
				t.Fatalf("TypeDisplay(%q) = %q，期望 %q", tc.versionType, got, tc.want)
			}
		})
	}
}

// TestMinecraftVersionTypeIcon 防的回归：图标复制粘贴错位（快照版显示成正式版图标），
// 以及未知类型没有兜底图标导致前端出现空图标位。
func TestMinecraftVersionTypeIcon(t *testing.T) {
	cases := []struct {
		name        string
		versionType string
		want        string
	}{
		{name: "release", versionType: "release", want: "📦"},
		{name: "snapshot", versionType: "snapshot", want: "🧪"},
		{name: "old_beta", versionType: "old_beta", want: "🔶"},
		{name: "old_alpha", versionType: "old_alpha", want: "🔷"},
		{name: "空类型兜底", versionType: "", want: "📄"},
		{name: "未知类型兜底", versionType: "unknown", want: "📄"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			version := MinecraftVersion{Type: tc.versionType}
			if got := version.TypeIcon(); got != tc.want {
				t.Fatalf("TypeIcon(%q) = %q，期望 %q", tc.versionType, got, tc.want)
			}
		})
	}

	// 四种已知类型的图标必须互不相同，否则列表里根本区分不出类型
	seen := map[string]string{}
	for _, versionType := range []string{"release", "snapshot", "old_beta", "old_alpha"} {
		icon := MinecraftVersion{Type: versionType}.TypeIcon()
		if other, duplicated := seen[icon]; duplicated {
			t.Fatalf("%s 与 %s 的图标重复：%q", versionType, other, icon)
		}
		seen[icon] = versionType
	}
}

// TestMinecraftVersionDisplayName 防的回归：显示名把版本号弄丢或少了 Minecraft 前缀，
// 实例列表与下载页出现两个看不出区别的条目。
func TestMinecraftVersionDisplayName(t *testing.T) {
	if got := (MinecraftVersion{ID: "1.21.1"}).DisplayName(); got != "Minecraft 1.21.1" {
		t.Fatalf("DisplayName() = %q，期望 %q", got, "Minecraft 1.21.1")
	}
	if got := (MinecraftVersion{ID: "24w33a"}).DisplayName(); got != "Minecraft 24w33a" {
		t.Fatalf("快照版显示名 = %q", got)
	}
	// ID 缺失时不 panic（清单字段偶尔缺 id）
	if got := (MinecraftVersion{}).DisplayName(); got != "Minecraft " {
		t.Fatalf("空 ID 的显示名 = %q", got)
	}
}

// TestVersionManifestUnmarshal 防的回归：json tag 与 Mojang 清单字段名不一致
// （例如把 releaseTime 写成 release_time），反序列化后时间全为零值，
// 版本列表按时间排序时顺序错乱。
func TestVersionManifestUnmarshal(t *testing.T) {
	payload := `{
	  "latest": {"release": "1.21.1", "snapshot": "24w33a"},
	  "versions": [
	    {
	      "id": "1.21.1",
	      "type": "release",
	      "url": "https://piston-meta.mojang.com/v1/packages/aaa/1.21.1.json",
	      "time": "2024-08-08T14:24:53+00:00",
	      "releaseTime": "2024-08-08T14:22:55+00:00",
	      "sha1": "deadbeef",
	      "complianceLevel": 1,
	      "IsLatestRelease": true
	    },
	    {
	      "id": "24w33a",
	      "type": "snapshot",
	      "url": "https://piston-meta.mojang.com/v1/packages/bbb/24w33a.json",
	      "time": "2024-08-15T10:00:00Z",
	      "releaseTime": "2024-08-15T09:59:00Z"
	    }
	  ]
	}`

	var manifest VersionManifest
	if err := json.Unmarshal([]byte(payload), &manifest); err != nil {
		t.Fatalf("解析版本清单失败：%v", err)
	}

	if manifest.Latest.Release != "1.21.1" || manifest.Latest.Snapshot != "24w33a" {
		t.Fatalf("latest 段解析错误：%+v", manifest.Latest)
	}
	if len(manifest.Versions) != 2 {
		t.Fatalf("版本条目数量 = %d，期望 2", len(manifest.Versions))
	}

	first := manifest.Versions[0]
	if first.ID != "1.21.1" || first.Type != "release" {
		t.Fatalf("第一条版本解析错误：%+v", first)
	}
	if !strings.HasSuffix(first.URL, "/1.21.1.json") {
		t.Fatalf("url 字段解析错误：%q", first.URL)
	}
	wantTime := time.Date(2024, 8, 8, 14, 24, 53, 0, time.UTC)
	if !first.Time.Equal(wantTime) {
		t.Fatalf("time = %v，期望 %v", first.Time, wantTime)
	}
	wantReleaseTime := time.Date(2024, 8, 8, 14, 22, 55, 0, time.UTC)
	if !first.ReleaseTime.Equal(wantReleaseTime) {
		t.Fatalf("releaseTime = %v，期望 %v", first.ReleaseTime, wantReleaseTime)
	}

	// IsLatest* 是运行期标记（json:"-"），API 里即使带同名字段也不能被写进来，
	// 否则"最新版"高亮会被服务端数据带偏
	if first.IsLatestRelease || first.IsLatestSnapshot {
		t.Fatalf("IsLatest* 只能由加载方填充，解析后应为 false：%+v", first)
	}

	// 时间字段缺失时为零值且不报错（旧清单里偶尔缺字段）
	var partial VersionManifest
	if err := json.Unmarshal([]byte(`{"versions":[{"id":"x","type":"release"}]}`), &partial); err != nil {
		t.Fatalf("缺时间字段不应报错：%v", err)
	}
	if len(partial.Versions) != 1 || !partial.Versions[0].Time.IsZero() {
		t.Fatalf("缺时间字段应得到零值：%+v", partial.Versions)
	}
}

// TestVersionManifestUnmarshalEmptyAndUnknown 防的回归：
// 清单为空/null 时 panic 或产生 nil 切片导致的越界；
// 以及多余字段（Mojang 加新字段）让整份清单解析失败。
func TestVersionManifestUnmarshalEmptyAndUnknown(t *testing.T) {
	var empty VersionManifest
	if err := json.Unmarshal([]byte(`{}`), &empty); err != nil {
		t.Fatalf("空清单不应报错：%v", err)
	}
	if len(empty.Versions) != 0 {
		t.Fatalf("空清单不应有版本条目：%+v", empty.Versions)
	}
	if empty.Latest.Release != "" || empty.Latest.Snapshot != "" {
		t.Fatalf("空清单的 latest 应为零值：%+v", empty.Latest)
	}

	var explicitEmpty VersionManifest
	if err := json.Unmarshal([]byte(`{"latest":{"release":"1.0"},"versions":[]}`), &explicitEmpty); err != nil {
		t.Fatalf("versions 为空数组不应报错：%v", err)
	}
	if len(explicitEmpty.Versions) != 0 {
		t.Fatalf("versions 为空数组时长度应为 0：%d", len(explicitEmpty.Versions))
	}
	if explicitEmpty.Latest.Release != "1.0" {
		t.Fatalf("latest 应照常解析：%+v", explicitEmpty.Latest)
	}

	var nullVersions VersionManifest
	if err := json.Unmarshal([]byte(`{"versions":null}`), &nullVersions); err != nil {
		t.Fatalf("versions 为 null 不应报错：%v", err)
	}
	if len(nullVersions.Versions) != 0 {
		t.Fatalf("versions 为 null 时长度应为 0：%d", len(nullVersions.Versions))
	}
}

// TestMinecraftVersionTimeMustBeRFC3339 防的回归：
// time.Time 字段被换成 string 或自定义宽松解析后，格式错误的时间被静默吞掉；
// 当前契约是"格式错就报错"，这里把它钉住。
func TestMinecraftVersionTimeMustBeRFC3339(t *testing.T) {
	var manifest VersionManifest
	err := json.Unmarshal([]byte(`{"versions":[{"id":"x","type":"release","time":"2024-08-08 14:24:53"}]}`), &manifest)
	if err == nil {
		t.Fatal("非法时间格式应导致解析失败（目前契约是严格 RFC3339）")
	}

	// 反过来：合法 RFC3339 的各种写法都必须能解析（带不带毫秒、Z 还是 +08:00）
	for _, raw := range []string{
		"2024-08-08T14:24:53Z",
		"2024-08-08T14:24:53.123Z",
		"2024-08-08T14:24:53+00:00",
		"2024-08-08T22:24:53+08:00",
	} {
		var single VersionManifest
		payload := `{"versions":[{"id":"x","type":"release","time":"` + raw + `"}]}`
		if err := json.Unmarshal([]byte(payload), &single); err != nil {
			t.Fatalf("合法时间 %q 解析失败：%v", raw, err)
		}
		if len(single.Versions) != 1 || single.Versions[0].Time.IsZero() {
			t.Fatalf("合法时间 %q 未解析出时间值：%+v", raw, single.Versions)
		}
	}
}

// TestMinecraftVersionJSONOmitsRuntimeFlags 防的回归：
// json:"-" 标签被误改成普通 tag，运行期标记被回写到前端/缓存 JSON 里，
// 下次读缓存时"最新版"标记停留在过期版本上。
func TestMinecraftVersionJSONOmitsRuntimeFlags(t *testing.T) {
	encoded, err := json.Marshal(MinecraftVersion{
		ID:               "1.21.1",
		Type:             "release",
		IsLatestRelease:  true,
		IsLatestSnapshot: true,
	})
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	text := string(encoded)
	if strings.Contains(text, "IsLatest") {
		t.Fatalf("运行期标记不应出现在 JSON 里：%s", text)
	}
	if !strings.Contains(text, `"id":"1.21.1"`) || !strings.Contains(text, `"type":"release"`) {
		t.Fatalf("基础字段应正常序列化：%s", text)
	}
}
