package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveTransformComposesClasspath 变换的类路径前后插入 / 替换 / 剔除规则。
func TestResolveTransformComposesClasspath(t *testing.T) {
	directory := t.TempDir()
	prepend := writeJar(t, directory, "prepend.jar")
	appended := writeJar(t, directory, "append.jar")
	replacement := writeJar(t, directory, "replacement.jar")
	working := t.TempDir()
	// 原版 classpath 由依赖解析器给出，一定是绝对路径且文件真实存在
	libA := writeJar(t, directory, "lib-a.jar")
	libB := writeJar(t, directory, "lib-b.jar")
	libC := writeJar(t, directory, "lib-c.jar")

	transform := &MinecraftLaunchTransform{
		PrependClasspath: []string{prepend},
		AppendClasspath:  []string{appended},
		ClasspathReplacements: []MinecraftClasspathReplacement{
			{ExistingPath: libB, ReplacementPath: replacement},
		},
		RemoveClasspath:          []string{libC},
		MainClassOverride:        "com.example.PluginMain",
		WorkingDirectoryOverride: working,
	}

	options := MinecraftLaunchOptions{Transform: transform}
	resolved, err := resolveMinecraftTransform(
		options, "net.minecraft.client.main.Main", t.TempDir(),
		[]string{libA, libB, libC})
	if err != nil {
		t.Fatalf("解析变换失败：%v", err)
	}

	want := []string{prepend, libA, replacement, appended}
	if strings.Join(resolved.Classpath, "|") != strings.Join(want, "|") {
		t.Fatalf("classpath = %v，期望 %v", resolved.Classpath, want)
	}
	if resolved.MainClass != "com.example.PluginMain" {
		t.Fatalf("主类覆盖没生效：%q", resolved.MainClass)
	}
	if resolved.WorkingDirectory != working {
		t.Fatalf("工作目录覆盖没生效：%q", resolved.WorkingDirectory)
	}
}

// TestResolveTransformRejectsMissingPaths 覆盖到不存在的位置时要报错，而不是把
// 一条必然起不来的命令行丢给用户。
func TestResolveTransformRejectsMissingPaths(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-here")

	_, err := resolveMinecraftTransform(
		MinecraftLaunchOptions{Transform: &MinecraftLaunchTransform{JavaExecutableOverride: missing}},
		"Main", t.TempDir(), []string{"lib.jar"})
	if err == nil {
		t.Fatal("Java 覆写指向不存在文件时应报错")
	}

	_, err = resolveMinecraftTransform(
		MinecraftLaunchOptions{Transform: &MinecraftLaunchTransform{WorkingDirectoryOverride: missing}},
		"Main", t.TempDir(), []string{"lib.jar"})
	if err == nil {
		t.Fatal("工作目录覆写指向不存在目录时应报错")
	}
}

// TestBuildChildEnvironmentInjectsAndRemoves 环境变量注入优先于父进程，移除列表生效。
func TestBuildChildEnvironmentInjectsAndRemoves(t *testing.T) {
	t.Setenv("NEKO_PLUGIN_TEST", "parent")
	t.Setenv("NEKO_PLUGIN_REMOVE", "parent")

	// 走真实的规范化路径：Windows 上移除列表按小写键匹配
	injected, removed, err := resolveEnvironmentVariables(&MinecraftLaunchTransform{
		EnvironmentVariables:        map[string]string{"NEKO_PLUGIN_TEST": "plugin", "NEKO_PLUGIN_NEW": "1"},
		RemovedEnvironmentVariables: []string{"NEKO_PLUGIN_REMOVE"},
	})
	if err != nil {
		t.Fatalf("环境变量规范化失败：%v", err)
	}

	environment := buildChildEnvironment(&MinecraftLaunchPlan{
		EnvironmentVariables:        injected,
		RemovedEnvironmentVariables: removed,
	})

	joined := strings.ToLower(strings.Join(environment, "\n"))
	if !strings.Contains(joined, "neko_plugin_test=plugin") {
		t.Fatalf("注入的环境变量没生效：%s", joined)
	}
	if strings.Contains(joined, "neko_plugin_test=parent") {
		t.Fatalf("父进程的值没有被覆盖：%s", joined)
	}
	if strings.Contains(joined, "neko_plugin_remove=") {
		t.Fatalf("移除列表没生效：%s", joined)
	}
	if !strings.Contains(joined, "neko_plugin_new=1") {
		t.Fatalf("新增变量没生效：%s", joined)
	}
}

// TestPluginTransformFlowsIntoCommandLine 端到端（P3-2 的核心验证）：插件贡献经
// LaunchTransformProvider 注入后，真的出现在最终 Java 命令行里。
func TestPluginTransformFlowsIntoCommandLine(t *testing.T) {
	restore := LaunchTransformProvider
	t.Cleanup(func() { LaunchTransformProvider = restore })

	pluginDirectory := t.TempDir()
	agentJar := writeJar(t, pluginDirectory, "demo-agent.jar")
	working := t.TempDir()

	LaunchTransformProvider = func() *MinecraftLaunchTransform {
		return &MinecraftLaunchTransform{
			PrependClasspath:            []string{agentJar},
			PrependJvmArguments:         []string{"-javaagent:" + agentJar},
			AppendGameArguments:         []string{"--plugin-flag"},
			MainClassOverride:           "com.example.PluginMain",
			WorkingDirectoryOverride:    working,
			EnvironmentVariables:        map[string]string{"DEMO_PLUGIN": "1"},
			RemovedEnvironmentVariables: []string{"DEMO_REMOVED"},
		}
	}

	profile := versionProfileFixture()
	options := MinecraftLaunchOptions{
		MinecraftDirectory: t.TempDir(),
		VersionId:          profile.Id,
		Account:            MustOfflineAccount("Tester"),
		MinimumMemoryMb:    1024,
		MaximumMemoryMb:    4096,
		LauncherName:       "NekoLauncher",
		LauncherVersion:    "test",
	}
	// 走真实路径：启动管线用 currentLaunchTransform() 取变换
	options.Transform = currentLaunchTransform()

	resolved, err := resolveMinecraftTransform(
		options, profile.MainClass, t.TempDir(), []string{writeJar(t, pluginDirectory, "lib.jar")})
	if err != nil {
		t.Fatalf("解析变换失败：%v", err)
	}

	arguments, err := MinecraftArgumentBuilder{}.Build(
		profile, options, t.TempDir(), resolved.Classpath, resolved.MainClass,
		resolved.PrependJvmArguments, resolved.AppendJvmArguments,
		resolved.PrependGameArguments, resolved.AppendGameArguments)
	if err != nil {
		t.Fatalf("装配参数失败：%v", err)
	}

	if arguments[0] != "-javaagent:"+agentJar {
		t.Fatalf("插件的前置 JVM 参数应在最前：%v", arguments[:min(3, len(arguments))])
	}
	// 类路径里插件 jar 先于原版库
	classpath := ""
	for index, argument := range arguments {
		if argument == "-cp" && index+1 < len(arguments) {
			classpath = arguments[index+1]
		}
	}
	if !strings.HasPrefix(classpath, agentJar) {
		t.Fatalf("插件 jar 应在 classpath 最前：%q", classpath)
	}
	if indexOfArgument(arguments, "com.example.PluginMain") < 0 {
		t.Fatalf("主类覆盖没进入命令行：%v", arguments)
	}
	if indexOfArgument(arguments, "net.minecraft.client.main.Main") >= 0 {
		t.Fatalf("原版主类不该再出现：%v", arguments)
	}
	if arguments[len(arguments)-1] != "--plugin-flag" {
		t.Fatalf("插件的追加游戏参数应在末尾：%v", arguments[len(arguments)-1])
	}
	if resolved.WorkingDirectory != working {
		t.Fatalf("工作目录覆盖没生效：%q", resolved.WorkingDirectory)
	}
	// 环境变量名在 Windows 上按小写规范化（子进程环境不区分大小写），
	// 其他平台保留声明时的原始大小写
	environmentVariables := resolved.EnvironmentVariables
	if environmentVariables["demo_plugin"] != "1" && environmentVariables["DEMO_PLUGIN"] != "1" {
		t.Fatalf("环境变量注入没生效：%v", resolved.EnvironmentVariables)
	}
	removedEnvironmentVariables := resolved.RemovedEnvironmentVariables
	_, removedLowercase := removedEnvironmentVariables["demo_removed"]
	_, removedUppercase := removedEnvironmentVariables["DEMO_REMOVED"]
	if !removedLowercase && !removedUppercase {
		t.Fatalf("环境变量移除没生效：%v", resolved.RemovedEnvironmentVariables)
	}
}

// writeJar 造一个占位 jar 文件（只做路径存在性校验，不解析内容）。
func writeJar(t *testing.T, directory, name string) string {
	t.Helper()

	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("jar"), 0o644); err != nil {
		t.Fatalf("写入 %s 失败：%v", name, err)
	}

	return path
}

// nameSet 把名字列表变成集合（启动计划的移除列表用的是集合语义）。
func nameSet(names ...string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}

	return set
}
