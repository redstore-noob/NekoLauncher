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
import React, { useEffect, useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { Button, Tooltip } from "@heroui/react";
// 图标统一用 Fluent UI System Icons（20px 系）；页面自己的图标由注册项提供
import { List20Regular as MenuIcon } from "@fluentui/react-icons";

import { useI18n } from "../i18n";
import { tooltipMotionProps, TRANSITION_EASINGS } from "../lib/motion";

import { useShellPages } from "./simple-mode";
import { useSidebarSettings, orderPages } from "./sidebar-settings";

interface SidebarProps {
  activeKey: string;
  onNavigate: (key: string) => void;
}

// 自动隐藏的贴边热区与判定宽度：鼠标 x ≤ 8px 弹出；离开侧边栏最宽状态
// （168px 展开态）后再留一点余量即收起，避免贴着边缘操作时来回闪
const EDGE_TRIGGER_PX = 8;
const SIDEBAR_LEAVE_PX = 176;

const Sidebar: React.FC<SidebarProps> = ({ activeKey, onNavigate }) => {
  const { t } = useI18n();
  const [isExpanded, setIsExpanded] = useState(false);
  // 页面列表由 useShellPages 提供：普通模式即注册表；S 模式过滤成五个页面
  const pages = useShellPages();
  const { hiddenPages, autoHide, pageOrder } = useSidebarSettings();
  // 自动隐藏模式下侧边栏是否处于"鼠标贴边弹出"状态
  const [peeked, setPeeked] = useState(false);
  // 系统开了"减少动态效果"时滑出/收起退化为瞬切
  const reduceMotion = useReducedMotion();

  // 自动隐藏：监听全局鼠标位置（比 hover 热区元素稳，不会与侧边栏自身
  // 的 enter/leave 互相打架）——贴到窗口左缘弹出，横向离开侧边栏后收起
  useEffect(() => {
    if (!autoHide) {
      setPeeked(false);

      return;
    }
    const onMouseMove = (event: MouseEvent) => {
      if (event.clientX <= EDGE_TRIGGER_PX) setPeeked(true);
      else if (event.clientX > SIDEBAR_LEAVE_PX) setPeeked(false);
    };

    window.addEventListener("mousemove", onMouseMove);

    return () => window.removeEventListener("mousemove", onMouseMove);
  }, [autoHide]);

  // 先滤掉隐藏页，再按设置页里保存的顺序排列（未保存过时按注册表 order）
  const visiblePages = orderPages(
    pages.filter((page) => !hiddenPages.has(page.id)),
    pageOrder,
  );

  return (
    <motion.aside
      // 自动隐藏模式的滑出/收起交给 framer 弹簧驱动：CSS 过渡的贝塞尔
      // 过冲只有几个像素看不出来，弹簧能给面板本体一个明显的回弹
      animate={{ x: autoHide && !peeked ? "-100%" : 0 }}
      className={`
        absolute left-0 top-10 h-[calc(100%-2.5rem)] z-20
        nya-sidebar
        border-r nya-border
        transition-[width,box-shadow] duration-300
        ${autoHide && !peeked ? "w-16 shadow-none" : `shadow-[0_0_24px_rgba(0,0,0,0.25)] ${isExpanded ? "w-[168px]" : "w-16"}`}
        flex flex-col overflow-hidden
      `}
      transition={
        reduceMotion
          ? { duration: 0 }
          : autoHide && !peeked
            ? // 收起：稍微等一等再走，鼠标擦过边缘时不会瞬间塌下去
              { duration: 0.3, delay: 0.15, ease: [0.4, 0, 0.2, 1] }
            : { type: "spring", bounce: 0.35, duration: 0.55 }
      }
    >
      <div className="h-14 flex items-center flex-shrink-0">
        <div className="w-16 flex items-center justify-center flex-shrink-0">
          <Button
            isIconOnly
            className="text-gray-500 min-w-10 w-10 h-10"
            variant="light"
            onPress={() => setIsExpanded((v) => !v)}
          >
            <MenuIcon />
          </Button>
        </div>
      </div>

      {/*菜单*/}
      <nav className="flex-1 overflow-y-auto py-4">
        <ul className="space-y-1 px-2">
          {visiblePages.map((item, index) => {
            const isActive = activeKey === item.id;
            const button = (
              <Button
                className={`
                  relative z-10
                  ${isExpanded ? "w-full justify-start gap-3 px-3" : "min-w-0 w-10 h-10 mx-auto"}
                `}
                color={isActive ? "primary" : "default"}
                isIconOnly={!isExpanded}
                variant="light"
                onPress={() => onNavigate(item.id)}
              >
                {isExpanded ? (
                  <>
                    <span className="nya-sidebar-icon w-5 flex-shrink-0 flex items-center justify-center">
                      {item.icon}
                    </span>
                    <AnimatePresence initial={false}>
                      <motion.span
                        key="label"
                        animate={{
                          opacity: 1,
                          x: 0,
                          transition: {
                            duration: 0.2,
                            delay: 0.05,
                            ease: TRANSITION_EASINGS.easeOut,
                          },
                        }}
                        className="whitespace-nowrap text-sm"
                        exit={{
                          opacity: 0,
                          x: -6,
                          transition: {
                            duration: 0.1,
                            ease: TRANSITION_EASINGS.easeIn,
                          },
                        }}
                        initial={{ opacity: 0, x: -6 }}
                      >
                        {t(item.label)}
                      </motion.span>
                    </AnimatePresence>
                  </>
                ) : (
                  <span className="nya-sidebar-icon flex items-center justify-center">
                    {item.icon}
                  </span>
                )}
              </Button>
            );

            return (
              <motion.li
                key={item.id}
                layout
                className={`nya-sidebar-item relative ${isActive ? "nya-sidebar-item-active" : ""}`}
                style={{ animationDelay: `${index * 40}ms` }}
                transition={{ type: "spring", bounce: 0.15, duration: 0.4 }}
              >
                {/* 包裹层与按钮实际尺寸一致：指示胶囊贴着按钮滑，不会越界
                 * 盖到相邻项（收起态按钮 w-10 居中，li 却占整行宽） */}
                <div
                  className={`relative ${isExpanded ? "w-full" : "w-10 mx-auto"}`}
                >
                  {/* 选中指示条：HeroUI Tabs cursor 的做法——同一 layoutId，
                   * 切换页面时这颗胶囊会用弹簧动画滑到新的位置 */}
                  {isActive && (
                    <motion.span
                      className="nya-sidebar-cursor absolute inset-0 rounded-medium bg-primary/15"
                      layoutId="nya-sidebar-cursor"
                      transition={{
                        type: "spring",
                        bounce: 0.25,
                        duration: 0.5,
                      }}
                    />
                  )}
                  {isExpanded ? (
                    button
                  ) : (
                    <Tooltip
                      content={t(item.label)}
                      delay={200}
                      motionProps={tooltipMotionProps}
                      placement="right"
                    >
                      {button}
                    </Tooltip>
                  )}
                </div>
              </motion.li>
            );
          })}
        </ul>
      </nav>
    </motion.aside>
  );
};

export default Sidebar;
