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
import type { config } from "../../../wailsjs/go/models";

import React from "react";
import { Timer20Regular } from "@fluentui/react-icons";

import { formatPlaytime, formatRelativeTime } from "../../lib/home";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 游玩统计卡片：总时长 + Top 实例排行 */
const PlaytimeCard: React.FC<{
  records: config.PlaytimeRecord[];
  isGameRunning: boolean;
}> = ({ records, isGameRunning }) => {
  const totalSeconds = records.reduce((sum, r) => sum + r.PlaytimeSeconds, 0);
  const top = records.slice(0, 3);
  const maxSeconds =
    top.length > 0 ? Math.max(...top.map((r) => r.PlaytimeSeconds), 1) : 1;

  return (
    <HomeCard
      icon={<Timer20Regular />}
      label={t("游玩统计")}
      live={isGameRunning}
      tileClass="from-cyan-400 via-blue-500 to-purple-500 shadow-cyan-500/30"
      value={formatPlaytime(totalSeconds)}
      valueClass="bg-gradient-to-r from-cyan-500 via-blue-500 to-purple-500"
    >
      {/* Top 实例排行 */}
      {top.length > 0 ? (
        <ul className="flex flex-col gap-2.5">
          {top.map((record, index) => (
            <li
              key={`${record.MinecraftDirectory}/${record.VersionId}`}
              className={`nya-enter nya-stagger-${index + 1} flex flex-col gap-1.5`}
            >
              <div className="flex items-baseline justify-between gap-2">
                <span className="overflow-hidden text-xs font-medium text-ellipsis whitespace-nowrap">
                  {record.VersionId}
                </span>
                <span className="flex-none text-[11px] text-gray-400 tabular-nums">
                  {formatRelativeTime(record.LastPlayedAt)}
                </span>
              </div>
              {/* 时长条 + 时长文本 */}
              <div className="flex items-center gap-2">
                <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-black/10 dark:bg-white/10">
                  <div
                    className="nya-bar h-full rounded-full transition-[width] duration-700 ease-out"
                    style={{
                      width: `${Math.max(6, (record.PlaytimeSeconds / maxSeconds) * 100)}%`,
                    }}
                  />
                </div>
                <span className="flex-none text-[11px] font-semibold text-cyan-600 tabular-nums dark:text-cyan-300">
                  {formatPlaytime(record.PlaytimeSeconds)}
                </span>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <div className="nya-enter nya-stagger-1 rounded-2xl bg-black/5 px-4 py-3 text-center text-xs leading-relaxed text-gray-400 dark:bg-white/5">
          {t("暂无记录")}
        </div>
      )}

      {/* 游戏运行中提示 */}
      {isGameRunning ? (
        <div className="nya-enter nya-stagger-2 flex items-center justify-center gap-1.5 text-[11px] text-cyan-600 dark:text-cyan-300">
          <span className="relative flex size-2">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-cyan-400 opacity-60" />
            <span className="relative inline-flex size-2 rounded-full bg-cyan-500" />
          </span>

          {t("正在记录本次游玩…")}
        </div>
      ) : null}
    </HomeCard>
  );
};

export default PlaytimeCard;
