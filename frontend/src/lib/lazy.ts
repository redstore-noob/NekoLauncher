/*
 * 路由级懒加载：把各页面切成独立 chunk，首包只带主页与壳层，
 * 减小启动时要解析/求值的 JS 体量（配合 WindowReveal 的揭幕时机，
 * 首屏更快定型）；页面切换时由 PageHost 的 Suspense 兜底。
 *
 * 无卡顿关键：prefetchIdlePages 在首帧渲染后的浏览器空闲期把其余
 * chunk 逐个预热好，用户真正点过去时早已在内存里，不会看到 loading。
 * 请求本地资源极快，prefetch 也天然被浏览器去重。
 */
import { lazy, type ComponentType, type LazyExoticComponent } from "react";

/** 已登记的页面预取函数（lazyPage 创建时自动登记） */
const prefetchers: Array<() => void> = [];

/**
 * 包一层 React.lazy 并登记预取函数。factory 只执行一次
 * （lazy 与预取共享同一个 in-flight Promise，不会重复下载）。
 */
export function lazyPage<C extends ComponentType<object>>(
  factory: () => Promise<{ default: C }>,
): LazyExoticComponent<C> {
  let pending: Promise<{ default: C }> | null = null;

  const once = () => {
    if (!pending) pending = factory();

    return pending;
  };

  prefetchers.push(() => {
    once().catch(() => {
      // 预取失败不重试也不报错：真实挂载时 lazy 会再拉一次，失败走 ErrorBoundary
      pending = null;
    });
  });

  return lazy(once);
}

/**
 * 空闲期预取全部已登记页面。每个 chunk 之间都过一次空闲回调，
 * 避免同一帧连发十几个请求抢占网络与主线程，把"预热"本身变成卡顿源。
 */
export function prefetchIdlePages(): void {
  const queue = [...prefetchers];
  let index = 0;

  const idle = (task: () => void) => {
    if (typeof requestIdleCallback === "function")
      requestIdleCallback(() => task(), { timeout: 4000 });
    else setTimeout(task, 1500);
  };

  const step = () => {
    const next = queue[index++];

    if (!next) return;
    next();
    idle(step);
  };

  idle(step);
}
