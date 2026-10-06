package content

import (
	"os"
	"path/filepath"
	"testing"
)

func findFinding(health InstanceHealth, kind HealthFindingKind) *HealthFinding {
	for index := range health.Findings {
		if health.Findings[index].Kind == kind {
			return &health.Findings[index]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 健康实例：不该扣分
// ---------------------------------------------------------------------------

func TestHealthyInstanceScoresFull(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "sodium.jar",
		`{"id":"sodium","name":"Sodium","version":"1.0.0","depends":{"minecraft":"1.20.x"}}`)

	health := RunInstanceHealth(HealthInput{
		ModsDirectory:             directory,
		MinecraftVersion:          "1.20.1",
		LoaderName:                "fabric",
		RequiredJavaMajor:         17,
		CurrentJavaMajor:          17,
		ConfiguredMaximumMemoryMb: 4096,
		SystemTotalMemoryMb:       16384,
	})

	if health.Score != 100 {
		t.Errorf("一切正常时分数 = %d，期望 100（结论：%+v）", health.Score, health.Findings)
	}
	if health.Grade != GradeHealthy {
		t.Errorf("等级 = %q，期望 healthy", health.Grade)
	}
	if len(health.Findings) != 0 {
		t.Errorf("不该有任何结论：%+v", health.Findings)
	}
}

// ---------------------------------------------------------------------------
// 未知数据不扣分 —— 这是体检功能最容易做错的地方
// ---------------------------------------------------------------------------

func TestUnknownDataNeverDeducts(t *testing.T) {
	// 全部字段未知：没有任何依据就下结论，用户会无从下手
	health := RunInstanceHealth(HealthInput{})

	if health.Score != 100 {
		t.Errorf("数据全未知时分数 = %d，期望 100（不该凭空扣分）", health.Score)
	}
	if health.Grade != GradeHealthy {
		t.Errorf("等级 = %q，期望 healthy", health.Grade)
	}
}

func TestUnknownJavaSkipsComparison(t *testing.T) {
	// 当前 Java 未知：不能报"版本不匹配"（我们不知道它是什么版本）
	health := RunInstanceHealth(HealthInput{
		RequiredJavaMajor: 17,
		CurrentJavaMajor:  0,
	})
	// 但"该版本明确要求 Java 17、而一个都没配"是确定的事实，值得提示
	finding := findFinding(health, FindingNoJavaConfigured)
	if finding == nil {
		t.Fatalf("该报没配 Java：%+v", health.Findings)
	}
	if finding.Severity != SeverityWarning {
		t.Errorf("严重度 = %q，期望 warning", finding.Severity)
	}
	if findFinding(health, FindingJavaMismatch) != nil {
		t.Error("当前 Java 未知时不该报版本不匹配")
	}
}

func TestUnknownRequiredJavaSkipsCheck(t *testing.T) {
	health := RunInstanceHealth(HealthInput{
		RequiredJavaMajor: 0,
		CurrentJavaMajor:  17,
	})
	if len(health.Findings) != 0 {
		t.Errorf("版本要求未知时不该报 Java 问题：%+v", health.Findings)
	}
}

func TestUnknownMemorySkipsCheck(t *testing.T) {
	// 内存未配置（0）：不能报"内存偏低"
	health := RunInstanceHealth(HealthInput{ConfiguredMaximumMemoryMb: 0})
	if findFinding(health, FindingMemoryLow) != nil {
		t.Error("内存未配置时不该报内存问题")
	}
}

// ---------------------------------------------------------------------------
// Java 版本
// ---------------------------------------------------------------------------

func TestJavaTooLowIsBlocking(t *testing.T) {
	health := RunInstanceHealth(HealthInput{
		RequiredJavaMajor: 17,
		CurrentJavaMajor:  8,
	})

	finding := findFinding(health, FindingJavaMismatch)
	if finding == nil {
		t.Fatal("Java 过低必须报出来")
	}
	if finding.Severity != SeverityError {
		t.Errorf("Java 过低应为 Error，实际 %q", finding.Severity)
	}
	if finding.Detail != "17|8" {
		t.Errorf("Detail = %q，期望 17|8（需要|实际）", finding.Detail)
	}
	if health.BlockingCount == 0 {
		t.Error("BlockingCount 应大于 0")
	}
}

func TestJavaTooHighIsOnlyWarning(t *testing.T) {
	// 高版本 Java 多数情况能跑（旧 Forge 例外），不该当成阻断项
	health := RunInstanceHealth(HealthInput{
		RequiredJavaMajor: 8,
		CurrentJavaMajor:  21,
	})

	finding := findFinding(health, FindingJavaMismatch)
	if finding == nil {
		t.Fatal("Java 过高也应提示")
	}
	if finding.Severity != SeverityWarning {
		t.Errorf("Java 过高应为 Warning，实际 %q", finding.Severity)
	}
}

// ---------------------------------------------------------------------------
// 内存
// ---------------------------------------------------------------------------

func TestLowMemoryWarns(t *testing.T) {
	health := RunInstanceHealth(HealthInput{
		ConfiguredMaximumMemoryMb: 1024,
		SystemTotalMemoryMb:       16384,
	})

	finding := findFinding(health, FindingMemoryLow)
	if finding == nil {
		t.Fatal("1 GiB 内存应报偏低")
	}
	if finding.Detail != "1024" {
		t.Errorf("Detail = %q，期望 1024", finding.Detail)
	}
}

func TestMemoryAboveSystemTotalWarns(t *testing.T) {
	health := RunInstanceHealth(HealthInput{
		ConfiguredMaximumMemoryMb: 32768,
		SystemTotalMemoryMb:       16384,
	})

	finding := findFinding(health, FindingMemoryExcessive)
	if finding == nil {
		t.Fatal("内存超过物理内存应报出来")
	}
	if finding.Detail != "32768|16384" {
		t.Errorf("Detail = %q，期望 32768|16384", finding.Detail)
	}
}

// ---------------------------------------------------------------------------
// 与 mod 冲突的整合
// ---------------------------------------------------------------------------

func TestHealthIncludesModConflicts(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "dup1.jar",
		`{"id":"dup","name":"Dup","version":"1.0.0"}`)
	writeFabricMod(t, directory, "dup2.jar",
		`{"id":"dup","name":"Dup","version":"2.0.0"}`)

	health := RunInstanceHealth(HealthInput{ModsDirectory: directory})

	if findFinding(health, FindingDuplicateMod) == nil {
		t.Fatalf("重复 mod 没进体检结论：%+v", health.Findings)
	}
	if health.Score >= 100 {
		t.Error("有重复 mod 时不该是满分")
	}
	if health.AnalyzedMods != 2 {
		t.Errorf("已分析 mod 数 = %d，期望 2", health.AnalyzedMods)
	}
}

func TestHealthBlockingLowersGradeEvenWithHighScore(t *testing.T) {
	// 只有一个缺前置（扣 25 → 75 分），但它必然起不来：
	// 等级必须落到 poor 及以下，不能被平均分掩盖
	directory := modsDir(t)
	writeFabricMod(t, directory, "needs.jar",
		`{"id":"needs","name":"Needs","version":"1.0.0","depends":{"absent":"*"}}`)

	health := RunInstanceHealth(HealthInput{
		ModsDirectory:             directory,
		ConfiguredMaximumMemoryMb: 4096,
	})

	if health.BlockingCount == 0 {
		t.Fatal("缺前置应记为阻断项")
	}
	if health.Grade == GradeHealthy || health.Grade == GradeFair {
		t.Errorf("有阻断项时等级 = %q，期望 poor/critical", health.Grade)
	}
}

func TestScoreNeverGoesNegative(t *testing.T) {
	// 堆满严重问题：分数下限必须是 0，不能出现负数
	directory := modsDir(t)
	writeFabricMod(t, directory, "dup1.jar", `{"id":"dup","name":"D","version":"1"}`)
	writeFabricMod(t, directory, "dup2.jar", `{"id":"dup","name":"D","version":"2"}`)
	writeFabricMod(t, directory, "a.jar",
		`{"id":"a","name":"A","version":"1","depends":{"gone1":"*","gone2":"*"}}`)
	writeFabricMod(t, directory, "b.jar",
		`{"id":"b","name":"B","version":"1","breaks":{"a":"*"}}`)

	health := RunInstanceHealth(HealthInput{
		ModsDirectory:             directory,
		MinecraftVersion:          "1.20.1",
		LoaderName:                "forge",
		RequiredJavaMajor:         21,
		CurrentJavaMajor:          8,
		ConfiguredMaximumMemoryMb: 512,
		SystemTotalMemoryMb:       4096,
	})

	if health.Score < 0 {
		t.Errorf("分数 = %d，不能为负", health.Score)
	}
	if health.Grade != GradeCritical {
		t.Errorf("等级 = %q，期望 critical", health.Grade)
	}
}

// ---------------------------------------------------------------------------
// 仅提示项
// ---------------------------------------------------------------------------

func TestDisabledModsIsInformational(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "off.jar",
		`{"id":"off","name":"Off","version":"1.0.0"}`)
	writeFabricMod(t, directory, "off2.jar.disabled",
		`{"id":"off2","name":"Off2","version":"1.0.0"}`)

	health := RunInstanceHealth(HealthInput{
		ModsDirectory:    directory,
		DisabledModCount: CountDisabledMods(directory),
	})

	finding := findFinding(health, FindingDisabledMods)
	if finding == nil {
		t.Fatal("有禁用 mod 时应给一条提示")
	}
	// 用户主动禁用 mod 是正常操作，不该扣分
	if finding.Deduction != 0 {
		t.Errorf("禁用 mod 不该扣分，实际扣 %d", finding.Deduction)
	}
	if finding.Severity != SeverityInfo {
		t.Errorf("严重度 = %q，期望 info", finding.Severity)
	}
}

func TestUnreadableModsDeductsSlightly(t *testing.T) {
	directory := modsDir(t)
	if err := os.WriteFile(filepath.Join(directory, "broken.jar"), []byte("junk"), 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}

	health := RunInstanceHealth(HealthInput{ModsDirectory: directory})

	finding := findFinding(health, FindingUnreadableMods)
	if finding == nil {
		t.Fatal("读不出元数据的 jar 应给提示（说明检测覆盖不全）")
	}
	if health.UnreadableMods != 1 {
		t.Errorf("不可读计数 = %d，期望 1", health.UnreadableMods)
	}
	// 只是"看不全"，不代表有问题：扣分要非常轻
	if finding.Deduction > 5 {
		t.Errorf("扣分 = %d，过重（读不出元数据不等于坏）", finding.Deduction)
	}
}

// ---------------------------------------------------------------------------
// 排序稳定性
// ---------------------------------------------------------------------------

func TestFindingsSortedBySeverity(t *testing.T) {
	directory := modsDir(t)
	// 造一个 error（缺前置）+ 一个 info（禁用 mod）
	writeFabricMod(t, directory, "needs.jar",
		`{"id":"needs","name":"Needs","version":"1.0.0","depends":{"absent":"*"}}`)
	writeFabricMod(t, directory, "off.jar.disabled",
		`{"id":"off","name":"Off","version":"1.0.0"}`)

	health := RunInstanceHealth(HealthInput{
		ModsDirectory:    directory,
		DisabledModCount: CountDisabledMods(directory),
	})

	if len(health.Findings) < 2 {
		t.Fatalf("期望至少两条结论：%+v", health.Findings)
	}
	if health.Findings[0].Severity != SeverityError {
		t.Errorf("首条严重度 = %q，期望 error 排最前", health.Findings[0].Severity)
	}
	last := health.Findings[len(health.Findings)-1]
	if last.Severity != SeverityInfo {
		t.Errorf("末条严重度 = %q，期望 info 排最后", last.Severity)
	}
}

func TestRunInstanceHealthDeterministic(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "a.jar",
		`{"id":"a","name":"A","version":"1","depends":{"x":"*","y":"*"}}`)
	writeFabricMod(t, directory, "b.jar",
		`{"id":"b","name":"B","version":"1","depends":{"z":"*"}}`)

	input := HealthInput{ModsDirectory: directory}
	first := RunInstanceHealth(input)
	second := RunInstanceHealth(input)

	if len(first.Findings) != len(second.Findings) {
		t.Fatalf("两次条数不同：%d vs %d", len(first.Findings), len(second.Findings))
	}
	for index := range first.Findings {
		left, right := first.Findings[index], second.Findings[index]
		if left.Kind != right.Kind || left.Subject != right.Subject ||
			left.Severity != right.Severity || left.Deduction != right.Deduction {
			t.Fatalf("第 %d 条不稳定：%+v vs %+v", index, left, right)
		}
	}
}

// ---------------------------------------------------------------------------
// 辅助函数
// ---------------------------------------------------------------------------

func TestDetectRequiredJavaMajor(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"Java 17", 17},
		{"17", 17},
		{">=17", 17},
		{"Java 8", 8},
		{"需要 Java 21", 21},
		{"自动检测", 0},
		{"", 0},
		{"Java", 0},
	}
	for _, testCase := range cases {
		if got := DetectRequiredJavaMajor(testCase.input); got != testCase.want {
			t.Errorf("DetectRequiredJavaMajor(%q) = %d，期望 %d",
				testCase.input, got, testCase.want)
		}
	}
}

func TestCountDisabledMods(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "on.jar", `{"id":"on","name":"On","version":"1"}`)
	writeFabricMod(t, directory, "off1.jar.disabled", `{"id":"o1","name":"O1","version":"1"}`)
	writeFabricMod(t, directory, "off2.jar.disabled", `{"id":"o2","name":"O2","version":"1"}`)

	if got := CountDisabledMods(directory); got != 2 {
		t.Errorf("禁用数 = %d，期望 2", got)
	}
	if got := CountDisabledMods(filepath.Join(t.TempDir(), "nope")); got != 0 {
		t.Errorf("目录不存在时应为 0，实际 %d", got)
	}
}

func TestModsDirectoryFor(t *testing.T) {
	if got := ModsDirectoryFor("/x/versions/inst"); got != filepath.Join("/x/versions/inst", "mods") {
		t.Errorf("ModsDirectoryFor = %q", got)
	}
	if got := ModsDirectoryFor(""); got != "" {
		t.Errorf("空目录应返回空串，实际 %q", got)
	}
}
