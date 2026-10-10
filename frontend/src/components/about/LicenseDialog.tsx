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
import React from "react";
import { Modal, ModalContent } from "@heroui/react";
import { BookOpen20Regular } from "@fluentui/react-icons";

import { BrowserOpenURL } from "../../../wailsjs/runtime/runtime";
import {
  LICENSES,
  PROJECT_SPDX,
  type LicenseEntry,
  type LicenseGroup,
} from "../../lib/licenses";
import { ModalShell, modalBehaviorProps } from "../modal-shell";
import { t } from "../../i18n";

/** 分组顺序即展示顺序 */
const GROUPS: LicenseGroup[] = ["前端", "后端", "工具链", "外部服务与工具"];

/** 许可证徽章配色：给常见 SPDX 一点区分度，其余走中性色 */
function spdxClass(spdx: string): string {
  if (spdx === "MIT")
    return "bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300";
  if (spdx.startsWith("Apache"))
    return "bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300";

  return "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300";
}

const LicenseRow: React.FC<{ entry: LicenseEntry }> = ({ entry }) => (
  <button
    className="flex w-full cursor-pointer items-center justify-between gap-3 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-gray-100 dark:hover:bg-gray-800/70"
    type="button"
    onClick={() => BrowserOpenURL(entry.url)}
  >
    <span className="min-w-0">
      <span className="block truncate text-sm text-gray-800 dark:text-gray-200">
        {entry.name}
      </span>
      {entry.note ? (
        <span className="block text-[11px] leading-snug text-gray-400">
          {t(entry.note)}
        </span>
      ) : null}
    </span>
    <span
      className={`flex-shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium tabular-nums ${spdxClass(entry.spdx)}`}
    >
      {entry.spdx}
    </span>
  </button>
);

const LicenseDialog: React.FC<{ isOpen: boolean; onClose: () => void }> = ({
  isOpen,
  onClose,
}) => {
  return (
    <Modal isOpen={isOpen} size="lg" onClose={onClose} {...modalBehaviorProps}>
      <ModalContent className="max-h-[85vh]">
        <ModalShell
          icon={<BookOpen20Regular />}
          subtitle={t("NekoLauncher 以 {0} 发布", {
            "0": PROJECT_SPDX,
          })}
          title={t("开源许可")}
          onClose={onClose}
        >
          <div className="space-y-4">
            {GROUPS.map((group) => (
              <div key={group}>
                <div className="mb-1 px-2 text-xs font-medium text-gray-400">
                  {t(group)}
                </div>
                <div className="space-y-0.5">
                  {LICENSES.filter((entry) => entry.group === group).map(
                    (entry) => (
                      <LicenseRow key={entry.name} entry={entry} />
                    ),
                  )}
                </div>
              </div>
            ))}
          </div>
          <div className="mt-4 border-t border-gray-100 px-2 pt-3 text-xs text-gray-400 dark:border-gray-800/60">
            {t(
              "以上为主要依赖；完整清单见仓库根目录的 go.mod 与 frontend/package-lock.json。",
            )}
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default LicenseDialog;
