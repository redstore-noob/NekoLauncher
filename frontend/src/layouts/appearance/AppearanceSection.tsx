/*
 * 外观设置分区：启动器背景图源、窗口不透明度、亚克力模糊与背景模糊效果。
 * 窗口底色不透明度存 launcherWindowOpacity（0-100，越低桌面越透出）；
 * 亚克力模糊存 launcherAcrylicEnabled（Win11 22621+ 运行时热切换即时生效，
 * 旧系统由后端自动重启应用）；图源存 launcherBackgroundMode（none=纯白默认 /
 * wallpaper=桌面壁纸 / bing=必应每日 / image=自定义图片 /
 * wallpaper-engine=Wallpaper Engine 联动，支持静态图 / 视频 / 网页三类壁纸），
 * 自定义图存 launcherBackgroundPath，壁纸不透明度存 launcherBackgroundOpacity（0-100），
 * 模糊半径存 launcherBackgroundBlur（0 = 不模糊），面板毛玻璃存
 * launcherPanelBlurEnabled + launcherPanelBlurStrength（0-100），网页壁纸交互存
 * launcherWebWallpaperInteractive。本地图片均经 /localfile 路由中转，
 * 网页壁纸资源经 /wwwallpaper 路由中转。
 * 主题模式（浅色/深色/跟随系统）只影响外观，存前端 localStorage，见 src/theme.tsx；
 * 主题色支持手选与"跟随背景自动取色"，见 src/theme-color.tsx 与 src/lib/monet.ts。
 */
import React, { useEffect, useState } from "react";
import { Button, Checkbox, Slider, Switch } from "@heroui/react";
import {
  ArrowClockwise20Regular,
  WeatherMoon20Filled,
  WeatherMoon20Regular,
  WeatherSunny20Filled,
  WeatherSunny20Regular,
} from "@fluentui/react-icons";

import {
  SelectFile,
  GetWallpaperEngineWallpaper,
  SetAcrylicBackdropEnabled,
} from "../../../wailsjs/go/bindings/SystemAPI";
import {
  GetValue,
  SetValue,
  ClearValue,
} from "../../../wailsjs/go/bindings/ConfigAPI";
import {
  BACKGROUND_PATH_KEY,
  BACKGROUND_BLUR_KEY,
  BACKGROUND_MODE_KEY,
  BACKGROUND_OPACITY_KEY,
  BACKGROUND_SCRIM_KEY,
  BACKGROUND_WINDOW_OPACITY_KEY,
  PANEL_BLUR_KEY,
  PANEL_BLUR_STRENGTH_KEY,
  WEB_WALLPAPER_INTERACTIVE_KEY,
  useBackground,
  BackgroundMode,
} from "../background";
import { useThemeMode } from "../../theme";
import { LOCALE_OPTIONS, useI18n } from "../../i18n";
import { useThemeColor, THEME_COLOR_PRESETS } from "../../theme-color";
import { useSimpleMode } from "../simple-mode";
import {
  HOME_WIDGET_COLUMNS_KEY,
  emitWidgetColumns,
  parseWidgetColumns,
} from "../../lib/home";
import Section, { SettingRow } from "../settings/Section";

/** 图源切换按钮组定义（顺序即展示顺序） */
const MODE_OPTIONS: Array<{ value: BackgroundMode; label: string }> = [
  { value: "none", label: "纯色" },
  { value: "wallpaper", label: "桌面壁纸" },
  { value: "bing", label: "必应每日" },
  { value: "image", label: "自选图片" },
  { value: "wallpaper-engine", label: "Wallpaper Engine" },
];

/** WE 联动可用性：checking=检测中 / ok=已找到并解析出壁纸 / missing=未检测到 /
 * unsupported=当前平台不支持（WE 只有 Windows 版） */
type WeStatus = "checking" | "ok" | "missing" | "unsupported";

const AppearanceSection: React.FC = () => {
  const {
    mode,
    isCustom,
    blur,
    opacity,
    scrim,
    windowOpacity,
    acrylic,
    panelBlur,
    panelBlurStrength,
    webInteractive,
    wallpaperEngineTitle,
    wallpaperEngineType,
    wallpaperEngineUnsupported: weUnsupported,
    refresh,
  } = useBackground();
  const { locale, setLocale, t } = useI18n();
  const { simpleMode, setSimpleMode } = useSimpleMode();
  // NekoLauncher-S 简洁模式（外观/界面结构开关，状态由全局 Provider 持有）
  const {
    mode: themeMode,
    resolved: resolvedTheme,
    setMode: setThemeMode,
  } = useThemeMode();
  const isDark = resolvedTheme === "dark";
  const {
    colorKey,
    setColorKey,
    source: colorSource,
    setSource: setColorSource,
    extracted,
    refreshExtraction,
  } = useThemeColor();
  const customColor = colorKey.startsWith("#") ? colorKey : "#006FEE";
  const [blurValue, setBlurValue] = useState<number>(blur);
  const [opacityValue, setOpacityValue] = useState<number>(opacity);
  const [scrimValue, setScrimValue] = useState<number>(scrim);
  const [panelBlurStrengthValue, setPanelBlurStrengthValue] =
    useState<number>(panelBlurStrength);
  const [windowOpacityValue, setWindowOpacityValue] =
    useState<number>(windowOpacity);
  // WE 可用性探测：无论当前模式，挂载时查一次，供切换前提示
  const [weStatus, setWeStatus] = useState<WeStatus>("checking");
  const [weProbeTitle, setWeProbeTitle] = useState("");
  const [weProbeType, setWeProbeType] = useState("");
  // 主页小组件列数（1~3，存 homeWidgetColumns，改动实时广播到主页）
  const [widgetColumns, setWidgetColumns] = useState(1);

  useEffect(() => {
    GetValue(HOME_WIDGET_COLUMNS_KEY)
      .then((raw) => {
        const saved = parseWidgetColumns(raw ?? "");

        if (saved) setWidgetColumns(saved);
      })
      .catch(() => {
        /* 读配置失败按 1 列显示 */
      });
  }, []);

  useEffect(() => {
    setBlurValue(blur);
  }, [blur]);
  useEffect(() => {
    setOpacityValue(opacity);
  }, [opacity]);
  useEffect(() => {
    setScrimValue(scrim);
  }, [scrim]);
  useEffect(() => {
    setWindowOpacityValue(windowOpacity);
  }, [windowOpacity]);
  useEffect(() => {
    setPanelBlurStrengthValue(panelBlurStrength);
  }, [panelBlurStrength]);
  useEffect(() => {
    GetWallpaperEngineWallpaper()
      .then((wp) => {
        if (wp?.Unsupported) {
          setWeStatus("unsupported");
        } else if (wp?.Path) {
          setWeStatus("ok");
          setWeProbeTitle(wp.Title || "");
          setWeProbeType(wp.Type || "");
        } else {
          setWeStatus("missing");
        }
      })
      .catch(() => setWeStatus("missing"));
  }, []);

  // 亚克力模糊开关状态来自背景上下文（acrylic）。热切换成功（返回 true）后
  // refresh() 让状态立刻跟随；返回 false 表示旧版 Windows 已走重启路径。
  const toggleAcrylic = async (enabled: boolean) => {
    const appliedLive = await SetAcrylicBackdropEnabled(enabled);

    if (appliedLive) refresh();
  };

  // 面板毛玻璃（前端 backdrop-filter）：写入 launcherPanelBlurEnabled 后由
  // BackgroundProvider 把开关 + 强度落到 <html data-panel-blur> 与 --nya-blur-scale，
  // 全部面板即时切换
  const togglePanelBlur = async (enabled: boolean) => {
    await SetValue(PANEL_BLUR_KEY, enabled ? "true" : "false");
    refresh();
  };

  const savePanelBlurStrength = async (value: number | number[]) => {
    const percent = Math.round(Array.isArray(value) ? value[0] : value);

    await SetValue(PANEL_BLUR_STRENGTH_KEY, String(percent));
    refresh();
  };

  const toggleWebWallpaperInteractive = async (enabled: boolean) => {
    await SetValue(WEB_WALLPAPER_INTERACTIVE_KEY, enabled ? "true" : "false");
    refresh();
  };

  const saveMode = async (next: BackgroundMode) => {
    if (next === mode) return;
    await SetValue(BACKGROUND_MODE_KEY, next);
    refresh();
  };

  const pickBackground = async () => {
    let path = "";

    try {
      path = await SelectFile(
        t("选择背景图片"),
        t("图片文件"),
        "*.png;*.jpg;*.jpeg;*.webp;*.gif;*.bmp",
      );
    } catch {
      /* 用户取消 */
    }
    if (!path) return;
    await SetValue(BACKGROUND_PATH_KEY, path);
    if (mode !== "image") await SetValue(BACKGROUND_MODE_KEY, "image");
    refresh();
  };

  const clearBackground = async () => {
    await ClearValue(BACKGROUND_PATH_KEY);
    refresh();
  };

  const saveOpacity = async (value: number | number[]) => {
    const percent = Math.round(Array.isArray(value) ? value[0] : value);

    await SetValue(BACKGROUND_OPACITY_KEY, String(percent));
    refresh();
  };

  const saveWindowOpacity = async (value: number | number[]) => {
    const percent = Math.round(Array.isArray(value) ? value[0] : value);

    await SetValue(BACKGROUND_WINDOW_OPACITY_KEY, String(percent));
    refresh();
  };

  const saveBlur = async (value: number | number[]) => {
    const px = Math.round(Array.isArray(value) ? value[0] : value);

    await SetValue(BACKGROUND_BLUR_KEY, String(px));
    refresh();
  };

  const saveScrim = async (value: number | number[]) => {
    const percent = Math.round(Array.isArray(value) ? value[0] : value);

    await SetValue(BACKGROUND_SCRIM_KEY, String(percent));
    refresh();
  };

  const saveWidgetColumns = async (count: number) => {
    setWidgetColumns(count);
    try {
      await SetValue(HOME_WIDGET_COLUMNS_KEY, String(count));
      emitWidgetColumns(count);
    } catch {
      /* 持久化失败不阻断界面 */
    }
  };

  // WE 模式下的状态提示：优先用实时联动数据，未启用时用挂载探测结果
  const weActiveTitle = wallpaperEngineTitle || weProbeTitle;
  const weActiveType = wallpaperEngineType || weProbeType;
  const weHint = (() => {
    // 非 Windows：WE 本身只有 Windows 版，别让用户对着一个永远加载不出的图源发呆
    if (weUnsupported)
      return t(
        "Wallpaper Engine 联动目前只在 Windows 上可用，请改用桌面壁纸或自定义图片。",
      );
    if (weStatus === "checking") return t("正在检测 Wallpaper Engine…");
    if (weStatus === "missing")
      return t("未检测到 Wallpaper Engine（需通过 Steam 安装并设置壁纸）");
    const suffix =
      weActiveType === "video"
        ? t("（动态壁纸，播放原始视频）")
        : weActiveType === "web"
          ? t("（网页壁纸，在背景里运行）")
          : weActiveType && weActiveType !== "img"
            ? t("（动态壁纸，展示静态原画）")
            : "";

    return weActiveTitle
      ? t("当前壁纸：{title}{suffix}", { title: weActiveTitle, suffix })
      : t("已检测到 Wallpaper Engine{suffix}", { suffix });
  })();

  return (
    <Section
      aliases={[
        t("主题"),
        t("颜色"),
        t("背景"),
        t("壁纸"),
        t("透明度"),
        t("模糊"),
        t("亚克力"),
        t("面板"),
        t("必应"),
        t("图源"),
        t("桌面"),
        t("小组件"),
        t("列数"),
        t("布局"),
        t("简洁模式"),
        "theme",
        "wallpaper",
        "widget",
        "simple",
      ]}
      title={t("外观")}
    >
      <SettingRow
        hint={t(
          "只保留启动、外观、下载、账号与设置五个页面；NekoSolo 安装包装的启动器默认开启",
        )}
        label={t("NekoLauncher-S 简洁模式")}
      >
        <Switch
          aria-label={t("NekoLauncher-S 简洁模式")}
          color="primary"
          isSelected={simpleMode}
          size="sm"
          onValueChange={setSimpleMode}
        />
      </SettingRow>

      <SettingRow label={t("语言")}>
        <div className="flex flex-wrap justify-end gap-1">
          {LOCALE_OPTIONS.map((option) => (
            <Button
              key={option.value}
              color={locale === option.value ? "primary" : "default"}
              size="sm"
              variant={locale === option.value ? "solid" : "flat"}
              onPress={() => setLocale(option.value)}
            >
              {option.label}
            </Button>
          ))}
        </div>
      </SettingRow>

      <SettingRow label={t("主题")}>
        <div className="flex items-center justify-end gap-2">
          {/* 太阳 / 月亮开关：点击在浅色与深色间切换 */}
          <button
            aria-checked={isDark}
            aria-label={t(isDark ? t("切换到浅色") : t("切换到深色"))}
            className={`relative flex h-8 w-[68px] cursor-pointer items-center justify-between rounded-full border nya-border px-2 transition-colors ${
              isDark ? "bg-indigo-500/15" : "bg-amber-400/20"
            }`}
            role="switch"
            title={t(isDark ? t("切换到浅色") : t("切换到深色"))}
            type="button"
            onClick={() => setThemeMode(isDark ? "light" : "dark")}
          >
            <span
              className={`text-amber-500 transition-opacity ${
                isDark ? "opacity-100" : "opacity-0"
              }`}
            >
              <WeatherSunny20Regular />
            </span>
            <span
              className={`text-slate-500 transition-opacity dark:text-slate-300 ${
                isDark ? "opacity-0" : "opacity-100"
              }`}
            >
              <WeatherMoon20Regular />
            </span>
            <span
              className={`absolute top-1/2 flex size-6 -translate-y-1/2 items-center justify-center rounded-full text-white shadow-md transition-all duration-300 ${
                isDark ? "left-[40px] bg-indigo-500" : "left-1 bg-amber-400"
              }`}
            >
              {isDark ? <WeatherMoon20Filled /> : <WeatherSunny20Filled />}
            </span>
          </button>

          {/* 跟随系统：勾选后忽略上面的手动选择，实时跟随系统深浅色 */}
          <Checkbox
            isSelected={themeMode === "system"}
            size="sm"
            onValueChange={(selected) =>
              setThemeMode(selected ? "system" : resolvedTheme)
            }
          >
            <span className="text-xs text-gray-500 dark:text-gray-400">
              {t("跟随系统")}
            </span>
          </Checkbox>
        </div>
      </SettingRow>

      <SettingRow label={t("取色方式")}>
        <div className="flex flex-wrap items-center gap-1.5 justify-end">
          <Button
            color={colorSource === "manual" ? "primary" : "default"}
            size="sm"
            variant={colorSource === "manual" ? "solid" : "flat"}
            onPress={() => setColorSource("manual")}
          >
            {t("手动选色")}
          </Button>
          <Button
            color={colorSource === "background" ? "primary" : "default"}
            size="sm"
            variant={colorSource === "background" ? "solid" : "flat"}
            onPress={() => setColorSource("background")}
          >
            {t("跟随背景")}
          </Button>
          {colorSource === "background" ? (
            <>
              <span
                aria-label={t("当前取色结果")}
                className="h-6 w-6 flex-shrink-0 rounded-full border border-black/10 ring-1 ring-black/5 dark:ring-white/20"
                style={{ backgroundColor: extracted ?? customColor }}
                title={extracted ?? t("尚未取到颜色")}
              />
              <Button
                isIconOnly
                aria-label={t("重新取色")}
                size="sm"
                title={t("重新取色")}
                variant="light"
                onPress={refreshExtraction}
              >
                <ArrowClockwise20Regular />
              </Button>
            </>
          ) : null}
        </div>
      </SettingRow>

      <SettingRow label={t("主题色")}>
        <div className="flex flex-wrap items-center gap-1.5 justify-end">
          {THEME_COLOR_PRESETS.map((preset) => (
            <button
              key={preset.id}
              aria-label={t("主题色 {0}", { "0": preset.label })}
              className={`h-6 w-6 rounded-full border-2 transition-transform hover:scale-110 ${
                colorKey.toLowerCase() === preset.id.toLowerCase()
                  ? "border-gray-900 dark:border-white"
                  : "border-transparent"
              }`}
              style={{ backgroundColor: preset.hex }}
              title={t(preset.label)}
              type="button"
              onClick={() => setColorKey(preset.id)}
            />
          ))}
          <label
            className={`relative inline-flex h-6 cursor-pointer items-center gap-1.5 rounded-full border px-2 text-xs transition-colors ${
              colorKey.startsWith("#")
                ? "border-transparent bg-primary/15 text-primary"
                : "nya-border text-gray-600 dark:text-gray-300"
            }`}
            title={t("自定义颜色")}
          >
            <span
              className="h-3 w-3 flex-shrink-0 rounded-full ring-1 ring-black/10 dark:ring-white/20"
              style={{ backgroundColor: customColor }}
            />
            {t("自定义")}
            <input
              aria-label={t("自定义主题色")}
              className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
              type="color"
              value={customColor}
              onChange={(e) => setColorKey(e.target.value)}
            />
          </label>
        </div>
      </SettingRow>

      <SettingRow label={t("背景不透明度")}>
        <div className="flex items-center gap-3 w-48">
          <Slider
            aria-label={t("背景不透明度")}
            maxValue={100}
            minValue={0}
            size="sm"
            step={1}
            value={windowOpacityValue}
            onChange={(v) =>
              setWindowOpacityValue(Math.round(Array.isArray(v) ? v[0] : v))
            }
            onChangeEnd={(v) => void saveWindowOpacity(v)}
          />
          <span className="text-xs text-gray-400 w-12 text-right">
            {windowOpacityValue}%
          </span>
        </div>
      </SettingRow>

      <SettingRow label={t("亚克力模糊")}>
        <Switch
          aria-label={t("亚克力模糊")}
          color="primary"
          isSelected={acrylic}
          size="sm"
          onValueChange={(v) => void toggleAcrylic(v)}
        />
      </SettingRow>

      <SettingRow label={t("面板毛玻璃")}>
        <Switch
          aria-label={t("面板毛玻璃")}
          color="primary"
          isSelected={panelBlur}
          size="sm"
          onValueChange={(v) => void togglePanelBlur(v)}
        />
      </SettingRow>

      <SettingRow
        hint={t("越强越透；背景压暗度越低越明显")}
        label={t("毛玻璃强度")}
      >
        <div className="flex items-center gap-3 w-48">
          <Slider
            aria-label={t("毛玻璃强度")}
            isDisabled={!panelBlur}
            maxValue={100}
            minValue={0}
            size="sm"
            step={5}
            value={panelBlurStrengthValue}
            onChange={(v) =>
              setPanelBlurStrengthValue(Math.round(Array.isArray(v) ? v[0] : v))
            }
            onChangeEnd={(v) => void savePanelBlurStrength(v)}
          />
          <span className="text-xs text-gray-400 w-12 text-right">
            {panelBlurStrengthValue}%
          </span>
        </div>
      </SettingRow>

      {/* S 模式禁用小组件功能，列数设置随之隐藏 */}
      {!simpleMode ? (
        <SettingRow label={t("小组件列数")}>
          <div className="flex flex-wrap gap-1 justify-end">
            {[1, 2, 3].map((count) => (
              <Button
                key={count}
                color={widgetColumns === count ? "primary" : "default"}
                size="sm"
                variant={widgetColumns === count ? "solid" : "flat"}
                onPress={() => void saveWidgetColumns(count)}
              >
                {t("{count} 列", { count })}
              </Button>
            ))}
          </div>
        </SettingRow>
      ) : null}

      <SettingRow label={t("背景图源")}>
        <div className="flex flex-wrap gap-1 max-w-[320px] justify-end">
          {MODE_OPTIONS.map((option) => (
            <Button
              key={option.value}
              color={mode === option.value ? "primary" : "default"}
              size="sm"
              variant={mode === option.value ? "solid" : "flat"}
              onPress={() => void saveMode(option.value)}
            >
              {t(option.label)}
            </Button>
          ))}
        </div>
      </SettingRow>

      {mode === "image" && (
        <SettingRow label={t("自定义图片")}>
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="flat"
              onPress={() => void pickBackground()}
            >
              {t("选择图片")}
            </Button>
            {isCustom && (
              <Button
                color="danger"
                size="sm"
                variant="light"
                onPress={() => void clearBackground()}
              >
                {t("清除")}
              </Button>
            )}
          </div>
        </SettingRow>
      )}

      {mode === "wallpaper-engine" && (
        <>
          <SettingRow hint={weHint} label="Wallpaper Engine">
            <span />
          </SettingRow>
          {wallpaperEngineType.toLowerCase() === "web" && (
            <SettingRow
              hint={t("开启后壁纸会接收鼠标移动与点击")}
              label={t("允许与网页壁纸交互")}
            >
              <Switch
                aria-label={t("允许与网页壁纸交互")}
                color="primary"
                isSelected={webInteractive}
                size="sm"
                onValueChange={(v) => void toggleWebWallpaperInteractive(v)}
              />
            </SettingRow>
          )}
        </>
      )}

      {mode !== "none" && (
        <>
          <SettingRow label={t("壁纸不透明度")}>
            <div className="flex items-center gap-3 w-48">
              <Slider
                aria-label={t("壁纸不透明度")}
                maxValue={100}
                minValue={0}
                size="sm"
                step={1}
                value={opacityValue}
                onChange={(v) =>
                  setOpacityValue(Math.round(Array.isArray(v) ? v[0] : v))
                }
                onChangeEnd={(v) => void saveOpacity(v)}
              />
              <span className="text-xs text-gray-400 w-12 text-right">
                {opacityValue}%
              </span>
            </div>
          </SettingRow>

          <SettingRow hint={t("保证壁纸上的文字可读")} label={t("背景压暗度")}>
            <div className="flex items-center gap-3 w-48">
              <Slider
                aria-label={t("背景压暗度")}
                maxValue={100}
                minValue={0}
                size="sm"
                step={1}
                value={scrimValue}
                onChange={(v) =>
                  setScrimValue(Math.round(Array.isArray(v) ? v[0] : v))
                }
                onChangeEnd={(v) => void saveScrim(v)}
              />
              <span className="text-xs text-gray-400 w-12 text-right">
                {scrimValue}%
              </span>
            </div>
          </SettingRow>

          <SettingRow label={t("背景模糊")}>
            <div className="flex items-center gap-3 w-48">
              <Slider
                aria-label={t("背景模糊")}
                maxValue={40}
                minValue={0}
                size="sm"
                step={1}
                value={blurValue}
                onChange={(v) =>
                  setBlurValue(Math.round(Array.isArray(v) ? v[0] : v))
                }
                onChangeEnd={(v) => void saveBlur(v)}
              />
              <span className="text-xs text-gray-400 w-12 text-right">
                {blurValue}px
              </span>
            </div>
          </SettingRow>
        </>
      )}
    </Section>
  );
};

export default AppearanceSection;
