package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeVersionJSON 在 <minecraftDirectory>/versions/<id>/<id>.json 写入版本描述。
func writeVersionJSON(t *testing.T, minecraftDirectory, id, content string) {
	t.Helper()

	directory := filepath.Join(minecraftDirectory, "versions", id)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("建版本目录失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, id+".json"), []byte(content), 0o644); err != nil {
		t.Fatalf("写版本描述失败：%v", err)
	}
}

// loggingFixtureJSON 原版版本 JSON 的 logging 段（取自真实 1.16.5 描述）。
const loggingFixtureJSON = `{
  "id": "1.16.5",
  "mainClass": "net.minecraft.client.main.Main",
  "assets": "1.16",
  "downloads": {
    "client": {
      "sha1": "25d06f1d0e0d2d8d9e6f0c2ffd9e5b3d1f7b2a4c",
      "size": 100,
      "url": "https://piston-data.mojang.com/v1/objects/example/client.jar"
    }
  },
  "logging": {
    "client": {
      "argument": "-Dlog4j.configurationFile=${path}",
      "file": {
        "id": "client-1.12.xml",
        "sha1": "bd65e7d2e3c237be76cfbef4c2405033d7f91521",
        "size": 888,
        "url": "https://piston-data.mojang.com/v1/objects/bd65e7d2e3c237be76cfbef4c2405033d7f91521/client-1.12.xml"
      },
      "type": "log4j2-xml"
    }
  },
  "arguments": {
    "jvm": ["-Djava.library.path=${natives_directory}", "-cp", "${classpath}"],
    "game": ["--username", "${auth_player_name}", "--version", "${version_name}"]
  }
}`

// TestProfileKeepsLoggingSection 版本档案要带上 logging 段：原版 JSON 用它声明
// log4j2 配置，启动参数装配靠它补 -Dlog4j.configurationFile。
func TestProfileKeepsLoggingSection(t *testing.T) {
	directory := t.TempDir()
	writeVersionJSON(t, directory, "1.16.5", loggingFixtureJSON)

	profile, err := MinecraftVersionProfileLoader{}.Load(t.Context(), directory, "1.16.5")
	if err != nil {
		t.Fatalf("加载版本失败：%v", err)
	}

	if profile.LoggingArgument != "-Dlog4j.configurationFile=${path}" {
		t.Fatalf("logging argument = %q", profile.LoggingArgument)
	}
	if profile.LoggingFileId != "client-1.12.xml" {
		t.Fatalf("logging file id = %q", profile.LoggingFileId)
	}
}

// TestBuildInjectsLoggingArgument 装配启动参数时要补上 log4j 配置参数，
// 且 ${path} 解析成本机的 assets/log_configs/<id>。
func TestBuildInjectsLoggingArgument(t *testing.T) {
	minecraftDirectory := t.TempDir()
	logConfigPath := filepath.Join(minecraftDirectory, "assets", "log_configs", "client-1.12.xml")
	if err := os.MkdirAll(filepath.Dir(logConfigPath), 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}
	if err := os.WriteFile(logConfigPath, []byte("<Configuration/>"), 0o644); err != nil {
		t.Fatalf("写配置失败：%v", err)
	}

	profile := versionProfileFixture()
	profile.LoggingArgument = "-Dlog4j.configurationFile=${path}"
	profile.LoggingFileId = "client-1.12.xml"

	arguments := buildFixtureArgumentsAt(t, profile, minecraftDirectory, MustOfflineAccount("Tester"))

	want := "-Dlog4j.configurationFile=" + logConfigPath
	index := indexOfArgument(arguments, want)
	if index < 0 {
		t.Fatalf("缺少日志配置参数 %q：\n%s", want, strings.Join(arguments, "\n"))
	}
	// 与官方启动器一致：放在 JVM 参数最前面（插件注入参数之后的第一个）
	if index != 0 {
		t.Fatalf("日志配置参数应在最前，实际位置 %d：%v", index, arguments[:min(index+1, len(arguments))])
	}
}

// TestBuildSkipsMissingLoggingConfig 日志配置还没下到本地时不下发该参数：
// 指着一个不存在的文件会让 log4j 报错，而用默认配置照样能进游戏。
func TestBuildSkipsMissingLoggingConfig(t *testing.T) {
	profile := versionProfileFixture()
	profile.LoggingArgument = "-Dlog4j.configurationFile=${path}"
	profile.LoggingFileId = "client-1.12.xml"

	arguments := buildFixtureArguments(t, profile, MustOfflineAccount("Tester"))

	for _, argument := range arguments {
		if strings.HasPrefix(argument, "-Dlog4j.configurationFile=") {
			t.Fatalf("配置缺失时不该下发日志参数：%q", argument)
		}
	}
}

// TestBuildWithoutLoggingSection 没有 logging 段的版本（部分 Loader 描述）
// 不产生该参数，也不因 ${path} 缺失报错。
func TestBuildWithoutLoggingSection(t *testing.T) {
	arguments := buildFixtureArguments(t, versionProfileFixture(), MustOfflineAccount("Tester"))

	for _, argument := range arguments {
		if strings.Contains(argument, "log4j") || strings.Contains(argument, "${path}") {
			t.Fatalf("不该出现日志参数：%q", argument)
		}
	}
}
