//go:build linux

package monitoring

import "testing"

func TestParseNvidiaSmuUtilization(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want float64
		ok   bool
	}{
		{"单卡", "37 \n", 37, true},
		{"多卡取最大", "12\n64\n", 64, true},
		{"N/A 行忽略", " 0 \n[N/A]\n", 0, true},
		{"全无效", "[N/A]\nnot-a-number\n", 0, false},
		{"空输出", "\n \n", 0, false},
		{"越界忽略", "150\n-3\n42\n", 42, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseNvidiaSmuUtilization(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("parseNvidiaSmuUtilization(%q) = (%v, %v), want (%v, %v)",
					tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}
