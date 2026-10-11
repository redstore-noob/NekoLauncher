package network

import (
	"context"
	"net"
	"strings"
	"time"
)

// 本文件实现 Minecraft SRV 记录解析：与原版客户端一致，未显式指定端口的
// 域名先查询 _minecraft._tcp.<host> SRV 记录再连接；只覆盖状态查询侧，
// 进服路径把裸域名交给游戏自身解析。

// srvLookupTimeout SRV 查询的独立超时：DNS 卡死最多拖慢一次状态查询，
// 不允许吃掉整体 10s 预算；查询失败按「无 SRV 记录」处理，回退直连。
const srvLookupTimeout = 3 * time.Second

// lookupSRVRecords SRV 查询入口；抽成变量仅为测试注入 DNS 替身。
var lookupSRVRecords = func(ctx context.Context, name string) ([]*net.SRV, error) {
	_, records, err := net.DefaultResolver.LookupSRV(ctx, "minecraft", "tcp", name)
	return records, err
}

// resolveServerTarget 解析实际连接目标，规则与原版客户端一致：
// 端口为默认 25565（即用户未显式写端口，快速进服列表保存裸域名时也是此形态）
// 的域名先查 SRV，命中则连接记录的目标与端口；未命中（无记录 / NXDOMAIN /
// 查询失败 / 超时）回退直连原地址。IP 字面量与显式端口跳过查询——
// 进服时 host:非25565 端口会原样传给游戏（显式端口不走 SRV），此处保持一致。
func resolveServerTarget(ctx context.Context, host string, port int) (string, int) {
	if port != defaultPort || isIPLiteral(host) {
		return host, port
	}

	lookupCtx, cancel := context.WithTimeout(ctx, srvLookupTimeout)
	defer cancel()
	records, err := lookupSRVRecords(lookupCtx, host)
	if err != nil || len(records) == 0 {
		return host, port
	}

	// 返回结果已由解析器按优先级/权重排序，取第一条；
	// Target "."（RFC 2782）或端口 0 表示服务明确不可用，同样回退直连
	record := records[0]
	target := strings.TrimSuffix(record.Target, ".")
	if target == "" || record.Port == 0 {
		return host, port
	}
	return target, int(record.Port)
}

// isIPLiteral 判断 host 是否为 IP 字面量（IPv4 / IPv6，含裸 IPv6）。
// IP 地址没有域名 SRV 语义，原版客户端也跳过查询。
func isIPLiteral(host string) bool {
	return net.ParseIP(host) != nil
}
