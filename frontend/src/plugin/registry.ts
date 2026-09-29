/*
 * 运行时注册表：小组件与页面的唯一来源。
 *
 * 内置实现与第三方插件都通过 registerWidget / registerPage 注册，注册表在运行时可变，
 * 插件加载完成后界面会自动跟着更新（useWidgets / usePages 订阅变更）。
 * 布局只持久化 id 列表，因此注册顺序与持久化数据互不依赖。
 */
import type { PageDefinition, WidgetDefinition } from "./types";

import { useSyncExternalStore } from "react";

import { t } from "../i18n";

const widgetRegistry = new Map<string, WidgetDefinition>();
const pageRegistry = new Map<string, PageDefinition>();
const listeners = new Set<() => void>();

// useSyncExternalStore 要求同一份快照在无变更时保持同一引用，故变更时才重建
let widgetSnapshot: WidgetDefinition[] = [];
let pageSnapshot: PageDefinition[] = [];

function rebuildSnapshots() {
  widgetSnapshot = [...widgetRegistry.values()];
  pageSnapshot = [...pageRegistry.values()].sort(
    (left, right) =>
      left.order - right.order || left.id.localeCompare(right.id),
  );
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
 * 卸载某个插件的全部注册项：id 以 "<插件id>:" 开头的都会被清掉。
 * 布局里对应的 id 不会被删除，插件重新启用后组件回到原位。
 */
export function unregisterPlugin(pluginId: string) {
  const prefix = `${pluginId}:`;
  let changed = false;

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
