/*
 * lib/navigation.ts 的测试（P3-6）。
 *
 * 这条总线承担一个容易出错的小机制：切页是同步广播的，而目标页的订阅 effect
 * 要等挂载后才注册——所以 detail 必须"暂存一次、被取走即清空"。
 * 暂存不清理会让下一个页面误领上一条请求；清理太早又会让详情定位丢失。
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  consumePendingDetail,
  navigateToPage,
  onNavigate,
  type NavigateRequest,
} from "./navigation";

describe("navigation 总线", () => {
  // Shell 常驻订阅：pending 暂存是在订阅者收到事件时写入的（模块设计如此），
  // 所以"暂存 detail"的用例必须先挂一个订阅者
  let off: (() => void) | null = null;

  beforeEach(() => {
    // 清掉上一条用例残留的暂存 detail 与订阅
    consumePendingDetail("任意页面");
    off?.();
    off = onNavigate(() => undefined);
  });

  afterEach(() => {
    off?.();
    off = null;
  });

  it("广播切页请求并带上 detail", () => {
    const handler = vi.fn<(request: NavigateRequest) => void>();
    const unsubscribe = onNavigate(handler);

    navigateToPage("settings", "java");

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledWith({
      pageId: "settings",
      detail: "java",
    });
    unsubscribe();
  });

  it("detail 被取走一次后即清空（防止被后来的页面误领）", () => {
    navigateToPage("download", "fabric");

    expect(consumePendingDetail("download")).toBe("fabric");
    expect(consumePendingDetail("download")).toBeUndefined();
  });

  it("页面不匹配时也会清空暂存，但不返回 detail", () => {
    navigateToPage("download", "fabric");

    expect(consumePendingDetail("settings")).toBeUndefined();
    expect(consumePendingDetail("download")).toBeUndefined();
  });

  it("取消订阅后不再收到请求", () => {
    const handler = vi.fn<(request: NavigateRequest) => void>();
    const unsubscribe = onNavigate(handler);

    unsubscribe();
    navigateToPage("home");

    expect(handler).not.toHaveBeenCalled();
  });

  it("非法请求（缺 pageId / 非字符串）被静默忽略", () => {
    const handler = vi.fn<(request: NavigateRequest) => void>();
    const unsubscribe = onNavigate(handler);

    window.dispatchEvent(new CustomEvent("nya:navigate", { detail: null }));
    window.dispatchEvent(new CustomEvent("nya:navigate", { detail: {} }));
    window.dispatchEvent(
      new CustomEvent("nya:navigate", { detail: { pageId: "" } }),
    );

    expect(handler).not.toHaveBeenCalled();
    unsubscribe();
  });
});
