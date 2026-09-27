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
import React, { useEffect, useId } from "react";

import {
  matchAliases,
  useSettingsSearch,
} from "../../components/settings-search";
import { useI18n } from "../../i18n";

interface SectionProps {
  title: string;
  /** 搜索别名：除标题外可命中本分区的关键词（等价旧版设置卡片的 aliases） */
  aliases?: string[];
  children: React.ReactNode;
}

const Section: React.FC<SectionProps> = ({ title, aliases, children }) => {
  const { t } = useI18n();
  const search = useSettingsSearch();
  const searchKey = useId();
  const searching = !!search && search.query.trim().length > 0;
  const titleText = t(title);
  // 标题按当前语言参与匹配；别名保留原文，兼容中文搜索
  const visible =
    !searching || matchAliases([titleText, ...(aliases ?? [])], search.query);

  // 命中数回传页面聚合（等价旧版子页 setCount）；卸载时清零防残留计数
  useEffect(() => {
    if (!search || !searching) return;
    search.report(searchKey, visible ? 1 : 0);

    return () => search.report(searchKey, 0);
  }, [search, searching, searchKey, visible]);

  if (!visible) return null;

  return (
    <section>
      <div className="mb-3">
        <h2 className="text-md font-semibold text-black dark:text-gray-400">
          {titleText}
        </h2>
      </div>
      <div className="space-y-3">{children}</div>
    </section>
  );
};

export default Section;

export const SettingRow: React.FC<{
  label: string;
  hint?: string;
  children: React.ReactNode;
}> = ({ label, hint, children }) => {
  const { t } = useI18n();

  // flex-wrap：窄窗口时右侧控件换行而不是把固定宽度的输入框顶出边界
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 py-2">
      <div className="min-w-0">
        <div className="text-sm text-gray-800 dark:text-gray-200">
          {t(label)}
        </div>
        {hint && <div className="text-xs text-gray-400 mt-0.5">{t(hint)}</div>}
      </div>
      <div className="flex-shrink-0 max-w-full">{children}</div>
    </div>
  );
};
