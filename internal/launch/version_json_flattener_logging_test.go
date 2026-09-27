package launch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestFlattenKeepsLoggingSection 扁平化要保持 logging 段：log4j 的配置声明只在
// 原版 JSON 里，扁平化后实例不再有 inheritsFrom，丢掉它控制台日志就退回默认配置。
func TestFlattenKeepsLoggingSection(t *testing.T) {
	minecraftDirectory := t.TempDir()

	parent := `{
  "id": "1.16.5",
  "mainClass": "net.minecraft.client.main.Main",
  "assets": "1.16",
  "downloads": {"client": {"sha1": "aa", "size": 10, "url": "https://example.com/client.jar"}},
  "logging": {
    "client": {
      "argument": "-Dlog4j.configurationFile=${path}",
      "file": {"id": "client-1.12.xml", "sha1": "bb", "size": 888, "url": "https://example.com/client-1.12.xml"},
      "type": "log4j2-xml"
    }
  },
  "arguments": {"jvm": ["-cp", "${classpath}"], "game": ["--username", "${auth_player_name}"]}
}`
	child := `{
  "id": "fabric-loader-0.19.5-1.16.5",
  "inheritsFrom": "1.16.5",
  "mainClass": "net.fabricmc.loader.impl.launch.knot.KnotClient",
  "arguments": {"jvm": ["-DFabricMcEmu=net.minecraft.client.main.Main"]}
}`

	writeVersionJSON(t, minecraftDirectory, "1.16.5", parent)
	writeVersionJSON(t, minecraftDirectory, "fabric-loader-0.19.5-1.16.5", child)
	// 扁平化需要客户端 JAR 能落到实例目录（模拟依赖版本已装好）
	parentJar := filepath.Join(minecraftDirectory, "versions", "1.16.5", "1.16.5.jar")
	if err := os.WriteFile(parentJar, []byte("jar"), 0o644); err != nil {
		t.Fatalf("写依赖 JAR 失败：%v", err)
	}

	result, err := VersionJsonFlattener.Flatten(t.Context(), minecraftDirectory, "fabric-loader-0.19.5-1.16.5")
	if err != nil {
		t.Fatalf("扁平化失败：%v", err)
	}
	if !result.Flattened {
		t.Fatal("应当发生扁平化")
	}

	flattenedPath := filepath.Join(minecraftDirectory, "versions", "fabric-loader-0.19.5-1.16.5",
		"fabric-loader-0.19.5-1.16.5.json")
	data, err := os.ReadFile(flattenedPath)
	if err != nil {
		t.Fatalf("读扁平化结果失败：%v", err)
	}

	var merged struct {
		Logging *struct {
			Client *struct {
				Argument string `json:"argument"`
				File     *struct {
					ID string `json:"id"`
				} `json:"file"`
			} `json:"client"`
		} `json:"logging"`
	}
	if err := json.Unmarshal(data, &merged); err != nil {
		t.Fatalf("解析扁平化结果失败：%v", err)
	}
	if merged.Logging == nil || merged.Logging.Client == nil {
		t.Fatalf("扁平化后丢了 logging 段：%s", data)
	}
	if merged.Logging.Client.Argument != "-Dlog4j.configurationFile=${path}" ||
		merged.Logging.Client.File == nil || merged.Logging.Client.File.ID != "client-1.12.xml" {
		t.Fatalf("logging 段内容不符：%+v", merged.Logging.Client)
	}

	// 扁平化后的档案加载出来仍带日志声明（子级没声明时沿用父级）
	profile, err := MinecraftVersionProfileLoader{}.Load(t.Context(), minecraftDirectory, "fabric-loader-0.19.5-1.16.5")
	if err != nil {
		t.Fatalf("加载扁平化版本失败：%v", err)
	}
	if profile.LoggingFileId != "client-1.12.xml" {
		t.Fatalf("扁平化后的档案丢了日志声明：%q", profile.LoggingFileId)
	}
}
