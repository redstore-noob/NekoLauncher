/*
 * 性能监控卡片：任务管理器同款的 CPU / GPU / 内存占用折线图，三条线合一。
 * 数据来自 MonitorAPI.GetSystemUsage（CPU/内存走 gopsutil，GPU 走 Windows
 * GPU 引擎性能计数器，不可用时 GPU 线隐藏、图例显示「不可用」）。
 * 每秒采样一次，保留最近 60 秒；曲线用 SVG polyline 手绘，无图表库依赖。
 */
import type { monitoring } from "../../../wailsjs/go/models";

import React, { useEffect, useState } from "react";
import { TopSpeed20Regular } from "@fluentui/react-icons";

import { GetSystemUsage } from "../../../wailsjs/go/bindings/MonitorAPI";
import { asObject } from "../../lib/guards";
import { isLinuxPlatform } from "../../lib/platform";
import { startVisiblePoll } from "../../lib/visibility";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/**
 * 采样间隔（ms），与 gopsutil CPU 差值采样节奏匹配。Linux 后端要遍历 /proc、
 * IPC 与卡片重绘（毛玻璃表面）都比 Windows 贵，放宽到 3 秒。
 */
const POLL_INTERVAL_MS = isLinuxPlatform() ? 3000 : 1000;
/** 曲线窗口：60 个采样点（Windows=60 秒，Linux=3 分钟） */
const WINDOW_SIZE = 60;

/** 三条线的配色（任务管理器风格：CPU 蓝 / GPU 绿 / 内存紫） */
const SERIES = [
  { key: "cpu", label: "CPU", color: "#4cc2ff" },
  { key: "gpu", label: "GPU", color: "#4ade80" },
  { key: "mem", label: t("内存"), color: "#b29dff" },
] as const;

type SeriesKey = (typeof SERIES)[number]["key"];

interface UsageSample {
  cpu: number;
  gpu: number;
  mem: number;
  /** 已用 / 总物理内存（GB），仅用于底部文字 */
  memUsedGb: number;
  memTotalGb: number;
}

/** SVG 画布逻辑尺寸；preserveAspectRatio="none" 拉伸到容器，线条用 non-scaling-stroke */
const CHART_W = 320;
const CHART_H = 110;

/** 占用值夹到 0~100；-1（GPU 不可用）保持原样由调用方过滤 */
function clamp(value: number): number {
  if (value < 0) return 0;
  if (value > 100) return 100;

  return value;
}

/**
 * 把一条序列的采样点转成 polyline 的 points 串。
 * 曲线右端锚定在图表右缘、随采样数向左生长（任务管理器同款）；
 * GPU 不可用（-1）的采样会产生断线（拆成多段连续段）。
 */
function polylinePoints(history: UsageSample[], key: SeriesKey): string[] {
  const step = CHART_W / (WINDOW_SIZE - 1);
  const segments: string[][] = [[]];

  history.forEach((sample, index) => {
    const value = sample[key];

    if (value < 0) {
      segments.push([]); // 断点：开启新段

      return;
    }
    const x = CHART_W - (history.length - 1 - index) * step;
    const y = CHART_H - (clamp(value) / 100) * CHART_H;

    segments[segments.length - 1].push(`${x.toFixed(1)},${y.toFixed(1)}`);
  });

  // 单点段画不出线，直接丢弃
  return segments
    .filter((segment) => segment.length >= 2)
    .map((s) => s.join(" "));
}

const PerformanceCard: React.FC = () => {
  const [history, setHistory] = useState<UsageSample[]>([]);
  const [gpuAvailable, setGpuAvailable] = useState(true);

  useEffect(() => {
    let alive = true;
    const sample = async () => {
      try {
        const usage = asObject(
          await GetSystemUsage(),
        ) as monitoring.SystemUsage | null;

        if (!alive || !usage) return;
        setGpuAvailable(usage.GpuPercent >= 0);
        setHistory((prev) => {
          const next = [
            ...prev,
            {
              cpu: usage.CpuPercent,
              gpu: usage.GpuPercent,
              mem: usage.MemoryPercent,
              memUsedGb: usage.MemoryUsedGb,
              memTotalGb: usage.MemoryTotalGb,
            },
          ];

          return next.length > WINDOW_SIZE ? next.slice(-WINDOW_SIZE) : next;
        });
      } catch {
        /* 采样失败跳过这一秒 */
      }
    };

    void sample();
    const stop = startVisiblePoll(() => void sample(), POLL_INTERVAL_MS);

    return () => {
      alive = false;
      stop();
    };
  }, []);

  const latest = history.length > 0 ? history[history.length - 1] : null;
  const currentValues: Record<SeriesKey, number | null> = {
    cpu: latest ? clamp(latest.cpu) : null,
    gpu: latest ? (latest.gpu >= 0 ? clamp(latest.gpu) : null) : null,
    mem: latest ? clamp(latest.mem) : null,
  };

  return (
    <HomeCard
      icon={<TopSpeed20Regular />}
      label={t("性能监控")}
      value={latest ? `${Math.round(currentValues.cpu ?? 0)}%` : t("采样中…")}
    >
      {/* 图例：彩色圆点 + 当前值；GPU 不可用时单独标注 */}
      <div className="flex items-center gap-x-3 gap-y-1">
        {SERIES.map((series) => (
          <span
            key={series.key}
            className="flex items-center gap-1.5 text-[11px] text-gray-400"
          >
            <span
              className="size-2 flex-none rounded-full"
              style={{ backgroundColor: series.color }}
            />
            {series.label}
            <span className="text-gray-900 tabular-nums dark:text-gray-100">
              {currentValues[series.key] === null
                ? series.key === "gpu" && !gpuAvailable
                  ? t("不可用")
                  : "--"
                : `${Math.round(currentValues[series.key]!)}%`}
            </span>
          </span>
        ))}
      </div>

      {/* 折线图：纵轴 0~100%，横向网格每 25% 一条，0 与 100 用实线收口 */}
      <div className="relative">
        <div className="pointer-events-none absolute inset-0">
          {[0, 25, 50, 75, 100].map((tick) => (
            <div
              key={tick}
              className="absolute inset-x-0 flex items-center"
              style={{ bottom: `${tick}%` }}
            >
              <span className="w-7 flex-none text-right text-[9px] leading-none text-gray-300 tabular-nums dark:text-gray-600">
                {tick}
              </span>
              <span
                className={`
                  h-px flex-1
                  ${tick === 0 ? "bg-black/20 dark:bg-white/20" : "bg-black/10 dark:bg-white/10"}
                `}
              />
            </div>
          ))}
        </div>
        {/* 左侧留出刻度文字宽度 */}
        <div className="pl-8">
          <svg
            aria-label={t("CPU、GPU 与内存占用折线图")}
            className="block h-[110px] w-full overflow-visible"
            preserveAspectRatio="none"
            viewBox={`0 0 ${CHART_W} ${CHART_H}`}
          >
            {SERIES.map((series) =>
              polylinePoints(history, series.key).map(
                (points, segmentIndex) => (
                  <polyline
                    key={`${series.key}-${segmentIndex}`}
                    fill="none"
                    points={points}
                    stroke={series.color}
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={1.6}
                    vectorEffect="non-scaling-stroke"
                  />
                ),
              ),
            )}
          </svg>
        </div>
      </div>

      <div className="flex items-center justify-between text-[10px] text-gray-400">
        <span className="tabular-nums">
          {latest && latest.memTotalGb > 0
            ? t("内存 {0} / {1} GB", {
                "0": latest.memUsedGb.toFixed(1),
                "1": latest.memTotalGb.toFixed(1),
              })
            : " "}
        </span>
        <span>
          {t(
            isLinuxPlatform()
              ? "每 3 秒采样 · 最近 3 分钟"
              : "每秒采样 · 最近 60 秒",
          )}
        </span>
      </div>
    </HomeCard>
  );
};

export default PerformanceCard;
