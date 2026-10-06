// 该文件已经过验证。
/*
 * 插件API。
 * 很安全，只有必要能力。
 */
import type {
  DownloadTaskSummary,
  GameExitInfo,
  LaunchCardDefinition,
  LaunchStateSummary,
  LogLineBatch,
  LogLineOptions,
  ModLoaderKind,
  PageActionDefinition,
  PageDefinition,
  PluginApi,
  PluginManifest,
  PluginMessage,
  WidgetDefinition,
} from "./types";
import type {
  config as configModels,
  download as downloadModels,
  launch as launchModels,
  models,
} from "../../wailsjs/go/models";

// createFrom 是运行时方法，需要值导入

import React from "react";
import {
  Autocomplete,
  AutocompleteItem,
  Button,
  Card,
  CardBody,
  Chip,
  Code,
  Divider,
  Input,
  Kbd,
  Link,
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
  Progress,
  Radio,
  RadioGroup,
  ScrollShadow,
  Select,
  SelectItem,
  Slider,
  Spinner,
  Switch,
  Tab,
  Tabs,
  Textarea,
  Tooltip,
} from "@heroui/react";
import {
  Add20Regular,
  ArrowClockwise20Regular,
  Box20Regular,
  CheckmarkCircle20Regular,
  Cube20Regular,
  Dismiss20Regular,
  DocumentText20Regular,
  Folder20Regular,
  Globe20Regular,
  Heart20Regular,
  Image20Regular,
  Key20Regular,
  PaintBrush20Regular,
  Play20Regular,
  Pulse20Regular,
  PuzzleCube20Regular,
  Search20Regular,
  Server20Regular,
  Settings20Regular,
  Sparkle20Regular,
  Star20Regular,
  Timer20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";

import { instance as instanceModels } from "../../wailsjs/go/models";
import {
  GetAccounts,
  GetAccountStableKey,
  GetAvatarUrl,
} from "../../wailsjs/go/bindings/AccountAPI";
import {
  ReadSaves,
  ToggleContentEntry,
} from "../../wailsjs/go/bindings/ContentAPI";
import {
  DownloadResourceVersion,
  GetAllDownloadSources,
  GetContentTasks,
  GetCurrentDownloadSnapshot,
  GetInstalledJavaRuntimes,
  GetModLoaderVersions,
  GetVersions,
  InstallModpackToInstance,
  StartDownload,
  StartModLoaderDownload,
} from "../../wailsjs/go/bindings/DownloadAPI";
import {
  GetDiskUsage,
  GetMemorySnapshot,
  GetSystemUsage,
} from "../../wailsjs/go/bindings/MonitorAPI";
import {
  GetCurrentTrack,
  GetMusicTracks,
} from "../../wailsjs/go/bindings/MusicAPI";
import {
  ClearValue,
  GetVersionProfile,
  GetValue,
  LoadGlobalLaunchSettings,
  SaveGlobalLaunchSettings,
  SaveVersionProfile,
  SetValue,
} from "../../wailsjs/go/bindings/ConfigAPI";
import {
  GetCurrentInstanceSnapshot,
  GetVersionDetails,
  SelectInstance,
} from "../../wailsjs/go/bindings/InstanceAPI";
import {
  GetLaunchSnapshot,
  Launch,
  LaunchVersion,
} from "../../wailsjs/go/bindings/LauncherAPI";
import { OpenPage } from "../../wailsjs/go/bindings/OnlineAPI";
import { OpenPluginPath } from "../../wailsjs/go/bindings/PluginAPI";
import { PingServer } from "../../wailsjs/go/bindings/ServerAPI";
import {
  ListScreenshots,
  SetClipboard,
} from "../../wailsjs/go/bindings/SystemAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { confirm as hostConfirm, notify } from "../components/overlay/dialog";
import { t } from "../i18n";
import { navigateToPage as hostNavigateToPage } from "../lib/navigation";

/**
 * 插件版 HomeCard：只有「大数值」头部行（图标与标题已移除，传入会被忽略），**不带卡片容器**——
 * 卡片壳由注册层统一提供（套两层卡就是截图里那种嵌套观感）。
 * 样式与内置 HomeCard 的头部行逐类对齐。
 */
const PluginHomeCard: React.FC<{
  icon: React.ReactNode;
  label: string;
  value: React.ReactNode;
}> = ({ value }) =>
  React.createElement(
    "div",
    { className: "flex items-center gap-3" },
    React.createElement(
      "div",
      { className: "flex min-w-0 flex-1 flex-col" },
      React.createElement(
        "span",
        {
          className:
            "truncate text-2xl font-bold tabular-nums tracking-tight text-gray-900 dark:text-gray-100",
        },
        value,
      ),
    ),
  );

import {
  isPluginActive,
  pageById,
  registerPage,
  registerPageAction,
  registerWidget,
  setLaunchCardOverride,
} from "./registry";
import {
  downloadTasksFromSnapshots,
  watchDownloadTasks,
} from "./download-tasks";
import { isPermissionGranted, NETWORK_PERMISSION } from "./grants";
import { gameExitInfo, gameExitRevision } from "./game-exit";
import { subscribeLogLines } from "./log-stream";
import {
  announceSensitive,
  forgetSensitiveDecisions,
  requireSensitiveApproval,
  sensitiveDetail,
  sensitiveFileName,
} from "./sensitive";
import { injectPluginStyle, removePluginStyle } from "./styles";
import {
  deliverPluginMessage,
  ipcPlugins,
  registerIpcPlugin,
  subscribePluginMessage,
  unregisterIpcPlugin,
  validatePluginMessage,
} from "./ipc";

/** 宿主 API 版本：不兼容的改动才递增，插件在 plugin.yaml 里声明目标版本 */
export const PLUGIN_API_VERSION = "1";

/** 各插件的清理函数登记表：loader 在卸载/重载前调用 runPluginCleanups */
const pluginCleanups = new Map<string, Set<() => void>>();

/** runPluginCleanups 依次执行并清空某插件的全部清理函数（loader 专用）。 */
export function runPluginCleanups(id: string): void {
  const cleanups = pluginCleanups.get(id);

  if (!cleanups) return;

  pluginCleanups.delete(id);
  for (const cleanup of cleanups) {
    try {
      cleanup();
    } catch (error) {
      // 单个清理函数异常不扩散，不影响其余清理
      console.warn(`[plugin:${id}] 清理函数执行失败：`, error);
    }
  }
}

/**
 * seedPluginSettings 把清单 settings 块的默认值种入插件配置：
 * 仅当对应键为空时写入，用户改过的值不会被覆盖。须在 activate 之前调用，
 * 让插件激活时就能读到自己的设置。需要插件已声明 storage 权限。
 */
export async function seedPluginSettings(
  manifest: PluginManifest,
  api: PluginApi,
): Promise<void> {
  const settings = manifest.settings;

  if (!settings || manifest.capabilities?.storage !== true) return;

  for (const [key, value] of Object.entries(settings)) {
    const existing = await api.config.get(key).catch(() => "");

    if (existing === "") {
      await api.config.set(key, value).catch(() => {});
    }
  }
}

/** 暴露给插件的 UI 组件白名单 */
const UI_COMPONENTS: Record<string, React.ComponentType<any>> = {
  Autocomplete,
  AutocompleteItem,
  Button,
  Card,
  CardBody,
  Chip,
  Code,
  Divider,
  Input,
  Kbd,
  Link,
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
  Progress,
  Radio,
  RadioGroup,
  ScrollShadow,
  Select,
  SelectItem,
  Slider,
  Spinner,
  Switch,
  Tab,
  Tabs,
  Textarea,
  Tooltip,
};

/** 暴露给插件的图标白名单（均为应用已在用的图标，不额外增加包体） */
const UI_ICONS: Record<string, React.ComponentType<any>> = {
  Add20Regular,
  ArrowClockwise20Regular,
  Box20Regular,
  CheckmarkCircle20Regular,
  Cube20Regular,
  Dismiss20Regular,
  DocumentText20Regular,
  Folder20Regular,
  Globe20Regular,
  Heart20Regular,
  Image20Regular,
  Key20Regular,
  PaintBrush20Regular,
  Play20Regular,
  Pulse20Regular,
  PuzzleCube20Regular,
  Search20Regular,
  Server20Regular,
  Settings20Regular,
  Sparkle20Regular,
  Star20Regular,
  Timer20Regular,
  Warning20Regular,
};

/**
 * readDownloadTasks 读一次全部下载任务：宿主内部游戏本体是单条状态机快照、内容
 * 资源 / Java 是任务注册表，这里两路一起取再折成同一形状（见 download-tasks.ts）。
 * 两路各自兜底——一路取不到不该让另一路的任务也看不见。
 */
async function readDownloadTasks(): Promise<DownloadTaskSummary[]> {
  const [game, content] = await Promise.all([
    GetCurrentDownloadSnapshot().catch(() => null),
    GetContentTasks().catch(() => []),
  ]);

  return downloadTasksFromSnapshots(game, content);
}

/**
 * modLoaderCode 插件侧字符串 → 宿主内部数字枚举（Go 侧 ModLoaderType 的 iota 顺序：
 * Vanilla 0 / Fabric 1 / Quilt 2 / NeoForge 3 / Forge 4 / OptiFine 5）。
 *
 * 生成的 DownloadAPI.d.ts 里写的是 `download.ModLoaderType`，而 models.ts 从来没有
 * 导出过这个名字（命名数字类型没被生成），所以调用点必须 `as never`——下载页的
 * MinecraftDownloadOverlay / ContentDownloadOverlay 也是这么绕的。这里把转换与
 * 这个坑一起收在一处，插件不必知道。
 */
const MOD_LOADER_CODES: Record<ModLoaderKind, number> = {
  fabric: 1,
  quilt: 2,
  neoforge: 3,
  forge: 4,
  optifine: 5,
};

function modLoaderCode(kind: ModLoaderKind): number {
  const code = MOD_LOADER_CODES[kind];

  if (code === undefined) {
    throw new Error(t("未知的 Mod Loader：{0}", { "0": String(kind) }));
  }

  return code;
}

/** createPluginApi 为单个插件构造 API 实例（每次加载插件调用一次） */
export function createPluginApi(manifest: PluginManifest): PluginApi {
  const prefix = `${manifest.id}:`;
  const scopedKey = (key: string) =>
    key.startsWith(prefix) ? key : prefix + key;

  // 权限系统：这不是沙箱（插件在 WebView 内全权限），而是声明制权限——
  // 插件必须在 plugin.yaml 的 capabilities 里声明权限，对应 API 才能调用；
  // 未声明就调用直接抛错（报错指出缺哪个权限、怎么补），绝不静默失败。
  const declaredPermissions = new Set(
    Object.entries(manifest.capabilities ?? {})
      .filter(([, enabled]) => enabled === true)
      .map(([name]) => name),
  );
  const requirePermission = (permission: string, action: string): void => {
    if (!declaredPermissions.has(permission)) {
      throw new Error(
        t(
          "插件「{0}」未声明权限「{1}」，已拒绝调用 {2}；请在 plugin.yaml 的 capabilities 中声明后重新加载",
          {
            "0": manifest.id,
            "1": permission,
            "2": action,
          },
        ),
      );
    }
  };

  // 写操作单独校验：读 / 写拆成两个权限（instances-write 等），用户在安装页
  // 才能看出插件会不会改全局状态。旧插件只声明了读权限（instances）时按旧
  // 语义放行并记警告——兼容存量插件，但新插件应直接声明写权限。
  const requireWritePermission = (
    writePermission: string,
    legacyPermission: string,
    action: string,
  ): void => {
    if (declaredPermissions.has(writePermission)) return;
    if (declaredPermissions.has(legacyPermission)) {
      console.warn(
        t(
          "[plugins] {0} 通过旧权限「{1}」调用了写操作 {2}，建议改声明「{3}」",
          {
            "0": manifest.id,
            "1": legacyPermission,
            "2": action,
            "3": writePermission,
          },
        ),
      );

      return;
    }
    requirePermission(writePermission, action);
  };

  // 清理登记：插件卸载/重载时由 loader 依次调用（runPluginCleanups）。
  // 事件订阅类 API 在这里自动登记，插件无感知；onCleanup 供插件补自己的清理。
  const cleanups = new Set<() => void>();

  pluginCleanups.set(manifest.id, cleanups);
  const registerCleanup = (fn: () => void): (() => void) => {
    cleanups.add(fn);

    return () => {
      fn();
      cleanups.delete(fn);
    };
  };

  // 高危动作的"本次运行内同意/拒绝"记在宿主侧；插件被卸载或重新加载时一并忘掉，
  // 这样误点过"拒绝"的用户重新加载插件就能被重新询问。
  registerCleanup(() => forgetSensitiveDecisions(manifest.id));

  const log = (...args: unknown[]) =>
    // eslint-disable-next-line no-console -- 这是暴露给插件的 log API，必须落到控制台
    console.log(`[plugin:${manifest.id}]`, ...args);

  // 卸载后守卫：插件被卸载/停用后，遗留的定时器、回调仍持有 api 对象。
  // 走这些入口再注册/注入会留下无人清理的孤儿（组件、<style>），统一拒绝并记警告。
  const requireActive = (action: string): boolean => {
    if (isPluginActive(manifest.id)) return true;
    log(`${action} 被拒绝：插件已卸载或停用`);

    return false;
  };

  // 通知频控：同一插件 5 秒窗口最多 3 条，超出降级为 log——弹横条是共享资源，
  // 不能让一个失控插件把它刷满。
  let notifyTimestamps: number[] = [];
  const notifyAllowed = () => {
    const now = Date.now();

    notifyTimestamps = notifyTimestamps.filter((stamp) => now - stamp < 5000);
    if (notifyTimestamps.length >= 3) return false;
    notifyTimestamps.push(now);

    return true;
  };
  const pushNotify = (
    level: "info" | "success" | "warning" | "error",
    message: string,
    duration?: number,
  ) => {
    if (!allowed("notifications", `notify.${level}`)) return;
    if (notifyAllowed()) notify[level](message, duration);
    else log(`notify(${level}) 被频控丢弃：${message}`);
  };

  // ---- 用户授权层（插件页每个权限一个 Switch）----
  // 与"未声明直接抛错"分开：前者是插件的用途声明缺失（装之前就该看出），
  // 后者是用户当下的选择——关掉的权限**返回 null**：不执行，也不抛错，
  // 让"我不想让它干这个"不必卸载插件。同一权限只记一次警告，
  // 免得轮询型插件把控制台刷满。
  const warnedBlocked = new Set<string>();
  const blocked = (permission: string, action: string): boolean => {
    if (isPermissionGranted(manifest.id, permission)) return false;
    if (!warnedBlocked.has(permission)) {
      warnedBlocked.add(permission);
      console.warn(
        t(
          "[plugins] {0} 的权限「{1}」已被用户关闭：{2} 返回 null（可在插件页重新打开）",
          { "0": manifest.id, "1": permission, "2": action },
        ),
      );
    }

    return true;
  };

  /**
   * allowed 过前两道闸：声明门（未声明抛错）+ 授权门（用户关掉则返回 false，
   * 调用点直接返回 null）。
   */
  const allowed = (permission: string, action: string): boolean => {
    requirePermission(permission, action);

    return !blocked(permission, action);
  };
  const allowedWrite = (
    writePermission: string,
    legacyPermission: string,
    action: string,
  ): boolean => {
    requireWritePermission(writePermission, legacyPermission, action);

    return !blocked(writePermission, action);
  };
  /**
   * networkAllowed 联网开关。这是**用户独占**的横切开关：不要求 plugin.yaml 声明
   * ——插件本来就能自己 fetch，声明与否都拦不住；这个开关管的是"宿主代它联网"
   * （版本清单、服务器状态、资源与整合包下载）。
   */
  const networkAllowed = (action: string): boolean =>
    !blocked(NETWORK_PERMISSION, action);

  // ---- 插件间消息总线（权限：ipc）----
  // 发送身份由宿主注入；卸载时摘掉清单登记与订阅（订阅经 registerCleanup 自动退订）。
  const senderInfo = {
    id: manifest.id,
    name: manifest.name,
    version: manifest.version,
  };

  registerIpcPlugin(manifest);
  registerCleanup(() => unregisterIpcPlugin(manifest.id));

  // 发送频控：同一插件 5 秒窗口最多 100 条，超出丢弃并记日志——广播是共享
  // 通道，失控插件不该把其它插件的消息处理器刷爆。
  let ipcSendTimestamps: number[] = [];
  const ipcSendAllowed = () => {
    const now = Date.now();

    ipcSendTimestamps = ipcSendTimestamps.filter((stamp) => now - stamp < 5000);
    if (ipcSendTimestamps.length >= 100) return false;
    ipcSendTimestamps.push(now);

    return true;
  };

  // 接收资格：活跃且 ipc 权限仍被用户授权（停用/被收回的插件收不到消息）
  const canReceive = (pluginId: string): boolean =>
    isPluginActive(pluginId) && isPermissionGranted(pluginId, "ipc");

  const dispatch = (
    to: string | null,
    type: string,
    payload: unknown,
  ): number | null => {
    const message = validatePluginMessage(type, payload);

    if (!ipcSendAllowed()) {
      log(
        `ipc.${to === null ? "broadcast" : "send"} 被频控丢弃：${message.type}`,
      );

      return 0;
    }

    return deliverPluginMessage(
      senderInfo,
      to,
      message.type,
      message.payload,
      canReceive,
    );
  };

  return {
    apiVersion: PLUGIN_API_VERSION,
    plugin: { id: manifest.id, name: manifest.name, version: manifest.version },
    react: React,
    h: React.createElement,
    Fragment: React.Fragment,
    ui: UI_COMPONENTS,
    icons: UI_ICONS,
    HomeCard: PluginHomeCard,
    registerWidget: (definition: WidgetDefinition) => {
      if (!requireActive("registerWidget")) return;

      // 注册层规范化：主页列容器是 pointer-events-none，子元素必须显式恢复
      // 事件才可交互（内置组件由 HomeCard 自带）；样式也须与内置卡片一致。
      // 与其要求每个作者记住这些约定，不如在这里统一包一层标准卡片壳——
      // 外观自动归一，交互（含长按拖动）自动恢复。
      return registerWidget({
        ...definition,
        id: scopedKey(definition.id),
        render: (context) =>
          React.createElement(
            "div",
            {
              className:
                "pointer-events-auto nya-enter flex w-full flex-none flex-col gap-3 rounded-lg p-5 shadow-none backdrop-blur-md nya-neon-card",
            },
            definition.render(context),
          ),
      });
    },
    registerPage: (definition: PageDefinition) => {
      if (!requireActive("registerPage")) return;

      return registerPage({
        ...definition,
        id: scopedKey(definition.id),
        order: definition.order ?? 1000,
      });
    },
    // 页面操作按钮：只改自己 id 前缀，目标页面由宿主页面渲染插槽时决定
    registerPageAction: (definition: PageActionDefinition) => {
      if (!requireActive("registerPageAction")) return;

      return registerPageAction({
        ...definition,
        id: scopedKey(definition.id),
        order: definition.order ?? 1000,
      });
    },
    // 启动卡覆盖：独占槽，最后注册者生效；本插件被卸载/停用时由
    // unregisterPlugin 自动摘除，主页回落内置卡片，插件无需清理。
    registerLaunchCard: (definition: LaunchCardDefinition) => {
      if (!requireActive("registerLaunchCard")) return;
      if (typeof definition?.render !== "function") {
        throw new Error(t("registerLaunchCard 需要 { render } 定义对象"));
      }

      return setLaunchCardOverride(manifest.id, definition.render);
    },
    config: {
      get: async (key: string) => {
        if (!allowed("storage", "config.get")) return null;

        return GetValue(scopedKey(key)).catch(() => "");
      },
      // 绑定返回布尔值，这里收敛成 void，插件不必关心实现细节
      set: async (key: string, value: string) => {
        if (!allowed("storage", "config.set")) return;
        await SetValue(scopedKey(key), value);
      },
      clear: async (key: string) => {
        if (!allowed("storage", "config.clear")) return;
        await ClearValue(scopedKey(key));
      },
    },
    // 全局 CSS 注入：作用于整个启动器，可自定义任意控件的样式。卸载/重载/
    // 停用时宿主按插件 id 自动移除全部注入，插件无需清理（见 styles.ts）。
    styles: {
      inject: (css: string, key?: string) => {
        if (!allowed("styles", "styles.inject")) return;
        if (!requireActive("styles.inject")) return;
        injectPluginStyle(manifest.id, css, key);
        // 注入全局 CSS 会波及整个启动器（含内置控件），属于要公示的动作；
        // 移除是自己撤自己的东西，不公示。
        announceSensitive(
          manifest,
          "global-styles",
          sensitiveDetail(key ?? "inline"),
        );
      },
      remove: (key: string) => {
        if (!allowed("styles", "styles.remove")) return;
        removePluginStyle(manifest.id, key);
      },
    },
    log,
    onCleanup: (fn: () => void) => {
      // 清理函数在卸载路径上执行，非函数值会让整批清理中断在半路
      if (typeof fn !== "function") {
        throw new Error(t("onCleanup 需要一个函数"));
      }
      cleanups.add(fn);
    },
    notify: {
      info: (message: string, duration?: number) =>
        pushNotify("info", message, duration),
      success: (message: string, duration?: number) =>
        pushNotify("success", message, duration),
      warning: (message: string, duration?: number) =>
        pushNotify("warning", message, duration),
      error: (message: string, duration?: number) =>
        pushNotify("error", message, duration),
    },
    // OpenPage 只放行 http(s)；openPath 在宿主侧锁进插件自己的目录，越界直接拒绝
    openUrl: async (url: string) => {
      if (!allowed("open-url", "openUrl")) return;

      return OpenPage(url);
    },
    openPath: async (path: string) => {
      if (!allowed("open-path", "openPath")) return;

      return OpenPluginPath(manifest.id, path);
    },
    // 启动状态只读摘要：插件据此禁用启动按钮，避免盲点启动
    getLaunchState: async () => {
      const snapshot = await GetLaunchSnapshot();
      const phase = Number(snapshot?.Phase ?? 0);

      return {
        launchPhase: phase,
        isBusy: phase === 1,
        isGameRunning: phase === 2,
      };
    },
    // 下载任务状态：宿主内部是"游戏本体单快照 + 内容任务注册表"两套状态，
    // 这里折成一个列表交给插件（只读，与右下角下载中心同源）。
    getDownloadTasks: async () => {
      if (!allowed("downloads", "getDownloadTasks")) return null;

      return readDownloadTasks();
    },
    // ---- 下载域只读查询（权限：downloads）----
    // 版本清单与 Loader 元数据是"发起下载"的前置：插件先查再挑，最后把对象
    // 原样交给 startDownload / startModLoaderDownload，避免自己拼清单 URL。
    // 这两条还额外过联网开关（清单要去外网拉）。
    getVersions: async () => {
      if (!allowed("downloads", "getVersions")) return null;
      if (!networkAllowed("getVersions")) return null;

      return GetVersions();
    },
    getModLoaderVersions: async (
      loader: ModLoaderKind,
      minecraftVersion: string,
    ) => {
      if (!allowed("downloads", "getModLoaderVersions")) return null;
      if (!networkAllowed("getModLoaderVersions")) return null;

      return GetModLoaderVersions(
        modLoaderCode(loader) as never,
        minecraftVersion,
      );
    },
    getDownloadSources: async () => {
      if (!allowed("downloads", "getDownloadSources")) return null;

      return GetAllDownloadSources();
    },
    getJavaRuntimes: async () => {
      if (!allowed("downloads", "getJavaRuntimes")) return null;

      return GetInstalledJavaRuntimes();
    },
    // ---- 下载域写入（权限：downloads-write）----
    // 这里刻意**不用** requireWritePermission 的"旧读权限放行"兼容：downloads 是
    // 新增权限、没有存量插件，放行只会让"只想看进度"的插件静默拿到写盘/安装能力。
    // 过闸在前、动手在后：用户在 NekoPrompt 里拒绝时，磁盘上不该有任何东西被改动。
    // 后端用布尔值表示"拒绝启动"（已有下载在跑 / 版本非法），这里一律翻成抛错：
    // 插件不该拿到一个假成功的 Promise。
    startDownload: async (version: models.MinecraftVersion) => {
      // 这里用 allowed 而不是 allowedWrite：下载域刻意不做"旧读权限放行"兼容
      if (!allowed("downloads-write", "startDownload")) return;
      if (!networkAllowed("startDownload")) return;
      await requireSensitiveApproval(
        manifest,
        "install-content",
        sensitiveDetail(`Minecraft ${version?.id ?? ""}`),
      );
      if (!(await StartDownload(version))) {
        throw new Error(
          t("下载未能启动：宿主拒绝了这次请求（可能已有下载在进行）"),
        );
      }
    },
    startModLoaderDownload: async (options: {
      version: models.MinecraftVersion;
      loader: downloadModels.ModLoaderVersion;
      instanceName: string;
      skipFabricApi?: boolean;
    }) => {
      if (!allowed("downloads-write", "startModLoaderDownload")) return;
      if (!networkAllowed("startModLoaderDownload")) return;
      await requireSensitiveApproval(
        manifest,
        "install-content",
        sensitiveDetail(options.instanceName),
      );
      if (
        !(await StartModLoaderDownload(
          options.version,
          options.loader,
          options.instanceName,
          options.skipFabricApi === true,
        ))
      ) {
        throw new Error(
          t("下载未能启动：宿主拒绝了这次请求（可能已有下载在进行）"),
        );
      }
    },
    downloadResource: async (request: models.ResourceDownloadRequest) => {
      if (!allowed("downloads-write", "downloadResource")) return null;
      if (!networkAllowed("downloadResource")) return null;
      await requireSensitiveApproval(
        manifest,
        "install-content",
        sensitiveDetail(
          [request?.source, request?.projectId].filter(Boolean).join(" / "),
        ),
      );

      return DownloadResourceVersion(request);
    },
    installModpack: async (mrpackPath: string, contentDirectory: string) => {
      if (!allowed("downloads-write", "installModpack")) return null;
      await requireSensitiveApproval(
        manifest,
        "install-content",
        sensitiveDetail(sensitiveFileName(mrpackPath)),
      );

      return InstallModpackToInstance(mrpackPath, contentDirectory);
    },
    // ---- 只读：机器占用与音乐库 ----
    getMemorySnapshot: async () => {
      if (!allowed("system-status", "getMemorySnapshot")) return null;

      return GetMemorySnapshot();
    },
    getSystemUsage: async () => {
      if (!allowed("system-status", "getSystemUsage")) return null;

      return GetSystemUsage();
    },
    getDiskUsage: async (path: string) => {
      if (!allowed("system-status", "getDiskUsage")) return null;

      return GetDiskUsage(path);
    },
    getCurrentTrack: async () => {
      if (!allowed("music", "getCurrentTrack")) return null;

      return GetCurrentTrack();
    },
    getMusicTracks: async () => {
      if (!allowed("music", "getMusicTracks")) return null;

      return GetMusicTracks();
    },
    // 导航：目标是宿主或其它插件注册的页面，页面不存在直接抛错——
    // 静默跳到主页会让插件作者以为"跳转成功了但页面是错的"。
    navigateToPage: (pageId: string, detail?: string) => {
      const target = String(pageId ?? "").trim();

      if (!target) throw new Error(t("navigateToPage 需要页面 id"));
      if (!pageById(target)) {
        throw new Error(t("页面不存在：{0}", { "0": target }));
      }
      hostNavigateToPage(target, detail ?? undefined);
    },
    setClipboard: async (text: string) => {
      if (!allowed("clipboard", "setClipboard")) return;

      return SetClipboard(text);
    },
    // 插件间通信：投递目标只能是活跃且 ipc 权限仍授权的插件；负载校验、
    // 频控与深拷贝隔离见上方 dispatch 与 ipc.ts
    ipc: {
      plugins: () => {
        if (!allowed("ipc", "ipc.plugins")) return null;

        return ipcPlugins();
      },
      send: (targetPluginId: string, type: string, payload?: unknown) => {
        if (!allowed("ipc", "ipc.send")) return null;
        if (!requireActive("ipc.send")) return null;
        const target = String(targetPluginId ?? "").trim();

        if (!target) throw new Error(t("ipc.send 需要目标插件 id"));

        return dispatch(target, type, payload);
      },
      broadcast: (type: string, payload?: unknown) => {
        if (!allowed("ipc", "ipc.broadcast")) return null;
        if (!requireActive("ipc.broadcast")) return null;

        return dispatch(null, type, payload);
      },
      onMessage: (
        handler: (message: PluginMessage) => void,
      ): (() => void) | null => {
        if (!allowed("ipc", "ipc.onMessage")) return null;
        if (typeof handler !== "function") {
          throw new Error(t("ipc.onMessage 需要一个消息处理函数"));
        }

        return registerCleanup(subscribePluginMessage(manifest.id, handler));
      },
    },
    confirm: (message: string, options?: { title?: string }) =>
      hostConfirm(options?.title ?? manifest.name, message, {
        confirmLabel: t("确定"),
      }),
    t: (text: string, params?: Record<string, string | number>) =>
      t(text, params),
    onLaunchPhaseChange: (
      handler: (state: LaunchStateSummary) => void,
    ): (() => void) => {
      const cancel = EventsOn(
        "launch:changed",
        (snapshot: launchModels.GameLaunchSnapshot | undefined) => {
          const phase = Number(snapshot?.Phase ?? 0);

          handler({
            launchPhase: phase,
            isBusy: phase === 1,
            isGameRunning: phase === 2,
          });
        },
      );

      // 卸载/重载时自动清理；返回给插件的取消函数会同时解除登记
      return registerCleanup(typeof cancel === "function" ? cancel : () => {});
    },
    onInstancesChanged: (
      handler: (snapshot: instanceModels.GameInstanceSnapshot) => void,
    ): (() => void) => {
      const cancel = EventsOn("instance:changed", (snapshot: unknown) => {
        handler(
          instanceModels.GameInstanceSnapshot.createFrom(
            (snapshot ?? {}) as Record<string, unknown>,
          ),
        );
      });

      return registerCleanup(typeof cancel === "function" ? cancel : () => {});
    },
    // 下载任务变化订阅：宿主两路事件（游戏本体进度 / 内容任务列表）都指向
    // "重读一份完整列表再投递"，节流由 watchDownloadTasks 兜住（最多一轮在途
    // + 一次补跑）；订阅不当场回调，需要当前状态请先调 getDownloadTasks()。
    onDownloadTasksChanged: (
      handler: (tasks: DownloadTaskSummary[]) => void,
    ): (() => void) | null => {
      if (!allowed("downloads", "onDownloadTasksChanged")) return null;
      const watcher = watchDownloadTasks(handler, readDownloadTasks);
      const offGame = EventsOn("download:progress", () => watcher.notify());
      const offContent = EventsOn("download:contentTasks", () =>
        watcher.notify(),
      );
      const cancel = () => {
        watcher.cancel();
        offGame();
        offContent();
      };

      return registerCleanup(cancel);
    },
    // 启动终态：launch:changed 的快照可能因重连重复送达，按 Revision 去重；
    // 崩溃判定复用 lib/crashNotice（与宿主自己的崩溃弹窗同一份规则）。
    onGameExit: (handler: (info: GameExitInfo) => void): (() => void) => {
      let lastRevision = -1;
      const cancel = EventsOn(
        "launch:changed",
        (snapshot: launchModels.GameLaunchSnapshot | undefined) => {
          const info = gameExitInfo(snapshot);

          if (!info) return;
          const revision = gameExitRevision(snapshot);

          if (revision === lastRevision) return;
          lastRevision = revision;
          handler(info);
        },
      );

      return registerCleanup(typeof cancel === "function" ? cancel : () => {});
    },
    // 启动日志新增行：轮询与窗口化都在 log-stream 里，这里只管权限与清理。
    // 日志里有路径 / 账号名 / 服务器地址 / 报错原文，是"危险读"，所以从免费改为
    // 需要声明 logs 权限——声明在安装页可见，用户装之前就知道它会读运行日志。
    onLogLine: (
      handler: (batch: LogLineBatch) => void,
      options?: LogLineOptions,
    ): (() => void) | null => {
      if (!allowed("logs", "onLogLine")) return null;

      return registerCleanup(subscribeLogLines(handler, options));
    },
    // 一次拿全实例深数据：加载器信息、隔离布局与 Mod / 资源包 / 光影 / 存档列表
    getVersionDetails: async (versionId: string) => {
      if (!allowed("instances", "getVersionDetails")) return null;

      return GetVersionDetails(versionId);
    },
    getScreenshots: async (max: number) => {
      if (!allowed("instances", "getScreenshots")) return null;

      return ListScreenshots(max);
    },
    // 连接失败抛错由插件接住；状态对象原样透传（地址/版本/在线人数等展示字段）。
    // 宿主代插件联网 → 额外过联网开关。
    getServerStatus: async (host: string, port: number) => {
      if (!allowed("server-status", "getServerStatus")) return null;
      if (!networkAllowed("getServerStatus")) return null;

      return PingServer(host, port);
    },
    getSaves: async (savesDirectory: string) => {
      if (!allowed("instances", "getSaves")) return null;

      return ReadSaves(savesDirectory);
    },
    // 实例 / 账号是只读查询；启动类动作改变整机状态，须声明 launch 权限
    getInstances: async () => {
      if (!allowed("instances", "getInstances")) return null;

      return GetCurrentInstanceSnapshot();
    },
    // 只交摘要：LaunchAccount 里嵌着微软/皮肤站凭据，原始结构永远不出宿主；
    // 头像拉取失败不阻塞列表（留空走首字母占位）
    getAccounts: async () => {
      if (!allowed("accounts", "getAccounts")) return null;
      const accounts = await GetAccounts();

      return Promise.all(
        accounts.map(async (account) => {
          const key = await GetAccountStableKey(account);
          const avatar = await GetAvatarUrl(key).catch(() => "");

          return {
            key,
            name: account.DisplayName ?? "",
            type: account.Type ?? "",
            avatar,
          };
        }),
      );
    },
    // 切换选中是显式的全局状态写入（与 launchVersion 的"不落选中"相对）
    selectInstance: async (versionId: string) => {
      if (!allowedWrite("instances-write", "instances", "selectInstance")) {
        return;
      }
      if (!(await SelectInstance(versionId))) {
        throw new Error(t("切换实例失败：{0}", { "0": versionId }));
      }
      announceSensitive(
        manifest,
        "switch-instance",
        sensitiveDetail(versionId),
      );
    },
    // 内容启停：后端第二参是"禁用"，这里翻成插件侧更直观的 enabled。
    // 只做 .disabled 重命名——宿主从不删除用户的 mod 文件，插件也一样。
    setContentEnabled: async (entryPath: string, enabled: boolean) => {
      if (!allowedWrite("instances-write", "instances", "setContentEnabled")) {
        return;
      }
      await ToggleContentEntry(entryPath, !enabled);
      announceSensitive(
        manifest,
        "toggle-content",
        enabled
          ? t("启用了{0}", {
              "0": sensitiveDetail(sensitiveFileName(entryPath)),
            })
          : t("禁用了{0}", {
              "0": sensitiveDetail(sensitiveFileName(entryPath)),
            }),
      );
    },
    getVersionProfile: async (
      minecraftDirectory: string,
      versionId: string,
    ) => {
      if (!allowed("instances", "getVersionProfile")) return null;

      return GetVersionProfile(minecraftDirectory, versionId);
    },
    saveVersionProfile: async (profile: configModels.GameVersionProfile) => {
      if (!allowedWrite("instances-write", "instances", "saveVersionProfile")) {
        return;
      }
      // 过闸在前：档案里的 Java 路径 / 包装命令 / JVM 参数等同"下次启动执行什么"，
      // 用户拒绝时一个字都不该落盘。
      await requireSensitiveApproval(
        manifest,
        "modify-instance",
        sensitiveDetail(profile?.VersionId ?? ""),
      );
      if (!(await SaveVersionProfile(profile))) {
        throw new Error(t("保存实例档案失败"));
      }
    },
    getLauncherSettings: async () => {
      if (!allowed("launcher-config", "getLauncherSettings")) return null;

      return LoadGlobalLaunchSettings();
    },
    saveLauncherSettings: async (
      settings: configModels.GlobalLaunchSettings,
    ) => {
      if (
        !allowedWrite(
          "launcher-config-write",
          "launcher-config",
          "saveLauncherSettings",
        )
      ) {
        return;
      }
      await requireSensitiveApproval(manifest, "modify-settings");
      if (!(await SaveGlobalLaunchSettings(settings))) {
        throw new Error(t("保存启动器设置失败"));
      }
    },
    launchSelected: async () => {
      if (!allowed("launch", "launchSelected")) return null;
      await requireSensitiveApproval(manifest, "launch");

      return Launch("", null, "");
    },
    // 显式版本启动：宿主在本次启动内临时应用版本，不改变"当前选中"
    launchVersion: async (versionId: string) => {
      if (!allowed("launch", "launchVersion")) return null;
      await requireSensitiveApproval(
        manifest,
        "launch",
        sensitiveDetail(versionId),
      );

      return LaunchVersion(versionId, "", null, "");
    },
  };
}
