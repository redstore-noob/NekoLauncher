package network

import (
	"strings"
	"testing"
)

func TestParseServerAddress(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		host    string
		port    int
		wantErr bool
	}{
		{name: "仅域名", input: "mc.example.com", host: "mc.example.com", port: 25565},
		{name: "域名带端口", input: "mc.example.com:25566", host: "mc.example.com", port: 25566},
		{name: "IPv4 带端口", input: "1.2.3.4:25565", host: "1.2.3.4", port: 25565},
		{name: "IPv6 方括号带端口", input: "[::1]:25565", host: "::1", port: 25565},
		{name: "裸 IPv6 不当端口解析", input: "::1", host: "::1", port: 25565},
		{name: "tcp 前缀", input: "tcp://mc.example.com:1234", host: "mc.example.com", port: 1234},
		{name: "端口越界时不拆分整串当主机名", input: "mc.example.com:99999", host: "mc.example.com:99999", port: 25565},
		{name: "空白地址", input: "   ", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseServerAddress(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际得到 %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外报错: %v", err)
			}
			if got.Host != tc.host || got.Port != tc.port {
				t.Fatalf("got %+v, want host=%s port=%d", got, tc.host, tc.port)
			}
		})
	}
}

func TestFlattenChatComponent(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  string
	}{
		{
			name:  "纯字符串",
			input: "hello",
			want:  "hello",
		},
		{
			name:  "字符串数组",
			input: []any{"a", "b"},
			want:  "ab",
		},
		{
			name: "命名颜色 + 粗体斜体",
			input: map[string]any{
				"text":   "标题",
				"color":  "gold",
				"bold":   true,
				"italic": true,
			},
			want: "§6§l§o标题",
		},
		{
			name: "hex 颜色转译为 Bungee 格式",
			input: map[string]any{
				"text":  "渐变",
				"color": "#FF55AA",
			},
			want: "§x§f§f§5§5§a§a渐变",
		},
		{
			name: "extra 子组件继承拼接",
			input: map[string]any{
				"text":  "父",
				"color": "red",
				"extra": []any{
					map[string]any{"text": "子", "color": "aqua", "underlined": true},
					"尾巴",
				},
			},
			want: "§c父§b§n子尾巴",
		},
		{
			name: "未知颜色被忽略",
			input: map[string]any{
				"text":  "文本",
				"color": "rainbow",
			},
			want: "文本",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := flattenChatComponent(tc.input)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsHexColor(t *testing.T) {
	valid := []string{"#000000", "#FFFFFF", "#ff55aA", "#AbC123"}
	for _, color := range valid {
		if !isHexColor(color) {
			t.Errorf("%s 应为合法 hex 颜色", color)
		}
	}
	invalid := []string{"#fff", "ffffff", "#GGGGGG", "red", "#12345", "#1234567", ""}
	for _, color := range invalid {
		if isHexColor(color) {
			t.Errorf("%s 不应为合法 hex 颜色", color)
		}
	}
}

func TestExtractDescriptionForms(t *testing.T) {
	cases := []struct {
		name string
		root map[string]any
		want string
	}{
		{
			name: "缺失 description",
			root: map[string]any{},
			want: "无 MOTD",
		},
		{
			name: "纯字符串（含遗留 § 码）",
			root: map[string]any{"description": "§a欢迎§r来到服务器"},
			want: "§a欢迎§r来到服务器",
		},
		{
			name: "text 组件 + 命名颜色",
			root: map[string]any{
				"description": map[string]any{"text": "公告", "color": "yellow"},
			},
			want: "§e公告",
		},
		{
			name: "尾部空白被裁剪",
			root: map[string]any{"description": "内容 \n"},
			want: "内容",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractDescription(tc.root)
			if !strings.Contains(got, tc.want) && got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
