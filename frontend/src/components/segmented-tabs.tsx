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
/*
 * 胶囊分段切换（tab）：选中态是一个共享 layoutId 的主色滑块，切换时
 * 借 framer-motion 的 FLIP 在各选项之间平滑滑动（观感对齐 HeroUI Tabs
 * 的 cursor 指示条）。外壳容器与按钮布局类由调用方传入，保持各页面
 * 原有胶囊样式不变；同页多组实例时 layoutId 需各自唯一。
 * 滑块动画挂起的最坏情形只是滑块瞬移，不影响交互。
 */
import React from "react";
import { motion } from "framer-motion";

export interface SegmentedTabItem {
  key: string;
  label: React.ReactNode;
}

interface SegmentedTabsProps {
  items: readonly SegmentedTabItem[];
  value: string;
  onChange: (value: string) => void;
  /** 滑块的 layoutId，同一渲染树内多组分段控件时需各自唯一 */
  layoutId: string;
  /** 外壳胶囊容器的 className（圆角 / 底色 / 边框） */
  className?: string;
  /** 单个按钮的布局类（宽度 / 内距 / 字号；配色由组件统一给出） */
  itemClassName?: string;
  disabled?: boolean;
}

export const SegmentedTabs: React.FC<SegmentedTabsProps> = ({
  items,
  value,
  onChange,
  layoutId,
  className,
  itemClassName,
  disabled,
}) => (
  <div className={className}>
    {items.map((item) => {
      const active = item.key === value;

      return (
        <button
          key={item.key}
          aria-pressed={active}
          className={`relative flex cursor-pointer items-center justify-center rounded-full transition-colors ${
            itemClassName ?? "px-3.5 py-1.5 text-[13px]"
          } ${
            active
              ? "font-semibold text-white"
              : "text-gray-600 dark:text-gray-300 hover:bg-default-100/60 hover:text-gray-800 dark:hover:text-gray-100"
          }`}
          disabled={disabled}
          type="button"
          onClick={() => onChange(item.key)}
        >
          {active ? (
            <motion.span
              className="absolute inset-0 rounded-full bg-primary shadow-md shadow-primary/25"
              layoutId={layoutId}
              transition={{ type: "spring", bounce: 0.25, duration: 0.5 }}
            />
          ) : null}
          <span className="relative z-10 flex items-center gap-1.5">
            {item.label}
          </span>
        </button>
      );
    })}
  </div>
);

export default SegmentedTabs;
