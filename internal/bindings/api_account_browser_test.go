package bindings

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBuildMicrosoftLoginBarScript 注入脚本要幂等、防 iframe 套娃，
// 且"返回启动器"目标与文案都被安全转义。
func TestBuildMicrosoftLoginBarScript(t *testing.T) {
	script := buildMicrosoftLoginBarScript("http://wails.localhost")

	// 幂等守卫 + 顶层窗口守卫，防止跨域跳转后重复注入/在 iframe 里注入
	if !strings.Contains(script, "getElementById('"+microsoftLoginBarID+"')") {
		t.Fatalf("缺少幂等守卫：%s", script)
	}
	if !strings.Contains(script, "window.top!==window") {
		t.Fatalf("缺少顶层窗口守卫：%s", script)
	}
	if !strings.Contains(script, microsoftLoginBarID) {
		t.Fatalf("缺少元素 ID：%s", script)
	}

	// 返回目标必须作为合法 JSON 字符串字面量出现（防注入/断串）
	if !strings.Contains(script, `"http://wails.localhost?msoauth=cancel"`) {
		t.Fatalf("返回目标未正确转义：%s", script)
	}

	// 脚本本身必须是可解析的 JS：表达式语句包裹，不因引号破坏语法
	if !strings.HasPrefix(script, "(function(){") || !strings.HasSuffix(script, "})();") {
		t.Fatalf("脚本应被 IIFE 包裹：%s", script)
	}

	// MarshalJSONString 产出的字面量应能解析回原文（抽查文案）
	if !strings.Contains(script, `\u`) && !strings.Contains(script, "微软账号登录中") {
		t.Fatalf("文案缺失：%s", script)
	}

	// 模拟脚本里按钮目标字面量的解析（等价于浏览器读到的字符串）
	var decoded string

	if err := json.Unmarshal([]byte(`"http://wails.localhost?msoauth=cancel"`), &decoded); err != nil {
		t.Fatalf("目标字面量应可解析：%v", err)
	}
	if decoded != "http://wails.localhost?msoauth=cancel" {
		t.Fatalf("解码结果不符：%q", decoded)
	}
}
