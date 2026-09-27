package download

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// logging_config.go 版本 JSON 的 logging 段（log4j2 配置）。
//
// 每个原版版本的 JSON 里都有：
//
//	"logging": {"client": {
//	    "argument": "-Dlog4j.configurationFile=${path}",
//	    "file": {"id": "client-1.12.xml", "sha1": "…", "size": 888, "url": "…"},
//	    "type": "log4j2-xml"}}
//
// 官方启动器会把 file 下载到 assets/log_configs/<id>，再用它替换 argument 里的
// ${path} 作为 JVM 参数。启动器漏掉这一步时游戏仍能跑（log4j 用默认配置），
// 但控制台日志格式/级别与官方不一致，1.16 时代的 Log4Shell 缓解配置也一起丢了。

// LoggingConfigDirectory 日志配置目录（相对 .minecraft/assets）。
const LoggingConfigDirectory = "log_configs"

// LoggingConfigPath 版本声明的日志配置在本机的落盘路径；未声明时返回空串。
func LoggingConfigPath(minecraftDirectory string, metadata *VersionJSON) string {
	file := loggingConfigFile(metadata)
	if file == nil {
		return ""
	}

	return filepath.Join(filepath.Clean(minecraftDirectory), "assets", LoggingConfigDirectory, file.ID)
}

// LoggingConfigArgument 版本声明的日志配置启动参数模板（如
// -Dlog4j.configurationFile=${path}）；未声明时返回空串。
func LoggingConfigArgument(metadata *VersionJSON) string {
	if metadata == nil || metadata.Logging == nil || metadata.Logging.Client == nil {
		return ""
	}

	return strings.TrimSpace(metadata.Logging.Client.Argument)
}

// loggingConfigFile 取 logging.client.file（声明不完整时返回 nil）。
func loggingConfigFile(metadata *VersionJSON) *downloadInfoJSON {
	if metadata == nil || metadata.Logging == nil || metadata.Logging.Client == nil {
		return nil
	}
	file := metadata.Logging.Client.File
	if file == nil {
		return nil
	}
	if strings.TrimSpace(file.ID) == "" || strings.TrimSpace(file.URL) == "" {
		return nil
	}

	return file
}

// createLoggingPlan 生成日志配置的下载条目；未声明或声明不完整时返回 nil。
func createLoggingPlan(metadata *VersionJSON, minecraftDirectory string) *downloadFile {
	file := loggingConfigFile(metadata)
	if file == nil {
		return nil
	}

	return &downloadFile{
		url:         file.URL,
		targetPath:  filepath.Join(filepath.Clean(minecraftDirectory), "assets", LoggingConfigDirectory, file.ID),
		sha1:        strings.TrimSpace(file.SHA1),
		size:        file.Size,
		displayName: file.ID,
	}
}

// EnsureLoggingConfig 确保版本声明的日志配置已就位（缺失或哈希不符时重新下载）。
// 返回是否发生了下载；版本没有 logging 段时不做任何事。
func EnsureLoggingConfig(
	ctx context.Context,
	minecraftDirectory string,
	metadata *VersionJSON,
	status StatusFunc,
) (bool, error) {
	plan := createLoggingPlan(metadata, minecraftDirectory)
	if plan == nil {
		return false, nil
	}

	valid, err := isExistingFileValid(ctx, *plan)
	if err != nil {
		return false, err
	}
	if valid {
		return false, nil
	}
	if _, statErr := os.Stat(plan.targetPath); statErr == nil {
		// 文件在但校验值不符：内容被改坏或上次下载不全，重新下载覆盖
		reportStatus(status, "日志配置与官方校验值不符，正在重新下载…")
	} else {
		reportStatus(status, "正在补全日志配置…")
	}

	counters := &installCounters{}
	counters.addTotalBytes(maxInt64(0, plan.size))
	counters.addTotalFiles(1)

	// progress 传 nil：补全流程不驱动安装进度条，只用 status 文案汇报
	return true, downloadStage(ctx, 5, "下载日志配置",
		[]downloadFile{*plan}, nil, counters, time.Now())
}

// ClientVersionOf 读取版本 JSON 里的 clientVersion 标记（扁平化实例才有）。
func ClientVersionOf(jsonBytes []byte) string {
	var probe struct {
		ClientVersion string `json:"clientVersion"`
	}
	if json.Unmarshal(jsonBytes, &probe) != nil {
		return ""
	}

	return strings.TrimSpace(probe.ClientVersion)
}

// MetadataHasLogging 版本 JSON 字节里是否声明了 logging 段。
func MetadataHasLogging(jsonBytes []byte) bool {
	var metadata VersionJSON
	if json.Unmarshal(jsonBytes, &metadata) != nil {
		return false
	}

	return loggingConfigFile(&metadata) != nil
}

// EnsureLoggingConfigFromMetadataURL 用"版本元数据 URL"补全日志配置。
//
// 存在的意义是**已经被扁平化过的实例**：旧版扁平化会丢掉 logging 段，而它继承的
// 原版 JSON 也一并删掉了，本地再也查不到声明。好在扁平化会写下 clientVersion
// （对应的原版版本 id），拿它去官方元数据取回声明即可补上文件。
// 元数据取不到、或该版本本来就没有 logging 段时静默返回（日志配置属于可选项，
// 不该把修复流程搅黄）。
func EnsureLoggingConfigFromMetadataURL(
	ctx context.Context,
	minecraftDirectory, metadataURL string,
	status StatusFunc,
) (bool, error) {
	if strings.TrimSpace(metadataURL) == "" {
		return false, nil
	}

	payload, err := SourceProvider.GetBytes(ctx, metadataURL, nil)
	if err != nil {
		return false, nil
	}

	var metadata VersionJSON
	if json.Unmarshal(payload, &metadata) != nil {
		return false, nil
	}
	if loggingConfigFile(&metadata) == nil {
		return false, nil
	}

	return EnsureLoggingConfig(ctx, minecraftDirectory, &metadata, status)
}
