package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 新实例语言跟随系统：启动前检查实例游戏目录的 options.txt，若尚未写入
// lang 键（新实例 / 从未运行过的实例），按电脑系统语言写入；已有 lang 的
// 实例带着用户自己的语言启动，不做覆盖。失败只记日志，不阻断启动。

// optionsFileName Minecraft 选项文件名（key:value，每行一项）
const optionsFileName = "options.txt"

// syncInstanceLanguage 在启动前把新实例的游戏语言对齐到系统语言。
// 实例 gameDir 按版本隔离设置解析（与启动参数同一来源）。
func (s *GameLaunchService) syncInstanceLanguage(snap GameInstanceSnapshot, versionId string) {
	minecraftDir := strings.TrimSpace(snap.MinecraftDirectory)
	if minecraftDir == "" {
		return
	}
	gameDir := resolveIsolatedGameDirectory(minecraftDir, snap.SourcePath, versionId)
	if strings.TrimSpace(gameDir) == "" {
		gameDir = minecraftDir
	}

	code := systemMinecraftLanguage()
	if code == "" {
		s.appendLog("无法识别系统语言，游戏语言保持默认。", "LAUNCH")
		return
	}

	optionsPath := filepath.Join(gameDir, optionsFileName)
	content, err := os.ReadFile(optionsPath)
	if err != nil && !os.IsNotExist(err) {
		s.appendLog(fmt.Sprintf("读取 %s 失败，游戏语言保持默认：%v", optionsFileName, err), "LAUNCH")
		return
	}

	if hasOptionsKey(string(content), "lang") {
		// 实例已经跑过一次（MC 首次运行就会写入 lang）或用户设置过语言
		return
	}

	updated := upsertOptionsKey(string(content), "lang", code)
	if err := os.WriteFile(optionsPath, []byte(updated), 0o644); err != nil {
		s.appendLog(fmt.Sprintf("写入游戏语言失败：%v", err), "LAUNCH")
		return
	}
	s.appendLog(fmt.Sprintf("新实例：已将游戏语言设置为 %s（跟随系统语言）。", code), "LAUNCH")
}

// hasOptionsKey 判断 options.txt 内容里是否已有某个键。
func hasOptionsKey(content, key string) bool {
	prefix := key + ":"
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true
		}
	}
	return false
}

// upsertOptionsKey 写入（或替换）options.txt 的一个键，保留其余行原样。
func upsertOptionsKey(content, key, value string) string {
	entry := key + ":" + value
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), key+":") {
			lines[i] = entry

			return strings.Join(lines, "\n")
		}
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	return content + entry + "\n"
}
