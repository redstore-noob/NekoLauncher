// Package network Minecraft 服务器状态查询。
// 移植自 NyaLauncher.Core/Network/MinecraftServerPinger.cs。
package network

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"nekolauncher/internal/config"
)

// MinecraftServerStatus 服务器状态查询结果（Minecraft Server List Ping 协议）。
// JSON 字段名与 C# record 属性一致（PascalCase）。
type MinecraftServerStatus struct {
	// Motd 展开为纯文本（含 § 样式码）的服务器描述。
	Motd string `json:"Motd"`
	// VersionName 服务端版本名（可能为空）。
	VersionName string `json:"VersionName"`
	// ProtocolVersion 服务端协议版本号（未知为 0）。
	ProtocolVersion int `json:"ProtocolVersion"`
	// OnlinePlayers 在线玩家数。
	OnlinePlayers int `json:"OnlinePlayers"`
	// MaxPlayers 最大玩家数。
	MaxPlayers int `json:"MaxPlayers"`
	// IconPath 服务器图标缓存文件路径（无图标或缓存失败为空）。
	IconPath string `json:"IconPath"`
}

// MinecraftServerPinger 原版 Minecraft 服务器状态查询（Server List Ping）：
// TCP 连接 → 握手包（nextState=1）→ 状态请求 → 读取 JSON 状态响应。
// 仅依赖原版协议，不额外引入依赖。
type MinecraftServerPinger struct{}

// handshakeProtocolCandidates 跨版本服务器（ViaVersion 等）可能拒绝特定协议的
// 握手，依次尝试代表性协议直至成功：
// 47=1.8.9（大多数服务器兼容）、767=1.21、763=1.20.1、340=1.12.2
var handshakeProtocolCandidates = []int{47, 767, 763, 340}

const (
	defaultTimeout = 6 * time.Second
	overallTimeout = 10 * time.Second
	defaultPort    = 25565

	// maxPacketLength 数据包长度上限（协议规定 2^23-1）。
	maxPacketLength = 2_097_151
	// maxStringLength 状态响应字符串长度上限。
	maxStringLength = 1_048_576
	// maximumCachedServerIcons 图标缓存条目上限；超过后按最后写入时间淘汰最旧的。
	maximumCachedServerIcons = 200
)

// ParseAddress 解析 "host" / "host:port" / "[ipv6]:port" 形式的服务器地址。
// 无端口时使用默认端口 25565。非法端口返回错误。
func ParseAddress(input string) (string, int, error) {
	if strings.TrimSpace(input) == "" {
		return "", 0, errors.New("服务器地址不能为空")
	}

	address := strings.TrimSpace(input)
	if len(address) >= 6 && strings.EqualFold(address[:6], "tcp://") {
		address = address[6:]
	}

	// [ipv6]:port
	if strings.HasPrefix(address, "[") {
		closing := strings.IndexByte(address, ']')
		if closing > 0 {
			host := address[1:closing]
			port := defaultPort
			if closing+1 < len(address) && address[closing+1] == ':' {
				var err error
				port, err = parsePort(address[closing+2:])
				if err != nil {
					return "", 0, err
				}
			}
			return host, port, nil
		}
	}

	// host:port（仅一个冒号时按 IPv4/域名处理；多个冒号视为裸 IPv6）
	firstColon := strings.IndexByte(address, ':')
	lastColon := strings.LastIndexByte(address, ':')
	if firstColon > 0 && firstColon == lastColon {
		if port, err := strconv.Atoi(address[firstColon+1:]); err == nil && port > 0 && port <= 65535 {
			return address[:firstColon], port, nil
		}
	}

	return address, defaultPort, nil
}

// ServerAddress 解析后的服务器地址。
//
// 供 Wails 绑定层使用：桥只支持 0/1/2 个返回值（第 2 个为 error），
// 直接绑定 (string, int, error) 会让前端拿到 null 并吞掉错误，
// 故以结构体承载 host + port。
type ServerAddress struct {
	// Host 主机名或 IP（IPv6 已去掉方括号）。
	Host string `json:"Host"`
	// Port 端口，缺省为 25565。
	Port int `json:"Port"`
}

// ParseServerAddress 解析服务器地址，语义与 ParseAddress 完全一致。
func ParseServerAddress(input string) (ServerAddress, error) {
	host, port, err := ParseAddress(input)
	if err != nil {
		return ServerAddress{}, err
	}
	return ServerAddress{Host: host, Port: port}, nil
}

// Ping 查询服务器状态；失败返回 error（调用方决定降级展示）。
// ctx 用于取消（对应 C# 的 CancellationToken）。
func (MinecraftServerPinger) Ping(ctx context.Context, host string, port int) (MinecraftServerStatus, error) {
	// 总超时（对应 C# OverallTimeout = 10s）
	ctx, cancel := context.WithTimeout(ctx, overallTimeout)
	defer cancel()

	var lastError error
	for _, protocol := range handshakeProtocolCandidates {
		status, err := pingOnce(ctx, host, port, protocol)
		if err == nil {
			return status, nil
		}
		if ctx.Err() == context.Canceled {
			// 调用方主动取消：直接返回
			return MinecraftServerStatus{}, ctx.Err()
		}
		if ctx.Err() == context.DeadlineExceeded {
			// 总超时耗尽：不再重试，抛出可读的超时错误
			return MinecraftServerStatus{}, fmt.Errorf("连接服务器超时，请检查地址或稍后重试（%w）", err)
		}
		lastError = err
	}

	if lastError != nil {
		return MinecraftServerStatus{}, lastError
	}
	return MinecraftServerStatus{}, errors.New("无法连接服务器")
}

// Ping 使用默认后台 context 查询。
func Ping(host string, port int) (MinecraftServerStatus, error) {
	return MinecraftServerPinger{}.Ping(context.Background(), host, port)
}

// pingOnce 单次尝试（单次超时对应 C# DefaultTimeout = 6s）。
func pingOnce(ctx context.Context, host string, port, protocolVersion int) (MinecraftServerStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return MinecraftServerStatus{}, err
	}
	defer conn.Close()

	// 设置读写截止时间，保证读包阶段也受超时约束
	_ = conn.SetDeadline(deadlineOf(ctx))

	// 握手包：packetId=0x00 + 协议版本 + 主机名 + 端口 + nextState=1
	var handshake bytesBuffer
	writeVarint(&handshake, 0x00)
	writeVarint(&handshake, protocolVersion)
	writeString(&handshake, host)
	writeUnsignedShort(&handshake, uint16(port))
	writeVarint(&handshake, 1)
	if err := writePacket(conn, handshake.bytes()); err != nil {
		return MinecraftServerStatus{}, err
	}

	// 状态请求：仅 packetId=0x00
	if err := writePacket(conn, []byte{0x00}); err != nil {
		return MinecraftServerStatus{}, err
	}

	payload, err := readPacket(conn)
	if err != nil {
		return MinecraftServerStatus{}, err
	}
	if len(payload) == 0 || payload[0] != 0x00 {
		return MinecraftServerStatus{}, errors.New("服务器返回了意外的状态响应")
	}

	reader := bytesBuffer(payload[1:])
	jsonText, err := readString(&reader)
	if err != nil {
		return MinecraftServerStatus{}, err
	}

	var root map[string]any
	if err := json.Unmarshal([]byte(jsonText), &root); err != nil {
		return MinecraftServerStatus{}, fmt.Errorf("状态响应 JSON 解析失败: %w", err)
	}
	return parseStatus(root, host, port), nil
}

// deadlineOf 返回 ctx 的截止时间；无截止时取较远的兜底时间。
func deadlineOf(ctx context.Context) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	return time.Now().Add(defaultTimeout)
}

func parseStatus(root map[string]any, host string, port int) MinecraftServerStatus {
	motd := extractDescription(root)

	versionName := ""
	protocol := 0
	if version, ok := root["version"].(map[string]any); ok {
		if name, ok := version["name"].(string); ok {
			versionName = name
		}
		if v, ok := version["protocol"].(float64); ok {
			protocol = int(v)
		}
	}

	online, maxPlayers := 0, 0
	if players, ok := root["players"].(map[string]any); ok {
		if v, ok := players["online"].(float64); ok {
			online = int(v)
		}
		if v, ok := players["max"].(float64); ok {
			maxPlayers = int(v)
		}
	}

	return MinecraftServerStatus{
		Motd:            motd,
		VersionName:     versionName,
		ProtocolVersion: protocol,
		OnlinePlayers:   online,
		MaxPlayers:      maxPlayers,
		IconPath:        tryCacheFavicon(root, host, port),
	}
}

// tryCacheFavicon 解析状态响应中的 favicon 字段（"data:image/png;base64,…"），
// 解码后缓存为本地 PNG 并返回其路径；字段缺失、格式非法或磁盘写入失败返回空串。
// 缓存目录为 <存储目录>/cache/server-icons（对应 C# LauncherConfig.StorageDirectory）。
func tryCacheFavicon(root map[string]any, host string, port int) string {
	defer func() {
		_ = recover() // 防御性：缓存失败不影响状态解析
	}()

	dataURI, ok := root["favicon"].(string)
	if !ok {
		return ""
	}

	const prefix = "data:image/png;base64,"
	if len(dataURI) <= len(prefix) || !strings.EqualFold(dataURI[:len(prefix)], prefix) {
		return ""
	}

	bytes, err := base64.StdEncoding.DecodeString(dataURI[len(prefix):])
	if err != nil || len(bytes) == 0 || len(bytes) > 128*1024 {
		return ""
	}

	directory := filepath.Join(StorageDirectory(), "cache", "server-icons")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return ""
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", host, port)))
	path := filepath.Join(directory, strings.ToLower(hex.EncodeToString(hash[:8]))+".png")
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		return ""
	}
	pruneIconCache(directory)
	return path
}

// StorageDirectory 返回启动器存储目录（服务器图标缓存落在此目录下）。
// 转发到 config.StorageDirectory，与插件、皮肤、快照等其余缓存保持同一处：
// 此前这里硬编码 ~/.nyalauncher，用户改了存储目录后图标仍写到旧位置。
func StorageDirectory() string { return config.StorageDirectory() }

// pruneIconCache 按最后写入时间修剪服务器图标缓存：保留最新的 200 个。
func pruneIconCache(directory string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return // 修剪失败不影响本次缓存写入
	}
	type iconFile struct {
		path    string
		modTime time.Time
	}
	var files []iconFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".png") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, iconFile{filepath.Join(directory, entry.Name()), info.ModTime()})
	}
	if len(files) <= maximumCachedServerIcons {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
	for _, file := range files[maximumCachedServerIcons:] {
		_ = os.Remove(file.path)
	}
}

// extractDescription 解析 description 字段。
// 兼容三种历史形态：纯字符串、{"text": ...}、含 extra 数组的聊天组件树。
// 递归展开为纯文本；JSON 组件的 color/bold 等样式属性转译为 § 样式码保留。
func extractDescription(root map[string]any) string {
	description, ok := root["description"]
	if !ok {
		return "无 MOTD"
	}
	return strings.TrimRight(flattenChatComponent(description), " \t\n\r")
}

func flattenChatComponent(element any) string {
	switch value := element.(type) {
	case string:
		// 历史形态：字符串本身可能已含 § 样式码
		return value
	case []any:
		var builder strings.Builder
		for _, child := range value {
			builder.WriteString(flattenChatComponent(child))
		}
		return builder.String()
	case map[string]any:
		var builder strings.Builder
		appendStylePrefix(&builder, value)
		if text, ok := value["text"].(string); ok {
			builder.WriteString(text)
		}
		if extra, ok := value["extra"].([]any); ok {
			for _, child := range extra {
				builder.WriteString(flattenChatComponent(child))
			}
		}
		return builder.String()
	default:
		return ""
	}
}

// appendStylePrefix 把 JSON 聊天组件的样式属性转译为 § 样式码前缀。
func appendStylePrefix(builder *strings.Builder, element map[string]any) {
	if color, ok := element["color"].(string); ok {
		switch {
		case isHexColor(color):
			// 原版 Bungee/Spigot 的 hex 表示：§x 后跟 6 个 §<hex位>。
			// 前端渲染器据此还原精确颜色（现代服务器渐变 MOTD 依赖此格式）。
			builder.WriteString("§x")
			for _, digit := range strings.ToLower(strings.TrimPrefix(color, "#")) {
				builder.WriteString("§")
				builder.WriteRune(digit)
			}
		default:
			if code, ok := minecraftTextColorCode(color); ok {
				builder.WriteString("§")
				builder.WriteRune(code)
			}
		}
	}
	if isTrue(element, "bold") {
		builder.WriteString("§l")
	}
	if isTrue(element, "italic") {
		builder.WriteString("§o")
	}
	if isTrue(element, "underlined") {
		builder.WriteString("§n")
	}
	if isTrue(element, "strikethrough") {
		builder.WriteString("§m")
	}
}

func isTrue(element map[string]any, propertyName string) bool {
	value, ok := element[propertyName]
	if !ok {
		return false
	}
	// encoding/json 把 JSON true 解析为 bool
	b, ok := value.(bool)
	return ok && b
}

func minecraftTextColorCode(name string) (rune, bool) {
	switch strings.ToLower(name) {
	case "black":
		return '0', true
	case "dark_blue":
		return '1', true
	case "dark_green":
		return '2', true
	case "dark_aqua":
		return '3', true
	case "dark_red":
		return '4', true
	case "dark_purple":
		return '5', true
	case "gold":
		return '6', true
	case "gray":
		return '7', true
	case "dark_gray":
		return '8', true
	case "blue":
		return '9', true
	case "green":
		return 'a', true
	case "aqua":
		return 'b', true
	case "red":
		return 'c', true
	case "light_purple":
		return 'd', true
	case "yellow":
		return 'e', true
	case "white":
		return 'f', true
	default:
		return 0, false
	}
}

// isHexColor 判断是否为 "#RRGGBB" 形式的十六进制颜色（大小写均可）。
func isHexColor(color string) bool {
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	for _, digit := range color[1:] {
		if !isHexDigit(digit) {
			return false
		}
	}
	return true
}

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// ---------------------------------------------------------------------------
// 数据包读写
// ---------------------------------------------------------------------------

// bytesBuffer 轻量字节缓冲（等价 C# MemoryStream 的写法习惯）。
type bytesBuffer []byte

func (b *bytesBuffer) write(bytes []byte)   { *b = append(*b, bytes...) }
func (b *bytesBuffer) writeByte(value byte) { *b = append(*b, value) }
func (b *bytesBuffer) bytes() []byte        { return []byte(*b) }

// readByte 读取单字节；越界返回 false。
func (b *bytesBuffer) readByte() (byte, bool) {
	if len(*b) == 0 {
		return 0, false
	}
	value := (*b)[0]
	*b = (*b)[1:]
	return value, true
}

// writePacket 写一个完整数据包：VarInt 长度前缀 + 包内容。
func writePacket(conn net.Conn, body []byte) error {
	var packet bytesBuffer
	writeVarint(&packet, len(body))
	packet.write(body)
	_, err := conn.Write(packet.bytes())
	return err
}

// readPacket 读取一个完整数据包：长度前缀 → 包内容（含 packetId）。
func readPacket(conn net.Conn) ([]byte, error) {
	length, err := readVarintConn(conn)
	if err != nil {
		return nil, err
	}
	if length < 0 || length > maxPacketLength {
		return nil, errors.New("服务器返回了非法的数据包长度")
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, errors.New("服务器连接被提前关闭")
		}
		return nil, err
	}
	return payload, nil
}

// readVarintConn 从连接读取 VarInt。
func readVarintConn(conn net.Conn) (int, error) {
	var result int
	one := make([]byte, 1)
	for shift := 0; shift < 32; shift += 7 {
		if _, err := io.ReadFull(conn, one); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return 0, errors.New("服务器连接被提前关闭")
			}
			return 0, err
		}
		result |= int(one[0]&0x7F) << shift
		if one[0]&0x80 == 0 {
			return result, nil
		}
	}
	return 0, errors.New("VarInt 超出长度限制")
}

// writeVarint 向缓冲写入 VarInt。
func writeVarint(stream *bytesBuffer, value int) {
	unsigned := uint32(value)
	for {
		if unsigned & ^uint32(0x7F) == 0 {
			stream.writeByte(byte(unsigned))
			return
		}
		stream.writeByte(byte(unsigned&0x7F | 0x80))
		unsigned >>= 7
	}
}

// writeUnsignedShort 写大端 16 位无符号整数。
func writeUnsignedShort(stream *bytesBuffer, value uint16) {
	var pair [2]byte
	binary.BigEndian.PutUint16(pair[:], value)
	stream.write(pair[:])
}

// writeString 写 UTF-8 字符串（VarInt 长度前缀）。
func writeString(stream *bytesBuffer, value string) {
	bytes := []byte(value)
	writeVarint(stream, len(bytes))
	stream.write(bytes)
}

// readString 从缓冲读取 UTF-8 字符串（VarInt 长度前缀）。
func readString(stream *bytesBuffer) (string, error) {
	// 长度本身以 VarInt 编码，直接从缓冲读取
	var length int
	for shift := 0; shift < 32; shift += 7 {
		value, ok := stream.readByte()
		if !ok {
			return "", errors.New("服务器连接被提前关闭")
		}
		length |= int(value&0x7F) << shift
		if value&0x80 == 0 {
			break
		}
		if shift == 28 && value&0x80 != 0 {
			return "", errors.New("VarInt 超出长度限制")
		}
	}
	if length < 0 || length > maxStringLength {
		return "", errors.New("服务器返回了非法的字符串长度")
	}
	if len(*stream) < length {
		return "", errors.New("服务器连接被提前关闭")
	}
	bytes := (*stream)[:length]
	*stream = (*stream)[length:]
	return string(bytes), nil
}

// parsePort 解析端口文本，非法时返回错误。
func parsePort(text string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("非法端口：%s", text)
	}
	return port, nil
}
