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
import React, { useState, useEffect } from "react";
import { Button, Tooltip } from "@heroui/react";

import { useI18n } from "../i18n";

interface TitleBarProps {
  title?: string;
  className?: string;
}

export const TitleBar: React.FC<TitleBarProps> = ({
  title = "",
  className = "",
}) => {
  const { t } = useI18n();
  const [isMax, setIsMax] = useState(false);

  const minimize = () => {
    if (window.runtime?.WindowMinimise) {
      window.runtime.WindowMinimise();
    }
  };

  const maximize = async () => {
    if (window.runtime?.WindowToggleMaximise) {
      await window.runtime.WindowToggleMaximise();
      if (window.runtime?.WindowIsMaximised) {
        const max = await window.runtime.WindowIsMaximised();

        setIsMax(max);
      }
    }
  };

  const quit = () => {
    if (window.runtime?.Quit) {
      window.runtime.Quit();
    }
  };

  //检查初始最大化状态，并在窗口尺寸变化时重新同步——
  //Win+Up、拖拽贴边、双击标题栏等途径改变最大化状态时，按钮图标/提示才能跟上
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;

    const checkMaximized = async () => {
      if (window.runtime?.WindowIsMaximised) {
        const max = await window.runtime.WindowIsMaximised();

        setIsMax(max);
      }
    };

    void checkMaximized();

    // resize 连发时防抖，只在稳定后查询一次
    const onResize = () => {
      clearTimeout(timer);
      timer = setTimeout(() => void checkMaximized(), 150);
    };

    window.addEventListener("resize", onResize);

    return () => {
      clearTimeout(timer);
      window.removeEventListener("resize", onResize);
    };
  }, []);

  //双击标题栏空白区切换最大化（与系统行为一致；按钮区域不触发）
  const onTitleBarDoubleClick = (e: React.MouseEvent) => {
    if ((e.target as HTMLElement).closest("button")) return;

    void maximize();
  };

  return (
    <div
      className={`
        h-10 
        backdrop-blur-md 
        fixed top-0 left-0 right-0 
        z-50 
        flex items-center 
        px-4 
        select-none
        ${className}
      `}
      style={{ "--wails-draggable": "drag" } as React.CSSProperties}
      onDoubleClick={onTitleBarDoubleClick}
    >
      <div className="flex items-center gap-2">
        <img
          alt=""
          className="h-[18px] w-[18px] flex-shrink-0 select-none"
          draggable={false}
          src="/favicon.ico"
        />
        <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
          {title}
        </span>
      </div>

      {/*可拖拽区域*/}
      <div className="flex-1 h-full" />

      {/*窗口控制按钮*/}
      <div
        className="flex items-center gap-1"
        style={{ "--wails-draggable": "no-drag" } as React.CSSProperties}
      >
        {/*最小化*/}
        <Tooltip closeDelay={0} content={t("最小化")} delay={300}>
          <Button
            isIconOnly
            aria-label={t("最小化")}
            className="h-8 min-w-8 w-8 rounded bg-transparent hover:bg-default-200/70 dark:hover:bg-default-100/20"
            variant="light"
            onClick={minimize}
          >
            <svg fill="none" height="10" viewBox="0 0 10 10" width="10">
              <path d="M0 5H10" stroke="currentColor" strokeWidth="1.2" />
            </svg>
          </Button>
        </Tooltip>

        {/*最大化/还原*/}
        <Tooltip
          closeDelay={0}
          content={isMax ? t("还原") : t("最大化")}
          delay={300}
        >
          <Button
            isIconOnly
            aria-label={isMax ? t("还原") : t("最大化")}
            className="h-8 min-w-8 w-8 rounded bg-transparent hover:bg-default-200/70 dark:hover:bg-default-100/20"
            variant="light"
            onClick={maximize}
          >
            {isMax ? (
              <svg fill="none" height="10" viewBox="0 0 10 10" width="10">
                <rect
                  fill="none"
                  height="7"
                  stroke="currentColor"
                  strokeWidth="1.2"
                  width="7"
                  x="1.5"
                  y="1.5"
                />
                <rect
                  fill="none"
                  height="5"
                  stroke="currentColor"
                  strokeWidth="1.2"
                  width="5"
                  x="3.5"
                  y="3.5"
                />
              </svg>
            ) : (
              <svg fill="none" height="10" viewBox="0 0 10 10" width="10">
                <rect
                  fill="none"
                  height="8"
                  stroke="currentColor"
                  strokeWidth="1.2"
                  width="8"
                  x="1"
                  y="1"
                />
              </svg>
            )}
          </Button>
        </Tooltip>

        {/*关闭*/}
        <Tooltip closeDelay={0} content={t("关闭")} delay={300}>
          <Button
            isIconOnly
            aria-label={t("关闭")}
            className="h-8 min-w-8 w-8 rounded bg-transparent data-[hover=true]:bg-red-600 data-[hover=true]:text-white"
            variant="light"
            onClick={quit}
          >
            <svg fill="none" height="10" viewBox="0 0 10 10" width="10">
              <path
                d="M1 1L9 9M1 9L9 1"
                stroke="currentColor"
                strokeLinecap="round"
                strokeWidth="1.2"
              />
            </svg>
          </Button>
        </Tooltip>
      </div>
    </div>
  );
};
