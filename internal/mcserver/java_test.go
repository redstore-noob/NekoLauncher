package mcserver

import "testing"

func TestRequiredJavaMajorVersion(t *testing.T) {
	cases := []struct {
		mcVersion string
		want      int // -1 表示应返回 nil（未知，不校验）
	}{
		{"1.16.5", 8},
		{"1.17", 16},
		{"1.17.1", 16},
		{"1.18.2", 17},
		{"1.20.1", 17},
		{"1.20.4", 17},
		{"1.20.5", 21},
		{"1.20.6", 21},
		{"1.21", 21},
		{"1.21.4", 21},
		{"26.1", 25},
		{"27.2", 25},
		{"26.1-pre1", 25},
		{"1.21.4-rc1", 21},
		{"", -1},
		{"unknown", -1},
	}
	for _, c := range cases {
		got := RequiredJavaMajorVersion(c.mcVersion)
		if c.want < 0 {
			if got != nil {
				t.Errorf("RequiredJavaMajorVersion(%q) = %d，期望 nil", c.mcVersion, *got)
			}

			continue
		}
		if got == nil || *got != c.want {
			t.Errorf("RequiredJavaMajorVersion(%q) = %v，期望 %d", c.mcVersion, got, c.want)
		}
	}
}
