/*
 * 侧边栏设置：侧边栏页面的显示/隐藏、排序、"自动隐藏"开关、停靠边与形态。
 *
 * 隐藏列表存 launcherHiddenSidebarPages（英文逗号分隔的页面 id，空则清键）；
 * 显示顺序存 launcherSidebarPageOrder（同样逗号分隔，未列出的页面按注册表
 * order 排在后面）；自动隐藏存 launcherSidebarAutoHide（"true"，关闭时清键）；
 * 停靠边存 launcherSidebarPlacement；形态存 launcherSidebarStyle
 * （island = 岛式悬浮面板 / land = 陆式贴边连体，缺省 island）。
 * 开启自动隐藏后侧边栏平时整体收起，鼠标贴到窗口左缘时自动弹出（弹出逻辑
 * 见 Sidebar.tsx）。主页、外观与设置是导航的兜底入口，永远不可隐藏（可以
 * 排序）；隐藏只影响侧边栏导航，页面本身仍注册（下载指示器等入口仍可跳转）。
 */
import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";

import {
  ClearValue,
  GetValue,
  SetValue,
} from "../../wailsjs/go/bindings/ConfigAPI";

// launcher.yaml 中的键（经 ConfigAPI.SetValue/GetValue 读写）
export const SIDEBAR_HIDDEN_PAGES_KEY = "launcherHiddenSidebarPages";
export const SIDEBAR_PAGE_ORDER_KEY = "launcherSidebarPageOrder";
export const SIDEBAR_AUTO_HIDE_KEY = "launcherSidebarAutoHide";
export const SIDEBAR_PLACEMENT_KEY = "launcherSidebarPlacement";
export const SIDEBAR_STYLE_KEY = "launcherSidebarStyle";

/** 侧边栏停靠边：上下左右；非法值回落 left */
export type SidebarPlacement = "left" | "right" | "top" | "bottom";

/**
 * 侧边栏形态：island = 岛式（悬浮面板，四边留 12px 边距、圆角、投影）；
 * land = 陆式（贴着停靠边与窗口边缘连成一体，无圆角投影，只留朝内容区的
 * 一条边线）。非法值回落 island（现行默认）。
 */
export type SidebarStyle = "island" | "land";

export function parseSidebarPlacement(raw: string): SidebarPlacement {
  return raw === "right" || raw === "top" || raw === "bottom" ? raw : "left";
}

export function parseSidebarStyle(raw: string): SidebarStyle {
  return raw === "land" ? "land" : "island";
}

/**
 * 不允许隐藏的页面 id。外观是主题/背景/侧边栏这些设置的唯一入口，藏了就
 * 找不回来，所以和主页（启动入口）、设置一起受保护；其余页面（含插件页）
 * 都可以在外观页里拖进"隐藏"盒。
 */
export const PROTECTED_SIDEBAR_PAGES = new Set([
  "home",
  "appearance",
  "settings",
]);

interface SidebarSettingsState {
  /** 已隐藏的页面 id（受保护页绝不会出现在这里） */
  hiddenPages: Set<string>;
  /** 保存的显示顺序（只含当时可见的页面；未列出的按注册表顺序垫底） */
  pageOrder: string[];
  /** 自动隐藏：开启后侧边栏收起，鼠标贴窗口对应边缘时弹出 */
  autoHide: boolean;
  /** 停靠边：上下左右（浮动面板贴哪条边） */
  placement: SidebarPlacement;
  /** 形态：岛式（悬浮）或陆式（贴边连体） */
  style: SidebarStyle;
  /**
   * 启动配置是否已读完。autoHide/隐藏列表/排序都会改变首屏布局，窗口显示
   * （治启动闪屏）需要等它落定，避免显示后侧边栏再跳一下。
   */
  hydrated: boolean;
  /**
   * 一次提交完整的侧边栏布局并持久化：visibleIds 即新的显示顺序，
   * hiddenIds 即新的隐藏集合（受保护页会被强制留在可见列表里）
   */
  applySidebarLayout: (visibleIds: string[], hiddenIds: string[]) => void;
  /** 开/关自动隐藏并持久化 */
  setAutoHide: (enabled: boolean) => void;
  /** 切换停靠边并持久化 */
  setPlacement: (placement: SidebarPlacement) => void;
  /** 切换岛式/陆式并持久化 */
  setStyle: (style: SidebarStyle) => void;
}

const SidebarSettingsContext = createContext<SidebarSettingsState>({
  hiddenPages: new Set<string>(),
  pageOrder: [],
  autoHide: false,
  placement: "left",
  style: "island",
  hydrated: false,
  applySidebarLayout: () => {
    /* Provider 未挂载时的空实现 */
  },
  setAutoHide: () => {
    /* Provider 未挂载时的空实现 */
  },
  setPlacement: () => {
    /* Provider 未挂载时的空实现 */
  },
  setStyle: () => {
    /* Provider 未挂载时的空实现 */
  },
});

export function useSidebarSettings(): SidebarSettingsState {
  return useContext(SidebarSettingsContext);
}

function parseIdList(raw: string): string[] {
  return raw
    .split(",")
    .map((id) => id.trim())
    .filter((id) => !!id);
}

/** 按"保存顺序 → 注册表 order → id"排序；未列入保存顺序的页面统一垫底 */
export function orderPages<T extends { id: string; order: number }>(
  pages: T[],
  savedOrder: string[],
): T[] {
  const rank = new Map(savedOrder.map((id, index) => [id, index]));

  return [...pages].sort((left, right) => {
    const leftRank = rank.get(left.id) ?? Number.MAX_SAFE_INTEGER;
    const rightRank = rank.get(right.id) ?? Number.MAX_SAFE_INTEGER;

    return leftRank !== rightRank
      ? leftRank - rightRank
      : left.order - right.order || left.id.localeCompare(right.id);
  });
}

/** 全空时清键，让 launcher.yaml 回到未设置状态 */
async function persistIdList(key: string, ids: string[]) {
  if (ids.length === 0) await ClearValue(key);
  else await SetValue(key, ids.join(","));
}

export const SidebarSettingsProvider: React.FC<{
  children: React.ReactNode;
}> = ({ children }) => {
  const [hiddenPages, setHiddenPages] = useState<Set<string>>(new Set());
  const [pageOrder, setPageOrder] = useState<string[]>([]);
  const [autoHide, setAutoHideState] = useState(false);
  const [placement, setPlacementState] = useState<SidebarPlacement>("left");
  const [style, setStyleState] = useState<SidebarStyle>("island");
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    // 各读取自带 catch（失败回落默认值），Promise.all 必然 resolve；
    // 落定后置 hydrated 供启动流程判断"首屏布局已稳定"（见 WindowReveal）
    void Promise.all([
      GetValue(SIDEBAR_HIDDEN_PAGES_KEY)
        .then((value) => {
          // 手改 config 把受保护页写进列表时直接忽略
          setHiddenPages(
            new Set(
              parseIdList(value).filter(
                (id) => !PROTECTED_SIDEBAR_PAGES.has(id),
              ),
            ),
          );
        })
        .catch(() => setHiddenPages(new Set())),
      GetValue(SIDEBAR_PAGE_ORDER_KEY)
        .then((value) => setPageOrder(parseIdList(value)))
        .catch(() => setPageOrder([])),
      GetValue(SIDEBAR_AUTO_HIDE_KEY)
        .then((value) => setAutoHideState(value === "true"))
        .catch(() => setAutoHideState(false)),
      GetValue(SIDEBAR_PLACEMENT_KEY)
        .then((value) => setPlacementState(parseSidebarPlacement(value)))
        .catch(() => setPlacementState("left")),
      GetValue(SIDEBAR_STYLE_KEY)
        .then((value) => setStyleState(parseSidebarStyle(value)))
        .catch(() => setStyleState("island")),
    ]).then(() => setHydrated(true));
  }, []);

  const applySidebarLayout = useCallback(
    (visibleIds: string[], hiddenIds: string[]) => {
      const nextVisible = [
        ...new Set(visibleIds.filter((id) => !hiddenIds.includes(id))),
      ];

      // 受保护页即使拖错也强制保留在可见列表里
      for (const id of PROTECTED_SIDEBAR_PAGES) {
        if (!nextVisible.includes(id)) nextVisible.push(id);
      }
      const nextHidden = hiddenIds.filter(
        (id) => !PROTECTED_SIDEBAR_PAGES.has(id) && !nextVisible.includes(id),
      );

      setHiddenPages(new Set(nextHidden));
      setPageOrder(nextVisible);
      void persistIdList(SIDEBAR_PAGE_ORDER_KEY, nextVisible);
      void persistIdList(SIDEBAR_HIDDEN_PAGES_KEY, nextHidden);
    },
    [],
  );

  const setAutoHide = useCallback((enabled: boolean) => {
    setAutoHideState(enabled);
    if (enabled) void SetValue(SIDEBAR_AUTO_HIDE_KEY, "true");
    else void ClearValue(SIDEBAR_AUTO_HIDE_KEY);
  }, []);

  const setPlacement = useCallback((next: SidebarPlacement) => {
    setPlacementState(next);
    void SetValue(SIDEBAR_PLACEMENT_KEY, next);
  }, []);

  const setStyle = useCallback((next: SidebarStyle) => {
    setStyleState(next);
    void SetValue(SIDEBAR_STYLE_KEY, next);
  }, []);

  return (
    <SidebarSettingsContext.Provider
      value={{
        hiddenPages,
        pageOrder,
        autoHide,
        placement,
        style,
        hydrated,
        applySidebarLayout,
        setAutoHide,
        setPlacement,
        setStyle,
      }}
    >
      {children}
    </SidebarSettingsContext.Provider>
  );
};
