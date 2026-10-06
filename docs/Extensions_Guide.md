# NekoLauncher 插件开发指南

欢迎来到 NekoLauncher 的插件开发文档！你可以通过该文档编写属于自己的插件，或者直接将该文件输入给 AIGC 工具以生成插件。

本指南同时是 **v1 API 规范**：自发布日起算 v1，此后破坏性改动一律递增主版本号（`plugin.yaml` 里的 `api` 字段只比主版本号）。

## 1. 信任模型（必读）

**插件 ≠ 与启动器同权限。** 宿主在启动最早时刻安装"后端闸门"（`frontend/src/plugin/backend-gate.ts`）：

- `window.go`（Wails 注入的全部 Go 绑定）被收进宿主闭包，全局只剩一个只抛错的 getter；
- 所有能直呼 Go 方法的原始出口（`window.WailsInvoke`、`window.ObfuscatedCall`、`chrome.webview.postMessage` / `webkit` 消息通道）都被上闸——Wails 的任意方法调用一律是 `'C'/'c'` 前缀消息，闸门只放行宿主自己的调用帧；
- 宿主代码本身经构建期重写（`vite.config.ts` 的 `rewriteWailsBindings`）走闭包内的包装视图，行为不变。

因此插件**只能**通过本文列出的注入 `api` 对象使用宿主能力。绕过尝试（直呼 `window.go`、伪造 IPC 消息）会抛错，不会到达 Go 侧。

仍然成立的边界与承诺：

- 这是**能力隔离**，不是密码学沙箱：插件与宿主同处一个 JS realm，理论上存在同 realm 的旁路（如 dev 模式下经 Vite 开发服务器 import 宿主模块）；生产构建中宿主模块已打进 bundle、无可 import 的 URL；
- `window.runtime`（关窗、退出、事件订阅等固定动作）与 `window.wails`（flags）仍在全局——Wails 运行时内部按全局名使用它们，且它们只能触发固定消息类型，无法任意调用 Go 方法；
- 安装插件 = 信任它在你自己的启动器 UI 里运行代码：它仍能读取界面状态、注入样式与 DOM、通过授权后的 `api` 发起网络请求。安装提示与插件管理页会明示这一点；
- 权限系统（§5）是声明制契约 + 用户开关 + 高危动作确认，与闸门互补：闸门决定"能不能碰"，权限决定"让不让用"。

## 2. 插件文件夹格式

一个完整可用的插件目录：

```
<插件id>/                ← 目录名必须等于清单 id（宿主强校验）
├── plugin.yaml          ← 唯一声明文件（见 §4）
├── icon.png             ← 图标（固定名，可选）
├── index.js             ← 编译产物 = 清单默认入口
├── theme.css            ← 清单 styles 声明的样式文件（可选，存盘即热生效）
└── src/…                ← 源码（作者保留，随包分发）
```

- 编写语言 **TSX / JSX / 手写 JS**（`h()`）皆可；**分发物永远是编译后的单文件 ESM `index.js`**，启动器的生产加载路径上没有编译器；
- **打包**：`.nekoex`（zip + 识别后缀）；启动器内的打包工具会把 JSX 等源文件编译为 JS 后再打包；
- **源码随包分发是特性**：信任模型要求用户能读到插件在做什么；
- 静态资源放目录内，用 `new URL("./assets/x.png", import.meta.url)` 引用；宿主的 Tailwind 工具类对插件 DOM 可用，但仅限宿主源码出现过的类，任意值请用 inline style。要改**其它控件**的样式（包括宿主自身的），用清单 `styles` 字段或 `api.styles.inject`（见 §5/§6）；受支持的定制锚点（CSS 变量与 `nya-*` 语义类）见 [CSS_STYLE_TABLE.md](CSS_STYLE_TABLE.md)。

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

styles:                     # 样式文件（相对插件目录、限 .css）：加载时自动注入为全局 CSS
  - theme.css               # 改动会被宿主监听，保存即热生效（约 3 秒内），无需重新加载
```

- `entry` / `icon` 字段不存在：入口固定 `index.js`（dev 插件 `index.jsx`），图标固定 `icon.png`；
- **不存在"改写启动命令行"的通道**：插件只能通过 `launchSelected` / `launchVersion` 走与用户手点完全相同的启动正门。

## 5. 权限系统（声明制，未声明 = 报错）

受权限控制的 API **始终存在**，但调用时校验清单的 `capabilities`：**未声明对应权限直接抛错**（错误信息指明缺哪个权限、如何声明），绝不静默失败。

| 权限键 | 解锁的 API |
|---|---|
| `storage` | `config.get / set / clear` |
| `launch` | `launchSelected` / `launchVersion` |
| `instances` | 只读查询：`getInstances` / `getSaves` / `getVersionProfile` / `getVersionDetails` / `getScreenshots` |
| `instances-write` | 改全局状态：`selectInstance` / `saveVersionProfile` / `setContentEnabled`（兼容旧的 `instances`，但新插件请显式声明写权限） |
| `downloads` | 只读查询：`getDownloadTasks` / `onDownloadTasksChanged` / `getVersions` / `getModLoaderVersions` / `getDownloadSources` / `getJavaRuntimes` |
| `downloads-write` | 写：`startDownload` / `startModLoaderDownload` / `downloadResource` / `installModpack`（**不**兼容 `downloads`：读权限不构成写权限） |
| `system-status` | 只读机器占用：`getMemorySnapshot` / `getSystemUsage` / `getDiskUsage` |
| `music` | 只读音乐库：`getCurrentTrack` / `getMusicTracks`（**默认关闭**） |
| `logs` | 只读运行日志：`onLogLine`（日志含路径 / 账号名 / 服务器地址 / 报错原文；**默认关闭**） |
| `network` | 联网（宿主代插件发起的请求）：`getVersions` / `getModLoaderVersions` / `getServerStatus` / `startDownload` / `startModLoaderDownload` / `downloadResource` / `installModpack`（**默认关闭**；可声明表达意图，但不是必须） |
| `accounts` | `getAccounts` |
| `launcher-config` | `getLauncherSettings` |
| `launcher-config-write` | `saveLauncherSettings`（兼容旧的 `launcher-config`，但新插件请显式声明写权限） |
| `notifications` | `notify.*` |
| `clipboard` | `setClipboard` |
| `open-url` | `openUrl` |
| `open-path` | `openPath`（叠加"仅插件目录内"的宿主侧限制） |
| `server-status` | `getServerStatus`（叠加联网开关） |
| `styles` | `styles.inject` / `styles.remove`（全局 CSS 注入，可自定义任意控件样式） |
| `ipc` | 插件间通信：`ipc.send` / `ipc.broadcast` / `ipc.onMessage` / `ipc.plugins`（见下方插件间通信约定） |

读 / 写权限拆分（`instances-write`、`launcher-config-write`）的动机：让用户在安装页
就能看出插件**会不会改全局状态**。只声明读权限的存量插件调用写 API 仍会放行并记
一条控制台警告，但新插件应直接声明写权限，后续版本可能移除该兼容。

`downloads-write` 是唯一的例外：**它不认 `downloads`**。拆分兼容是给"拆分之前就存在"的
插件用的，而 `downloads` 是本轮新增的权限、没有存量插件——放行只会让"只想看下载进度"
的插件静默拿到写盘与安装能力，正好违背拆权的初衷。所以下载域必须显式声明
`downloads-write`，写侧 API 一个都不会因为声明了读权限而放行。

**权限有三层闸**（声明只是第一层）：

| 层 | 在哪 | 作用 | 不满足时 |
|---|---|---|---|
| ① 声明 | `plugin.yaml` 的 `capabilities` | 安装页可见的用途清单 | 调用**直接抛错**（绝不静默） |
| ② 授权 | **插件页每个权限一个 Switch** | 用户随时收回某一项能力，不必卸载插件 | 相关 API **返回 null**（不执行、不抛错），控制台记一条警告 |
| ③ 动作 | 高危动作的 NekoPrompt / 中危动作的 NekoAlert | 每次真正动手前的确认与留痕 | 拒绝即抛错，磁盘上不留改动 |

- **默认关闭**的四项：`network`（联网）、`logs`（运行日志）、`accounts`（账号）、`music`（音乐库）。其余权限"声明即开启"——默认关太宽会把新插件一律弄成"坏的"，反而逼用户闭眼全开；而需要动作级确认的写权限（`downloads-write` 等）每一次动作本来都要过 NekoPrompt；
- **`network` 是用户独占的横切开关**（不要求声明，但可以声明）：它管的是"宿主代插件发的网络请求"——版本清单、Loader 元数据、服务器状态、资源与整合包下载。插件**自己** `fetch()` 不受它约束（信任模型 §1 决定了拦不住），所以别把它当成防火墙；
- 授权表存在 `launcher.yaml` 的 `pluginPermissions` 键，**不是沙箱的一部分**：与 §1 声明的一致，这套开关给的是"用户看得见、关得掉、误伤有救"，不是"防住恶意插件"；
- `plugin.yaml` 里的 `capabilities` 可以写 `network: true` 表达意图（安装页会显示），但开关本身对每个插件都可见；
- 插件必须**判空**：受权限控制的成员返回类型都带 `| null`（订阅类 `onDownloadTasksChanged` / `onLogLine` 同样返回 `null`），`null` 的含义就是"用户关掉了这一项"。

无需权限（永远可用）：元信息、`react/h/Fragment/ui/icons/HomeCard`、`registerWidget` / `registerPage` / `registerPageAction` / `registerLaunchCard`、`log`、`t`、`confirm`、`navigateToPage`、`getLaunchState`、`onLaunchPhaseChange`、`onInstancesChanged`、`onGameExit`、`onCleanup`。插件管理页会展示每个插件声明的权限列表。

**高危动作：确认与公示**：权限声明只说明"插件请求了什么"，看不出它**此刻正在做什么**。所以高危动作有两道闸：

| 动作 | 闸门 | 触发 API |
|---|---|---|
| 发起游戏启动 | **NekoPrompt 确认** | `launchSelected` / `launchVersion` |
| 下载 / 安装 | **NekoPrompt 确认** | `startDownload` / `startModLoaderDownload` / `downloadResource` / `installModpack` |
| 写实例启动档案（Java 路径 / 包装命令 / JVM 参数） | **NekoPrompt 确认** | `saveVersionProfile` |
| 写启动器全局设置（同上，作用于所有实例） | **NekoPrompt 确认** | `saveLauncherSettings` |
| 启停实例内容 | 只公示（NekoAlert） | `setContentEnabled` |
| 注入全局样式 | 只公示（NekoAlert） | `styles.inject` |
| 切换当前实例 | 只公示（NekoAlert） | `selectInstance` |

确认的口径：

- **闸门在动手之前**：用户在确认框里拒绝时，动作以错误结束（`插件「X」的「…」操作被用户拒绝`），磁盘上不留下任何改动——插件不能把"被拒绝"当成成功；
- 确认框有三个按钮：**允许一次** / **本次运行内允许** / **拒绝**（默认落在拒绝上，回车不等于同意）。同意按（插件 + 动作）记在本次运行内，所以批处理（一次装 20 个 mod）只问一次；
- 拒绝同样记下：同一动作在本次运行内直接失败、不再弹框（插件用对话框刷屏也没用）。**重新加载插件即可重新询问**；
- 并发调用共用一个确认框：插件一口气发起 5 次也只问一次；
- **公示是宿主行为，插件关不掉**：同意之后每次执行仍会在左下角弹一条 NekoAlert（同一插件同一动作 4 秒合并成一条，全局 3 秒最多 4 条）。权限决定"能不能调"，确认决定"这次允不允许"，公示决定"用户看不看得见"——三条合起来才是这套系统给的承诺：插件可以强，但不能悄悄改东西。

**危险读的收口**（只读，但能翻出用户机器上的东西）：

| 读 | 收口方式 |
|---|---|
| `onLogLine` 运行日志 | 从免权限改为需要 `logs` 声明（安装页可见）；日志里有路径、账号名、服务器地址与报错原文 |
| `getSaves(savesDirectory)` | 路径必须落在**已知游戏根目录**内（当前实例的 .minecraft / 游戏目录 / 来源目录 + 设置里额外扫描的游戏目录）；越界返回空数组并记一条 WARN 日志，不再是"任意目录枚举器" |
| `getAccounts` | 已经是展示摘要，凭据不出宿主（无变化） |
| `getMusicTracks` / `getScreenshots` | 已分别受 `music` / `instances` 权限约束（无变化） |

确认框与公示的文案都走 i18n（`zh-CN` 原文即 key，其它语言的词条缺了就回落中文）。

**小组件卡片壳约定**：主页组件列对指针事件做了统一管理，`registerWidget` 注册的小组件会被宿主**自动装入标准卡片壳**（主题描边、圆角内边距、事件恢复）——因此：

- 组件内容**不要自带卡片背景/外壳**（不要再套一层 Card），直接输出内容行即可；
- 要与内置组件一致的「图标 + 标题 + 大数值」头部，用 `api.HomeCard`；
- 长按 1 秒拖动排序、错误边界与内置组件完全相同，无需插件处理。

**启动卡覆盖约定**：`api.registerLaunchCard({ render })` 可以整体替换主页右侧启动卡的内容：

- render 收到 `LaunchCardContext`——与内置卡完全相同的数据与回调（版本列表 / 选中版本 /
  账号列表 / 启动停止 / 刷新 / 打开目录），宿主在外层提供面板容器、滑动切换与拖动删除区，
  **不要再自带整块面板背景**；
- 独占槽：同时只有一张覆盖卡生效（最后注册者赢）；插件被停用 / 卸载 / 重载失败后
  宿主自动摘除覆盖并回落内置卡片，无需插件清理；
- 覆盖卡出错只坏这一张卡（宿主套错误边界），不影响主页其余部分；
- 只想微调内置卡样式而不是换整张卡的，优先用清单 `styles` / `styles.inject` 注入 CSS，
  内置卡根节点带 `data-nya="launch-card"` 选择锚点。

## 6. 运行时 API（v1 全表）

| 组 | 成员 | 权限 | 说明 |
|---|---|---|---|
| 元信息 | `apiVersion` / `plugin{id,name,version}` | — | 只读 |
| 建材 | `react` / `h` / `Fragment` / `ui`(白名单) / `icons`(白名单) / `HomeCard` | — | 宿主注入，插件不得自带 React |
| 扩展点 | `registerWidget` / `registerPage` / `registerLaunchCard` | — | 前两者 id 自动加 `<插件id>:` 前缀；页面 render 收 `PageRenderContext`（随启动状态自动重渲染）；`registerLaunchCard` 是独占槽（见下方启动卡覆盖约定） |
| 扩展点 | `registerPageAction` | — | 往宿主**已有页面**里加一个按钮（目前 `instances` / `download` 渲染插槽），不是整页占位（见下方页面按钮约定） |
| 生命周期 | `onCleanup` | — | 注册清理函数，卸载/重载时宿主依次调用 |
| 反馈 | `log` / `t`(占位插值) / `confirm` | — | 宿主统一样式 |
| 反馈 | `notify`(4 级) | `notifications` | 另有频控：5s 内最多 3 条 |
| 系统 | `setClipboard` | `clipboard` | 只写不读 |
| 系统 | `openUrl` | `open-url` | 仅 http(s) |
| 系统 | `openPath` | `open-path` | 仅插件目录内 |
| 查询 | `getInstances` / `getSaves` / `getVersionProfile` / `getVersionDetails` / `getScreenshots` | `instances` | 只读；`getVersionDetails` 含加载器信息与全部内容列表。`getSaves` 的路径被限制在已知游戏根目录内（见上方危险读收口） |
| 写入 | `selectInstance`（切换全局选中）/ `saveVersionProfile`（实例启动档案） | `instances-write`（兼容旧 `instances`） | 应基于 get 的返回值原样修改后写回；`saveVersionProfile` 会先弹确认框（档案里的 Java 路径 / 包装命令等同"下次启动执行什么"） |
| 查询 | `getAccounts` | `accounts` | 只读摘要（含头像），**凭据不出宿主** |
| 查询 | `getLaunchState` | — | 只读启动状态 |
| 查询 | `getDownloadTasks` | `downloads` | 只读：全部下载任务的完成状态（游戏本体 / 内容资源 / 整合包 / Java 同源，见下方下载任务约定） |
| 查询 | `getVersions` / `getModLoaderVersions` / `getDownloadSources` / `getJavaRuntimes` | `downloads` | 只读：版本清单、Loader 可用版本、下载源与托管 Java 运行时——发起下载的前置数据 |
| 写入 | `startDownload` / `startModLoaderDownload` / `downloadResource` / `installModpack` | `downloads-write` | 与下载页 / 资源页同一条管线：进度进右下角下载中心、可暂停取消；后端拒绝（已有下载在跑等）时**抛错**，不返回假成功。声明 `downloads` **不会**解锁这些（见 §5） |
| 写入 | `setContentEnabled`（Mod 等启停） | `instances-write`（兼容旧 `instances`） | 只做 `.disabled` 后缀重命名；**宿主不提供"删除内容"，插件也没有**（见下方内容启停约定） |
| 查询 | `getMemorySnapshot` / `getSystemUsage` / `getDiskUsage` | `system-status` | 只读：内存、CPU、磁盘占用（内置「内存 / 性能 / 磁盘」小组件同源） |
| 查询 | `getCurrentTrack` / `getMusicTracks` | `music` | 只读音乐库（无播放控制；播放控制等有真实场景再议） |
| 导航 | `navigateToPage(pageId, detail?)` | — | 切到内置页或其它插件页；页面不存在抛错 |
| 事件 | `onGameExit` | — | 一次启动的终态：`failed` / `exited` + 退出码 + 是否崩溃 + 是否手动停止 |
| 查询 | `getServerStatus` | `server-status` | 连接失败抛错由插件接住 |
| 写入 | `saveLauncherSettings`（全局启动设置） | `launcher-config-write`（兼容旧 `launcher-config`） | 应基于 get 的返回值原样修改后写回 |
| 启动 | `launchSelected` / `launchVersion` | `launch` | 与手点同管线；`launchVersion` 校验版本存在且**不改变**用户当前选中。首次调用弹确认框，拒绝即抛错 |
| 事件 | `onLaunchPhaseChange` / `onInstancesChanged` / `onDownloadTasksChanged` | — | 返回取消订阅函数；卸载/重载时宿主自动清理。`onDownloadTasksChanged` 回调收到的是重新读来的完整下载任务列表（订阅时不会立刻回调一次，需权限 `downloads`） |
| 事件 | `onLogLine` | `logs` | 启动日志新增行：成批投递（见下方日志事件约定）、窗口化 + 背压，窗口隐藏时暂停轮询。日志含路径 / 账号名 / 服务器地址，属危险读 |
| 设置 | `config.get / set / clear` | `storage` | 键前缀隔离；仅字符串值；种子来自清单 `settings`；**卸载插件时会一并删除其全部配置键** |
| 样式 | `styles.inject(css, key?)` / `styles.remove(key)` | `styles` | 注入全局 CSS（可改任意控件样式，含圆角变量 `--nya-radius-*`，见 CSS_STYLE_TABLE）；同 key 重复注入为替换；卸载/重载/停用时宿主自动移除该插件全部样式。静态样式文件直接用清单 `styles` 字段（宿主监听文件变化，保存即热生效），无需写代码。宿主会对注入内容做约束：单条上限 256 KB、`@import` 语句一律移除（`<style>` 里的 `@import` 只有指向外网的才有意义，宿主不允许插件借此发外部请求） |
| 通信 | `ipc.send(target, type, payload?)` / `ipc.broadcast(type, payload?)` / `ipc.onMessage(handler)` / `ipc.plugins()` | `ipc` | 插件间消息总线（见下方插件间通信约定）：发送方身份宿主注入；负载 JSON 校验 + 256 KB 上限 + 每接收者深拷贝；5 秒 100 条频控；接收方异常隔离；只有活跃且权限仍授权的插件能收到；返回送达的处理器数，权限被关闭返回 null |

**下载任务约定**：`getDownloadTasks()` 返回宿主此刻记得的全部下载任务（与右下角下载中心同源）——游戏本体固定一条（id `"game"`，空闲或本次运行还没下载过时不出现），内容资源 / 整合包 / Java 运行时各一条；终态任务按宿主策略只保留最近几条，**不要当作下载历史**。

- 每条带 `kind`（`game` / `content` / `modpack` / `java`）、归一后的 `phase`（`downloading` / `completed` / `failed` / `cancelled`）与三个便捷布尔值 `isActive` / `isFinished` / `isCompleted`；「是否全部完成」用 `tasks.every((task) => task.isCompleted)`；
- 宿主内部游戏本体是单条状态机快照、内容类下载是任务注册表（两套 Phase 取值不同），这里已折成同一形状，插件不必分辨来源；
- 进度字段：`percent`（0~100，总大小未知时为 0）、`indeterminate`（进度无法估算）、`downloadedBytes` / `totalBytes` / `bytesPerSecond` / `etaSeconds`（`null` = 无法估算）；失败任务的 `detail` 即失败原因；
- 只读：**不能**经它暂停 / 取消 / 重试任务（那是用户在下载中心的活）。要跟着进度刷新用 `onDownloadTasksChanged`（回调收到重新读来的完整列表，宿主已把密集事件折成最多一轮读取在途 + 一次补跑）；它**订阅时不会立刻回调一次**，所以先 `getDownloadTasks()` 取初值，再订阅。卸载 / 重载时宿主自动退订，也可以自己调用返回的取消函数。
- 读侧要权限 `downloads`，写侧（发起下载）要 `downloads-write`——**只声明 `downloads` 不会拿到写权限**（下载域不做读→写兼容，理由见 §5）。

**插件间通信约定**：插件之间通过宿主的消息总线对话（点对点 `send` / 广播 `broadcast` / 订阅 `onMessage`，发现用 `plugins`），权限 `ipc` 收发都要声明——只发不收或只收不发都各自声明即可，不做方向拆分。

- **发送方身份由宿主注入**：`from` 里的 `id` / `name` / `version` 来自宿主解析过的清单，接收方可直接信任，插件伪造不了；`to` 为 `null` 表示广播；
- **负载必须 JSON 可序列化且序列化后 ≤ 256 KB**（函数 / 循环引用 / 超限直接抛错）；`type` 是 1~64 字符的路由键，只当路由用、不承载内容。每个接收者拿到的是**独立深拷贝**——互改对象不会串；
- **接收方异常隔离**：某个插件的消息处理器抛错只记一条警告，不影响其它订阅者收到同一条消息，也不影响发送方；发送方另有频控：同一插件 5 秒内最多 100 条，超出丢弃并记日志（广播是共享通道，失控插件不该刷爆别人）；
- **投递资格**：只有"当前活跃且 `ipc` 权限仍被授权"的插件能收到——停用 / 被用户收回开关的插件自动收不到；订阅在卸载 / 重载 / 停用时由宿主自动退订，插件也可以自己调返回的取消函数；
- `send` / `broadcast` 返回**送达的处理器数**（`0` = 目标没在监听或收不到，不是错误）；权限被用户关闭时返回 `null`（与其它受权限成员一致）；
- 语义建议：`type` 用"域名式"前缀避免撞名（如 `my-plugin:settings-changed`）；回信用 `send(message.from.id, ...)`。需要请求-响应语义时自行用两条消息 + 自增序号拼，宿主不代管会话。

**发起下载约定**：常见的"一键装某某"是四步，宿主不替你做选择，但把每一步都开出来了：

```js
const versions = await api.getVersions();                  // 1. 版本清单（downloads）
const target = versions.find((item) => item.id === "1.21.1");
const loaders = await api.getModLoaderVersions("fabric", target.id); // 2. Loader 版本
await api.startModLoaderDownload({                          // 3. 发起安装（downloads-write）
  version: target,
  loader: loaders.at(-1),
  instanceName: "1.21.1-Fabric",
});
await api.downloadResource({                                // 或：往已有实例里装一个 mod
  source: "modrinth", projectId: "AANobbMI", versionId: "...",
  contentDirectory: instance.MinecraftDirectory,
});
```

- 版本对象 / Loader 对象请**原样**传给发起接口：清单 URL、哈希、依赖关系都在里面，自己拼字符串等于绕开宿主的校验与镜像策略；
- `downloadResource` 的 `subDirectory` 被限制在 `contentDirectory` 内（写 `..\..` 之类会直接报错），文件名由宿主按资源平台返回值收敛——插件不需要也不该自己拼落盘路径；
- 后端拒绝（已有下载在跑、版本非法）时统一**抛错**——不要按"调用成功"往下走；
- 进度与终态一律从 `getDownloadTasks()` / `onDownloadTasksChanged` 看，发起接口不返回进度。

**内容启停约定**：`setContentEnabled(entryPath, enabled)` 只做 `.disabled` 后缀重命名（`entryPath` 取 `getVersionDetails()` 内容列表的 `SourcePath`）。**宿主没有"删除内容"能力，插件也没有**：启动器界面同样只提供启用 / 禁用，mod 文件永远由用户自己处置——插件拿到的手动能力不该超过启动器自己的界面。

**页面按钮约定**：

```js
api.registerPageAction({
  pageId: "instances",              // 目前 instances / download 有插槽
  id: "health-check",               // 注册为 "<插件id>:health-check"
  label: "体检",
  icon: api.h(api.icons.Warning20Regular),
  onPress: async () => { await api.getVersionDetails(selected); },
});
```

- 只往**别人画好的页面**里加一个按钮，不做整页占位（那是 `registerPage`）；没有插件注册时插槽**不占任何空间**，不必担心"凭空多一条空行"；
- `onPress` 返回 Promise 时按钮进入忙碌态（禁用 + loading）；抛错由宿主接住并弹一条错误提示，不会弄坏宿主页面；
- 目标 pageId 未注册时只记一条控制台警告、不显示（插件之间的加载顺序无法保证）；插件卸载 / 重载 / 停用时按钮自动摘除。

**日志与退出事件约定**：

- `onLogLine(handler, { tailLines })` 回调收到的是**一批**新增行（`{ lines, droppedLines, totalLines, rotated }`），不是逐行回调：宿主按 1 秒窗口轮询 `GetLogText` 后合并，单批最多 200 行 / 256 KB，超出只保留末尾窗口并如实报告 `droppedLines`；
- 只推送**已完整**的行（最后一个换行之后没写完的残片先攒着）；日志被清空或轮转时 `rotated` 为 true、`totalLines` 重新计数，插件应据此清空自己的面板；
- `tailLines` 上限 2000：默认 0 = 只收订阅之后的新行（避免一订阅就灌进整份历史）；窗口隐藏 / 最小化时轮询自动暂停，回到前台补一拍；
- 所有插件共享同一条轮询（订阅计数归零即停），所以"多订阅几个"不会线性增加 IO；但**别在回调里做重活**，一批最多 200 行是给渲染留的余量；
- `onGameExit(handler)` 只在一次启动的**终态**触发：`phase: "failed"`（启动阶段就失败，没跑起来）或 `"exited"`（进程退出），带 `exitCode` / `crashed` / `stoppedManually` / `message`。崩溃判定与启动器自己的崩溃弹窗同源（非零退出码且非手动停止）；同一个快照 Revision 只回调一次。要"崩了自动做点什么"就用它，别自己去监听 `launch:changed` 猜。

`WidgetRenderContext`（小组件注入）自 v1 封版：只增不改名，改名/删除即升主版本。

## 7. 明确不做的

运行时编译进生产路径、任意路径文件系统访问、网络代理封装、包签名——每一项都等第一个真实场景来拽再议。跨插件通信原在此列，现以 `ipc` 权限 + 宿主消息总线的形态落地（见 §6 插件间通信约定）；**通用事件总线仍不做**——插件与宿主之间的事件走既有 `on*` 订阅，不再开第二个泛型通道，插件与插件之间的请求-响应语义也由双方用消息自行拼装，宿主不代管会话。

## 8. 给未来改 API 的人

加一个 API 前先过三问：有真实场景拽着吗？内置实现自己会用到吗？插件是否反正能绕过 API 摸到（能绕 = 更该收编进 API 统一管）？三问皆"是"才加；删除或改名 = 主版本号 +1。
