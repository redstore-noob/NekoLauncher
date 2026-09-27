/*
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

/**
 * 实例内容更新检测（X-4）的前端纯逻辑。
 *
 * 为什么单独成模块：这些判定决定了界面上「有没有更新」这四个字怎么写，
 * 写错的代价是让用户以为自己的 Mod 是最新的（或反过来，被引导去覆盖文件）。
 * 抽成不依赖 React 的纯函数后可以用 vitest 直接锁住语义：
 *
 *  - unknown（Modrinth 查不到）**不等于** latest（已是最新）——文案必须区分，
 *    否则自建包 / CurseForge 独占的 Mod 会被谎报成"最新"；
 *  - checkFailed（网络/服务端失败）也**不等于** latest；
 *  - 只有 status === "updatable" 才认为可以更新。
 */

import type { download } from "../../wailsjs/go/models";

/** 检测结果里一个条目的状态（与 Go 侧 ContentUpdateStatus* 常量一一对应）。 */
export type UpdateStatus =
  | "updatable"
  | "latest"
  | "unknown"
  | "checkFailed"
  | "skipped";

/** 列表里的标记语义：none 表示不显示角标。 */
export type UpdateBadgeTone = "update" | "unknown" | "failed" | "none";

export const UPDATE_STATUS_UPDATABLE = "updatable";
export const UPDATE_STATUS_LATEST = "latest";
export const UPDATE_STATUS_UNKNOWN = "unknown";
export const UPDATE_STATUS_CHECK_FAILED = "checkFailed";
export const UPDATE_STATUS_SKIPPED = "skipped";

/**
 * 按文件路径建立索引，便于列表渲染时 O(1) 查一条目的检测结果。
 * 路径大小写不敏感（Windows 上同一个文件可能以不同大小写出现）。
 */
export function indexUpdateFiles(
  files?: download.ContentUpdateFile[] | null,
): Map<string, download.ContentUpdateFile> {
  const index = new Map<string, download.ContentUpdateFile>();

  for (const file of files ?? []) {
    const key = normalizePathKey(file?.FilePath);

    if (key) index.set(key, file);
  }

  return index;
}

/** 路径归一化：统一分隔符 + 小写，用于比较。 */
export function normalizePathKey(path?: string | null): string {
  return (path ?? "").trim().replace(/\\/g, "/").toLowerCase();
}

/**
 * 默认占位符替换（与 i18n 的 interpolate 同语义）。
 * 组件里会把全局 t 传进来（拿译文），测试与非组件场景用原文 + 替换。
 */
function interpolate(
  source: string,
  params?: Record<string, string | number>,
): string {
  if (!params) return source;
  let result = source;

  for (const [key, value] of Object.entries(params)) {
    result = result.split(`{${key}}`).join(String(value));
  }

  return result;
}

/** 以某个条目的磁盘路径取它的检测结果（找不到返回 null）。 */
export function findUpdateFor(
  index: Map<string, download.ContentUpdateFile>,
  sourcePath?: string | null,
): download.ContentUpdateFile | null {
  const key = normalizePathKey(sourcePath);

  if (!key) return null;

  return index.get(key) ?? null;
}

/** 是否可更新（唯一判据：后端明确给了 updatable）。 */
export function isUpdatable(
  entry?: download.ContentUpdateFile | null,
): boolean {
  return entry?.Status === UPDATE_STATUS_UPDATABLE;
}

/**
 * 列表角标语义。
 * 注意："查不到"不是"没有更新"，它要给一个中性的「未知」标记，
 * 而不是什么都不显示（什么都不显示 = 用户以为没问题）。
 */
export function badgeToneFor(
  entry?: download.ContentUpdateFile | null,
): UpdateBadgeTone {
  switch (entry?.Status) {
    case UPDATE_STATUS_UPDATABLE:
      return "update";
    case UPDATE_STATUS_UNKNOWN:
      return "unknown";
    case UPDATE_STATUS_CHECK_FAILED:
      return "failed";
    default:
      return "none";
  }
}

/** 是否值得在列表里显示这个条目的检测结论（skipped/未检测过不显示）。 */
export function shouldShowBadge(
  entry?: download.ContentUpdateFile | null,
): boolean {
  return badgeToneFor(entry) !== "none";
}

/** 版本展示："1.0.0 → 1.1.0"；缺当前版本时不编造。 */
export function versionTransition(
  entry?: download.ContentUpdateFile | null,
): string {
  if (!entry) return "";
  const current = (entry.CurrentVersion ?? "").trim();
  const latest = (entry.LatestVersion ?? "").trim();

  if (current && latest) return `${current} → ${latest}`;
  if (latest) return latest;

  return "";
}

/**
 * 一次性摘要文本（工具栏上用）。
 *
 * 每个计数都必须来自后端的真实计数：摘要里出现的数字与列表里的角标数量对不上，
 * 用户就会开始怀疑角标。
 */
export function summarizeResult(
  result?: download.ContentUpdateCheckResult | null,
  translate: (
    source: string,
    params?: Record<string, string | number>,
  ) => string = interpolate,
): string {
  if (!result) return "";

  const parts: string[] = [
    translate("可更新 {0} 个", { "0": result.UpdatableCount ?? 0 }),
    translate("已是最新 {0} 个", { "0": result.LatestCount ?? 0 }),
  ];

  if ((result.UnknownCount ?? 0) > 0) {
    // 「未知」必须出现在摘要里：它不是「已是最新」，不能悄悄合并进上一项
    parts.push(translate("未知 {0} 个", { "0": result.UnknownCount }));
  }
  if ((result.FailedCount ?? 0) > 0) {
    parts.push(translate("检查失败 {0} 个", { "0": result.FailedCount }));
  }
  if ((result.SkippedCount ?? 0) > 0) {
    parts.push(translate("跳过 {0} 个", { "0": result.SkippedCount }));
  }

  return `${parts.join(translate("，"))}${translate("。")}`;
}

/**
 * 结果里的整合包清单比对是否"有问题"（缺失/被改动）。
 * 用于把「实例内容有问题」在 UI 上单独提示出来——它比"有更新"更需要用户注意。
 */
export function hasModpackIssues(
  result?: download.ContentUpdateCheckResult | null,
): boolean {
  return (
    result?.Modpack?.Status === "issues" &&
    ((result.Modpack.MissingCount ?? 0) > 0 ||
      (result.Modpack.ModifiedCount ?? 0) > 0)
  );
}

/** 整合包清单比对的展示文案（已是最优信息，不再额外编造）。 */
export function modpackSummary(
  result?: download.ContentUpdateCheckResult | null,
): string {
  const modpack = result?.Modpack;

  if (!modpack) return "";
  if (modpack.StatusText) return modpack.StatusText;

  return "";
}

/**
 * 覆盖式替换前的风险提示文本。
 * 一键替换是本功能里唯一会动用户文件的操作，文案必须把"会备份"与"备份到哪"
 * 说清楚；这里只负责「确认前」的提示，实际备份路径由后端返回后再展示。
 */
export function replaceConfirmMessage(
  entry: download.ContentUpdateFile,
): string {
  const target = entry?.FileName ?? "";
  const latest = (entry?.LatestVersion ?? "").trim();

  if (latest) {
    return `将把 ${target} 替换为 ${latest}，原文件会先备份为 ${target}.bak-<时间戳>。继续吗？`;
  }

  return `将把 ${target} 替换为新版本，原文件会先备份为 ${target}.bak-<时间戳>。继续吗？`;
}

/**
 * 另存新版本时的建议文件名：保留原扩展名，补上版本号，
 * 不覆盖用户原有的同名文件（由保存对话框决定最终路径）。
 */
export function suggestedFileName(entry: download.ContentUpdateFile): string {
  const original = (entry?.FileName ?? "").trim() || "content.jar";
  const dot = original.lastIndexOf(".");
  const base = dot > 0 ? original.slice(0, dot) : original;
  const extension = dot > 0 ? original.slice(dot) : "";
  const version = (entry?.LatestVersion ?? "").trim();
  const suffix = version ? `-${version}` : "-new";

  return `${base}${suffix}${extension}`;
}

/** Modrinth 项目页地址（用于「打开下载页」；没有项目 ID 时返回空串）。 */
export function projectPageURL(
  entry?: download.ContentUpdateFile | null,
): string {
  const projectID = (entry?.ProjectID ?? "").trim();

  if (!projectID) return "";

  return `https://modrinth.com/project/${encodeURIComponent(projectID)}`;
}

/** 可直接打开的下载地址（没有则返回空串）。 */
export function downloadLink(
  entry?: download.ContentUpdateFile | null,
): string {
  return (entry?.DownloadURL ?? "").trim();
}
