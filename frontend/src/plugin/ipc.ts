/*
 * 插件间消息总线：宿主代为投递的插件 ↔ 插件通信通道。
 *
 * 信任模型与权限系统一致（见 docs/guide/TRUST_MODEL.md 与 docs/guide/PERMISSIONS.md）：这不是沙箱——
 * 插件在同一 WebView 里本来就能摸到彼此，总线给的是**可预期的契约**：
 *  · 发送方身份由宿主注入（from 里的 id/name/version 来自清单，插件伪造不了）；
 *  · 负载必须 JSON 可序列化且 ≤ MAX_PAYLOAD_BYTES——每个接收者拿到的是深拷贝，
 *    互相改对象不会串；
 *  · 接收方处理器抛错只记日志，不中断投递，也不影响发送方；
 *  · 只有"当前活跃且 ipc 权限仍被授权"的插件能收到消息（判定回调由 api.ts 注入）。
 *
 * 订阅的登记与退订走 api.ts 的 registerCleanup：插件卸载/重载/停用时宿主自动退订，
 * 同时摘掉它的清单登记（ipc.plugins 发现列表里不再出现）。
 */
import type {
  PluginManifest,
  PluginMessage,
  PluginMessageSender,
} from "./types";

import { t } from "../i18n";

/** 消息类型名上限（类型名只当路由键用，不该承载内容） */
export const MAX_MESSAGE_TYPE_LENGTH = 64;

/** 单条消息负载上限：JSON 序列化后 256 KB（与 styles.inject 的上限对齐） */
export const MAX_PAYLOAD_BYTES = 256 * 1024;

interface BusSubscription {
  pluginId: string;
  handler: (message: PluginMessage) => void;
}

/** 活跃订阅（按订阅者插件 id 归属，卸载时整组摘除） */
const subscriptions = new Set<BusSubscription>();

/** 已登记清单的插件（ipc.plugins 发现列表的数据源；重载时覆盖自己的旧条目） */
const knownPlugins = new Map<string, PluginMessageSender>();

function senderOf(manifest: PluginManifest): PluginMessageSender {
  return { id: manifest.id, name: manifest.name, version: manifest.version };
}

/** registerIpcPlugin 登记插件清单（createPluginApi 时调用，ipc.plugins 据此返回列表） */
export function registerIpcPlugin(manifest: PluginManifest): void {
  knownPlugins.set(manifest.id, senderOf(manifest));
}

/** unregisterIpcPlugin 摘除插件的登记与全部订阅（api.ts 的清理路径调用） */
export function unregisterIpcPlugin(pluginId: string): void {
  knownPlugins.delete(pluginId);
  for (const entry of subscriptions) {
    if (entry.pluginId === pluginId) subscriptions.delete(entry);
  }
}

/** ipcPlugins 当前已登记的插件身份列表（发现：该给谁发消息） */
export function ipcPlugins(): PluginMessageSender[] {
  return [...knownPlugins.values()];
}

/** resetPluginBus 清空总线状态（登记与订阅全部丢弃）；仅测试用 */
export function resetPluginBus(): void {
  subscriptions.clear();
  knownPlugins.clear();
}

/** subscribePluginMessage 订阅插件消息；返回退订函数（api.ts 会挂到自动清理） */
export function subscribePluginMessage(
  pluginId: string,
  handler: (message: PluginMessage) => void,
): () => void {
  const entry: BusSubscription = { pluginId, handler };

  subscriptions.add(entry);

  return () => {
    subscriptions.delete(entry);
  };
}

/**
 * validatePluginMessage 校验并归一发送参数：类型名 1~64 字符、负载 JSON 可序列化
 * 且序列化后 ≤ 256 KB。不合法直接抛错（与权限系统"绝不静默失败"同一原则）。
 */
export function validatePluginMessage(
  type: string,
  payload: unknown,
): { type: string; payload: unknown } {
  const normalizedType = typeof type === "string" ? type.trim() : "";

  if (!normalizedType || normalizedType.length > MAX_MESSAGE_TYPE_LENGTH) {
    throw new Error(
      t("消息类型名须为 1~{0} 字符", { "0": MAX_MESSAGE_TYPE_LENGTH }),
    );
  }

  // JSON 化既是负载合法性校验（函数 / 循环引用过不了），也是大小测量：
  // 接收方拿到的负载由这份序列化重建，天然与发送方对象隔离。
  let serialized = "null";

  if (payload !== undefined) {
    try {
      serialized = JSON.stringify(payload);
    } catch {
      throw new Error(
        t("消息负载必须 JSON 可序列化（不能含函数 / 循环引用等）"),
      );
    }
  }
  if (serialized.length > MAX_PAYLOAD_BYTES) {
    throw new Error(
      t("消息负载超过上限（{0} KB）", { "0": MAX_PAYLOAD_BYTES / 1024 }),
    );
  }

  return { type: normalizedType, payload: JSON.parse(serialized) };
}

/**
 * deliverPluginMessage 向订阅者投递一条消息，返回成功送达的处理器数。
 * canReceive 由 api.ts 注入（活跃 + ipc 权限仍授权）；单个处理器抛错只记
 * 警告并继续——一个坏插件不该连累其它订阅者也收不到。
 */
export function deliverPluginMessage(
  sender: PluginMessageSender,
  to: string | null,
  type: string,
  payload: unknown,
  canReceive: (pluginId: string) => boolean,
): number {
  let delivered = 0;

  for (const entry of subscriptions) {
    if (to !== null && entry.pluginId !== to) continue;
    if (!canReceive(entry.pluginId)) continue;

    // 每个接收者一份独立深拷贝：负载已过 JSON 校验，这里重建一定成功
    const message: PluginMessage = {
      from: { ...sender },
      to,
      type,
      payload:
        payload === null || payload === undefined
          ? null
          : JSON.parse(JSON.stringify(payload)),
    };

    try {
      entry.handler(message);
      delivered += 1;
    } catch (error) {
      console.warn(
        t("[plugins] {0} 的消息处理器处理「{1}」时抛错：{2}", {
          "0": entry.pluginId,
          "1": type,
          "2": error instanceof Error ? error.message : String(error),
        }),
      );
    }
  }

  return delivered;
}
