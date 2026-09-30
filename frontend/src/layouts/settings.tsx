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

import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Button } from "@heroui/react";

import {
  SettingsSearchBox,
  SettingsSearchContext,
} from "../components/settings-search";
import { useI18n } from "../i18n";
import { consumePendingDetail, onNavigate } from "../lib/navigation";

import GameDirectorySection from "./settings/GameDirectorySection";
import BehaviorSection from "./settings/BehaviorSection";
import JavaSection from "./settings/JavaSection";
import LaunchSection from "./settings/LaunchSection";
import MemorySection from "./settings/MemorySection";
import DownloadSection from "./settings/DownloadSection";
import NetworkSection from "./settings/NetworkSection";
import AboutSection from "./settings/AboutSection";
import { SIMPLE_MODE_HIDDEN_SETTINGS_IDS, useSimpleMode } from "./simple-mode";

const SettingsPage: React.FC = () => {
  const { t } = useI18n();
  const { simpleMode } = useSimpleMode();
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<Record<string, number>>({});
  const searching = query.trim().length > 0;
  const total = Object.values(hits).reduce((sum, n) => sum + n, 0);
  const report = useCallback((key: string, n: number) => {
    setHits((prev) => (prev[key] === n ? prev : { ...prev, [key]: n }));
  }, []);
  const searchValue = useMemo(() => ({ query, report }), [query, report]);
  // S 模式隐藏的硬核分区（游戏目录 / Java / 下载 / 网络），见 simple-mode.tsx
  const hiddenSections = useMemo(
    () =>
      simpleMode ? new Set<string>(SIMPLE_MODE_HIDDEN_SETTINGS_IDS) : null,
    [simpleMode],
  );

  // 主页网络状态卡片等请求定位到下载设置；帮助页会请求定位到 Java 分区。
  // 两个来源：页面已挂载 → 走导航总线实时事件；跨页跳过来 → 读挂载时暂存的 detail
  useEffect(() => {
    const scrollTo = (anchorId: string) => {
      document
        .getElementById(anchorId)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
    };
    const anchorFor = (detail: string | undefined) =>
      detail === "download"
        ? "settings-download"
        : detail === "java"
          ? "settings-java"
          : "";

    const pending = consumePendingDetail("settings");

    if (anchorFor(pending)) scrollTo(anchorFor(pending));

    return onNavigate((request) => {
      if (request.pageId !== "settings") return;
      const anchor = anchorFor(request.detail);

      if (anchor) scrollTo(anchor);
    });
  }, []);

  return (
    <SettingsSearchContext.Provider value={searchValue}>
      <div className="h-full w-full overflow-y-auto">
        <div className="max-w-2xl mx-auto px-6 py-8 space-y-10">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h1 className="text-xl font-semibold text-gray-800 dark:text-gray-200">
              {t("设置")}
            </h1>
            <SettingsSearchBox
              hitCount={searching ? total : null}
              query={query}
              onQueryChange={setQuery}
            />
          </div>
          {searching && total === 0 ? (
            <div className="py-16 text-center">
              <div className="text-sm text-gray-400">
                {t("没有找到与「{query}」相关的设置", { query: query.trim() })}
              </div>
              <Button
                className="mt-3"
                size="sm"
                variant="flat"
                onPress={() => setQuery("")}
              >
                {t("清除搜索")}
              </Button>
            </div>
          ) : (
            <>
              {/* 非搜索态下分区依次入场（stagger）；搜索过滤时立即显示不重播。
                  带 id 的先按分区 id 过滤再入场：S 模式下被隐藏的分区不渲染，
                  既不参与搜索命中计数，也不留下空的入场槽位 */}
              {[
                { id: "game-directory", node: <GameDirectorySection /> },
                { id: "behavior", node: <BehaviorSection /> },
                {
                  id: "java",
                  node: (
                    <div id="settings-java">
                      <JavaSection />
                    </div>
                  ),
                },
                { id: "launch", node: <LaunchSection /> },
                { id: "memory", node: <MemorySection /> },
                {
                  id: "download",
                  node: (
                    <div id="settings-download">
                      <DownloadSection />
                    </div>
                  ),
                },
                { id: "network", node: <NetworkSection /> },
                { id: "about", node: <AboutSection /> },
              ]
                .filter((section) => !hiddenSections?.has(section.id))
                .map((section, index) => (
                  <div
                    key={section.id}
                    className={searching ? "" : "nya-enter"}
                    style={{
                      animationDelay: `${index * 60}ms`,
                    }}
                  >
                    {section.node}
                  </div>
                ))}
            </>
          )}
        </div>
      </div>
    </SettingsSearchContext.Provider>
  );
};

export default SettingsPage;
