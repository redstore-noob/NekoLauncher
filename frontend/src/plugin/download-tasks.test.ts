/*
 * 下载任务归一与变更节流的契约：宿主内部有两套互不相干的下载状态（游戏本体是单条
 * 状态机快照，内容资源 / Java 是任务注册表），插件看到的必须是同一形状。完成态判定
 * 错了，插件要么在下载没结束时误报"已完成"，要么永远等不到完成；而变更订阅的节流做
 * 错了，密集的进度事件会把插件的回调刷爆。
 */
import type { DownloadTaskPhase, DownloadTaskSummary } from "./types";

import { describe, expect, it, vi } from "vitest";

import { download } from "../../wailsjs/go/models";

import {
  contentDownloadTask,
  downloadTasksFromSnapshots,
  gameDownloadTask,
  watchDownloadTasks,
} from "./download-tasks";

const gameSnapshot = (source: Record<string, unknown>) =>
  download.GameDownloadSnapshot.createFrom(source);
const contentSnapshot = (source: Record<string, unknown>) =>
  download.ContentTaskSnapshot.createFrom(source);

/** 空闲快照：宿主初始化后的默认值（Phase 0 + 提示文案，无版本 id） */
const idleSnapshot = () =>
  gameSnapshot({
    Phase: 0,
    StageName: "尚无下载任务",
    Detail: "请在资源下载页选择 Minecraft 版本。",
  });

describe("gameDownloadTask", () => {
  it("空闲快照不产出任务", () => {
    expect(gameDownloadTask(null)).toBeNull();
    expect(gameDownloadTask(idleSnapshot())).toBeNull();
  });

  it("下载中：百分比、速度与剩余时间来自快照字节数", () => {
    const task = gameDownloadTask(
      gameSnapshot({
        Phase: 2,
        VersionID: "1.20.1",
        StageName: "下载依赖库",
        Percentage: 42.5,
        CompletedBytes: 425,
        TotalBytes: 1000,
        BytesPerSecond: 100,
      }),
    );

    expect(task).toMatchObject({
      id: "game",
      name: "1.20.1",
      kind: "game",
      phase: "downloading",
      isActive: true,
      isFinished: false,
      isCompleted: false,
      percent: 42.5,
      indeterminate: false,
      etaSeconds: 5.75,
      detail: "下载依赖库",
    });
  });

  it("准备阶段未知总大小：不定进度、无法估算剩余时间", () => {
    const task = gameDownloadTask(
      gameSnapshot({
        Phase: 1,
        VersionID: "1.20.1",
        StageName: "获取版本元数据",
      }),
    );

    expect(task).toMatchObject({
      phase: "downloading",
      percent: 0,
      indeterminate: true,
      etaSeconds: null,
      bytesPerSecond: 0,
    });
  });

  it("完成 / 取消 / 失败分别落到 isCompleted 与终态阶段", () => {
    expect(
      gameDownloadTask(
        gameSnapshot({ Phase: 3, VersionID: "1.20.1", Percentage: 100 }),
      ),
    ).toMatchObject({
      phase: "completed",
      isActive: false,
      isFinished: true,
      isCompleted: true,
      etaSeconds: null,
      bytesPerSecond: 0,
    });
    expect(
      gameDownloadTask(gameSnapshot({ Phase: 4, VersionID: "1.20.1" })),
    ).toMatchObject({ phase: "failed", isCompleted: false, isFinished: true });
    expect(
      gameDownloadTask(gameSnapshot({ Phase: 5, VersionID: "1.20.1" })),
    ).toMatchObject({
      phase: "cancelled",
      isCompleted: false,
      isFinished: true,
    });
  });
});

describe("contentDownloadTask", () => {
  it("活跃任务：按总大小算百分比，未知总大小走不定进度", () => {
    expect(
      contentDownloadTask(
        contentSnapshot({
          id: "ct-1",
          name: "sodium.jar",
          kind: "content",
          phase: 1,
          detail: "下载依赖文件…",
          downloadedBytes: 30,
          totalBytes: 120,
          bytesPerSecond: 10,
          etaSeconds: 9,
        }),
      ),
    ).toMatchObject({
      id: "ct-1",
      kind: "content",
      phase: "downloading",
      isActive: true,
      percent: 25,
      indeterminate: false,
      etaSeconds: 9,
      detail: "下载依赖文件…",
    });

    expect(
      contentDownloadTask(
        contentSnapshot({
          id: "ct-2",
          name: "整合包",
          kind: "modpack",
          phase: 1,
          totalBytes: 0,
          etaSeconds: -1,
        }),
      ),
    ).toMatchObject({
      kind: "modpack",
      percent: 0,
      indeterminate: true,
      etaSeconds: null,
    });
  });

  it("终态任务：完成 / 失败（detail 即失败原因）/ 取消", () => {
    expect(
      contentDownloadTask(contentSnapshot({ id: "ct-3", phase: 3 })),
    ).toMatchObject({
      phase: "completed",
      isCompleted: true,
      isFinished: true,
    });
    expect(
      contentDownloadTask(
        contentSnapshot({ id: "ct-4", phase: 4, detail: "403 禁止分发" }),
      ),
    ).toMatchObject({
      phase: "failed",
      isCompleted: false,
      detail: "403 禁止分发",
    });
    expect(
      contentDownloadTask(
        contentSnapshot({ id: "ct-5", phase: 5, detail: "已取消" }),
      ),
    ).toMatchObject({ phase: "cancelled", isCompleted: false });
  });

  it("未知类别收敛为单文件内容资源", () => {
    expect(
      contentDownloadTask(
        contentSnapshot({ id: "ct-6", kind: "未知", phase: 1 }),
      ).kind,
    ).toBe("content");
  });
});

describe("downloadTasksFromSnapshots", () => {
  it("游戏本体在前，内容任务按注册表顺序随后", () => {
    const tasks = downloadTasksFromSnapshots(
      gameSnapshot({ Phase: 2, VersionID: "1.20.1", Percentage: 10 }),
      [
        contentSnapshot({ id: "ct-1", name: "a.jar", phase: 1 }),
        contentSnapshot({ id: "ct-2", name: "b.jar", phase: 3 }),
      ],
    );

    expect(tasks.map((task) => task.id)).toEqual(["game", "ct-1", "ct-2"]);
    expect(tasks[1].isCompleted).toBe(false);
    expect(tasks[2].isCompleted).toBe(true);
  });

  it("没有游戏任务时只返回内容任务；全程无任务返回空列表", () => {
    expect(
      downloadTasksFromSnapshots(idleSnapshot(), [
        contentSnapshot({ id: "ct-9", name: "lib.jar", phase: 4 }),
      ]).map((task) => task.id),
    ).toEqual(["ct-9"]);
    expect(downloadTasksFromSnapshots(idleSnapshot(), [])).toEqual([]);
    expect(downloadTasksFromSnapshots(undefined, undefined)).toEqual([]);
  });
});

/** 可手动兑现的 Promise：钉住"读取在途时又来事件"的时序 */
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });

  return { promise, resolve };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

const contentTask = (
  id: string,
  phase: DownloadTaskPhase = "downloading",
): DownloadTaskSummary => ({
  id,
  name: id,
  kind: "content",
  phase,
  isActive: phase === "downloading",
  isFinished: phase !== "downloading",
  isCompleted: phase === "completed",
  percent: 0,
  indeterminate: false,
  downloadedBytes: 0,
  totalBytes: 0,
  bytesPerSecond: 0,
  etaSeconds: null,
  detail: "",
});

describe("watchDownloadTasks", () => {
  it("notify 重读一份完整列表并投递给 handler", async () => {
    const read = vi.fn(async () => [contentTask("ct-1")]);
    const received: DownloadTaskSummary[][] = [];
    const watcher = watchDownloadTasks((tasks) => received.push(tasks), read);

    watcher.notify();
    await flush();

    expect(read).toHaveBeenCalledTimes(1);
    expect(received).toEqual([[contentTask("ct-1")]]);
  });

  it("读取在途时的多次 notify 合并成一次补跑", async () => {
    const first = deferred<DownloadTaskSummary[]>();
    const second = deferred<DownloadTaskSummary[]>();
    const read = vi
      .fn<() => Promise<DownloadTaskSummary[]>>()
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const received: DownloadTaskSummary[][] = [];
    const watcher = watchDownloadTasks((tasks) => received.push(tasks), read);

    watcher.notify();
    watcher.notify();
    watcher.notify();
    await flush();
    // 一次在途 + 一次补跑：补跑已在途，第三次事件不再排队
    expect(read).toHaveBeenCalledTimes(1);

    first.resolve([contentTask("ct-1")]);
    await flush();
    expect(read).toHaveBeenCalledTimes(2);
    expect(received.map((tasks) => tasks[0].id)).toEqual(["ct-1"]);

    second.resolve([contentTask("ct-2")]);
    await flush();
    expect(received.map((tasks) => tasks[0].id)).toEqual(["ct-1", "ct-2"]);
  });

  it("cancel 丢弃在途结果，之后的 notify 也不再读取", async () => {
    const first = deferred<DownloadTaskSummary[]>();
    const read = vi.fn<() => Promise<DownloadTaskSummary[]>>(
      () => first.promise,
    );
    const received: DownloadTaskSummary[][] = [];
    const watcher = watchDownloadTasks((tasks) => received.push(tasks), read);

    watcher.notify();
    watcher.cancel();
    first.resolve([contentTask("ct-1")]);
    await flush();

    watcher.notify();
    await flush();

    expect(read).toHaveBeenCalledTimes(1);
    expect(received).toEqual([]);
  });

  it("handler 抛错只吃掉这一次，后续事件照常投递", async () => {
    const onError = vi.spyOn(console, "error").mockImplementation(() => {});
    const read = vi.fn(async () => [contentTask("ct-1")]);
    let calls = 0;
    const watcher = watchDownloadTasks(() => {
      calls++;
      throw new Error("插件回调炸了");
    }, read);

    watcher.notify();
    await flush();
    watcher.notify();
    await flush();

    expect(calls).toBe(2);
    // 每次抛错各记一条，但都不外泄、不打断后续轮次
    expect(onError).toHaveBeenCalledTimes(2);
    onError.mockRestore();
  });

  it("读取失败不投递，下一个事件会重试", async () => {
    const read = vi
      .fn<() => Promise<DownloadTaskSummary[]>>()
      .mockRejectedValueOnce(new Error("绑定暂时不可用"))
      .mockResolvedValueOnce([contentTask("ct-1", "completed")]);
    const received: DownloadTaskSummary[][] = [];
    const watcher = watchDownloadTasks((tasks) => received.push(tasks), read);

    watcher.notify();
    await flush();
    expect(received).toEqual([]);

    watcher.notify();
    await flush();
    expect(received).toEqual([[contentTask("ct-1", "completed")]]);
  });
});
