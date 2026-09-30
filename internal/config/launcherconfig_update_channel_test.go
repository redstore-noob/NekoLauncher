package config

import "testing"

// 通道默认值：未写过任何偏好时回落预览通道（项目以 preview 发版为主）。
func TestUpdateChannelDefaultsToPreview(t *testing.T) {
	newTestStorage(t)

	if got := UpdateChannel(); got != UpdateChannelPreview {
		t.Fatalf("默认通道 = %q, 期望 %q", got, UpdateChannelPreview)
	}
}

// 老版布尔偏好迁移：launcherAutoUpdateIncludePrerelease=false → 稳定通道；
// 未设置/true → 预览通道；写了新键 launcherUpdateChannel 时新键优先。
func TestUpdateChannelMigratesLegacyBool(t *testing.T) {
	newTestStorage(t)

	if !SetValue("launcherAutoUpdateIncludePrerelease", "False") {
		t.Fatal("写入旧偏好失败")
	}
	if got := UpdateChannel(); got != UpdateChannelStable {
		t.Fatalf("旧偏好 false 应迁移为 stable，得到 %q", got)
	}

	// 新键优先于旧键
	SaveUpdateChannel(UpdateChannelPreview)
	if got := UpdateChannel(); got != UpdateChannelPreview {
		t.Fatalf("新键应优先，得到 %q", got)
	}
}

// 通道值清洗：未知/空值回落预览，大小写不敏感。
func TestNormalizeUpdateChannel(t *testing.T) {
	cases := map[string]string{
		"stable":       UpdateChannelStable,
		"STABLE":       UpdateChannelStable,
		"preview":      UpdateChannelPreview,
		"":             UpdateChannelPreview,
		"nightly":      UpdateChannelPreview,
		"  stable  \t": UpdateChannelStable,
	}
	for input, want := range cases {
		if got := NormalizeUpdateChannel(input); got != want {
			t.Errorf("NormalizeUpdateChannel(%q) = %q, 期望 %q", input, got, want)
		}
	}
}

// 保存后能读回；无效值写入时被洗成合法通道而不是原样落盘。
func TestSaveUpdateChannelRoundTrip(t *testing.T) {
	newTestStorage(t)

	SaveUpdateChannel(UpdateChannelStable)
	if got := UpdateChannel(); got != UpdateChannelStable {
		t.Fatalf("保存 stable 后读回 %q", got)
	}
	SaveUpdateChannel("whatever")
	if got := UpdateChannel(); got != UpdateChannelPreview {
		t.Fatalf("保存非法值后应回落 preview，读回 %q", got)
	}
}
