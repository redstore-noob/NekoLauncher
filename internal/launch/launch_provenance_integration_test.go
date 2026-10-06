package launch

import (
	"path/filepath"
	"strings"
	"testing"

	"nekolauncher/internal/auth"
)

// buildWithProvenanceFixture 走真实 Build 路径并打开溯源记账。
func buildWithProvenanceFixture(
	t *testing.T,
	options MinecraftLaunchOptions,
) ([]string, *LaunchProvenanceReport) {
	t.Helper()

	profile := versionProfileFixture()
	options.MinecraftDirectory = t.TempDir()
	options.VersionId = profile.Id
	options.Account = MustOfflineAccount("Steve")
	options.LauncherName = "NekoLauncher"
	options.LauncherVersion = "test"
	if options.MinimumMemoryMb == 0 {
		options.MinimumMemoryMb = 1024
	}
	if options.MaximumMemoryMb == 0 {
		options.MaximumMemoryMb = 4096
	}
	options.CollectProvenance = true

	// 插件变换的参数要在这里展开成 prepend/append 参数（与 launch_tool.go 同口径）
	var prependJvm, appendJvm, prependGame, appendGame []string
	if options.Transform != nil {
		prependJvm = options.Transform.PrependJvmArguments
		appendJvm = options.Transform.AppendJvmArguments
		prependGame = options.Transform.PrependGameArguments
		appendGame = options.Transform.AppendGameArguments
	}

	var report *LaunchProvenanceReport
	arguments, err := MinecraftArgumentBuilder{}.buildWithProvenance(
		profile, options,
		filepath.Join(t.TempDir(), "natives"),
		[]string{"lib.jar"},
		profile.MainClass,
		prependJvm, appendJvm, prependGame, appendGame,
		&report,
	)
	if err != nil {
		t.Fatalf("构建参数失败：%v", err)
	}
	if report == nil {
		// 对齐失败时把差异打出来：这是"记账点漏记/多记"的直接诊断
		profile := versionProfileFixture()
		options.MinecraftDirectory = t.TempDir()
		options.VersionId = profile.Id
		options.Account = MustOfflineAccount("Steve")
		options.LauncherName = "NekoLauncher"
		options.LauncherVersion = "test"
		options.CollectProvenance = true
		options.MinimumMemoryMb = 1024
		options.MaximumMemoryMb = 4096
		provenanceDebugEnabled = true
		_, _ = MinecraftArgumentBuilder{}.buildWithProvenance(
			profile, options,
			filepath.Join(t.TempDir(), "natives"),
			[]string{"lib.jar"}, profile.MainClass,
			nil, nil, nil, nil, nil)
		provenanceDebugEnabled = false
		t.Fatal("打开了记账却没有产出报告")
	}
	return arguments, report
}

// ---------------------------------------------------------------------------
// 报告与真实命令行必须严格对齐
// ---------------------------------------------------------------------------

func TestProvenanceAlignsWithRealCommandLine(t *testing.T) {
	arguments, report := buildWithProvenanceFixture(t, MinecraftLaunchOptions{})

	if len(report.Entries) != len(arguments) {
		t.Fatalf("报告条目 %d 条，实际命令行 %d 条——记账点与 append 点没对齐",
			len(report.Entries), len(arguments))
	}
	for index, entry := range report.Entries {
		if entry.Index != index {
			t.Errorf("Entries[%d].Index = %d", index, entry.Index)
		}
		if entry.Argument != arguments[index] {
			t.Errorf("下标 %d：报告 %q，实际 %q", index, entry.Argument, arguments[index])
		}
	}
}

func TestProvenanceDisabledByDefault(t *testing.T) {
	// 不开开关时绝不能有报告（零开销路径），且必须仍能正常构建
	profile := versionProfileFixture()
	var report *LaunchProvenanceReport
	arguments, err := MinecraftArgumentBuilder{}.buildWithProvenance(
		profile,
		MinecraftLaunchOptions{
			MinecraftDirectory: t.TempDir(),
			VersionId:          profile.Id,
			Account:            MustOfflineAccount("Steve"),
			MinimumMemoryMb:    1024,
			MaximumMemoryMb:    4096,
			LauncherName:       "NekoLauncher",
			LauncherVersion:    "test",
		},
		filepath.Join(t.TempDir(), "natives"),
		[]string{"lib.jar"}, profile.MainClass,
		nil, nil, nil, nil, &report,
	)
	if err != nil {
		t.Fatalf("构建失败：%v", err)
	}
	if report != nil {
		t.Error("未开启记账却产出了报告")
	}
	if len(arguments) == 0 {
		t.Error("未开启记账不应影响命令行生成")
	}
}

// ---------------------------------------------------------------------------
// 归因正确性
// ---------------------------------------------------------------------------

func TestProvenanceAttributesInstanceMemory(t *testing.T) {
	// 实例独立内存设置：报告必须说清 -Xmx 来自实例而不是全局
	_, report := buildWithProvenanceFixture(t, MinecraftLaunchOptions{
		MaximumMemoryMb:            2048,
		MemoryFromInstanceSettings: true,
	})

	entry := findProvenanceEntry(t, report, "-Xmx2048M")
	if entry.Source.Kind != SourceInstanceSettings {
		t.Errorf("来源 = %q，期望 %q", entry.Source.Kind, SourceInstanceSettings)
	}
	if entry.Source.Key != "memory.max.instance" {
		t.Errorf("来源键 = %q，期望 memory.max.instance", entry.Source.Key)
	}
	if entry.Source.Detail != "2048" {
		t.Errorf("来源明细 = %q，期望 2048", entry.Source.Detail)
	}
}

func TestProvenanceAttributesAutomaticMemory(t *testing.T) {
	_, report := buildWithProvenanceFixture(t, MinecraftLaunchOptions{
		MaximumMemoryMb:   3072,
		MemoryIsAutomatic: true,
	})

	entry := findProvenanceEntry(t, report, "-Xmx3072M")
	if entry.Source.Kind != SourceLauncherAuto {
		t.Errorf("来源 = %q，期望 %q", entry.Source.Kind, SourceLauncherAuto)
	}
	if entry.Source.Key != "memory.max.automatic" {
		t.Errorf("来源键 = %q，期望 memory.max.automatic", entry.Source.Key)
	}
}

func TestProvenanceAttributesUserJvmArgumentsToGlobal(t *testing.T) {
	_, report := buildWithProvenanceFixture(t, MinecraftLaunchOptions{
		AdditionalJvmArguments:    []string{"-Dfoo=bar"},
		UsingGlobalLaunchSettings: true,
	})

	entry := findProvenanceEntry(t, report, "-Dfoo=bar")
	if entry.Source.Kind != SourceGlobalSettings {
		t.Errorf("来源 = %q，期望全局设置", entry.Source.Kind)
	}
}

func TestProvenanceMarksBuiltinTuning(t *testing.T) {
	_, report := buildWithProvenanceFixture(t, MinecraftLaunchOptions{})

	entry := findProvenanceEntry(t, report, "-XX:+UseG1GC")
	if entry.Source.Kind != SourceLauncherAuto || entry.Source.Key != "jvm.tuning.g1" {
		t.Errorf("内置调优归因错误：%+v", entry.Source)
	}
}

func TestProvenanceClassifiesSections(t *testing.T) {
	arguments, report := buildWithProvenanceFixture(t, MinecraftLaunchOptions{})

	mainClassIndex := indexOfArgument(arguments, "net.minecraft.client.main.Main")
	if mainClassIndex < 0 {
		t.Fatal("命令行里找不到主类")
	}
	if entry := report.Entries[mainClassIndex]; entry.Section != sectionMainClass {
		t.Errorf("主类区段 = %q，期望 %q", entry.Section, sectionMainClass)
	}
	// 主类之前的最后一段 JVM 参数
	if entry := report.Entries[mainClassIndex-1]; entry.Section != sectionJVM {
		t.Errorf("主类前的区段 = %q，期望 %q", entry.Section, sectionJVM)
	}
	// 主类之后是游戏参数
	if entry := report.Entries[mainClassIndex+1]; entry.Section != sectionGame {
		t.Errorf("主类后的区段 = %q，期望 %q", entry.Section, sectionGame)
	}
}

// ---------------------------------------------------------------------------
// 真实世界的高频困惑：重复的 -Xmx
// ---------------------------------------------------------------------------

func TestProvenanceUserMemoryOverrideSuppressesBuiltin(t *testing.T) {
	// 用户手填 -Xmx 时，装配器**不会**再下发自己的 -Xmx（见 userSpecifiesXmx），
	// 所以这里不该出现重复参数、也不该有冲突。
	// 这条测试锁定该行为：如果有人"顺手"把内置 -Xmx 又加回来，
	// 面板会立刻多出一条"你的设置被覆盖了"的假警报。
	_, report := buildWithProvenanceFixture(t, MinecraftLaunchOptions{
		MaximumMemoryMb:            2048,
		MemoryFromInstanceSettings: true,
		AdditionalJvmArguments:     []string{"-Xmx6144M"},
	})

	if len(report.Conflicts) != 0 {
		t.Fatalf("手填 -Xmx 时不应产生重复下发：%+v", report.Conflicts)
	}
	// 生效值必须是用户手填的那条
	entry := findProvenanceEntry(t, report, "-Xmx6144M")
	if entry.Shadowed {
		t.Error("用户手填的 -Xmx 被标成了被覆盖")
	}
	// 设置里的 2048 不应该出现在命令行上
	for _, item := range report.Entries {
		if item.Argument == "-Xmx2048M" {
			t.Error("内置 -Xmx 未被抑制，出现了重复内存参数")
		}
	}
}

// 真出现重复参数时的冲突识别（装配器不会主动制造，但插件/手改档案会）
func TestProvenanceDetectsDuplicateFromTransform(t *testing.T) {
	// 插件通过 Transform 追加一条 -Xmx：这次真的有两条，必须报冲突
	arguments, report := buildWithProvenanceFixture(t, MinecraftLaunchOptions{
		MaximumMemoryMb:        2048,
		AdditionalJvmArguments: nil,
		Transform: &MinecraftLaunchTransform{
			AppendJvmArguments: []string{"-Xmx6144M"},
		},
		TransformPluginID:          "example-plugin",
		UsingGlobalLaunchSettings:  false,
		MemoryFromInstanceSettings: true,
	})

	// 内置 -Xmx2048M 在前，插件 -Xmx6144M 在后 → 插件生效
	var conflict *LaunchProvenanceConflict
	for index := range report.Conflicts {
		if report.Conflicts[index].Prefix == "-Xmx" {
			conflict = &report.Conflicts[index]

			break
		}
	}
	if conflict == nil {
		t.Fatalf("没检出 -Xmx 冲突：%+v", report.Conflicts)
	}
	if got := report.Entries[conflict.WinnerIndex].Argument; got != "-Xmx6144M" {
		t.Errorf("生效项 = %q，期望插件的 -Xmx6144M", got)
	}
	// 被覆盖的那条来自启动器自动下发，且必须能在最终命令行里找到证实
	if got := report.Entries[conflict.LoserIndices[0]].Argument; got != "-Xmx2048M" {
		t.Errorf("被覆盖项 = %q，期望内置 -Xmx2048M", got)
	}
	if indexOfArgument(arguments, "-Xmx2048M") < 0 {
		t.Error("报告说 -Xmx2048M 存在，但最终命令行里没有")
	}
	// 插件来源应被正确归因
	if entry := report.Entries[conflict.WinnerIndex]; entry.Source.Kind != SourcePlugin {
		t.Errorf("插件参数来源 = %q，期望 %q", entry.Source.Kind, SourcePlugin)
	}
}

func TestProvenanceCleanWhenNoConflict(t *testing.T) {
	_, report := buildWithProvenanceFixture(t, MinecraftLaunchOptions{})
	if len(report.Conflicts) != 0 {
		t.Errorf("默认配置不应有冲突，实际 %+v", report.Conflicts)
	}
	if summarizeProvenance(*report) != "provenance.summary.clean" {
		t.Errorf("摘要 = %q", summarizeProvenance(*report))
	}
}

// ---------------------------------------------------------------------------
// 令牌绝不进报告
// ---------------------------------------------------------------------------

func TestProvenanceNeverLeaksAccessToken(t *testing.T) {
	const secret = "eyJhbGciOiJIUzI1NiJ9.SUPER_SECRET_TOKEN.SIGNATURE"

	profile := versionProfileFixture()
	// 正版账号才会下发真实令牌
	account := auth.MicrosoftAccount{
		Username:    "Steve",
		Uuid:        "12345678123456781234567812345678",
		AccessToken: secret,
	}
	var report *LaunchProvenanceReport
	_, err := MinecraftArgumentBuilder{}.buildWithProvenance(
		profile,
		MinecraftLaunchOptions{
			MinecraftDirectory: t.TempDir(),
			VersionId:          profile.Id,
			Account:            account,
			MinimumMemoryMb:    1024,
			MaximumMemoryMb:    4096,
			LauncherName:       "NekoLauncher",
			LauncherVersion:    "test",
			CollectProvenance:  true,
		},
		filepath.Join(t.TempDir(), "natives"),
		[]string{"lib.jar"}, profile.MainClass,
		nil, nil, nil, nil, &report,
	)
	if err != nil {
		t.Fatalf("构建失败：%v", err)
	}
	if report == nil {
		t.Fatal("没产出报告")
	}

	for _, entry := range report.Entries {
		if strings.Contains(entry.Argument, secret) {
			t.Fatalf("令牌泄漏进溯源报告：%q", entry.Argument)
		}
	}
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

func findProvenanceEntry(t *testing.T, report *LaunchProvenanceReport, argument string) LaunchArgumentEntry {
	t.Helper()

	for _, entry := range report.Entries {
		if entry.Argument == argument {
			return entry
		}
	}
	t.Fatalf("报告里找不到参数 %q", argument)

	return LaunchArgumentEntry{}
}
