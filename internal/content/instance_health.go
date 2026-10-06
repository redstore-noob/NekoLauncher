package content

// instance_health.go 实例"体检分"：把散落在各处的可诊断项收敛成一个分数 + 可执行建议。
//
// 与既有诊断的分工：
//   - crash_diagnosis.go（launch 包）：**崩溃之后**根据报错文本倒推原因；
//   - 本文件：**启动之前**根据实例的静态事实提前发现问题。
//
// 为什么需要它：用户遇到"游戏起不来"时，面对的是 Java 版本、内存、重复 mod、
// 缺前置、加载器不匹配五类互不相干的信息，散在五个页面里。这里把它们合成
// 一个 0-100 的分数和一份"按严重度排序的待办清单"，让用户知道**先修哪个**。
//
// 设计约束：
//   - **扣分必须可解释**：每一项扣多少分都写在下发给前端的 Findings 里，
//     分数只是汇总，绝不做"黑盒打分"。
//   - **拿不到的数据不扣分**：信息缺失（版本未识别、内存未配置）不等于有问题。
//     这是本文件与"体检"类功能最容易做错的地方——把"不知道"当"有毛病"，
//     用户就会被一堆无从下手的告警淹没。
//   - 纯只读：不修改任何文件、不启动任何进程。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HealthGrade 体检等级（稳定标识，前端查词典）。
type HealthGrade string

const (
	// GradeHealthy 没有发现问题。
	GradeHealthy HealthGrade = "healthy"
	// GradeFair 有小问题，能启动但可能不稳定。
	GradeFair HealthGrade = "fair"
	// GradePoor 有明确问题，很可能起不来。
	GradePoor HealthGrade = "poor"
	// GradeCritical 有阻断级问题，几乎必然起不来。
	GradeCritical HealthGrade = "critical"
)

// HealthFindingKind 体检项的类型（稳定标识，前端查词典）。
type HealthFindingKind string

const (
	FindingDuplicateMod      HealthFindingKind = "duplicate-mod"
	FindingMissingDependency HealthFindingKind = "missing-dependency"
	FindingIncompatibleMod   HealthFindingKind = "incompatible-mod"
	FindingLoaderMismatch    HealthFindingKind = "loader-mismatch"
	FindingMCVersionMismatch HealthFindingKind = "mc-version-mismatch"
	FindingJavaMismatch      HealthFindingKind = "java-mismatch"
	FindingMemoryLow         HealthFindingKind = "memory-low"
	FindingMemoryExcessive   HealthFindingKind = "memory-excessive"
	FindingNoJavaConfigured  HealthFindingKind = "no-java-configured"
	FindingDisabledMods      HealthFindingKind = "disabled-mods"
	FindingUnreadableMods    HealthFindingKind = "unreadable-mods"
)

// HealthFinding 一条体检结论。
type HealthFinding struct {
	Kind HealthFindingKind `json:"Kind"`
	// Severity 复用冲突检测的严重度（error / warning / info）。
	Severity ConflictSeverity `json:"Severity"`
	// Deduction 本条扣了多少分（0 表示只提示不扣分）。
	Deduction int `json:"Deduction"`
	// Subject 主体（mod id / "java" / "memory"），前端按 Kind 找模板后插值。
	Subject string `json:"Subject"`
	// Detail 运行期数值（"需要 Java 17|当前 Java 8" 这种成对信息用 | 分隔）。
	Detail string `json:"Detail"`
	// Related 相关方（谁需要这个前置、涉及哪些文件）。
	Related []string `json:"Related"`
}

// HealthInput 体检输入：由绑定层采集（便于单测直接构造）。
type HealthInput struct {
	// ModsDirectory 实例的 mods 目录。
	ModsDirectory string
	// MinecraftVersion 实例基础 MC 版本（空串表示未识别 → 跳过相关检查）。
	MinecraftVersion string
	// LoaderName 加载器 id（fabric/forge/neoforge/quilt/vanilla；空串跳过检查）。
	LoaderName string
	// RequiredJavaMajor 该版本要求的 Java 主版本（0 表示未知 → 跳过检查）。
	RequiredJavaMajor int
	// CurrentJavaMajor 当前配置的 Java 主版本（0 表示未知 → 跳过检查）。
	CurrentJavaMajor int
	// ConfiguredMaximumMemoryMb 当前生效的最大内存（0 表示未知 → 跳过检查）。
	ConfiguredMaximumMemoryMb int
	// SystemTotalMemoryMb 物理内存总量（0 表示未知 → 跳过检查）。
	SystemTotalMemoryMb int
	// DisabledModCount 已禁用的 mod 数量（仅提示，不扣分）。
	DisabledModCount int
}

// InstanceHealth 体检结果。
type InstanceHealth struct {
	// Score 0-100。100 表示没发现任何问题（不代表绝对能启动）。
	Score int `json:"Score"`
	// Grade 等级。
	Grade HealthGrade `json:"Grade"`
	// Findings 全部结论，按严重度排序。
	Findings []HealthFinding `json:"Findings"`
	// BlockingCount Error 级结论数（前端可据此决定是否"建议先修复再启动"）。
	BlockingCount int `json:"BlockingCount"`
	// AnalyzedMods / UnreadableMods 检测覆盖率：让用户知道结论的可信范围。
	AnalyzedMods   int `json:"AnalyzedMods"`
	UnreadableMods int `json:"UnreadableMods"`
}

// 单项扣分权重。集中放在这里，便于审阅"为什么是这个分数"。
const (
	deductionDuplicateMod      = 25
	deductionMissingDepend     = 25
	deductionIncompatible      = 20
	deductionLoaderMismatch    = 25
	deductionMCVersionMismatch = 5
	deductionJavaMismatch      = 30
	deductionNoJava            = 15
	deductionMemoryLow         = 15
	deductionMemoryExcessive   = 5
	deductionUnreadableMods    = 3
)

// RunInstanceHealth 执行体检。
func RunInstanceHealth(input HealthInput) InstanceHealth {
	// 1. mod 层：复用冲突检测（同一次分析同时喂给分数与冲突列表，口径唯一）
	conflictReport := AnalyzeModConflicts(
		input.ModsDirectory, input.MinecraftVersion, input.LoaderName)

	findings := make([]HealthFinding, 0, len(conflictReport.Conflicts)+4)
	for _, conflict := range conflictReport.Conflicts {
		findings = append(findings, healthFindingFromConflict(conflict))
	}

	// 2. Java 层
	findings = append(findings, checkJava(input)...)

	// 3. 内存层
	findings = append(findings, checkMemory(input)...)

	// 4. 仅提示项
	findings = append(findings, informationalFindings(input, conflictReport)...)

	sortHealthFindings(findings)

	totalDeduction := 0
	blocking := 0
	for _, finding := range findings {
		totalDeduction += finding.Deduction
		if finding.Severity == SeverityError {
			blocking++
		}
	}
	score := 100 - totalDeduction
	if score < 0 {
		score = 0
	}

	return InstanceHealth{
		Score:          score,
		Grade:          gradeFor(score, blocking),
		Findings:       findings,
		BlockingCount:  blocking,
		AnalyzedMods:   conflictReport.AnalyzedMods,
		UnreadableMods: conflictReport.UnreadableMods,
	}
}

// healthFindingFromConflict 把冲突结论翻译成体检项（含扣分）。
func healthFindingFromConflict(conflict ModConflict) HealthFinding {
	deduction := 0
	switch conflict.Kind {
	case "duplicate-mod":
		deduction = deductionDuplicateMod
	case "missing-dependency":
		deduction = deductionMissingDepend
	case "incompatible-declared":
		deduction = deductionIncompatible
	case "loader-mismatch":
		deduction = deductionLoaderMismatch
	case "mc-version-mismatch":
		deduction = deductionMCVersionMismatch
	}

	files := make([]string, 0, len(conflict.Files)+len(conflict.Related))
	files = append(files, conflict.Files...)
	files = append(files, conflict.Related...)

	return HealthFinding{
		Kind:      HealthFindingKind(conflict.Kind),
		Severity:  conflict.Severity,
		Deduction: deduction,
		Subject:   conflict.Subject,
		Detail:    conflict.Detail,
		Related:   files,
	}
}

// checkJava Java 版本相关。
func checkJava(input HealthInput) []HealthFinding {
	// 任一侧未知就跳过：把"不知道"当"有问题"是体检功能最容易犯的错
	if input.RequiredJavaMajor <= 0 || input.CurrentJavaMajor <= 0 {
		if input.RequiredJavaMajor > 0 && input.CurrentJavaMajor <= 0 {
			return []HealthFinding{{
				Kind:      FindingNoJavaConfigured,
				Severity:  SeverityWarning,
				Deduction: deductionNoJava,
				Subject:   "java",
				Detail:    itoa(input.RequiredJavaMajor),
			}}
		}
		return nil
	}
	if input.RequiredJavaMajor == input.CurrentJavaMajor {
		return nil
	}

	// 低于要求：几乎必然 UnsupportedClassVersionError
	// 高于要求：旧版 Forge 对 JPMS 敏感，可能起不来，但多数情况能跑
	severity := SeverityError
	if input.CurrentJavaMajor > input.RequiredJavaMajor {
		severity = SeverityWarning
	}
	return []HealthFinding{{
		Kind:      FindingJavaMismatch,
		Severity:  severity,
		Deduction: deductionJavaMismatch,
		Subject:   "java",
		// "需要|实际"：前端按 | 拆开展示，不去解析中文
		Detail: itoa(input.RequiredJavaMajor) + "|" + itoa(input.CurrentJavaMajor),
	}}
}

// checkMemory 内存相关。
func checkMemory(input HealthInput) []HealthFinding {
	if input.ConfiguredMaximumMemoryMb <= 0 {
		return nil
	}

	var findings []HealthFinding
	// 低于 2 GiB：现代 MC 世界生成的实际堆需求远超此值（与启动器的
	// automaticMemoryFloorMb 保底策略同口径）
	if input.ConfiguredMaximumMemoryMb < 2048 {
		findings = append(findings, HealthFinding{
			Kind:      FindingMemoryLow,
			Severity:  SeverityWarning,
			Deduction: deductionMemoryLow,
			Subject:   "memory",
			Detail:    itoa(input.ConfiguredMaximumMemoryMb),
		})
	}
	// 超过物理内存：会疯狂换页
	if input.SystemTotalMemoryMb > 0 &&
		input.ConfiguredMaximumMemoryMb > input.SystemTotalMemoryMb {
		findings = append(findings, HealthFinding{
			Kind:      FindingMemoryExcessive,
			Severity:  SeverityWarning,
			Deduction: deductionMemoryExcessive,
			Subject:   "memory",
			Detail: itoa(input.ConfiguredMaximumMemoryMb) + "|" +
				itoa(input.SystemTotalMemoryMb),
		})
	}
	return findings
}

// informationalFindings 只提示、不扣分的项。
// 这些是"信息"而非"问题"：用户主动禁用 mod、个别 jar 读不出元数据都很正常。
func informationalFindings(input HealthInput, report ModConflictReport) []HealthFinding {
	var findings []HealthFinding
	if input.DisabledModCount > 0 {
		findings = append(findings, HealthFinding{
			Kind:      FindingDisabledMods,
			Severity:  SeverityInfo,
			Deduction: 0,
			Subject:   "mods",
			Detail:    itoa(input.DisabledModCount),
		})
	}
	if report.UnreadableMods > 0 {
		findings = append(findings, HealthFinding{
			Kind:      FindingUnreadableMods,
			Severity:  SeverityInfo,
			Deduction: deductionUnreadableMods,
			Subject:   "mods",
			Detail:    itoa(report.UnreadableMods),
		})
	}
	return findings
}

// gradeFor 由分数与阻断项数决定等级。
// 有阻断项时即使分数还高也必须落到 poor 及以下——分数是汇总，
// 但"有一项必然起不来"这件事不能被平均分掩盖。
func gradeFor(score, blockingCount int) HealthGrade {
	switch {
	case blockingCount > 0 && score >= 60:
		return GradePoor
	case blockingCount > 0:
		return GradeCritical
	case score >= 90:
		return GradeHealthy
	case score >= 70:
		return GradeFair
	}
	return GradePoor
}

// sortHealthFindings 严重度排序（error → warning → info），同级按 Kind + Subject 稳定排序。
func sortHealthFindings(findings []HealthFinding) {
	rank := func(severity ConflictSeverity) int {
		switch severity {
		case SeverityError:
			return 0
		case SeverityWarning:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(findings, func(left, right int) bool {
		leftRank, rightRank := rank(findings[left].Severity), rank(findings[right].Severity)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if findings[left].Kind != findings[right].Kind {
			return findings[left].Kind < findings[right].Kind
		}
		return findings[left].Subject < findings[right].Subject
	})
}

// DetectRequiredJavaMajor 从版本的 Java 要求文本里提取主版本号。
// 输入形如 "Java 17" / "17" / ">=17" / "Java 8"；提取不到返回 0（跳过检查）。
func DetectRequiredJavaMajor(requirement string) int {
	text := strings.TrimSpace(requirement)
	if text == "" {
		return 0
	}
	// 取第一段连续数字
	start := -1
	for index, character := range text {
		if character >= '0' && character <= '9' {
			if start < 0 {
				start = index
			}

			continue
		}
		if start >= 0 {
			return atoiOrZero(text[start:index])
		}
	}
	if start >= 0 {
		return atoiOrZero(text[start:])
	}
	return 0
}

// CountDisabledMods 统计目录下被禁用的 mod 数量（.jar.disabled）。
func CountDisabledMods(modsDirectory string) int {
	entries, err := readModDirectoryNames(modsDirectory)
	if err != nil {
		return 0
	}
	count := 0
	for _, name := range entries {
		lowered := strings.ToLower(name)
		if strings.HasSuffix(lowered, ".jar"+disabledFileSuffix) {
			count++
		}
	}
	return count
}

// readModDirectoryNames 读取目录下的文件名（非递归）；目录不存在返回错误。
func readModDirectoryNames(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

// ModsDirectoryFor 由实例内容目录推出 mods 目录。
func ModsDirectoryFor(contentDirectory string) string {
	if strings.TrimSpace(contentDirectory) == "" {
		return ""
	}
	return filepath.Join(contentDirectory, "mods")
}

// itoa 轻量整数转字符串。
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

// atoiOrZero 解析十进制整数；失败返回 0。
// 与 content_archive.go 的 atoiSafe 的区别：那个返回 error（用于内部严格解析），
// 这个用于"解析不出来就当没有"的体检场景。
func atoiOrZero(text string) int {
	if text == "" {
		return 0
	}
	value := 0
	for _, character := range text {
		if character < '0' || character > '9' {
			return 0
		}
		value = value*10 + int(character-'0')
		if value > 1_000_000 {
			// 防御异常长的数字串
			return 0
		}
	}
	return value
}
