/*
 * 插件与注册表共用的类型定义。
 *
 * 扩展点目前有两个：主页小组件（WidgetDefinition）与页面（PageDefinition）。
 * 两者都按 id 寻址、由宿主注入数据，内置实现与第三方插件走同一套注册通道。
 */
import type React from "react";
import type {
  config,
  content,
  instance,
  launch,
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
  onLaunchVersion: (versionId: string) => Promise<string | null>;
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
   * accounts / launcher-config / notifications / clipboard / open-url /
   * open-path / server-status / styles。
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
   * 插件私有配置（落在启动器 launcher.yaml，键自动加前缀隔离）。
   * 需要权限：storage——未声明时调用直接抛错。
   */
  config: {
    get: (key: string) => Promise<string>;
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
   * 实例详情：加载器信息、隔离布局与全部内容列表（Mod / 资源包 / 光影 / 存档）。
   * 需要权限：instances
   */
  getVersionDetails: (
    versionId: string,
  ) => Promise<instance.GameVersionDetails>;
  /** 启动器截图墙（全部实例，按修改时间降序取前 max 张）。需要权限：instances */
  getScreenshots: (max: number) => Promise<instance.ScreenshotInfo[]>;
  /** 查询 Minecraft 服务器在线状态（连接失败会抛错，插件自行接住）。需要权限：server-status */
  getServerStatus: (
    host: string,
    port: number,
  ) => Promise<network.MinecraftServerStatus>;
  /**
   * 读取存档目录列表（只读）。savesDirectory 传实例的 saves 目录：
   * 可由 getInstances() 的 GameDirectory + "/saves" 拼出。需要权限：instances
   */
  getSaves: (savesDirectory: string) => Promise<content.GameContentEntry[]>;
  /** 已安装实例视图：版本 id 列表 + 当前选中（只读）。需要权限：instances */
  getInstances: () => Promise<instance.GameInstanceSnapshot>;
  /**
   * 账号列表（只读）。返回的是展示用摘要：凭据（令牌等）永远不出宿主，
   * 插件拿不到原始 LaunchAccount。需要权限：accounts
   */
  getAccounts: () => Promise<SelectedAccountSummary[]>;
  /**
   * 切换"当前选中"的实例——这是显式的全局选择变更（主页展示、用户手点
   * 启动都会跟着变；与 launchVersion 的"不落选中"相反）。版本不存在或
   * 实例扫描未就绪时抛错。需要权限：instances-write（兼容旧的 instances）
   */
  selectInstance: (versionId: string) => Promise<void>;
  /**
   * 读取实例启动档案（独立内存 / 窗口 / JVM 参数 / 图标偏好；无则返回默认值）。
   * minecraftDirectory 传 getInstances() 的 MinecraftDirectory。需要权限：instances
   */
  getVersionProfile: (
    minecraftDirectory: string,
    versionId: string,
  ) => Promise<config.GameVersionProfile>;
  /**
   * 保存实例启动档案：应基于 getVersionProfile 的返回值原样修改后写回。
   * 需要权限：instances-write（兼容旧的 instances）
   */
  saveVersionProfile: (profile: config.GameVersionProfile) => Promise<void>;
  /**
   * 读取启动器全局启动设置（Java 路径、全局 JVM/游戏参数、窗口、进程优先级等）。
   * 需要权限：launcher-config
   */
  getLauncherSettings: () => Promise<config.GlobalLaunchSettings>;
  /** 保存启动器全局启动设置（应基于 getLauncherSettings 的返回值原样修改）。需要权限：launcher-config-write（兼容旧的 launcher-config） */
  saveLauncherSettings: (
    settings: config.GlobalLaunchSettings,
  ) => Promise<void>;
  /**
   * 启动当前选中的实例。需要权限：launch——未声明时调用直接抛错。
   * 返回的 LaunchResult.Success 为 false 时 Message 是失败原因。
   */
  launchSelected: () => Promise<launch.LaunchResult>;
  /**
   * 切换到指定实例版本并启动。需要权限：launch——未声明时调用直接抛错。
   * 不改变"当前选中"的实例：只在本次启动内生效，路径与用户手点启动一致。
   */
  launchVersion: (versionId: string) => Promise<launch.LaunchResult>;
}

/** 插件模块的导出约定：默认导出（或具名 activate）一个接收 API 的函数 */
export type PluginActivate = (api: PluginApi) => void | Promise<void>;
