// NekoSolo v3 首启内容补全：安装器只把待装 mrpack 落盘（marker.PendingPayload），
// Minecraft 本体、加载器、mod 与 Java 运行时统一由启动器首次启动时补全——
// 复用启动器既有的下载基础设施（并行下载 / 镜像回退 / 下载中心任务 UI），
// 避免在 C# 安装器里重写一整套下载逻辑。
//
// solo 包不依赖 download/instance（会成环），实际补全逻辑由 bindings 层
// 通过 PendingPayloadHook 注入；这里只负责定义钩子与触发时机。
package solo

// PendingPayloadHook 首启内容补全钩子（bindings 层注入）。marker 里带着
// 待装 mrpack 路径与实例元数据；返回 nil 表示补全完成（调用方随后清掉
// pendingPayload 字段），非nil 时下次启动重试。
var PendingPayloadHook func(marker Marker) error
