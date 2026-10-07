package info

import "testing"

func TestBuildVersionOverridesSourceVersion(t *testing.T) {
	previous := buildVersion
	buildVersion = "9.8.7-preview2"
	t.Cleanup(func() { buildVersion = previous })

	if got := Version(); got != "9.8.7-preview2" {
		t.Fatalf("Version() = %q，期望构建注入版本", got)
	}
}
