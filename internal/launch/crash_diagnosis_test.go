package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleCrashReport = `---- Minecraft Crash Report ----
// Who set us up the TNT?

Time: 2026-09-24 19:40:11
Description: Rendering overlay

java.lang.OutOfMemoryError: Java heap space
	at net.minecraft.client.renderer.LevelRenderer.renderLevel(LevelRenderer.java:1234)
	at net.minecraft.client.Minecraft.runTick(Minecraft.java:2100)

-- System Details --
Minecraft Version: 1.21.1
Java Version: 21.0.4, Oracle Corporation
`

// TestDiagnoseCrashFromReport 崩溃报告存在时：要认出报告、Description、异常行，
// 并按内容给出"内存不足"的结论与建议。
func TestDiagnoseCrashFromReport(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "crash-2026-09-24_19.40.11-client.txt"), []byte(sampleCrashReport), 0o644); err != nil {
		t.Fatalf("写崩溃报告失败：%v", err)
	}

	diagnosis := DiagnoseCrash(CrashDiagnosisInput{
		CrashReportsDirectory: directory,
		LogText:               "[19:40:10] [Render thread/INFO]: Setting user",
		MinecraftVersion:      "1.21.1",
		JavaVersion:           "21.0.4",
		MemoryMb:              2048,
	})

	if diagnosis.ReportPath == "" {
		t.Fatal("应当识别到崩溃报告")
	}
	if diagnosis.Description != "Rendering overlay" {
		t.Fatalf("Description = %q", diagnosis.Description)
	}
	if !strings.Contains(diagnosis.Exception, "OutOfMemoryError") {
		t.Fatalf("Exception = %q", diagnosis.Exception)
	}
	if len(diagnosis.Suspected) == 0 || !strings.Contains(diagnosis.Suspected[0], "内存") {
		t.Fatalf("应判定为内存不足：%+v", diagnosis.Suspected)
	}
	if len(diagnosis.Suggestions) != len(diagnosis.Suspected) {
		t.Fatalf("建议数量应与原因一一对应：%+v / %+v", diagnosis.Suspected, diagnosis.Suggestions)
	}
	if diagnosis.Summary == "" {
		t.Fatal("结论文案不应为空")
	}
}

// TestDiagnoseCrashFromLogOnly 没有崩溃报告（例如服务端日志被删）时，
// 也要能从启动日志里得出结论。
func TestDiagnoseCrashFromLogOnly(t *testing.T) {
	logText := strings.Join([]string{
		"[19:00:00] [main/INFO]: Loading Minecraft 1.21.1 with Fabric Loader 0.19.5",
		"[19:00:03] [main/ERROR]: Missing or unsupported mandatory dependencies:",
		"\tMod ID: 'architectury', Requested by: 'mymod', Expected range: '*', Actual version: '[MISSING]'",
	}, "\n")

	diagnosis := DiagnoseCrash(CrashDiagnosisInput{LogText: logText})

	if diagnosis.ReportPath != "" {
		t.Fatalf("没有报告目录时不应给出路径：%q", diagnosis.ReportPath)
	}
	joined := strings.Join(diagnosis.Suspected, " | ")
	if !strings.Contains(joined, "前置") {
		t.Fatalf("应判定为缺前置：%+v", diagnosis.Suspected)
	}
	if diagnosis.Summary == "" || !strings.Contains(diagnosis.Summary, "启动日志") {
		t.Fatalf("结论应说明来源是启动日志：%q", diagnosis.Summary)
	}
}

// TestDiagnoseCrashMultipleCauses 多条规则同时命中时都要给出（例如缺前置 + 内存不足）。
func TestDiagnoseCrashMultipleCauses(t *testing.T) {
	logText := "java.lang.UnsupportedClassVersionError: net/fabricmc/loader/impl/launch/knot/KnotClient has been compiled by a more recent version\n" +
		"java.lang.OutOfMemoryError: Java heap space\n"

	diagnosis := DiagnoseCrash(CrashDiagnosisInput{LogText: logText})

	if len(diagnosis.Suspected) < 2 {
		t.Fatalf("应同时命中 Java 版本与内存两条：%+v", diagnosis.Suspected)
	}
	if len(diagnosis.Suggestions) != len(diagnosis.Suspected) {
		t.Fatalf("建议与原因数量不一致：%d / %d", len(diagnosis.Suspected), len(diagnosis.Suggestions))
	}
}

// TestDiagnoseCrashNoEvidence 什么都没命中时也要有可展示的结论，不能空着。
func TestDiagnoseCrashNoEvidence(t *testing.T) {
	diagnosis := DiagnoseCrash(CrashDiagnosisInput{LogText: "[19:00:00] [main/INFO]: 一切正常"})

	if len(diagnosis.Suspected) != 0 {
		t.Fatalf("不该误报原因：%+v", diagnosis.Suspected)
	}
	if diagnosis.Summary == "" {
		t.Fatal("无证据时也要给结论文案")
	}
	// JSON 里必须是空数组而不是 null（前端直接 map）
	if diagnosis.Suspected == nil || diagnosis.Suggestions == nil {
		t.Fatal("Suspected / Suggestions 应为空切片而非 nil")
	}
}

// TestDiagnoseCrashIgnoresEarlyLogNoise 日志早段出现过的错误字样（例如中途
// 进服被拒的 "Invalid session"）不应触发对应诊断——只看末尾，防误报。
func TestDiagnoseCrashIgnoresEarlyLogNoise(t *testing.T) {
	logText := "[19:00:00] [main/ERROR]: Invalid session for player Herobrine\n" +
		"[19:00:05] [main/INFO]: All mods loaded successfully\n"
	// 末尾再垫 300 行足够长的正常日志（总量超过 16KB 字节窗口），
	// 把错误字样彻底推出"末尾 200 行"窗口
	for range 300 {
		logText += "[19:05:00] [main/INFO]: game tick normal, all systems nominal, padding padding padding\n"
	}

	diagnosis := DiagnoseCrash(CrashDiagnosisInput{LogText: logText})

	joined := strings.Join(diagnosis.Suspected, " | ")
	if strings.Contains(joined, "会话") {
		t.Fatalf("日志早段的 Invalid session 不应触发会话失效诊断：%+v", diagnosis.Suspected)
	}

	// 反例：同一字样出现在末尾时仍要能命中
	fresh := "[19:00:00] [main/INFO]: game starting\n[19:59:59] [main/ERROR]: Invalid session for player Herobrine"
	diagnosis = DiagnoseCrash(CrashDiagnosisInput{LogText: fresh})
	if !strings.Contains(strings.Join(diagnosis.Suspected, " | "), "会话") {
		t.Fatalf("末尾出现 Invalid session 应命中会话失效：%+v", diagnosis.Suspected)
	}
}

// TestDiagnoseCrashNewRules 新增规则的代表性命中：Mixin / 磁盘 / 权限 / Java 启动 / 存档。
func TestDiagnoseCrashNewRules(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"org.spongepowered.asm.mixin.throwables.MixinApplyError: Mixin apply failed", "Mixin"},
		{"java.io.IOException: No space left on device", "磁盘"},
		{"java.io.FileNotFoundException: options.txt (Access is denied)", "权限"},
		{"java.io.IOException: Cannot run program \"javaw\": CreateProcess error=2", "Java 进程"},
		{"Failed to load level data: world/level.dat", "存档"},
	}
	for _, tc := range cases {
		diagnosis := DiagnoseCrash(CrashDiagnosisInput{LogText: tc.line})
		joined := strings.Join(diagnosis.Suspected, " | ")

		if !strings.Contains(joined, tc.want) {
			t.Errorf("%q 应命中 %q，实际：%+v", tc.line, tc.want, diagnosis.Suspected)
		}
	}
}

// TestDiagnoseCrashJVMStartupFailure "点了启动，窗口一闪就关"这一类：JVM 在
// 初始化阶段就退出，不进游戏、不生成 crash-report，只有 stderr 几行。
// 这里用真机抓到的原始输出做用例（Java 21 收到 Java 25 才有的长选项）。
func TestDiagnoseCrashJVMStartupFailure(t *testing.T) {
	// 日志行前面带 [GAME][stderr] 前缀，异常行规则匹配不到行首，必须靠专门的规则
	unknownOption := strings.Join([]string{
		"[12:00:00][LAUNCH]Java 进程已启动，进程 ID：40180。",
		"[12:00:01][GAME][stderr] Unrecognized option: --sun-misc-unsafe-memory-access=allow",
		"[12:00:01][GAME][stderr] Error: Could not create the Java Virtual Machine.",
		"[12:00:01][GAME][stderr] Error: A fatal exception has occurred. Program will exit.",
		"[12:00:01][LAUNCH]游戏进程已退出，退出代码：1。",
	}, "\n")
	diagnosis := DiagnoseCrash(CrashDiagnosisInput{LogText: unknownOption})
	suspected := strings.Join(diagnosis.Suspected, " | ")

	if !strings.Contains(suspected, "JVM 启动参数") {
		t.Fatalf("应命中 JVM 启动参数不兼容，实际：%+v", diagnosis.Suspected)
	}
	// 具体是哪条参数必须原样出现在 Details 里——用户要据此删掉它
	if !hasDetail(diagnosis, "Unrecognized option: --sun-misc-unsafe-memory-access=allow") {
		t.Fatalf("Details 应含被拒的参数行，实际：%+v", diagnosis.Details)
	}

	// 老版本启动器残留的 Java 9 已移除参数（真机日志里出现过的那条）
	removedFlag := "[12:00:01][GAME][stderr] Unrecognized VM option 'UseFastAccessorMethods'"
	if got := DiagnoseCrash(CrashDiagnosisInput{LogText: removedFlag}); !strings.Contains(
		strings.Join(got.Suspected, " | "), "JVM 启动参数") {
		t.Fatalf("Unrecognized VM option 应命中 JVM 启动参数，实际：%+v", got.Suspected)
	}

	// 堆要不到内存（-Xmx 比机器能给的大）属于同一类"初始化就退出"，但处置不同
	heap := strings.Join([]string{
		"Error occurred during initialization of VM",
		"Could not reserve enough space for 8388608KB object heap",
	}, "\n")
	heapDiagnosis := DiagnoseCrash(CrashDiagnosisInput{LogText: heap})
	if got := strings.Join(heapDiagnosis.Suspected, " | "); !strings.Contains(got, "内存设置超出系统可用范围") {
		t.Fatalf("堆保留失败应命中内存设置规则，实际：%+v", heapDiagnosis.Suspected)
	}

	// 精度：泛化的收尾行单独出现时不该被当成 JVM 参数问题（它伴随所有 VM 初始化失败）
	if got := DiagnoseCrash(CrashDiagnosisInput{
		LogText: "Error: A fatal exception has occurred. Program will exit.",
	}); len(got.Suspected) != 0 {
		t.Fatalf("只有通用收尾行时不应给出结论，实际：%+v", got.Suspected)
	}
}

// hasDetail 判断 Details 里是否出现了某条证据（去空白后比较，允许截断标记）。
func hasDetail(diagnosis CrashDiagnosis, want string) bool {
	for _, detail := range diagnosis.Details {
		if strings.Contains(detail, want) {
			return true
		}
	}

	return false
}

// TestLoadNewestCrashReport 多份报告时取最新的一份，且忽略非 .txt。
func TestLoadNewestCrashReport(t *testing.T) {
	directory := t.TempDir()
	older := filepath.Join(directory, "crash-old.txt")
	newer := filepath.Join(directory, "crash-new.txt")

	if err := os.WriteFile(older, []byte("Description: old\n"), 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	if err := os.WriteFile(newer, []byte("Description: new\n"), 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "hs_err_pid1234.log"), []byte("x"), 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	// 明确拉开修改时间，避免同秒写入导致排序不稳定
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatalf("改时间失败：%v", err)
	}

	report := loadNewestCrashReport(directory)
	if report.path != newer {
		t.Fatalf("应取最新的 .txt：%q", report.path)
	}
	if report.description != "new" {
		t.Fatalf("Description = %q", report.description)
	}

	// 目录不存在时零值返回，不 panic
	if empty := loadNewestCrashReport(filepath.Join(directory, "missing")); empty.path != "" {
		t.Fatalf("目录不存在时应返回零值：%+v", empty)
	}
	if empty := loadNewestCrashReport(""); empty.path != "" {
		t.Fatalf("空目录参数应返回零值：%+v", empty)
	}
}

// TestDiagnoseCrashFabricIncompatibleMods Fabric Loader 的图形化"不兼容模组"
// 报错（只写日志、不生成 crash-report）也要能认出缺前置，并把具体报错行
// （哪个模组缺什么依赖）放进 Details。
func TestDiagnoseCrashFabricIncompatibleMods(t *testing.T) {
	logText := strings.Join([]string{
		"[19:00:00] [main/INFO]: Loading Minecraft 1.21.1 with Fabric Loader 0.19.5",
		"[19:00:03] [main/ERROR]: Incompatible mods found!",
		"[19:00:03] [main/ERROR]: Some of your mods are incompatible with the game or each other!",
		"[19:00:03] [main/ERROR]: \tMod 'BetterGrassify' (bettergrass) 1.8.8+fabric.26.3 requires fabric-api any version, but it's missing!",
	}, "\n")

	diagnosis := DiagnoseCrash(CrashDiagnosisInput{LogText: logText})

	joined := strings.Join(diagnosis.Suspected, " | ")
	if !strings.Contains(joined, "前置") {
		t.Fatalf("应判定为缺前置/模组冲突：%+v", diagnosis.Suspected)
	}
	if len(diagnosis.Details) == 0 {
		t.Fatal("应摘出具体报错行到 Details")
	}
	foundDependency := false
	for _, detail := range diagnosis.Details {
		if strings.Contains(detail, "bettergrass") && strings.Contains(detail, "fabric-api") {
			foundDependency = true
		}
	}
	if !foundDependency {
		t.Fatalf("Details 应包含 bettergrass 缺 fabric-api 的明细行：%+v", diagnosis.Details)
	}
	if diagnosis.Details == nil {
		t.Fatal("Details 应为空切片而非 nil")
	}
}
