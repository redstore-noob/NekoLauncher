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
import React, { useCallback, useMemo, useState } from "react";
import { Button } from "@heroui/react";

import {
  SettingsSearchBox,
  SettingsSearchContext,
} from "../../components/settings-search";
import { useI18n } from "../../i18n";

import AppearanceSection from "./AppearanceSection";
import SidebarSection from "./SidebarSection";

/** 外观页：启动器的视觉与侧边栏个性化（自设置页拆出），受保护不可隐藏 */
const AppearancePage: React.FC = () => {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<Record<string, number>>({});
  const searching = query.trim().length > 0;
  const total = Object.values(hits).reduce((sum, n) => sum + n, 0);
  const report = useCallback((key: string, n: number) => {
    setHits((prev) => (prev[key] === n ? prev : { ...prev, [key]: n }));
  }, []);
  const searchValue = useMemo(() => ({ query, report }), [query, report]);

  return (
    <SettingsSearchContext.Provider value={searchValue}>
      <div className="h-full w-full overflow-y-auto">
        <div className="max-w-2xl mx-auto px-6 py-8 space-y-10">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h1 className="text-xl font-semibold text-gray-800 dark:text-gray-200">
              {t("外观")}
            </h1>
            <SettingsSearchBox
              hitCount={searching ? total : null}
              placeholder={t("搜索外观设置…")}
              query={query}
              onQueryChange={setQuery}
            />
          </div>
          {searching && total === 0 ? (
            <div className="py-16 text-center">
              <div className="text-sm text-gray-400">
                {t("没有找到与「{query}」相关的外观设置", {
                  query: query.trim(),
                })}
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
              <AppearanceSection />
              <SidebarSection />
            </>
          )}
        </div>
      </div>
    </SettingsSearchContext.Provider>
  );
};

export default AppearancePage;
