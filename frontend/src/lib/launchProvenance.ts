/*
 * 启动参数溯源（launch provenance）的纯函数层。
 *
 * 后端在装配命令行时旁路记账，产出一份 LaunchProvenanceReport：每条参数都带着
 * 「是谁加的」（Source.Kind / Source.Key / Source.Detail）。Kind 与 Key 都是
 * **稳定标识**（不是给人看的文案），前端负责把它们翻成人类可读的说明——与
 * crash diagnosis「固定 Key + 单独字段」的约定一致。
 *
 * 为什么把映射放在 lib 而不是组件里：
 *   - 这些映射是纯数据 + 纯函数，可以在没有 DOM / HeroUI 的情况下直接测；
 *   - 未知 Key 的降级行为（显示原始 key，绝不崩）是最需要被测试盯住的一条；
 *   - 组件只负责排版，改文案不会碰到渲染逻辑。
 */
import type { launch } from "../../wailsjs/go/models";

import { t } from "../i18n";

/**
 * Source.Kind 的字面量集合（与 Go 侧 LaunchArgumentSourceKind 常量一一对应）。
 * 用 as const 数组而不是 enum：既能在运行时遍历校验，也能推出联合类型。
 */
export const SOURCE_KINDS = [
  "version-json",
  "launcher-auto",
  "global-settings",
  "instance-settings",
  "instance-or-global",
  "direct-connect",
  "authlib-injector",
  "plugin",
  "transform",
  "unknown",
] as const;

export type SourceKind = (typeof SOURCE_KINDS)[number];

/**
 * Kind → 中文原文（i18n key）。
 * 未列出的 Kind（后端将来新增、或数据被篡改）回退到 Kind 原始字符串，
 * 保证界面永远有东西可显示，不会出现空白标签。
 */
const KIND_LABELS: Record<string, string> = {
  "version-json": "版本文件自带",
  "launcher-auto": "启动器自动添加",
  "global-settings": "全局启动设置",
  "instance-settings": "实例独立设置",
  "instance-or-global": "实例或全局设置",
  "direct-connect": "直接进服",
  "authlib-injector": "外置登录注入",
  plugin: "插件添加",
  transform: "启动变换",
  unknown: "来源未知",
};

/**
 * Key → 中文原文（i18n key）。模板里的 {0} 由 Source.Detail 插值，
 * 约定：Detail 为空时直接去掉末尾的「（{0}）」括号片段。
 *
 * 这些 Key 由 internal/launch/argument_builder.go 直接产出，属于前后端契约：
 * 后端新增 Key 时这里没跟上，界面会显示原始 key（可读性下降但不会崩），
 * 这正是我们想要的降级方向。
 */
const KEY_TEMPLATES: Record<string, string> = {
  // ---- 内存 ----
  "memory.min.auto": "启动器自动设置的最小堆（{0} MiB）",
  "memory.max.instance": "实例独立设置的最大堆（{0} MiB）",
  "memory.max.global": "全局设置的最大堆（{0} MiB）",
  "memory.max.automatic": "启动器按系统内存自动计算的最大堆（{0} MiB）",
  // ---- JVM ----
  "jvm.tuning.g1": "启动器内置的 G1 垃圾回收调优参数",
  "jvm.user.instance": "实例的额外 JVM 参数",
  "jvm.user.global": "全局的额外 JVM 参数",
  "jvm.library-directory": "Forge / NeoForge 需要的库目录声明",
  "jvm.classpath": "启动器拼出的类路径",
  // ---- 版本文件 ----
  "version-json.logging": "版本文件声明的日志配置",
  "version-json.jvm": "版本文件自带的 JVM 参数",
  "version-json.game": "版本文件自带的游戏参数",
  "version-json.main-class": "版本文件声明的主类",
  "version-json.classpath-legacy": "旧版版本文件的库路径与类路径",
  // ---- 游戏参数 ----
  "game.user.instance": "实例的额外游戏参数",
  "game.user.global": "全局的额外游戏参数",
  // ---- 插件 ----
  "plugin.prepend-jvm": "插件前置的 JVM 参数",
  "plugin.append-jvm": "插件追加的 JVM 参数",
  "plugin.prepend-game": "插件前置的游戏参数",
  "plugin.append-game": "插件追加的游戏参数",
};

/** 模板末尾的可选插值片段：Detail 为空时连同括号一起去掉。 */
const OPTIONAL_DETAIL_SUFFIX = /（\{0\}[^）]*）$/;

/**
 * 把一条来源翻成人类可读的说明。
 *
 * 优先级：Key 的模板（可插值 Detail）> Kind 的大类标签 > 原始 Key。
 * 任何一步都不抛异常：溯源面板是"解释层"，绝不能因为一条意外数据整块崩掉。
 */
export function describeSource(source: launch.LaunchArgumentSource): string {
  const key = source?.Key ?? "";
  const kind = source?.Kind ?? "";
  const detail = source?.Detail ?? "";
  const template = key ? KEY_TEMPLATES[key] : undefined;

  if (template) {
    // Detail 为空时不留一个孤零零的括号：去掉可选片段，模板本身仍是完整句子
    return detail
      ? t(template, { "0": detail })
      : t(template.replace(OPTIONAL_DETAIL_SUFFIX, ""));
  }
  if (key) return key;

  return KIND_LABELS[kind] ? t(KIND_LABELS[kind]) : kind || t("来源未知");
}

/** Kind → 人类可读的大类标签（徽章用）。未知 Kind 原样返回。 */
export function describeKind(kind: string): string {
  return KIND_LABELS[kind] ? t(KIND_LABELS[kind]) : kind || t("来源未知");
}

/**
 * 插件来源优先展示插件 id：用户关心的是"哪个插件干的"，
 * 而不是"插件前置/追加"这种位置差异。
 */
export function describeSourceWithPlugin(
  source: launch.LaunchArgumentSource,
): string {
  const label = describeSource(source);
  const pluginID = source?.PluginID ?? "";

  return pluginID ? `${label} · ${pluginID}` : label;
}

/** Section 的字面量顺序：JVM → 主类 → 游戏参数（与命令行顺序一致）。 */
export const SECTION_ORDER = ["jvm", "main-class", "game"] as const;

export type SectionName = (typeof SECTION_ORDER)[number];

const SECTION_LABELS: Record<string, string> = {
  jvm: "JVM 参数",
  "main-class": "主类",
  game: "游戏参数",
};

/** Section → 人类可读的分组标题。未知 Section 原样返回。 */
export function describeSection(section: string): string {
  return SECTION_LABELS[section]
    ? t(SECTION_LABELS[section])
    : section || t("其他参数");
}

/** Section → 分组标题下的一句说明（告诉用户这段参数是给谁用的）。 */
const SECTION_HINTS: Record<string, string> = {
  jvm: "交给 Java 虚拟机，在游戏启动前生效",
  "main-class": "决定实际启动哪个入口类",
  game: "原样传给 Minecraft 本体",
};

export function describeSectionHint(section: string): string {
  return SECTION_HINTS[section] ? t(SECTION_HINTS[section]) : "";
}

/** 一个 Section 分组。 */
export interface ProvenanceGroup {
  section: string;
  label: string;
  hint: string;
  entries: launch.LaunchArgumentEntry[];
}

/**
 * 按 Section 分组，并保持后端给的顺序（命令行顺序）。
 *
 * 分组顺序用 SECTION_ORDER，未知 Section 排在最后（按出现顺序）——
 * 这样即使后端将来加了新区段，界面也只是多一段，不会整块错乱。
 */
export function groupBySection(
  entries: launch.LaunchArgumentEntry[] | null | undefined,
): ProvenanceGroup[] {
  const buckets = new Map<string, launch.LaunchArgumentEntry[]>();

  for (const entry of entries ?? []) {
    const section = entry?.Section || "unknown";
    const bucket = buckets.get(section);

    if (bucket) bucket.push(entry);
    else buckets.set(section, [entry]);
  }

  const ordered = [
    ...SECTION_ORDER.filter((section) => buckets.has(section)),
    ...[...buckets.keys()].filter(
      (section) => !(SECTION_ORDER as readonly string[]).includes(section),
    ),
  ];

  return ordered.map((section) => ({
    section,
    label: describeSection(section),
    hint: describeSectionHint(section),
    entries: buckets.get(section) ?? [],
  }));
}

/** 统计结果，供摘要行使用。 */
export interface ProvenanceSummary {
  conflictCount: number;
  overriddenCount: number;
  effectiveCount: number;
  totalCount: number;
}

export function summarizeReport(
  report: launch.LaunchProvenanceReport | null | undefined,
): ProvenanceSummary {
  const entries = report?.Entries ?? [];

  return {
    conflictCount: report?.Conflicts?.length ?? 0,
    // Overridden 是后端算好的派生视图；缺失时按 Shadowed 现算，保证老快照也能显示
    overriddenCount:
      report?.Overridden?.length ??
      entries.filter((entry) => entry?.Shadowed).length,
    effectiveCount: report?.Effective?.length ?? 0,
    totalCount: entries.length,
  };
}

/**
 * 冲突的一句话描述：「`-Xmx` 出现 3 次；来自 <赢家> 的那条生效，
 * 来自 <输家> 的被忽略。」
 * 后端已按前缀排序，冲突条数有限，直接展开成句子即可。
 */
export function describeConflict(
  conflict: launch.LaunchProvenanceConflict,
): string {
  const winner = describeSourceWithPlugin(conflict?.WinnerSource);
  const loser = describeSourceWithPlugin(conflict?.LoserSource);
  const times = (conflict?.LoserIndices?.length ?? 0) + 1;

  return t("「{0}」出现 {1} 次：来自「{2}」的那条生效，来自「{3}」的被忽略。", {
    "0": conflict?.Prefix ?? "",
    "1": times,
    "2": winner,
    "3": loser,
  });
}

/**
 * 参数文本是否长到需要截断。
 * 类路径（-cp 后那条）动辄几千字符，直接铺开会把弹窗撑爆。
 */
export const LONG_ARGUMENT_THRESHOLD = 96;

export function isLongArgument(argument: string): boolean {
  return (argument?.length ?? 0) > LONG_ARGUMENT_THRESHOLD;
}

/**
 * 截断展示用的短文本（保留尾部，因为类路径/路径的末尾信息量最大）。
 * 完整值仍由 title 属性与展开交互提供。
 */
export function truncateArgument(
  argument: string,
  limit = LONG_ARGUMENT_THRESHOLD,
): string {
  const text = argument ?? "";

  if (text.length <= limit) return text;
  const keep = Math.max(8, limit - 1);

  return `…${text.slice(text.length - keep)}`;
}

/**
 * 把报告拆成「显示全部」与「只显示生效项」两个视图。
 * 只显示生效项时，仍按 Section 分组（结构不变，只是少了几行）。
 */
export function entriesForView(
  report: launch.LaunchProvenanceReport | null | undefined,
  showAll: boolean,
): launch.LaunchArgumentEntry[] {
  if (!report) return [];

  return showAll ? (report.Entries ?? []) : (report.Effective ?? []);
}
