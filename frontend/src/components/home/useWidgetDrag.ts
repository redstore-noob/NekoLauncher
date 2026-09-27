/*
 * Copyright 2024 Next UI
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
import React, { useLayoutEffect, useRef, useState } from "react";

/**
 * 主页小组件的拖动引擎：
 * - 长按 1 秒进入拖动（列表内重新排序 / 拖到右侧启动页或组件库删除）；
 * - 组件库条目无需长按，拖出即添加；
 * - 拖动期间只更新必要的 React 状态：幽灵条位置与命中结果分开处理，
 *   幽灵条直接改 DOM style，避免每次 pointermove 都重渲染整个主页。
 */

/** 长按多久进入拖动（ms） */
const HOLD_MS = 1000;
/** 长按计时期间的位移容差：超过视为滚动/选择，取消长按 */
const HOLD_MOVE_TOLERANCE_PX = 10;
/** 组件库条目位移超过该距离即视为拖出 */
const LIBRARY_DRAG_THRESHOLD_PX = 4;
/** 幽灵条相对指针的偏移 */
const GHOST_OFFSET_PX = 12;
/** 列表命中判定在水平/垂直方向上的额外容差 */
const LIST_HIT_MARGIN_X = 90;
const LIST_HIT_MARGIN_Y = 40;
/** 拖放结束后继续吞掉 click 的时长（ms）：防止嵌套按钮在松手瞬间被触发 */
const CLICK_ABSORB_MS = 350;

/** 拖动来源：library=组件库条目，list=页面上已有的组件 */
export type DragPayload = {
  kind: "library" | "list";
  widgetId: string;
  /** 列表内拖动时为原下标，组件库拖动为 -1 */
  index: number;
};

/** 松手位置：插到某一纵排的某个位置之前，或落在删除区 */
export type DropTarget =
  | { kind: "list"; column: number; index: number }
  | { kind: "delete" };

export interface DragSession {
  payload: DragPayload;
  /** 目标纵排下标；null = 不在列表上 */
  targetColumn: number | null;
  /** 目标纵排内的插入位置（0..该列长度），null = 不在列表上 */
  targetIndex: number | null;
  /** 指针是否落在删除区（右侧启动页） */
  overDeleteZone: boolean;
}

interface PressState {
  widgetId: string;
  index: number;
  startX: number;
  startY: number;
  /** true = 组件库条目（位移即拖出），false = 页面组件（需长按 1 秒） */
  library: boolean;
  timer: number;
}

/**
 * 指针是否按在应跳过长按拖动的元素上：
 * 文本输入（长按是文本选择）、画布（长按是 3D 旋转），
 * 以及标记了 data-widget-interactive 的交互区（如木鱼的长按连敲）。
 */
function isInteractiveTarget(target: EventTarget | null): boolean {
  return (
    target instanceof HTMLElement &&
    target.closest("input, textarea, canvas, [data-widget-interactive]") !==
      null
  );
}

export interface WidgetDragApi {
  session: DragSession | null;
  /** 长按计时中的组件 id（用于顶部进度条与缩放反馈） */
  pressingId: string | null;
  /** 跟随指针的幽灵条容器（内部直接写 transform） */
  ghostRef: React.RefObject<HTMLDivElement>;
  /** 页面组件的长按事件（挂在卡片外层容器上） */
  listPressHandlers: (
    widgetId: string,
    index: number,
  ) => { onPointerDown: (event: React.PointerEvent) => void };
  /** 组件库条目的拖动事件 */
  libraryHandlers: (widgetId: string) => {
    onPointerDown: (event: React.PointerEvent) => void;
    onClick: () => void;
  };
}

export function useWidgetDrag(options: {
  /** 小组件列容器（多纵排的外层包裹，用于计算落点） */
  listRef: React.RefObject<HTMLDivElement>;
  /** 右侧启动页容器（拖动时作为删除区） */
  deleteRef: React.RefObject<HTMLElement>;
  /** 组件库容器：展开时拖回组件库同样算删除区 */
  libraryRef?: React.RefObject<HTMLElement>;
  /** 松手回调：target 为 null 表示原地放回（取消） */
  onDrop: (payload: DragPayload, target: DropTarget | null) => void;
  /** 组件库条目被点击（未拖出）时回调 */
  onLibraryPick: (widgetId: string) => void;
}): WidgetDragApi {
  const { listRef, deleteRef, libraryRef, onDrop, onLibraryPick } = options;
  const [session, setSession] = useState<DragSession | null>(null);
  const [pressingId, setPressingId] = useState<string | null>(null);

  const ghostRef = useRef<HTMLDivElement>(null);
  const sessionRef = useRef<DragSession | null>(null);
  const pressRef = useRef<PressState | null>(null);
  const pointerRef = useRef({ x: 0, y: 0 });
  const absorbTimerRef = useRef(0);
  const onDropRef = useRef(onDrop);
  const onLibraryPickRef = useRef(onLibraryPick);

  onDropRef.current = onDrop;
  onLibraryPickRef.current = onLibraryPick;

  const moveGhost = (x: number, y: number) => {
    const ghost = ghostRef.current;

    if (ghost) {
      ghost.style.transform = `translate3d(${x + GHOST_OFFSET_PX}px, ${y + GHOST_OFFSET_PX}px, 0)`;
    }
  };

  const clearPressTimer = () => {
    const press = pressRef.current;

    if (press && press.timer) window.clearTimeout(press.timer);
  };

  /** 指针是否落在某个删除区元素内 */
  const hitsDeleteZone = (x: number, y: number) =>
    [deleteRef.current, libraryRef?.current ?? null].some((el) => {
      if (!el) return false;
      const rect = el.getBoundingClientRect();

      return (
        x >= rect.left && x <= rect.right && y >= rect.top && y <= rect.bottom
      );
    });

  /**
   * 指针位置 → 落点：目标纵排 + 列内插入位置 + 是否在删除区。
   * 多纵排各自独立滚动，所以先按 X 选列（落在列间空隙取最近列），
   * 再在该列内按卡片垂直中线判定列内插入点。
   */
  const computeTarget = (x: number, y: number) => {
    let overDeleteZone = false;
    let targetColumn: number | null = null;
    let targetIndex: number | null = null;

    if (!sessionRef.current)
      return { targetColumn, targetIndex, overDeleteZone };

    // 删除区只对页面组件生效：组件库条目本来就待在库里，
    // 刚拖动时指针还在库上，不能被误判成"拖回删除"。
    if (sessionRef.current.payload.kind === "list") {
      overDeleteZone = hitsDeleteZone(x, y);
    }

    const listEl = listRef.current;

    if (!overDeleteZone && listEl) {
      const rect = listEl.getBoundingClientRect();
      const insideX =
        x >= rect.left - LIST_HIT_MARGIN_X &&
        x <= rect.right + LIST_HIT_MARGIN_X;
      const insideY =
        y >= rect.top - LIST_HIT_MARGIN_Y &&
        y <= rect.bottom + LIST_HIT_MARGIN_Y;

      if (insideX && insideY) {
        const columns = listEl.querySelectorAll<HTMLElement>(
          "[data-widget-column]",
        );

        // 选列：指针 X 落在列内优先，否则取水平距离最近的一列
        let chosen = 0;
        let bestDistance = Number.POSITIVE_INFINITY;

        for (let c = 0; c < columns.length; c += 1) {
          const columnRect = columns[c].getBoundingClientRect();

          if (x >= columnRect.left && x <= columnRect.right) {
            chosen = c;
            bestDistance = 0;
            break;
          }
          const distance =
            x < columnRect.left ? columnRect.left - x : x - columnRect.right;

          if (distance < bestDistance) {
            bestDistance = distance;
            chosen = c;
          }
        }

        const columnEl = columns[chosen];

        if (columnEl) {
          const cards = columnEl.querySelectorAll<HTMLElement>(
            "[data-widget-index]",
          );

          targetColumn = chosen;
          targetIndex = cards.length;
          for (let i = 0; i < cards.length; i += 1) {
            const cardRect = cards[i].getBoundingClientRect();

            if (y < cardRect.top + cardRect.height / 2) {
              targetIndex = i;
              break;
            }
          }
        }
      }
    }

    return { targetColumn, targetIndex, overDeleteZone };
  };

  // 稳定的 window 监听器：只读写 ref，不依赖每次渲染的闭包
  const listeners = useRef({
    absorb(event: MouseEvent) {
      event.preventDefault();
      event.stopPropagation();
    },
    move(event: PointerEvent) {
      pointerRef.current = { x: event.clientX, y: event.clientY };
      const press = pressRef.current;

      // 尚未进入拖动：长按计时中或组件库条目等待位移
      if (!sessionRef.current) {
        if (!press) return;
        const moved = Math.hypot(
          event.clientX - press.startX,
          event.clientY - press.startY,
        );

        if (!press.library) {
          if (moved > HOLD_MOVE_TOLERANCE_PX) cancelPressRef.current();

          return;
        }
        if (moved < LIBRARY_DRAG_THRESHOLD_PX) return;
        startSessionRef.current(
          { kind: "library", widgetId: press.widgetId, index: -1 },
          pointerRef.current,
        );
      }

      const current = sessionRef.current;

      if (!current) return;
      event.preventDefault();
      moveGhostRef.current(pointerRef.current.x, pointerRef.current.y);
      const { targetColumn, targetIndex, overDeleteZone } =
        computeTargetRef.current(pointerRef.current.x, pointerRef.current.y);

      if (
        targetColumn !== current.targetColumn ||
        targetIndex !== current.targetIndex ||
        overDeleteZone !== current.overDeleteZone
      ) {
        const next = {
          ...current,
          targetColumn,
          targetIndex,
          overDeleteZone,
        };

        sessionRef.current = next;
        setSession(next);
      }
    },
    up() {
      if (sessionRef.current) {
        finishSessionRef.current();

        return;
      }
      cancelPressRef.current();
    },
    key(event: KeyboardEvent) {
      if (event.key === "Escape" && sessionRef.current) {
        finishSessionRef.current();
      }
    },
  }).current;

  // 跨渲染共享的稳定引用（监听器与回调互相调用，避免闭包过期）
  const moveGhostRef = useRef(moveGhost);
  const computeTargetRef = useRef(computeTarget);
  const startSessionRef = useRef<
    (payload: DragPayload, point: { x: number; y: number }) => void
  >(() => undefined);
  const finishSessionRef = useRef<() => void>(() => undefined);
  const cancelPressRef = useRef<() => void>(() => undefined);

  moveGhostRef.current = moveGhost;
  computeTargetRef.current = computeTarget;

  const attachListeners = () => {
    window.addEventListener("pointermove", listeners.move);
    window.addEventListener("pointerup", listeners.up);
    window.addEventListener("pointercancel", listeners.up);
    window.addEventListener("keydown", listeners.key);
  };

  const detachListeners = () => {
    window.removeEventListener("pointermove", listeners.move);
    window.removeEventListener("pointerup", listeners.up);
    window.removeEventListener("pointercancel", listeners.up);
    window.removeEventListener("keydown", listeners.key);
  };

  const cancelPress = () => {
    clearPressTimer();
    pressRef.current = null;
    setPressingId(null);
    detachListeners();
  };

  cancelPressRef.current = cancelPress;

  const startSession = (
    payload: DragPayload,
    point: { x: number; y: number },
  ) => {
    clearPressTimer();
    pressRef.current = null;
    setPressingId(null);
    pointerRef.current = point;

    // 列表内拖动：定位源卡片所在纵排与列内下标，让指示线一开始就出现在原位
    let targetColumn: number | null = null;
    let targetIndex: number | null = null;

    if (payload.kind === "list") {
      const sourceCard = listRef.current?.querySelector<HTMLElement>(
        `[data-widget-index="${payload.index}"]`,
      );
      const sourceColumn = sourceCard?.closest<HTMLElement>(
        "[data-widget-column]",
      );

      if (sourceCard && sourceColumn) {
        const cards = Array.from(
          sourceColumn.querySelectorAll<HTMLElement>("[data-widget-index]"),
        );

        targetColumn = Number(sourceColumn.dataset.widgetColumn ?? 0);
        targetIndex = Math.max(cards.indexOf(sourceCard), 0);
      }
    }

    const next: DragSession = {
      payload,
      targetColumn,
      targetIndex,
      overDeleteZone: false,
    };

    sessionRef.current = next;
    setSession(next);
    // 拖动中禁止选中文本，并吞掉松手瞬间产生的那次 click
    document.body.style.userSelect = "none";
    window.addEventListener("click", listeners.absorb, true);
  };

  startSessionRef.current = startSession;

  const finishSession = () => {
    const current = sessionRef.current;

    sessionRef.current = null;
    pressRef.current = null;
    setSession(null);
    setPressingId(null);
    document.body.style.userSelect = "";
    detachListeners();
    // click 的吞噬延后一点移除：pointerup 之后紧跟的那次 click 仍会被拦下
    window.clearTimeout(absorbTimerRef.current);
    absorbTimerRef.current = window.setTimeout(() => {
      window.removeEventListener("click", listeners.absorb, true);
      absorbTimerRef.current = 0;
    }, CLICK_ABSORB_MS);

    if (!current) return;
    if (current.overDeleteZone) {
      onDropRef.current(current.payload, { kind: "delete" });
    } else if (current.targetIndex !== null && current.targetColumn !== null) {
      onDropRef.current(current.payload, {
        kind: "list",
        column: current.targetColumn,
        index: current.targetIndex,
      });
    } else {
      onDropRef.current(current.payload, null);
    }
  };

  finishSessionRef.current = finishSession;

  // 会话开始后幽灵条才挂载，这里补一次初始位置
  useLayoutEffect(() => {
    if (session)
      moveGhostRef.current(pointerRef.current.x, pointerRef.current.y);
  }, [session]);

  // 卸载时清理，避免留下全局监听与选中锁
  useLayoutEffect(() => {
    return () => {
      window.removeEventListener("pointermove", listeners.move);
      window.removeEventListener("pointerup", listeners.up);
      window.removeEventListener("pointercancel", listeners.up);
      window.removeEventListener("keydown", listeners.key);
      window.removeEventListener("click", listeners.absorb, true);
      window.clearTimeout(absorbTimerRef.current);
      document.body.style.userSelect = "";
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const listPressHandlers = (widgetId: string, index: number) => ({
    onPointerDown: (event: React.PointerEvent) => {
      if (event.button !== 0) return;
      if (isInteractiveTarget(event.target)) return;
      cancelPress();
      pointerRef.current = { x: event.clientX, y: event.clientY };
      setPressingId(widgetId);
      const timer = window.setTimeout(() => {
        const press = pressRef.current;

        if (!press || press.library) return;
        startSessionRef.current(
          { kind: "list", widgetId, index },
          pointerRef.current,
        );
      }, HOLD_MS);

      pressRef.current = {
        widgetId,
        index,
        startX: event.clientX,
        startY: event.clientY,
        library: false,
        timer,
      };
      attachListeners();
    },
  });

  const libraryHandlers = (widgetId: string) => ({
    onPointerDown: (event: React.PointerEvent) => {
      if (event.button !== 0) return;
      cancelPress();
      pointerRef.current = { x: event.clientX, y: event.clientY };
      pressRef.current = {
        widgetId,
        index: -1,
        startX: event.clientX,
        startY: event.clientY,
        library: true,
        timer: 0,
      };
      attachListeners();
    },
    onClick: () => {
      // 拖出会吞掉 click，能走到这里的都是纯点击
      onLibraryPickRef.current(widgetId);
    },
  });

  return {
    session,
    pressingId,
    ghostRef,
    listPressHandlers,
    libraryHandlers,
  };
}
