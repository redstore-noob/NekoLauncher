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
/*
 * 全站统一的 HeroUI 动效预设。
 *
 * HeroUI 的弹出类组件（Modal / Popover / Dropdown / Tooltip / Select /
 * Autocomplete）都通过 `motionProps`（framer-motion 的 HTMLMotionProps）
 * 接入动画；这里把缓动曲线与常用 variants 收敛到一处，调用点只引用现成的
 * `xxxMotionProps`，避免每个组件各写一套参数导致观感不一致。
 *
 * variants 与 `@heroui/framer-utils` 的 TRANSITION_VARIANTS 保持一致
 * （该包被 npm 嵌套安装、不是直接依赖，故在此本地维护一份）。
 */
import type { HTMLMotionProps, Variants } from "framer-motion";

/** 缓动曲线（数值与 HeroUI 内置一致） */
export const TRANSITION_EASINGS = {
  ease: [0.36, 0.66, 0.4, 1],
  easeIn: [0.4, 0, 1, 1],
  easeOut: [0, 0, 0.2, 1],
  easeInOut: [0.4, 0, 0.2, 1],
  spring: [0.155, 1.105, 0.295, 1.12],
  springOut: [0.57, -0.15, 0.62, 0.07],
  softSpring: [0.16, 1.11, 0.3, 1.02],
} as const;

/** 通用进出场 variants（对应 @heroui/framer-utils 的 TRANSITION_VARIANTS）。
 *  每个预设有 enter / exit（及可选 initial）三个变体名，供组件的
 *  initial="exit" / animate="enter" / exit="exit" 约定使用。 */
export const TRANSITION_VARIANTS = {
  scaleSpring: {
    enter: {
      transform: "scale(1)",
      opacity: 1,
      transition: { type: "spring", bounce: 0, duration: 0.2 },
    },
    exit: {
      transform: "scale(0.85)",
      opacity: 0,
      transition: { type: "easeOut", duration: 0.15 },
    },
  },
  scaleSpringOpacity: {
    initial: { opacity: 0, transform: "scale(0.8)" },
    enter: {
      opacity: 1,
      transform: "scale(1)",
      transition: { type: "spring", bounce: 0, duration: 0.3 },
    },
    exit: {
      opacity: 0,
      transform: "scale(0.96)",
      transition: { type: "easeOut", bounce: 0, duration: 0.15 },
    },
  },
  scale: {
    enter: { scale: 1 },
    exit: { scale: 0.95 },
  },
  scaleFadeIn: {
    enter: {
      transform: "scale(1)",
      opacity: 1,
      transition: { duration: 0.25, ease: TRANSITION_EASINGS.easeIn },
    },
    exit: {
      transform: "scale(0.95)",
      opacity: 0,
      transition: { duration: 0.2, ease: TRANSITION_EASINGS.easeOut },
    },
  },
  scaleInOut: {
    enter: {
      transform: "scale(1)",
      opacity: 1,
      transition: { duration: 0.4, ease: TRANSITION_EASINGS.ease },
    },
    exit: {
      transform: "scale(1.03)",
      opacity: 0,
      transition: { duration: 0.3, ease: TRANSITION_EASINGS.ease },
    },
  },
  fade: {
    enter: {
      opacity: 1,
      transition: { duration: 0.4, ease: TRANSITION_EASINGS.ease },
    },
    exit: {
      opacity: 0,
      transition: { duration: 0.3, ease: TRANSITION_EASINGS.ease },
    },
  },
  collapse: {
    enter: {
      opacity: 1,
      height: "auto",
      transition: {
        height: { type: "spring", bounce: 0, duration: 0.3 },
        opacity: { easings: "ease", duration: 0.4 },
      },
    },
    exit: {
      opacity: 0,
      height: 0,
      transition: { easings: "ease", duration: 0.3 },
    },
  },
} as unknown as Record<string, Variants>;

/**
 * Modal 的进出场已不再走 framer：modalBehaviorProps 里 disableAnimation=true，
 * 遮罩与面板的入场由 globals.css 的 CSS keyframes 驱动（.nya-modal-backdrop /
 * .nya-modal-enter），避免 framer 在本环境偶发的"事件丢失"导致遮罩卡透明态。
 */

/**
 * 浮层开合：**入场不动整层 opacity**（Dropdown / Popover / Select / Autocomplete 统一使用）。
 *
 * HeroUI 默认给浮层用 scaleSpringOpacity，它的 initial 是 `opacity: 0`，
 * 于是面板打开的头几帧整层是半透明的：面板背后那一块页面会**透出来**，
 * 观感就是"下拉面板打开时闪一下"。
 *
 * 注意这跟面板**表面**是否透明是两码事：表面底色再实，整层 opacity 从 0 爬到 1
 * 的路径照样让背后内容露出来 —— 所以只改表面治不了它。这条通道自 Pre-Beta 1 起就开着，
 * 不是哪次改动引入的。
 *
 * 这里 initial / enter 都显式写 opacity: 1：面板第一帧就是实的，只保留
 * 缩放；关闭仍走 exit 的淡出，收起来依旧是柔和的。
 * src/lib/motion.test.ts 会守住"入场不得透明"这条契约。
 */
const surfaceOpenVariants: Variants = {
  initial: { opacity: 1, transform: "scale(0.96)" },
  enter: {
    opacity: 1,
    transform: "scale(1)",
    transition: { type: "spring", bounce: 0, duration: 0.2 },
  },
  exit: {
    opacity: 0,
    transform: "scale(0.96)",
    transition: { type: "easeOut", bounce: 0, duration: 0.15 },
  },
};

/** Popover 的内容浮层（Dropdown 见下；Select / Autocomplete 见 selectPopoverProps） */
export const popoverMotionProps: HTMLMotionProps<"div"> = {
  variants: surfaceOpenVariants,
};

/**
 * Select / Autocomplete 的浮层：传给 `popoverProps`（这两个组件不收顶层
 * motionProps，浮层动效挂在 popover 上）。与 Dropdown / Popover 共用
 * surfaceOpenVariants，入场不再从 opacity: 0 爬起，打开不闪。
 */
export const selectPopoverProps = {
  motionProps: popoverMotionProps,
} as const;

/** Dropdown 菜单浮层 */
export const dropdownMotionProps: HTMLMotionProps<"div"> = {
  variants: surfaceOpenVariants,
};

/** Tooltip */
export const tooltipMotionProps: HTMLMotionProps<"div"> = {
  variants: TRANSITION_VARIANTS.scaleSpring,
};

/**
 * 列表项增删进出场（实例 / 账号 / 插件左列等真实增删的列表）。
 * 高度 0 ↔ auto：新项"撑开"、删除项"收拢"，其余项交给 motion 的
 * layout 属性平滑补位；须配 AnimatePresence initial={false} 与
 * overflow-hidden 使用，且 key 在增删后保持稳定。
 */
export const listItemVariants: Variants = {
  enter: { opacity: 0, height: 0 },
  center: {
    opacity: 1,
    height: "auto",
    transition: { duration: 0.25, ease: TRANSITION_EASINGS.easeOut },
  },
  exit: {
    opacity: 0,
    height: 0,
    transition: { duration: 0.2, ease: TRANSITION_EASINGS.easeIn },
  },
};

/** 右下角浮动卡片（全局下载指示器等）：从右侧弹性滑入、快速滑出 */
export const indicatorVariants: Variants = {
  enter: { opacity: 0, x: 24, scale: 0.97 },
  center: {
    opacity: 1,
    x: 0,
    scale: 1,
    transition: { type: "spring", bounce: 0.2, duration: 0.45 },
  },
  exit: {
    opacity: 0,
    x: 24,
    scale: 0.97,
    transition: { duration: 0.15, ease: TRANSITION_EASINGS.easeIn },
  },
};
