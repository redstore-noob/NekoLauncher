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
 * 全局浮层门面（NekoAlert / NekoPrompt 的对外 API）。
 * 宿主由 <OverlayHost /> 挂载（App.tsx 中已挂）；无需 import 组件即可调用。
 *
 * 用法：
 *   import { alert, confirm, promptDialog, showDialogBox } from '@/components/overlay/dialog'
 *   alert('已保存');                              // 信息警示
 *   alert('失败', { severity: 'error' });
 *   if (await confirm('删除账户', '该操作不可恢复')) { ... }
 *   const name = await promptDialog('新建账户', '请输入名称', { defaultValue: 'Player_01' });
 *   const id = await showDialogBox({ title, message, severity, buttons: [{label:'甲'}, {label:'乙', default:true}] });
 */
import { t } from "../../i18n";

import {
  hideAlertNow,
  showAlert,
  showDialog,
  type DialogButton,
  type DialogRequest,
} from "./state";

export { hideAlertNow };

/** 底部警示滑条。severity: 'info' | 'success' | 'warning' | 'error'。 */
export function alert(
  message: string,
  options: { severity?: string; duration?: number } = {},
) {
  showAlert(message, options);
}

/** alert 的语义化快捷键。 */
export const notify = {
  info: (message: string, duration?: number) =>
    showAlert(message, { severity: "info", duration }),
  success: (message: string, duration?: number) =>
    showAlert(message, { severity: "success", duration }),
  warning: (message: string, duration?: number) =>
    showAlert(message, { severity: "warning", duration }),
  error: (message: string, duration?: number) =>
    showAlert(message, { severity: "error", duration }),
};

/**
 * 确认对话框，resolve true（确认） / false（取消）。
 * 默认 warning 级别。
 */
export function confirm(
  title: string,
  message: string,
  {
    confirmLabel = t("确认"),
    cancelLabel = t("取消"),
    severity = "warning",
  }: {
    confirmLabel?: string;
    cancelLabel?: string;
    severity?: string;
  } = {},
): Promise<boolean> {
  return showDialog({
    title,
    message,
    severity,
    buttons: [
      { label: cancelLabel, id: "cancel" },
      { label: confirmLabel, id: "ok", default: true },
    ],
  }).then((id) => id === "ok");
}

/**
 * 文本输入对话框（window.prompt 的替代品）。
 * resolve 输入字符串（取消时为 null）。
 */
export function promptDialog(
  title: string,
  message: string,
  {
    defaultValue = "",
    placeholder = "",
  }: { defaultValue?: string; placeholder?: string } = {},
): Promise<string | null> {
  return showDialog({
    title,
    message,
    severity: "info",
    input: { value: defaultValue, placeholder },
    buttons: [
      { label: t("取消"), id: "cancel" },
      { label: t("确认"), id: "ok", default: true },
    ],
  }).then((result) => {
    if (!result || (result as { id?: string }).id !== "ok") return null;

    return (result as { value?: string }).value ?? "";
  });
}

/**
 * 通用对话框（NekoPrompt.ShowAsync）。
 * resolve 被点击按钮的 id；被新对话框顶掉时 resolve null。
 */
export function showDialogBox(
  request: DialogRequest,
): Promise<string | { id: string; value: string } | null> {
  return showDialog(request) as Promise<
    string | { id: string; value: string } | null
  >;
}

export type { DialogButton, DialogRequest };
