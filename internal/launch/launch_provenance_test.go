package launch

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 冲突分析
// ---------------------------------------------------------------------------

func TestAnalyzeProvenanceMarksShadowedMemoryArgument(t *testing.T) {
	// 真实场景：全局设置下发 -Xmx4096M，实例独立设置又下发 -Xmx2048M。
	// JVM 取后者，前者"用户改了没生效"——必须被标成被覆盖。
	entries := []LaunchArgumentEntry{
		{Argument: "-Xmx4096M", Source: LaunchArgumentSource{Kind: SourceGlobalSettings, Key: "memory.max.global"}},
		{Argument: "-Xmx2048M", Source: LaunchArgumentSource{Kind: SourceInstanceSettings, Key: "memory.max.instance"}},
	}
	report := analyzeProvenance(entries)

	if len(report.Conflicts) != 1 {
		t.Fatalf("期望 1 处冲突，实际 %d", len(report.Conflicts))
	}
	conflict := report.Conflicts[0]
	if conflict.Prefix != "-Xmx" {
		t.Errorf("冲突前缀 = %q，期望 -Xmx", conflict.Prefix)
	}
	if conflict.WinnerIndex != 1 {
		t.Errorf("生效下标 = %d，期望 1（后者生效）", conflict.WinnerIndex)
	}
	if len(conflict.LoserIndices) != 1 || conflict.LoserIndices[0] != 0 {
		t.Errorf("被覆盖下标 = %v，期望 [0]", conflict.LoserIndices)
	}
	if conflict.WinnerSource.Kind != SourceInstanceSettings {
		t.Errorf("生效来源 = %q，期望实例设置", conflict.WinnerSource.Kind)
	}

	if len(report.Overridden) != 1 || !report.Overridden[0].Shadowed {
		t.Fatalf("被覆盖视图 = %+v，期望恰好一条且 Shadowed=true", report.Overridden)
	}
	if report.Overridden[0].Argument != "-Xmx4096M" {
		t.Errorf("被覆盖项 = %q，期望 -Xmx4096M", report.Overridden[0].Argument)
	}
	if len(report.Effective) != 1 || report.Effective[0].Argument != "-Xmx2048M" {
		t.Fatalf("生效视图 = %+v，期望仅 -Xmx2048M", report.Effective)
	}
}

func TestAnalyzeProvenanceEntriesCarryFinalIndex(t *testing.T) {
	// 记账顺序即最终顺序，Index 必须与切片下标一致（前端按 Index 定位整条命令）
	entries := []LaunchArgumentEntry{
		{Argument: "-Xms512M"}, {Argument: "-Xmx2048M"}, {Argument: "-cp"}, {Argument: "a.jar"},
	}
	report := analyzeProvenance(entries)
	for index, entry := range report.Entries {
		if entry.Index != index {
			t.Errorf("Entries[%d].Index = %d，期望 %d", index, entry.Index, index)
		}
	}
}

func TestAnalyzeProvenanceNoFalseConflictForRepeatedFlags(t *testing.T) {
	// --server 不是"覆盖"语义（版本 JSON 与直连都可能下发），不应报冲突；
	// 但 --username 重复确实后者生效。这里验证"不参与的参数不会被误报"。
	entries := []LaunchArgumentEntry{
		{Argument: "--server", Source: LaunchArgumentSource{Kind: SourceDirectConnect}},
		{Argument: "play.example.com", Source: LaunchArgumentSource{Kind: SourceDirectConnect}},
		{Argument: "--port", Source: LaunchArgumentSource{Kind: SourceDirectConnect}},
		{Argument: "25565", Source: LaunchArgumentSource{Kind: SourceDirectConnect}},
	}
	report := analyzeProvenance(entries)
	if len(report.Conflicts) != 0 {
		t.Fatalf("期望无冲突，实际 %+v", report.Conflicts)
	}
	if len(report.Effective) != 4 {
		t.Errorf("生效条目 = %d，期望 4", len(report.Effective))
	}
}

func TestAnalyzeProvenanceDetectsSystemPropertyOverride(t *testing.T) {
	// -Dkey=value 同名后者覆盖；不同名互不影响
	entries := []LaunchArgumentEntry{
		{Argument: "-Dfml.ignoreInvalidMinecraftCertificates=true"},
		{Argument: "-Dfml.ignoreInvalidMinecraftCertificates=false"},
		{Argument: "-DlibraryDirectory=/x/libraries"},
	}
	report := analyzeProvenance(entries)
	if len(report.Conflicts) != 1 {
		t.Fatalf("期望 1 处冲突，实际 %d：%+v", len(report.Conflicts), report.Conflicts)
	}
	if report.Conflicts[0].Prefix != "-Dfml.ignoreInvalidMinecraftCertificates=" {
		t.Errorf("冲突前缀 = %q", report.Conflicts[0].Prefix)
	}
	if report.Conflicts[0].WinnerIndex != 1 {
		t.Errorf("生效下标 = %d，期望 1", report.Conflicts[0].WinnerIndex)
	}
}

func TestAnalyzeProvenanceTripleMemoryConflict(t *testing.T) {
	// 三条同前缀：最后一条生效，前两条都算被覆盖
	entries := []LaunchArgumentEntry{
		{Argument: "-Xmx1024M"}, {Argument: "-Xmx2048M"}, {Argument: "-Xmx3072M"},
	}
	report := analyzeProvenance(entries)
	if len(report.Overridden) != 2 {
		t.Fatalf("被覆盖 = %d，期望 2", len(report.Overridden))
	}
	if len(report.Conflicts[0].LoserIndices) != 2 {
		t.Errorf("LoserIndices = %v，期望两条", report.Conflicts[0].LoserIndices)
	}
	if report.Conflicts[0].WinnerIndex != 2 {
		t.Errorf("WinnerIndex = %d，期望 2", report.Conflicts[0].WinnerIndex)
	}
}

func TestAnalyzeProvenanceEmptyInputYieldsNonNilSlices(t *testing.T) {
	// 前端会直接 map 这些字段；nil 切片在 JS 侧是 undefined，必须给空数组
	report := analyzeProvenance(nil)
	if report.Entries == nil || report.Effective == nil ||
		report.Overridden == nil || report.Conflicts == nil {
		t.Fatalf("空输入必须返回非 nil 切片，实际 %+v", report)
	}
}

// ---------------------------------------------------------------------------
// 脱敏
// ---------------------------------------------------------------------------

func TestRedactArgumentSequenceHidesAccessTokenValue(t *testing.T) {
	// 真实形态："--accessToken <jwt>" 是**两个独立参数**，令牌在下标 +1
	arguments := []string{
		"--username", "Steve",
		"--accessToken", "eyJhbGciOiJIUzI1NiJ9.SECRET.SIGNATURE",
		"--uuid", "abc123",
	}
	redacted := redactArgumentSequence(arguments)

	joined := strings.Join(redacted, " ")
	if strings.Contains(joined, "SECRET") {
		t.Fatalf("令牌泄漏到溯源报告：%v", redacted)
	}
	if redacted[3] != "***" {
		t.Errorf("令牌位 = %q，期望 ***", redacted[3])
	}
	// 键名与非敏感值必须保留，否则面板无法解释
	if redacted[2] != "--accessToken" {
		t.Errorf("键名被破坏：%q", redacted[2])
	}
	if redacted[1] != "Steve" || redacted[5] != "abc123" {
		t.Errorf("非敏感值被误改：%v", redacted)
	}
	// 原切片不得被就地修改（调用方可能还在用）
	if arguments[3] != "eyJhbGciOiJIUzI1NiJ9.SECRET.SIGNATURE" {
		t.Error("脱敏就地修改了入参切片")
	}
}

func TestRedactArgumentHidesEqualsForm(t *testing.T) {
	if got := redactArgument("--accessToken=SECRET"); got != "--accessToken=***" {
		t.Errorf("等号形态 = %q", got)
	}
	if got := redactArgument("-Xmx2048M"); got != "-Xmx2048M" {
		t.Errorf("普通参数被误改：%q", got)
	}
}

func TestRedactArgumentSequenceHandlesTrailingKeyWithoutValue(t *testing.T) {
	// 末尾只有一个键、没有配对值：不得越界 panic
	redacted := redactArgumentSequence([]string{"--username", "Steve", "--accessToken"})
	if len(redacted) != 3 {
		t.Fatalf("长度变化：%v", redacted)
	}
	if redacted[2] != "--accessToken" {
		t.Errorf("末尾键被破坏：%q", redacted[2])
	}
}

// ---------------------------------------------------------------------------
// 记账器
// ---------------------------------------------------------------------------

func TestProvenanceRecorderNilSafe(t *testing.T) {
	// 未启用溯源时必须是零开销空转，绝不能 panic
	var recorder *provenanceRecorder
	recorder.section("jvm")
	recorder.record([]string{"-Xmx2048M"}, LaunchArgumentSource{Kind: SourceLauncherAuto})
	recorder.recordOne("-cp", LaunchArgumentSource{Kind: SourceLauncherAuto})
	if recorder != nil {
		t.Fatal("nil 记账器不应被赋值")
	}
}

func TestProvenanceRecorderSkipsBlankArguments(t *testing.T) {
	recorder := &provenanceRecorder{}
	recorder.section("jvm")
	recorder.record([]string{"-Xmx2048M", "", "   "}, LaunchArgumentSource{Kind: SourceLauncherAuto})
	if len(recorder.entries) != 1 {
		t.Fatalf("空白参数应被跳过，实际 %d 条", len(recorder.entries))
	}
	if recorder.entries[0].Section != "jvm" {
		t.Errorf("区段 = %q，期望 jvm", recorder.entries[0].Section)
	}
}

func TestConflictPrefixOfIgnoresBareFlags(t *testing.T) {
	// 裸 -Xmx（无数值）不构成"覆盖"语义，交给 validateMemory 报错
	if got := conflictPrefixOf("-Xmx"); got != "" {
		t.Errorf("裸 -Xmx 前缀 = %q，期望空", got)
	}
	if got := conflictPrefixOf("-Xmx2048M"); got != "-Xmx" {
		t.Errorf("-Xmx2048M 前缀 = %q", got)
	}
	if got := conflictPrefixOf("-Dfoo"); got != "" {
		t.Errorf("无等号的 -Dfoo 前缀 = %q，期望空", got)
	}
	if got := conflictPrefixOf("--width"); got != "--width" {
		t.Errorf("--width 前缀 = %q", got)
	}
}
