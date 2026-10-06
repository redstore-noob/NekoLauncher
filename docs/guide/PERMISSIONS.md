# 权限系统

声明制契约 + 用户开关 + 高危动作确认，与后端闸门互补（见
[TRUST_MODEL.md](TRUST_MODEL.md)）。入门路线见 [Extensions_Guide.md](Extensions_Guide.md)。

## 声明制：未声明 = 报错

受权限控制的 API **始终存在**，但调用时校验清单的 `capabilities`：**未声明对应
权限直接抛错**（错误信息指明缺哪个权限、如何声明），绝不静默失败。

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
| `accounts` | `getAccounts`（**默认关闭**） |
| `launcher-config` | `getLauncherSettings` |
| `launcher-config-write` | `saveLauncherSettings`（兼容旧的 `launcher-config`，但新插件请显式声明写权限） |
| `notifications` | `notify.*` |
| `clipboard` | `setClipboard` |
| `open-url` | `openUrl` |
| `open-path` | `openPath`（叠加"仅插件目录内"的宿主侧限制） |
| `server-status` | `getServerStatus`（叠加联网开关） |
| `styles` | `styles.inject` / `styles.remove`（全局 CSS 注入，可自定义任意控件样式）；`registerUiTheme` / `unregisterUiTheme`（界面主题，见 [UI_THEMES.md](UI_THEMES.md)） |
| `ipc` | 插件间通信：`ipc.send` / `ipc.broadcast` / `ipc.onMessage` / `ipc.plugins`（见 [CONVENTIONS.md](CONVENTIONS.md) 插件间通信约定） |

读 / 写权限拆分（`instances-write`、`launcher-config-write`）的动机：让用户在安装页
就能看出插件**会不会改全局状态**。只声明读权限的存量插件调用写 API 仍会放行并记
一条控制台警告，但新插件应直接声明写权限，后续版本可能移除该兼容。

`downloads-write` 是唯一的例外：**它不认 `downloads`**。拆分兼容是给"拆分之前就存在"的
插件用的，而 `downloads` 是本轮新增的权限、没有存量插件——放行只会让"只想看下载进度"
的插件静默拿到写盘与安装能力，正好违背拆权的初衷。所以下载域必须显式声明
`downloads-write`，写侧 API 一个都不会因为声明了读权限而放行。

## 权限有三层闸（声明只是第一层）

| 层 | 在哪 | 作用 | 不满足时 |
|---|---|---|---|
| ① 声明 | `plugin.yaml` 的 `capabilities` | 安装页可见的用途清单 | 调用**直接抛错**（绝不静默） |
| ② 授权 | **插件页每个权限一个 Switch** | 用户随时收回某一项能力，不必卸载插件 | 相关 API **返回 null**（不执行、不抛错），控制台记一条警告 |
| ③ 动作 | 高危动作的 NekoPrompt / 中危动作的 NekoAlert | 每次真正动手前的确认与留痕 | 拒绝即抛错，磁盘上不留改动 |

- **默认关闭**的四项：`network`（联网）、`logs`（运行日志）、`accounts`（账号）、
  `music`（音乐库）。其余权限"声明即开启"——默认关太宽会把新插件一律弄成"坏的"，
  反而逼用户闭眼全开；而需要动作级确认的写权限（`downloads-write` 等）每一次动作
  本来都要过 NekoPrompt；
- **`network` 是用户独占的横切开关**（不要求声明，但可以声明）：它管的是"宿主代
  插件发的网络请求"——版本清单、Loader 元数据、服务器状态、资源与整合包下载。
  插件**自己** `fetch()` 不受它约束（信任模型决定了拦不住），所以别把它当成防火墙；
- 授权表存在 `launcher.yaml` 的 `pluginPermissions` 键，**不是沙箱的一部分**：这套
  开关给的是"用户看得见、关得掉、误伤有救"，不是"防住恶意插件"；
- `plugin.yaml` 里的 `capabilities` 可以写 `network: true` 表达意图（安装页会显示），
  但开关本身对每个插件都可见；
- 插件必须**判空**：受权限控制的成员返回类型都带 `| null`（订阅类
  `onDownloadTasksChanged` / `onLogLine` 同样返回 `null`），`null` 的含义就是
  "用户关掉了这一项"。

无需权限（永远可用）：元信息、`react/h/Fragment/ui/icons/HomeCard`、
`registerWidget` / `registerPage` / `registerPageAction` / `registerLaunchCard`、`log`、
`t`、`confirm`、`navigateToPage`、`getLaunchState`、`onLaunchPhaseChange`、
`onInstancesChanged`、`onGameExit`、`onCleanup`。插件管理页会展示每个插件声明的
权限列表。

## 高危动作：确认与公示

权限声明只说明"插件请求了什么"，看不出它**此刻正在做什么**。所以高危动作有两道闸：

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

- **闸门在动手之前**：用户在确认框里拒绝时，动作以错误结束
  （`插件「X」的「…」操作被用户拒绝`），磁盘上不留下任何改动——插件不能把"被拒绝"
  当成成功；
- 确认框有三个按钮：**允许一次** / **本次运行内允许** / **拒绝**（默认落在拒绝上，
  回车不等于同意）。同意按（插件 + 动作）记在本次运行内，所以批处理（一次装
  20 个 mod）只问一次；
- 拒绝同样记下：同一动作在本次运行内直接失败、不再弹框（插件用对话框刷屏也没用）。
  **重新加载插件即可重新询问**；
- 并发调用共用一个确认框：插件一口气发起 5 次也只问一次；
- **公示是宿主行为，插件关不掉**：同意之后每次执行仍会在左下角弹一条 NekoAlert
  （同一插件同一动作 4 秒合并成一条，全局 3 秒最多 4 条）。权限决定"能不能调"，
  确认决定"这次允不允许"，公示决定"用户看不看得见"——三条合起来才是这套系统给的
  承诺：插件可以强，但不能悄悄改东西。

## 危险读的收口（只读，但能翻出用户机器上的东西）

| 读 | 收口方式 |
|---|---|
| `onLogLine` 运行日志 | 从免权限改为需要 `logs` 声明（安装页可见）；日志里有路径、账号名、服务器地址与报错原文 |
| `getSaves(savesDirectory)` | 路径必须落在**已知游戏根目录**内（当前实例的 .minecraft / 游戏目录 / 来源目录 + 设置里额外扫描的游戏目录）；越界返回空数组并记一条 WARN 日志，不再是"任意目录枚举器" |
| `getAccounts` | 已经是展示摘要，凭据不出宿主（无变化） |
| `getMusicTracks` / `getScreenshots` | 已分别受 `music` / `instances` 权限约束（无变化） |

确认框与公示的文案都走 i18n（`zh-CN` 原文即 key，其它语言的词条缺了就回落中文）。
