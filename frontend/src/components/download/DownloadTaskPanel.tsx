/*
 * 下载任务面板（下载大厅页内）：与右下角下载中心同源的实时任务视图。
 *
 * 下载页此前只有一条"任务已在右下角下载中心运行"的摘要文案——进度、速度、
 * 失败原因都要去浮窗看。这里把全部任务（游戏本体 / 内容资源 / Java）直接铺进
 * 页面：每条任务带进度条、字节数 / 速度 / 剩余时间、暂停（仅游戏本体）与取消，
 * 终态任务可一键清除；游戏本体完成时提供「打开文件夹」。
 * 数据来源与 DownloadCenter 完全一致（download:progress / download:contentTasks
 * 事件 + 挂载时读一次快照），两个视图互不影响。
 */
import type { download } from "../../../wailsjs/go/models";

import React, { useEffect, useMemo, useState } from "react";
import { Progress } from "@heroui/react";
import {
  CheckmarkCircle20Regular,
  Delete20Regular,
  Dismiss20Regular,
  FolderOpen20Regular,
  Pause20Regular,
  Play20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";

import {
  CancelContentTask,
  CancelDownload,
  GetCurrentDownloadSnapshot,
  GetContentTasks,
  IsDownloadPaused,
  PauseDownload,
  ResumeDownload,
} from "../../../wailsjs/go/bindings/DownloadAPI";
import { GetGameDirectory } from "../../../wailsjs/go/bindings/ConfigAPI";
import { OpenInExplorer } from "../../../wailsjs/go/bindings/SystemAPI";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { t } from "../../i18n";

// GameDownloadPhase：0 空闲 / 1 准备 / 2 下载 / 3 完成 / 4 失败 / 5 取消
const GAME_PHASE_PREPARING = 1;
const GAME_PHASE_DOWNLOADING = 2;
const GAME_PHASE_COMPLETED = 3;

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
    i += 1;
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

type TaskPhase = "active" | "completed" | "failed" | "cancelled";

interface PanelTask {
  key: string;
  name: string;
  detail: string;
  percent: number;
  indeterminate: boolean;
  phase: TaskPhase;
  bytes: string;
  speed: string;
  eta: string | null;
  /** 游戏本体任务：暂停 / 取消走全局下载状态机 */
  isGame?: boolean;
  group: "game" | "content" | "java";
}

const GROUP_ORDER: PanelTask["group"][] = ["game", "content", "java"];
const GROUP_LABELS: Record<PanelTask["group"], string> = {
  game: t("游戏本体"),
  content: t("内容资源"),
  java: "Java",
};

function taskFromGameSnapshot(
  snap: download.GameDownloadSnapshot | null,
): PanelTask | null {
  if (!snap || !snap.VersionID) return null;
  const phase = snap.Phase ?? 0;

  if (phase < GAME_PHASE_PREPARING) return null;
  const active =
    phase === GAME_PHASE_PREPARING || phase === GAME_PHASE_DOWNLOADING;
  const remaining = Math.max(
    0,
    (snap.TotalBytes ?? 0) - (snap.CompletedBytes ?? 0),
  );
  const etaSeconds =
    active && remaining > 0 && (snap.BytesPerSecond ?? 0) > 1
      ? remaining / snap.BytesPerSecond
      : null;
  const detail = [snap.StageName, snap.Detail].filter(Boolean).join(" · ");

  return {
    key: "game",
    name: snap.VersionID || "Minecraft",
    detail,
    percent: Math.max(0, Math.min(100, snap.Percentage ?? 0)),
    indeterminate: active && (snap.Percentage ?? 0) <= 0,
    phase: active
      ? "active"
      : phase === GAME_PHASE_COMPLETED
        ? "completed"
        : phase === 5
          ? "cancelled"
          : "failed",
    bytes: `${formatBytes(snap.CompletedBytes)}/${formatBytes(snap.TotalBytes)}`,
    speed: active ? `${formatBytes(snap.BytesPerSecond)}/s` : "",
    eta: formatEta(etaSeconds),
    isGame: true,
    group: "game",
  };
}

/** 打开已完成游戏本体的版本目录（不存在时退回游戏根目录） */
async function openGameFolder(versionId: string): Promise<void> {
  try {
    const gameDir = (await GetGameDirectory()) || "";
    const join = (dir: string, name: string) =>
      dir ? dir.replace(/[\\/]+$/, "") + "/" + name : name;
    const candidates = [
      join(join(gameDir, "versions"), versionId),
      gameDir,
    ].filter(Boolean);

    for (const dir of candidates) {
      try {
        await OpenInExplorer(dir);

        return;
      } catch {
        /* 尝试下一个 */
      }
    }
  } catch (ex) {
    console.error(t("打开下载目录失败"), ex);
  }
}

const DownloadTaskPanel: React.FC = () => {
  const [gameSnap, setGameSnap] =
    useState<download.GameDownloadSnapshot | null>(null);
  const [gamePaused, setGamePaused] = useState(false);
  const [contentTasks, setContentTasks] = useState<
    download.ContentTaskSnapshot[]
  >([]);
  // 本地清掉的终态任务 id（后端延迟修剪终态，前端先藏起来）
  const [clearedIds, setClearedIds] = useState<string[]>([]);

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
    // 只退订自己注册的回调：EventsOff(事件名) 会清掉该事件的全部监听
    const offGame = EventsOn(
      "download:progress",
      (s: download.GameDownloadSnapshot) => setGameSnap(s),
    );
    const offPaused = EventsOn("download:pauseChanged", (p: boolean) =>
      setGamePaused(!!p),
    );
    const offContent = EventsOn(
      "download:contentTasks",
      (tasks: download.ContentTaskSnapshot[]) => setContentTasks(tasks ?? []),
    );

    return () => {
      offGame();
      offPaused();
      offContent();
    };
  }, []);

  const gameTask = taskFromGameSnapshot(gameSnap);
  const tasks: PanelTask[] = useMemo(
    () => [
      ...(gameTask ? [gameTask] : []),
      ...contentTasks
        .filter((task) => !clearedIds.includes(task.id))
        .map((task) => {
          const active = task.phase === 1;
          const phase: TaskPhase =
            task.phase === TASK_COMPLETED
              ? "completed"
              : task.phase === TASK_FAILED
                ? "failed"
                : task.phase === TASK_CANCELLED
                  ? "cancelled"
                  : "active";

          return {
            key: task.id,
            name: task.name,
            detail: task.detail,
            percent:
              task.totalBytes > 0
                ? Math.max(
                    0,
                    Math.min(
                      100,
                      (task.downloadedBytes * 100) / task.totalBytes,
                    ),
                  )
                : 0,
            indeterminate: active && task.totalBytes <= 0,
            phase,
            bytes:
              task.totalBytes > 0
                ? `${formatBytes(task.downloadedBytes)}/${formatBytes(task.totalBytes)}`
                : formatBytes(task.downloadedBytes),
            speed:
              active && task.bytesPerSecond > 0
                ? `${formatBytes(task.bytesPerSecond)}/s`
                : "",
            eta:
              active && task.etaSeconds >= 0
                ? formatEta(task.etaSeconds)
                : null,
            group:
              task.kind === "java" ? ("java" as const) : ("content" as const),
          } satisfies PanelTask;
        }),
    ],
    [gameTask, contentTasks, clearedIds],
  );

  const anyActive = tasks.some((task) => task.phase === "active");
  const activeCount = tasks.filter((task) => task.phase === "active").length;
  // 聚合速度：全部进行中任务的字节数/秒之和（游戏本体快照 + 内容任务注册表）
  const totalSpeed =
    gameSnap && gameTask?.phase === "active"
      ? (gameSnap.BytesPerSecond ?? 0)
      : 0;
  const aggregateSpeed =
    totalSpeed +
    contentTasks
      .filter((task) => task.phase === 1)
      .reduce((sum, task) => sum + (task.bytesPerSecond ?? 0), 0);
  const totalPercent =
    activeCount > 0
      ? tasks
          .filter((task) => task.phase === "active")
          .reduce((sum, task) => sum + task.percent, 0) / activeCount
      : 0;

  const hasFinished = tasks.some((task) => task.phase !== "active");

  // 没有任何任务（含本地清空的终态）时整块不占位
  if (tasks.length === 0) return null;

  const togglePause = async () => {
    try {
      if (gamePaused) await ResumeDownload();
      else await PauseDownload();
    } catch {
      /* 状态变更失败不阻断界面 */
    }
  };

  const clearFinished = () => {
    setClearedIds(
      tasks
        .filter((task) => !task.isGame && task.phase !== "active")
        .map((task) => task.key),
    );
  };

  return (
    <div className="nya-border nya-panel flex flex-none flex-col gap-1 rounded-large px-4 py-3">
      {/* 头部：进行中数量 / 聚合速度 / 清除已完成 */}
      <div className="flex flex-none items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-[12px] font-semibold text-gray-500 dark:text-gray-400">
          {anyActive
            ? t("下载中 · {0} 个任务 · {1}/s", {
                "0": String(activeCount),
                "1": formatBytes(aggregateSpeed),
              })
            : t("暂无进行中的任务")}
        </span>
        {hasFinished ? (
          <button
            aria-label={t("清除已完成")}
            className="flex size-6 flex-none items-center justify-center rounded-md text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800 dark:hover:text-gray-200"
            title={t("清除已完成")}
            type="button"
            onClick={clearFinished}
          >
            <Delete20Regular />
          </button>
        ) : null}
      </div>
      {anyActive ? (
        <Progress
          aria-label={t("总进度")}
          color={gamePaused ? "warning" : "primary"}
          isIndeterminate={tasks
            .filter((task) => task.phase === "active")
            .every((task) => task.indeterminate)}
          size="sm"
          value={totalPercent}
        />
      ) : null}

      {GROUP_ORDER.map((group) => {
        const groupTasks = tasks
          .filter((task) => task.group === group)
          .sort((a, b) =>
            a.phase === "active" ? (b.phase === "active" ? 0 : -1) : 0,
          );

        if (groupTasks.length === 0) return null;

        return (
          <div key={group} className="flex flex-col">
            {/* 分组标题 */}
            <div className="flex items-center gap-1.5 pt-1.5 pb-0.5">
              <span className="text-[10px] font-semibold tracking-wide text-gray-400 uppercase">
                {GROUP_LABELS[group]}
              </span>
              <span className="flex-1 border-t border-default-200/60 dark:border-default-100/10" />
              <span className="text-[10px] text-gray-400">
                {groupTasks.length}
              </span>
            </div>
            {groupTasks.map((task) => (
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
                  {/* 终态徽记 */}
                  {task.phase === "completed" ? (
                    <span className="flex-none text-[10px] text-success-500">
                      {t("已完成")}
                    </span>
                  ) : task.phase === "failed" ? (
                    <span className="flex-none text-[10px] text-danger">
                      {t("失败")}
                    </span>
                  ) : task.phase === "cancelled" ? (
                    <span className="flex-none text-[10px] text-gray-400">
                      {t("已取消")}
                    </span>
                  ) : null}
                  {task.phase === "active" ? (
                    <span className="flex-none text-[11px] text-gray-400 tabular-nums">
                      {Math.round(task.percent)}%
                    </span>
                  ) : null}
                  {/* 游戏本体完成：直达版本目录 */}
                  {task.isGame && task.phase === "completed" ? (
                    <button
                      aria-label={t("打开文件夹")}
                      className="flex size-6 flex-none items-center justify-center rounded-md text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800 dark:hover:text-gray-200"
                      title={t("打开文件夹")}
                      type="button"
                      onClick={() => void openGameFolder(task.name)}
                    >
                      <FolderOpen20Regular />
                    </button>
                  ) : null}
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
                {/* 说明行：阶段 / 字节 / 速度 / 剩余时间 */}
                {task.detail || task.bytes || task.speed || task.eta ? (
                  <div className="truncate text-[11px] text-gray-400 tabular-nums">
                    {[
                      task.detail,
                      task.phase === "active" ? task.bytes : "",
                      task.speed,
                      task.eta ? t("剩余 {0}", { "0": task.eta }) : "",
                    ]
                      .filter(Boolean)
                      .join(" · ")}
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
        );
      })}
    </div>
  );
};

export default DownloadTaskPanel;
