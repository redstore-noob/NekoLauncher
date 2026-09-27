/*
 * 国际化（轻量自研）：以「中文原文」作为 key，t("中文", { 变量 }) 取当前语言译文。
 *
 * 为什么用原文当 key：本项目文案以静态中文 UI 字符串为主（约 2000 处），
 * 若额外维护一套语义 key，等于给每条文案再起一次名，成本高且容易漂移。原文
 * 当 key 时，zh-CN 无需词典（原文即译文），只需维护 en-US / zh-TW 两份映射；
 * 词典缺条目时自动回退原文，不会出现空白或 key 泄漏。
 *
 * 其它约定：
 * - 插值用 `{name}` 占位，如 t("已导入 {count} 个文件", { count: 3 })。
 * - 语言存 localStorage，需在首帧前同步读取（见 initLocale），避免闪烁。
 * - 组件里用 useI18n() 取 t；非组件场景（数据表）保留中文原文，在展示处调 t()。
 */
import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";

import enUS from "./locales/en-US";
import jaJP from "./locales/ja-JP";
import ruRU from "./locales/ru-RU";
import zhTW from "./locales/zh-TW";

export type Locale = "zh-CN" | "zh-TW" | "en-US" | "ja-JP" | "ru-RU";

export const LOCALE_OPTIONS: Array<{ value: Locale; label: string }> = [
  { value: "zh-CN", label: "简体中文" },
  { value: "zh-TW", label: "繁體中文" },
  { value: "en-US", label: "English" },
  { value: "ja-JP", label: "日本語" },
  { value: "ru-RU", label: "Русский" },
];

const STORAGE_KEY = "nekolauncher-locale";

type Dictionary = Record<string, string>;

/** zh-CN 无需词典（原文即译文），仅作为回退基准 */
const DICTIONARIES: Record<Locale, Dictionary> = {
  "zh-CN": {},
  "zh-TW": zhTW,
  "en-US": enUS,
  "ja-JP": jaJP,
  "ru-RU": ruRU,
};

/** 条目缺失时的兜底语言（日/俄缺失先回退英文，再回退中文原文） */
const FALLBACKS: Partial<Record<Locale, Locale>> = {
  "ja-JP": "en-US",
  "ru-RU": "en-US",
};

const LOCALES: Locale[] = ["zh-CN", "zh-TW", "en-US", "ja-JP", "ru-RU"];

function isLocale(value: string | null): value is Locale {
  return value !== null && (LOCALES as string[]).includes(value);
}

function readStoredLocale(): Locale | null {
  const stored = localStorage.getItem(STORAGE_KEY);

  return isLocale(stored) ? stored : null;
}

/** 按浏览器语言猜测：zh-Hant/zh-TW/zh-HK → 繁体；其余中文 → 简中；en → 英文 */
function detectLocale(): Locale {
  const candidates = navigator.languages?.length
    ? navigator.languages
    : [navigator.language];

  for (const raw of candidates) {
    const lang = (raw || "").toLowerCase();

    if (lang.startsWith("zh")) {
      return /(hant|tw|hk|mo)/.test(lang) ? "zh-TW" : "zh-CN";
    }
    if (lang.startsWith("en")) return "en-US";
    if (lang.startsWith("ja")) return "ja-JP";
    if (lang.startsWith("ru")) return "ru-RU";
  }

  return "zh-CN";
}

function initialLocale(): Locale {
  return readStoredLocale() ?? detectLocale();
}

function interpolate(text: string, params?: Record<string, string | number>) {
  if (!params) return text;
  let result = text;

  for (const [key, value] of Object.entries(params)) {
    result = result.split(`{${key}}`).join(String(value));
  }

  return result;
}

/** 纯函数翻译：给定语言与原文，返回译文（缺条目回退原文）。 */
export function translate(
  locale: Locale,
  source: string,
  params?: Record<string, string | number>,
): string {
  const fallback = FALLBACKS[locale];
  // 用 || 而不是 ??：空串也是"没有译文"，应当继续回退，否则会渲染出空白
  const text =
    DICTIONARIES[locale][source] ||
    (fallback ? DICTIONARIES[fallback][source] : "") ||
    source;

  return interpolate(text, params);
}

let activeLocale: Locale = initialLocale();

/** 同步全局 t() 读到的当前语言；写 <html lang> 由 I18nProvider 的 effect 负责。 */
export function setActiveLocale(locale: Locale) {
  activeLocale = locale;
}

/**
 * 全局翻译函数（不依赖 React hook）：供组件体、事件回调、模块级数据表等
 * 任意位置调用。语言切换时 I18nProvider 的 state 更新会让整棵子树重渲染，
 * 因此全局 t() 的结果也会刷新。
 */
export function t(
  source: string,
  params?: Record<string, string | number>,
): string {
  return translate(activeLocale, source, params);
}

/** 渲染前调用：让根节点 lang 与已保存语言一致，避免首帧语义不符。 */
export function initLocale() {
  document.documentElement.lang = activeLocale;
}

export interface I18nContextValue {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: (source: string, params?: Record<string, string | number>) => string;
}

const I18nContext = createContext<I18nContextValue>({
  locale: "zh-CN",
  setLocale: () => {
    /* Provider 未挂载时的空实现 */
  },
  t: (source, params) => translate("zh-CN", source, params),
});

export function useI18n(): I18nContextValue {
  return useContext(I18nContext);
}

export const I18nProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [locale, setLocaleState] = useState<Locale>(initialLocale);

  // 全局 t() 读的是模块级 activeLocale，必须在渲染期同步（模块级数据表中也会
  // 调 t()）。写 DOM 属副作用，交给下面的 effect。
  setActiveLocale(locale);

  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  const setLocale = useCallback((next: Locale) => {
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // 存储不可用（隐私模式 / 配额满）时仍然切语言，只是记不住
    }
    setLocaleState(next);
    // 重载以刷新模块加载期就写死的文案（如小组件注册表里的标题/描述）
    window.location.reload();
  }, []);

  const tHook = useCallback(
    (source: string, params?: Record<string, string | number>) =>
      translate(locale, source, params),
    [locale],
  );

  return (
    <I18nContext.Provider value={{ locale, setLocale, t: tHook }}>
      {children}
    </I18nContext.Provider>
  );
};
