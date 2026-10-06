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
/*
 * 启动参数溯源面板：回答「这次启动为什么长这样」。
 *
 * 后端（internal/launch/launch_provenance.go）在装配命令行时旁路记账，产出
 * 一份 LaunchProvenanceReport：每条参数都说得清是谁加的。本面板只负责把它
 * 排版成人能看懂的样子——Kind / Key 都是稳定标识，文案全部走词典。
 *
 * 三个视角：
 *   1. 头部：这次的 Java、主类、工作目录（"用的到底是不是我配的那个 Java"）；
 *   2. 正文：按 JVM / 主类 / 游戏 三段列出参数，每条带来源说明；
 *   3. 冲突：同前缀参数的"谁压过了谁"，被覆盖的那条在列表里划线变暗。
 *
 * 报告为 null（从未成功启动过）或条目为空时给友好空态，不报错——溯源只是
 * 解释层，任何异常都不该影响用户启动游戏。
 */
import type { launch } from "../../../wailsjs/go/models";

import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import {
  Button,
  Chip,
  Modal,
  ModalContent,
  Spinner,
  Switch,
} from "@heroui/react";
import {
  ArrowClockwise20Regular as RefreshIcon,
  Branch20Regular,
  CheckmarkCircle20Regular,
  Info20Regular,
  SlideText20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";

import {
  GetLaunchProvenance,
  GetLaunchProvenanceVersionId,
} from "../../../wailsjs/go/bindings/LauncherAPI";
import { ModalShell, modalBehaviorProps } from "../modal-shell";
import EmptyState from "../empty-state";
import {
  describeConflict,
  describeSourceWithPlugin,
  entriesForView,
  groupBySection,
  isLongArgument,
  summarizeReport,
  truncateArgument,
} from "../../lib/launchProvenance";
import { t } from "../../i18n";

interface LaunchProvenancePanelProps {
  isOpen: boolean;
  onClose: () => void;
}

/** 头部一行「标签 / 值」；值可复制，长路径换行不撑破布局。 */
const InfoRow: React.FC<{ label: string; value: string }> = ({
  label,
  value,
}) => (
  <div className="grid grid-cols-[92px_minmax(0,1fr)] items-baseline gap-2">
    <span className="text-[11px] text-gray-400">{label}</span>
    <span
      className="nya-scroll overflow-x-auto font-mono text-[11px] break-all text-gray-700 dark:text-gray-200"
      title={value || undefined}
    >
      {value || "—"}
    </span>
  </div>
);

/**
 * 单条参数。
 * - 被覆盖（Shadowed）：整行变暗 + 参数划线 + 「已被覆盖」徽章；
 * - 长参数（类路径等）：截断显示，点一下展开/收起，title 里始终有完整值。
 */
const ArgumentRow: React.FC<{
  entry: launch.LaunchArgumentEntry;
  /** 指明这条参数压过哪一条（仅冲突里的赢家） */
  beats?: string;
}> = ({ entry, beats }) => {
  const [expanded, setExpanded] = useState(false);
  const shadowed = !!entry?.Shadowed;
  const full = entry?.Argument ?? "";
  const long = isLongArgument(full);
  const shown = long && !expanded ? truncateArgument(full) : full;

  return (
    <div
      className={`
        flex items-start gap-2 rounded-lg px-2.5 py-1.5 transition-colors
        ${
          shadowed
            ? "bg-default-100/40 opacity-60"
            : "bg-default-100/40 hover:bg-primary/10"
        }
      `}
    >
      {/* 序号：与后端 Index 一致，方便和冲突行里的下标对上 */}
      <span className="w-6 flex-none pt-0.5 text-right text-[10px] tabular-nums text-gray-400">
        {entry?.Index}
      </span>

      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <button
          className={`
            nya-scroll w-full overflow-x-auto whitespace-pre text-left font-mono text-[11px] leading-relaxed
            ${shadowed ? "text-gray-400 line-through" : "text-gray-800 dark:text-gray-100"}
            ${long ? "cursor-pointer" : "cursor-default"}
          `}
          disabled={!long}
          title={
            long
              ? t("点击{0}完整参数", {
                  "0": expanded ? t("收起") : t("展开"),
                })
              : full
          }
          type="button"
          onClick={() => {
            if (long) setExpanded((value) => !value);
          }}
        >
          {shown || "\u00A0"}
        </button>

        {/* 来源说明：谁加的、为什么加 + 插件 id */}
        <div className="flex flex-wrap items-center gap-1.5 text-[10px] text-gray-400">
          <span className="min-w-0 truncate">
            {describeSourceWithPlugin(entry?.Source)}
          </span>
          {long ? (
            <span className="flex-none text-primary/70">
              {expanded ? t("点击收起") : t("点击展开完整参数")}
            </span>
          ) : null}
          {beats ? (
            <span className="flex-none text-gray-400">
              {t("压过了「{0}」", { "0": beats })}
            </span>
          ) : null}
        </div>
      </div>

      {shadowed ? (
        <Chip className="flex-none" color="warning" size="sm" variant="flat">
          {t("已被覆盖")}
        </Chip>
      ) : null}
    </div>
  );
};

const LaunchProvenancePanel: React.FC<LaunchProvenancePanelProps> = ({
  isOpen,
  onClose,
}) => {
  const [report, setReport] = useState<launch.LaunchProvenanceReport | null>(
    null,
  );
  const [versionID, setVersionID] = useState("");
  const [loading, setLoading] = useState(false);
  /** 只有「本次打开确实拉过」才允许显示空态，避免打开瞬间闪一下"没有记录" */
  const [loaded, setLoaded] = useState(false);
  const [showAll, setShowAll] = useState(true);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      // 两个绑定分开 catch：版本号拿不到不该让整份报告显示不出来
      const [next, id] = await Promise.all([
        GetLaunchProvenance().catch(() => null),
        GetLaunchProvenanceVersionId().catch(() => ""),
      ]);

      setReport(next ?? null);
      setVersionID(id ?? "");
    } finally {
      setLoading(false);
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    if (!isOpen) return;
    // 每次打开都重新拉：用户可能刚改过设置又启动了一次
    setLoaded(false);
    void reload();
  }, [isOpen, reload]);

  const summary = useMemo(() => summarizeReport(report), [report]);
  const entries = useMemo(
    () => entriesForView(report, showAll),
    [report, showAll],
  );
  const groups = useMemo(() => groupBySection(entries), [entries]);

  /**
   * 「赢家下标 → 它压过的参数文本」。
   * 冲突行已经单独列出，这里让列表里的赢家自己说一句"我压过了谁"，
   * 用户不用来回对照两张表。
   */
  const beatsByIndex = useMemo(() => {
    const map = new Map<number, string>();
    const byIndex = new Map<number, launch.LaunchArgumentEntry>();

    for (const entry of report?.Entries ?? []) byIndex.set(entry.Index, entry);
    for (const conflict of report?.Conflicts ?? []) {
      const losers = (conflict?.LoserIndices ?? [])
        .map((index) => byIndex.get(index)?.Argument ?? "")
        .filter(Boolean);

      if (losers.length > 0) map.set(conflict.WinnerIndex, losers.join("、"));
    }

    return map;
  }, [report]);

  const hasConflicts = summary.conflictCount > 0;
  const showEmpty = loaded && !loading && entries.length === 0;

  return (
    <Modal isOpen={isOpen} size="3xl" onClose={onClose} {...modalBehaviorProps}>
      <ModalContent className="max-h-[88vh]">
        <ModalShell
          icon={<Branch20Regular />}
          subtitle={
            report
              ? t("本次启动 · 共 {0} 条参数", { "0": summary.totalCount })
              : t("本次启动")
          }
          title={t("启动参数溯源")}
          onClose={onClose}
        >
          <div className="flex flex-col gap-3">
            {/* ---- 摘要行：有冲突就醒目告警，没有就平静报平安 ---- */}
            {report ? (
              hasConflicts ? (
                <div className="flex items-start gap-2 rounded-lg bg-warning-500/15 px-3 py-2 text-warning-600 dark:text-warning-400">
                  <span className="mt-px flex-none">
                    <Warning20Regular className="h-4 w-4" />
                  </span>
                  <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="text-[12px] font-semibold">
                      {t("有 {0} 组参数被后面的同名参数覆盖", {
                        "0": summary.conflictCount,
                      })}
                    </span>
                    <span className="text-[11px] leading-snug">
                      {t(
                        "共 {0} 条参数没有生效（列表中已划线标出）。JVM 取最后一次出现的值，被覆盖的那条不会起作用。",
                        { "0": summary.overriddenCount },
                      )}
                    </span>
                  </span>
                </div>
              ) : (
                <div className="flex items-center gap-2 rounded-lg bg-success/10 px-3 py-2 text-success-600 dark:text-success-400">
                  <span className="flex-none">
                    <CheckmarkCircle20Regular className="h-4 w-4" />
                  </span>
                  <span className="text-[11px] leading-snug">
                    {t("没有参数互相覆盖，{0} 条参数全部生效。", {
                      "0": summary.effectiveCount,
                    })}
                  </span>
                </div>
              )
            ) : null}

            {/* ---- 头部：这次用的是哪个实例 / Java / 主类 / 工作目录 ---- */}
            {report ? (
              <div className="flex flex-col gap-1.5 rounded-lg border nya-border bg-default-100/40 px-3 py-2.5">
                <div className="mb-0.5 text-[10px] font-semibold tracking-wide text-gray-400 uppercase">
                  {t("本次启动环境")}
                </div>
                {versionID ? (
                  <InfoRow label={t("实例 / 版本")} value={versionID} />
                ) : null}
                <InfoRow
                  label={t("Java 可执行文件")}
                  value={report.JavaExecutable}
                />
                <InfoRow
                  label={t("工作目录")}
                  value={report.WorkingDirectory}
                />
              </div>
            ) : null}

            {/* ---- 冲突明细 ---- */}
            {hasConflicts ? (
              <div className="flex flex-col gap-1.5">
                <div className="flex items-center gap-1.5 text-[11px] font-semibold text-gray-600 dark:text-gray-300">
                  <Branch20Regular className="h-3.5 w-3.5" />
                  {t("互相覆盖的参数")}
                </div>
                {(report?.Conflicts ?? []).map((conflict) => (
                  <div
                    key={`${conflict.Prefix}-${conflict.WinnerIndex}`}
                    className="flex items-start gap-2 rounded-lg border nya-border bg-default-100/40 px-2.5 py-2"
                  >
                    <span className="flex-none pt-0.5 text-warning-600 dark:text-warning-400">
                      <Info20Regular className="h-3.5 w-3.5" />
                    </span>
                    <span className="min-w-0 flex-1 text-[11px] leading-snug text-gray-600 dark:text-gray-300">
                      {describeConflict(conflict)}
                    </span>
                  </div>
                ))}
              </div>
            ) : null}

            {/* ---- 工具行：显示范围切换 + 刷新 ---- */}
            <div className="flex flex-wrap items-center gap-3">
              <Switch
                aria-label={t("显示全部参数")}
                isSelected={showAll}
                size="sm"
                onValueChange={setShowAll}
              />
              <span className="text-[11px] text-gray-500 dark:text-gray-400">
                {showAll
                  ? t("显示全部参数（含被覆盖的）")
                  : t("只显示生效的参数")}
              </span>
              <span className="ml-auto flex items-center gap-2">
                <span className="text-[11px] text-gray-400 tabular-nums">
                  {t("{0} / {1} 条", {
                    "0": entries.length,
                    "1": summary.totalCount,
                  })}
                </span>
                <Button
                  isIconOnly
                  aria-label={t("刷新")}
                  isLoading={loading}
                  size="sm"
                  title={t("刷新")}
                  variant="flat"
                  onPress={() => void reload()}
                >
                  {loading ? undefined : <RefreshIcon />}
                </Button>
              </span>
            </div>

            {/* ---- 正文：三态共用固定高度，弹窗打开后尺寸稳定不跳动 ---- */}
            {loading && !loaded ? (
              <div className="flex h-[46vh] items-center justify-center gap-2 text-sm text-gray-400">
                <Spinner size="sm" /> {t("正在读取启动参数…")}
              </div>
            ) : showEmpty ? (
              <div className="flex h-[46vh] flex-col items-center justify-center">
                <EmptyState
                  icon={<SlideText20Regular className="h-8 w-8" />}
                  text={
                    report
                      ? t("这次启动没有记录到任何参数")
                      : t("还没有成功启动过游戏，暂时没有可以回溯的参数")
                  }
                />
                {!report ? (
                  <div className="max-w-md text-center text-[11px] leading-relaxed text-gray-400">
                    {t(
                      "启动参数溯源会在游戏成功启动后记录。先启动一次游戏，再回到这里查看每个参数是谁加的。",
                    )}
                  </div>
                ) : null}
              </div>
            ) : (
              <div className="nya-scroll flex h-[46vh] flex-col gap-3 overflow-y-auto pr-1">
                {groups.map((group) => (
                  <div key={group.section} className="flex flex-col gap-1">
                    <div className="flex flex-wrap items-baseline gap-2 px-1">
                      <span className="text-[12px] font-semibold text-gray-700 dark:text-gray-200">
                        {group.label}
                      </span>
                      <span className="text-[10px] text-gray-400">
                        {group.hint}
                      </span>
                      <span className="ml-auto text-[10px] text-gray-400 tabular-nums">
                        {t("{0} 条", { "0": group.entries.length })}
                      </span>
                    </div>
                    {group.entries.map((entry) => (
                      <ArgumentRow
                        key={`${entry.Index}-${entry.Argument.slice(0, 24)}`}
                        beats={beatsByIndex.get(entry.Index)}
                        entry={entry}
                      />
                    ))}
                  </div>
                ))}
              </div>
            )}
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default LaunchProvenancePanel;

// ---- 全局单例：与 LogViewer 同一套约定 ----
// 实例详情页、运行日志卡片、崩溃提示都可能想打开溯源面板，各自维护一份
// 弹层状态会互相打架（同时开出两个）。统一挂一个 Provider，
// 调用方只用 useLaunchProvenance().open()。

interface LaunchProvenanceContextValue {
  open: () => void;
}

const LaunchProvenanceContext = createContext<LaunchProvenanceContextValue>({
  open: () => {
    /* Provider 未挂载时的空实现 */
  },
});

export function useLaunchProvenance(): LaunchProvenanceContextValue {
  return useContext(LaunchProvenanceContext);
}

/**
 * 模块级打开入口：供非 React 上下文（崩溃提示、事件回调）调用。
 * Provider 挂载时把真实的 open 注册进来；未挂载时是空实现。
 */
let openProvenanceBridge: (() => void) | null = null;

/** 打开启动参数溯源面板（组件外可用）。 */
export function openLaunchProvenance(): void {
  openProvenanceBridge?.();
}

export const LaunchProvenanceProvider: React.FC<{
  children: React.ReactNode;
}> = ({ children }) => {
  const [isOpen, setIsOpen] = useState(false);
  const open = useCallback(() => setIsOpen(true), []);
  const close = useCallback(() => setIsOpen(false), []);

  useEffect(() => {
    openProvenanceBridge = open;

    return () => {
      openProvenanceBridge = null;
    };
  }, [open]);

  return (
    <LaunchProvenanceContext.Provider value={{ open }}>
      {children}
      <LaunchProvenancePanel isOpen={isOpen} onClose={close} />
    </LaunchProvenanceContext.Provider>
  );
};
