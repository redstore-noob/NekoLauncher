package tools

import (
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// windowsVersionPattern RtlGetVersion 结果的约定形状："10.0.26200"。
var windowsVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// TestOSVersionDescriptionMatchesMojangRuleShape 防的回归：
// 描述串带上 "Microsoft Windows " 前缀或空格，Mojang 的 `^10\.` 这类**锚定**正则
// 永远匹配不上，lwjgl/natives 的 os.version 规则失效（表现为"装得上、启不来"）。
func TestOSVersionDescriptionMatchesMojangRuleShape(t *testing.T) {
	description := OSVersionDescription()
	if description == "" {
		t.Fatal("OSVersionDescription() 不应为空串：空串会让所有 os.version 规则失配")
	}
	if strings.ContainsAny(description, " \t\r\n") {
		t.Fatalf("描述串不应含空白字符（锚定正则会失配）：%q", description)
	}
	if strings.Contains(description, "Microsoft Windows") {
		t.Fatalf("描述串不应带 Windows 友好名前缀：%q", description)
	}

	switch runtime.GOOS {
	case "windows":
		if !windowsVersionPattern.MatchString(description) {
			t.Fatalf("Windows 下描述串 = %q，期望形如 \"10.0.26200\"", description)
		}
		if description == runtime.Version() {
			t.Fatalf("Windows 下取到了回退值 %q：RtlGetVersion 探测失败（检查 DwOSVersionInfoSize 是否填写）", description)
		}
		if raw := windowsOSVersion(); description != raw {
			t.Fatalf("Windows 下应直接使用 RtlGetVersion 结果 %q，实际 %q", raw, description)
		}
		// 规则是 `^10\.` 这种锚定写法：Windows 10/11 都必须落在 10.x
		major := strings.SplitN(description, ".", 2)[0]
		if major == "10" && !regexp.MustCompile(`^10\.`).MatchString(description) {
			t.Fatalf("Windows 10/11 的描述串必须匹配 ^10\\. ：%q", description)
		}
	case "darwin":
		if description == runtime.Version() {
			t.Fatalf("macOS 下取到了回退值 %q：sw_vers 探测失败", description)
		}
	default:
		// 其它平台按约定回退到 runtime.Version()（规则几乎不会命中，与旧行为一致）
		if description != runtime.Version() {
			t.Fatalf("非 Windows/macOS 平台应回退到 runtime.Version() = %q，实际 %q",
				runtime.Version(), description)
		}
	}
}

// TestWindowsOSVersionProbe 防的回归：RtlGetVersion 结构体大小没填导致探测永远失败
// （早先就漏了 DwOSVersionInfoSize 这一行，整条 os.version 探测静默失效）。
func TestWindowsOSVersionProbe(t *testing.T) {
	got := windowsOSVersion()

	if runtime.GOOS != "windows" {
		// 非 Windows 平台是占位实现，必须返回空串让调用方走回退
		if got != "" {
			t.Fatalf("非 Windows 平台的 windowsOSVersion() = %q，期望空串", got)
		}
		return
	}

	if got == "" {
		t.Fatal("Windows 上 windowsOSVersion() 返回空串：RtlGetVersion 调用失败，os.version 规则会全部失配")
	}
	if !windowsVersionPattern.MatchString(got) {
		t.Fatalf("windowsOSVersion() = %q，期望 \"主版本.次版本.内部版本\" 三段数字", got)
	}
	parts := strings.Split(got, ".")
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		t.Fatalf("主版本不可解析：%q", got)
	}
	build, err := strconv.Atoi(parts[2])
	if err != nil {
		t.Fatalf("内部版本不可解析：%q", got)
	}
	if major < 6 {
		t.Fatalf("主版本 = %d，本启动器要求 Windows 10 及以上（Vista 为 6.0）", major)
	}
	if build <= 0 {
		t.Fatal("内部版本号为 0：结构体没有真正被填充，探测结果不可信")
	}
}

// TestDarwinOSVersionProbe 防的回归：非 macOS 平台误返回非空串，
// 让 OSVersionDescription 走进不该走的分支；macOS 上则必须真取到版本号。
func TestDarwinOSVersionProbe(t *testing.T) {
	got := darwinOSVersion()

	if runtime.GOOS != "darwin" {
		if got != "" {
			t.Fatalf("非 macOS 平台的 darwinOSVersion() = %q，期望空串", got)
		}
		return
	}
	if got == "" {
		t.Fatal("macOS 上 sw_vers 取不到产品版本，1.5.2/1.6.4 的 lwjgl os.version 规则会失配")
	}
	if strings.ContainsAny(got, " \t\r\n") {
		t.Fatalf("macOS 产品版本不应含空白：%q", got)
	}
}
