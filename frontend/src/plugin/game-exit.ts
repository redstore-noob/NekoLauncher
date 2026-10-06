/*
 * 启动终态的归一（onGameExit 的载荷）。
 *
 * 宿主只在 launch:changed 上发快照，没有单独的"游戏退出"事件；而一次启动的终态
 * 有两种：Phase 3（准备/启动阶段就失败，游戏压根没跑起来）与 Phase 4（进程退出，
 * 含正常退出 / 手动停止 / 崩溃）。崩溃判定直接复用启动器自己的 lib/crashNotice：
 * 同一份快照，插件与宿主弹窗不能得出两个结论。
 */
import type { launch } from "../../wailsjs/go/models";
import type { GameExitInfo } from "./types";

import { detectCrash } from "../lib/crashNotice";

/** GameLaunchPhase：0 空闲 / 1 准备中 / 2 运行中 / 3 失败 / 4 已退出 */
const PHASE_FAILED = 3;
const PHASE_EXITED = 4;

/**
 * gameExitInfo 把一次快照折成插件看到的退出信息；还没到终态时返回 null
 * （调用方什么都不用做）。
 */
export function gameExitInfo(
  snapshot: launch.GameLaunchSnapshot | null | undefined,
): GameExitInfo | null {
  const phase = Number(snapshot?.Phase ?? 0);

  if (phase !== PHASE_FAILED && phase !== PHASE_EXITED) return null;

  // detectCrash 只认 Phase 4：Phase 3 天然给出 { crashed:false, exitCode:null }
  const { crashed, exitCode } = detectCrash(
    (snapshot ?? {}) as launch.GameLaunchSnapshot,
  );

  return {
    versionId: String(snapshot?.VersionId ?? ""),
    phase: phase === PHASE_FAILED ? "failed" : "exited",
    exitCode,
    crashed,
    stoppedManually: !!snapshot?.StoppedManually,
    message: String(snapshot?.Message || snapshot?.Title || ""),
  };
}

/** gameExitRevision 快照版本号（去重用）：同一 Revision 只该回调一次 */
export function gameExitRevision(
  snapshot: launch.GameLaunchSnapshot | null | undefined,
): number {
  return Number(snapshot?.Revision ?? 0);
}
