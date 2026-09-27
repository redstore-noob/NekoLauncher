package online

import "strings"

// 陶瓦联机的房间码形如 `U/YNZE-U61D-2206-HXRG`（Scaffolding 协议）：
// 前缀 U/ 之后是 4 段 4 位，字符集为 0-9 加除 I、O 外的大写字母。
// 用户粘贴时可能带前缀也可能不带，还可能混入空格或全角字符，
// 这里只做"收敛 + 候选生成"，合法性最终由陶瓦进程判定（返回 400 就换下一种）。
const (
	// roomCodePrefix Scaffolding 协议里的房间码前缀
	roomCodePrefix = "U/"
	// roomCodeAlphabet 房间码字符集（去掉易混的 I、O）
	roomCodeAlphabet = "0123456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	// roomCodeBodyLength 去掉前缀后的位数
	roomCodeBodyLength = 16
	// roomCodeGroup 每段位数（展示用连字符分组）
	roomCodeGroup = 4
)

// sanitizeRoomCode 只保留字母数字：丢掉连字符、斜杠、空白与全角符号，
// 并统一转大写。返回值可能包含协议前缀里的那个 U。
func sanitizeRoomCode(raw string) string {
	var builder strings.Builder

	for _, char := range strings.ToUpper(strings.TrimSpace(raw)) {
		switch {
		case char >= '0' && char <= '9', char >= 'A' && char <= 'Z':
			builder.WriteRune(char)
		default:
			// 分隔符（- / _ 空白 全角符号）一律丢弃，下面按固定位数重新分组
		}
	}

	return builder.String()
}

// RoomCodeBody 返回去掉前缀后的 16 位房间码主体；位数不符时返回空串。
func RoomCodeBody(raw string) string {
	body := sanitizeRoomCode(raw)
	// "U/" 被 sanitize 掉斜杠后剩下一个前导 U，需要单独剥掉
	if len(body) == roomCodeBodyLength+1 && strings.HasPrefix(body, "U") {
		body = body[1:]
	}
	if len(body) != roomCodeBodyLength {
		return ""
	}

	return body
}

// FormatRoomCode 把房间码按 4 位一段重新分组（`XXXXXXXXXXXXXXXX` → `XXXX-XXXX-XXXX-XXXX`）。
func FormatRoomCode(body string) string {
	if len(body) != roomCodeBodyLength {
		return body
	}

	groups := make([]string, 0, roomCodeBodyLength/roomCodeGroup)
	for index := 0; index < roomCodeBodyLength; index += roomCodeGroup {
		groups = append(groups, body[index:index+roomCodeGroup])
	}

	return strings.Join(groups, "-")
}

// RoomCodeLooksValid 粗校验房间码：位数正确且字符都在协议字符集内。
// 真正的合法性（末段校验位）由陶瓦进程判定，这里只挡掉明显的手误。
func RoomCodeLooksValid(raw string) bool {
	body := RoomCodeBody(raw)
	if body == "" {
		return false
	}

	for _, char := range body {
		if !strings.ContainsRune(roomCodeAlphabet, char) {
			return false
		}
	}

	return true
}

// RoomCodeVariants 返回按可信度排序的候选房间码，进房时逐个尝试：
// 陶瓦进程对前缀的接受范围不做假设（带 U/ 与不带各试一次），
// 最后兜底用用户原始输入（进程可能接受别的历史形态）。
func RoomCodeVariants(raw string) []string {
	body := RoomCodeBody(raw)
	if body == "" {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil
		}

		return []string{trimmed}
	}

	formatted := FormatRoomCode(body)

	return []string{roomCodePrefix + formatted, formatted}
}

// FormatRoomCodeForDisplay 把用户输入或进程回传的房间码整理成展示形态。
// 无法解析时原样返回，避免把进程的新形态改坏。
func FormatRoomCodeForDisplay(raw string) string {
	body := RoomCodeBody(raw)
	if body == "" {
		return strings.TrimSpace(raw)
	}

	return roomCodePrefix + FormatRoomCode(body)
}
