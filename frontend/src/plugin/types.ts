/*
 * 插件与注册表共用的类型定义。
 *
 * 扩展点目前有三个：主页小组件（WidgetDefinition）、页面（PageDefinition）
 * 与启动卡覆盖（LaunchCardDefinition）。前两者都按 id 寻址、由宿主注入数据，
 * 内置实现与第三方插件走同一套注册通道；启动卡是独占槽，注册即替换主页右侧
 * 启动面板的内容，卸载后自动回落内置卡片。
 */
import type React from "react";
import type {
  config,
  content,
  download,
  instance,
  launch,
  models,
  monitoring,
  music,
  network,
} from "../../wailsjs/go/models";

/** 账号摘要（小组件皮肤展示与插件 getAccounts 共用）；未添加账号时列表为空 */
export interface SelectedAccountSummary {
  /** 账号稳定键（后端寻址用） */
  key: string;
  /** 显示名 */
  name: string;
  /** 账号类型：microsoft / offline / authlib */
  type: string;
  /** 皮肤头像（data URI），加载失败为空串（空时显示首字母占位） */
  avatar?: string;
}

/** 主页小组件渲染上下文：由主页注入，组件本身不感知布局 */
export interface WidgetRenderContext {
  playtimeRecords: config.PlaytimeRecord[];
  /** 游戏进程运行中 */
  isGameRunning: boolean;
  /** 准备中或运行中：小组件禁止再次发起启动 */
  isBusy: boolean;
  /** 启动阶段：0 空闲 / 1 准备中 / 2 运行中 / 3 失败 / 4 已退出 */
  launchPhase: number;
  /** 启动快照版本号，变化时重新拉取日志 */
  launchRevision: number;
  selectedVersion: string;
  onSelectVersion: (versionId: string) => void;
  /** 切换到指定实例并启动到指定存档（worldName 为空表示只启动到主菜单） */
  onLaunchWorld: (
    versionId: string,
    worldName: string,
  ) => Promise<string | null>;
  onJoin: (host: string, port: number) => Promise<string | null>;
  /** 当前选中账号（主页账户列表的选中项），皮肤展示组件使用 */
  selectedAccount: SelectedAccountSummary | null;
  /** 全部账号（切换账号组件使用） */
  accounts: SelectedAccountSummary[];
  /** 切换选中账号（同步后端存储，启动游戏时生效） */
  onSelectAccount: (stableKey: string) => void;
}

/** 主页小组件：元信息 + 渲染函数 */
export interface WidgetDefinition {
  id: string;
  /** 组件库与拖动幽灵条上显示的名字 */
  title: string;
  /** 组件库里的一句话说明 */
  description: string;
  /** 组件库条目与幽灵条的图标 */
  icon: React.ReactNode;
  /** 图标磁贴渐变（与卡片内保持一致） */
  tileClass: string;
  render: (context: WidgetRenderContext) => React.ReactNode;
}

/** 启动状态摘要（只读）：页面上下文与 getLaunchState 共用 */
export interface LaunchStateSummary {
  /** 启动阶段：0 空闲 / 1 准备中 / 2 运行中 / 3 失败 / 4 已退出 */
  launchPhase: number;
  /** 准备中或运行中 */
  isBusy: boolean;
  /** 游戏进程运行中 */
  isGameRunning: boolean;
}

/** 下载任务阶段（游戏本体与内容任务的取值已归一，见 download-tasks.ts） */
export type DownloadTaskPhase =
  | "downloading"
  | "completed"
  | "failed"
  | "cancelled";

/** 下载任务类别：game 游戏本体 / content 单文件资源 / modpack 整合包 / java 运行时 */
export type DownloadTaskKind = "game" | "content" | "modpack" | "java";

/**
 * 下载任务只读摘要（getDownloadTasks）。
 *
 * 宿主内部维持两套互不相干的下载状态——游戏本体安装是单条状态机快照，内容资源
 * 与 Java 运行时是任务注册表；这里已经折成同一形状：右下角下载中心看到几个任务，
 * 插件就拿到几条（终态任务同样只保留最近几条）。
 */
export interface DownloadTaskSummary {
  /** 稳定 id：游戏本体固定 "game"，其余为宿主任务 id（如 "ct-3"） */
  id: string;
  /** 展示名（版本 id / 文件名 / 整合包名 / "Azul Java 21" 等） */
  name: string;
  kind: DownloadTaskKind;
  /** 归一后的阶段 */
  phase: DownloadTaskPhase;
  /** 进行中（准备 / 下载 / 解压阶段都算） */
  isActive: boolean;
  /** 已结束（完成 / 失败 / 取消） */
  isFinished: boolean;
  /** 已成功完成 */
  isCompleted: boolean;
  /** 0~100；总大小未知时为 0（此时看 indeterminate） */
  percent: number;
  /** 进度无法估算（总大小未知） */
  indeterminate: boolean;
  downloadedBytes: number;
  /** 未知总大小时为 0 */
  totalBytes: number;
  bytesPerSecond: number;
  /** 预计剩余秒数；null = 无法估算 */
  etaSeconds: number | null;
  /** 阶段描述（如"下载依赖库"）；失败 / 取消时为对应说明 */
  detail: string;
}

/**
 * 启动卡渲染上下文：主页装配并注入，覆盖卡与内置卡消费同一份数据与回调。
 * 插件经 registerLaunchCard 提供的 render 收到的就是这个对象——它拿到的是
 * 与内置卡完全相同的启动能力（选版本 / 选账号 / 启动 / 停止 / 刷新），
 * 不需要也无法越过宿主去碰后端凭据。
 */
export interface LaunchCardContext {
  /** 已安装版本 id 列表 */
  versions: string[];
  /** 版本列表读取中 */
  isLoading: boolean;
  /** 版本扫描错误文本（空 = 正常） */
  loadError: string;
  /** 当前 .minecraft 根目录（可能为空） */
  minecraftDirectory: string;
  selectedVersion: string;
  onSelectVersion: (versionId: string) => void;
  /** 全部账号（展示摘要，凭据永远不出宿主） */
  accounts: SelectedAccountSummary[];
  /** 当前选中账号的稳定键（空 = 未选） */
  selectedAccountKey: string;
  /** 切换选中账号（同步后端存储，启动时生效） */
  onSelectAccount: (stableKey: string) => void;
  /** 启动阶段：0 空闲 / 1 准备中 / 2 运行中 / 3 失败 / 4 已退出 */
  launchPhase: number;
  /** 准备中或运行中：应禁止再次发起启动 */
  isBusy: boolean;
  /** 游戏进程运行中 */
  isGameRunning: boolean;
  /** 启动管线的实时状态文本（准备中 / 运行中；空闲时为空串） */
  statusText: string;
  /** 最近一次启动失败原因（空 = 无失败） */
  launchError: string;
  /** 发起启动：返回失败原因文本，null = 已发起。会先切到选中实例 */
  onLaunch: () => Promise<string | null>;
  /** 停止运行中的游戏 */
  onStop: () => void;
  /** 重新扫描版本列表 */
  onRefreshVersions: () => void;
  /** 用系统文件管理器打开 .minecraft 目录（排障入口） */
  onOpenDirectory: () => void;
}

/**
 * Mod Loader 类型（插件侧用可读字符串；宿主内部是数字枚举，api.ts 负责转换）。
 * `vanilla` 不在这里：原版下载走 startDownload。
 */
export type ModLoaderKind =
  | "fabric"
  | "quilt"
  | "neoforge"
  | "forge"
  | "optifine";

/** 启动卡覆盖定义：插件注册的自定义启动卡（独占槽，最后注册者生效） */
export interface LaunchCardDefinition {
  render: (context: LaunchCardContext) => React.ReactNode;
}

/**
 * 一次启动的终态（onGameExit 回调载荷）。
 *
 * 游戏进程退出（exited）与"还没跑起来就失败"（failed）都会触发；两者语义不同，
 * 用 phase 区分。crashed 只在 exited 且非用户手动停止、退出码非 0 时为 true——
 * 判定规则与启动器自己的崩溃提示完全一致（lib/crashNotice.ts）。
 */
export interface GameExitInfo {
  /** 退出的实例版本 id（快照为空时为空串） */
  versionId: string;
  /** failed = 启动阶段就失败（没跑起来）；exited = 进程已退出 */
  phase: "failed" | "exited";
  /** 进程退出码；failed 或拿不到时为 null */
  exitCode: number | null;
  /** 异常退出（非零退出码且不是用户手动停止） */
  crashed: boolean;
  /** 用户主动点了「停止游戏」 */
  stoppedManually: boolean;
  /** 快照文案：failed 时是启动失败原因，exited 时是退出说明 */
  message: string;
}

/**
 * 一批新增日志（onLogLine 回调载荷）。
 *
 * 日志不逐行回调：宿主按轮询窗口把新增行合并成一批（见 log-stream.ts），
 * 单批超过上限时只保留末尾窗口并报告丢弃量——日志爆发时插件不会被回调刷爆。
 */
export interface LogLineBatch {
  /** 本批新增的日志行（按时间顺序；超上限时只保留末尾窗口） */
  lines: string[];
  /** 本批因上限被丢弃的行数（0 = 一行没丢） */
  droppedLines: number;
  /** 采集时的日志总行数 */
  totalLines: number;
  /** 日志被清空或轮转（宿主从头重读），本批是该文件的开头 */
  rotated: boolean;
}

/** onLogLine 订阅选项 */
export interface LogLineOptions {
  /** 订阅时先补发的日志尾部行数（默认 0 = 只收订阅之后的新行；上限 2000） */
  tailLines?: number;
}

/**
 * 页面级操作按钮：挂在宿主内置页面的工具区（registerPageAction）。
 *
 * 与 registerPage 的"整页占位"相对——这里只往**别人已经画好的页面**里加一个按钮，
 * 适合"对当前页做点事"（导出、体检、批量操作）。宿主只为内置页渲染插槽，
 * pageId 指向没有插槽的页面时静默不显示。
 */
export interface PageActionDefinition {
  /** 目标页面 id：`instances`（实例页）/ `download`（下载页）已支持 */
  pageId: string;
  /** 按钮 id，自动加 "<插件id>:" 前缀，避免与其它插件撞名 */
  id: string;
  /** 按钮文案 */
  label: string;
  /** 按钮图标（建议 20px 的 Fluent 图标，如 h(icons.Warning20Regular)） */
  icon?: React.ReactNode;
  /** 悬停说明；缺省用 label */
  tooltip?: string;
  /** 点击回调；返回 Promise 时宿主会在等待期间禁用该按钮 */
  onPress: () => void | Promise<void>;
  /** 同页排序，小的在前；缺省 1000 */
  order?: number;
}

/** 侧边栏页面渲染上下文：由宿主注入，随启动状态自动重渲染 */
export interface PageRenderContext extends LaunchStateSummary {}

/** 侧边栏页面 */
export interface PageDefinition {
  id: string;
  label: string;
  icon: React.ReactNode;
  render: (context: PageRenderContext) => React.ReactNode;
  /** 侧边栏排序，小的在前；内置页 10~90，插件缺省 1000 */
  order: number;
}

/** 消息里的发送方身份：字段来自宿主解析过的清单，接收方可直接信任 */
export interface PluginMessageSender {
  id: string;
  name: string;
  version: string;
}

/**
 * 插件间消息（ipc.send / ipc.broadcast 投递、ipc.onMessage 收到的形状）。
 *
 * 负载必须 JSON 可序列化且序列化后 ≤ 256 KB；每个接收者拿到的是独立深拷贝，
 * 互相改对象不会串。发送方身份由宿主注入（from.id 即清单 id，伪造不了）。
 */
export interface PluginMessage {
  /** 发送方身份（宿主注入） */
  from: PluginMessageSender;
  /** 目标插件 id；null = 广播 */
  to: string | null;
  /** 路由键：收发双方自行约定的短字符串（1~64 字符） */
  type: string;
  /** JSON 可序列化负载；未传时为 null */
  payload: unknown;
}

/** 插件清单（plugins/<id>/plugin.yaml） */
export interface PluginManifest {
  id: string;
  name: string;
  version: string;
  /** 目标宿主 API 版本；与 PLUGIN_API_VERSION 不一致时拒绝加载 */
  apiVersion: string;
  description?: string;
  author?: string;
  /** 入口文件，相对插件目录；缺省 index.js（dev 插件缺省 index.jsx） */
  entry?: string;
  /** 开发模式：入口是 JSX 源码，宿主运行时编译后加载（Sucrase 懒加载） */
  dev?: boolean;
  /**
   * 权限声明。这不是沙箱——插件在 WebView 内全权限运行；它是安装时可见的
   * 用途清单，并决定对应 API 能否调用：未声明就调用会直接抛错。
   * 权限键见 docs/Extensions_Guide.md 的权限表：storage / launch / instances /
   * instances-write / downloads / downloads-write / accounts / launcher-config /
   * launcher-config-write / notifications / clipboard / open-url / open-path /
   * server-status / system-status / music / logs / styles / ipc。
   *
   * 另外：声明了权限也只是"能调"，**会拉起进程 / 写磁盘 / 改写启动参数的调用还会
   * 弹 NekoPrompt 让用户当场确认**（拒绝即以错误结束），撤销得掉的动作则只在左下角
   * 公示一条 NekoAlert；节流与决定记录都由宿主掌握，插件关不掉——
   * 见 docs/Extensions_Guide.md §5「高危动作：确认与公示」。
   */
  capabilities?: Record<string, boolean>;
  /**
   * 样式文件列表（相对插件目录、须为 .css）：插件加载时自动注入为全局样式，
   * 可自定义任意控件的样式；卸载/重载/停用时宿主自动移除。
   */
  styles?: string[];
  /**
   * 默认设置：首次加载时逐项种入 api.config（仅当对应键为空时写入，
   * 用户改过的值不会被覆盖）。
   */
  settings?: Record<string, string>;
}

/**
 * 交给插件的宿主 API。
 *
 * 插件不应自带 react/@heroui 等库——运行时 ESM 无法解析裸模块名，而且重复的
 * React 实例会让 hooks 直接失效。宿主把 React 与常用组件、图标通过这里注入。
 *
 * **权限有三层闸**（见 docs/Extensions_Guide.md §5）：
 *  ① `plugin.yaml` 的 `capabilities` 声明 —— 未声明就调用**直接抛错**，用户在安装页
 *     先看到用途清单；
 *  ② **插件管理页每个权限一个 Switch**（用户授权）——关掉的权限**返回 null**：不执行
 *     也不抛错，所以下面这些受权限控制的成员返回类型都带 `| null`，**请判空**；
 *     订阅类（`onDownloadTasksChanged` / `onLogLine`）被关闭时同样返回 null；
 *     默认关闭的有：`network`（联网）、`logs`（运行日志）、`accounts`（账号）、
 *     `music`（音乐库），其余声明即开启；
 *  ③ 动作闸门 —— 高危动作（启动、下载安装、写启动参数）每次调 NekoPrompt 让用户
 *     当场确认，撤销得掉的动作（切实例 / 启停内容 / 注入样式）只发一条 NekoAlert。
 *
 * 插件不应自带 react/@heroui 等库——运行时 ESM 无法解析裸模块名，而且重复的
 * React 实例会让 hooks 直接失效。宿主把 React 与常用组件、图标通过这里注入。
 */
export interface PluginApi {
  apiVersion: string;
  plugin: { id: string; name: string; version: string };
  react: typeof React;
  /** React.createElement 别名：插件以 .js 形式分发时用 h('div', props, ...) 写界面 */
  h: typeof React.createElement;
  Fragment: typeof React.Fragment;
  /** 常用 HeroUI 组件（按需挑选的白名单，不是整个包） */
  ui: Record<string, React.ComponentType<any>>;
  /** 常用 Fluent 图标（按需挑选的白名单） */
  icons: Record<string, React.ComponentType<any>>;
  /**
   * 宿主标准小组件头部行（图标磁贴 + 小标题 + 大号数值，与内置组件同款）。
   * 插件小组件的内容已被宿主自动装入标准卡片壳（含主题描边与事件恢复），
   * 不要再自带卡片背景；头部直接用这个（它只是头部行，不含卡片容器）。
   */
  HomeCard: React.ComponentType<{
    icon: React.ReactNode;
    label: string;
    value: React.ReactNode;
  }>;
  /** 注册主页小组件；id 会自动加上 "<插件id>:" 前缀，避免与内置/其它插件撞名 */
  registerWidget: (definition: WidgetDefinition) => void;
  /** 注册侧边栏页面；id 同样自动加前缀 */
  registerPage: (definition: PageDefinition) => void;
  /**
   * 往宿主已有页面里加一个操作按钮（不是整页占位）：目前 `instances`（实例页）与
   * `download`（下载页）渲染插槽，其它 pageId 静默不显示。id 同样自动加前缀；
   * 插件卸载 / 重载 / 停用时宿主自动摘除按钮。
   */
  registerPageAction: (definition: PageActionDefinition) => void;
  /**
   * 覆盖主页右侧启动卡：你的 render 会替换内置卡片的内容（外层面板容器、
   * 圆角描边与拖动删除区仍由宿主提供）。上下文含与内置卡完全相同的数据与
   * 回调（选版本 / 选账号 / 启动 / 停止 / 刷新 / 打开目录）。
   * 独占槽：多个插件注册时最后注册者生效；插件被卸载 / 停用 / 重载失败后
   * 自动回落内置卡片。只做样式微调请优先考虑 styles 字段 / styles.inject
   * 注入 CSS（宿主给内置卡根节点挂了 data-nya="launch-card" 选择锚点）。
   */
  registerLaunchCard: (definition: LaunchCardDefinition) => void;
  /**
   * 插件私有配置（落在启动器 launcher.yaml，键自动加前缀隔离）。
   * 需要权限：storage——未声明时调用直接抛错。
   */
  config: {
    get: (key: string) => Promise<string | null>;
    set: (key: string, value: string) => Promise<void>;
    clear: (key: string) => Promise<void>;
  };
  /**
   * 注入全局 CSS（作用于整个启动器，可改任意控件的样式）。同 key 重复注入
   * 为替换；卸载/重载/停用时宿主自动移除该插件注入的全部样式，无需清理。
   * 清单里静态的样式文件请用 styles 字段，这个 API 面向运行时动态样式
   * （如随插件设置切换主题）。需要权限：styles——未声明时调用直接抛错。
   */
  styles: {
    /** 注入（或按 key 替换）一条全局 CSS；key 缺省 "inline" */
    inject: (css: string, key?: string) => void;
    /** 按 inject 时的 key 移除一条样式 */
    remove: (key: string) => void;
  };
  log: (...args: unknown[]) => void;
  /**
   * 注册清理函数：插件被卸载/重载时宿主会依次调用（事件订阅类 API
   * 已自动清理，无需手动重复注册）。
   */
  onCleanup: (fn: () => void) => void;
  /** 宿主通知横条（与启动器自身提示同一样式）。需要权限：notifications */
  notify: {
    info: (message: string, duration?: number) => void;
    success: (message: string, duration?: number) => void;
    warning: (message: string, duration?: number) => void;
    error: (message: string, duration?: number) => void;
  };
  /** 用系统默认浏览器打开 http(s) 链接（其它协议会被宿主拒绝）。需要权限：open-url */
  openUrl: (url: string) => Promise<void>;
  /**
   * 用系统默认程序打开文件或目录（只允许插件自己目录内的路径）。
   * 需要权限：open-path。
   */
  openPath: (path: string) => Promise<void>;
  /** 启动状态（只读）：页面/小组件据此禁用启动按钮 */
  getLaunchState: () => Promise<LaunchStateSummary>;
  /**
   * 全部下载任务的只读状态（游戏本体 / 内容资源 / 整合包 / Java 运行时），与右下角
   * 下载中心同源。游戏本体固定 id "game"、同一时刻至多一条；失败任务的 detail 即
   * 失败原因。需要权限：downloads
   */
  getDownloadTasks: () => Promise<DownloadTaskSummary[] | null>;
  /**
   * Mojang 版本清单（按发布时间降序），配合 startDownload 用：
   * 找到目标版本对象后原样传给 startDownload。需要权限：downloads
   */
  getVersions: () => Promise<models.MinecraftVersion[] | null>;
  /**
   * 指定 Loader（fabric / quilt / neoforge / forge / optifine）在该 Minecraft 版本下
   * 的可用版本。需要权限：downloads
   */
  getModLoaderVersions: (
    loader: ModLoaderKind,
    minecraftVersion: string,
  ) => Promise<download.ModLoaderVersion[] | null>;
  /** 内置下载源清单（名称 / 描述 / 是否官方）。需要权限：downloads */
  getDownloadSources: () => Promise<download.DownloadSource[] | null>;
  /** 已安装的托管 Java 运行时（含路径与主版本）。需要权限：downloads */
  getJavaRuntimes: () => Promise<download.InstalledJavaRuntime[] | null>;
  /**
   * 下载并安装原版 Minecraft（与下载页同一条管线：进度进下载中心、可暂停取消）。
   * version 传 getVersions() 里的对象。宿主已有下载在跑或版本不合法时抛错。
   * 需要权限：downloads-write
   */
  startDownload: (version: models.MinecraftVersion) => Promise<void>;
  /**
   * 以 Mod Loader 模式下载安装（先确保原版，再叠加 Loader），并建出实例目录。
   * 需要权限：downloads-write
   */
  startModLoaderDownload: (options: {
    version: models.MinecraftVersion;
    loader: download.ModLoaderVersion;
    instanceName: string;
    /** Fabric 系默认会补装 Fabric API，置 true 跳过 */
    skipFabricApi?: boolean;
  }) => Promise<void>;
  /**
   * 下载一个资源版本并装进实例内容目录（Mod / 资源包 / 光影），Modrinth 与
   * CurseForge 都走宿主既有的源解析、镜像与限速策略。request 的字段见
   * models.ResourceDownloadRequest（source / projectId / versionId / contentDirectory 等）。
   * subDirectory 会被限制在 contentDirectory 内（写 `..\..` 之类直接报错），文件名由
   * 宿主按资源平台返回值收敛——插件不需要也不该自己拼落盘路径。
   * 需要权限：downloads-write
   */
  downloadResource: (
    request: models.ResourceDownloadRequest,
  ) => Promise<models.ResourceDownloadResult | null>;
  /**
   * 把本机整合包文件（.mrpack / CurseForge .zip）安装到内容目录。
   * 需要权限：downloads-write
   */
  installModpack: (
    mrpackPath: string,
    contentDirectory: string,
  ) => Promise<download.ModpackInstallResult | null>;
  /** 宿主机内存 / 磁盘 / CPU 占用（只读）。需要权限：system-status */
  getMemorySnapshot: () => Promise<monitoring.MemorySnapshot | null>;
  /** 宿主机整体占用（CPU / 内存，见 monitoring.SystemUsage）。需要权限：system-status */
  getSystemUsage: () => Promise<monitoring.SystemUsage | null>;
  /** 指定路径所在磁盘的占用。需要权限：system-status */
  getDiskUsage: (path: string) => Promise<monitoring.DiskUsage | null>;
  /** 当前播放曲目（未播放时是零值对象）。需要权限：music */
  getCurrentTrack: () => Promise<music.MusicTrack | null>;
  /** 音乐库曲目列表（只读扫描结果，不含播放控制）。需要权限：music */
  getMusicTracks: () => Promise<music.MusicTrack[] | null>;
  /**
   * 切到宿主的内置页面（pageId 如 `instances` / `download` / `settings`，也可以是
   * 其它插件注册的页面 id）。detail 是页面自解释的定位参数（如下载页的 "java"）。
   * 页面不存在时抛错，绝不静默跳到别处。
   */
  navigateToPage: (pageId: string, detail?: string) => void;
  /** 写入系统剪贴板文本。需要权限：clipboard */
  setClipboard: (text: string) => Promise<void>;
  /** 宿主样式的确认框：插件自身的破坏性操作前使用 */
  confirm: (message: string, options?: { title?: string }) => Promise<boolean>;
  /** 宿主多语言：占位符插值（{0}）；宿主词条里没有的文案原样返回 */
  t: (text: string, params?: Record<string, string | number>) => string;
  /**
   * 订阅启动阶段变化（启动 / 退出都会触发）。返回取消订阅函数，
   * 插件卸载/重载时宿主也会自动清理。
   */
  onLaunchPhaseChange: (
    handler: (state: LaunchStateSummary) => void,
  ) => () => void;
  /**
   * 订阅实例变化（扫描完成 / 切换选中 / 重命名等）。返回取消订阅函数，
   * 插件卸载/重载时宿主也会自动清理。
   */
  onInstancesChanged: (
    handler: (snapshot: instance.GameInstanceSnapshot) => void,
  ) => () => void;
  /**
   * 订阅全部下载任务的状态变化（任务增删、进度推进、完成 / 失败 / 取消都会触发）。
   * 收到的是重新读来的**完整列表**（与 getDownloadTasks 同源），不是逐条增量；宿主
   * 事件本身已有节流，这里再把密集事件折成最多一轮在途 + 一次补跑。订阅时不会立刻
   * 回调一次——需要当前状态请先调 getDownloadTasks()。返回取消订阅函数，插件卸载 /
   * 重载时宿主也会自动清理。
   */
  onDownloadTasksChanged: (
    handler: (tasks: DownloadTaskSummary[]) => void,
  ) => (() => void) | null;
  /**
   * 订阅"一次启动结束"（进程退出，或还没跑起来就失败）。载荷见 GameExitInfo：
   * failed / exited、退出码、是否崩溃、是否用户手动停止。同一个快照 Revision
   * 只回调一次（launch:changed 可能重复送达）。返回取消订阅函数，插件卸载 /
   * 重载时宿主也会自动清理。
   */
  onGameExit: (handler: (info: GameExitInfo) => void) => () => void;
  /**
   * 订阅启动日志的新增行（宿主按轮询窗口合并成批，见 LogLineBatch）。默认只收
   * 订阅之后的新行；传 `{ tailLines }` 可以先补发一段尾部。窗口隐藏时暂停轮询，
   * 回到前台补一拍。返回取消订阅函数，插件卸载 / 重载时宿主也会自动清理。
   * 需要权限：logs——日志里有路径、账号名、服务器地址与报错原文，属于危险读。
   */
  onLogLine: (
    handler: (batch: LogLineBatch) => void,
    options?: LogLineOptions,
  ) => (() => void) | null;
  /**
   * 实例详情：加载器信息、隔离布局与全部内容列表（Mod / 资源包 / 光影 / 存档）。
   * 需要权限：instances
   */
  getVersionDetails: (
    versionId: string,
  ) => Promise<instance.GameVersionDetails | null>;
  /** 启动器截图墙（全部实例，按修改时间降序取前 max 张）。需要权限：instances */
  getScreenshots: (max: number) => Promise<instance.ScreenshotInfo[] | null>;
  /** 查询 Minecraft 服务器在线状态（连接失败会抛错，插件自行接住）。需要权限：server-status */
  getServerStatus: (
    host: string,
    port: number,
  ) => Promise<network.MinecraftServerStatus | null>;
  /**
   * 读取存档目录列表（只读）。savesDirectory 传实例的 saves 目录：
   * 可由 getInstances() 的 GameDirectory + "/saves" 拼出。路径必须落在**已知游戏
   * 根目录**内（当前实例的 .minecraft / 游戏目录 / 来源目录，以及设置里额外扫描
   * 的游戏目录），越界按读不到处理：返回空数组并记一条 WARN 日志。
   * 需要权限：instances
   */
  getSaves: (
    savesDirectory: string,
  ) => Promise<content.GameContentEntry[] | null>;
  /** 已安装实例视图：版本 id 列表 + 当前选中（只读）。需要权限：instances */
  getInstances: () => Promise<instance.GameInstanceSnapshot | null>;
  /**
   * 账号列表（只读）。返回的是展示用摘要：凭据（令牌等）永远不出宿主，
   * 插件拿不到原始 LaunchAccount。需要权限：accounts
   */
  getAccounts: () => Promise<SelectedAccountSummary[] | null>;
  /**
   * 切换"当前选中"的实例——这是显式的全局选择变更（主页展示、用户手点
   * 启动都会跟着变；与 launchVersion 的"不落选中"相反）。版本不存在或
   * 实例扫描未就绪时抛错。需要权限：instances-write（兼容旧的 instances）
   */
  selectInstance: (versionId: string) => Promise<void>;
  /**
   * 启用 / 禁用实例内容条目（Mod 等）：宿主只做 .disabled 后缀重命名，**从不删除**
   * 用户文件。entryPath 传 getVersionDetails() 内容列表里的 SourcePath。
   * 需要权限：instances-write（兼容旧的 instances）
   */
  setContentEnabled: (entryPath: string, enabled: boolean) => Promise<void>;
  /**
   * 读取实例启动档案（独立内存 / 窗口 / JVM 参数 / 图标偏好；无则返回默认值）。
   * minecraftDirectory 传 getInstances() 的 MinecraftDirectory。需要权限：instances
   */
  getVersionProfile: (
    minecraftDirectory: string,
    versionId: string,
  ) => Promise<config.GameVersionProfile | null>;
  /**
   * 保存实例启动档案：应基于 getVersionProfile 的返回值原样修改后写回。
   * 需要权限：instances-write（兼容旧的 instances）
   */
  saveVersionProfile: (profile: config.GameVersionProfile) => Promise<void>;
  /**
   * 读取启动器全局启动设置（Java 路径、全局 JVM/游戏参数、窗口、进程优先级等）。
   * 需要权限：launcher-config
   */
  getLauncherSettings: () => Promise<config.GlobalLaunchSettings | null>;
  /** 保存启动器全局启动设置（应基于 getLauncherSettings 的返回值原样修改）。需要权限：launcher-config-write（兼容旧的 launcher-config） */
  saveLauncherSettings: (
    settings: config.GlobalLaunchSettings,
  ) => Promise<void>;
  /**
   * 启动当前选中的实例。需要权限：launch——未声明时调用直接抛错。
   * 返回的 LaunchResult.Success 为 false 时 Message 是失败原因。
   */
  launchSelected: () => Promise<launch.LaunchResult | null>;
  /**
   * 切换到指定实例版本并启动。需要权限：launch——未声明时调用直接抛错。
   * 不改变"当前选中"的实例：只在本次启动内生效，路径与用户手点启动一致。
   */
  launchVersion: (versionId: string) => Promise<launch.LaunchResult | null>;
  /**
   * 插件间通信：向其它（或全部）插件投递消息并订阅入站消息。需要权限：ipc。
   *
   * 约定：负载必须 JSON 可序列化且 ≤ 256 KB；type 是 1~64 字符的路由键；
   * 每个接收者拿到独立深拷贝；from 身份由宿主注入、伪造不了；发送方另有频控
   * （5 秒内最多 100 条，超出丢弃并记日志）。只有"活跃且 ipc 权限仍被授权"
   * 的插件能收到。订阅在卸载/重载/停用时宿主自动退订。
   */
  ipc: {
    /** 当前可通信的插件列表（已登记清单的活跃插件） */
    plugins: () => PluginMessageSender[] | null;
    /**
     * 点对点发送。返回送达的处理器数（0 = 目标没在监听或收不到），
     * 权限被用户关闭时返回 null。
     */
    send: (
      targetPluginId: string,
      type: string,
      payload?: unknown,
    ) => number | null;
    /** 广播给所有监听中的插件。返回送达数，权限被关闭时返回 null */
    broadcast: (type: string, payload?: unknown) => number | null;
    /**
     * 订阅入站消息（点对点与广播都会到达）。返回取消订阅函数，
     * 插件卸载/重载/停用时宿主自动退订；权限被用户关闭时返回 null。
     */
    onMessage: (
      handler: (message: PluginMessage) => void,
    ) => (() => void) | null;
  };
}

/** 插件模块的导出约定：默认导出（或具名 activate）一个接收 API 的函数 */
export type PluginActivate = (api: PluginApi) => void | Promise<void>;
