/*
 * 启动终态的归一测试：插件拿到的"游戏退出"必须与宿主自己的崩溃弹窗同源
 * （都走 lib/crashNotice.detectCrash），且只在真正的终态触发一次。
 */
import type { launch } from "../../wailsjs/go/models";

import { describe, expect, it } from "vitest";

import { gameExitInfo, gameExitRevision } from "./game-exit";

const snapshot = (source: Record<string, unknown>) =>
  ({ ...source }) as unknown as launch.GameLaunchSnapshot;

describe("gameExitInfo", () => {
  it("非终态（空闲 / 准备 / 运行）不产出退出信息", () => {
    expect(gameExitInfo(null)).toBeNull();
    expect(gameExitInfo(undefined)).toBeNull();
    expect(gameExitInfo(snapshot({ Phase: 0 }))).toBeNull();
    expect(gameExitInfo(snapshot({ Phase: 1 }))).toBeNull();
    expect(gameExitInfo(snapshot({ Phase: 2 }))).toBeNull();
  });

  it("Phase 3：启动阶段就失败，没有退出码也不叫崩溃", () => {
    expect(
      gameExitInfo(
        snapshot({
          Phase: 3,
          VersionId: "1.20.1",
          Message: "缺少 Java 运行时",
        }),
      ),
    ).toEqual({
      versionId: "1.20.1",
      phase: "failed",
      exitCode: null,
      crashed: false,
      stoppedManually: false,
      message: "缺少 Java 运行时",
    });
  });

  it("Phase 4 非零退出码：判定为崩溃并带上退出码", () => {
    expect(
      gameExitInfo(
        snapshot({
          Phase: 4,
          VersionId: "fabric-1.21",
          ExitCode: 1,
          StoppedManually: false,
          Message: "游戏异常退出",
        }),
      ),
    ).toMatchObject({
      versionId: "fabric-1.21",
      phase: "exited",
      exitCode: 1,
      crashed: true,
      stoppedManually: false,
    });
  });

  it("Phase 4 正常退出与手动停止都不算崩溃", () => {
    expect(
      gameExitInfo(snapshot({ Phase: 4, ExitCode: 0, Message: "游戏已退出" })),
    ).toMatchObject({ crashed: false, exitCode: 0, stoppedManually: false });

    expect(
      gameExitInfo(snapshot({ Phase: 4, ExitCode: 1, StoppedManually: true })),
    ).toMatchObject({ crashed: false, stoppedManually: true, exitCode: 1 });
  });

  it("文案缺失时回落到标题，版本 id 缺失时给空串（不抛错）", () => {
    expect(
      gameExitInfo(snapshot({ Phase: 4, Title: "游戏已停止" })),
    ).toMatchObject({ message: "游戏已停止", versionId: "" });
  });
});

describe("gameExitRevision", () => {
  it("取快照版本号用于去重", () => {
    expect(gameExitRevision(snapshot({ Revision: 12 }))).toBe(12);
    expect(gameExitRevision(null)).toBe(0);
  });
});
