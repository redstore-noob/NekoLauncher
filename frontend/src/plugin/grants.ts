/*
 * 插件权限的"用户授权"层：插件管理页里每个权限一个 Switch，关掉当场失效。
 *
 * 三层闸各管一段：
 *  ① plugin.yaml 的 `capabilities` —— 装之前的用途声明。未声明就调用**直接抛错**，
 *     让用户在安装页先看清单；
 *  ② 本模块的授权表 —— 用户开关。关掉的权限**返回 null**（不执行，也不抛错），
 *     让"我不想让它干这个"不需要卸载插件；
 *  ③ 动作闸门（sensitive.ts）—— 高危动作每次弹确认，中危动作发 NekoAlert。
 *
 * 存放位置是 launcher.yaml 的 pluginPermissions 键：与 closeAction /
 * homeWidgetColumns 同类的"前端独占"普通键（不属于账户域，Go 侧守卫放行）。
 * 安全边界是后端闸门（backend-gate.ts：插件碰不到 window.go 与原始 IPC
 * 出口）；这一层管的是"用户看得见、关得掉、误伤有救"，与闸门互补。
 */
import { useSyncExternalStore } from "react";

import { GetValue, SetValue } from "../../wailsjs/go/bindings/ConfigAPI";
import { t } from "../i18n";

/** 授权表在 launcher.yaml 里的键 */
export const PLUGIN_GRANTS_CONFIG_KEY = "pluginPermissions";

/** 联网：宿主代插件发起的网络请求（版本清单 / 服务器状态 / 资源与整合包下载） */
export const NETWORK_PERMISSION = "network";

/**
 * 默认关闭的权限：装完先不给，用户在插件页显式打开才生效。
 *
 * 只放**域窄、隐私或对外发起请求**的那几个——默认关太宽会把新插件一律弄成"坏的"，
 * 反而逼用户闭眼全开。需要动作级确认的高危写权限不在这里：它们的每一次动作本来
 * 都要过 NekoPrompt。
 */
export const DEFAULT_OFF_PERMISSIONS: readonly string[] = [
  NETWORK_PERMISSION,
  "logs",
  "accounts",
  "music",
];

/** 插件 id → 权限 → 是否授权（只记用户显式改过的，缺省走默认值） */
type GrantTable = Record<string, Record<string, boolean>>;

let table: GrantTable = {};
let version = 0;
const listeners = new Set<() => void>();

function notify() {
  version += 1;
  listeners.forEach((listener) => listener());
}

/** 权限的默认授权状态（未在表里显式设置时用） */
export function defaultPermissionGrant(permission: string): boolean {
  return !DEFAULT_OFF_PERMISSIONS.includes(permission);
}

/** isPermissionGranted 某插件某权限当前是否被用户授权 */
export function isPermissionGranted(
  pluginId: string,
  permission: string,
): boolean {
  const explicit = table[pluginId]?.[permission];

  return explicit === undefined ? defaultPermissionGrant(permission) : explicit;
}

/** grantedPermissions 某插件当前被关掉的权限列表（插件页展示用） */
export function disabledPermissions(
  pluginId: string,
  permissions: readonly string[],
): string[] {
  return permissions.filter(
    (permission) => !isPermissionGranted(pluginId, permission),
  );
}

/**
 * setPluginPermission 改一个开关并落盘。
 * 授权表整体写回同一个键：条目很少（每个插件几项），不值得拆键。
 */
export async function setPluginPermission(
  pluginId: string,
  permission: string,
  granted: boolean,
): Promise<void> {
  const next: GrantTable = { ...table, [pluginId]: { ...table[pluginId] } };

  next[pluginId][permission] = granted;
  table = next;
  notify();

  try {
    await SetValue(PLUGIN_GRANTS_CONFIG_KEY, JSON.stringify(table));
  } catch (error) {
    // 落盘失败不回滚内存态：本次运行内开关仍然生效，只是重启后回到旧值
    console.error("[plugins] 保存权限开关失败：", error);
  }
}

/** clearPluginGrants 卸载插件时清掉它的授权记录（避免残留条数无限增长） */
export async function clearPluginGrants(pluginId: string): Promise<void> {
  if (!table[pluginId]) return;
  const next: GrantTable = { ...table };

  delete next[pluginId];
  table = next;
  notify();

  try {
    await SetValue(PLUGIN_GRANTS_CONFIG_KEY, JSON.stringify(table));
  } catch (error) {
    console.error("[plugins] 清理权限开关失败：", error);
  }
}

/** hydratePluginGrants 启动时读一次授权表（须在 loadPlugins 之前完成） */
export async function hydratePluginGrants(): Promise<void> {
  try {
    const raw = await GetValue(PLUGIN_GRANTS_CONFIG_KEY);

    if (!raw) return;
    const parsed: unknown = JSON.parse(raw);

    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      console.warn(t("[plugins] 权限开关配置格式不对，已按默认值处理"));

      return;
    }
    const next: GrantTable = {};

    for (const [pluginId, value] of Object.entries(
      parsed as Record<string, unknown>,
    )) {
      if (!value || typeof value !== "object" || Array.isArray(value)) continue;
      const entry: Record<string, boolean> = {};

      for (const [permission, granted] of Object.entries(
        value as Record<string, unknown>,
      )) {
        if (typeof granted === "boolean") entry[permission] = granted;
      }
      if (Object.keys(entry).length > 0) next[pluginId] = entry;
    }
    table = next;
    notify();
  } catch (error) {
    // 读不到 / 解析失败都按"全部走默认值"处理：默认值本身是安全的
    console.error("[plugins] 读取权限开关失败，已按默认值处理：", error);
  }
}

/**
 * usePluginGrantsVersion 订阅授权表变更的版本号。
 * 插件页每个权限一行 Switch，用版本号触发整页重渲染即可，不必逐权限订阅。
 */
export function usePluginGrantsVersion(): number {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);

      return () => listeners.delete(listener);
    },
    () => version,
    () => version,
  );
}
