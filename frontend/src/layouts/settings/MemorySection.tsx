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
import { Slider, Switch } from "@heroui/react";

import {
  GetMemorySliderMaximum,
  GetManualMaximumMemoryMb,
  SaveManualMaximumMemoryMb,
  IsAutomaticMemoryAdjustmentEnabled,
  SetAutomaticMemoryAdjustmentEnabled,
  GetSystemMemory,
} from "../../../wailsjs/go/bindings/LauncherAPI";
import { t } from "../../i18n";

import Section, { SettingRow } from "./Section";

const MemorySection: React.FC = () => {
  const [systemTotalMb, setSystemTotalMb] = useState(0);
  const [usedMb, setUsedMb] = useState(0);
  const [sliderMax, setSliderMax] = useState(8192);
  const [memoryMb, setMemoryMb] = useState(4096);
  const [isAuto, setIsAuto] = useState(true);
  const [, setIsLoading] = useState(true);
  const [saveHint, setSaveHint] = useState("");

  useEffect(() => {
    (async () => {
      const [max, cur, auto] = await Promise.all([
        GetMemorySliderMaximum(),
        GetManualMaximumMemoryMb(),
        IsAutomaticMemoryAdjustmentEnabled(),
      ]);

      setSliderMax(max || 8192);
      const clamped = Math.min(Math.max(cur || 4096, 512), max || 8192);

      setMemoryMb(clamped);
      setIsAuto(auto);
      setIsLoading(false);
    })();
  }, []);

  // 系统内存占用：进入页面拉一次，之后每 5 秒刷新，驱动占用条实时变化
  useEffect(() => {
    let cancelled = false;
    const refresh = async () => {
      try {
        const sys = await GetSystemMemory();

        if (cancelled) return;
        const total = sys?.TotalMemoryMb ?? 0;
        const available = sys?.AvailableMemoryMb ?? 0;

        setSystemTotalMb(total);
        setUsedMb(Math.max(0, total - available));
      } catch {
        /* 拉不到就不显示占用条 */
      }
    };

    void refresh();
    const timer = setInterval(() => void refresh(), 5000);

    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, []);

  const commit = async (mb: number) => {
    const ok = await SaveManualMaximumMemoryMb(mb);

    setSaveHint(ok ? t("已保存：{0} MB", { "0": mb }) : t("保存失败"));
    setTimeout(() => setSaveHint(""), 2000);
  };

  return (
    <Section
      aliases={[
        t("内存"),
        "memory",
        "ram",
        t("最大"),
        t("最小"),
        t("自动调整"),
        t("占用"),
      ]}
      title={t("内存")}
    >
      <SettingRow label={t("自动调整内存")}>
        <Switch
          color="primary"
          isSelected={isAuto}
          onValueChange={async (v) => {
            setIsAuto(v);
            await SetAutomaticMemoryAdjustmentEnabled(v);
          }}
        />
      </SettingRow>

      <div className={`py-3 ${isAuto ? "opacity-50 pointer-events-none" : ""}`}>
        {isAuto ? (
          <div className="mb-2 rounded-xl bg-default-100 px-3 py-2 text-xs text-gray-500 dark:bg-default-100/50">
            {t(
              "已开启自动调整：启动器按游戏版本与系统剩余内存自动分配，无需手动设置。",
            )}
          </div>
        ) : null}
        <div className="flex items-center justify-between mb-2">
          <div className="text-sm text-gray-800 dark:text-gray-200">
            {t("最大内存")}
          </div>
          <div className="text-sm text-primary font-medium tabular-nums">
            {memoryMb}MB
          </div>
        </div>
        {/* 合并式占用条：同一条轨道上，琥珀色段 = 系统已占用（垫底），
            主题色滑块 = 分配给游戏的内存，轨道右侧灰色 = 剩余可用 */}
        {systemTotalMb > 0 && (
          <div className="mb-1.5 flex items-center justify-between text-xs">
            <span className="text-gray-400">
              {t("系统内存占用 · 已用")} {usedMb}MB
            </span>
            <span className="font-medium tabular-nums text-amber-500 dark:text-amber-400">
              {t("剩余")} {Math.max(0, systemTotalMb - usedMb)}MB
            </span>
          </div>
        )}
        <div className="relative">
          <div
            aria-hidden
            className="pointer-events-none absolute inset-x-0 top-1/2 h-2.5 -translate-y-1/2 overflow-hidden rounded-full bg-default-200/70"
          >
            <div
              className="h-full rounded-full bg-amber-400 transition-[width] duration-500 dark:bg-amber-500/90"
              style={{
                width: `${Math.min(100, sliderMax > 0 ? (usedMb / sliderMax) * 100 : 0)}%`,
              }}
            />
          </div>
          <Slider
            showTooltip
            aria-label={t("最大内存")}
            classNames={{
              track: "bg-transparent!",
              filler: "bg-primary",
            }}
            color="primary"
            fillOffset={512}
            getValue={(v) => `${v} MB`}
            maxValue={sliderMax}
            minValue={512}
            step={256}
            value={memoryMb}
            onChange={(v) => setMemoryMb(Array.isArray(v) ? v[0] : v)}
            onChangeEnd={(v) => commit(Array.isArray(v) ? v[0] : v)}
          />
        </div>
        <div className="flex items-center justify-between mt-1 text-xs text-gray-400">
          <span>512 MB</span>
          <span>{sliderMax}MB</span>
        </div>
        {saveHint && (
          <div className="text-xs text-primary mt-2">{saveHint}</div>
        )}
      </div>
    </Section>
  );
};

export default MemorySection;
