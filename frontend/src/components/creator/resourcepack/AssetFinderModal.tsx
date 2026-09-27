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
/*
 * 资源查找：输入中文名（如「石头」）或英文 id（如 stone）即时筛选原版资源，
 * 点击即创建对应文件（贴图 / 模型 / 方块状态 / 语言…）并在编辑器打开。
 */
import type { CatalogEntry } from "./catalog";

import React, { useEffect, useMemo, useRef, useState } from "react";
import { Button, Input, Modal, ModalContent } from "@heroui/react";
import { Search20Regular } from "@fluentui/react-icons";

import { ModalShell, modalBehaviorProps } from "../../modal-shell";
import { t } from "../../../i18n";

import { CATALOG, searchCatalog } from "./catalog";

interface AssetFinderModalProps {
  isOpen: boolean;
  existingPaths: Set<string>;
  onClose: () => void;
  onPick: (entry: CatalogEntry) => void;
}

const AssetFinderModal: React.FC<AssetFinderModalProps> = ({
  isOpen,
  existingPaths,
  onClose,
  onPick,
}) => {
  const [query, setQuery] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const results = useMemo(() => searchCatalog(query), [query]);

  // 打开后自动聚焦搜索框（不用 autoFocus，避免无障碍告警）
  useEffect(() => {
    if (!isOpen) return;
    const timer = window.setTimeout(() => inputRef.current?.focus(), 50);

    return () => window.clearTimeout(timer);
  }, [isOpen]);

  return (
    <Modal isOpen={isOpen} size="3xl" onClose={onClose} {...modalBehaviorProps}>
      <ModalContent className="h-[70vh] max-h-[70vh]">
        <ModalShell
          icon={<Search20Regular />}
          subtitle={t("点击即创建并在编辑器打开")}
          title={t("资源查找")}
          onClose={onClose}
        >
          <div className="flex h-full min-h-0 flex-col gap-3">
            <Input
              ref={inputRef}
              aria-label={t("搜索资源")}
              classNames={{ inputWrapper: "h-9" }}
              placeholder={t("搜索，如：石头 / 钻石剑 / stone / diamond_sword")}
              size="sm"
              startContent={<Search20Regular className="text-gray-400" />}
              value={query}
              onKeyDown={(event) => {
                if (
                  event.key === "Enter" &&
                  query.trim() &&
                  results.length > 0
                ) {
                  event.preventDefault();
                  onPick(results[0]);
                }
              }}
              onValueChange={setQuery}
            />

            <div className="min-h-0 flex-1 overflow-y-auto">
              {results.length === 0 ? (
                <div className="py-8 text-center text-[12px] text-gray-400">
                  {t("没有匹配「")}
                  {query}
                  {t("」的资源，可回到文件树手填路径")}
                </div>
              ) : (
                <div className="flex flex-col gap-0.5">
                  {results.map((entry) => {
                    const exists = existingPaths.has(entry.path);

                    return (
                      <button
                        key={entry.path}
                        className="flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-default-100 dark:hover:bg-gray-800"
                        type="button"
                        onClick={() => onPick(entry)}
                      >
                        <span className="w-14 flex-none rounded-md bg-default-100 px-1.5 py-0.5 text-center text-[10px] text-gray-500 dark:text-gray-400">
                          {entry.category}
                        </span>
                        <span className="w-28 flex-none truncate text-[12px] font-medium text-gray-700 dark:text-gray-200">
                          {entry.zh}
                        </span>
                        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-gray-400">
                          {entry.path}
                        </span>
                        {exists ? (
                          <span className="flex-none rounded-full bg-primary/15 px-2 py-0.5 text-[10px] text-primary">
                            {t("已存在")}
                          </span>
                        ) : (
                          <span className="flex-none text-[10px] text-gray-400">
                            {entry.kind === "png" ? t("贴图") : t("文本")}
                          </span>
                        )}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>

            <div className="flex flex-none items-center justify-between text-[11px] text-gray-400">
              <span>
                {t("共")} {CATALOG.length} {t("条常用资源，显示")}{" "}
                {results.length} {t("条")}
              </span>
              <Button size="sm" variant="light" onPress={onClose}>
                {t("关闭")}
              </Button>
            </div>
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default AssetFinderModal;
