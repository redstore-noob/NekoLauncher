package mcserver

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RCON：Minecraft 服务端的远程控制协议（Source RCON）。
//
// 报文体是"长度前缀 + 请求 id + 类型 + 正文"，小端序：
//
//	int32 长度（后续字节数，含结尾两个 0x00）
//	int32 请求 id（鉴权失败时服务端回 -1）
//	int32 类型（3=鉴权 2=执行指令 0=响应）
//	bytes 正文 + 0x00 + 0x00
//
// 它比"往 stdin 写 list 再解析 stdout"稳得多：不依赖服务端输出格式、
// 不受日志刷屏影响、能拿到结构化响应（在线玩家列表）。因此在线人数优先走 RCON，
// 没配 RCON 的老服务器才退回 stdin。
const (
	rconTypeResponse = 0
	rconTypeCommand  = 2
	rconTypeAuth     = 3

	// rconAuthFailedRequestID 鉴权失败时服务端回的请求 id。
	rconAuthFailedRequestID = -1

	// rconPacketLimit 单个报文上限（防伪造/损坏的长度字段把内存撑爆）。
	rconPacketLimit = 4096

	rconDialTimeout    = 5 * time.Second
	rconCommandTimeout = 8 * time.Second
)

// rconClient RCON 会话（一次连接可以连续发多条指令）。
type rconClient struct {
	conn      net.Conn
	requestID int32
}

// dialRCON 建立连接并完成鉴权。
func dialRCON(ctx context.Context, address, password string) (*rconClient, error) {
	dialer := &net.Dialer{Timeout: rconDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("连接 RCON 失败：%w", err)
	}

	client := &rconClient{conn: conn}
	if err := client.authenticate(ctx, password); err != nil {
		_ = conn.Close()

		return nil, err
	}

	return client, nil
}

// authenticate 发送鉴权包并校验响应（失败时服务端回 -1）。
func (c *rconClient) authenticate(ctx context.Context, password string) error {
	if err := c.writePacket(rconTypeAuth, password); err != nil {
		return err
	}
	id, _, _, err := c.readPacket(ctx)
	if err != nil {
		return err
	}
	if id == rconAuthFailedRequestID {
		return errors.New("RCON 密码不正确")
	}

	return nil
}

// command 执行一条指令并返回响应正文。
func (c *rconClient) command(ctx context.Context, command string) (string, error) {
	if err := c.writePacket(rconTypeCommand, command); err != nil {
		return "", err
	}

	var builder strings.Builder
	for {
		_, kind, body, err := c.readPacket(ctx)
		if err != nil {
			return "", err
		}
		builder.WriteString(body)
		// 服务端可能把长响应拆成多包；长度明显短于上限时视为结束
		if kind != rconTypeResponse || len(body) < rconPacketLimit-16 {
			break
		}
	}

	return builder.String(), nil
}

// Close 关闭 RCON 连接。
func (c *rconClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}

	return c.conn.Close()
}

// writePacket 写一个请求包（正文以两个 0x00 收尾）。
func (c *rconClient) writePacket(kind int32, body string) error {
	c.requestID++
	payload := make([]byte, 0, len(body)+14)
	payload = binary.LittleEndian.AppendUint32(payload, uint32(len(body)+10))
	payload = binary.LittleEndian.AppendUint32(payload, uint32(c.requestID))
	payload = binary.LittleEndian.AppendUint32(payload, uint32(kind))
	payload = append(payload, body...)
	payload = append(payload, 0x00, 0x00)

	deadline := time.Now().Add(rconCommandTimeout)
	_ = c.conn.SetWriteDeadline(deadline)

	_, err := c.conn.Write(payload)

	return err
}

// readPacket 读一个响应包。
func (c *rconClient) readPacket(ctx context.Context) (int32, int32, string, error) {
	deadline := time.Now().Add(rconCommandTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = c.conn.SetReadDeadline(deadline)

	header := make([]byte, 4)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return 0, 0, "", err
	}
	length := int32(binary.LittleEndian.Uint32(header))
	if length < 10 || length > rconPacketLimit {
		return 0, 0, "", fmt.Errorf("RCON 报文长度异常：%d", length)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return 0, 0, "", err
	}
	requestID := int32(binary.LittleEndian.Uint32(payload[0:4]))
	kind := int32(binary.LittleEndian.Uint32(payload[4:8]))
	body := payload[8:]
	// 去掉结尾的 0x00 填充
	body = []byte(strings.TrimRight(string(body), "\x00"))

	return requestID, kind, string(body), nil
}

// rconAddress 组装 RCON 地址（服务端只监听本机时的默认地址）。
func rconAddress(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// runRCONCommand 一次性执行 RCON 指令（自己建连、自己关）。
// 未配置 RCON（密码为空）时返回错误，由调用方决定是否退回 stdin。
func runRCONCommand(ctx context.Context, address, password, command string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", errors.New("未配置 RCON 密码")
	}
	if strings.TrimSpace(address) == "" {
		return "", errors.New("未配置 RCON 地址")
	}
	commandCtx, cancel := context.WithTimeout(ctx, rconCommandTimeout)
	defer cancel()

	client, err := dialRCON(commandCtx, address, password)
	if err != nil {
		return "", err
	}
	defer client.Close()

	return client.command(commandCtx, command)
}

// rconConfiguration 从 server.properties 里读出的 RCON 配置。
type rconConfiguration struct {
	Enabled  bool
	Port     int
	Password string
}

// readRCONConfiguration 读 server.properties 里的 RCON 三项配置。
// 文件不存在或没开 RCON 时返回零值（调用方据此走 stdin 回退）。
func readRCONConfiguration(dir string) rconConfiguration {
	lines, err := loadPropertyLines(dir)
	if err != nil {
		return rconConfiguration{}
	}
	values := make(map[string]string, len(lines))
	for _, line := range lines {
		if line.key != "" {
			values[line.key] = strings.TrimSpace(line.value)
		}
	}
	if !strings.EqualFold(values["enable-rcon"], "true") {
		return rconConfiguration{}
	}
	port, portErr := strconv.Atoi(values["rcon.port"])
	if portErr != nil || port <= 0 || port > 65535 {
		port = 25575
	}
	password := values["rcon.password"]
	if strings.TrimSpace(password) == "" {
		return rconConfiguration{}
	}

	return rconConfiguration{Enabled: true, Port: port, Password: password}
}

// ensureRCONProperties 为服务器补上 RCON 配置（创建时写一次；老服务器启动时按需补）。
//
// 返回写入后的密码（已存在则原样返回）。空密码会生成一个新的随机密码：
// 服务端只在 enable-rcon=true 且密码非空时才开启 RCON。
func ensureRCONProperties(dir string) (rconConfiguration, error) {
	lines, err := loadPropertyLines(dir)
	if err != nil {
		return rconConfiguration{}, err
	}

	values := make(map[string]string, len(lines))
	for _, line := range lines {
		if line.key != "" {
			values[line.key] = line.value
		}
	}

	password := strings.TrimSpace(values["rcon.password"])
	if password == "" {
		generated, generateErr := randomRCONPassword()
		if generateErr != nil {
			return rconConfiguration{}, generateErr
		}
		password = generated
	}

	port := 25575
	if parsed, parseErr := strconv.Atoi(strings.TrimSpace(values["rcon.port"])); parseErr == nil &&
		parsed > 0 && parsed <= 65535 {
		port = parsed
	}

	updates := []Property{
		{Key: "enable-rcon", Value: "true"},
		{Key: "rcon.port", Value: strconv.Itoa(port)},
		{Key: "rcon.password", Value: password},
	}
	changed := false
	for _, property := range updates {
		if strings.TrimSpace(values[property.Key]) != property.Value {
			changed = true

			break
		}
	}
	if changed {
		if err := writePropertyUpdatesByDirectory(dir, updates); err != nil {
			return rconConfiguration{}, err
		}
	}

	return rconConfiguration{Enabled: true, Port: port, Password: password}, nil
}

// writePropertyUpdatesByDirectory 按目录写入键值（保留注释与未知行）。
//
// 与 SetServerProperties 的区别：不校验"服务器是否运行"。启动流程里补 RCON 配置时
// 状态已经是 starting，走 id 版本会被那句守卫挡下来，而这三个键本来就是启动前写好的。
func writePropertyUpdatesByDirectory(dir string, updates []Property) error {
	lines, err := loadPropertyLines(dir)
	if err != nil {
		return err
	}
	wanted := make(map[string]string, len(updates))
	for _, property := range updates {
		wanted[property.Key] = property.Value
	}

	var builder strings.Builder
	seen := map[string]bool{}
	for _, line := range lines {
		if line.key == "" {
			builder.WriteString(line.comment + "\n")

			continue
		}
		seen[line.key] = true
		if value, ok := wanted[line.key]; ok {
			builder.WriteString(line.key + "=" + value + "\n")
		} else {
			builder.WriteString(line.key + "=" + line.value + "\n")
		}
	}
	for _, property := range updates {
		if !seen[property.Key] && property.Key != "" {
			builder.WriteString(property.Key + "=" + property.Value + "\n")
		}
	}

	return os.WriteFile(filepath.Join(dir, "server.properties"), []byte(builder.String()), 0o644)
}

// randomRCONPassword 生成 24 位随机密码（RCON 只在回环地址上用，强度足够）。
func randomRCONPassword() (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	for index := range buffer {
		buffer[index] = alphabet[int(buffer[index])%len(alphabet)]
	}

	return string(buffer), nil
}
