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
import type { WidgetDefinition } from "../../plugin";

import React, { useMemo, useState } from "react";
import { Button, Input } from "@heroui/react";
import {
  Apps20Regular,
  Checkmark20Regular,
  Dismiss20Regular,
  ReOrderDotsVertical20Regular,
  Search20Regular,
} from "@fluentui/react-icons";

import { t } from "../../i18n";

interface WidgetLibraryProps {
  /** 可添加的组件（页面上还没有的） */
  available: WidgetDefinition[];
  /** 页面上已有的组件 */
  placed: WidgetDefinition[];
  /** 被拖动的组件 id（拖动中条目变淡） */
  draggingId: string | null;
  onClose: () => void;
  /** 条目拖动事件（从 useWidgetDrag 注入） */
  libraryHandlers: (widgetId: string) => {
    onPointerDown: (event: React.PointerEvent) => void;
    onClick: () => void;
  };
}

/** 按关键词匹配组件（标题 + 描述，不区分大小写） */
function matchesQuery(widget: WidgetDefinition, query: string): boolean {
  const keyword = query.trim().toLowerCase();

  if (!keyword) return true;

  return (
    widget.title.toLowerCase().includes(keyword) ||
    widget.description.toLowerCase().includes(keyword)
  );
}

/**
 * 组件盒面板：与右侧启动页同一位置、同一外壳，两者滑动互换。
 * 条目可拖到左侧小组件列添加；直接点击则追加到列表末尾。
 * 支持按名称/描述搜索：可添加的进入列表，已放置的以只读条目提示。
 */
const WidgetLibrary: React.FC<WidgetLibraryProps> = ({
  available,
  placed,
  draggingId,
  onClose,
  libraryHandlers,
}) => {
  const [query, setQuery] = useState("");

  const matchedAvailable = useMemo(
    () => available.filter((widget) => matchesQuery(widget, query)),
    [available, query],
  );
  const matchedPlaced = useMemo(
    () => placed.filter((widget) => matchesQuery(widget, query)),
    [placed, query],
  );
  const searching = query.trim() !== "";

  return (
    <section
      className="
        flex h-full flex-col overflow-hidden rounded-3xl
        border nya-border
        nya-panel backdrop-blur-md shadow-lg
      "
    >
      {/* 面板头 */}
      <div className="flex flex-none items-center gap-3 px-5 pt-5 pb-4">
        <div className="flex size-11 flex-none items-center justify-center rounded-2xl bg-primary/15 text-primary shadow-lg shadow-primary/10">
          <Apps20Regular />
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="overflow-hidden text-base font-bold tracking-tight text-ellipsis whitespace-nowrap">
            {t("组件盒")}
          </span>
          <span className="overflow-hidden text-[11px] text-gray-400 text-ellipsis whitespace-nowrap">
            {t("拖到左侧，或点击添加")}
          </span>
        </div>
        <Button
          isIconOnly
          aria-label={t("关闭组件盒")}
          className="h-8 w-8 min-w-8 text-gray-400"
          radius="full"
          size="sm"
          title={t("关闭组件盒")}
          variant="light"
          onPress={onClose}
        >
          <Dismiss20Regular />
        </Button>
      </div>

      {/* 搜索框 */}
      <div className="flex-none px-4 pb-3">
        <Input
          aria-label={t("搜索组件")}
          classNames={{
            inputWrapper: "bg-default-100/80 data-[hover=true]:bg-default-200",
          }}
          placeholder={t("搜索组件…")}
          radius="full"
          size="sm"
          startContent={<Search20Regular className="text-gray-400" />}
          value={query}
          onValueChange={setQuery}
        />
      </div>

      {/* 可添加组件列表 */}
      <div className="nya-scroll flex-1 overflow-y-auto px-4 pb-4">
        {matchedAvailable.length === 0 ? (
          <div className="flex flex-col items-center gap-2 px-4 py-10 text-center text-gray-400">
            <div className="flex size-12 items-center justify-center rounded-2xl bg-default-100/80">
              <Apps20Regular className="h-6 w-6" />
            </div>
            <span className="text-xs leading-relaxed">
              {searching && matchedPlaced.length > 0
                ? t("匹配的组件都已经添加到页面上了")
                : searching
                  ? t("没有匹配的组件")
                  : t("所有组件都已经在页面上了")}
            </span>
          </div>
        ) : (
          <ul className="space-y-1.5">
            {matchedAvailable.map((widget) => (
              <li key={widget.id}>
                <div
                  {...libraryHandlers(widget.id)}
                  className={`
                    flex cursor-grab items-center gap-3 rounded-2xl px-3 py-2.5
                    transition-all select-none active:cursor-grabbing
                    hover:bg-default-100/80
                    ${draggingId === widget.id ? "opacity-40" : "opacity-100"}
                  `}
                  role="button"
                  style={{ touchAction: "none" }}
                  tabIndex={0}
                  title={t("添加「{0}」", { "0": widget.title })}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      libraryHandlers(widget.id).onClick();
                    }
                  }}
                >
                  <span
                    className={`
                      flex size-9 flex-none items-center justify-center rounded-xl
                      bg-primary/10 text-primary shadow-sm
                    `}
                  >
                    {widget.icon}
                  </span>
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="overflow-hidden text-sm font-medium text-ellipsis whitespace-nowrap">
                      {widget.title}
                    </span>
                    <span className="overflow-hidden text-[11px] text-gray-400 text-ellipsis whitespace-nowrap">
                      {widget.description}
                    </span>
                  </span>
                  <span className="flex-none text-gray-300 dark:text-gray-600">
                    <ReOrderDotsVertical20Regular />
                  </span>
                </div>
              </li>
            ))}
          </ul>
        )}

        {/* 搜索命中但已放置的组件：只读提示 */}
        {searching && matchedPlaced.length > 0 ? (
          <div className="pt-3">
            <div className="px-1 pb-1.5 text-[11px] font-semibold text-gray-400 uppercase tracking-wider">
              {t("已添加")}
            </div>
            <ul className="space-y-1.5">
              {matchedPlaced.map((widget) => (
                <li
                  key={widget.id}
                  className="flex items-center gap-3 rounded-2xl px-3 py-2.5 opacity-60"
                >
                  <span className="flex size-9 flex-none items-center justify-center rounded-xl bg-default-100 text-gray-400">
                    {widget.icon}
                  </span>
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="overflow-hidden text-sm font-medium text-ellipsis whitespace-nowrap">
                      {widget.title}
                    </span>
                    <span className="overflow-hidden text-[11px] text-gray-400 text-ellipsis whitespace-nowrap">
                      {widget.description}
                    </span>
                  </span>
                  <span className="flex-none text-primary">
                    <Checkmark20Regular />
                  </span>
                </li>
              ))}
            </ul>
          </div>
        ) : null}

        {!searching && placed.length > 0 ? (
          <div className="px-1 pt-3 text-center text-[11px] text-gray-400">
            {t("页面上已有")} {placed.length}{" "}
            {t("个组件 · 长按 1 秒排序，拖回此处即可移除")}
          </div>
        ) : null}
      </div>
    </section>
  );
};

export default WidgetLibrary;
