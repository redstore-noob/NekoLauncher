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
import type { ModalProps } from "@heroui/react";

import React from "react";
import { ModalBody, ModalHeader } from "@heroui/react";
import { Dismiss16Regular } from "@fluentui/react-icons";

import { t } from "../i18n";

// 统一的弹窗外壳：标题行 + 内嵌右上角关闭按钮 + 固定内边距的内容区。
//
// 为什么不用 HeroUI 内建的关闭按钮：它是绝对定位在弹窗右上角（top-4），
// 与标题文字不在一条水平线上，视觉上"悬"在角上（错位观感的主要来源之一）。
// 这里把关闭钮放进标题 flex 行内，与标题垂直居中、与内容区右缘对齐。
//
// 使用约定：
//   <Modal isOpen={...} onClose={onClose} size="md" hideCloseButton
//          isDismissable={false} placement="center">
//     <ModalContent>
//       <ModalShell title="..." onClose={onClose}>...</ModalShell>
//     </ModalContent>
//   </Modal>
// closeGuard：忙碌时禁止关闭（点 X 无效），缺省为允许。
interface ModalShellProps {
  title: string;
  subtitle?: string;
  /** 标题行左侧图标（可选，下载弹层的方形图标） */
  icon?: React.ReactNode;
  /** 标题行右侧的附加内容（如版本类型徽章），位于标题文字与关闭钮之间 */
  titleExtra?: React.ReactNode;
  onClose: () => void;
  /** 返回 true 表示当前允许关闭；缺省始终允许 */
  closeGuard?: () => boolean;
  children: React.ReactNode;
}

const CloseIcon: React.FC = () => <Dismiss16Regular />;

export const ModalShell: React.FC<ModalShellProps> = ({
  title,
  subtitle,
  icon,
  titleExtra,
  onClose,
  closeGuard,
  children,
}) => {
  const requestClose = () => {
    if (!closeGuard || closeGuard()) onClose();
  };

  return (
    <>
      <ModalHeader className="flex items-center gap-3 px-6 pt-4 pb-3 border-b border-gray-100 dark:border-gray-800/60">
        {icon ? (
          <div className="w-10 h-10 flex-shrink-0 flex items-center justify-center rounded-[10px] bg-primary/15 text-primary-600 dark:text-primary-300">
            {icon}
          </div>
        ) : null}
        <div className="min-w-0 flex-1">
          <div className="text-base font-semibold text-gray-800 dark:text-gray-200 truncate">
            {title}
          </div>
          {subtitle ? (
            <div className="text-xs font-normal text-gray-400 truncate">
              {subtitle}
            </div>
          ) : null}
        </div>
        {titleExtra ? <div className="flex-shrink-0">{titleExtra}</div> : null}
        <button
          aria-label={t("关闭")}
          className="w-8 h-8 flex-shrink-0 flex items-center justify-center rounded-lg text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors cursor-pointer"
          title={t("关闭")}
          onClick={requestClose}
        >
          <CloseIcon />
        </button>
      </ModalHeader>
      {/* min-h-0 flex-1：固定高度的业务弹窗（如整合包制作）靠它撑满内部网格；
          内容较短的弹窗不受影响（ModalContent 高度仍由内容决定）。
          overflow-y-auto：半窗高（50vh）的下载类弹窗内容超出时在体内滚动。 */}
      <ModalBody className="min-h-0 flex-1 overflow-y-auto px-6 pb-5 pt-4">
        {children}
      </ModalBody>
    </>
  );
};

// ModalProps 的公共片段：全部业务弹窗统一使用。
// isDismissable=false 防止误点外部关闭；scrollBehavior="inside" 让弹窗高于
// 视口时在体内滚动，而不是被 overflow-hidden 裁掉底部按钮。
//
// 闪屏治理（遮罩层）：
// - backdrop="blur"：遮罩从纯黑半透明改为毛玻璃，不再是"糊一层黑"的观感。
// - disableAnimation：关闭 HeroUI 内建 framer 动画。此前遮罩是 framer 逐帧
//   JS 驱动的 0.4s 淡入（面板 initial opacity:0 由 JS 弹簧入场），在本项目
//   的 WebView2 环境里偶发"事件丢失"导致卡在透明态/隐形遮罩（同 screen-
//   transition.tsx 记录的问题），且大块遮罩的 JS 淡入本身就表现为闪屏。
//   入场动画改由 globals.css 的 CSS keyframes 驱动（.nya-modal-backdrop /
//   .nya-modal-enter），浏览器渲染引擎执行、不会丢帧；关闭即时卸载。
// - classNames.base 的 .nya-modal-surface 给面板本体毛玻璃表面（覆盖
//   HeroUI 默认的不透明 bg-background）。
export const modalBehaviorProps: Partial<ModalProps> = {
  hideCloseButton: true,
  isDismissable: false,
  placement: "center",
  scrollBehavior: "inside",
  backdrop: "blur",
  disableAnimation: true,
  classNames: {
    backdrop: "nya-modal-backdrop",
    base: "nya-modal-surface nya-modal-enter",
  },
};
