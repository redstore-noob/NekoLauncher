package launch

import (
	"strings"
	"testing"
)

// TestAppendLogEmitsLogLine 逐行日志回调：appendLog 每写一行就要回调一次，
// 且回调拿到的正是内存日志里那一条（带时间戳、已脱敏）——前端的增量日志窗口
// 依赖这个契约，否则只能退回 2s 全量轮询。
func TestAppendLogEmitsLogLine(t *testing.T) {
	service := NewGameLaunchService(nil)

	type captured struct{ tag, line string }
	var got []captured

	service.OnLogLine = func(tag, line string) {
		got = append(got, captured{tag: tag, line: line})
	}

	service.appendLog("游戏进程已启动", "LAUNCH")
	service.appendLog("Starting minecraft server version 1.21.4", "GAME")
	service.appendLog("   ", "GAME") // 空白行不入日志，也不应回调

	if len(got) != 2 {
		t.Fatalf("回调次数 = %d，期望 2（空白行应被忽略）：%+v", len(got), got)
	}
	if got[0].tag != "LAUNCH" || !strings.Contains(got[0].line, "游戏进程已启动") {
		t.Fatalf("第一条回调不符：%+v", got[0])
	}
	if got[1].tag != "GAME" || !strings.Contains(got[1].line, "Starting minecraft") {
		t.Fatalf("第二条回调不符：%+v", got[1])
	}
	// 回调内容必须与内存日志一致（含 [HH:MM:SS] 前缀）
	if !strings.HasPrefix(got[1].line, "[") || !strings.Contains(got[1].line, "] ") {
		t.Fatalf("回调内容缺少时间戳前缀：%q", got[1].line)
	}
	if lines := service.GetLogText(); !strings.Contains(lines, got[1].line) {
		t.Fatalf("回调内容与内存日志不一致：回调=%q 日志=%q", got[1].line, lines)
	}
}

// TestAppendLogRedactsSecrets 逐行推送也必须走脱敏（事件会到前端，不能让令牌漏出去）。
func TestAppendLogRedactsSecrets(t *testing.T) {
	service := NewGameLaunchService(nil)

	var pushed string
	service.OnLogLine = func(_, line string) { pushed = line }

	service.appendLog("--accessToken abcdefghijklmnop --version 1.21.4", "GAME")

	if strings.Contains(pushed, "abcdefghijklmnop") {
		t.Fatalf("访问令牌未脱敏：%q", pushed)
	}
	if !strings.Contains(pushed, "1.21.4") {
		t.Fatalf("脱敏误伤了正常内容：%q", pushed)
	}
}
