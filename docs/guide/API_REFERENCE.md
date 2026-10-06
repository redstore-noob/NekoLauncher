# 运行时 API（v1 全表）

`activate(api)` 收到的注入对象全表。权限列的含义与三层闸见
[PERMISSIONS.md](PERMISSIONS.md)；各 API 的使用约定（下载任务、IPC、日志等）单独
拆在 [CONVENTIONS.md](CONVENTIONS.md)。

| 组 | 成员 | 权限 | 说明 |
|---|---|---|---|
| 元信息 | `apiVersion` / `plugin{id,name,version}` | — | 只读 |
| 建材 | `react` / `h` / `Fragment` / `ui`(白名单) / `icons`(白名单) / `HomeCard` | — | 宿主注入，插件不得自带 React |
| 扩展点 | `registerWidget` / `registerPage` / `registerLaunchCard` | — | 前两者 id 自动加 `<插件id>:` 前缀；页面 render 收 `PageRenderContext`（随启动状态自动重渲染）；`registerLaunchCard` 是独占槽（见 CONVENTIONS 启动卡覆盖约定） |
| 扩展点 | `registerPageAction` | — | 往宿主**已有页面**里加一个按钮（目前 `instances` / `download` 渲染插槽），不是整页占位（见 CONVENTIONS 页面按钮约定） |
| 生命周期 | `onCleanup` | — | 注册清理函数，卸载/重载时宿主依次调用 |
| 反馈 | `log` / `t`(占位插值) / `confirm` | — | 宿主统一样式 |
| 反馈 | `notify`(4 级) | `notifications` | 另有频控：5s 内最多 3 条 |
| 系统 | `setClipboard` | `clipboard` | 只写不读 |
| 系统 | `openUrl` | `open-url` | 仅 http(s) |
| 系统 | `openPath` | `open-path` | 仅插件目录内 |
| 查询 | `getInstances` / `getSaves` / `getVersionProfile` / `getVersionDetails` / `getScreenshots` | `instances` | 只读；`getVersionDetails` 含加载器信息与全部内容列表。`getSaves` 的路径被限制在已知游戏根目录内（见 PERMISSIONS 危险读收口） |
| 写入 | `selectInstance`（切换全局选中）/ `saveVersionProfile`（实例启动档案） | `instances-write`（兼容旧 `instances`） | 应基于 get 的返回值原样修改后写回；`saveVersionProfile` 会先弹确认框（档案里的 Java 路径 / 包装命令等同"下次启动执行什么"） |
| 查询 | `getAccounts` | `accounts` | 只读摘要（含头像），**凭据不出宿主** |
| 查询 | `getLaunchState` | — | 只读启动状态 |
| 查询 | `getDownloadTasks` | `downloads` | 只读：全部下载任务的完成状态（游戏本体 / 内容资源 / 整合包 / Java 同源，见 CONVENTIONS 下载任务约定） |
| 查询 | `getVersions` / `getModLoaderVersions` / `getDownloadSources` / `getJavaRuntimes` | `downloads` | 只读：版本清单、Loader 可用版本、下载源与托管 Java 运行时——发起下载的前置数据 |
| 写入 | `startDownload` / `startModLoaderDownload` / `downloadResource` / `installModpack` | `downloads-write` | 与下载页 / 资源页同一条管线：进度进右下角下载中心、可暂停取消；后端拒绝（已有下载在跑等）时**抛错**，不返回假成功。声明 `downloads` **不会**解锁这些（见 PERMISSIONS） |
| 写入 | `setContentEnabled`（Mod 等启停） | `instances-write`（兼容旧 `instances`） | 只做 `.disabled` 后缀重命名；**宿主不提供"删除内容"，插件也没有**（见 CONVENTIONS 内容启停约定） |
| 查询 | `getMemorySnapshot` / `getSystemUsage` / `getDiskUsage` | `system-status` | 只读：内存、CPU、磁盘占用（内置「内存 / 性能 / 磁盘」小组件同源） |
| 查询 | `getCurrentTrack` / `getMusicTracks` | `music` | 只读音乐库（无播放控制；播放控制等有真实场景再议） |
| 导航 | `navigateToPage(pageId, detail?)` | — | 切到内置页或其它插件页；页面不存在抛错 |
| 事件 | `onGameExit` | — | 一次启动的终态：`failed` / `exited` + 退出码 + 是否崩溃 + 是否手动停止 |
| 查询 | `getServerStatus` | `server-status` | 连接失败抛错由插件接住 |
| 写入 | `saveLauncherSettings`（全局启动设置） | `launcher-config-write`（兼容旧 `launcher-config`） | 应基于 get 的返回值原样修改后写回 |
| 启动 | `launchSelected` / `launchVersion` | `launch` | 与手点同管线；`launchVersion` 校验版本存在且**不改变**用户当前选中。首次调用弹确认框，拒绝即抛错 |
| 事件 | `onLaunchPhaseChange` / `onInstancesChanged` / `onDownloadTasksChanged` | — | 返回取消订阅函数；卸载/重载时宿主自动清理。`onDownloadTasksChanged` 回调收到的是重新读来的完整下载任务列表（订阅时不会立刻回调一次，需权限 `downloads`） |
| 事件 | `onLogLine` | `logs` | 启动日志新增行：成批投递（见 CONVENTIONS 日志事件约定）、窗口化 + 背压，窗口隐藏时暂停轮询。日志含路径 / 账号名 / 服务器地址，属危险读 |
| 设置 | `config.get / set / clear` | `storage` | 键前缀隔离；仅字符串值；种子来自清单 `settings`；**卸载插件时会一并删除其全部配置键** |
| 样式 | `styles.inject(css, key?)` / `styles.remove(key)` | `styles` | 注入全局 CSS（可改任意控件样式，含圆角变量 `--nya-radius-*`，见 [CSS_STYLE_TABLE.md](CSS_STYLE_TABLE.md)）；同 key 重复注入为替换；卸载/重载/停用时宿主自动移除该插件全部样式。静态样式文件直接用清单 `styles` 字段（宿主监听文件变化，保存即热生效），无需写代码。宿主会对注入内容做约束：单条上限 256 KB、`@import` 语句一律移除（`<style>` 里的 `@import` 只有指向外网的才有意义，宿主不允许插件借此发外部请求） |
| 样式 | `registerUiTheme({ id?, name, apply })` / `unregisterUiTheme(id)` | `styles` | 注册**界面主题**：出现在外观设置的「界面主题」列表（见 [UI_THEMES.md](UI_THEMES.md)）；id 自动加 `<插件id>:` 前缀，返回完整 id。样式本体走清单 `styles` / `styles.inject`，`apply` 只操作 `<html>` 的 data-* 属性门控（抛错被宿主隔离）；卸载/重载/停用时宿主自动摘除该插件全部主题，若被摘的是用户当前选中项则整体回落默认主题（配置键保留，插件回来还能恢复）；外观列表随注册/摘除实时增删 |
| 通信 | `ipc.send(target, type, payload?)` / `ipc.broadcast(type, payload?)` / `ipc.onMessage(handler)` / `ipc.plugins()` | `ipc` | 插件间消息总线（见 CONVENTIONS 插件间通信约定）：发送方身份宿主注入；负载 JSON 校验 + 256 KB 上限 + 每接收者深拷贝；5 秒 100 条频控；接收方异常隔离；只有活跃且权限仍授权的插件能收到；返回送达的处理器数，权限被关闭返回 null |

`WidgetRenderContext`（小组件注入）自 v1 封版：只增不改名，改名/删除即升主版本。
