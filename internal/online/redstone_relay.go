package online

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/config"
)

// 红石联机中继节点：内置官方节点 + 用户自定义节点，支持并发测速与可用性预检。
//
// 为什么需要这一层：官方节点在国内可用性并不稳定（本轮实测从部分网络访问
// 122.51.108.96:3000 直接超时）。没有预检时用户只会看到一句"申请隧道失败"，
// 既不知道是网络问题，也不知道可以换节点。
const (
	// keyRedstoneRelayList 用户自定义节点（每行一条：名称=地址 或 仅地址）。
	keyRedstoneRelayList = "online.redstoneRelayList"
	// relayProbeTimeout 单个节点的预检超时。
	relayProbeTimeout = 5 * time.Second
	// relayProbePath 预检请求路径：不带密钥访问 /tunnels 会得到 401/403，
	// 但**能拿到任何 HTTP 响应就说明节点活着**，这正是预检要判断的。
	relayProbePath = "/tunnels"
)

// RelayOption 一个中继节点选项。
type RelayOption struct {
	Name    string `json:"Name"`
	Address string `json:"Address"`
	// Builtin 是否内置（内置项不可删除）
	Builtin bool `json:"Builtin"`
}

// RelayProbe 单个节点的预检结果。
type RelayProbe struct {
	Name string `json:"Name"`
	// Address host:port（控制面）
	Address string `json:"Address"`
	// Reachable 是否收到了 HTTP 响应（4xx 也算活着）
	Reachable bool `json:"Reachable"`
	// LatencyMs 首个响应字节耗时（不可达时为 0）
	LatencyMs int64 `json:"LatencyMs"`
	// Status 响应的 HTTP 状态码（不可达时为 0）
	Status int `json:"Status"`
	// Error 不可达时的原因
	Error string `json:"Error"`
}

// builtinRelayOptions 内置节点。官方节点地址来自 RedstoneOnline 模组源码里的默认列表。
func builtinRelayOptions() []RelayOption {
	return []RelayOption{
		{Name: "官方·上海", Address: DefaultRelayAddress, Builtin: true},
	}
}

// RedstoneRelayOptions 全部可选节点：内置 + 用户自定义（按控制面地址去重）。
func RedstoneRelayOptions() []RelayOption { return parseRelayList(GetRedstoneRelayList()) }

// parseRelayList 解析用户自定义节点（每行一条：`名称=地址` 或 `仅地址`），
// 与内置节点合并后按控制面地址去重：同一个节点写 "1.2.3.4" 和 "1.2.3.4:3000"
// 只保留一条。
func parseRelayList(raw string) []RelayOption {
	options := builtinRelayOptions()
	seen := map[string]bool{}
	for _, option := range options {
		if _, apiAddress := relayAddress(option.Address); apiAddress != "" {
			seen[apiAddress] = true
		}
	}

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		name, address := "", trimmed
		// 只写了「=地址」时名称留空，下面回落成地址本身
		if index := strings.IndexAny(trimmed, "=＝"); index >= 0 {
			name = strings.TrimSpace(trimmed[:index])
			address = strings.TrimSpace(trimmed[index+1:])
		}
		address = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(address, "http://"), "https://"))
		address = strings.TrimSuffix(strings.TrimSpace(address), "/")
		if address == "" {
			continue
		}
		if name == "" {
			name = address
		}

		host, apiAddress := relayAddress(address)
		if host == "" || apiAddress == "" || seen[apiAddress] {
			continue
		}
		seen[apiAddress] = true
		options = append(options, RelayOption{Name: name, Address: address})
	}

	return options
}

// SaveRedstoneRelayList 保存用户自定义节点（每行一条）。
func SaveRedstoneRelayList(list string) {
	config.SetValue(keyRedstoneRelayList, strings.TrimSpace(list))
}

// GetRedstoneRelayList 读取用户自定义节点原文。
func GetRedstoneRelayList() string { return config.GetValue(keyRedstoneRelayList) }

// ProbeRedstoneRelay 预检单个节点：能拿到任何 HTTP 响应就算活着。
func ProbeRedstoneRelay(ctx context.Context, name, address string) RelayProbe {
	probe := RelayProbe{Name: name, Address: address}
	_, apiAddress := relayAddress(address)
	if apiAddress == "" {
		probe.Error = "节点地址为空"

		return probe
	}
	probe.Address = apiAddress

	probeCtx, cancel := context.WithTimeout(ctx, relayProbeTimeout)
	defer cancel()

	client := &http.Client{Timeout: relayProbeTimeout}
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet,
		"http://"+apiAddress+relayProbePath, nil)
	if err != nil {
		probe.Error = err.Error()

		return probe
	}

	start := time.Now()
	response, err := client.Do(request)
	latency := time.Since(start)
	if err != nil {
		probe.Error = friendlyRelayError(err)

		return probe
	}
	defer func() { _ = response.Body.Close() }()

	probe.Reachable = true
	probe.Status = response.StatusCode
	probe.LatencyMs = latency.Milliseconds()

	return probe
}

// friendlyRelayError 把网络错误翻译成用户能懂的一句话。
func friendlyRelayError(err error) string {
	var netErr net.Error

	message := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(message, "context deadline exceeded"):
		return "连接超时（节点不通或被网络拦截）"
	case errors.As(err, &netErr) && netErr.Timeout():
		return "连接超时（节点不通或被网络拦截）"
	case strings.Contains(message, "no such host"):
		return "域名解析失败"
	case strings.Contains(message, "connection refused"):
		return "节点拒绝连接（服务未启动或端口不对）"
	default:
		return message
	}
}

// ProbeRedstoneRelays 并发预检多个节点（按延迟升序返回，不可达的排在最后）。
func ProbeRedstoneRelays(ctx context.Context, options []RelayOption) []RelayProbe {
	probes := make([]RelayProbe, len(options))

	var waitGroup sync.WaitGroup
	for index, option := range options {
		waitGroup.Add(1)
		go func(position int, item RelayOption) {
			defer waitGroup.Done()
			probes[position] = ProbeRedstoneRelay(ctx, item.Name, item.Address)
		}(index, option)
	}
	waitGroup.Wait()

	sort.SliceStable(probes, func(left, right int) bool {
		if probes[left].Reachable != probes[right].Reachable {
			return probes[left].Reachable
		}
		if probes[left].Reachable {
			return probes[left].LatencyMs < probes[right].LatencyMs
		}

		return false
	})

	return probes
}

// FastestRedstoneRelay 预检全部节点并返回最快的一个（都不可达时返回空）。
func FastestRedstoneRelay(ctx context.Context) (RelayProbe, bool) {
	probes := ProbeRedstoneRelays(ctx, RedstoneRelayOptions())
	for _, probe := range probes {
		if probe.Reachable {
			// 回传时把地址收敛成设置里存的形态（host:port）
			probe.Address = relayAddressInput(probe.Address)

			return probe, true
		}
	}

	return RelayProbe{}, false
}

// relayAddressInput 把 host:port 还原成设置项里保存的形态
// （默认端口时只留主机名，便于用户看懂）。
func relayAddressInput(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	if port == strconv.Itoa(redstoneAPIPort) {
		return host
	}

	return address
}

// preflightRedstoneRelay 建房前预检中继：
//
//	配置节点可达   → 原样使用，不加说明；
//	配置节点不可达 → 并发试其它候选，取最快的一个并回一句说明；
//	全都不通       → 保持原配置继续（探测失败不等于接口不可用：某些网络只拦
//	                 非预期路径，让真正的请求去报错，避免误伤能用的节点）。
//
// 返回（数据面主机、控制面 host:port、给用户看的说明）。
func preflightRedstoneRelay(ctx context.Context, host, api, configured string) (string, string, string) {
	return preflightRelay(ctx, RedstoneRelayOptions(), host, api, configured)
}

// relayProbeBudget 一次预检的整体预算（默认等于单节点超时；测试里调小）。
var relayProbeBudget = relayProbeTimeout

// preflightRelay 预检的实现：options 是全部候选（便于测试注入）。
func preflightRelay(
	ctx context.Context,
	options []RelayOption,
	host, api, configured string,
) (string, string, string) {
	probeCtx, cancel := context.WithTimeout(ctx, relayProbeBudget*3)
	defer cancel()

	current := ProbeRedstoneRelay(probeCtx, "当前节点", configured)
	if current.Reachable {
		return host, api, ""
	}

	candidates := make([]RelayOption, 0, len(options))
	for _, option := range options {
		if _, candidateAPI := relayAddress(option.Address); candidateAPI == "" || candidateAPI == api {
			continue
		}
		candidates = append(candidates, option)
	}

	probes := ProbeRedstoneRelays(probeCtx, candidates)
	for _, probe := range probes {
		if !probe.Reachable {
			continue
		}
		fallbackHost, fallbackAPI := relayAddress(probe.Address)
		if fallbackHost == "" || fallbackAPI == "" {
			continue
		}

		return fallbackHost, fallbackAPI, fmt.Sprintf(
			"配置的中继节点 %s 不可达（%s），本次已自动改用 %s（%d ms）",
			current.Address, current.Error, probe.Address, probe.LatencyMs)
	}

	return host, api, relayAllUnreachableHint(append([]RelayProbe{current}, probes...))
}

// relayAllUnreachableHint 全部候选都不可达时的说明（带上首个失败原因）。
func relayAllUnreachableHint(probes []RelayProbe) string {
	reason := ""
	for _, probe := range probes {
		if probe.Error == "" {
			continue
		}
		reason = fmt.Sprintf("（%s：%s）", probe.Address, probe.Error)

		break
	}

	return "预检发现所有中继节点都不可达" + reason +
		"，仍按配置节点继续尝试；若随后失败，请检查网络或换成自己的节点。"
}

// withRelayNote 把预检说明附在错误后面：用户看到的就不再只是"注册密钥失败"，
// 而是"节点不通"这句真正能指导操作的结论。
func withRelayNote(err error, note string) error {
	if err == nil || note == "" {
		return err
	}

	return fmt.Errorf("%w（%s）", err, note)
}
