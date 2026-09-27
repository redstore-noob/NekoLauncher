# NekoLauncher 插件开发指南

欢迎来到 NekoLauncher 的插件开发文档！你可以通过该文档编写属于自己的插件，或者直接将该文件输入给 AIGC 工具以生成插件。

本指南同时是 **v1 API 规范**：自发布日起算 v1，此后破坏性改动一律递增主版本号（`plugin.yaml` 里的 `api` 字段只比主版本号）。

## 1. 信任模型（必读）

**插件 = 与启动器同权限的受信代码。** 插件在启动器 WebView 内以普通前端代码运行，除了本文列出的显式 API，它技术上可以触及 `window.go` 上的**全部宿主绑定**——包括但不限于任意路径的文件读写（`SystemAPI.ReadTextFile` / `WriteTextFile`）、删除实例、移除账号、卸载其它插件、退出启动器。

这不是疏漏，是明确的取舍：单机桌面应用、安装来源可追溯，换取扩展能力的上限。因此：

- **安装插件 = 把代码交给它**。安装提示与插件管理页会明示这一点；
- 权限系统（§5）是**声明制契约与入口收敛**，不是安全沙箱，不构成任何隔离承诺；
- 除非未来引入真正的隔离运行时（iframe / 受限 Worker，工作量重写级），文档不会暗示"沙箱"。

## 2. 插件文件夹格式

一个完整可用的插件目录：

```
<插件id>/                ← 目录名必须等于清单 id（宿主强校验）
├── plugin.yaml          ← 唯一声明文件（见 §4）
├── icon.png             ← 图标（固定名，可选）
├── index.js             ← 编译产物 = 清单默认入口
└── src/…                ← 源码（作者保留，随包分发）
```

- 编写语言 **TSX / JSX / 手写 JS**（`h()`）皆可；**分发物永远是编译后的单文件 ESM `index.js`**，启动器的生产加载路径上没有编译器；
- **打包**：`.nekoex`（zip + 识别后缀）；启动器内的打包工具会把 JSX 等源文件编译为 JS 后再打包；
- **源码随包分发是特性**：信任模型要求用户能读到插件在做什么；
- 静态资源放目录内，用 `new URL("./assets/x.png", import.meta.url)` 引用；宿主的 Tailwind 工具类对插件 DOM 可用，但仅限宿主源码出现过的类，任意值请用 inline style。

## 3. 第一个插件（dev 模式，零工具链）

在 `plugin.yaml` 里加 `dev: true`，入口写 `index.jsx` 源码：宿主会懒加载 Sucrase 现场编译后加载（无 dev 插件时零开销）。保存后点插件页的「重新加载」即生效。

```yaml
# plugin.yaml
id: account-quick-switch
name: 账号快切
version: "1.0.0"
api: "1"
description: 主页小组件：一键切换登录账号
capabilities:
  storage: true
settings:
  showAvatar: "1"        # 默认设置，首次加载种入 api.config
```

```js
// index.js —— 手写 JS 版（dev 模式也可直接写 index.jsx 用 JSX 语法）
export default function activate(api) {
  const { h, ui, icons, notify } = api;

  api.registerWidget({
    id: "account-switcher",       // 实际注册为 "<插件id>:account-switcher"
    title: "账号快切",
    description: "在主页一键切换登录账号",
    icon: h(icons.Key20Regular),  // 注意：icon 要节点不是组件
    tileClass: "from-violet-500 to-fuchsia-500",
    render: function AccountCard({ context }) {
      const { accounts, selectedAccount, onSelectAccount } = context;
      if (accounts.length === 0) {
        return h("div", { className: "text-xs text-gray-400" }, "还没有添加账号");
      }
      return h(ui.Card, { shadow: "sm" },
        h(ui.CardBody, { className: "flex flex-wrap gap-1.5" },
          accounts.map((account) =>
            h(ui.Button, {
              key: account.key,
              size: "sm",
              color: "primary",
              variant: account.key === selectedAccount?.key ? "solid" : "flat",
              onPress: () => void onSelectAccount(account.key),
            }, account.name),
          ),
        ),
      );
    },
  });

  api.notify.success("账号快切已加载");
}
```

发布时用插件模板仓库（esbuild）把 TSX/JSX 编译为 `index.js` 再打包；从手写 `h()` 迁到 JSX 是机械翻译。

> **官方示例**：[`examples/server-status/`](../examples/server-status/) —— 服务器状态小组件，完整演示 settings 种子、权限声明、轮询 + `onCleanup`、`getServerStatus`、`setClipboard` 与 `onJoin` 快速进服。拷进插件目录点「重新加载」即可试用。

## 4. plugin.yaml 清单

```yaml
id: playtime-stats          # 必填，= 目录名
name: 游戏时长统计
version: "1.0.0"            # 所有版本字段必须带引号（防 YAML 类型强转，如 1.20 → 1.2）
api: "1"                    # 目标宿主主版本
description: …
author: …

capabilities:               # 权限声明（见 §5）
  storage: true
  launch: true

settings:                   # 默认设置：首次加载种入 api.config（仅空键写入）
  dailyGoalHours: "2"
```

- `entry` / `icon` 字段不存在：入口固定 `index.js`（dev 插件 `index.jsx`），图标固定 `icon.png`；
- **不存在"改写启动命令行"的通道**：插件只能通过 `launchSelected` / `launchVersion` 走与用户手点完全相同的启动正门。

## 5. 权限系统（声明制，未声明 = 报错）

受权限控制的 API **始终存在**，但调用时校验清单的 `capabilities`：**未声明对应权限直接抛错**（错误信息指明缺哪个权限、如何声明），绝不静默失败。

| 权限键 | 解锁的 API |
|---|---|
| `storage` | `config.get / set / clear` |
| `launch` | `launchSelected` / `launchVersion` |
| `instances` | `getInstances` / `getSaves` / `selectInstance` / `getVersionProfile` / `saveVersionProfile` / `getVersionDetails` / `getScreenshots` |
| `accounts` | `getAccounts` |
| `launcher-config` | `getLauncherSettings` / `saveLauncherSettings` |
| `notifications` | `notify.*` |
| `clipboard` | `setClipboard` |
| `open-url` | `openUrl` |
| `open-path` | `openPath`（叠加"仅插件目录内"的宿主侧限制） |
| `server-status` | `getServerStatus` |

无需权限（永远可用）：元信息、`react/h/Fragment/ui/icons/HomeCard`、`registerWidget` / `registerPage`、`log`、`t`、`confirm`、`getLaunchState`、`onLaunchPhaseChange`、`onInstancesChanged`、`onCleanup`。插件管理页会展示每个插件声明的权限列表。

**小组件卡片壳约定**：主页组件列对指针事件做了统一管理，`registerWidget` 注册的小组件会被宿主**自动装入标准卡片壳**（主题描边、圆角内边距、事件恢复）——因此：

- 组件内容**不要自带卡片背景/外壳**（不要再套一层 Card），直接输出内容行即可；
- 要与内置组件一致的「图标 + 标题 + 大数值」头部，用 `api.HomeCard`；
- 长按 1 秒拖动排序、错误边界与内置组件完全相同，无需插件处理。

## 6. 运行时 API（v1 全表）

| 组 | 成员 | 权限 | 说明 |
|---|---|---|---|
| 元信息 | `apiVersion` / `plugin{id,name,version}` | — | 只读 |
| 建材 | `react` / `h` / `Fragment` / `ui`(白名单) / `icons`(白名单) / `HomeCard` | — | 宿主注入，插件不得自带 React |
| 扩展点 | `registerWidget` / `registerPage` | — | id 自动加 `<插件id>:` 前缀；页面 render 收 `PageRenderContext`（随启动状态自动重渲染） |
| 生命周期 | `onCleanup` | — | 注册清理函数，卸载/重载时宿主依次调用 |
| 反馈 | `log` / `t`(占位插值) / `confirm` | — | 宿主统一样式 |
| 反馈 | `notify`(4 级) | `notifications` | 另有频控：5s 内最多 3 条 |
| 系统 | `setClipboard` | `clipboard` | 只写不读 |
| 系统 | `openUrl` | `open-url` | 仅 http(s) |
| 系统 | `openPath` | `open-path` | 仅插件目录内 |
| 查询 | `getInstances` / `getSaves` / `getVersionDetails` / `getScreenshots` | `instances` | 只读；`getVersionDetails` 含加载器信息与全部内容列表 |
| 写入 | `selectInstance`（切换全局选中）/ `getVersionProfile` / `saveVersionProfile`（实例启动档案） | `instances` | 应基于 get 的返回值原样修改后写回 |
| 查询 | `getAccounts` | `accounts` | 只读摘要（含头像），**凭据不出宿主** |
| 查询 | `getLaunchState` | — | 只读启动状态 |
| 查询 | `getServerStatus` | `server-status` | 连接失败抛错由插件接住 |
| 写入 | `getLauncherSettings` / `saveLauncherSettings`（全局启动设置） | `launcher-config` | 应基于 get 的返回值原样修改后写回 |
| 启动 | `launchSelected` / `launchVersion` | `launch` | 与手点同管线；`launchVersion` 校验版本存在且**不改变**用户当前选中 |
| 事件 | `onLaunchPhaseChange` / `onInstancesChanged` | — | 返回取消订阅函数；卸载/重载时宿主自动清理 |
| 设置 | `config.get / set / clear` | `storage` | 键前缀隔离；仅字符串值；种子来自清单 `settings` |

`WidgetRenderContext`（小组件注入）自 v1 封版：只增不改名，改名/删除即升主版本。

## 7. 明确不做的

运行时编译进生产路径、跨插件通信、任意路径文件系统访问、网络代理封装、包签名、通用事件总线——每一项都等第一个真实场景来拽再议。

## 8. 给未来改 API 的人

加一个 API 前先过三问：有真实场景拽着吗？内置实现自己会用到吗？插件是否反正能绕过 API 摸到（能绕 = 更该收编进 API 统一管）？三问皆"是"才加；删除或改名 = 主版本号 +1。
