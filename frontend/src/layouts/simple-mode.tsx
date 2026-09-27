/*
 * NekoLauncher-S 模式：面向低龄玩家与整合包分发的精简界面。
 *
 * 开启后侧边栏只保留五个页面：启动（主页）、外观、下载、账号、设置；
 * 主页改标「启动」突出一键开玩。开关存 launcher.yaml 的 simpleMode 键
 *（与后端 internal/solo 的首启自动开启写同一键），NekoSolo 安装包装的
 * 启动器首启默认进入 S 模式。
 *
 * useShellPages 是侧边栏与内容区共用的页面列表：普通模式原样返回注册表，
 * S 模式按白名单过滤并替换主页标签。隐藏只影响导航，页面本身仍注册
 * （下载指示器等入口跳转不受影响，落点页面照常渲染）。
 */
import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

import {
  ClearValue,
  GetValue,
  SetValue,
} from "../../wailsjs/go/bindings/ConfigAPI";
import { usePages, type PageDefinition } from "../plugin";
import { t } from "../i18n";

/** launcher.yaml 中的键（经 ConfigAPI.SetValue/GetValue 读写） */
export const SIMPLE_MODE_KEY = "simpleMode";

/** S 模式保留的页面 id（按侧边栏期望顺序） */
export const SIMPLE_MODE_PAGE_IDS = [
  "home",
  "appearance",
  "download",
  "account",
  "settings",
];

interface SimpleModeState {
  simpleMode: boolean;
  /**
   * 启动配置是否已读完。S 模式改变侧边栏构成与首屏页面，窗口显示
   *（治启动闪屏）需要等它落定，避免显示后再跳一下。
   */
  hydrated: boolean;
  /** 开/关 S 模式并持久化 */
  setSimpleMode: (enabled: boolean) => void;
}

const SimpleModeContext = createContext<SimpleModeState>({
  simpleMode: false,
  hydrated: false,
  setSimpleMode: () => {
    /* Provider 未挂载时的空实现 */
  },
});

export function useSimpleMode(): SimpleModeState {
  return useContext(SimpleModeContext);
}

export const SimpleModeProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [simpleMode, setSimpleModeState] = useState(false);
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    GetValue(SIMPLE_MODE_KEY)
      .then((value: string) => setSimpleModeState(value === "true"))
      .catch(() => setSimpleModeState(false))
      .finally(() => setHydrated(true));
  }, []);

  const setSimpleMode = useCallback((enabled: boolean) => {
    setSimpleModeState(enabled);
    if (enabled) void SetValue(SIMPLE_MODE_KEY, "true");
    else void ClearValue(SIMPLE_MODE_KEY);
  }, []);

  return (
    <SimpleModeContext.Provider value={{ simpleMode, hydrated, setSimpleMode }}>
      {children}
    </SimpleModeContext.Provider>
  );
};

/** useShellPages 侧边栏/内容区共用的页面列表（S 模式过滤 + 主页改标启动） */
export function useShellPages(): PageDefinition[] {
  const pages = usePages();
  const { simpleMode } = useSimpleMode();

  return useMemo(() => {
    if (!simpleMode) return pages;
    const allowed = new Set(SIMPLE_MODE_PAGE_IDS);

    return pages
      .filter((page) => allowed.has(page.id))
      .map((page) =>
        page.id === "home" ? { ...page, label: t("启动") } : page,
      );
  }, [pages, simpleMode]);
}
