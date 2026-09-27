/*
 * 外观快捷调节：毛玻璃（系统亚克力 / 面板毛玻璃）开关 + 窗口不透明度 /
 * 壁纸不透明度 / 背景模糊的滑杆，改动即时生效（与外观设置页同一套配置键）。
 */
import React, { useEffect, useState } from "react";
import { Button, Chip, Slider, Switch } from "@heroui/react";
import { PaintBrush20Regular } from "@fluentui/react-icons";

import { SetValue } from "../../../wailsjs/go/bindings/ConfigAPI";
import { SetAcrylicBackdropEnabled } from "../../../wailsjs/go/bindings/SystemAPI";
import { navigateToPage } from "../../lib/navigation";
import { t } from "../../i18n";
import {
  BACKGROUND_BLUR_KEY,
  BACKGROUND_OPACITY_KEY,
  BACKGROUND_WINDOW_OPACITY_KEY,
  PANEL_BLUR_KEY,
  useBackground,
} from "../../layouts/background";

import HomeCard from "./HomeCard";

function toPercent(value: number | number[]): number {
  return Math.round(Array.isArray(value) ? value[0] : value);
}

const AppearanceQuickCard: React.FC = () => {
  const { blur, opacity, windowOpacity, acrylic, panelBlur, refresh } =
    useBackground();
  const [windowOpacityValue, setWindowOpacityValue] = useState(windowOpacity);
  const [opacityValue, setOpacityValue] = useState(opacity);
  const [blurValue, setBlurValue] = useState(blur);

  useEffect(() => setWindowOpacityValue(windowOpacity), [windowOpacity]);
  useEffect(() => setOpacityValue(opacity), [opacity]);
  useEffect(() => setBlurValue(blur), [blur]);

  const toggleAcrylic = async (enabled: boolean) => {
    // Win11 22621+ 返回 true = 已热切换即时生效；旧系统走重启路径，同样刷新状态
    const appliedLive = await SetAcrylicBackdropEnabled(enabled);

    if (appliedLive) refresh();
  };

  const togglePanelBlur = async (enabled: boolean) => {
    await SetValue(PANEL_BLUR_KEY, enabled ? "true" : "false");
    refresh();
  };

  const saveWindowOpacity = async (value: number | number[]) => {
    await SetValue(BACKGROUND_WINDOW_OPACITY_KEY, String(toPercent(value)));
    refresh();
  };

  const saveOpacity = async (value: number | number[]) => {
    await SetValue(BACKGROUND_OPACITY_KEY, String(toPercent(value)));
    refresh();
  };

  const saveBlur = async (value: number | number[]) => {
    await SetValue(BACKGROUND_BLUR_KEY, String(toPercent(value)));
    refresh();
  };

  const sliderRow = (
    label: string,
    value: number,
    display: string,
    max: number,
    onChange: (v: number | number[]) => void,
    onChangeEnd: (v: number | number[]) => void,
  ) => (
    <div className="flex items-center gap-2">
      <span className="w-14 flex-none text-[11px] text-gray-400">{label}</span>
      <Slider
        aria-label={label}
        className="min-w-0 flex-1"
        maxValue={max}
        minValue={0}
        size="sm"
        step={1}
        value={value}
        onChange={onChange}
        onChangeEnd={onChangeEnd}
      />
      <span className="w-10 flex-none text-right text-[10px] tabular-nums text-gray-400">
        {display}
      </span>
    </div>
  );

  return (
    <HomeCard
      action={
        <Chip
          as="button"
          className="cursor-pointer"
          color="primary"
          size="sm"
          variant="flat"
          onClick={() => navigateToPage("appearance")}
        >
          {t("更多")}
        </Chip>
      }
      icon={<PaintBrush20Regular />}
      label={t("外观调节")}
      value={acrylic ? t("毛玻璃开") : t("毛玻璃关")}
    >
      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap gap-3">
          <Switch
            aria-label={t("毛玻璃（亚克力）")}
            color="primary"
            isSelected={acrylic}
            size="sm"
            onValueChange={(v) => void toggleAcrylic(v)}
          >
            <span className="text-[11px] text-gray-500 dark:text-gray-400">
              {t("毛玻璃（亚克力）")}
            </span>
          </Switch>
          <Switch
            aria-label={t("面板毛玻璃")}
            color="primary"
            isSelected={panelBlur}
            size="sm"
            onValueChange={(v) => void togglePanelBlur(v)}
          >
            <span className="text-[11px] text-gray-500 dark:text-gray-400">
              {t("面板毛玻璃")}
            </span>
          </Switch>
        </div>

        {sliderRow(
          t("窗口不透明度"),
          windowOpacityValue,
          `${windowOpacityValue}%`,
          100,
          (v) => setWindowOpacityValue(toPercent(v)),
          (v) => void saveWindowOpacity(v),
        )}
        {sliderRow(
          t("壁纸不透明度"),
          opacityValue,
          `${opacityValue}%`,
          100,
          (v) => setOpacityValue(toPercent(v)),
          (v) => void saveOpacity(v),
        )}
        {sliderRow(
          t("背景模糊"),
          blurValue,
          `${blurValue}px`,
          40,
          (v) => setBlurValue(toPercent(v)),
          (v) => void saveBlur(v),
        )}

        <Button
          fullWidth
          radius="lg"
          size="sm"
          startContent={<PaintBrush20Regular />}
          variant="flat"
          onPress={() => navigateToPage("appearance")}
        >
          {t("更多个性化设置")}
        </Button>
      </div>
    </HomeCard>
  );
};

export default AppearanceQuickCard;
