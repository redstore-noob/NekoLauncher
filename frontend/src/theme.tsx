/*
 * 主题模式：浅色 / 深色 / 跟随系统。
 *
 * 模式存 localStorage 而非 launcher.yaml——它只影响外观，且必须在首帧前同步读到，
 * 否则每次启动都会先闪一下浅色。跟随系统时监听 prefers-color-scheme 实时切换，
 * 且不会把系统当前值写回存储，模式始终停留在 system。
 *
 * 落地方式是给 <html> 加 dark/light 类：全局样式与各组件的 dark: 变体都挂在 dark 类上。
 */
import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";

export type ThemeMode = "light" | "dark" | "system";

type ResolvedTheme = "light" | "dark";

const STORAGE_KEY = "nekolauncher-theme";

const DARK_MEDIA_QUERY = "(prefers-color-scheme: dark)";

function readStoredMode(): ThemeMode {
  const stored = localStorage.getItem(STORAGE_KEY);

  return stored === "light" || stored === "dark" || stored === "system"
    ? stored
    : "system";
}

function systemPrefersDark(): boolean {
  return window.matchMedia?.(DARK_MEDIA_QUERY).matches ?? false;
}

function resolveTheme(mode: ThemeMode): ResolvedTheme {
  if (mode !== "system") return mode;

  return systemPrefersDark() ? "dark" : "light";
}

function applyTheme(mode: ThemeMode) {
  const resolved = resolveTheme(mode);
  const root = document.documentElement;

  root.classList.toggle("dark", resolved === "dark");
  root.classList.toggle("light", resolved === "light");
  // 让滚动条、原生控件这些由浏览器绘制的部分也跟着切换
  root.style.colorScheme = resolved;
}

/** 渲染前调用：先把已保存的主题落到根节点，避免首帧闪浅色。 */
export function initTheme() {
  applyTheme(readStoredMode());
}

interface ThemeContextValue {
  mode: ThemeMode;
  /** 当前实际生效的明暗（system 时跟随系统解析结果） */
  resolved: ResolvedTheme;
  setMode: (mode: ThemeMode) => void;
}

const ThemeContext = createContext<ThemeContextValue>({
  mode: "system",
  resolved: "light",
  setMode: () => {
    /* Provider 未挂载时的空实现 */
  },
});

export function useThemeMode(): ThemeContextValue {
  return useContext(ThemeContext);
}

export const ThemeProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [mode, setModeState] = useState<ThemeMode>(readStoredMode);
  const [resolved, setResolved] = useState<ResolvedTheme>(() =>
    resolveTheme(readStoredMode()),
  );

  const setMode = useCallback((next: ThemeMode) => {
    localStorage.setItem(STORAGE_KEY, next);
    setModeState(next);
  }, []);

  // 应用主题：首帧已由 initTheme 处理，这里负责后续的模式切换
  useEffect(() => {
    setResolved(resolveTheme(mode));
    applyTheme(mode);
  }, [mode]);

  // 只在跟随系统时监听系统深浅色变化，实时跟随
  useEffect(() => {
    if (mode !== "system") return;
    const media = window.matchMedia(DARK_MEDIA_QUERY);
    const onChange = () => {
      setResolved(resolveTheme("system"));
      applyTheme("system");
    };

    media.addEventListener("change", onChange);

    return () => media.removeEventListener("change", onChange);
  }, [mode]);

  return (
    <ThemeContext.Provider value={{ mode, resolved, setMode }}>
      {children}
    </ThemeContext.Provider>
  );
};
