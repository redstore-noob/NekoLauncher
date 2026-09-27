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
 * NekoAlert：左下滑入的底部警示滑条。图标随级别使用 HeroUI 语义色，
 * 展示中再次触发就地更新，不重播动画。
 */
import React from "react";
import { Dismiss20Regular } from "@fluentui/react-icons";
import { Button } from "@heroui/react";

import { t } from "../../i18n";

import { hideAlertNow, mapSeverity, useOverlayState } from "./state";

const NekoAlert: React.FC = () => {
  const state = useOverlayState();
  const show = state.alertVisible && !!state.alert;
  const severity = mapSeverity(state.alert?.severity);
  const SeverityIcon = severity.Icon;

  return (
    <div
      aria-live="polite"
      className={[
        "nya-panel nya-border fixed bottom-[46px] left-5 z-[940] flex max-w-[min(440px,calc(100vw-2.5rem))] items-start gap-2.5 rounded-2xl border py-3 pl-3.5 pr-2.5 shadow-lg backdrop-blur-md",
        show && !state.alertClosing ? "neko-alert-enter" : "neko-alert-leave",
      ].join(" ")}
      role="alert"
      style={{ visibility: show ? "visible" : "hidden" }}
    >
      <span className={`mt-0.5 flex-none ${severity.text}`}>
        <SeverityIcon />
      </span>
      <span className="min-w-0 flex-1 select-text break-words text-sm leading-5 text-gray-700 dark:text-gray-200">
        {state.alert?.message}
      </span>
      <Button
        isIconOnly
        aria-label={t("关闭")}
        className="-mr-0.5 -mt-0.5 h-6 min-w-6 w-6 rounded-lg bg-transparent text-gray-400 data-[hover=true]:bg-default-100 data-[hover=true]:text-gray-600 dark:data-[hover=true]:bg-default-100/20 dark:data-[hover=true]:text-gray-200"
        radius="none"
        variant="light"
        onClick={hideAlertNow}
      >
        <Dismiss20Regular />
      </Button>
    </div>
  );
};

export default NekoAlert;
