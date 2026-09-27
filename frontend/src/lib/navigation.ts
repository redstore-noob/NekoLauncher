/*
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

/**
 * 跨组件页面导航总线：主页小组件等深层组件没有导航上下文，
 * 经由这里的全局窗口事件请求 Shell（layouts/index.tsx）切换页面。
 * 页面 id 与侧边栏页面注册表一致（如 "download" / "settings"）。
 */

/** 一次切页请求 */
export interface NavigateRequest {
  pageId: string;
  /** 可选的二级定位参数：目标页可据此切标签/滚动（如 "java" / "download"） */
  detail?: string;
}

const NAVIGATE_EVENT = "nya:navigate";

/**
 * 最近一次切页请求的二级参数。切页是同步广播出去的，而目标页的订阅 effect
 * 要等它挂载后才注册——那时事件早就过去了。所以这里暂存一份，由目标页
 * 在挂载时用 consumePendingDetail 取走。
 */
let pending: NavigateRequest | null = null;

/**
 * 取走当前挂载页面的二级定位参数（无论是否匹配都会清空暂存，
 * 避免它被后来的页面误当成自己的请求）。
 */
export function consumePendingDetail(pageId: string): string | undefined {
  const current = pending;

  pending = null;

  return current && current.pageId === pageId ? current.detail : undefined;
}

/**
 * 订阅切页请求（Shell 在挂载时调用一次）；返回取消订阅函数。
 * 非法请求（detail 缺 pageId）会被静默忽略，由请求方自己保证 id 有效。
 */
export function onNavigate(
  handler: (request: NavigateRequest) => void,
): () => void {
  const listener = (event: Event) => {
    const detail = (event as CustomEvent<NavigateRequest | null>).detail;

    if (detail && typeof detail.pageId === "string" && detail.pageId) {
      pending = detail;
      handler(detail);
    }
  };

  window.addEventListener(NAVIGATE_EVENT, listener);

  return () => window.removeEventListener(NAVIGATE_EVENT, listener);
}

/** 请求切换到指定页面；未知 id 由 Shell 回落到默认页 */
export function navigateToPage(pageId: string, detail?: string): void {
  window.dispatchEvent(
    new CustomEvent<NavigateRequest>(NAVIGATE_EVENT, {
      detail: { pageId, detail },
    }),
  );
}
