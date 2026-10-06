package tools

import (
	"path/filepath"
	"strings"
)

// questionMarkReplacement '?' 的替换字符。Windows 文件名不允许 '?'，
// 用户输入的整合包名/版本号、服务器名、插件名、歌曲名等带上它时，
// 保存对话框会拒绝这个名字（表现为"点了保存却什么都没存下来"）。
const questionMarkReplacement = "0"

// SanitizeSaveName 保存文件名兜底：把 '?' 换成 '0'。
//
// 只处理 '?'：换成 '0' 后长度不变、位置不变，作者与玩家都能把文件名
// 对回原来的名字。其余字符保持原样，交给各自的既有校验——'?' 之外的
// 非法字符由保存对话框本身拦下，不会写坏文件。
func SanitizeSaveName(name string) string {
	if !strings.Contains(name, "?") {
		return name
	}
	return strings.ReplaceAll(name, "?", questionMarkReplacement)
}

// SanitizeSavePath 对保存路径做同样的兜底，只改文件名部分：目录部分原样
// 保留。Windows 上目录名本来就不可能有 '?'，而其他平台允许——把目录里的
// '?' 也换掉只会指向一个不存在的目录，反而存不下来。
func SanitizeSavePath(path string) string {
	if !strings.Contains(path, "?") {
		return path
	}
	directory, name := filepath.Split(path)
	if name == "" {
		return path // 以分隔符结尾：这是目录，不改
	}
	return directory + SanitizeSaveName(name)
}
