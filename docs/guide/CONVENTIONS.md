# 使用约定

API 全表见 [API_REFERENCE.md](API_REFERENCE.md)；这里是与特定 API 组配套的行为
约定，写成一篇方便按场景查。

## 小组件卡片壳约定

主页组件列对指针事件做了统一管理，`registerWidget` 注册的小组件会被宿主**自动
装入标准卡片壳**（主题描边、圆角内边距、事件恢复）——因此：

- 组件内容**不要自带卡片背景/外壳**（不要再套一层 Card），直接输出内容行即可；
- 要与内置组件一致的「图标 + 标题 + 大数值」头部，用 `api.HomeCard`；
- 长按 1 秒拖动排序、错误边界与内置组件完全相同，无需插件处理。

## 启动卡覆盖约定

`api.registerLaunchCard({ render })` 可以整体替换主页右侧启动卡的内容：

- render 收到 `LaunchCardContext`——与内置卡完全相同的数据与回调（版本列表 /
  选中版本 / 账号列表 / 启动停止 / 刷新 / 打开目录），宿主在外层提供面板容器、
  滑动切换与拖动删除区，**不要再自带整块面板背景**；
- 独占槽：同时只有一张覆盖卡生效（最后注册者赢）；插件被停用 / 卸载 / 重载失败后
  宿主自动摘除覆盖并回落内置卡片，无需插件清理；
- 覆盖卡出错只坏这一张卡（宿主套错误边界），不影响主页其余部分；
- 只想微调内置卡样式而不是换整张卡的，优先用清单 `styles` / `styles.inject` 注入
  CSS，内置卡根节点带 `data-nya="launch-card"` 选择锚点。

## 下载任务约定

`getDownloadTasks()` 返回宿主此刻记得的全部下载任务（与右下角下载中心同源）——
游戏本体固定一条（id `"game"`，空闲或本次运行还没下载过时不出现），内容资源 /
整合包 / Java 运行时各一条；终态任务按宿主策略只保留最近几条，**不要当作下载历史**。

- 每条带 `kind`（`game` / `content` / `modpack` / `java`）、归一后的 `phase`
  （`downloading` / `completed` / `failed` / `cancelled`）与三个便捷布尔值
  `isActive` / `isFinished` / `isCompleted`；「是否全部完成」用
  `tasks.every((task) => task.isCompleted)`；
- 宿主内部游戏本体是单条状态机快照、内容类下载是任务注册表（两套 Phase 取值
  不同），这里已折成同一形状，插件不必分辨来源；
- 进度字段：`percent`（0~100，总大小未知时为 0）、`indeterminate`（进度无法估算）、
  `downloadedBytes` / `totalBytes` / `bytesPerSecond` / `etaSeconds`（`null` = 无法
  估算）；失败任务的 `detail` 即失败原因；
- 只读：**不能**经它暂停 / 取消 / 重试任务（那是用户在下载中心的活）。要跟着进度
  刷新用 `onDownloadTasksChanged`（回调收到重新读来的完整列表，宿主已把密集事件
  折成最多一轮读取在途 + 一次补跑）；它**订阅时不会立刻回调一次**，所以先
  `getDownloadTasks()` 取初值，再订阅。卸载 / 重载时宿主自动退订，也可以自己调用
  返回的取消函数。
- 读侧要权限 `downloads`，写侧（发起下载）要 `downloads-write`——**只声明
  `downloads` 不会拿到写权限**（下载域不做读→写兼容，理由见
  [PERMISSIONS.md](PERMISSIONS.md)）。

## 发起下载约定

常见的"一键装某某"是四步，宿主不替你做选择，但把每一步都开出来了：

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

- 版本对象 / Loader 对象请**原样**传给发起接口：清单 URL、哈希、依赖关系都在里面，
  自己拼字符串等于绕开宿主的校验与镜像策略；
- `downloadResource` 的 `subDirectory` 被限制在 `contentDirectory` 内（写 `..\..`
  之类会直接报错），文件名由宿主按资源平台返回值收敛——插件不需要也不该自己拼
  落盘路径；
- 后端拒绝（已有下载在跑、版本非法）时统一**抛错**——不要按"调用成功"往下走；
- 进度与终态一律从 `getDownloadTasks()` / `onDownloadTasksChanged` 看，发起接口
  不返回进度。

## 内容启停约定

`setContentEnabled(entryPath, enabled)` 只做 `.disabled` 后缀重命名（`entryPath`
取 `getVersionDetails()` 内容列表的 `SourcePath`）。**宿主没有"删除内容"能力，
插件也没有**：启动器界面同样只提供启用 / 禁用，mod 文件永远由用户自己处置——插件
拿到的手动能力不该超过启动器自己的界面。

## 页面按钮约定

```js
api.registerPageAction({
  pageId: "instances",              // 目前 instances / download 有插槽
  id: "health-check",               // 注册为 "<插件id>:health-check"
  label: "体检",
  icon: api.h(api.icons.Warning20Regular),
  onPress: async () => { await api.getVersionDetails(selected); },
});
```

- 只往**别人画好的页面**里加一个按钮，不做整页占位（那是 `registerPage`）；没有
  插件注册时插槽**不占任何空间**，不必担心"凭空多一条空行"；
- `onPress` 返回 Promise 时按钮进入忙碌态（禁用 + loading）；抛错由宿主接住并弹
  一条错误提示，不会弄坏宿主页面；
- 目标 pageId 未注册时只记一条控制台警告、不显示（插件之间的加载顺序无法保证）；
  插件卸载 / 重载 / 停用时按钮自动摘除。

## 插件间通信约定

插件之间通过宿主的消息总线对话（点对点 `send` / 广播 `broadcast` / 订阅
`onMessage`，发现用 `plugins`），权限 `ipc` 收发都要声明——只发不收或只收不发都
各自声明即可，不做方向拆分。

- **发送方身份由宿主注入**：`from` 里的 `id` / `name` / `version` 来自宿主解析过的
  清单，接收方可直接信任，插件伪造不了；`to` 为 `null` 表示广播；
- **负载必须 JSON 可序列化且序列化后 ≤ 256 KB**（函数 / 循环引用 / 超限直接抛错）；
  `type` 是 1~64 字符的路由键，只当路由用、不承载内容。每个接收者拿到的是**独立
  深拷贝**——互改对象不会串；
- **接收方异常隔离**：某个插件的消息处理器抛错只记一条警告，不影响其它订阅者收到
  同一条消息，也不影响发送方；发送方另有频控：同一插件 5 秒内最多 100 条，超出
  丢弃并记日志（广播是共享通道，失控插件不该刷爆别人）；
- **投递资格**：只有"当前活跃且 `ipc` 权限仍被授权"的插件能收到——停用 / 被用户
  收回开关的插件自动收不到；订阅在卸载 / 重载 / 停用时由宿主自动退订，插件也可以
  自己调返回的取消函数；
- `send` / `broadcast` 返回**送达的处理器数**（`0` = 目标没在监听或收不到，不是
  错误）；权限被用户关闭时返回 `null`（与其它受权限成员一致）；
- 语义建议：`type` 用"域名式"前缀避免撞名（如 `my-plugin:settings-changed`）；
  回信用 `send(message.from.id, ...)`。需要请求-响应语义时自行用两条消息 + 自增
  序号拼，宿主不代管会话。

## 日志与退出事件约定

- `onLogLine(handler, { tailLines })` 回调收到的是**一批**新增行（`{ lines,
  droppedLines, totalLines, rotated }`），不是逐行回调：宿主按 1 秒窗口轮询
  `GetLogText` 后合并，单批最多 200 行 / 256 KB，超出只保留末尾窗口并如实报告
  `droppedLines`；
- 只推送**已完整**的行（最后一个换行之后没写完的残片先攒着）；日志被清空或轮转时
  `rotated` 为 true、`totalLines` 重新计数，插件应据此清空自己的面板；
- `tailLines` 上限 2000：默认 0 = 只收订阅之后的新行（避免一订阅就灌进整份历史）；
  窗口隐藏 / 最小化时轮询自动暂停，回到前台补一拍；
- 所有插件共享同一条轮询（订阅计数归零即停），所以"多订阅几个"不会线性增加 IO；
  但**别在回调里做重活**，一批最多 200 行是给渲染留的余量；
- `onGameExit(handler)` 只在一次启动的**终态**触发：`phase: "failed"`（启动阶段就
  失败，没跑起来）或 `"exited"`（进程退出），带 `exitCode` / `crashed` /
  `stoppedManually` / `message`。崩溃判定与启动器自己的崩溃弹窗同源（非零退出码
  且非手动停止）；同一个快照 Revision 只回调一次。要"崩了自动做点什么"就用它，
  别自己去监听 `launch:changed` 猜。
