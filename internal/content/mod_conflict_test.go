package content

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// 测试夹具：造真实的 mod jar
// ---------------------------------------------------------------------------

// writeModJar 造一个含指定元数据文件的 jar。
func writeModJar(t *testing.T, directory, fileName, metadataName, metadataContent string) {
	t.Helper()

	path := filepath.Join(directory, fileName)
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建 jar 失败：%v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	entry, err := writer.Create(metadataName)
	if err != nil {
		t.Fatalf("创建条目失败：%v", err)
	}
	if _, err := entry.Write([]byte(metadataContent)); err != nil {
		t.Fatalf("写入条目失败：%v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭 zip 失败：%v", err)
	}
}

// writeFabricMod 造一个 fabric.mod.json 的 jar。
func writeFabricMod(t *testing.T, directory, fileName, jsonContent string) {
	t.Helper()

	writeModJar(t, directory, fileName, "fabric.mod.json", jsonContent)
}

func modsDir(t *testing.T) string {
	t.Helper()

	directory := filepath.Join(t.TempDir(), "mods")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("建 mods 目录失败：%v", err)
	}
	return directory
}

// findConflict 找指定 kind + subject 的结论。
func findConflict(report ModConflictReport, kind, subject string) *ModConflict {
	for index := range report.Conflicts {
		if report.Conflicts[index].Kind == kind && report.Conflicts[index].Subject == subject {
			return &report.Conflicts[index]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 重复 mod
// ---------------------------------------------------------------------------

func TestDetectDuplicateMods(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "sodium-1.0.jar",
		`{"id":"sodium","name":"Sodium","version":"1.0.0"}`)
	writeFabricMod(t, directory, "sodium-2.0.jar",
		`{"id":"sodium","name":"Sodium","version":"2.0.0"}`)

	report := AnalyzeModConflicts(directory, "", "")

	conflict := findConflict(report, "duplicate-mod", "sodium")
	if conflict == nil {
		t.Fatalf("没检出重复 mod：%+v", report.Conflicts)
	}
	if conflict.Severity != SeverityError {
		t.Errorf("重复 mod 必须是 Error，实际 %q", conflict.Severity)
	}
	if len(conflict.Files) != 2 {
		t.Errorf("涉及文件 = %v，期望两个", conflict.Files)
	}
	if !report.HasBlockingConflict() {
		t.Error("HasBlockingConflict 应为 true")
	}
}

func TestDuplicateModsIgnoresDisabledFiles(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "sodium-1.0.jar",
		`{"id":"sodium","name":"Sodium","version":"1.0.0"}`)
	// 用户已经禁用旧版本，这正是解决重复 mod 的标准做法
	writeFabricMod(t, directory, "sodium-2.0.jar.disabled",
		`{"id":"sodium","name":"Sodium","version":"2.0.0"}`)

	report := AnalyzeModConflicts(directory, "", "")
	if findConflict(report, "duplicate-mod", "sodium") != nil {
		t.Error("已禁用的 jar 不该计入重复 mod（否则告警永远消不掉）")
	}
}

// ---------------------------------------------------------------------------
// 缺前置
// ---------------------------------------------------------------------------

func TestDetectMissingDependency(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "needs-api.jar", `{
		"id":"needs-api","name":"需要前置","version":"1.0.0",
		"depends":{"fabric-api":"*","minecraft":"1.20.x"}
	}`)

	report := AnalyzeModConflicts(directory, "", "")

	conflict := findConflict(report, "missing-dependency", "fabric-api")
	if conflict == nil {
		t.Fatalf("没检出缺失前置：%+v", report.Conflicts)
	}
	if conflict.Severity != SeverityError {
		t.Errorf("缺失强制前置必须是 Error，实际 %q", conflict.Severity)
	}
	if len(conflict.Related) != 1 || conflict.Related[0] != "needs-api" {
		t.Errorf("Related = %v，期望 [needs-api]", conflict.Related)
	}
}

func TestMissingDependencyNotReportedWhenPresent(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "needs-api.jar", `{
		"id":"needs-api","name":"需要前置","version":"1.0.0",
		"depends":{"fabric-api":"*"}
	}`)
	writeFabricMod(t, directory, "fabric-api.jar",
		`{"id":"fabric-api","name":"Fabric API","version":"0.92.0"}`)

	report := AnalyzeModConflicts(directory, "", "")
	if findConflict(report, "missing-dependency", "fabric-api") != nil {
		t.Error("前置已存在却报了缺失")
	}
}

func TestMissingDependencySatisfiedByProvides(t *testing.T) {
	directory := modsDir(t)
	// 一个 mod 通过 provides 声明它顶替了另一个 id（如 fabric-api 的兼容层）
	writeFabricMod(t, directory, "compat.jar",
		`{"id":"compat","name":"兼容层","version":"1.0.0","provides":["fabric-api"]}`)
	writeFabricMod(t, directory, "needs-api.jar", `{
		"id":"needs-api","name":"需要前置","version":"1.0.0",
		"depends":{"fabric-api":"*"}
	}`)

	report := AnalyzeModConflicts(directory, "", "")
	if findConflict(report, "missing-dependency", "fabric-api") != nil {
		t.Error("provides 已顶替该 id，不该报缺失")
	}
}

func TestEnvironmentDependenciesAreNotMissingMods(t *testing.T) {
	directory := modsDir(t)
	// minecraft / java / fabricloader 由加载器自己满足，报"缺前置"是纯误导
	writeFabricMod(t, directory, "plain.jar", `{
		"id":"plain","name":"普通","version":"1.0.0",
		"depends":{"minecraft":">=1.20","java":">=17","fabricloader":">=0.15"}
	}`)

	report := AnalyzeModConflicts(directory, "", "")
	if len(report.Conflicts) != 0 {
		t.Errorf("环境依赖不该报成缺失 mod：%+v", report.Conflicts)
	}
}

func TestMissingDependencyAggregatesMultipleConsumers(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "a.jar",
		`{"id":"a","name":"A","version":"1.0.0","depends":{"shared-lib":"*"}}`)
	writeFabricMod(t, directory, "b.jar",
		`{"id":"b","name":"B","version":"1.0.0","depends":{"shared-lib":"*"}}`)

	report := AnalyzeModConflicts(directory, "", "")

	// 同一个缺失前置只报一条，但列出两个需求方
	var count int
	for _, conflict := range report.Conflicts {
		if conflict.Kind == "missing-dependency" && conflict.Subject == "shared-lib" {
			count++
			if len(conflict.Related) != 2 {
				t.Errorf("需求方 = %v，期望两个", conflict.Related)
			}
		}
	}
	if count != 1 {
		t.Errorf("同一个缺失前置报了 %d 条，期望 1 条", count)
	}
}

// ---------------------------------------------------------------------------
// 声明式不兼容
// ---------------------------------------------------------------------------

func TestDetectDeclaredIncompatibility(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "sodium.jar",
		`{"id":"sodium","name":"Sodium","version":"1.0.0","breaks":{"optifine":"*"}}`)
	writeFabricMod(t, directory, "optifine.jar",
		`{"id":"optifine","name":"OptiFine","version":"1.0.0"}`)

	report := AnalyzeModConflicts(directory, "", "")

	conflict := findConflict(report, "incompatible-declared", "sodium")
	if conflict == nil {
		t.Fatalf("没检出声明的不兼容：%+v", report.Conflicts)
	}
	if conflict.Detail != "optifine" {
		t.Errorf("不兼容对象 = %q，期望 optifine", conflict.Detail)
	}
}

func TestIncompatibilityNotReportedWhenTargetAbsent(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "sodium.jar",
		`{"id":"sodium","name":"Sodium","version":"1.0.0","breaks":{"optifine":"*"}}`)

	report := AnalyzeModConflicts(directory, "", "")
	if len(report.Conflicts) != 0 {
		t.Errorf("被 break 的 mod 不在场，不该报冲突：%+v", report.Conflicts)
	}
}

// ---------------------------------------------------------------------------
// 版本 / 加载器匹配
// ---------------------------------------------------------------------------

func TestLoaderMismatchDetected(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "fabric-mod.jar",
		`{"id":"fabric-mod","name":"Fabric 模组","version":"1.0.0"}`)

	report := AnalyzeModConflicts(directory, "1.20.1", "forge")

	if findConflict(report, "loader-mismatch", "fabric-mod") == nil {
		t.Errorf("fabric mod 装进 forge 实例应报出来：%+v", report.Conflicts)
	}
}

func TestQuiltAcceptsFabricMods(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "fabric-mod.jar",
		`{"id":"fabric-mod","name":"Fabric 模组","version":"1.0.0"}`)

	// Quilt Loader 主动兼容 Fabric mod：报不匹配是误报
	report := AnalyzeModConflicts(directory, "1.20.1", "quilt")
	if findConflict(report, "loader-mismatch", "fabric-mod") != nil {
		t.Error("quilt 实例不该把 fabric mod 报成加载器不匹配")
	}
}

func TestMCVersionMismatchDetected(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "old-mod.jar", `{
		"id":"old-mod","name":"旧模组","version":"1.0.0",
		"depends":{"minecraft":"1.19.x"}
	}`)

	report := AnalyzeModConflicts(directory, "1.20.1", "")

	conflict := findConflict(report, "mc-version-mismatch", "old-mod")
	if conflict == nil {
		t.Fatalf("没检出 MC 版本不匹配：%+v", report.Conflicts)
	}
	// 版本不匹配不一定起不来（很多 mod 只是没更新声明），所以是 Warning 不是 Error
	if conflict.Severity != SeverityWarning {
		t.Errorf("严重度 = %q，期望 warning", conflict.Severity)
	}
	if conflict.Detail != "1.19.x|1.20.1" {
		t.Errorf("Detail = %q，期望 1.19.x|1.20.1", conflict.Detail)
	}
}

func TestMCVersionUnknownSkipsCheck(t *testing.T) {
	directory := modsDir(t)
	writeFabricMod(t, directory, "old-mod.jar", `{
		"id":"old-mod","name":"旧模组","version":"1.0.0",
		"depends":{"minecraft":"1.19.x"}
	}`)

	// 实例版本未知时必须跳过检查，而不是把所有 mod 报成不匹配
	report := AnalyzeModConflicts(directory, "", "")
	if len(report.Conflicts) != 0 {
		t.Errorf("实例版本未知时不该报版本不匹配：%+v", report.Conflicts)
	}
}

// ---------------------------------------------------------------------------
// 版本范围求解
// ---------------------------------------------------------------------------

func TestVersionRangeAllows(t *testing.T) {
	cases := []struct {
		declared string
		actual   string
		want     bool
		why      string
	}{
		{"", "1.20.1", true, "空声明视为全部允许"},
		{"*", "1.20.1", true, "通配"},
		{"1.20.1", "1.20.1", true, "精确相等"},
		{"1.20.1", "1.20.2", false, "精确不等"},
		{"1.20.x", "1.20.4", true, "x 通配前缀"},
		{"1.20.x", "1.21.0", false, "x 通配前缀不跨次版本"},
		{"1.20.*", "1.20.4", true, "* 通配前缀"},
		{">=1.20", "1.21.0", true, "大于等于"},
		{">=1.20", "1.19.4", false, "大于等于不成立"},
		{">=1.20 <1.21", "1.20.4", true, "区间内"},
		{">=1.20 <1.21", "1.21.0", false, "区间外"},
		{"~1.20", "1.20.4", true, "波浪号同主次版本"},
		{"~1.20", "1.21.0", false, "波浪号不跨次版本"},
		{"1.20", "1.20.4", true, "只写 major.minor 视为同系列"},
		{"1.20", "1.21.4", false, "同系列判定要挡住跨版本"},
		{"[1.19,1.21)", "1.20.1", true, "看不懂的写法一律放行（不误报）"},
		{"garbage", "1.20.1", true, "完全无法理解时放行"},
	}
	for _, testCase := range cases {
		got := versionRangeAllows(testCase.declared, testCase.actual)
		if got != testCase.want {
			t.Errorf("versionRangeAllows(%q, %q) = %v，期望 %v（%s）",
				testCase.declared, testCase.actual, got, testCase.want, testCase.why)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{
		{"1.20.1", "1.20.1", 0},
		{"1.20.1", "1.20.2", -1},
		{"1.21.0", "1.20.9", 1},
		// "1.20" 与 "1.20.0" 必须相等，否则 >=1.20 会漏判
		{"1.20", "1.20.0", 0},
		{"1.20", "1.21", -1},
	}
	for _, testCase := range cases {
		got := compareVersions(testCase.left, testCase.right)
		if got != testCase.want {
			t.Errorf("compareVersions(%q, %q) = %d，期望 %d",
				testCase.left, testCase.right, got, testCase.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Forge TOML 依赖
// ---------------------------------------------------------------------------

func TestReadForgeTomlDependencies(t *testing.T) {
	directory := modsDir(t)
	writeModJar(t, directory, "forge-mod.jar", "META-INF/mods.toml", `
modLoader="javafml"
loaderVersion="[47,)"
license="MIT"

[[mods]]
modId="forge-mod"
version="1.0.0"
displayName="Forge 模组"

[[dependencies.forge-mod]]
    modId="forge"
    mandatory=true
    versionRange="[47,)"
    ordering="NONE"

[[dependencies.forge-mod]]
    modId="missing-lib"
    mandatory=true
    versionRange="[1.0,2.0)"

[[dependencies.forge-mod]]
    modId="optional-lib"
    mandatory=false
    versionRange="[1.0,)"
`)

	metadata := ReadModMetadata(filepath.Join(directory, "forge-mod.jar"))
	if metadata.Loader != "forge" {
		t.Errorf("加载器 = %q，期望 forge", metadata.Loader)
	}
	if metadata.ModID != "forge-mod" {
		t.Errorf("mod id = %q", metadata.ModID)
	}
	if metadata.Name != "Forge 模组" {
		t.Errorf("名称 = %q", metadata.Name)
	}
	// forge 是环境依赖（被 isEnvironmentDependency 过滤），optional 不强制 → 只剩 missing-lib
	if len(metadata.Depends) != 1 {
		t.Fatalf("依赖 = %+v，期望只剩 missing-lib", metadata.Depends)
	}
	if metadata.Depends[0].ModID != "missing-lib" {
		t.Errorf("依赖 id = %q", metadata.Depends[0].ModID)
	}
	if metadata.Depends[0].VersionRange != "[1.0,2.0)" {
		t.Errorf("版本范围 = %q", metadata.Depends[0].VersionRange)
	}

	// 端到端：缺的应该被报出来
	report := AnalyzeModConflicts(directory, "", "")
	if findConflict(report, "missing-dependency", "missing-lib") == nil {
		t.Errorf("Forge 缺失依赖没报出来：%+v", report.Conflicts)
	}
	if findConflict(report, "missing-dependency", "optional-lib") != nil {
		t.Error("optional 依赖不该报缺失")
	}
}

func TestForgeTemplateVersionFallsBackToManifest(t *testing.T) {
	directory := modsDir(t)
	// 未构建的模板常把版本写成 ${file.jarVersion}
	writeModJar(t, directory, "template.jar", "META-INF/mods.toml", `
[[mods]]
modId="template-mod"
version="${file.jarVersion}"
displayName="模板模组"
`)

	metadata := ReadModMetadata(filepath.Join(directory, "template.jar"))
	if metadata.ModID != "template-mod" {
		t.Errorf("mod id = %q", metadata.ModID)
	}
	// 版本是模板占位符时必须被清掉，而不是把 "${file.jarVersion}" 当版本展示
	if metadata.Version == "${file.jarVersion}" {
		t.Error("模板占位符被原样当成了版本号")
	}
}

// ---------------------------------------------------------------------------
// 健壮性
// ---------------------------------------------------------------------------

func TestReadModMetadataSurvivesGarbage(t *testing.T) {
	directory := modsDir(t)
	// 不是一个合法 zip
	if err := os.WriteFile(filepath.Join(directory, "broken.jar"), []byte("not a zip"), 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}

	metadata := ReadModMetadata(filepath.Join(directory, "broken.jar"))
	if metadata.FileName != "broken.jar" {
		t.Errorf("文件名 = %q", metadata.FileName)
	}
	// 拿不到 id：不参与依赖分析，但也不能让整次检测失败
	if metadata.ModID != "" {
		t.Errorf("坏 jar 不该解析出 mod id：%q", metadata.ModID)
	}

	report := AnalyzeModConflicts(directory, "1.20.1", "fabric")
	if report.UnreadableMods != 1 {
		t.Errorf("不可读计数 = %d，期望 1", report.UnreadableMods)
	}
}

func TestAnalyzeModConflictsMissingDirectory(t *testing.T) {
	report := AnalyzeModConflicts(filepath.Join(t.TempDir(), "nope"), "1.20.1", "fabric")
	if report.Conflicts == nil || report.Mods == nil {
		t.Error("必须返回非 nil 切片（前端会直接 map）")
	}
	if len(report.Conflicts) != 0 {
		t.Errorf("目录不存在不该有冲突：%+v", report.Conflicts)
	}
}

func TestAnalyzeModConflictsIgnoresNonJars(t *testing.T) {
	directory := modsDir(t)
	for _, name := range []string{"readme.txt", "config.json", "notes.md"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("写文件失败：%v", err)
		}
	}

	report := AnalyzeModConflicts(directory, "1.20.1", "fabric")
	if len(report.Mods) != 0 {
		t.Errorf("非 jar 文件被当成 mod：%+v", report.Mods)
	}
}

func TestConflictOrderIsDeterministic(t *testing.T) {
	directory := modsDir(t)
	// 造多个不同严重度的冲突
	writeFabricMod(t, directory, "dup.jar",
		`{"id":"dup","name":"Dup","version":"1.0.0"}`)
	writeFabricMod(t, directory, "dup2.jar",
		`{"id":"dup","name":"Dup","version":"2.0.0"}`)
	writeFabricMod(t, directory, "needs.jar",
		`{"id":"needs","name":"Needs","version":"1.0.0","depends":{"absent":"*"}}`)

	first := AnalyzeModConflicts(directory, "", "")
	second := AnalyzeModConflicts(directory, "", "")

	if len(first.Conflicts) != len(second.Conflicts) {
		t.Fatalf("两次结果条数不同：%d vs %d", len(first.Conflicts), len(second.Conflicts))
	}
	for index := range first.Conflicts {
		left, right := first.Conflicts[index], second.Conflicts[index]
		if left.Kind != right.Kind || left.Subject != right.Subject ||
			left.Severity != right.Severity || left.Detail != right.Detail ||
			len(left.Files) != len(right.Files) || len(left.Related) != len(right.Related) {
			t.Fatalf("第 %d 条不稳定：%+v vs %+v", index, left, right)
		}
	}
	// Error 级必须排在前面
	if first.Conflicts[0].Severity != SeverityError {
		t.Errorf("首条严重度 = %q，期望 error 排最前", first.Conflicts[0].Severity)
	}
}

func TestDisableSuffixConstantMatchesScanner(t *testing.T) {
	// 冲突检测与扫描必须用同一个"已禁用"后缀判断，否则会出现
	// "列表里显示已禁用、冲突检测却still 认为它启用着"的错位
	if disabledFileSuffix != ".disabled" {
		t.Errorf("disabledFileSuffix = %q", disabledFileSuffix)
	}
	if !isDisabledFile("/x/some.jar.disabled") {
		t.Error("isDisabledFile 与 disabledFileSuffix 口径不一致")
	}
}
