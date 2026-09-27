// 该文件已经过验证。
/*
 * 插件API。
 * 很安全，只有必要能力。
 */
import type {
  LaunchStateSummary,
  PageDefinition,
  PluginApi,
  PluginManifest,
  WidgetDefinition,
} from "./types";
import type {
  config as configModels,
  launch as launchModels,
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
import { ReadSaves } from "../../wailsjs/go/bindings/ContentAPI";
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
import { confirm as hostConfirm, notify } from "../components/overlay/dialog";
import { t } from "../i18n";
import { EventsOn } from "../../wailsjs/runtime/runtime";

/**
 * 插件版 HomeCard：只有「图标磁贴 + 标题 + 大数值」头部行，**不带卡片容器**——
 * 卡片壳由注册层统一提供（套两层卡就是截图里那种嵌套观感）。
 * 样式与内置 HomeCard 的头部行逐类对齐。
 */
const PluginHomeCard: React.FC<{
  icon: React.ReactNode;
  label: string;
  value: React.ReactNode;
}> = ({ icon, label, value }) =>
  React.createElement(
    "div",
    { className: "flex items-center gap-3" },
    React.createElement(
      "div",
      {
        className:
          "flex size-10 flex-none items-center justify-center rounded-2xl bg-primary/15 text-primary",
      },
      icon,
    ),
    React.createElement(
      "div",
      { className: "flex min-w-0 flex-1 flex-col" },
      React.createElement(
        "span",
        {
          className:
            "text-[11px] font-semibold uppercase tracking-wider text-gray-400",
        },
        label,
      ),
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

import { registerPage, registerWidget } from "./registry";

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

  const log = (...args: unknown[]) =>
    // eslint-disable-next-line no-console -- 这是暴露给插件的 log API，必须落到控制台
    console.log(`[plugin:${manifest.id}]`, ...args);

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
    requirePermission("notifications", `notify.${level}`);
    if (notifyAllowed()) notify[level](message, duration);
    else log(`notify(${level}) 被频控丢弃：${message}`);
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
                "pointer-events-auto nya-enter flex w-full flex-none flex-col gap-3 rounded-3xl p-5 shadow-none backdrop-blur-md nya-neon-card",
            },
            definition.render(context),
          ),
      });
    },
    registerPage: (definition: PageDefinition) =>
      registerPage({
        ...definition,
        id: scopedKey(definition.id),
        order: definition.order ?? 1000,
      }),
    config: {
      get: (key: string) => {
        requirePermission("storage", "config.get");

        return GetValue(scopedKey(key)).catch(() => "");
      },
      // 绑定返回布尔值，这里收敛成 void，插件不必关心实现细节
      set: async (key: string, value: string) => {
        requirePermission("storage", "config.set");
        await SetValue(scopedKey(key), value);
      },
      clear: async (key: string) => {
        requirePermission("storage", "config.clear");
        await ClearValue(scopedKey(key));
      },
    },
    log,
    onCleanup: (fn: () => void) => {
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
    openUrl: (url: string) => {
      requirePermission("open-url", "openUrl");

      return OpenPage(url);
    },
    openPath: (path: string) => {
      requirePermission("open-path", "openPath");

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
    setClipboard: (text: string) => {
      requirePermission("clipboard", "setClipboard");

      return SetClipboard(text);
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
    // 一次拿全实例深数据：加载器信息、隔离布局与 Mod / 资源包 / 光影 / 存档列表
    getVersionDetails: (versionId: string) => {
      requirePermission("instances", "getVersionDetails");

      return GetVersionDetails(versionId);
    },
    getScreenshots: (max: number) => {
      requirePermission("instances", "getScreenshots");

      return ListScreenshots(max);
    },
    // 连接失败抛错由插件接住；状态对象原样透传（地址/版本/在线人数等展示字段）
    getServerStatus: (host: string, port: number) => {
      requirePermission("server-status", "getServerStatus");

      return PingServer(host, port);
    },
    getSaves: (savesDirectory: string) => {
      requirePermission("instances", "getSaves");

      return ReadSaves(savesDirectory);
    },
    // 实例 / 账号是只读查询；启动类动作改变整机状态，须声明 launch 权限
    getInstances: () => {
      requirePermission("instances", "getInstances");

      return GetCurrentInstanceSnapshot();
    },
    // 只交摘要：LaunchAccount 里嵌着微软/皮肤站凭据，原始结构永远不出宿主；
    // 头像拉取失败不阻塞列表（留空走首字母占位）
    getAccounts: async () => {
      requirePermission("accounts", "getAccounts");
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
      requirePermission("instances", "selectInstance");
      if (!(await SelectInstance(versionId))) {
        throw new Error(t("切换实例失败：{0}", { "0": versionId }));
      }
    },
    getVersionProfile: (minecraftDirectory: string, versionId: string) => {
      requirePermission("instances", "getVersionProfile");

      return GetVersionProfile(minecraftDirectory, versionId);
    },
    saveVersionProfile: async (profile: configModels.GameVersionProfile) => {
      requirePermission("instances", "saveVersionProfile");
      if (!(await SaveVersionProfile(profile))) {
        throw new Error(t("保存实例档案失败"));
      }
    },
    getLauncherSettings: () => {
      requirePermission("launcher-config", "getLauncherSettings");

      return LoadGlobalLaunchSettings();
    },
    saveLauncherSettings: async (
      settings: configModels.GlobalLaunchSettings,
    ) => {
      requirePermission("launcher-config", "saveLauncherSettings");
      if (!(await SaveGlobalLaunchSettings(settings))) {
        throw new Error(t("保存启动器设置失败"));
      }
    },
    launchSelected: () => {
      requirePermission("launch", "launchSelected");

      return Launch("", null);
    },
    // 显式版本启动：宿主在本次启动内临时应用版本，不改变"当前选中"
    launchVersion: (versionId: string) => {
      requirePermission("launch", "launchVersion");

      return LaunchVersion(versionId, "", null);
    },
  };
}
