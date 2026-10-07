package info

import "testing"

func TestBuildKindControlsUpdates(t *testing.T) {
	previous := buildKindOverride
	t.Cleanup(func() { buildKindOverride = previous })

	cases := []struct {
		kind string
		want bool
	}{
		{"official", true},
		{"official-preview", true},
		{"internal", false},
		{"unknown", false},
		{"", BuildKind == "official" || BuildKind == "official-preview"},
	}
	for _, item := range cases {
		t.Run(item.kind, func(t *testing.T) {
			buildKindOverride = item.kind
			if got := UpdatesEnabled(); got != item.want {
				t.Fatalf("构建类型 %q：UpdatesEnabled() = %v，期望 %v", item.kind, got, item.want)
			}
		})
	}
}
