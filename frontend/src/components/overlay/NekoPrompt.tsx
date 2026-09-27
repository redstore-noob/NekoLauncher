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
 * NekoPrompt：全局提示对话框，沿用本应用统一的 ModalShell 外壳（HeroUI Modal）。
 * 标题行带级别图标，正文 + 可选文本输入，底部右对齐按钮；关闭（X / Esc）
 * 等价于取消（resolve null）。不点按钮不会误关（禁用外部点击）。
 */
import type { DialogButton } from "./state";

import React, { useEffect, useRef, useState } from "react";
import { Button, Input, Modal, ModalContent } from "@heroui/react";

import { ModalShell, modalBehaviorProps } from "../modal-shell";
import { t } from "../../i18n";

import { completeDialog, mapSeverity, useOverlayState } from "./state";

const NekoPrompt: React.FC = () => {
  const state = useOverlayState();
  const open = state.dialogVisible && !!state.dialog;
  const inputRef = useRef<HTMLInputElement>(null);
  const [inputValue, setInputValue] = useState("");
  const severity = mapSeverity(state.dialog?.severity);
  const SeverityIcon = severity.Icon;

  useEffect(() => {
    setInputValue(state.dialog?.input?.value ?? "");
    if (state.dialog?.input && open) {
      // 等 Modal 内容挂载后聚焦输入框
      const timer = window.setTimeout(() => inputRef.current?.focus(), 30);

      return () => window.clearTimeout(timer);
    }

    return undefined;
  }, [state.dialog, open]);

  const choose = (button: DialogButton) => {
    completeDialog(
      state.dialog?.input ? { id: button.id, value: inputValue } : button.id,
    );
  };

  const submit = () => {
    const dialog = state.dialog;
    const target =
      dialog?.buttons.find((button) => button.default) ?? dialog?.buttons[0];

    if (target) choose(target);
  };

  return (
    <Modal
      isOpen={open}
      size="md"
      onClose={() => completeDialog(null)}
      {...modalBehaviorProps}
    >
      <ModalContent>
        <ModalShell
          icon={
            <span className={severity.text}>
              <SeverityIcon />
            </span>
          }
          title={state.dialog?.title ?? ""}
          onClose={() => completeDialog(null)}
        >
          <div className="flex flex-col gap-4 py-1">
            {state.dialog?.message ? (
              <p className="select-text whitespace-pre-wrap break-words text-sm leading-6 text-gray-600 dark:text-gray-300">
                {state.dialog.message}
              </p>
            ) : null}

            {state.dialog?.input ? (
              <Input
                ref={inputRef}
                aria-label={state.dialog.title || t("输入")}
                placeholder={state.dialog.input.placeholder || ""}
                size="sm"
                value={inputValue}
                onKeyDown={(event) => {
                  if (event.key === "Enter") submit();
                }}
                onValueChange={setInputValue}
              />
            ) : null}

            <div className="flex flex-wrap justify-end gap-2">
              {(state.dialog?.buttons ?? []).map((button) => (
                <Button
                  key={button.id}
                  color={button.default ? "primary" : "default"}
                  size="sm"
                  variant={button.default ? "solid" : "flat"}
                  onPress={() => choose(button)}
                >
                  {button.label}
                </Button>
              ))}
            </div>
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default NekoPrompt;
