/*
 * 下载任务状态的归一。
 *
 * 宿主内部有两套互不相干的下载状态：游戏本体安装是 GameDownloadService 的单条
 * 状态机快照（download.GameDownloadSnapshot），内容资源 / 整合包 / Java 运行时
 * 是内容任务注册表（download.ContentTaskSnapshot 列表）。插件侧只需要一个列表，
 * 这里把两者折成同一形状，取值语义与右下角下载中心保持一致（Phase 3 完成 /
 * 4 失败 / 5 取消）。
 */
import type { download } from "../../wailsjs/go/models";
import type { DownloadTaskKind, DownloadTaskSummary } from "./types";

/** 游戏本体快照阶段：0 空闲 / 1 准备 / 2 下载 / 3 完成 / 4 失败 / 5 取消 */
const GAME_PHASE_PREPARING = 1;
const GAME_PHASE_DOWNLOADING = 2;
const GAME_PHASE_COMPLETED = 3;
const GAME_PHASE_CANCELLED = 5;

/** 内容任务阶段：1 活跃 / 3 完成 / 4 失败 / 5 取消 */
const CONTENT_PHASE_ACTIVE = 1;
const CONTENT_PHASE_COMPLETED = 3;
const CONTENT_PHASE_CANCELLED = 5;

/** 游戏本体任务在列表里的固定 id（同一时刻至多一条）。 */
export const GAME_DOWNLOAD_TASK_ID = "game";

function clampPercent(value: number): number {
  if (!isFinite(value)) return 0;

  return Math.max(0, Math.min(100, value));
}

/** 游戏本体快照 → 插件任务摘要；空闲或尚无任务时返回 null。 */
export function gameDownloadTask(
  snapshot: download.GameDownloadSnapshot | null | undefined,
): DownloadTaskSummary | null {
  if (!snapshot || !snapshot.VersionID) return null;

  const phase = Number(snapshot.Phase ?? 0);

  if (phase < GAME_PHASE_PREPARING) return null;

  const active =
    phase === GAME_PHASE_PREPARING || phase === GAME_PHASE_DOWNLOADING;
  const percentage = clampPercent(Number(snapshot.Percentage ?? 0));
  const totalBytes = Number(snapshot.TotalBytes ?? 0);
  const downloadedBytes = Number(snapshot.CompletedBytes ?? 0);
  const bytesPerSecond = Number(snapshot.BytesPerSecond ?? 0);
  const remaining = Math.max(0, totalBytes - downloadedBytes);
  const etaSeconds =
    active && remaining > 0 && bytesPerSecond > 1
      ? remaining / bytesPerSecond
      : null;
  const detail = [snapshot.StageName, snapshot.Detail]
    .filter(Boolean)
    .join(" · ");

  return {
    id: GAME_DOWNLOAD_TASK_ID,
    name: snapshot.VersionID || "Minecraft",
    kind: "game",
    phase: active
      ? "downloading"
      : phase === GAME_PHASE_COMPLETED
        ? "completed"
        : phase === GAME_PHASE_CANCELLED
          ? "cancelled"
          : "failed",
    isActive: active,
    isFinished: !active,
    isCompleted: phase === GAME_PHASE_COMPLETED,
    percent: percentage,
    indeterminate: active && totalBytes <= 0,
    downloadedBytes,
    totalBytes,
    bytesPerSecond: active ? bytesPerSecond : 0,
    etaSeconds,
    detail,
  };
}

/** contentTaskKind 收敛任务类别：未知类别按单文件内容资源处理。 */
function contentTaskKind(kind: string): DownloadTaskKind {
  return kind === "modpack" || kind === "java" ? kind : "content";
}

/** 内容任务快照 → 插件任务摘要。 */
export function contentDownloadTask(
  task: download.ContentTaskSnapshot,
): DownloadTaskSummary {
  const phase = Number(task.phase ?? 0);
  const active = phase === CONTENT_PHASE_ACTIVE;
  const totalBytes = Number(task.totalBytes ?? 0);
  const downloadedBytes = Number(task.downloadedBytes ?? 0);
  const etaSeconds = Number(task.etaSeconds ?? -1);

  return {
    id: task.id ?? "",
    name: task.name ?? "",
    kind: contentTaskKind(String(task.kind ?? "")),
    phase: active
      ? "downloading"
      : phase === CONTENT_PHASE_COMPLETED
        ? "completed"
        : phase === CONTENT_PHASE_CANCELLED
          ? "cancelled"
          : "failed",
    isActive: active,
    isFinished: !active,
    isCompleted: phase === CONTENT_PHASE_COMPLETED,
    percent:
      totalBytes > 0 ? clampPercent((downloadedBytes * 100) / totalBytes) : 0,
    indeterminate: active && totalBytes <= 0,
    downloadedBytes,
    totalBytes,
    bytesPerSecond: active ? Number(task.bytesPerSecond ?? 0) : 0,
    etaSeconds: active && etaSeconds >= 0 ? etaSeconds : null,
    detail: task.detail ?? "",
  };
}

/**
 * 全部下载任务：游戏本体在前，其后是内容任务（活跃在前、终态殿后，与下载中心
 * 的排序口径一致）。
 */
export function downloadTasksFromSnapshots(
  game: download.GameDownloadSnapshot | null | undefined,
  contentTasks: download.ContentTaskSnapshot[] | null | undefined,
): DownloadTaskSummary[] {
  const gameTask = gameDownloadTask(game);

  return [
    ...(gameTask ? [gameTask] : []),
    ...(contentTasks ?? []).map(contentDownloadTask),
  ];
}

/** watchDownloadTasks 的句柄：宿主事件 -> notify，卸载 -> cancel。 */
export interface DownloadTaskWatcher {
  /** 宿主事件触发时调用（同步返回，重读与投递在后台进行）；已取消后是空操作 */
  notify: () => void;
  /** 取消：丢弃在途结果，此后不再触发任何回调 */
  cancel: () => void;
}

/**
 * 下载任务变更节流器：宿主的游戏本体进度事件与内容任务列表事件都很密集
 * （进度 100~250ms、列表 200ms，两路还会交错），这里把它们折成"最多一轮读取
 * 在途 + 一次补跑"，每轮都重新读一份完整列表再交给 handler——列表本身就是权威
 * 状态，逐条增量通知对插件没有意义，而投递半截状态（只有内容任务、缺游戏本体）
 * 反而会让插件显示错误结果。
 *
 * 读取失败按"这次没有变化"处理（readTasks 内部已对两路各自兜底），下次事件会再试；
 * handler 抛错只吃掉这一次，不打断后续轮次。
 */
export function watchDownloadTasks(
  handler: (tasks: DownloadTaskSummary[]) => void,
  readTasks: () => Promise<DownloadTaskSummary[]>,
): DownloadTaskWatcher {
  let cancelled = false;
  let reading = false;
  let pending = false;

  const drain = async () => {
    reading = true;
    try {
      do {
        pending = false;
        const tasks = await readTasks();

        if (cancelled) return;
        try {
          handler(tasks);
        } catch (error) {
          console.error("[plugins] onDownloadTasksChanged 回调抛错：", error);
        }
      } while (pending && !cancelled);
    } catch {
      // 读取失败：这一轮不投递，等宿主的下一个事件
    } finally {
      reading = false;
    }
  };

  return {
    notify: () => {
      if (cancelled) return;
      if (reading) {
        pending = true;

        return;
      }
      void drain();
    },
    cancel: () => {
      cancelled = true;
      pending = false;
    },
  };
}
