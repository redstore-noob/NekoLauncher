package mcserver

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// 玩家与权限管理：白名单 / OP / 封禁名单的结构化读写 + 在线玩家与广播。
//
// 设计取舍（和 HMCL、PCL 一致的做法）：
//   - **服务器在运行时优先用指令**（whitelist add/remove、op/deop、ban/pardon、kick、say）。
//     由服务端自己决定 UUID，正版/离线/皮肤站都能对得上；启动器直接改 JSON 反而容易写错。
//   - **服务器已停止时**才直接改 JSON 文件；此时离线模式下按 Minecraft 的离线 UUID 规则
//     （MD5("OfflinePlayer:"+name) + 版本/变体位）补 UUID，正版服务器请启动后用指令添加。
//
// 四个文件都是 JSON 数组，缺文件按空列表处理（服务端首次运行才会写）。
const (
	whitelistFileName = "whitelist.json"
	opsFileName       = "ops.json"
	bannedFileName    = "banned-players.json"
)

// 名字校验与服务端一致：1–16 位字母/数字/下划线。
var playerNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,16}$`)

// WhitelistEntry whitelist.json 条目。
type WhitelistEntry struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// OpEntry ops.json 条目。
type OpEntry struct {
	UUID                string `json:"uuid"`
	Name                string `json:"name"`
	Level               int    `json:"level"`
	BypassesPlayerLimit bool   `json:"bypassesPlayerLimit"`
}

// BannedPlayerEntry banned-players.json 条目。
type BannedPlayerEntry struct {
	UUID    string `json:"uuid"`
	Name    string `json:"name"`
	Created string `json:"created"`
	Source  string `json:"source"`
	Expires string `json:"expires"`
	Reason  string `json:"reason"`
}

// ServerPlayers 一个服务器的玩家与权限全貌（前端一个面板展示）。
type ServerPlayers struct {
	// Online 在线玩家名（服务器运行时经 RCON list 解析；未运行/未启用 RCON 时为空）
	Online []string `json:"Online"`
	// WhitelistEnabled server.properties 的 white-list
	WhitelistEnabled bool                `json:"WhitelistEnabled"`
	Whitelist        []WhitelistEntry    `json:"Whitelist"`
	Ops              []OpEntry           `json:"Ops"`
	Banned           []BannedPlayerEntry `json:"Banned"`
	// Running 服务器是否在运行（前端据此决定"用指令"还是"改文件"的提示）
	Running bool `json:"Running"`
}

// serverPropertyValues 读取 server.properties 的键值对（文件缺失返回空）。
func serverPropertyValues(dir string) []Property {
	lines, err := loadPropertyLines(dir)
	if err != nil {
		return nil
	}
	result := make([]Property, 0, len(lines))
	for _, line := range lines {
		if line.key != "" {
			result = append(result, Property{Key: line.key, Value: line.value})
		}
	}

	return result
}

// GetServerPlayers 汇总玩家与权限信息。
func GetServerPlayers(id string) ServerPlayers {
	if err := validateID(id); err != nil {
		return ServerPlayers{
			Online:    []string{},
			Whitelist: []WhitelistEntry{},
			Ops:       []OpEntry{},
			Banned:    []BannedPlayerEntry{},
		}
	}
	dir := serverDirectory(id)
	result := ServerPlayers{
		Online:    onlinePlayerNames(id),
		Whitelist: readPlayersFile[WhitelistEntry](filepath.Join(dir, whitelistFileName)),
		Ops:       readPlayersFile[OpEntry](filepath.Join(dir, opsFileName)),
		Banned:    readPlayersFile[BannedPlayerEntry](filepath.Join(dir, bannedFileName)),
		Running:   Default().IsRunning(id),
	}
	for _, line := range serverPropertyValues(dir) {
		if line.Key == "white-list" {
			result.WhitelistEnabled = strings.EqualFold(line.Value, "true")
		}
	}
	sort.Strings(result.Online)

	return result
}

// SaveServerWhitelist 覆盖写入白名单（服务器必须已停止）。
func SaveServerWhitelist(id string, entries []WhitelistEntry) error {
	return writePlayersEntries(id, whitelistFileName, normalizeWhitelist(entries))
}

// SaveServerOps 覆盖写入 OP 列表（服务器必须已停止）。
func SaveServerOps(id string, entries []OpEntry) error {
	normalized := make([]OpEntry, 0, len(entries))
	for _, entry := range entries {
		name, err := normalizePlayerName(entry.Name)
		if err != nil {
			return err
		}
		level := entry.Level
		if level < 1 {
			level = 4
		}
		if level > 4 {
			level = 4
		}
		normalized = append(normalized, OpEntry{
			UUID:                resolveEntryUUID(entry.UUID, name),
			Name:                name,
			Level:               level,
			BypassesPlayerLimit: entry.BypassesPlayerLimit,
		})
	}

	return writePlayersEntries(id, opsFileName, normalized)
}

// SaveServerBanned 覆盖写入封禁名单（服务器必须已停止）。
func SaveServerBanned(id string, entries []BannedPlayerEntry) error {
	normalized := make([]BannedPlayerEntry, 0, len(entries))
	for _, entry := range entries {
		name, err := normalizePlayerName(entry.Name)
		if err != nil {
			return err
		}
		if strings.TrimSpace(entry.Reason) == "" {
			entry.Reason = "Banned by an operator."
		}
		if strings.TrimSpace(entry.Created) == "" {
			entry.Created = "1970-01-01 00:00:00 +0000"
		}
		if strings.TrimSpace(entry.Source) == "" {
			entry.Source = "NekoLauncher"
		}
		if strings.TrimSpace(entry.Expires) == "" {
			entry.Expires = "forever"
		}
		entry.Name = name
		entry.UUID = resolveEntryUUID(entry.UUID, name)
		normalized = append(normalized, entry)
	}

	return writePlayersEntries(id, bannedFileName, normalized)
}

// SetWhitelistEnabled 开关白名单（写 server.properties；服务器运行中会被拒绝）。
func SetWhitelistEnabled(id string, enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}

	return SetServerProperties(id, []Property{{Key: "white-list", Value: value}})
}

// RunPlayerCommand 在运行中的服务器上执行一条玩家/权限指令
// （whitelist add、op、ban、kick、say…）。RCON 优先，未启用时退回控制台 stdin。
func RunPlayerCommand(id, command string) error {
	if strings.TrimSpace(command) == "" {
		return errors.New("指令为空")
	}
	if !Default().IsRunning(id) {
		return errors.New("服务器未在运行；请先启动，或直接编辑下方的名单")
	}

	return sendServerCommandAny(id, command)
}

// onlinePlayerNames 从 RCON 的 list 响应里取在线玩家名。
// 响应形如 "There are 2 of a max of 20 players online: A, B"；无人时冒号后为空。
func onlinePlayerNames(id string) []string {
	state := Default().state(id)
	if state == nil {
		return []string{}
	}
	state.mu.Lock()
	running := state.status == StatusRunning
	rcon := state.rcon
	state.mu.Unlock()

	if !running || !rcon.Enabled {
		return []string{}
	}

	response, err := runRCONCommand(context.Background(), rconAddress(rcon.Port), rcon.Password, "list")
	if err != nil {
		return []string{}
	}
	_, names := parsePlayerListNames(response)

	return names
}

// parsePlayerListNames 解析 list 响应里的人名列表。
func parsePlayerListNames(response string) (int, []string) {
	matches := listResponseRe.FindStringSubmatch(response)
	if matches == nil {
		return 0, []string{}
	}
	count := 0
	if _, err := fmt.Sscanf(matches[1], "%d", &count); err != nil {
		return 0, []string{}
	}

	names := []string{}
	if index := strings.Index(response, ":"); index >= 0 {
		for _, name := range strings.Split(response[index+1:], ",") {
			trimmed := strings.TrimSpace(name)
			if trimmed != "" {
				names = append(names, trimmed)
			}
		}
	}

	return count, names
}

// ---- JSON 读写 ----

// readPlayersFile 读一个玩家名单文件；文件缺失/内容损坏时返回空列表
// （名单文件是服务端自己写的，坏了也不该让整个页面打不开）。
func readPlayersFile[T any](path string) []T {
	data, err := os.ReadFile(path)
	if err != nil {
		return []T{}
	}
	var entries []T
	if err := json.Unmarshal(data, &entries); err != nil {
		return []T{}
	}
	if entries == nil {
		return []T{}
	}

	return entries
}

// writePlayersEntries 覆盖写入名单文件（服务器必须已停止）。
func writePlayersEntries[T any](id, fileName string, entries []T) error {
	if err := validateID(id); err != nil {
		return err
	}
	if Default().IsRunning(id) {
		return errors.New("服务器运行中：请用指令修改（启动器会自动改走 RCON / 控制台），停止后再改文件")
	}
	payload, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	return os.WriteFile(filepath.Join(serverDirectory(id), fileName), payload, 0o644)
}

// normalizeWhitelist 校验并补全白名单条目。
func normalizeWhitelist(entries []WhitelistEntry) []WhitelistEntry {
	normalized := make([]WhitelistEntry, 0, len(entries))
	for _, entry := range entries {
		name, err := normalizePlayerName(entry.Name)
		if err != nil {
			continue
		}
		normalized = append(normalized, WhitelistEntry{
			UUID: resolveEntryUUID(entry.UUID, name),
			Name: name,
		})
	}

	return normalized
}

// normalizePlayerName 校验玩家名（服务端只接受 1–16 位字母/数字/下划线）。
func normalizePlayerName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if !playerNamePattern.MatchString(trimmed) {
		return "", fmt.Errorf("玩家名不合法：%q（1–16 位字母、数字或下划线）", name)
	}

	return trimmed, nil
}

// resolveEntryUUID 有合法 UUID 就用它，否则按离线规则补一个。
// 两种形态（32 位无连字符 / 36 位带连字符）统一规范化成带连字符的小写形式——
// 服务端读名单时按 Java 的 UUID.fromString 解析，带连字符才是稳妥写法。
func resolveEntryUUID(uuid, name string) string {
	trimmed := strings.ToLower(strings.TrimSpace(uuid))
	if isUUIDLike(trimmed) {
		compact := strings.ReplaceAll(trimmed, "-", "")

		return strings.Join([]string{
			compact[0:8], compact[8:12], compact[12:16], compact[16:20], compact[20:32],
		}, "-")
	}

	return OfflinePlayerUUID(name)
}

// isUUIDLike 32 位无连字符或 36 位带连字符的十六进制。
func isUUIDLike(value string) bool {
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 {
		return false
	}
	_, err := hex.DecodeString(compact)

	return err == nil
}

// OfflinePlayerUUID 离线模式 UUID：MD5("OfflinePlayer:"+name)，
// 按 RFC 4122 设置版本位（3）与变体位，输出带连字符的小写形式。
//
// 与 Java 的 UUID.nameUUIDFromBytes 完全一致——离线服务器认的就是这个值。
func OfflinePlayerUUID(name string) string {
	sum := md5.Sum([]byte("OfflinePlayer:" + name))
	bytes := sum[:]
	bytes[6] = (bytes[6] & 0x0f) | 0x30
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes)

	return strings.Join([]string{
		encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32],
	}, "-")
}
