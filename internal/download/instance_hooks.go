// instance_hooks.go download 包对宿主「实例管理」能力的钩子声明：
// 实例扫描/选中、内容目录解析、安装布局解析、版本继承扁平化。
//
// 为什么是钩子而不是直接 import：真实实现在 internal/instance 与 internal/launch，
// 二者都依赖本包（instance 的内容扫描、launch 的启动前校验），直接 import 会成环。
// 钩子由 internal/bindings/wiring.go 在启动时注入；未注入时的降级行为见各函数注释。
//
// 对应 C# 时代的 GameInstanceStore / GameVersionIsolation /
// GameInstanceLayoutResolver / VersionJsonFlattener。
// 配置读写已直接依赖 internal/config，不再需要钩子。
package download

import "context"

// ---------------------------------------------------------------------------
// 实例扫描与选中（GameInstanceStore）
// ---------------------------------------------------------------------------

// RefreshInstancesHook 扫描并刷新实例列表（对应 GameInstanceStore.RefreshAsync）。
var RefreshInstancesHook func(gameDirectory string) error

// SelectInstanceHook 选中实例（对应 GameInstanceStore.Select）。
var SelectInstanceHook func(instanceID string)

// RefreshInstances 刷新实例列表（无钩子时为空操作）。
func RefreshInstances(gameDirectory string) error {
	if RefreshInstancesHook != nil {
		return RefreshInstancesHook(gameDirectory)
	}
	return nil
}

// SelectInstance 选中实例（无钩子时为空操作）。
func SelectInstance(instanceID string) {
	if SelectInstanceHook != nil {
		SelectInstanceHook(instanceID)
	}
}

// ---------------------------------------------------------------------------
// 目录布局解析（GameVersionIsolation / GameInstanceLayoutResolver）
// ---------------------------------------------------------------------------

// ResolveContentDirectoryHook 解析实例的内容目录（对应 GameVersionIsolation.Resolve）。
// 返回 mods / resourcepacks 等的父目录。nil 时退化为 minecraftDirectory 本身。
var ResolveContentDirectoryHook func(minecraftDirectory, sourcePath, versionID string) string

// ResolveInstanceLayoutHook 解析实例布局（对应 GameInstanceLayoutResolver.Resolve）。
// 返回内容目录。参数：（目标根目录, 活跃游戏目录, 实例名, 版本ID, 全局默认隔离）。
var ResolveInstanceLayoutHook func(targetRoot, sourcePath, instanceName, versionID string, defaultIsolation bool) string

// ---------------------------------------------------------------------------
// 版本 JSON 扁平化（VersionJsonFlattener）
// 本包不保留最小实现——此前的自制版本合并语义不完整（arguments 整体替换、
// 无 clientVersion 元字段、非原子写入），Loader 安装一律走 launch 包的完整实现。
// ---------------------------------------------------------------------------

// FlattenVersionJSONHook 把实例的 inheritsFrom 继承链合并为自包含版本 JSON
// 并复制客户端 JAR（对应 launch.VersionJsonFlattener.Flatten）。
var FlattenVersionJSONHook func(ctx context.Context, minecraftDirectory, versionId string) error

// IsVersionReferencedHook 检查指定版本是否仍被其它版本的 inheritsFrom 引用
// （对应 launch.VersionJsonFlattener.IsVersionReferenced）。
var IsVersionReferencedHook func(minecraftDirectory, versionId string) bool

// FlattenVersionJSON 扁平化实例继承链（无钩子时为空操作：未接线时安装的实例
// 保持继承结构，仍可启动）。
func FlattenVersionJSON(ctx context.Context, minecraftDirectory, versionId string) error {
	if FlattenVersionJSONHook != nil {
		return FlattenVersionJSONHook(ctx, minecraftDirectory, versionId)
	}
	return nil
}

// IsVersionReferenced 检查版本是否仍被引用（无钩子时保守返回 true，不删依赖目录）。
func IsVersionReferenced(minecraftDirectory, versionId string) bool {
	if IsVersionReferencedHook != nil {
		return IsVersionReferencedHook(minecraftDirectory, versionId)
	}
	return true
}
