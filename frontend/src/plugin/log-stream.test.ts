/*
 * 日志增量流的契约：只推新增行、只推已完整的行、爆发时窗口化并如实报告丢弃量、
 * 上一拍未返回就跳过（背压）、日志被清空/轮转要标记。这些坏了插件要么丢日志、
 * 要么被回调刷爆、要么在日志换文件后把旧行当新行重放。
 */
import type { LogLineBatch } from "./types";

import { describe, expect, it, vi } from "vitest";

import { createLogStream } from "./log-stream";

/** flush 让排队的微任务（prime / 投递）跑完 */
const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

/** 可手动兑现的 Promise（测背压用） */
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });

  return { promise, resolve };
}

describe("createLogStream", () => {
  it("首个订阅者先定标：旧日志不投递，只推之后的新行并累计总行数", async () => {
    let text = "旧行一\n旧行二\n";
    const stream = createLogStream(async () => text);
    const batches: LogLineBatch[] = [];

    stream.subscribe((batch) => batches.push(batch));
    await flush();
    expect(batches).toEqual([]);

    text += "新行\n";
    await stream.poll();
    expect(batches).toEqual([
      { lines: ["新行"], droppedLines: 0, totalLines: 3, rotated: false },
    ]);

    // 没有新增时不再投递（不会每次 poll 都空推）
    await stream.poll();
    expect(batches).toHaveLength(1);
  });

  it("tailLines：订阅即补发尾部窗口，并报告被裁掉的行数", async () => {
    const text = "a\nb\nc\nd\n";
    const stream = createLogStream(async () => text);
    const batches: LogLineBatch[] = [];

    stream.subscribe((batch) => batches.push(batch), { tailLines: 2 });
    await flush();
    expect(batches).toEqual([
      { lines: ["c", "d"], droppedLines: 2, totalLines: 4, rotated: false },
    ]);
  });

  it("未写完的最后一行先攒着，换行到了才成行", async () => {
    let text = "a\nb";
    const stream = createLogStream(async () => text);
    const batches: LogLineBatch[] = [];

    stream.subscribe((batch) => batches.push(batch));
    await flush();

    text = "a\nb\nc\n";
    await stream.poll();
    expect(batches[0].lines).toEqual(["b", "c"]);
    expect(batches[0].totalLines).toBe(3);
  });

  it("超过单批上限只保留末尾窗口，并如实报告丢弃行数", async () => {
    let text = "";
    const stream = createLogStream(async () => text);
    const batches: LogLineBatch[] = [];

    stream.subscribe((batch) => batches.push(batch));
    await flush();

    text =
      Array.from({ length: 250 }, (_, index) => `行${index}`).join("\n") + "\n";
    await stream.poll();

    expect(batches[0].lines).toHaveLength(200);
    // 窗口末行是最后一行，丢弃的是最早那 50 行
    expect(batches[0].lines.at(-1)).toBe("行249");
    expect(batches[0].droppedLines).toBe(50);
    expect(batches[0].totalLines).toBe(250);
  });

  it("日志变短（清空 / 轮转）→ 标记 rotated 并重置总行数", async () => {
    let text = "a\nb\n";
    const stream = createLogStream(async () => text);
    const batches: LogLineBatch[] = [];

    stream.subscribe((batch) => batches.push(batch));
    await flush();

    text = "";
    await stream.poll();
    expect(batches).toEqual([
      { lines: [], droppedLines: 0, totalLines: 0, rotated: true },
    ]);

    text = "新一轮\n";
    await stream.poll();
    expect(batches[1]).toMatchObject({ lines: ["新一轮"], totalLines: 1 });
  });

  it("多订阅者拿到同一批；一个回调抛错不影响其它订阅者", async () => {
    let text = "";
    const stream = createLogStream(async () => text);
    const good: LogLineBatch[] = [];
    const onError = vi.spyOn(console, "error").mockImplementation(() => {});

    stream.subscribe(() => {
      throw new Error("插件回调炸了");
    });
    stream.subscribe((batch) => good.push(batch));
    await flush();

    text = "新行\n";
    await stream.poll();

    expect(good[0].lines).toEqual(["新行"]);
    expect(onError).toHaveBeenCalledTimes(1);
    onError.mockRestore();
  });

  it("背压：上一拍未返回时跳过这一拍（不排积压）", async () => {
    const prime = deferred<string>();
    const inflight = deferred<string>();
    const read = vi
      .fn<() => Promise<string>>()
      .mockReturnValueOnce(prime.promise)
      .mockReturnValueOnce(inflight.promise);
    const stream = createLogStream(read);

    stream.subscribe(() => {});
    await flush();
    prime.resolve("");
    await flush();

    const before = read.mock.calls.length;

    void stream.poll();
    void stream.poll();
    void stream.poll();
    await flush();

    // 并发三拍只落地一次读取，其余被背压挡掉
    expect(read.mock.calls.length).toBe(before + 1);

    inflight.resolve("");
    await flush();
    // 落地后可以正常再拍
    void stream.poll();
    await flush();
  });

  it("取消订阅后不再投递，订阅数归零", async () => {
    let text = "";
    const stream = createLogStream(async () => text);
    const batches: LogLineBatch[] = [];

    const cancel = stream.subscribe((batch) => batches.push(batch));

    await flush();
    expect(stream.size()).toBe(1);

    cancel();
    expect(stream.size()).toBe(0);

    text = "新行\n";
    await stream.poll();
    expect(batches).toEqual([]);
  });
});
