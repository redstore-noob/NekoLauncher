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
 * 标签 / 页面切换动画。
 *
 * 用法：把随 activeKey 变化的子树包进来，key 变化时旧子树直接卸载、
 * 新子树挂载并播放方向性入场动画（方向由 useSwitchDirection 根据标签
 * 序号给出），等价一层带 key 的普通 div。
 *
 * 为什么不用 framer-motion 的 AnimatePresence：它的进出场由 JS 状态机
 * 派发，本项目的组件树里会偶发"事件丢失"——动画（enter 或 exit）根本
 * 不启动，元素停在 opacity:0 的初始态（页面看起来空白），mode="wait"
 * 下更会永久堵死后续切换（AnimatePresence 永远等不到退场完成）。CSS
 * keyframes 由浏览器渲染引擎驱动，不存在不派发的可能；代价是退场动画
 * 省略（旧内容直接卸载），观感差异很小，换来确定性。
 * 首次挂载不播放动画（页面加载时不会莫名一抖）。
 */
import React, { useRef, useState } from "react";

interface SwitchTransitionProps {
  /** 当前标签 / 页面的唯一标识，变化即触发切换动画 */
  activeKey: string;
  /** 切换方向：1 向后（新内容从右入），-1 向前（从左入） */
  direction?: number;
  className?: string;
  /**
   * 动画预设：
   * - slide（默认）：页面 / 标签切换，纯横向滑动；
   * - instance：实例切换，滑入叠加轻微缩放与去模糊（更活泼）。
   */
  variant?: "slide" | "instance";
  children: React.ReactNode;
}

/**
 * 给随 activeKey 变化的子树加滑动淡入切换动画（CSS 驱动，见上）。
 * 外层容器的高度/滚动行为由 className 决定，与包一层普通 div 等价。
 */
export const SwitchTransition: React.FC<SwitchTransitionProps> = ({
  activeKey,
  direction = 1,
  className,
  variant = "slide",
  children,
}) => {
  // 首次挂载对应的 key：它的 div 不播动画，之后每个新 key 的 div 都带入场动画
  const firstKey = useRef(activeKey);
  const prefix = variant === "instance" ? "nya-instance" : "nya-switch";
  const animation =
    activeKey === firstKey.current
      ? ""
      : `${prefix}-in-${direction >= 0 ? "fwd" : "back"}`;

  return (
    <div key={activeKey} className={`${className ?? ""} ${animation}`}>
      {children}
    </div>
  );
};

/**
 * 根据当前标签序号推导切换方向（序号变大 = 向后，变小 = 向前）。
 * 传入 -1（未找到）时保持上一次方向不变。
 *
 * 用「渲染期派生 state」而不是 useEffect：effect 在提交并绘制之后才跑，
 * 那时新页面已经按上一轮的旧方向挂上了动画类，随后再被改成正确方向——
 * CSS 动画会从头重播一次，表现为"先往错误方向滑一下再反着滑"。
 */
export function useSwitchDirection(index: number): number {
  const [previous, setPrevious] = useState(index);
  const [direction, setDirection] = useState(1);

  if (index >= 0 && index !== previous) {
    setPrevious(index);
    if (previous >= 0) setDirection(index > previous ? 1 : -1);
  }

  return direction;
}

export default SwitchTransition;
