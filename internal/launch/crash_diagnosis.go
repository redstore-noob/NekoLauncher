package launch

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// 崩溃诊断：把"游戏非零退出"从一句提示变成可执行的结论。
//
// 数据源两路：
//   - 实例游戏目录下的 crash-reports/*.txt（原版/Forge/Fabric 都会写），取最新一份；
//   - 启动器内存里的启动日志（含游戏 stdout/stderr，见 OnLogLine）。
//
// 输出刻意都是**固定中文文案**（前端拿去查词典），需要拼运行期数值的地方单独放字段，
// 不做字符串拼接——否则词典永远查不中。
type CrashDiagnosis struct {
	// ReportPath 最新崩溃报告路径；没找到为空串。
	ReportPath string `json:"ReportPath"`
	// Description 崩溃报告里的 Description 字段。
	Description string `json:"Description"`
	// Exception 崩溃报告里的首个异常行（含 Caused by 链首条）。
	Exception string `json:"Exception"`
	// Suspected 命中的原因（中文原文，可能是多条）。
	Suspected []string `json:"Suspected"`
	// Suggestions 与 Suspected 一一对应的处置建议（中文原文）。
	Suggestions []string `json:"Suggestions"`
	// Summary 一句话结论，直接作为弹窗正文。
	Summary string `json:"Summary"`
}

// CrashDiagnosisInput 诊断输入（绑定层负责采集，便于单测直接构造）。
type CrashDiagnosisInput struct {
	// CrashReportsDirectory 实例游戏目录下的 crash-reports。
	CrashReportsDirectory string
	// LogText 启动日志全文（含游戏输出）。
	LogText string
	// MinecraftVersion 当前实例的版本 id（仅用于报告展示）。
	MinecraftVersion string
	// JavaVersion 当前使用的 Java 版本描述。
	JavaVersion string
	// MemoryMb 当前实例的最大内存（MiB，0 表示未知）。
	MemoryMb int
}

// 崩溃报告里的关键字段。
var (
	crashDescriptionPattern = regexp.MustCompile(`(?m)^\s*Description:\s*(.+?)\s*$`)
	crashJavaVersionPattern = regexp.MustCompile(`(?m)^\s*Java Version:\s*(.+?)\s*$`)
	crashExceptionPattern   = regexp.MustCompile(`(?m)^\s*(?:Caused by:\s*)?([\w.$]*(?:Exception|Error|Throwable)[^\n]*)$`)
)

// 常见崩溃原因的判定规则。命中即给出对应建议，可以同时命中多条
// （例如"缺前置"往往和"模组冲突"一起出现）。
type crashRule struct {
	reason     string
	suggestion string
	patterns   []*regexp.Regexp
}

func mustPattern(expression string) *regexp.Regexp {
	return regexp.MustCompile("(?i)" + expression)
}

var crashRules = []crashRule{
	{
		reason:     "内存不足（Java 堆溢出）",
		suggestion: "到「实例 → 内存」把最大内存调大（当前值见诊断信息），或减少同时加载的模组/光影。",
		patterns: []*regexp.Regexp{
			mustPattern(`java\.lang\.OutOfMemoryError`),
			mustPattern(`Java heap space`),
			mustPattern(`GC overhead limit exceeded`),
		},
	},
	{
		reason:     "Java 版本不匹配",
		suggestion: "该版本要求更高的 Java（或不能高于某个大版本）：到「设置 → Java」换一个 JDK 后重试。",
		patterns: []*regexp.Regexp{
			mustPattern(`UnsupportedClassVersionError`),
			mustPattern(`class file version \d+\.\d+`),
			mustPattern(`requires Java \d+`),
			mustPattern(`java\.lang\.NoSuchMethodError: .*java\.base`),
		},
	},
	{
		reason:     "缺少前置模组或模组冲突",
		suggestion: "按报错里提到的模组名补齐前置（Fabric API / Architectury 等），或二分法禁用模组定位冲突项。",
		patterns: []*regexp.Regexp{
			mustPattern(`Missing or unsupported mandatory dependencies`),
			mustPattern(`ModResolutionException`),
			mustPattern(`requires .* of (?:mod|fabric)`),
			mustPattern(`net\.fabricmc\.loader\.discovery\.ModResolutionException`),
			mustPattern(`java\.lang\.NoClassDefFoundError: .*(?:fabric|forge|mod|architectury|mixinextras)`),
			mustPattern(`Duplicate mods found`),
			mustPattern(`Incompatible mod set`),
		},
	},
	{
		reason:     "显卡驱动 / OpenGL 初始化失败",
		suggestion: "更新显卡驱动；笔记本双显卡机型请在显卡控制面板里把 java 指定为独显；必要时改用其它渲染后端。",
		patterns: []*regexp.Regexp{
			mustPattern(`Pixel format not accelerated`),
			mustPattern(`org\.lwjgl\.opengl\.GLException`),
			mustPattern(`Failed to create (?:window|context)`),
			mustPattern(`WGL: Failed to make context current`),
			mustPattern(`GLFW error`),
		},
	},
	{
		reason:     "资源/依赖文件缺失或损坏",
		suggestion: "到「设置 → 启动」打开「启动前校验文件」，或在实例页重新下载该版本以补全缺失文件。",
		patterns: []*regexp.Regexp{
			mustPattern(`Could not find or load main class`),
			mustPattern(`Failed to download`),
			mustPattern(`Corrupted .*jar`),
			mustPattern(`zip END header not found`),
			mustPattern(`Invalid or corrupt jarfile`),
		},
	},
	{
		reason:     "登录会话失效",
		suggestion: "到「账户」页重新登录该账号后再启动。",
		patterns: []*regexp.Regexp{
			mustPattern(`Failed to (?:verify|authenticate) username`),
			mustPattern(`Invalid session`),
		},
	},
	{
		reason:     "Mixin 注入失败（模组与当前加载器/版本不兼容）",
		suggestion: "把报错里提到的模组更新到与当前 MC 版本匹配的版本，或暂时移除该模组。",
		patterns: []*regexp.Regexp{
			mustPattern(`InvalidMixinException`),
			mustPattern(`Mixin apply failed`),
			mustPattern(`org\.spongepowered\.asm\.mixin\.[\w.]*[Ee]xception`),
			mustPattern(`MixinTransformerError`),
		},
	},
	{
		reason:     "磁盘空间不足",
		suggestion: "清理游戏目录所在磁盘的空间（崩溃报告、日志和模组缓存都很占地方），再重新启动。",
		patterns: []*regexp.Regexp{
			mustPattern(`No space left on device`),
			mustPattern(`磁盘空间不足`),
		},
	},
	{
		reason:     "文件权限不足",
		suggestion: "检查游戏目录是否被杀毒软件/ OneDrive 同步锁定；必要时以管理员身份运行，或把游戏目录移出受保护路径。",
		patterns: []*regexp.Regexp{
			mustPattern(`Access is denied`),
			mustPattern(`Permission denied`),
		},
	},
	{
		reason:     "Java 进程无法启动",
		suggestion: "到「设置 → Java」确认所选 Java 路径有效（路径里尽量不要有中文/空格），或重新选择一个 JDK。",
		patterns: []*regexp.Regexp{
			mustPattern(`Cannot run program`),
			mustPattern(`CreateProcess error=\d+`),
			mustPattern(`unable to access jarfile`),
		},
	},
	{
		reason:     "世界存档损坏",
		suggestion: "到「实例 → 存档」检查该存档是否能进入；有备份的话优先用备份恢复，或尝试新建同种子世界后复制区块文件。",
		patterns: []*regexp.Regexp{
			mustPattern(`Failed to load level`),
			mustPattern(`Exception loading level`),
			mustPattern(`Error loading world`),
		},
	},
}

// DiagnoseCrash 诊断最近一次崩溃。
func DiagnoseCrash(input CrashDiagnosisInput) CrashDiagnosis {
	result := CrashDiagnosis{Suspected: []string{}, Suggestions: []string{}}

	report := loadNewestCrashReport(input.CrashReportsDirectory)
	if report.path != "" {
		result.ReportPath = report.path
		result.Description = report.description
		result.Exception = report.exception
	}

	// 命中判定：crash-report 全文 + 启动日志的**末尾**一起看。
	// 只看日志末尾是因为崩溃原因总是出现在进程退出前的最后几屏；日志早段
	// 出现的同类字样（例如中途进服被拒的 "Invalid session"、被修复过的
	// "OutOfMemoryError"）与本次退出无关，全量匹配会造成误报。
	//
	// 兜底窗口：末尾 200 行，且至少覆盖最后 16KB（防止某行超长导致行数太少）。
	haystack := strings.Join([]string{logTail(input.LogText, 200, 16<<10), report.content}, "\n")
	for _, rule := range crashRules {
		matched := false
		for _, pattern := range rule.patterns {
			if pattern.MatchString(haystack) {
				matched = true

				break
			}
		}
		if !matched {
			continue
		}
		result.Suspected = append(result.Suspected, rule.reason)
		result.Suggestions = append(result.Suggestions, rule.suggestion)
	}

	result.Summary = buildCrashSummary(input, result)

	return result
}

// logTail 取日志末尾 maxLines 行；若截出的字节量低于 minBytes，则改为取末尾
// minBytes 字节（按行首对齐），保证单行超长时仍能覆盖足够的崩溃现场。
func logTail(text string, maxLines int, minBytes int) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	tail := strings.Join(lines, "\n")
	if len(tail) >= minBytes {
		return tail
	}
	// 行数太少（超长行），退回按字节截取并对齐到行首
	if len(text) <= minBytes {
		return text
	}
	cut := text[len(text)-minBytes:]
	if idx := strings.IndexByte(cut, '\n'); idx >= 0 {
		cut = cut[idx+1:]
	}

	return cut
}

// buildCrashSummary 生成一句话结论（固定文案 + 单独的数值字段由前端拼）。
func buildCrashSummary(input CrashDiagnosisInput, diagnosis CrashDiagnosis) string {
	switch {
	case len(diagnosis.Suspected) > 0 && diagnosis.ReportPath != "":
		return "已生成崩溃报告，并识别出可能的原因（见下方）。"
	case len(diagnosis.Suspected) > 0:
		return "从启动日志里识别出可能的原因（见下方）。"
	case diagnosis.ReportPath != "":
		return "已找到崩溃报告，但没能自动判断原因；可以把诊断信息发给开发者。"
	default:
		return "没有找到崩溃报告，也没有识别出已知原因；可以把诊断信息发给开发者。"
	}
}

// crashReport 从磁盘读到的一份崩溃报告。
type crashReport struct {
	path        string
	content     string
	description string
	exception   string
}

// loadNewestCrashReport 取 crash-reports 下最新的一份 .txt。
// 读不到（目录不存在 / 无权限 / 全是目录）时返回零值，不报错——
// 崩溃诊断本身不该再抛异常。
func loadNewestCrashReport(directory string) crashReport {
	if strings.TrimSpace(directory) == "" {
		return crashReport{}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return crashReport{}
	}

	type candidate struct {
		path    string
		modTime int64
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".txt") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		candidates = append(candidates, candidate{
			path:    filepath.Join(directory, entry.Name()),
			modTime: info.ModTime().UnixNano(),
		})
	}
	if len(candidates) == 0 {
		return crashReport{}
	}
	sort.Slice(candidates, func(left, right int) bool {
		return candidates[left].modTime > candidates[right].modTime
	})

	target := candidates[0].path
	data, readErr := os.ReadFile(target)
	if readErr != nil {
		return crashReport{}
	}
	// 崩溃报告可能很大（几千行堆栈），只保留头部用于解析与展示
	content := string(data)
	if len(content) > 64*1024 {
		content = content[:64*1024]
	}

	return crashReport{
		path:        target,
		content:     content,
		description: firstMatch(crashDescriptionPattern, content),
		exception:   firstMatch(crashExceptionPattern, content),
	}
}

// firstMatch 取第一个捕获组（无匹配返回空串）。
func firstMatch(pattern *regexp.Regexp, text string) string {
	matched := pattern.FindStringSubmatch(text)
	if len(matched) < 2 {
		return ""
	}

	return strings.TrimSpace(matched[1])
}
