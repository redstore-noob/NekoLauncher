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
import {
  useSidebarSettings,
  orderPages,
  type SidebarPlacement,
} from "./sidebar-settings";

interface SidebarProps {
  activeKey: string;
  onNavigate: (key: string) => void;
}

// 自动隐藏的贴边热区与判定宽度：鼠标贴到对应窗口边缘 ≤ 8px 弹出；离开
// 面板最宽状态（168px 展开态 / 横条 56px 高）加边距余量后收起，避免贴着
// 边缘操作时来回闪
const EDGE_TRIGGER_PX = 8;
const VERTICAL_LEAVE_PX = 184;
const HORIZONTAL_LEAVE_PX = 80;

// 岛式（island）：四条停靠边的浮动面板定位，统一留 12px 边距，
// 顶部避开 40px 标题栏再留 12px
const PLACEMENT_POS_ISLAND: Record<SidebarPlacement, string> = {
  left: "left-3 top-[3.25rem] bottom-3",
  right: "right-3 top-[3.25rem] bottom-3",
  top: "top-[3.25rem] left-3 right-3",
  bottom: "bottom-3 left-3 right-3",
};

// 陆式（land）：贴着停靠边与窗口边缘连成一体（容器已在标题栏下方，
// 所以纵栏 top-0 / 横条停靠时上下同样贴满），无圆角、无外边距
const PLACEMENT_POS_LAND: Record<SidebarPlacement, string> = {
  left: "left-0 top-0 bottom-0",
  right: "right-0 top-0 bottom-0",
  top: "top-0 left-0 right-0",
  bottom: "bottom-0 left-0 right-0",
};

// 陆式只留朝内容区的一条边线（岛式是整圈 border）
const LAND_INNER_BORDER: Record<SidebarPlacement, string> = {
  left: "border-r",
  right: "border-l",
  top: "border-b",
  bottom: "border-t",
};

// 自动隐藏收起方向：朝自己贴的那条边滑出去（连同 12px 边距一起藏干净）
const HIDDEN_TRANSFORM: Record<SidebarPlacement, { x?: string; y?: string }> = {
  left: { x: "calc(-100% - 12px)" },
  right: { x: "calc(100% + 12px)" },
  top: { y: "calc(-100% - 12px)" },
  bottom: { y: "calc(100% + 12px)" },
};

const Sidebar: React.FC<SidebarProps> = ({ activeKey, onNavigate }) => {
  const { t } = useI18n();
  const [isExpanded, setIsExpanded] = useState(false);
  // 页面列表由 useShellPages 提供：普通模式即注册表；S 模式过滤成五个页面
  const pages = useShellPages();
  const { hiddenPages, autoHide, placement, style, pageOrder } =
    useSidebarSettings();
  // 自动隐藏模式下侧边栏是否处于"鼠标贴边弹出"状态
  const [peeked, setPeeked] = useState(false);
  // 系统开了"减少动态效果"时滑出/收起退化为瞬切
  const reduceMotion = useReducedMotion();

  const isHorizontal = placement === "top" || placement === "bottom";

  // 自动隐藏：监听全局鼠标位置（比 hover 热区元素稳，不会与侧边栏自身
  // 的 enter/leave 互相打架）——贴到面板所在的那条窗口边缘弹出，横向/
  // 纵向离开面板范围后收起
  useEffect(() => {
    if (!autoHide) {
      setPeeked(false);

      return;
    }
    const onMouseMove = (event: MouseEvent) => {
      let near = false;
      let away = false;

      switch (placement) {
        case "right": {
          const edge = window.innerWidth - EDGE_TRIGGER_PX;

          near = event.clientX >= edge;
          away = event.clientX < window.innerWidth - VERTICAL_LEAVE_PX;
          break;
        }
        case "top":
          near = event.clientY <= EDGE_TRIGGER_PX;
          away = event.clientY > HORIZONTAL_LEAVE_PX;
          break;
        case "bottom": {
          const edge = window.innerHeight - EDGE_TRIGGER_PX;

          near = event.clientY >= edge;
          away = event.clientY < window.innerHeight - HORIZONTAL_LEAVE_PX;
          break;
        }
        default:
          near = event.clientX <= EDGE_TRIGGER_PX;
          away = event.clientX > VERTICAL_LEAVE_PX;
      }
      if (near) setPeeked(true);
      else if (away) setPeeked(false);
    };

    window.addEventListener("mousemove", onMouseMove);

    return () => window.removeEventListener("mousemove", onMouseMove);
  }, [autoHide, placement]);

  // 先滤掉隐藏页，再按设置页里保存的顺序排列（未保存过时按注册表 order）
  const visiblePages = orderPages(
    pages.filter((page) => !hiddenPages.has(page.id)),
    pageOrder,
  );

  const menuButton = (
    <Button
      isIconOnly
      className="text-gray-500 min-w-10 w-10 h-10"
      variant="light"
      onPress={() => setIsExpanded((v) => !v)}
    >
      <MenuIcon />
    </Button>
  );

  // 菜单项按钮：竖栏展开态铺满整行；横条展开态按内容自适应宽度
  const renderNavButton = (
    item: (typeof visiblePages)[number],
    isActive: boolean,
  ) => (
    <Button
      className={`
        relative z-10
        ${
          isExpanded
            ? isHorizontal
              ? "w-auto justify-start gap-3 px-3"
              : "w-full justify-start gap-3 px-3"
            : "min-w-0 w-10 h-10 mx-auto"
        }
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

  const renderItem = (item: (typeof visiblePages)[number], index: number) => {
    const isActive = activeKey === item.id;
    // 收起态用 tooltip 提示页面名：竖栏弹在右侧，横条弹在下方
    const tooltipPlacement = isHorizontal ? "bottom" : "right";

    return (
      <motion.li
        key={item.id}
        layout
        className={`nya-sidebar-item relative flex-shrink-0 ${isActive ? "nya-sidebar-item-active" : ""}`}
        style={{ animationDelay: `${index * 40}ms` }}
        transition={{ type: "spring", bounce: 0.15, duration: 0.4 }}
      >
        {/* 包裹层与按钮实际尺寸一致：指示胶囊贴着按钮滑，不会越界
         * 盖到相邻项（收起态按钮 w-10 居中，li 却占整行宽） */}
        <div
          className={`relative ${isExpanded ? (isHorizontal ? "w-auto" : "w-full") : "w-10 mx-auto"}`}
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
            renderNavButton(item, isActive)
          ) : (
            <Tooltip
              content={t(item.label)}
              delay={200}
              motionProps={tooltipMotionProps}
              placement={tooltipPlacement}
            >
              {renderNavButton(item, isActive)}
            </Tooltip>
          )}
        </div>
      </motion.li>
    );
  };

  return (
    <motion.aside
      // 自动隐藏模式的滑出/收起交给 framer 弹簧驱动：CSS 过渡的贝塞尔
      // 过冲只有几个像素看不出来，弹簧能给面板本体一个明显的回弹
      animate={
        autoHide && !peeked ? HIDDEN_TRANSFORM[placement] : { x: 0, y: 0 }
      }
      className={`
        absolute z-20
        ${(style === "land" ? PLACEMENT_POS_LAND : PLACEMENT_POS_ISLAND)[placement]}
        nya-sidebar
        ${
          style === "land"
            ? `${LAND_INNER_BORDER[placement]} nya-border rounded-none`
            : "border nya-border rounded-large"
        }
        transition-[width,box-shadow] duration-300
        ${
          autoHide && !peeked
            ? `${isHorizontal ? "h-14" : "w-16"} shadow-none`
            : `${style === "land" ? "" : "shadow-[0_8px_32px_rgba(0,0,0,0.28)]"} ${
                isHorizontal ? "h-14" : isExpanded ? "w-[168px]" : "w-16"
              }`
        }
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
      {isHorizontal ? (
        /* 横条：汉堡键 + 菜单排成一行，展开/收起只切换标签显隐 */
        <div className="flex h-full w-full items-center gap-1 px-2">
          {menuButton}
          <nav className="nya-scroll-area flex-1 overflow-x-auto py-2">
            <ul className="flex flex-row items-center gap-1 px-1">
              {visiblePages.map(renderItem)}
            </ul>
          </nav>
        </div>
      ) : (
        <>
          <div className="h-14 flex items-center flex-shrink-0">
            <div className="w-16 flex items-center justify-center flex-shrink-0">
              {menuButton}
            </div>
          </div>

          {/*菜单*/}
          <nav className="flex-1 overflow-y-auto py-4">
            <ul className="space-y-1 px-2">{visiblePages.map(renderItem)}</ul>
          </nav>
        </>
      )}
    </motion.aside>
  );
};

export default Sidebar;
