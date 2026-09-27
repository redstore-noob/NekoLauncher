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
 * 全局浮层状态（NekoAlert / NekoPrompt 的共享 store）。
 *
 * 同一时刻只有一条警示与一个提示框；新请求顶掉旧请求（旧 Promise 以 null 完成）。
 * 用 useSyncExternalStore 的轻量订阅实现，组件外也可直接调用 showAlert / showDialog。
 * 警示的出入场过渡由 alertVisible 驱动（NekoAlert 切 CSS animation class）。
 */
import { type ComponentType, useSyncExternalStore } from "react";
import {
  CheckmarkCircle20Regular,
  ErrorCircle20Regular,
  Info20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";

import { t } from "../../i18n";

export interface DialogButton {
  label: string;
  id?: string;
  default?: boolean;
}

export interface DialogRequest {
  title?: string;
  message?: string;
  severity?: string;
  buttons?: DialogButton[];
  input?: { value?: string; placeholder?: string } | null;
}

export interface DialogState {
  title: string;
  message: string;
  severity: string;
  buttons: Required<DialogButton>[];
  input: { value: string; placeholder: string } | null;
}

export interface OverlayState {
  /** 当前警示：{ message, severity } 或 null */
  alert: { message: string; severity: string } | null;
  /** 警示卡片可见性（出入场过渡用） */
  alertVisible: boolean;
  /** 警示正在退场（应用滑出动画，结束后再隐藏） */
  alertClosing: boolean;
  /** 当前提示框请求 */
  dialog: DialogState | null;
  dialogVisible: boolean;
}

export interface SeverityStyle {
  Icon: ComponentType;
  /** 图标 / 文字的 HeroUI 语义色 class */
  text: string;
  /** 警示左侧强调条的背景色 class */
  bar: string;
}

const SEVERITY_STYLES: Record<string, SeverityStyle> = {
  success: {
    Icon: CheckmarkCircle20Regular,
    text: "text-success",
    bar: "bg-success",
  },
  warning: {
    Icon: Warning20Regular,
    text: "text-warning",
    bar: "bg-warning",
  },
  error: {
    Icon: ErrorCircle20Regular,
    text: "text-danger",
    bar: "bg-danger",
  },
  info: { Icon: Info20Regular, text: "text-primary", bar: "bg-primary" },
};

/** 级别 → 图标 + HeroUI 语义色。 */
export function mapSeverity(severity?: string): SeverityStyle {
  return SEVERITY_STYLES[severity ?? ""] ?? SEVERITY_STYLES.info;
}

let overlayState: OverlayState = {
  alert: null,
  alertVisible: false,
  alertClosing: false,
  dialog: null,
  dialogVisible: false,
};

const listeners = new Set<() => void>();

function set(partial: Partial<OverlayState>) {
  overlayState = { ...overlayState, ...partial };
  listeners.forEach((listener) => listener());
}

/** React 绑定：组件里 const s = useOverlayState() */
export function useOverlayState(): OverlayState {
  return useSyncExternalStore(
    (callback) => {
      listeners.add(callback);

      return () => listeners.delete(callback);
    },
    () => overlayState,
    () => overlayState,
  );
}

let alertTimer: ReturnType<typeof setTimeout> | undefined;
let alertGeneration = 0;
let dialogResolver: ((result: unknown) => void) | null = null;
let dialogClosing = false;
let dialogGeneration = 0;

const SEVERITIES = ["info", "success", "warning", "error"];

/**
 * 展示一条底部警示滑条（NekoAlert.Info/Success/Warning/Error）。
 * 展示中再次触发：就地换文案与配色并重置倒计时，不重播滑入动画。
 */
export function showAlert(
  message: string,
  {
    severity = "info",
    duration = 4000,
  }: { severity?: string; duration?: number } = {},
) {
  const wasHiding = overlayState.alertClosing;

  alertGeneration++;
  clearTimeout(alertTimer);

  set({ alert: { message, severity }, alertClosing: false });
  if (overlayState.alertVisible && !wasHiding) {
    restartAutoHide(duration);

    return;
  }
  set({ alertVisible: true });
  restartAutoHide(duration);
}

function restartAutoHide(duration: number) {
  const generation = alertGeneration;

  clearTimeout(alertTimer);
  alertTimer = setTimeout(() => {
    if (generation === alertGeneration) hideAlertNow();
  }, duration);
}

/** 立即收回警示（点关闭或到点）。 */
export function hideAlertNow() {
  clearTimeout(alertTimer);
  if (!overlayState.alertVisible || overlayState.alertClosing) return;
  // 先播放 200ms 滑出动画，再真正隐藏（对应原版 AnimateOutAsync）
  const generation = alertGeneration;

  set({ alertClosing: true });
  setTimeout(() => {
    if (generation !== alertGeneration) return;
    set({ alertVisible: false, alertClosing: false });
  }, 200);
}

/**
 * 展示提示对话框（NekoPrompt.ShowAsync）。
 * resolve：被点击按钮的 id（有输入框时为 { id, value }）；被新提示顶掉时 resolve(null)。
 */
export function showDialog(request: DialogRequest): Promise<unknown> {
  dialogGeneration++;
  if (dialogResolver) {
    const old = dialogResolver;

    dialogResolver = null;
    old(null);
  }
  dialogClosing = false;
  const buttons = request.buttons?.length
    ? request.buttons
    : [{ label: t("好的"), id: "ok", default: true }];

  set({
    dialog: {
      title: request.title ?? "",
      message: request.message ?? "",
      severity: SEVERITIES.includes(request.severity ?? "")
        ? (request.severity as string)
        : "info",
      buttons: buttons.map((button, index) => ({
        label: button.label,
        id: button.id ?? button.label,
        default: button.default ?? index === buttons.length - 1,
      })),
      input: request.input
        ? {
            value: request.input.value ?? "",
            placeholder: request.input.placeholder ?? "",
          }
        : null,
    },
    dialogVisible: true,
  });

  return new Promise((resolve) => {
    dialogResolver = resolve;
  });
}

/** 完成对话框（按钮点击时由 NekoPrompt 调用）。 */
export function completeDialog(result: unknown) {
  if (dialogClosing || !dialogResolver) return;
  dialogClosing = true;
  const resolve = dialogResolver;
  const generation = dialogGeneration;

  dialogResolver = null;
  // 先关（触发退场动画），待动画结束再清空内容；期间新对话框会顶掉旧内容
  set({ dialogVisible: false });
  resolve(result);
  setTimeout(() => {
    if (generation !== dialogGeneration) return;
    set({ dialog: null });
  }, 220);
}
