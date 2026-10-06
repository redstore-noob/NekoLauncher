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
import React, { useEffect, useRef, useState } from "react";
import { Switch } from "@heroui/react";

import {
  GetMemorySliderMaximum,
  GetManualMaximumMemoryMb,
  SaveManualMaximumMemoryMb,
  IsAutomaticMemoryAdjustmentEnabled,
  SetAutomaticMemoryAdjustmentEnabled,
  GetSystemMemory,
} from "../../../wailsjs/go/bindings/LauncherAPI";
import { t } from "../../i18n";
import { startVisiblePoll } from "../../lib/visibility";

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
    const stop = startVisiblePoll(() => void refresh(), 5000);

    return () => {
      cancelled = true;
      stop();
    };
  }, []);

  const commit = async (mb: number) => {
    const ok = await SaveManualMaximumMemoryMb(mb);

    setSaveHint(ok ? t("已保存：{0} MB", { "0": mb }) : t("保存失败"));
    setTimeout(() => setSaveHint(""), 2000);
  };

  // ---- 单轨道内存条 ----
  // 一整条轨道、两种颜色：琥珀色 = 系统已占用（垫底），主题色 = 分配给游戏的
  // 内存（叠在上层、右端带手柄），右侧灰 = 剩余可用。不再用 HeroUI Slider +
  // 外层绝对定位的组合——两边轨道高度对不齐，视觉上会裂成两条。
  const MIN_MB = 512;
  const trackRef = useRef<HTMLDivElement>(null);
  const draggingRef = useRef(false);

  const gamePct =
    sliderMax > MIN_MB
      ? Math.min(
          100,
          Math.max(0, ((memoryMb - MIN_MB) / (sliderMax - MIN_MB)) * 100),
        )
      : 0;
  const usedPct =
    systemTotalMb > 0 ? Math.min(100, (usedMb / sliderMax) * 100) : 0;

  const valueFromPointer = (clientX: number) => {
    const rect = trackRef.current?.getBoundingClientRect();

    if (!rect || rect.width === 0) return memoryMb;
    const ratio = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width));
    const raw = MIN_MB + ratio * (sliderMax - MIN_MB);
    const stepped = Math.round(raw / 256) * 256;

    return Math.min(sliderMax, Math.max(MIN_MB, stepped));
  };

  const onPointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    draggingRef.current = true;
    e.currentTarget.setPointerCapture(e.pointerId);
    setMemoryMb(valueFromPointer(e.clientX));
  };
  const onPointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!draggingRef.current) return;
    setMemoryMb(valueFromPointer(e.clientX));
  };
  const onPointerUp = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!draggingRef.current) return;
    draggingRef.current = false;
    const final = valueFromPointer(e.clientX);

    setMemoryMb(final);
    void commit(final);
  };
  const onKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    // 步进 256MB，与拖拽吸附保持一致
    const delta = e.shiftKey ? 1024 : 256;
    let next: number | null = null;

    if (e.key === "ArrowLeft" || e.key === "ArrowDown") next = memoryMb - delta;
    if (e.key === "ArrowRight" || e.key === "ArrowUp") next = memoryMb + delta;
    if (next === null) return;
    e.preventDefault();
    const clamped = Math.min(sliderMax, Math.max(MIN_MB, next));

    setMemoryMb(clamped);
    void commit(clamped);
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
          <div className="mb-2 rounded-lg bg-default-100 px-3 py-2 text-xs text-gray-500 dark:bg-default-100/50">
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
        {/* 单轨道双色内存条：琥珀 = 系统已占用（垫底），主题色 = 分配给游戏
            （叠在上层、右端手柄），右侧灰 = 剩余可用 */}
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
        <div
          ref={trackRef}
          aria-label={t("最大内存")}
          aria-valuemax={sliderMax}
          aria-valuemin={MIN_MB}
          aria-valuenow={memoryMb}
          className="relative flex h-8 cursor-pointer touch-none select-none items-center"
          role="slider"
          tabIndex={0}
          onKeyDown={onKeyDown}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
        >
          <div className="relative h-2.5 w-full overflow-hidden rounded-full bg-default-200/70">
            <div
              className="absolute inset-y-0 left-0 rounded-full bg-amber-400 transition-[width] duration-500 dark:bg-amber-500/90"
              style={{ width: `${usedPct}%` }}
            />
            <div
              className="absolute inset-y-0 left-0 rounded-full bg-primary"
              style={{ width: `${gamePct}%` }}
            />
          </div>
          {/* 手柄：压在轨道上，指向"分配给游戏"的右端 */}
          <div
            className="pointer-events-none absolute top-1/2 size-4 -translate-x-1/2 -translate-y-1/2 rounded-full bg-primary shadow ring-4 ring-primary/25"
            style={{ left: `${gamePct}%` }}
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
