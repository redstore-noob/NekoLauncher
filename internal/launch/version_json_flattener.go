package launch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekolauncher/internal/tools"
)

// VersionFlattenResult 扁平化结果：是否发生了合并、被合并掉的依赖版本 id 列表。
type VersionFlattenResult struct {
	Flattened           bool
	ConsumedParentIds   []string
	ClientJarProviderId string
}

// orderedObject 保持插入顺序的 JSON 对象（对应 C# JsonObject 的顺序语义，
// 输出时按插入顺序生成键）。
type orderedObject struct {
	keys   []string
	values map[string]json.RawMessage
}

func newOrderedObject() *orderedObject {
	return &orderedObject{values: map[string]json.RawMessage{}}
}

func (o *orderedObject) set(name string, value json.RawMessage) {
	if _, exists := o.values[name]; !exists {
		o.keys = append(o.keys, name)
	}
	o.values[name] = value
}

func (o *orderedObject) has(name string) bool {
	_, exists := o.values[name]
	return exists
}

// marshal 按插入顺序输出紧凑 JSON。
func (o *orderedObject) marshal() ([]byte, error) {
	var builder strings.Builder
	builder.WriteByte('{')
	for index, key := range o.keys {
		if index > 0 {
			builder.WriteByte(',')
		}
		keyBytes, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		builder.Write(keyBytes)
		builder.WriteByte(':')
		builder.Write(o.values[key])
	}
	builder.WriteByte('}')
	return []byte(builder.String()), nil
}

// VersionJsonFlattener 版本继承扁平化：把 inheritsFrom 继承链（如 Fabric 实例 → 原版）
// 合并为单个自包含的版本 JSON，并把依赖版本的客户端 JAR 复制为实例自己的
// {实例id}.jar。合并后实例不再依赖任何其它版本目录，可以单独存在、
// 单独删除——同一 Minecraft 主版本的多个实例也互不共享版本目录。
// 合并语义与启动侧 MinecraftVersionProfileLoader 完全一致：
// mainClass / type / assets / assetIndex / javaVersion / downloads.client 子级覆盖；
// minecraftArguments 取最近声明的一层；arguments.jvm/game 数组按父→子顺序拼接；
// libraries 按 group:artifact:classifier@ext 键去重、子级覆盖，保持首次出现顺序。
var VersionJsonFlattener versionJsonFlattenerNamespace

type versionJsonFlattenerNamespace struct{}

// Flatten 把指定实例的继承链合并为自包含版本 JSON。
// 实例没有 inheritsFrom 时什么都不做（返回 Flattened=false）；
// 依赖版本的客户端 JAR 缺失且实例也没有自己的 JAR 时返回错误
// （保持原状，依赖仍可用）。
func (versionJsonFlattenerNamespace) Flatten(
	ctx context.Context,
	minecraftDirectory, versionId string,
) (*VersionFlattenResult, error) {
	if strings.TrimSpace(minecraftDirectory) == "" {
		panic("minecraftDirectory 不能为空")
	}
	if err := validateVersionId(versionId); err != nil {
		return nil, err
	}

	chain, err := loadFlattenChain(ctx, minecraftDirectory, versionId)
	if err != nil {
		return nil, err
	}
	if len(chain) <= 1 {
		return &VersionFlattenResult{Flattened: false}, nil
	}

	// 链反转后为「原版 → … → 实例」：合并自最顶层依赖开始，子级覆盖父级
	merged, err := mergeFlattenChain(versionId, chain)
	if err != nil {
		return nil, err
	}
	jarProviderId := resolveClientJarProviderId(chain)

	// 客户端 JAR 必须能落到实例自己的目录，否则扁平化后无法启动；
	// 依赖目录里找不到 JAR（且实例也没有）时放弃扁平化，保持继承结构
	versionDirectory := filepath.Join(minecraftDirectory, "versions", versionId)
	targetJar := filepath.Join(versionDirectory, versionId+".jar")
	if !tools.FileExists(targetJar) {
		if jarProviderId == "" {
			return nil, fmt.Errorf("无法扁平化 %s：继承链中没有任何版本声明客户端 JAR。", versionId)
		}
		sourceJar := filepath.Join(minecraftDirectory, "versions", jarProviderId, jarProviderId+".jar")
		if !tools.FileExists(sourceJar) {
			return nil, fmt.Errorf(
				"无法扁平化 %s：依赖版本 %s 的客户端 JAR 缺失（%s）。请先启动或修复该实例后重试。",
				versionId, jarProviderId, sourceJar)
		}
		if err := os.MkdirAll(versionDirectory, 0o755); err != nil {
			return nil, err
		}
		if err := copyFileContents(sourceJar, targetJar); err != nil {
			return nil, err
		}
	}

	// 依赖结构消失后，详情页/导出功能无法再靠 inheritsFrom 推断基础 MC 版本；
	// 写入 clientVersion 元字段（GameVersionDetailsService 优先读取）
	if !merged.has("clientVersion") {
		root := chain[0].root
		if declaredId, ok := tryGetString(root, "id"); ok {
			merged.set("clientVersion", mustMarshalString(declaredId))
		} else {
			merged.set("clientVersion", mustMarshalString(chain[0].id))
		}
	}

	// 原子写入合并后的版本 JSON（临时文件 + 替换），失败时依赖结构未被破坏
	jsonPath := filepath.Join(versionDirectory, versionId+".json")
	content, err := merged.marshal()
	if err != nil {
		return nil, err
	}
	temporaryPath := jsonPath + ".nya-flatten-tmp"
	if err := os.WriteFile(temporaryPath, content, 0o644); err != nil {
		_ = os.Remove(temporaryPath)
		return nil, err
	}
	if err := os.Rename(temporaryPath, jsonPath); err != nil {
		_ = os.Remove(temporaryPath)
		return nil, err
	}

	// chain 反转后为「最顶层依赖 → … → 实例自身」；被合并掉的是除实例外的全部父级
	consumedParents := make([]string, 0, len(chain)-1)
	for _, entry := range chain[:len(chain)-1] {
		consumedParents = append(consumedParents, entry.id)
	}
	return &VersionFlattenResult{
		Flattened:           true,
		ConsumedParentIds:   consumedParents,
		ClientJarProviderId: jarProviderId,
	}, nil
}

// IsVersionReferenced 检查指定版本是否仍被其它版本的 inheritsFrom 引用。
// 用于扁平化后判断依赖版本目录能否安全删除。
func (versionJsonFlattenerNamespace) IsVersionReferenced(minecraftDirectory, versionId string) bool {
	versionsDirectory := filepath.Join(minecraftDirectory, "versions")
	if !tools.DirectoryExists(versionsDirectory) {
		return false
	}

	entries, err := os.ReadDir(versionsDirectory)
	if err != nil {
		return true
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if strings.TrimSpace(id) == "" || strings.EqualFold(id, versionId) {
			continue
		}

		jsonPath := filepath.Join(versionsDirectory, id, id+".json")
		if !tools.FileExists(jsonPath) {
			continue
		}
		data, err := os.ReadFile(jsonPath)
		if err != nil {
			// 读取失败的版本无法证明引用关系，保守视为可能引用
			return true
		}
		var root map[string]json.RawMessage
		if json.Unmarshal(data, &root) != nil {
			return true
		}
		if inherits, ok := tryGetString(root, "inheritsFrom"); ok &&
			strings.EqualFold(inherits, versionId) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 继承链加载与合并
// ---------------------------------------------------------------------------

func loadFlattenChain(
	ctx context.Context,
	minecraftDirectory, versionId string,
) ([]versionChainEntry, error) {
	var chain []versionChainEntry
	visited := map[string]bool{}
	currentId := versionId

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if visited[strings.ToLower(currentId)] {
			return nil, fmt.Errorf("版本继承出现循环：%s", currentId)
		}
		visited[strings.ToLower(currentId)] = true
		if len(chain) >= maximumInheritanceDepth {
			return nil, fmt.Errorf("版本继承层级过深。")
		}

		jsonPath := versionJsonPath(minecraftDirectory, currentId)
		if !tools.FileExists(jsonPath) {
			return nil, fmt.Errorf("找不到版本配置：%s", jsonPath)
		}
		data, err := os.ReadFile(jsonPath)
		if err != nil {
			return nil, err
		}
		var root map[string]json.RawMessage
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, fmt.Errorf("版本配置不是有效 JSON：%s", jsonPath)
		}

		chain = append(chain, versionChainEntry{id: currentId, root: root})
		parentId, ok := tryGetString(root, "inheritsFrom")
		if !ok || strings.TrimSpace(parentId) == "" {
			break
		}
		if err := validateVersionId(parentId); err != nil {
			return nil, err
		}
		currentId = parentId
	}

	// 自最顶层依赖（原版）向实例方向合并
	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}
	return chain, nil
}

// mergeFlattenChain 合并继承链为单个自包含 JSON 对象（id 使用实例请求的版本 id）。
// 标量字段沿链后者覆盖（子级优先）；arguments.jvm/game 按父→子顺序拼接；
// libraries 按 group:artifact:classifier@ext 键去重、子级覆盖，保持首次出现顺序。
// 注意：inheritsFrom / jar 不复制——前者是本次要移除的依赖声明，
// 后者指向的版本 JAR 已复制为实例自己的 {id}.jar。
func mergeFlattenChain(requestedVersionId string, chain []versionChainEntry) (*orderedObject, error) {
	merged := newOrderedObject()

	// 标量字段：沿链后者覆盖（子级优先）
	for _, propertyName := range []string{
		"time", "releaseTime", "type", "mainClass",
		"assets", "minecraftArguments",
	} {
		if value := lastChainValue(chain, propertyName); value != nil {
			merged.set(propertyName, value)
		}
	}

	// 声明 id：链末（实例自身）声明的 id 优先；请求的目录名兜底
	declaredId, hasDeclaredId := "", false
	for _, entry := range chain {
		if value, ok := tryGetString(entry.root, "id"); ok && strings.TrimSpace(value) != "" {
			declaredId = value
			hasDeclaredId = true
		}
	}
	if !hasDeclaredId || strings.TrimSpace(declaredId) == "" {
		declaredId = requestedVersionId
	}
	merged.set("id", mustMarshalString(declaredId))

	// assetIndex / javaVersion / logging：对象整体沿链覆盖。
	// logging 必须保留：log4j 的配置声明只在原版 JSON 里，扁平化后实例不再有
	// inheritsFrom，丢掉它控制台日志就会退回 log4j 默认配置。
	for _, propertyName := range []string{"assetIndex", "javaVersion", "logging"} {
		if value := lastChainValue(chain, propertyName); value != nil {
			merged.set(propertyName, value)
		}
	}

	// 客户端下载声明：沿链取最后一个含 downloads.client 的节点（通常为原版）
	for _, entry := range chain {
		if clientNode := downloadsClientNode(entry.root); clientNode != nil {
			downloads := newOrderedObject()
			downloads.set("client", clientNode)
			serialized, err := downloads.marshal()
			if err != nil {
				return nil, err
			}
			merged.set("downloads", serialized)
		}
	}

	// arguments：jvm / game 数组按父→子顺序拼接（与启动侧合并语义一致）
	var jvmArguments, gameArguments []json.RawMessage
	hasArguments := false
	for _, entry := range chain {
		argumentsRaw, ok := entry.root["arguments"]
		if !ok {
			continue
		}
		var arguments map[string]json.RawMessage
		if json.Unmarshal(argumentsRaw, &arguments) != nil {
			continue
		}
		hasArguments = true
		jvmArguments = appendJsonArray(jvmArguments, arguments["jvm"])
		gameArguments = appendJsonArray(gameArguments, arguments["game"])
	}
	if hasArguments {
		arguments := newOrderedObject()
		if len(jvmArguments) > 0 {
			arguments.set("jvm", mustMarshal(jvmArguments))
		}
		if len(gameArguments) > 0 {
			arguments.set("game", mustMarshal(gameArguments))
		}
		serialized, err := arguments.marshal()
		if err != nil {
			return nil, err
		}
		merged.set("arguments", serialized)
	}

	// libraries：按坐标键去重，子级覆盖，保持首次出现顺序
	libraryIndexes := map[string]int{}
	var libraries []json.RawMessage
	for _, entry := range chain {
		librariesRaw, ok := entry.root["libraries"]
		if !ok {
			continue
		}
		var chainLibraries []json.RawMessage
		if json.Unmarshal(librariesRaw, &chainLibraries) != nil {
			continue
		}
		for _, library := range chainLibraries {
			key := libraryCoordinateKey(library)
			if existingIndex, ok := libraryIndexes[key]; ok {
				libraries[existingIndex] = library
			} else {
				libraryIndexes[key] = len(libraries)
				libraries = append(libraries, library)
			}
		}
	}
	merged.set("libraries", mustMarshal(libraries))

	return merged, nil
}

// lastChainValue 沿链取属性最后一个非空值（子级覆盖父级）。
func lastChainValue(chain []versionChainEntry, propertyName string) json.RawMessage {
	var last json.RawMessage
	for _, entry := range chain {
		if value, ok := entry.root[propertyName]; ok && len(value) > 0 {
			last = value
		}
	}
	return last
}

// downloadsClientNode 读取 downloads.client 节点；不存在时返回 nil。
func downloadsClientNode(root map[string]json.RawMessage) json.RawMessage {
	downloadsRaw, ok := root["downloads"]
	if !ok {
		return nil
	}
	var downloads map[string]json.RawMessage
	if json.Unmarshal(downloadsRaw, &downloads) != nil {
		return nil
	}
	client, ok := downloads["client"]
	if !ok || len(client) == 0 {
		return nil
	}
	return client
}

// resolveClientJarProviderId 解析客户端 JAR 的来源版本：显式 jar 字段优先
// （取最后声明），否则为链中最后一个声明 downloads.client 的版本 id。
// 同一节点内 downloads.client 先生效、显式 jar 字段后生效（与启动侧一致）。
func resolveClientJarProviderId(chain []versionChainEntry) string {
	providerId := ""
	for _, entry := range chain {
		if downloadsClientNode(entry.root) != nil {
			providerId = entry.id
		}
		if jarVersion, ok := tryGetString(entry.root, "jar"); ok && strings.TrimSpace(jarVersion) != "" {
			providerId = jarVersion
		}
	}
	return providerId
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

func mustMarshal(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return data
}

func mustMarshalString(value string) json.RawMessage {
	return mustMarshal(value)
}

func copyFileContents(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer target.Close()
	if _, err := target.ReadFrom(source); err != nil {
		return err
	}
	return nil
}
