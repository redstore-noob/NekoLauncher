package tools

import (
	"path/filepath"
	"testing"
)

func TestSanitizeSaveName(t *testing.T) {
	cases := []struct{ input, expected string }{
		{"整合包", "整合包"},
		{"", ""},
		{"为什么?.mrpack", "为什么0.mrpack"},
		{"a?b?c", "a0b0c"},
		{"??", "00"},
		// 全角问号是合法文件名字符，不该动
		{"为什么？.mrpack", "为什么？.mrpack"},
		// 其他非法字符不归这里管（各自入口另行校验）
		{"a<b>.zip", "a<b>.zip"},
	}
	for _, item := range cases {
		if got := SanitizeSaveName(item.input); got != item.expected {
			t.Errorf("SanitizeSaveName(%q) = %q，期望 %q", item.input, got, item.expected)
		}
	}
}

func TestSanitizeSavePathKeepsDirectory(t *testing.T) {
	separator := string(filepath.Separator)
	cases := []struct{ input, expected string }{
		{"C:" + separator + "导出" + separator + "整合包?.mrpack", "C:" + separator + "导出" + separator + "整合包0.mrpack"},
		{"整合包?.zip", "整合包0.zip"},
		// 目录部分的 '?' 原样保留：其他平台允许，改了会指向不存在的目录
		{"C:" + separator + "a?b" + separator + "包?.zip", "C:" + separator + "a?b" + separator + "包0.zip"},
		// 以分隔符结尾的路径是目录，不改
		{"C:" + separator + "a?b" + separator, "C:" + separator + "a?b" + separator},
		{"", ""},
		{"没有问号.zip", "没有问号.zip"},
	}
	for _, item := range cases {
		if got := SanitizeSavePath(item.input); got != item.expected {
			t.Errorf("SanitizeSavePath(%q) = %q，期望 %q", item.input, got, item.expected)
		}
	}
}
