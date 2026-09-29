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
import type { ReactNode } from "react";

import React, { useEffect, useState } from "react";

/** 一条右键菜单项。divider: true 时只渲染一条分割线（label 被忽略）。 */
export interface ContextMenuItem {
  key: string;
  label?: string;
  icon?: ReactNode;
  danger?: boolean;
  disabled?: boolean;
  divider?: boolean;
  onSelect?: () => void;
}

export interface ContextMenuState {
  x: number;
  y: number;
  items: ContextMenuItem[];
}

/**
 * 轻量右键菜单：受控状态（位置 + 条目），overlay 点击任意处关闭。
 * HeroUI 没有原生右键菜单组件，这里手写一个与整体风格一致的浮层；
 * 超出视口边缘时向内翻转，避免被窗口裁掉。
 */
export const ContextMenuOverlay: React.FC<{
  state: ContextMenuState | null;
  onClose: () => void;
}> = ({ state, onClose }) => {
  const [pos, setPos] = useState<{ x: number; y: number } | null>(null);

  // 挂载后按菜单实际尺寸做一次视口内回正（SSR 无需考虑，桌面应用固定有 DOM）。
  useEffect(() => {
    if (!state) {
      setPos(null);

      return;
    }
    const el = document.getElementById("nya-context-menu");

    if (!el) {
      setPos({ x: state.x, y: state.y });

      return;
    }
    const rect = el.getBoundingClientRect();
    const x = Math.min(state.x, window.innerWidth - rect.width - 8);
    const y = Math.min(state.y, window.innerHeight - rect.height - 8);

    setPos({ x: Math.max(8, x), y: Math.max(8, y) });
  }, [state]);

  // Escape 关闭
  useEffect(() => {
    if (!state) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };

    window.addEventListener("keydown", onKey);

    return () => window.removeEventListener("keydown", onKey);
  }, [state, onClose]);

  if (!state) return null;

  return (
    <div
      className="fixed inset-0 z-[80]"
      role="presentation"
      onClick={onClose}
      onContextMenu={(e) => {
        e.preventDefault();
        onClose();
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") onClose();
      }}
    >
      <div
        className="absolute min-w-44 overflow-hidden rounded-xl border nya-border nya-panel py-1.5 shadow-xl"
        id="nya-context-menu"
        role="menu"
        tabIndex={-1}
        style={{ left: (pos ?? state).x, top: (pos ?? state).y }}
        onKeyDown={(e) => {
          if (e.key === "Escape") onClose();
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {state.items.map((item, index) =>
          item.divider ? (
            <div key={`d-${index}`} className="my-1 h-px bg-default-200/70" />
          ) : (
            <button
              key={item.key}
              className={`flex w-full items-center gap-2.5 px-3.5 py-1.5 text-left text-[13px] transition-colors ${
                item.disabled
                  ? "cursor-not-allowed text-gray-300 dark:text-gray-600"
                  : item.danger
                    ? "text-danger hover:bg-danger/10"
                    : "text-gray-700 dark:text-gray-200 hover:bg-primary/10"
              }`}
              disabled={item.disabled}
              type="button"
              onClick={() => {
                onClose();
                item.onSelect?.();
              }}
            >
              {item.icon ? (
                <span className="flex w-4 flex-shrink-0 justify-center">
                  {item.icon}
                </span>
              ) : null}
              {item.label}
            </button>
          ),
        )}
      </div>
    </div>
  );
};
