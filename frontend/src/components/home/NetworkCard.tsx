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
import type { download } from "../../../wailsjs/go/models";

import React, { useCallback, useEffect, useState } from "react";
import { Button, Chip } from "@heroui/react";
import {
  ArrowClockwise20Regular as RefreshIcon,
  Wifi120Regular,
} from "@fluentui/react-icons";

import {
  GetActiveDownloadSourceName,
  GetSourceLatencies,
} from "../../../wailsjs/go/bindings/DownloadAPI";
import { asArray } from "../../lib/guards";
import { navigateToPage } from "../../lib/navigation";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 自动重测间隔（ms）：HEAD 一次版本清单不重，1 分钟一次不吵 */
const NETWORK_POLL_INTERVAL_MS = 60000;

/** 延迟分档 → 徽章配色（超时单独灰色） */
function latencyChip(ms: number): { label: string; className: string } {
  if (ms < 0) {
    return {
      className: "bg-default-100 text-gray-400",
      label: t("超时"),
    };
  }
  if (ms < 150) {
    return {
      className: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-300",
      label: `${ms} ms`,
    };
  }
  if (ms < 500) {
    return {
      className: "bg-amber-500/10 text-amber-600 dark:text-amber-300",
      label: `${ms} ms`,
    };
  }

  return {
    className: "bg-danger/10 text-danger",
    label: `${ms} ms`,
  };
}

/**
 * 网络状态卡片：并发测量官方与 BMCL 镜像的版本清单延迟，
 * 标出当前使用的下载源，帮助判断「要不要换镜像」。
 */
const NetworkCard: React.FC = () => {
  const [rows, setRows] = useState<download.SourceLatency[]>([]);
  const [activeName, setActiveName] = useState("");
  const [isLoading, setIsLoading] = useState(true);

  const reload = useCallback(async () => {
    setIsLoading(true);
    try {
      const [latencies, current] = await Promise.all([
        GetSourceLatencies(),
        GetActiveDownloadSourceName(),
      ]);

      setRows(asArray(latencies));
      setActiveName(current || "");
    } catch (err) {
      console.error(t("下载源测速失败"), err);
      setRows([]);
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  // 定时重测：网络状况会变，卡片常驻主页需要保鲜
  useEffect(() => {
    const timer = window.setInterval(
      () => void reload(),
      NETWORK_POLL_INTERVAL_MS,
    );

    return () => window.clearInterval(timer);
  }, [reload]);

  const activeRow = rows.find((row) => row.Name === activeName);
  const reachableCount = rows.filter((row) => row.Available).length;
  const value = isLoading
    ? t("测速中…")
    : activeRow
      ? `${activeRow.Name}${activeRow.Available ? ` · ${activeRow.LatencyMs} ms` : " · 不可达"}`
      : activeName || t("未知源");

  return (
    <HomeCard
      action={
        <Button
          isIconOnly
          aria-label={t("重新测速")}
          className="h-7 w-7 min-w-7 text-gray-400"
          isDisabled={isLoading}
          radius="full"
          size="sm"
          title={t("重新测速")}
          variant="light"
          onPress={() => void reload()}
        >
          <RefreshIcon />
        </Button>
      }
      icon={<Wifi120Regular />}
      label={t("网络状态")}
      tileClass="from-indigo-400 via-blue-500 to-cyan-500 shadow-indigo-500/30"
      value={value}
      valueClass="bg-gradient-to-r from-indigo-500 via-blue-500 to-cyan-500"
    >
      <ul className="flex flex-col gap-2">
        {rows.map((row, index) => {
          const chip = latencyChip(row.LatencyMs);

          return (
            <li
              key={row.Name}
              className={`nya-enter nya-stagger-${index + 1} flex items-center gap-2`}
            >
              <span className="flex min-w-0 flex-1 items-center gap-2">
                <span className="overflow-hidden text-xs font-medium text-ellipsis whitespace-nowrap">
                  {row.Name}
                </span>
                {row.Name === activeName ? (
                  <span className="flex-none rounded-full bg-primary/15 px-2 py-0.5 text-[10px] font-medium text-primary">
                    {t("当前")}
                  </span>
                ) : null}
              </span>
              <Chip className={chip.className} size="sm" variant="flat">
                {chip.label}
              </Chip>
            </li>
          );
        })}
        {rows.length === 0 ? (
          <li className="rounded-lg bg-black/5 px-4 py-3 text-center text-xs leading-relaxed text-gray-400 dark:bg-white/5">
            {isLoading ? t("正在测量下载源延迟…") : t("没有可探测的下载源")}
          </li>
        ) : null}
      </ul>
      {rows.length > 0 ? (
        <button
          className="cursor-pointer rounded-full bg-black/5 px-3 py-1 text-center text-[11px] text-gray-400 transition-colors hover:bg-black/10 dark:bg-white/10 dark:hover:bg-white/20"
          type="button"
          onClick={() => navigateToPage("settings", "download")}
        >
          {reachableCount}/{rows.length} {t("个源可用")}
        </button>
      ) : null}
    </HomeCard>
  );
};

export default NetworkCard;
