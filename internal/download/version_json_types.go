package download

// version_json_types.go 版本 JSON 的安装期/校验期解析类型（与 Mojang 格式对应）。
// 仅作反序列化载体；继承链合并语义在 internal/launch 的 VersionJsonFlattener。

import "encoding/json"

// VersionJSON 版本 JSON 的安装期所需子集。
type VersionJSON struct {
	ID                 string            `json:"id,omitempty"`
	InheritsFrom       string            `json:"inheritsFrom,omitempty"`
	MainClass          string            `json:"mainClass,omitempty"`
	Assets             string            `json:"assets,omitempty"`
	MinecraftArguments string            `json:"minecraftArguments,omitempty"`
	Jar                string            `json:"jar,omitempty"`
	Libraries          []libraryJSON     `json:"libraries,omitempty"`
	Arguments          json.RawMessage   `json:"arguments,omitempty"`
	Downloads          *downloadsJSON    `json:"downloads,omitempty"`
	AssetIndex         *downloadInfoJSON `json:"assetIndex,omitempty"`
	// Logging 日志配置段（log4j2）：客户端启动时用 -Dlog4j.configurationFile
	// 指向它。1.7 起所有原版版本都有，缺失时游戏用 log4j 默认配置。
	Logging       *loggingJSON `json:"logging,omitempty"`
	ClientVersion string       `json:"clientVersion,omitempty"`
}

// loggingJSON 版本 JSON 的 logging 段。
type loggingJSON struct {
	Client *loggingClientJSON `json:"client"`
}

// loggingClientJSON 客户端日志配置：argument 是启动参数模板
// （形如 -Dlog4j.configurationFile=${path}），file 是要落到
// assets/log_configs/<id> 的配置文件。
type loggingClientJSON struct {
	Argument string            `json:"argument"`
	File     *downloadInfoJSON `json:"file"`
	Type     string            `json:"type"`
}

type downloadsJSON struct {
	Client *downloadInfoJSON `json:"client"`
}

type downloadInfoJSON struct {
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	Path string `json:"path"`
	ID   string `json:"id,omitempty"` // assetIndex 节点用（资源索引 ID）
}

type libraryDownloadsJSON struct {
	Artifact    *downloadInfoJSON            `json:"artifact"`
	Classifiers map[string]*downloadInfoJSON `json:"classifiers"`
}

type libraryJSON struct {
	Name      string                `json:"name,omitempty"`
	URL       string                `json:"url,omitempty"`
	Natives   map[string]string     `json:"natives,omitempty"`
	Rules     []ruleJSON            `json:"rules,omitempty"`
	Downloads *libraryDownloadsJSON `json:"downloads,omitempty"`
}
