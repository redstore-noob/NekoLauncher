/*
 * 运行时注册表：小组件、页面、页面操作按钮与启动卡覆盖的唯一来源。
 *
 * 内置实现与第三方插件都通过 registerWidget / registerPage 注册，注册表在运行时可变，
 * 插件加载完成后界面会自动跟着更新（useWidgets / usePages 订阅变更）。
 * 布局只持久化 id 列表，因此注册顺序与持久化数据互不依赖。
 * 启动卡是独占覆盖槽（launchCardOverride）：插件 registerLaunchCard 即替换
 * 主页右侧启动卡内容，该插件卸载/停用时自动清空并回落内置卡片。
 * 页面操作按钮（pageActions）是细粒度插槽：往宿主已画好的页面里塞按钮，不做整页占位。
 */
import type {
  LaunchCardDefinition,
  PageActionDefinition,
  PageDefinition,
  WidgetDefinition,
} from "./types";

import { useSyncExternalStore } from "react";

import { t } from "../i18n";

const widgetRegistry = new Map<string, WidgetDefinition>();
const pageRegistry = new Map<string, PageDefinition>();
const pageActionRegistry = new Map<string, PageActionDefinition>();
const listeners = new Set<() => void>();

/* ---------------- 启动卡覆盖（独占槽） ----------------
 * 同时只允许一个插件覆盖启动卡：多个主题类插件各自换卡会互相打架，
 * 独占 + 最后注册者生效是最简单可预期的语义。对象引用在无变更时保持
 * 同一实例，可直接作为 useSyncExternalStore 的快照。 */
export interface LaunchCardOverride {
  pluginId: string;
  render: LaunchCardDefinition["render"];
}

let launchCardOverride: LaunchCardOverride | null = null;

/* ---------------- 插件活跃状态 ----------------
 * 当前处于"已加载"状态的插件 id 集合。放在注册表而不是 loader：api.ts 要在
 * styles.inject / registerWidget / registerPage 里做卸载后复查，而 api.ts 被
 * loader 反向依赖，直接引用 loader 会成环。由 loader 在加载/卸载路径上维护。 */
const activePlugins = new Set<string>();

/** markPluginActive loader 在插件完成加载时置 true、卸载/停用/加载失败时置 false */
export function markPluginActive(pluginId: string, active: boolean): void {
  if (active) activePlugins.add(pluginId);
  else activePlugins.delete(pluginId);
}

/** isPluginActive 插件当前是否处于已加载状态（供异步回调在 await 之后复查） */
export function isPluginActive(pluginId: string): boolean {
  return activePlugins.has(pluginId);
}

/** activePluginIDs 当前全部活跃插件的 id 快照（reloadPlugins 逐个卸载用） */
export function activePluginIDs(): string[] {
  return [...activePlugins];
}

// useSyncExternalStore 要求同一份快照在无变更时保持同一引用，故变更时才重建
let widgetSnapshot: WidgetDefinition[] = [];
let pageSnapshot: PageDefinition[] = [];
let pageActionSnapshot = new Map<string, PageActionDefinition[]>();

/** 空页面按钮列表：无注册项时返回同一个空数组，保持快照引用稳定 */
const EMPTY_PAGE_ACTIONS: PageActionDefinition[] = [];

function rebuildSnapshots() {
  widgetSnapshot = [...widgetRegistry.values()];
  pageSnapshot = [...pageRegistry.values()].sort(
    (left, right) =>
      left.order - right.order || left.id.localeCompare(right.id),
  );

  const grouped = new Map<string, PageActionDefinition[]>();

  for (const action of pageActionRegistry.values()) {
    const bucket = grouped.get(action.pageId);

    if (bucket) bucket.push(action);
    else grouped.set(action.pageId, [action]);
  }
  for (const bucket of grouped.values()) {
    bucket.sort(
      (left, right) =>
        (left.order ?? 1000) - (right.order ?? 1000) ||
        left.id.localeCompare(right.id),
    );
  }
  pageActionSnapshot = grouped;
}

function notify() {
  rebuildSnapshots();
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);

  return () => {
    listeners.delete(listener);
  };
}

/** 全部已注册的小组件（注册或卸载后自动更新） */
export function useWidgets(): WidgetDefinition[] {
  return useSyncExternalStore(
    subscribe,
    () => widgetSnapshot,
    () => widgetSnapshot,
  );
}

/** 全部已注册的页面，已按 order 排好 */
export function usePages(): PageDefinition[] {
  return useSyncExternalStore(
    subscribe,
    () => pageSnapshot,
    () => pageSnapshot,
  );
}

/** 按 id 取小组件定义；未注册（插件被禁用/已卸载）返回 undefined */
export function widgetById(id: string): WidgetDefinition | undefined {
  return widgetRegistry.get(id);
}

/** 按 id 取页面定义 */
export function pageById(id: string): PageDefinition | undefined {
  return pageRegistry.get(id);
}

/** 注册主页小组件；同 id 重复注册时后者覆盖（便于插件重载） */
export function registerWidget(definition: WidgetDefinition) {
  if (!definition.id) throw new Error(t("小组件缺少 id"));
  widgetRegistry.set(definition.id, definition);
  notify();
}

/** 注册侧边栏页面 */
export function registerPage(definition: PageDefinition) {
  if (!definition.id) throw new Error(t("页面缺少 id"));
  pageRegistry.set(definition.id, definition);
  notify();
}

/**
 * 注册一个页面操作按钮；同 id 重复注册时后者覆盖（便于插件重载）。
 * 目标页面不存在不报错也不显示——插件页的注册顺序（谁先加载）无法保证，
 * 这里只在控制台提醒一句，避免作者对着"按钮没出来"发呆。
 */
export function registerPageAction(definition: PageActionDefinition) {
  if (!definition.id) throw new Error(t("页面按钮缺少 id"));
  if (!definition.pageId) throw new Error(t("页面按钮缺少 pageId"));
  if (typeof definition.onPress !== "function") {
    throw new Error(t("页面按钮缺少 onPress"));
  }
  if (!pageRegistry.has(definition.pageId)) {
    console.warn(
      t("[plugins] 页面「{0}」尚未注册，按钮「{1}」在它出现前不会显示", {
        "0": definition.pageId,
        "1": definition.id,
      }),
    );
  }
  pageActionRegistry.set(definition.id, definition);
  notify();
}

/** usePageActions 某页面的全部插件按钮（按 order 排好，无注册项时是空数组） */
export function usePageActions(pageId: string): PageActionDefinition[] {
  return useSyncExternalStore(
    subscribe,
    () => pageActionSnapshot.get(pageId) ?? EMPTY_PAGE_ACTIONS,
    () => pageActionSnapshot.get(pageId) ?? EMPTY_PAGE_ACTIONS,
  );
}

/** pageActionsFor 非 React 环境的即时读取（宿主页自检与测试用） */
export function pageActionsFor(pageId: string): PageActionDefinition[] {
  return pageActionSnapshot.get(pageId) ?? EMPTY_PAGE_ACTIONS;
}

/**
 * setLaunchCardOverride 挂上一个插件的启动卡覆盖（独占槽，最后注册者生效）。
 * 由插件 API 的 registerLaunchCard 调用；同插件重复注册即替换自己的旧 render。
 */
export function setLaunchCardOverride(
  pluginId: string,
  render: LaunchCardDefinition["render"],
) {
  launchCardOverride = { pluginId, render };
  notify();
}

/** useLaunchCardOverride 当前生效的启动卡覆盖；无覆盖时为 null（主页渲染内置卡片） */
export function useLaunchCardOverride(): LaunchCardOverride | null {
  return useSyncExternalStore(
    subscribe,
    () => launchCardOverride,
    () => launchCardOverride,
  );
}

/** getLaunchCardOverride 非 React 环境的即时读取（宿主页自检与测试用） */
export function getLaunchCardOverride(): LaunchCardOverride | null {
  return launchCardOverride;
}

/**
 * 卸载某个插件的全部注册项：id 以 "<插件id>:" 开头的都会被清掉。
 * 布局里对应的 id 不会被删除，插件重新启用后组件回到原位。
 * 它挂的启动卡覆盖也一并摘除，主页回落内置卡片。
 */
export function unregisterPlugin(pluginId: string) {
  const prefix = `${pluginId}:`;
  let changed = false;

  if (launchCardOverride?.pluginId === pluginId) {
    launchCardOverride = null;
    changed = true;
  }
  for (const id of [...widgetRegistry.keys()]) {
    if (id.startsWith(prefix)) {
      widgetRegistry.delete(id);
      changed = true;
    }
  }
  for (const id of [...pageRegistry.keys()]) {
    if (id.startsWith(prefix)) {
      pageRegistry.delete(id);
      changed = true;
    }
  }
  for (const id of [...pageActionRegistry.keys()]) {
    if (id.startsWith(prefix)) {
      pageActionRegistry.delete(id);
      changed = true;
    }
  }
  if (changed) notify();
}

/** 首次运行（配置缺失）时的默认布局：常用信息 + 快捷入口 */
export const DEFAULT_WIDGET_IDS = [
  "playtime",
  "worlds",
  "rewind",
  "quickjoin",
  "log",
  "quick-download",
  "quick-settings",
  "appearance-quick",
];

/** 兜底页：配置里的页面 id 指向未注册页面时回到这里 */
export const DEFAULT_PAGE_ID = "home";
