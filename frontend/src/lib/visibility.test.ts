/*
 * lib/visibility.ts 的测试：高频轮询在前台按节拍触发，窗口隐藏期间暂停，
 * 回到前台先补一拍再恢复定时，卸载后不再有任何触发。
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { startVisiblePoll } from "./visibility";

// jsdom 的 document.hidden 是只读 getter，只能 redefine 后手动派发事件模拟切换
function setHidden(hidden: boolean) {
  Object.defineProperty(document, "hidden", {
    configurable: true,
    get: () => hidden,
  });
  document.dispatchEvent(new Event("visibilitychange"));
}

describe("startVisiblePoll", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setHidden(false);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("前台按固定节拍触发，stop 后停止", () => {
    const fn = vi.fn();
    const stop = startVisiblePoll(fn, 1000);

    expect(fn).not.toHaveBeenCalled(); // 首拍由调用方自己执行，这里不抢跑

    vi.advanceTimersByTime(3000);
    expect(fn).toHaveBeenCalledTimes(3);

    stop();
    vi.advanceTimersByTime(3000);
    expect(fn).toHaveBeenCalledTimes(3);
  });

  it("隐藏期间不跑，回前台先补一拍再恢复节拍", () => {
    const fn = vi.fn();
    const stop = startVisiblePoll(fn, 1000);

    setHidden(true);
    vi.advanceTimersByTime(5000);
    expect(fn).not.toHaveBeenCalled();

    setHidden(false);
    expect(fn).toHaveBeenCalledTimes(1); // 回前台立刻补一拍，数据不空窗

    vi.advanceTimersByTime(2000);
    expect(fn).toHaveBeenCalledTimes(3);

    stop();
  });

  it("stop 解绑监听：之后再切可见也不会触发", () => {
    const fn = vi.fn();
    const stop = startVisiblePoll(fn, 1000);

    stop();
    setHidden(true);
    setHidden(false);
    vi.advanceTimersByTime(5000);
    expect(fn).not.toHaveBeenCalled();
  });
});
