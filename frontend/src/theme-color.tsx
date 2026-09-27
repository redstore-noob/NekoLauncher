/*
 * 主题色（主色）设置：在 HeroUI 默认主色之上覆盖 --heroui-primary-* 变量。
 *
 * HeroUI 组件（Button/Slider/Switch/... 的 color="primary"）与 Tailwind 的
 * bg-primary/text-primary-500 等类都读取 --heroui-primary 及 --heroui-primary-50~900
 * （HSL 通道格式，如 "212 100% 47%"）。因此运行时把这些变量写到 <html> 的内联样式上
 * 即可全局换色，且内联优先级高于 HeroUI 的 :root/.dark 规则。
 *
 * 预置色为固定的一组十六进制色（按色相生成明暗阶梯，500 锚定所选色）；自定义色走
 * 同一套阶梯逻辑。选择存 localStorage（与明暗主题一致，仅影响外观，需在首帧前同步
 * 读取避免闪烁）。
 */
import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";

import { t } from "./i18n";

const STORAGE_KEY = "nekolauncher-theme-color";
/** 取色方式：manual=手选色板/自定义色；background=跟随背景图自动取色 */
const STORAGE_KEY_SOURCE = "nekolauncher-theme-color-source";
/**
 * 最近一次从背景图取到的颜色。它的作用是启动时"先顶上"：背景图要等图片解码
 * 之后才能取色（异步），不缓存就会先闪一下手选色再跳到取色结果。
 */
const STORAGE_KEY_EXTRACTED = "nekolauncher-theme-color-extracted";

const STOPS = [
  "50",
  "100",
  "200",
  "300",
  "400",
  "500",
  "600",
  "700",
  "800",
  "900",
] as const;

/** 预置主题色（顺序即展示顺序；id 即十六进制值，存储与自定义色同路径）。 */
export const THEME_COLOR_PRESETS: Array<{
  id: string;
  label: string;
  hex: string;
}> = [
  { id: "#39C5BB", label: t("初音青"), hex: "#39C5BB" },
  { id: "#F02E3A", label: t("绯红"), hex: "#F02E3A" },
  { id: "#55A532", label: t("抹茶绿"), hex: "#55A532" },
  { id: "#70DFFF", label: t("晴空蓝"), hex: "#70DFFF" },
  { id: "#976D4D", label: t("可可棕"), hex: "#976D4D" },
];

const DEFAULT_COLOR_KEY = THEME_COLOR_PRESETS[0].id;

type Channels = Record<string, string>;

function hexToHsl(hex: string): { h: number; s: number; l: number } {
  let value = hex.replace("#", "").trim();

  if (value.length === 3) {
    value = value
      .split("")
      .map((c) => c + c)
      .join("");
  }
  const num = Number.parseInt(value, 16);
  const r = ((num >> 16) & 255) / 255;
  const g = ((num >> 8) & 255) / 255;
  const b = (num & 255) / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  let h = 0;
  let s = 0;

  if (max !== min) {
    const d = max - min;

    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    if (max === r) h = (g - b) / d + (g < b ? 6 : 0);
    else if (max === g) h = (b - r) / d + 2;
    else h = (r - g) / d + 4;
    h /= 6;
  }

  return {
    h: Math.round(h * 360),
    s: Math.round(s * 100),
    l: Math.round(l * 100),
  };
}

function channels(h: number, s: number, l: number): string {
  return `${h} ${s}% ${Math.max(0, Math.min(100, Math.round(l)))}%`;
}

/** 自定义色 → 固定明暗阶梯的 HSL 通道（500 锚定所选色）。 */
function customToChannels(hex: string): Channels {
  const { h, s, l } = hexToHsl(hex);
  const ramp: Record<string, number> = {
    50: 97,
    100: 94,
    200: 86,
    300: 76,
    400: 66,
    500: l,
    600: l - 10,
    700: l - 20,
    800: l - 28,
    900: l - 36,
  };
  const out: Channels = {};

  for (const stop of STOPS) out[stop] = channels(h, s, ramp[stop]);

  return out;
}

function applyChannels(ch: Channels) {
  const root = document.documentElement;

  for (const stop of STOPS) {
    if (ch[stop]) root.style.setProperty(`--heroui-primary-${stop}`, ch[stop]);
  }
  const base = ch["500"] ?? "212 100% 47%";

  root.style.setProperty("--heroui-primary", base);
  const [hueRaw, satRaw, lightRaw] = base.split(" ");
  const hue = Number.parseFloat(hueRaw) || 212;
  const sat = Number.parseFloat(satRaw) || 100;
  const lightness = Number.parseFloat(lightRaw) || 47;

  // 亮色主色用深色前程色，保证按钮文字对比度
  root.style.setProperty(
    "--heroui-primary-foreground",
    lightness > 62 ? "0 0% 9%" : "0 0% 100%",
  );

  // 侧边栏/面板表面色：同一主题色相，降饱和并做明暗偏移（暗色更暗、亮色更亮）
  const surface = (satFactor: number, light: number) =>
    `${hue} ${Math.round(sat * satFactor)}% ${light}%`;

  root.style.setProperty("--nya-surface-1-light", surface(0.32, 97));
  root.style.setProperty("--nya-surface-2-light", surface(0.22, 99));
  root.style.setProperty("--nya-border-light", surface(0.26, 88));
  root.style.setProperty("--nya-surface-1-dark", surface(0.32, 11));
  root.style.setProperty("--nya-surface-2-dark", surface(0.22, 15));
  root.style.setProperty("--nya-border-dark", surface(0.2, 25));
}

/** 预置与自定义色统一走同一套十六进制 → 明暗阶梯逻辑。 */
function applyColorKey(key: string) {
  applyChannels(customToChannels(key));
}

/** 是否为可解析的十六进制颜色（#rgb / #rrggbb）。 */
function isHexColor(value: string): boolean {
  return /^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.test(value);
}

function readStoredColorKey(): string {
  const stored = localStorage.getItem(STORAGE_KEY);

  // 旧版本的预置 id（"blue" 等非十六进制值）已下线，统一回落默认色
  return stored && isHexColor(stored) ? stored : DEFAULT_COLOR_KEY;
}

export type ThemeColorSource = "manual" | "background";

function readStoredSource(): ThemeColorSource {
  return localStorage.getItem(STORAGE_KEY_SOURCE) === "background"
    ? "background"
    : "manual";
}

function readStoredExtracted(): string | null {
  const stored = localStorage.getItem(STORAGE_KEY_EXTRACTED);

  return stored && isHexColor(stored) ? stored : null;
}

/** 本地存储读写都包一层：隐私模式/配额满时不该让换主题色崩掉 */
function safeSet(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* 记不住就只在本次会话里生效 */
  }
}

/** 渲染前调用：先把已保存的主题色落到根节点，避免首帧闪默认色。 */
export function initThemeColor() {
  const source = readStoredSource();
  const extracted = readStoredExtracted();

  // 跟随背景时用缓存的上一次取色结果顶上，等背景图解码完再覆盖
  applyColorKey(
    source === "background" && extracted ? extracted : readStoredColorKey(),
  );
}

interface ThemeColorContextValue {
  /** 当前选择：预置色 id 或自定义色的十六进制（#rrggbb） */
  colorKey: string;
  setColorKey: (key: string) => void;
  /** 取色方式 */
  source: ThemeColorSource;
  setSource: (source: ThemeColorSource) => void;
  /** 最近一次从背景图取到的颜色（还没取到过时为 null） */
  extracted: string | null;
  /** 由背景取色桥接写入结果；传 null 表示这次没取到（保留原色） */
  setExtractedColor: (hex: string | null) => void;
  /** 重新取色用的自增令牌：背景层的取色 effect 依赖它 */
  extractNonce: number;
  refreshExtraction: () => void;
}

const ThemeColorContext = createContext<ThemeColorContextValue>({
  colorKey: DEFAULT_COLOR_KEY,
  setColorKey: () => {
    /* Provider 未挂载时的空实现 */
  },
  source: "manual",
  setSource: () => {
    /* Provider 未挂载时的空实现 */
  },
  extracted: null,
  setExtractedColor: () => {
    /* Provider 未挂载时的空实现 */
  },
  extractNonce: 0,
  refreshExtraction: () => {
    /* Provider 未挂载时的空实现 */
  },
});

export function useThemeColor(): ThemeColorContextValue {
  return useContext(ThemeColorContext);
}

export const ThemeColorProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [colorKey, setColorKeyState] = useState<string>(readStoredColorKey);
  const [source, setSourceState] = useState<ThemeColorSource>(readStoredSource);
  const [extracted, setExtractedState] = useState<string | null>(
    readStoredExtracted,
  );
  const [extractNonce, setExtractNonce] = useState(0);

  const setColorKey = useCallback((key: string) => {
    safeSet(STORAGE_KEY, key);
    setColorKeyState(key);
    // 手选颜色即视为"我要自己定"，顺手切回手动模式，避免选完没反应
    safeSet(STORAGE_KEY_SOURCE, "manual");
    setSourceState("manual");
  }, []);

  const setSource = useCallback((next: ThemeColorSource) => {
    safeSet(STORAGE_KEY_SOURCE, next);
    setSourceState(next);
  }, []);

  const setExtractedColor = useCallback((hex: string | null) => {
    if (!hex) return;
    safeSet(STORAGE_KEY_EXTRACTED, hex);
    setExtractedState(hex);
  }, []);

  const refreshExtraction = useCallback(() => {
    setExtractNonce((value) => value + 1);
  }, []);

  const effective = source === "background" && extracted ? extracted : colorKey;

  useEffect(() => {
    applyColorKey(effective);
  }, [effective]);

  return (
    <ThemeColorContext.Provider
      value={{
        colorKey,
        setColorKey,
        source,
        setSource,
        extracted,
        setExtractedColor,
        extractNonce,
        refreshExtraction,
      }}
    >
      {children}
    </ThemeColorContext.Provider>
  );
};
