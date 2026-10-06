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
import type { monitoring } from "../../../wailsjs/go/models";

import React, { useEffect, useState } from "react";
import { Pulse20Regular } from "@fluentui/react-icons";

import { GetMemorySnapshot } from "../../../wailsjs/go/bindings/MonitorAPI";
import { formatMemoryMb } from "../../lib/home";
import { asObject } from "../../lib/guards";
import { isLinuxPlatform } from "../../lib/platform";
import { startVisiblePoll } from "../../lib/visibility";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/**
 * 内存采样间隔（ms）。Linux 后端要遍历全部进程读 /proc，开销随进程数增长，
 * 放宽到 6 秒（Windows 保持 3 秒）。
 */
const MEMORY_POLL_INTERVAL_MS = isLinuxPlatform() ? 6000 : 3000;

/**
 * 内存监控卡片：MonitorAPI.GetMemorySnapshot 定时采样启动器与
 * 全部 java/javaw 进程的工作集，按固定间隔轮询刷新（Windows 3 秒 / Linux 6 秒）。
 */
const MemoryCard: React.FC = () => {
  const [snapshot, setSnapshot] = useState<monitoring.MemorySnapshot | null>(
    null,
  );

  useEffect(() => {
    let alive = true;
    const load = async () => {
      try {
        const next = asObject(
          await GetMemorySnapshot(),
        ) as monitoring.MemorySnapshot;

        if (alive) setSnapshot(next);
      } catch {
        /* 采集失败保留上一次快照 */
      }
    };

    void load();
    const stop = startVisiblePoll(() => void load(), MEMORY_POLL_INTERVAL_MS);

    return () => {
      alive = false;
      stop();
    };
  }, []);

  const launcherMb = snapshot?.LauncherMemoryMb ?? 0;
  const jvmMb = snapshot?.JvmMemoryMb ?? 0;
  const javaCount = snapshot?.JavaProcessCount ?? 0;
  const totalMb = launcherMb + jvmMb;
  // 进度条按两者中较大者归一，避免两边都顶满
  const scaleMb = Math.max(launcherMb, jvmMb, 1);

  return (
    <HomeCard
      icon={<Pulse20Regular />}
      label={t("内存监控")}
      live={javaCount > 0}
      tileClass="from-fuchsia-400 via-pink-500 to-rose-500 shadow-pink-500/30"
      value={snapshot ? formatMemoryMb(totalMb) : t("读取中…")}
      valueClass="bg-gradient-to-r from-fuchsia-500 via-pink-500 to-rose-500"
    >
      <ul className="flex flex-col gap-2.5">
        {[
          { label: t("启动器"), megabytes: launcherMb },
          { label: "JVM", megabytes: jvmMb },
        ].map((row, index) => (
          <li
            key={row.label}
            className={`nya-enter nya-stagger-${index + 1} flex flex-col gap-1.5`}
          >
            <div className="flex items-baseline justify-between gap-2">
              <span className="text-xs font-medium">{row.label}</span>
              <span className="flex-none text-[11px] text-gray-400 tabular-nums">
                {formatMemoryMb(row.megabytes)}
              </span>
            </div>
            <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-black/10 dark:bg-white/10">
              <div
                className="nya-bar h-full rounded-full transition-[width] duration-700 ease-out"
                style={{
                  width: `${Math.max(4, (row.megabytes / scaleMb) * 100)}%`,
                }}
              />
            </div>
          </li>
        ))}
      </ul>

      <div className="flex items-center justify-center">
        <span
          className={`
            rounded-full px-2 py-0.5 text-[11px] font-medium
            ${
              javaCount > 0
                ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-300"
                : "bg-black/5 text-gray-400 dark:bg-white/10"
            }
          `}
        >
          {javaCount > 0
            ? t("{0} 个 Java 进程", { "0": javaCount })
            : t("没有 Java 进程在运行")}
        </span>
      </div>
    </HomeCard>
  );
};

export default MemoryCard;
