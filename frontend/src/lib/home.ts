/*
 * Copyright 2024 Next UI
 * Copyright 2026 烟花
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
// 主页小组件共用的格式化与解析工具：时长、相对时间、MOTD 渲染、服务器地址解析。
import { createElement, type ReactNode } from "react";

import { t } from "../i18n";

/** 秒数 → 人话时长：大于 1 小时显示 "X.X 小时"，否则 "X 分钟" */
export function formatPlaytime(seconds: number): string {
  if (seconds <= 0) return t("0 分钟");
  if (seconds < 3600)
    return t("{0} 分钟", { "0": Math.max(1, Math.round(seconds / 60)) });

  return t("{0} 小时", { "0": (seconds / 3600).toFixed(1) });
}

/** Unix 秒 → 相对时间文本（无记录时回落 emptyText） */
function relativeFromUnix(unixSeconds: number, emptyText: string): string {
  if (!Number.isFinite(unixSeconds) || unixSeconds <= 0) return emptyText;
  const diff = Date.now() / 1000 - unixSeconds;

  if (diff < 60) return t("刚刚");
  if (diff < 3600) return t("{0} 分钟前", { "0": Math.floor(diff / 60) });
  if (diff < 86400) return t("{0} 小时前", { "0": Math.floor(diff / 3600) });
  if (diff < 86400 * 30)
    return t("{0} 天前", { "0": Math.floor(diff / 86400) });

  return new Date(unixSeconds * 1000).toLocaleDateString();
}

/** 游玩统计的 LastPlayedAt（Unix 秒）→ 相对时间 */
export function formatRelativeTime(unixSeconds: number): string {
  return relativeFromUnix(unixSeconds, t("从未游玩"));
}

/** 存档的 LastPlayed（后端 time.Time，JSON 字符串）→ 相对时间 */
export function formatRelativeTimeFrom(
  value: unknown,
  emptyText = t("时间未知"),
): string {
  const epochMs = toEpochMs(value);

  if (epochMs === null) return emptyText;

  return relativeFromUnix(epochMs / 1000, emptyText);
}

/** 后端时间 → epoch 毫秒；Go 零值时间（0001-01-01）与无法解析的输入返回 null */
function toEpochMs(value: unknown): number | null {
  if (value instanceof Date) {
    return Number.isNaN(value.getTime()) ? null : value.getTime();
  }
  if (typeof value !== "string" || value.trim() === "") return null;
  const parsed = Date.parse(value);

  if (Number.isNaN(parsed)) return null;

  return new Date(parsed).getFullYear() < 2000 ? null : parsed;
}

/** 去掉 MOTD 里的 § 样式码（§a §l 等），只留可读文本 */
export function stripMinecraftFormatting(text: string): string {
  return String(text ?? "").replace(/§./g, "");
}

/*
 * 彩色 MOTD 渲染（参考 Modrinth App 的 motd-parser autoToHTML 行为）：
 * 把 § 样式码流转译为带颜色的 React 节点，不经过 innerHTML，天然免疫 XSS。
 */

/** Minecraft Java 版 16 色码 → 官方默认色板 */
const MC_COLOR_CODES: Record<string, string> = {
  "0": "#000000",
  "1": "#0000AA",
  "2": "#00AA00",
  "3": "#00AAAA",
  "4": "#AA0000",
  "5": "#AA00AA",
  "6": "#FFAA00",
  "7": "#AAAAAA",
  "8": "#555555",
  "9": "#5555FF",
  a: "#55FF55",
  b: "#55FFFF",
  c: "#FF5555",
  d: "#FF55FF",
  e: "#FFFF55",
  f: "#FFFFFF",
};

/** 一段连续同样式文本的样式状态 */
interface MotdStyleState {
  color: string | null;
  bold: boolean;
  italic: boolean;
  underlined: boolean;
  strikethrough: boolean;
  obfuscated: boolean;
}

const PLAIN_MOTD_STYLE: MotdStyleState = {
  color: null,
  bold: false,
  italic: false,
  underlined: false,
  strikethrough: false,
  obfuscated: false,
};

function motdStyleKey(style: MotdStyleState): string {
  return [
    style.color ?? "-",
    style.bold ? "b" : "",
    style.italic ? "i" : "",
    style.underlined ? "u" : "",
    style.strikethrough ? "s" : "",
    style.obfuscated ? "k" : "",
  ].join("|");
}

function isHexDigit(char: string): boolean {
  return /^[0-9a-fA-F]$/.test(char);
}

/**
 * 尝试从 text[i] 开始读取 Bungee/Spigot 的 hex 色序列（§x§a§b§c§d§e§f）。
 * 匹配返回小写 "#rrggbb" 并给出结束下标；不匹配返回 null。
 */
function tryParseHexColor(
  text: string,
  start: number,
): { color: string; next: number } | null {
  // text[start] 已是 '§' 且 text[start+1] 已是 'x'
  let index = start + 2;
  let digits = "";

  for (let step = 0; step < 6; step++) {
    if (text[index] !== "§") return null;
    const digit = text[index + 1];

    if (!isHexDigit(digit)) return null;
    digits += digit;
    index += 2;
  }

  return { color: `#${digits.toLowerCase()}`, next: index };
}

/**
 * 把含 § 样式码的 MOTD 文本渲染为彩色节点数组。
 * 颜色码（§a 等）按原版语义重置装饰样式；§r 重置全部；§x 为 hex 颜色前缀。
 * 未识别的样式码原样丢弃；换行符保留（容器用 whitespace-pre-line 展示）。
 */
export function renderMinecraftFormatting(text: string): ReactNode[] {
  const source = String(text ?? "");
  const segments: ReactNode[] = [];
  let style = { ...PLAIN_MOTD_STYLE };
  let buffer = "";
  let bufferKey = motdStyleKey(style);
  let sequence = 0;

  const flush = () => {
    if (!buffer) return;
    const decorations: string[] = [];

    if (style.underlined) decorations.push("underline");
    if (style.strikethrough) decorations.push("line-through");
    segments.push(
      createElement(
        "span",
        {
          key: `${sequence++}-${bufferKey}`,
          style: {
            ...(style.color ? { color: style.color } : null),
            ...(style.bold ? { fontWeight: 700 } : null),
            ...(style.italic ? { fontStyle: "italic" } : null),
            ...(decorations.length
              ? { textDecorationLine: decorations.join(" ") }
              : null),
          },
          className: style.obfuscated ? "nya-mc-obfuscated" : undefined,
        },
        buffer,
      ),
    );
    buffer = "";
  };

  for (let i = 0; i < source.length; i++) {
    if (source[i] !== "§" || i + 1 >= source.length) {
      buffer += source[i];
      continue;
    }
    const code = source[i + 1].toLowerCase();
    let nextStyle: MotdStyleState | null = null;
    let skip = 2;

    if (code === "x") {
      // hex 色序列；不完整时按未知码丢弃
      const hex = tryParseHexColor(source, i);

      if (hex) {
        nextStyle = { ...PLAIN_MOTD_STYLE, color: hex.color };
        skip = hex.next - i;
      }
    } else if (MC_COLOR_CODES[code]) {
      // 颜色码按原版语义重置装饰
      nextStyle = { ...PLAIN_MOTD_STYLE, color: MC_COLOR_CODES[code] };
    } else if (code === "k") {
      nextStyle = { ...style, obfuscated: true };
    } else if (code === "l") {
      nextStyle = { ...style, bold: true };
    } else if (code === "o") {
      nextStyle = { ...style, italic: true };
    } else if (code === "n") {
      nextStyle = { ...style, underlined: true };
    } else if (code === "m") {
      nextStyle = { ...style, strikethrough: true };
    } else if (code === "r") {
      nextStyle = { ...PLAIN_MOTD_STYLE };
    }

    if (!nextStyle) continue; // 未知码：连同 § 一起丢弃
    const nextKey = motdStyleKey(nextStyle);

    if (nextKey !== bufferKey) {
      flush();
      bufferKey = nextKey;
    }
    style = nextStyle;
    i += skip - 1;
  }
  flush();

  return segments;
}

/** 本地文件路径 → 应用内 /localfile 中转 URL（WebView 无法直接读盘符路径） */
export function localFileUrl(path: string): string {
  return path ? `/localfile?path=${encodeURIComponent(path)}` : "";
}

/** launcher.yaml 中保存的主页小组件顺序（JSON 字符串数组） */
export const HOME_WIDGET_LAYOUT_KEY = "homeWidgetLayout";

/** launcher.yaml 中保存的主页小组件列数（1~3，缺省 1） */
export const HOME_WIDGET_COLUMNS_KEY = "homeWidgetColumns";
/** 小组件列数变化事件名（外观设置修改后广播，主页实时跟随） */
export const WIDGET_COLUMNS_EVENT = "nya:widgetColumns";

/** 解析小组件列数配置；非法值返回 null（调用方回落 1 列） */
export function parseWidgetColumns(raw: string): number | null {
  const value = Number(raw);

  if (!Number.isInteger(value) || value < 1 || value > 3) return null;

  return value;
}

/** 广播小组件列数变化（detail 为新列数） */
export function emitWidgetColumns(columns: number): void {
  window.dispatchEvent(
    new CustomEvent(WIDGET_COLUMNS_EVENT, { detail: columns }),
  );
}

/**
 * 把平铺的小组件 id 切成 columns 个尽量均衡的连续纵排（列主序）。
 * 每列各自渲染、独立滚动；1 列时原样返回，保持既有布局语义。
 */
export function splitWidgetColumns(ids: string[], columns: number): string[][] {
  const count = Math.max(1, Math.min(3, Math.floor(columns) || 1));

  if (count === 1) return [ids];

  const base = Math.floor(ids.length / count);
  const remainder = ids.length % count;
  const result: string[][] = [];
  let start = 0;

  for (let column = 0; column < count; column += 1) {
    const size = base + (column < remainder ? 1 : 0);

    result.push(ids.slice(start, start + size));
    start += size;
  }

  return result;
}

/**
 * (列, 列内位置) → 平铺数组插入下标。
 * 列与位置越界时收敛到合法范围（拖动落点可能来自旧的一帧）。
 */
export function columnInsertionIndex(
  slices: string[][],
  column: number,
  index: number,
): number {
  if (slices.length === 0) return 0;
  const targetColumn = Math.max(0, Math.min(column, slices.length - 1));
  let flat = 0;

  for (let i = 0; i < targetColumn; i += 1) flat += slices[i].length;
  const within = Math.max(
    0,
    Math.min(index, slices[targetColumn]?.length ?? 0),
  );

  return flat + within;
}

/**
 * 已下线小组件 id → 替代组件 id 的迁移表。
 * 旧 id 会留在用户的 launcher.yaml 布局里，读取时替换为替代者；
 * 「服务器公告」(motd) 的功能已并入「快速进服」(quickjoin)。
 */
const WIDGET_ID_MIGRATIONS: Record<string, string> = {
  motd: "quickjoin",
};

/**
 * 迁移布局里已下线的组件 id，并去掉迁移产生的重复位。
 * 未列入迁移表的 id（如被禁用插件的组件）原样保留。
 */
export function migrateWidgetIds(ids: string[]): string[] {
  const seen = new Set<string>();
  const migrated: string[] = [];

  for (const id of ids) {
    const target = WIDGET_ID_MIGRATIONS[id] ?? id;

    if (seen.has(target)) continue;
    seen.add(target);
    migrated.push(target);
  }

  return migrated;
}

/** 解析小组件布局配置；缺失或格式错误返回 null（调用方回落默认布局） */
export function parseWidgetLayout(raw: string): string[] | null {
  if (!raw) return null;
  try {
    const parsed: unknown = JSON.parse(raw);

    if (!Array.isArray(parsed)) return null;

    return parsed.filter((id): id is string => typeof id === "string");
  } catch {
    return null;
  }
}

/** MB → 人话内存占用：小于 1 GB 显示 MB */
export function formatMemoryMb(megabytes: number): string {
  if (!Number.isFinite(megabytes) || megabytes <= 0) return "0 MB";
  if (megabytes < 1024) return `${Math.round(megabytes)} MB`;

  return `${(megabytes / 1024).toFixed(1)} GB`;
}

/** 字节 → 人话大小（B/KB/MB/GB/TB），保留一位小数 */
export function formatBytes(bytes?: number | null): string {
  const amount = bytes ?? 0;

  if (!Number.isFinite(amount) || amount <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let index = 0;
  let value = amount;

  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index++;
  }

  return `${value.toFixed(1)} ${units[index]}`;
}

/** 账号类型标识 → 中文文案（与账户管理页一致） */
export function accountTypeLabel(type: string): string {
  switch (type) {
    case "microsoft":
      return t("正版");
    case "offline":
      return t("离线");
    case "authlib":
      return t("皮肤站");
    default:
      return t("第三方");
  }
}

/** 各类异常（Error / Wails 抛出的字符串 / 其他）→ 可展示的错误文本 */
export function errorMessage(err: unknown): string {
  if (!err) return "";
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;

  return String(err);
}
