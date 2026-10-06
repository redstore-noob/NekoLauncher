/*
 * 高级设置折叠组：设置页各分块独立收纳"高深"内容的可折叠容器。
 *
 * - 展开状态按 id 分别持久化在 localStorage，各分块互不影响
 * - 设置搜索进行中强制展开：折叠的内容也要能被搜到、被看到
 * - 纯 UI 组件：不关心内容里的表单状态，折叠只是隐藏而非卸载逻辑
 */
import React, { useEffect, useState } from "react";
import { ChevronDown20Regular } from "@fluentui/react-icons";

import { useSettingsSearch } from "../../components/settings-search";
import { useI18n } from "../../i18n";
import { useSimpleMode } from "../simple-mode";

const storageKey = (id: string) => `nya-settings-advanced-${id}`;

interface AdvancedGroupProps {
  /** 持久化键：每个分块用不同 id，展开状态互不影响 */
  id: string;
  /** 折叠头标题，缺省「高级设置」 */
  label?: string;
  /** 折叠头右侧的灰色说明（收起时提示里面有什么） */
  hint?: string;
  /** 外部条件成立时强制展开（如代理选到「自定义」），不受折叠状态影响 */
  forceOpen?: boolean;
  children: React.ReactNode;
}

const AdvancedGroup: React.FC<AdvancedGroupProps> = ({
  id,
  label,
  hint,
  forceOpen,
  children,
}) => {
  const { t } = useI18n();
  const search = useSettingsSearch();
  const searching = !!search && search.query.trim().length > 0;
  // S 模式面向低龄玩家：高级设置整组隐藏（连折叠头都不出现）
  const { simpleMode } = useSimpleMode();
  const [open, setOpen] = useState(
    () => localStorage.getItem(storageKey(id)) === "1",
  );

  useEffect(() => {
    localStorage.setItem(storageKey(id), open ? "1" : "0");
  }, [id, open]);

  if (simpleMode) return null;

  const expanded = open || searching || !!forceOpen;

  return (
    <div className="nya-border rounded-lg border bg-default-50/40 px-3.5 dark:bg-white/[0.03]">
      <button
        aria-expanded={expanded}
        className="flex w-full cursor-pointer items-center gap-1.5 py-2.5 text-left"
        type="button"
        onClick={() => setOpen((v) => !v)}
      >
        <ChevronDown20Regular
          className={`h-4 w-4 flex-none text-gray-400 transition-transform ${
            expanded ? "" : "-rotate-90"
          }`}
        />
        <span className="flex-none text-sm font-medium text-gray-700 dark:text-gray-300">
          {t(label ?? "高级设置")}
        </span>
        {hint && !expanded ? (
          <span className="min-w-0 flex-1 truncate text-xs text-gray-400">
            {t(hint)}
          </span>
        ) : null}
      </button>
      {expanded ? <div className="pb-2.5">{children}</div> : null}
    </div>
  );
};

export default AdvancedGroup;
