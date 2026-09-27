package download

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLiveFlattenedInstanceLoggingRecovery 真连 Mojang 元数据，验证"被扁平化的实例"
// 的日志配置修复路径：这类实例本地 JSON 里既没有 inheritsFrom 也没有 logging 段，
// 只剩 clientVersion 这个标记；凭它去官方清单取回原版描述，就能重新拿到 log4j
// 配置声明（进而补回 assets/log_configs/<id>）。
//
// 只读：只取元数据、不下载配置文件，所以不会改动本机的 .minecraft。
//
//	NEKO_LIVE_MINECRAFT="C:\Users\me\AppData\Roaming\.minecraft" \
//	NEKO_LIVE_VERSION="fabric-loader-0.19.5-1.21.1" \
//	go test ./internal/download/ -run TestLiveFlattenedInstanceLoggingRecovery -v
func TestLiveFlattenedInstanceLoggingRecovery(t *testing.T) {
	minecraftDirectory := strings.TrimSpace(os.Getenv("NEKO_LIVE_MINECRAFT"))
	versionID := strings.TrimSpace(os.Getenv("NEKO_LIVE_VERSION"))
	if minecraftDirectory == "" || versionID == "" {
		t.Skip("未设置 NEKO_LIVE_MINECRAFT / NEKO_LIVE_VERSION，跳过真实元数据校验")
	}

	jsonPath := filepath.Join(minecraftDirectory, "versions", versionID, versionID+".json")
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Skipf("实例描述不可读，跳过：%v", err)
	}

	var metadata VersionJSON
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatalf("实例描述不是有效 JSON：%v", err)
	}

	clientVersion := ClientVersionOf(raw)
	if clientVersion == "" {
		t.Skipf("实例 %s 没有 clientVersion 标记（不是扁平化实例），跳过", versionID)
	}
	if MetadataHasLogging(raw) {
		t.Skipf("实例 %s 本地仍有 logging 段，无需修复路径", versionID)
	}

	ctx := context.Background()
	url := ""
	versions, err := GetVersions(ctx)
	if err != nil {
		t.Fatalf("取版本清单失败：%v", err)
	}
	for _, version := range versions {
		if strings.EqualFold(version.ID, clientVersion) {
			url = version.URL

			break
		}
	}
	if url == "" {
		t.Fatalf("版本清单里找不到 %s（clientVersion 标记）", clientVersion)
	}

	payload, err := SourceProvider.GetBytes(ctx, url, nil)
	if err != nil {
		t.Fatalf("取 %s 的官方描述失败：%v", clientVersion, err)
	}

	var vanilla VersionJSON
	if err := json.Unmarshal(payload, &vanilla); err != nil {
		t.Fatalf("官方描述解析失败：%v", err)
	}
	file := loggingConfigFile(&vanilla)
	if file == nil {
		t.Fatalf("原版 %s 的描述里没有 logging 段，修复路径无从取值", clientVersion)
	}

	localPath := LoggingConfigPath(minecraftDirectory, &vanilla)
	_, statErr := os.Stat(localPath)
	t.Logf("实例 %s（clientVersion=%s）→ 日志配置 %s（sha1 %s，%d 字节），本机已存在=%v",
		versionID, clientVersion, file.ID, file.SHA1, file.Size, statErr == nil)

	if strings.TrimSpace(file.URL) == "" || strings.TrimSpace(file.ID) == "" {
		t.Fatalf("官方元数据里的 logging 声明不完整：%+v", file)
	}
}
