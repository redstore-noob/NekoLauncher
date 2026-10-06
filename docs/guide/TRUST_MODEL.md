# 信任模型（必读）

插件在启动器 UI 里运行代码，与宿主同处一个 WebView。本文说明这套系统的
能力边界、它防什么、不防什么。入门路线见
[Extensions_Guide.md](Extensions_Guide.md)。

**插件 ≠ 与启动器同权限。** 宿主在启动最早时刻安装"后端闸门"
（`frontend/src/plugin/backend-gate.ts`）：

- `window.go`（Wails 注入的全部 Go 绑定）被收进宿主闭包，全局只剩一个只抛错的 getter；
- 所有能直呼 Go 方法的原始出口（`window.WailsInvoke`、`window.ObfuscatedCall`、
  `chrome.webview.postMessage` / `webkit` 消息通道）都被上闸——Wails 的任意方法调用
  一律是 `'C'/'c'` 前缀消息，闸门只放行宿主自己的调用帧；
- 宿主代码本身经构建期重写（`vite.config.ts` 的 `rewriteWailsBindings`）走闭包内的
  包装视图，行为不变。

因此插件**只能**通过 [API_REFERENCE.md](API_REFERENCE.md) 列出的注入 `api`
对象使用宿主能力。绕过尝试（直呼 `window.go`、伪造 IPC 消息）会抛错，不会到达 Go 侧。

仍然成立的边界与承诺：

- 这是**能力隔离**，不是密码学沙箱：插件与宿主同处一个 JS realm，理论上存在同
  realm 的旁路（如 dev 模式下经 Vite 开发服务器 import 宿主模块）；生产构建中宿主
  模块已打进 bundle、无可 import 的 URL；
- `window.runtime`（关窗、退出、事件订阅等固定动作）与 `window.wails`（flags）仍在
  全局——Wails 运行时内部按全局名使用它们，且它们只能触发固定消息类型，无法任意
  调用 Go 方法；
- 安装插件 = 信任它在你自己的启动器 UI 里运行代码：它仍能读取界面状态、注入样式
  与 DOM、通过授权后的 `api` 发起网络请求。安装提示与插件管理页会明示这一点；
- 权限系统（[PERMISSIONS.md](PERMISSIONS.md)）是声明制契约 + 用户开关 + 高危动作
  确认，与闸门互补：闸门决定"能不能碰"，权限决定"让不让用"。
