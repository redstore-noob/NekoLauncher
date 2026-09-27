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
import { Button } from "@heroui/react";
import { Folder20Regular, HardDrive20Regular } from "@fluentui/react-icons";

import { GetDiskUsage } from "../../../wailsjs/go/bindings/MonitorAPI";
import { EnsureDefaultMinecraftDirectory } from "../../../wailsjs/go/bindings/InstanceAPI";
import { GetGameDirectory } from "../../../wailsjs/go/bindings/ConfigAPI";
import { OpenPath } from "../../../wailsjs/go/bindings/SystemAPI";
import { errorMessage, formatBytes } from "../../lib/home";
import { asObject } from "../../lib/guards";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 磁盘采样间隔（ms）：gopsutil disk.Usage 很轻，30 秒足够 */
const DISK_POLL_INTERVAL_MS = 30000;

/** 剩余空间不足总量的该比例时进入警告色 */
const WARN_FREE_RATIO = 0.2;
/** 进入危险色（卡片变红提醒清理） */
const DANGER_FREE_RATIO = 0.08;

/**
 * 磁盘空间卡片：监控 Minecraft 游戏目录所在分区的容量，
 * 快满时数字变红提醒清理，避免下载/存档写到一半磁盘爆掉。
 */
const DiskCard: React.FC = () => {
  const [gameDir, setGameDir] = useState("");
  const [usage, setUsage] = useState<monitoring.DiskUsage | null>(null);
  const [error, setError] = useState("");

  // 游戏目录只解析一次（与主页版本列表同款兜底链）
  useEffect(() => {
    let alive = true;

    (async () => {
      try {
        let directory = await GetGameDirectory();

        if (!directory) directory = await EnsureDefaultMinecraftDirectory();
        if (alive && directory) setGameDir(directory);
      } catch {
        /* 目录解析失败保持空，不渲染监控 */
      }
    })();

    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (!gameDir) return;
    let alive = true;
    const load = async () => {
      try {
        const next = asObject(
          await GetDiskUsage(gameDir),
        ) as monitoring.DiskUsage | null;

        if (alive) {
          setUsage(next);
          setError("");
        }
      } catch (err) {
        if (alive) setError(errorMessage(err));
      }
    };

    void load();
    const timer = window.setInterval(() => void load(), DISK_POLL_INTERVAL_MS);

    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, [gameDir]);

  const freeRatio =
    usage && usage.TotalBytes > 0 ? usage.FreeBytes / usage.TotalBytes : 1;
  const danger = freeRatio < DANGER_FREE_RATIO;
  const warning = !danger && freeRatio < WARN_FREE_RATIO;
  const usedPercent = usage ? Math.max(2, usage.UsedPercent) : 0;

  const openDirectory = async () => {
    if (!gameDir) return;
    try {
      await OpenPath(gameDir);
    } catch {
      /* 打开失败静默 */
    }
  };

  return (
    <HomeCard
      action={
        <Button
          isIconOnly
          aria-label={t("打开游戏目录")}
          className="h-7 w-7 min-w-7 text-gray-400"
          isDisabled={!gameDir}
          radius="full"
          size="sm"
          title={t("打开游戏目录")}
          variant="light"
          onPress={() => void openDirectory()}
        >
          <Folder20Regular />
        </Button>
      }
      icon={<HardDrive20Regular />}
      label={t("磁盘空间")}
      tileClass="from-teal-400 via-cyan-500 to-sky-500 shadow-cyan-500/30"
      value={
        error
          ? t("读取失败")
          : usage
            ? t("剩余 {0}", { "0": formatBytes(usage.FreeBytes) })
            : t("读取中…")
      }
      valueClass="bg-gradient-to-r from-teal-500 via-cyan-500 to-sky-500"
    >
      {error ? (
        <div className="rounded-2xl bg-danger/10 px-4 py-3 text-xs leading-relaxed text-danger">
          {error}
        </div>
      ) : usage ? (
        <>
          {/* 已用进度条：接近占满时变色提醒 */}
          <div className="h-2 overflow-hidden rounded-full bg-black/10 dark:bg-white/10">
            <div
              className={`nya-bar h-full rounded-full transition-[width] duration-700 ease-out ${
                danger ? "bg-danger" : warning ? "bg-warning" : "bg-primary"
              }`}
              style={{ width: `${usedPercent}%` }}
            />
          </div>
          <div className="flex items-baseline justify-between gap-2 text-[11px] text-gray-400">
            <span className="tabular-nums">
              {t("已用")} {formatBytes(usage.TotalBytes - usage.FreeBytes)}
            </span>
            <span className="tabular-nums">
              {t("共")} {formatBytes(usage.TotalBytes)}
            </span>
          </div>
          {(danger || warning) && (
            <div
              className={`rounded-xl px-3 py-2 text-center text-[11px] leading-relaxed ${
                danger
                  ? "bg-danger/10 text-danger"
                  : "bg-warning/10 text-warning-600 dark:text-warning-500"
              }`}
            >
              {danger
                ? t("空间即将耗尽，游戏可能无法写入存档，请尽快清理")
                : t("剩余空间偏少，建议留意下载与存档体积")}
            </div>
          )}
        </>
      ) : (
        <div className="rounded-2xl bg-black/5 px-4 py-3 text-center text-xs text-gray-400 dark:bg-white/5">
          {t("等待游戏目录就绪…")}
        </div>
      )}
      {gameDir ? (
        <div className="truncate text-[11px] text-gray-400" title={gameDir}>
          {gameDir}
        </div>
      ) : null}
    </HomeCard>
  );
};

export default DiskCard;
