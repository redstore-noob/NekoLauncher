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
 * 设置搜索：等价旧版 settings/search.ts 的「query 下发 + 命中数回传」协议，
 * 适配新版单页分区结构——设置/外观页持有 query 并渲染搜索框；Section 声明
 * 别名参与匹配，未命中的分区整体隐藏，命中数回传页面聚合，全部未命中时
 * 页面显示空态。匹配为大小写不敏感的包含匹配（等价旧版 matchAliases），
 * 分区标题恒参与匹配。
 */
import React, { createContext, useContext } from "react";
import { Input } from "@heroui/react";

import { useI18n } from "../i18n";

export interface SettingsSearchState {
  query: string;
  /** 分区回传命中数（1 命中 / 0 未命中）；仅搜索中调用 */
  report: (key: string, hits: number) => void;
}

export const SettingsSearchContext = createContext<SettingsSearchState | null>(
  null,
);

export function useSettingsSearch(): SettingsSearchState | null {
  return useContext(SettingsSearchContext);
}

/** 等价旧版 matchAliases：大小写不敏感的包含匹配；query 为空时恒命中 */
export function matchAliases(aliases: string[], query: string): boolean {
  const q = (query || "").trim().toLowerCase();

  if (!q) return true;

  return aliases.some((t) => t.toLowerCase().includes(q));
}

/** 页头胶囊搜索条：Esc 清空、可清空按钮，搜索中在左侧显示命中数 */
export const SettingsSearchBox: React.FC<{
  query: string;
  onQueryChange: (value: string) => void;
  placeholder?: string;
  /** 搜索中的命中分区数；null 表示未在搜索（不显示计数） */
  hitCount?: number | null;
}> = ({ query, onQueryChange, placeholder = "搜索设置…", hitCount = null }) => {
  const { t } = useI18n();
  const label = t(placeholder);

  return (
    <div className="flex flex-shrink-0 items-center gap-2">
      {hitCount !== null ? (
        <span className="whitespace-nowrap text-xs text-gray-400">
          {hitCount > 0
            ? t("{count} 个分区命中", { count: hitCount })
            : t("无匹配分区")}
        </span>
      ) : null}
      <Input
        isClearable
        aria-label={label}
        classNames={{
          inputWrapper: "rounded-full bg-gray-100 dark:bg-gray-800/60",
        }}
        placeholder={label}
        size="sm"
        value={query}
        onClear={() => onQueryChange("")}
        onKeyDown={(e) => {
          if (e.key === "Escape") onQueryChange("");
        }}
        onValueChange={onQueryChange}
      />
    </div>
  );
};
