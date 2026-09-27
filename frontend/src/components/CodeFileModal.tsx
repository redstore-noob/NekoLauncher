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
 * 文件查看 / 编辑遮罩弹层：毛玻璃 Modal + 轻量代码编辑器
 * （components/creator/resourcepack/CodeEditor，语法高亮 + 补全）。
 * 语言按文件名自动推断（json / yml / properties 等，见 code.ts），
 * 有修改未保存时右上角 X 与关闭均先确认。首次载入内容留档用于脏判定。
 */
import React, { useEffect, useMemo, useState } from "react";
import { Modal, ModalContent } from "@heroui/react";
import { DocumentEdit20Regular } from "@fluentui/react-icons";

import { t } from "../i18n";

import CodeEditor from "./creator/resourcepack/CodeEditor";
import { detectLanguage } from "./creator/resourcepack/code";
import { ModalShell, modalBehaviorProps } from "./modal-shell";
import { confirm } from "./overlay/dialog";

interface CodeFileModalProps {
  isOpen: boolean;
  /** 相对路径文件名（标题展示 + 语言推断 + 弹层复用 key） */
  fileName: string;
  value: string;
  onValueChange: (value: string) => void;
  onClose: () => void;
  /** 保存到后端；抛错时由调用方提示（弹层自身不改内容） */
  onSave: () => Promise<void> | void;
}

const CodeFileModal: React.FC<CodeFileModalProps> = ({
  isOpen,
  fileName,
  value,
  onValueChange,
  onClose,
  onSave,
}) => {
  const language = useMemo(() => detectLanguage(fileName), [fileName]);
  // 弹层每次打开（或换文件，调用方用 key 重挂载）记录初始内容做脏判定
  const [initial, setInitial] = useState(value);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (isOpen) setInitial(value);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen, fileName]);

  const dirty = value !== initial;

  const requestClose = async () => {
    if (dirty) {
      const ok = await confirm(
        t("放弃修改？"),
        t("文件有未保存的修改，关闭后将丢失。"),
        { confirmLabel: t("放弃修改"), cancelLabel: t("继续编辑") },
      );

      if (!ok) return;
    }
    onClose();
  };

  const save = async () => {
    setSaving(true);
    try {
      await onSave();
      setInitial(value);
    } finally {
      setSaving(false);
    }
  };

  const languageLabel: Record<string, string> = {
    json: "JSON",
    yaml: "YAML",
    kv: t("配置"),
    plain: t("文本"),
  };

  return (
    <Modal
      isOpen={isOpen}
      size="4xl"
      onClose={() => {
        if (!dirty) onClose();
        else void requestClose();
      }}
      {...modalBehaviorProps}
    >
      <ModalContent>
        <ModalShell
          closeGuard={() => !dirty}
          icon={<DocumentEdit20Regular />}
          subtitle={`${languageLabel[language]} · ${dirty ? t("有未保存的修改") : t("未修改")}`}
          title={fileName}
          onClose={() => {
            if (!dirty) onClose();
            else void requestClose();
          }}
        >
          <div className="flex h-[62vh] min-h-0 flex-col gap-2">
            <CodeEditor
              language={language}
              value={value}
              onChange={onValueChange}
            />
            <div className="flex flex-none items-center justify-between gap-2">
              <span className="text-[11px] text-gray-400">
                {t("Tab 补全 / 缩进 · 语法高亮按文件类型自动启用")}
              </span>
              <div className="flex gap-1.5">
                <button
                  className="cursor-pointer rounded-lg px-3 py-1.5 text-xs text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-gray-800 dark:hover:text-gray-300"
                  type="button"
                  onClick={() => void requestClose()}
                >
                  {t("关闭")}
                </button>
                <button
                  className={`cursor-pointer rounded-lg px-3 py-1.5 text-xs text-white transition-colors ${
                    dirty && !saving
                      ? "bg-primary hover:opacity-90"
                      : "cursor-default bg-primary/40"
                  }`}
                  disabled={!dirty || saving}
                  type="button"
                  onClick={() => void save()}
                >
                  {saving ? t("保存中…") : t("保存文件")}
                </button>
              </div>
            </div>
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default CodeFileModal;
