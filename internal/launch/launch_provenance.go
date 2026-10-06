package launch

import (
	"fmt"
	"sort"
	"strings"
)

// 启动溯源：把"最终 Java 命令行"从一串没有来历的参数，变成每条都说得清
// **是谁加的、为什么加**的可读因果链。
//
// 动机（真实痛点）：用户看到 `-Xmx4096M` 却不知道它来自实例独立设置还是全局手动上限；
// 看到重复的 `-cp` 不知道该删哪一个；装了一堆模组后想确认某个 javaagent 是谁挂的。
// 这些信息在装配过程中全部存在，但装配完就丢了——本文件负责在装配时把它记下来。
//
// 设计约束：
//   - **纯记账，不参与决策**：溯源记录绝不改变最终参数，只是旁路记录。
//     这样即使溯源出错，也绝不会影响游戏能不能启动。
//   - 来源用**稳定标识**（LaunchArgumentSource.Key）而非文案，前端拿去查词典，
//     与 crash_diagnosis 的"固定文案 + 单独字段"约定一致。
//   - 只记录"值得解释"的参数：占位符展开这类机械参数标为 version-json 即可，
//     不逐条展开（否则一次启动几百条，面板没法看）。

// LaunchArgumentSourceKind 参数来源的大类。
type LaunchArgumentSourceKind string

const (
	// SourceVersionJSON 版本档案（`<版本>.json` 里的 arguments）。
	SourceVersionJSON LaunchArgumentSourceKind = "version-json"
	// SourceLauncherAuto 启动器按规则自动下发（内置 JVM 调优、内存、classpath 等）。
	SourceLauncherAuto LaunchArgumentSourceKind = "launcher-auto"
	// SourceGlobalSettings 全局高级启动设置。
	SourceGlobalSettings LaunchArgumentSourceKind = "global-settings"
	// SourceInstanceSettings 实例独立高级启动设置。
	SourceInstanceSettings LaunchArgumentSourceKind = "instance-settings"
	// SourceInstanceOrGlobal 来自"实例未独立时回落到全局"的那一份设置。
	SourceInstanceOrGlobal LaunchArgumentSourceKind = "instance-or-global"
	// SourceDirectConnect 启动器为"直接进服"追加的参数。
	SourceDirectConnect LaunchArgumentSourceKind = "direct-connect"
	// SourceAuthlibInjector 外置登录注入器。
	SourceAuthlibInjector LaunchArgumentSourceKind = "authlib-injector"
	// SourcePlugin 插件通过启动变换贡献的参数。
	SourcePlugin LaunchArgumentSourceKind = "plugin"
	// SourceTransform 宿主启动变换中无法归到插件名下的部分。
	SourceTransform LaunchArgumentSourceKind = "transform"
	// SourceUnknown 未能归因（兜底，不隐藏信息）。
	SourceUnknown LaunchArgumentSourceKind = "unknown"
)

// LaunchArgumentSource 一条参数的来历。
type LaunchArgumentSource struct {
	Kind LaunchArgumentSourceKind `json:"Kind"`
	// Key 稳定标识：前端据此查词典得到人类可读说明。
	// 例："memory.max.instance"、"jvm.tuning.g1"、"plugin.foo"。
	Key string `json:"Key"`
	// Detail 运行期数值/名称，前端按 Key 找到模板后插值。
	// 例：内存 MiB、插件 id、服务器地址。不参与词典匹配。
	Detail string `json:"Detail"`
	// PluginID 仅当 Kind == SourcePlugin 时有值，便于按插件聚合展示。
	PluginID string `json:"PluginID"`
}

// LaunchArgumentEntry 最终命令行里的一条参数及其来历。
type LaunchArgumentEntry struct {
	// Index 在最终参数序列中的下标。
	Index int `json:"Index"`
	// Argument 参数原文。敏感值（accessToken 等）已由 collect 侧脱敏。
	Argument string `json:"Argument"`
	// Section jvm / game / main-class：参数在命令行中的区段。
	Section string `json:"Section"`
	// Source 该参数的来历。
	Source LaunchArgumentSource `json:"Source"`
	// Shadowed 该参数是否被后面的同名前缀参数覆盖（如重复的 -Xmx）。
	// JVM 取最后出现的值，被覆盖的那个是"用户改了没生效"的常见困惑源。
	Shadowed bool `json:"Shadowed"`
}

// LaunchProvenanceReport 一次启动的参数溯源报告。
type LaunchProvenanceReport struct {
	Entries []LaunchArgumentEntry `json:"Entries"`
	// Effective 只保留真正生效的参数（排除被覆盖项）。
	Effective []LaunchArgumentEntry `json:"Effective"`
	// Overridden 被后续同名参数覆盖的条目。
	Overridden []LaunchArgumentEntry `json:"Overridden"`
	// Conflicts 人类可读的冲突摘要（Key 形式，前端查词典）。
	Conflicts []LaunchProvenanceConflict `json:"Conflicts"`
	// JavaExecutable / MainClass / WorkingDirectory 便于面板头部展示。
	JavaExecutable   string `json:"JavaExecutable"`
	MainClass        string `json:"MainClass"`
	WorkingDirectory string `json:"WorkingDirectory"`
}

// LaunchProvenanceConflict 一处"重复下发/互相覆盖"的提示。
type LaunchProvenanceConflict struct {
	// Prefix 冲突的参数前缀（如 "-Xmx"、"-cp"）。
	Prefix string `json:"Prefix"`
	// WinnerIndex 最终生效的下标。
	WinnerIndex int `json:"WinnerIndex"`
	// LoserIndices 被覆盖的下标（升序）。
	LoserIndices []int `json:"LoserIndices"`
	// WinnerSource / LoserSource 双方的来源，用于说清"谁压过了谁"。
	WinnerSource LaunchArgumentSource `json:"WinnerSource"`
	LoserSource  LaunchArgumentSource `json:"LoserSource"`
}

// ---------------------------------------------------------------------------
// 采集器
// ---------------------------------------------------------------------------

// provenanceRecorder 装配过程中的旁路记账器。
// 由 Build 在需要时创建；未启用时（nil）所有方法都安全空转，零开销路径不变。
type provenanceRecorder struct {
	entries []LaunchArgumentEntry
	// lastSection 记录当前区段，append 时自动沿用。
	lastSection string
}

// 命令行区段标识。
const (
	sectionJVM       = "jvm"
	sectionGame      = "game"
	sectionMainClass = "main-class"
)

// newProvenanceRecorder 按需创建记账器：未启用返回 nil（空转路径）。
func newProvenanceRecorder(enabled bool) *provenanceRecorder {
	if !enabled {
		return nil
	}
	return &provenanceRecorder{entries: make([]LaunchArgumentEntry, 0, 64)}
}

// section 切换当前记账区段（jvm / game / main-class）。
func (r *provenanceRecorder) section(name string) {
	if r == nil {
		return
	}
	r.lastSection = name
}

// mark 记录"接下来要 append 的这些参数"的来历。
// 通过返回一个闭包给调用方在 append 后回填下标，避免大改 Build 的既有结构。
func (r *provenanceRecorder) record(arguments []string, source LaunchArgumentSource) {
	if r == nil {
		return
	}
	for _, argument := range arguments {
		if strings.TrimSpace(argument) == "" {
			continue
		}
		r.entries = append(r.entries, LaunchArgumentEntry{
			Argument: redactArgument(argument),
			Section:  r.lastSection,
			Source:   source,
		})
	}
}

// recordOne 记录单条参数。
func (r *provenanceRecorder) recordOne(argument string, source LaunchArgumentSource) {
	if r == nil {
		return
	}
	r.record([]string{argument}, source)
}

// undoLast 撤销最近一条记账。
// 用途：appendResolvedArgument 在"可空占位符解析为空"时会回溯删除已追加的标志
// （如离线账号的 --clientId），记账必须同步撤销，否则条数多于最终命令行。
func (r *provenanceRecorder) undoLast() {
	if r == nil || len(r.entries) == 0 {
		return
	}
	r.entries = r.entries[:len(r.entries)-1]
}

// collectProvenance 把记账条目与最终参数对齐后生成报告。
// 返回 nil 表示记账与最终参数不一致（调用方据此跳过报告，绝不报错——
// 溯源只是解释层，不能因为它让启动失败）。
func (r *provenanceRecorder) collectProvenance(finalArguments []string) *LaunchProvenanceReport {
	if r == nil {
		return nil
	}
	if len(r.entries) != len(finalArguments) {
		provenanceDebugMismatch(r.entries, finalArguments)
		return nil
	}
	entries := make([]LaunchArgumentEntry, len(r.entries))
	copy(entries, r.entries)
	// 逐条校验：对不上就不产出报告（宁可没有面板，也不能显示错误归因）
	for index := range entries {
		if !provenanceArgumentMatches(entries[index].Argument, finalArguments[index]) {
			provenanceDebugMismatch(r.entries, finalArguments)
			return nil
		}
		entries[index].Argument = redactArgument(finalArguments[index])
	}
	report := analyzeProvenance(entries)
	return &report
}

// provenanceDebugMismatch 记账与最终参数对不齐时的开发期诊断。
// 这是"记账点漏记/多记"的信号（改启动管线时最容易犯），必须能一眼看出差在哪；
// 生产路径上它只是往日志写几行，绝不影响启动。
func provenanceDebugMismatch(entries []LaunchArgumentEntry, finalArguments []string) {
	if !provenanceDebugEnabled {
		return
	}
	limit := len(entries)
	if len(finalArguments) > limit {
		limit = len(finalArguments)
	}
	for index := 0; index < limit; index++ {
		recorded := "<无>"
		if index < len(entries) {
			recorded = entries[index].Argument
		}
		actual := "<无>"
		if index < len(finalArguments) {
			actual = finalArguments[index]
		}
		if recorded != actual {
			println("provenance mismatch at", index, "recorded:", recorded, "actual:", actual)
		}
	}
}

// provenanceDebugEnabled 开发期开关：用 env 打开对齐诊断，默认关闭。
var provenanceDebugEnabled = false

// provenanceArgumentMatches 比对记账值与该位置的最终参数。
// 记账发生在占位符替换**之后**（记录的就是替换结果），故应当严格相等；
// 只有脱敏造成的差异需要用前缀比对兜底。
func provenanceArgumentMatches(recorded, final string) bool {
	if recorded == final {
		return true
	}
	// "--accessToken=<值>" 被脱敏成 "--accessToken=***"
	if strings.HasSuffix(recorded, "=***") {
		return strings.HasPrefix(final, strings.TrimSuffix(recorded, "***"))
	}
	// "令牌键 + 续行值"形态：值位被整体替换为 ***，任何原文都接受
	if recorded == "***" {
		return true
	}
	return false
}

// annotatedArgument 把一条参数和它的来历绑在一起，供 Build 内部传递。
type annotatedArgument struct {
	value  string
	source LaunchArgumentSource
}

// ---------------------------------------------------------------------------
// 脱敏
// ---------------------------------------------------------------------------

// sensitiveArgumentPrefixes 令牌类参数：原文绝不出现在溯源报告里。
// 报告会经绑定层送到前端渲染，这些值没有展示价值但泄漏代价很高。
var sensitiveArgumentPrefixes = []string{
	"--accessToken",
	"--clientId",
	"--auth_session",
	"--xuid",
}

// redactArgument 对令牌类参数做脱敏（保留键名，值替换为占位符）。
func redactArgument(argument string) string {
	for _, prefix := range sensitiveArgumentPrefixes {
		if argument == prefix {
			// 形如 "--accessToken <value>" 的分段写法无法在此处配对，
			// 由 redactArgumentSequence 兜底整体处理。
			return argument
		}
	}
	for _, prefix := range sensitiveArgumentPrefixes {
		if strings.HasPrefix(argument, prefix+"=") {
			return prefix + "=***"
		}
	}
	return argument
}

// redactArgumentSequence 处理"--accessToken <value>"这类"键值分列"的形态：
// 键保留，紧随其后的值替换为 ***。
func redactArgumentSequence(arguments []string) []string {
	if len(arguments) == 0 {
		return arguments
	}
	result := make([]string, len(arguments))
	copy(result, arguments)
	for index := 0; index < len(result)-1; index++ {
		for _, prefix := range sensitiveArgumentPrefixes {
			if result[index] == prefix {
				result[index+1] = "***"
			}
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// 冲突分析
// ---------------------------------------------------------------------------

// conflictPrefixOf 判断一条参数是否参与"后者覆盖前者"的语义。
// 返回空串表示该参数不参与冲突分析（可以重复出现）。
func conflictPrefixOf(argument string) string {
	// 形如 -Xmx4096M / -Xms512M / -Xss4M：前缀 + 数字部分
	for _, prefix := range []string{"-Xmx", "-Xms", "-Xss", "-Xmn"} {
		if strings.HasPrefix(argument, prefix) && len(argument) > len(prefix) {
			return prefix
		}
	}
	// 形如 -Dkey=value：同名系统属性后者覆盖前者
	if strings.HasPrefix(argument, "-D") {
		if equals := strings.Index(argument, "="); equals > 2 {
			return argument[:equals+1]
		}
	}
	// 形如 --width / --height（游戏参数里重复就是覆盖）
	for _, prefix := range []string{"--width", "--height", "--username", "--uuid"} {
		if argument == prefix {
			return prefix
		}
	}
	return ""
}

// analyzeProvenance 由记账条目算出生效/被覆盖/冲突三块视图。
func analyzeProvenance(entries []LaunchArgumentEntry) LaunchProvenanceReport {
	// 重排下标：记账顺序即最终顺序
	ordered := make([]LaunchArgumentEntry, len(entries))
	copy(ordered, entries)
	for index := range ordered {
		ordered[index].Index = index
	}

	// 同前缀分组，组内最后一条生效
	groups := map[string][]int{}
	for index, entry := range ordered {
		if prefix := conflictPrefixOf(entry.Argument); prefix != "" {
			groups[prefix] = append(groups[prefix], index)
		}
	}

	// 标出被覆盖项
	shadowed := map[int]bool{}
	var conflicts []LaunchProvenanceConflict
	// 前缀排序，保证输出稳定（map 遍历顺序随机会让测试与 UI 抖动）
	prefixes := make([]string, 0, len(groups))
	for prefix := range groups {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	for _, prefix := range prefixes {
		indices := groups[prefix]
		if len(indices) < 2 {
			continue
		}
		winner := indices[len(indices)-1]
		losers := indices[:len(indices)-1]
		for _, loser := range losers {
			shadowed[loser] = true
		}
		conflicts = append(conflicts, LaunchProvenanceConflict{
			Prefix:       prefix,
			WinnerIndex:  winner,
			LoserIndices: losers,
			WinnerSource: ordered[winner].Source,
			LoserSource:  ordered[losers[len(losers)-1]].Source,
		})
	}

	report := LaunchProvenanceReport{
		Entries:    ordered,
		Effective:  []LaunchArgumentEntry{},
		Overridden: []LaunchArgumentEntry{},
		Conflicts:  conflicts,
	}
	for index, entry := range ordered {
		entry.Shadowed = shadowed[index]
		// Shadowed 是派生字段，写回两份视图
		if entry.Shadowed {
			report.Overridden = append(report.Overridden, entry)
		} else {
			report.Effective = append(report.Effective, entry)
		}
		report.Entries[index].Shadowed = entry.Shadowed
	}
	if report.Effective == nil {
		report.Effective = []LaunchArgumentEntry{}
	}
	if report.Overridden == nil {
		report.Overridden = []LaunchArgumentEntry{}
	}
	if report.Conflicts == nil {
		report.Conflicts = []LaunchProvenanceConflict{}
	}
	return report
}

// summarizeProvenance 生成一句话摘要（稳定 Key 形式，前端查词典）。
func summarizeProvenance(report LaunchProvenanceReport) string {
	if len(report.Conflicts) == 0 {
		return "provenance.summary.clean"
	}
	return fmt.Sprintf("provenance.summary.conflicts:%d", len(report.Conflicts))
}
