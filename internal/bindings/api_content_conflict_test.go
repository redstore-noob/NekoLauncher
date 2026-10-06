package bindings

import "testing"

// normalizeLoaderName 把实例详情里的展示名收敛成检测侧的加载器 id。
// 这条映射错了会让整个"加载器不匹配"检测失效或误报，必须锁住。
func TestNormalizeLoaderName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", "vanilla"},
		{"   ", "vanilla"},
		{"原版", "vanilla"},
		{"Vanilla", "vanilla"},
		{"Fabric", "fabric"},
		{"Fabric 0.15.11", "fabric"},
		{"Forge", "forge"},
		{"Forge 47.2.0", "forge"},
		// NeoForge 必须先于 Forge 匹配：它名字里含 "forge"，
		// 顺序反了会把所有 NeoForge 实例误判成 Forge
		{"NeoForge", "neoforge"},
		{"NeoForge 20.4.1", "neoforge"},
		{"Quilt", "quilt"},
		// 认不出来：返回空串让检测侧跳过，而不是猜一个
		{"Rift", ""},
		{"LiteLoader", ""},
	}
	for _, testCase := range cases {
		if got := normalizeLoaderName(testCase.input); got != testCase.want {
			t.Errorf("normalizeLoaderName(%q) = %q，期望 %q",
				testCase.input, got, testCase.want)
		}
	}
}

// NeoForge 与 Forge 的判定顺序是这里唯一的真实陷阱，单独锁一条。
func TestNormalizeLoaderNamePrefersNeoForge(t *testing.T) {
	if got := normalizeLoaderName("NeoForge"); got == "forge" {
		t.Fatal("NeoForge 被误判成 Forge：NeoForge 实例的 mod 会被报成加载器不匹配")
	}
}
