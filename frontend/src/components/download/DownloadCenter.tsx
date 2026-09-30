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
/*
 * 下载中心（右下角浮窗，类 KDE 通知样式）：所有下载任务统一在这里展示——
 *  - Minecraft 本体安装（download:progress 快照 → 一条"game"任务）；
 *  - 内容下载（Mod / 资源包 / 整合包 / Java，download:contentTasks 任务列表，
 *    由 Go 侧内容任务注册表聚合推送，支持逐任务取消）。
 * 弹层内的进度条（内容下载弹层 / 资源搜索弹层）保持不变，浮窗负责全局视角。
 */
import type { download } from "../../../wailsjs/go/models";

import React, { useEffect, useRef, useState } from "react";
import { Progress } from "@heroui/react";
import {
  CheckmarkCircle20Regular,
  Dismiss20Regular,
  Pause20Regular,
  Play20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";
import { AnimatePresence, motion } from "framer-motion";

import { indicatorVariants } from "../../lib/motion";
import {
  CancelContentTask,
  CancelDownload,
  GetCurrentDownloadSnapshot,
  GetContentTasks,
  IsDownloadPaused,
  PauseDownload,
  ResumeDownload,
} from "../../../wailsjs/go/bindings/DownloadAPI";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { t } from "../../i18n";

// GameDownloadPhase：0 空闲 / 1 准备 / 2 下载 / 3 完成 / 4 失败 / 5 取消
const PHASE_PREPARING = 1;
const PHASE_DOWNLOADING = 2;
const PHASE_COMPLETED = 3;

// download.ContentTaskPhase
const TASK_COMPLETED = 3;
const TASK_FAILED = 4;
const TASK_CANCELLED = 5;

function formatBytes(n?: number | null): string {
  if (!n) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let i = 0;
  let v = n;

  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }

  return `${v.toFixed(1)} ${units[i]}`;
}

/** 剩余时间格式化：秒 → "0:42" / "3:20" / "1:05:30"；null = 无法估算 */
function formatEta(seconds: number | null): string | null {
  if (seconds === null || !isFinite(seconds) || seconds < 0) return null;
  const total = Math.max(0, Math.round(seconds));
  const hh = Math.floor(total / 3600);
  const mm = String(Math.floor((total % 3600) / 60)).padStart(2, "0");
  const ss = String(total % 60).padStart(2, "0");

  return hh > 0 ? `${hh}:${mm}:${ss}` : `${mm}:${ss}`;
}

/** 下载中心里的一行任务 */
interface CenterTask {
  key: string;
  name: string;
  detail: string;
  percent: number;
  indeterminate: boolean;
  phase: "active" | "completed" | "failed" | "cancelled";
  /** 预计剩余秒数；null = 无法估算 */
  eta: number | null;
  /** game 任务独有：暂停 / 取消走全局下载状态机 */
  isGame?: boolean;
}

function taskFromGameSnapshot(
  snap: download.GameDownloadSnapshot | null,
): CenterTask | null {
  if (!snap || !snap.VersionID) return null;
  const phase = snap.Phase ?? 0;

  if (phase < PHASE_PREPARING) return null;

  const active = phase === PHASE_PREPARING || phase === PHASE_DOWNLOADING;
  const remaining = Math.max(
    0,
    (snap.TotalBytes ?? 0) - (snap.CompletedBytes ?? 0),
  );
  const eta =
    active && remaining > 0 && (snap.BytesPerSecond ?? 0) > 1
      ? remaining / snap.BytesPerSecond
      : null;
  const detail = [
    snap.StageName,
    snap.Detail,
    active
      ? [
          `${formatBytes(snap.CompletedBytes)}/${formatBytes(snap.TotalBytes)}`,
          `${formatBytes(snap.BytesPerSecond)}/s`,
          eta ? t("剩余 {0}", { "0": formatEta(eta) ?? "" }) : "",
        ]
          .filter(Boolean)
          .join(" · ")
      : "",
  ]
    .filter(Boolean)
    .join(" · ");

  return {
    key: "game",
    name:
      (snap.VersionID || "Minecraft") +
      (active
        ? phase === PHASE_PREPARING
          ? t(" · 准备中")
          : t(" · 下载中")
        : ""),
    detail,
    percent: Math.max(0, Math.min(100, snap.Percentage ?? 0)),
    indeterminate: active && (snap.Percentage ?? 0) <= 0,
    eta,
    phase: active
      ? "active"
      : phase === PHASE_COMPLETED
        ? "completed"
        : phase === 5
          ? "cancelled"
          : "failed",
    isGame: true,
  };
}

const DownloadCenter: React.FC<{
  /** 点击任务名等处时切换到下载页（保留与旧浮标一致的入口语义） */
  onOpenDownloads?: () => void;
}> = () => {
  const [contentTasks, setContentTasks] = useState<
    download.ContentTaskSnapshot[]
  >([]);
  const [gameSnap, setGameSnap] =
    useState<download.GameDownloadSnapshot | null>(null);
  const [gamePaused, setGamePaused] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const dismissedRef = useRef(false);

  dismissedRef.current = dismissed;

  useEffect(() => {
    void (async () => {
      try {
        setGameSnap(await GetCurrentDownloadSnapshot());
      } catch {
        /* 忽略 */
      }
      try {
        setGamePaused(!!(await IsDownloadPaused()));
      } catch {
        /* 忽略 */
      }
      try {
        setContentTasks((await GetContentTasks()) ?? []);
      } catch {
        /* 忽略 */
      }
    })();
    // 只退订自己注册的回调：EventsOff(事件名) 会清掉该事件的全部监听，
    // 把主页下载卡片 / 下载页的订阅一起干掉。
    const offGame = EventsOn(
      "download:progress",
      (s: download.GameDownloadSnapshot) => {
        setGameSnap(s);
        setDismissed(false);
      },
    );
    const offPaused = EventsOn("download:pauseChanged", (p: boolean) =>
      setGamePaused(!!p),
    );
    const offContent = EventsOn(
      "download:contentTasks",
      (tasks: download.ContentTaskSnapshot[]) => {
        setContentTasks(tasks ?? []);
        setDismissed(false);
      },
    );

    return () => {
      offGame();
      offPaused();
      offContent();
    };
  }, []);

  const gameTask = taskFromGameSnapshot(gameSnap);
  const tasks: CenterTask[] = [
    ...(gameTask ? [gameTask] : []),
    ...contentTasks.map((task) => {
      const active = task.phase === 1;
      const detail = [
        task.detail,
        active
          ? [
              `${formatBytes(task.downloadedBytes)}${task.totalBytes > 0 ? `/${formatBytes(task.totalBytes)}` : ""}`,
              task.bytesPerSecond > 0
                ? `${formatBytes(task.bytesPerSecond)}/s`
                : "",
              task.etaSeconds >= 0
                ? t("剩余 {0}", { "0": formatEta(task.etaSeconds) ?? "" })
                : "",
            ]
              .filter(Boolean)
              .join(" · ")
          : "",
      ]
        .filter(Boolean)
        .join(" · ");

      return {
        key: task.id,
        name: task.name,
        detail,
        percent:
          task.totalBytes > 0
            ? Math.max(
                0,
                Math.min(100, (task.downloadedBytes * 100) / task.totalBytes),
              )
            : 0,
        indeterminate: active && task.totalBytes <= 0,
        eta: task.etaSeconds >= 0 ? task.etaSeconds : null,
        phase:
          task.phase === TASK_COMPLETED
            ? "completed"
            : task.phase === TASK_FAILED
              ? "failed"
              : task.phase === TASK_CANCELLED
                ? "cancelled"
                : "active",
      } as CenterTask;
    }),
  ];

  // 活跃任务排前面，终态殿后
  tasks.sort((a, b) =>
    a.phase === "active" ? (b.phase === "active" ? 0 : -1) : 0,
  );

  const anyActive = tasks.some((task) => task.phase === "active");
  const visible = tasks.length > 0 && (anyActive || !dismissedRef.current);

  const togglePause = async () => {
    try {
      if (gamePaused) await ResumeDownload();
      else await PauseDownload();
    } catch {
      /* 状态变更失败不阻断界面 */
    }
  };

  return (
    <AnimatePresence>
      {visible ? (
        <motion.div
          animate="center"
          className="fixed right-4 bottom-4 z-40 w-[340px] max-w-[calc(100vw-2rem)] overflow-hidden rounded-2xl border nya-border nya-panel-strong shadow-lg backdrop-blur-md"
          exit="exit"
          initial="enter"
          variants={indicatorVariants}
        >
          <div className="flex items-center gap-2 px-4 pt-2.5 pb-1.5">
            <span className="min-w-0 flex-1 truncate text-[12px] font-semibold text-gray-500 dark:text-gray-400">
              {t("下载")}
              {anyActive ? t(" · 下载中") : ""}
            </span>
            <button
              aria-label={t("关闭")}
              className="flex size-6 flex-none items-center justify-center rounded-md text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800 dark:hover:text-gray-200"
              title={t("关闭")}
              type="button"
              onClick={() => setDismissed(true)}
            >
              <Dismiss20Regular />
            </button>
          </div>

          <div className="nya-scroll max-h-[40vh] overflow-y-auto px-4 pb-3">
            {tasks.map((task) => (
              <div
                key={task.key}
                className="border-b border-default-200/60 py-2 last:border-b-0 dark:border-default-100/10"
              >
                <div className="flex items-center gap-1.5">
                  {task.phase === "completed" ? (
                    <span className="flex-none text-success-500">
                      <CheckmarkCircle20Regular />
                    </span>
                  ) : task.phase === "failed" ? (
                    <span className="flex-none text-danger">
                      <Warning20Regular />
                    </span>
                  ) : null}
                  <span
                    className={`min-w-0 flex-1 truncate text-[13px] font-semibold ${
                      task.phase === "failed"
                        ? "text-danger"
                        : "text-gray-800 dark:text-gray-200"
                    }`}
                    title={task.name}
                  >
                    {task.name}
                  </span>
                  {task.phase === "active" ? (
                    task.isGame ? (
                      <>
                        <button
                          aria-label={
                            gamePaused ? t("继续下载") : t("暂停下载")
                          }
                          className="flex size-6 flex-none items-center justify-center rounded-md text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800 dark:hover:text-gray-200"
                          title={gamePaused ? t("继续下载") : t("暂停下载")}
                          type="button"
                          onClick={() => void togglePause()}
                        >
                          {gamePaused ? <Play20Regular /> : <Pause20Regular />}
                        </button>
                        <button
                          aria-label={t("取消")}
                          className="flex size-6 flex-none items-center justify-center rounded-md text-gray-400 hover:bg-gray-100 hover:text-danger dark:hover:bg-gray-800"
                          title={t("取消")}
                          type="button"
                          onClick={() => void CancelDownload()}
                        >
                          <Dismiss20Regular />
                        </button>
                      </>
                    ) : (
                      <button
                        aria-label={t("取消")}
                        className="flex size-6 flex-none items-center justify-center rounded-md text-gray-400 hover:bg-gray-100 hover:text-danger dark:hover:bg-gray-800"
                        title={t("取消")}
                        type="button"
                        onClick={() => void CancelContentTask(task.key)}
                      >
                        <Dismiss20Regular />
                      </button>
                    )
                  ) : null}
                </div>
                {task.detail ? (
                  <div className="truncate text-[11px] text-gray-400">
                    {task.detail}
                  </div>
                ) : null}
                {task.phase === "active" ? (
                  <Progress
                    aria-label={t("下载进度")}
                    className="mt-1"
                    color={task.isGame && gamePaused ? "warning" : "primary"}
                    isIndeterminate={task.indeterminate}
                    size="sm"
                    value={task.percent}
                  />
                ) : null}
              </div>
            ))}
          </div>
        </motion.div>
      ) : null}
    </AnimatePresence>
  );
};

export default DownloadCenter;
