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
 * 共享分页条（此前下载页 / MC 下载弹层 / 日历卡各自手写了一套）。
 * - compact：左右箭头 + "当前页 / 总页数"，适合大列表底部；
 * - numbers：窗口化数字页码（页码多时省略号收拢），适合弹层内的紧凑列表。
 * 页码一律 1-based；onChange 与 setState 兼容：onChange((p) => p + 1)。
 */
import React from "react";
import { Button } from "@heroui/react";
import {
  ChevronLeft20Regular,
  ChevronRight20Regular,
} from "@fluentui/react-icons";

import { t } from "../i18n";

/** 数字分页的窗口化页码序列；-1 表示省略号 */
export function windowedPages(
  page: number,
  totalPages: number,
  windowSize = 5,
): number[] {
  const all = Array.from({ length: totalPages }, (_, i) => i + 1);

  if (totalPages <= windowSize + 2) return all;
  const half = Math.floor(windowSize / 2);
  const start = Math.max(2, page - half);
  const end = Math.min(totalPages - 1, page + half);
  const pages: number[] = [1];

  if (start > 2) pages.push(-1);
  for (let p = start; p <= end; p++) pages.push(p);
  if (end < totalPages - 1) pages.push(-1);
  pages.push(totalPages);

  return pages;
}

interface PagerProps {
  /** 当前页（1-based） */
  page: number;
  totalPages: number;
  /** 与 setState 同形：onChange((p) => Math.min(totalPages, p + 1)) */
  onChange: (updater: (p: number) => number) => void;
  variant?: "compact" | "numbers";
  className?: string;
}

const Pager: React.FC<PagerProps> = ({
  page,
  totalPages,
  onChange,
  variant = "compact",
  className = "",
}) => {
  // 数字分页只有一页时不占位；compact 保持原有占位行为（禁用箭头）
  if (variant === "numbers" && totalPages <= 1) return null;

  if (variant === "numbers") {
    return (
      <div className={`flex items-center justify-center gap-1.5 ${className}`}>
        {windowedPages(page, totalPages).map((p) =>
          p < 0 ? (
            <span
              key={`ellipsis-${p}`}
              className="px-0.5 text-[12px] text-gray-500"
            >
              …
            </span>
          ) : (
            <button
              key={p}
              className={`h-7 min-w-7 flex-shrink-0 cursor-pointer rounded-lg px-1.5 text-[12px] font-semibold transition-colors ${
                p === page
                  ? "bg-primary-500/15 text-primary-600 dark:text-primary-300"
                  : "text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-800"
              }`}
              onClick={() => onChange(() => p)}
            >
              {p}
            </button>
          ),
        )}
      </div>
    );
  }

  return (
    <div className={`flex items-center justify-center gap-2 pt-1 ${className}`}>
      <Button
        isIconOnly
        aria-label={t("上一页")}
        isDisabled={page <= 1}
        radius="full"
        size="sm"
        variant="flat"
        onPress={() => onChange((p) => Math.max(1, p - 1))}
      >
        <ChevronLeft20Regular />
      </Button>
      <span className="min-w-[64px] text-center text-xs text-gray-400 tabular-nums">
        {page} / {totalPages}
      </span>
      <Button
        isIconOnly
        aria-label={t("下一页")}
        isDisabled={page >= totalPages}
        radius="full"
        size="sm"
        variant="flat"
        onPress={() => onChange((p) => Math.min(totalPages, p + 1))}
      >
        <ChevronRight20Regular />
      </Button>
    </div>
  );
};

export default Pager;
