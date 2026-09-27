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

import React, { useEffect, useState } from "react";
import { Button, Progress } from "@heroui/react";
import {
  ArrowDownload20Regular,
  Dismiss20Regular,
  FolderOpen20Regular,
  Pause20Regular,
  Play20Regular,
} from "@fluentui/react-icons";

import {
  CancelDownload,
  GetCurrentDownloadSnapshot,
  IsDownloadPaused,
  PauseDownload,
  ResumeDownload,
} from "../../../wailsjs/go/bindings/DownloadAPI";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { navigateToPage } from "../../lib/navigation";
import { formatBytes } from "../../lib/home";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

// GameDownloadPhase：0 空闲 / 1 准备 / 2 下载 / 3 完成 / 4 失败 / 5 取消
const PHASE_PREPARING = 1;
const PHASE_DOWNLOADING = 2;
const PHASE_COMPLETED = 3;

/**
 * 下载任务卡片：与全局下载指示器（DownloadIndicator）共用同一份
 * download:progress / download:pauseChanged 事件流，把 Minecraft 安装
 * 进度以完整卡片形式放到主页，支持暂停/恢复/取消与跳转下载页。
 */
const DownloadCard: React.FC = () => {
  const [snap, setSnap] = useState<download.GameDownloadSnapshot | null>(null);
  const [paused, setPaused] = useState(false);

  useEffect(() => {
    GetCurrentDownloadSnapshot()
      .then((s) => {
        if (s) setSnap(s);
      })
      .catch(() => {
        /* 下载服务未初始化时忽略 */
      });
    IsDownloadPaused()
      .then((p) => setPaused(!!p))
      .catch(() => {
        /* 忽略 */
      });
    // 逐个退订：EventsOff 会连全局下载浮标 / 下载页的订阅一起清掉
    const offProgress = EventsOn(
      "download:progress",
      (s: download.GameDownloadSnapshot) => setSnap(s),
    );
    const offPaused = EventsOn("download:pauseChanged", (p: boolean) =>
      setPaused(!!p),
    );

    return () => {
      offProgress();
      offPaused();
    };
  }, []);

  const phase = snap?.Phase ?? 0;
  const active = phase === PHASE_PREPARING || phase === PHASE_DOWNLOADING;
  const percent = Math.max(0, Math.min(100, snap?.Percentage ?? 0));
  const versionId = snap?.VersionID || "Minecraft";

  const togglePause = async () => {
    try {
      if (paused) await ResumeDownload();
      else await PauseDownload();
    } catch {
      /* 状态变更失败不阻断界面 */
    }
  };

  const cancel = async () => {
    try {
      await CancelDownload();
    } catch {
      /* 任务可能已结束 */
    }
  };

  const value =
    !snap || phase === 0
      ? t("空闲")
      : active
        ? `${versionId} · ${phase === PHASE_PREPARING ? "准备中" : "下载中"}`
        : phase === PHASE_COMPLETED
          ? t("下载完成")
          : phase === 4
            ? t("下载失败")
            : t("已取消");

  return (
    <HomeCard
      icon={<ArrowDownload20Regular />}
      label={t("下载任务")}
      live={active && !paused}
      tileClass="from-sky-400 via-blue-500 to-indigo-500 shadow-blue-500/30"
      value={value}
      valueClass="bg-gradient-to-r from-sky-500 via-blue-500 to-indigo-500"
    >
      {active ? (
        <>
          <Progress
            aria-label={t("下载进度")}
            color={paused ? "warning" : "primary"}
            isIndeterminate={percent <= 0}
            size="sm"
            value={percent}
          />
          <ul className="flex flex-col gap-1.5 text-xs">
            {[
              { label: t("阶段"), detail: snap?.StageName || t("准备中") },
              { label: t("进度"), detail: `${percent.toFixed(1)}%` },
              {
                label: t("速度"),
                detail: `${formatBytes(snap?.BytesPerSecond)}/s`,
              },
              {
                label: t("文件"),
                detail:
                  snap && snap.TotalFiles > 0
                    ? `${snap.CompletedFiles}/${snap.TotalFiles}`
                    : "—",
              },
              {
                label: t("体积"),
                detail:
                  snap && snap.TotalBytes > 0
                    ? `${formatBytes(snap.CompletedBytes)} / ${formatBytes(snap.TotalBytes)}`
                    : snap?.Detail || "—",
              },
            ].map((row) => (
              <li
                key={row.label}
                className="flex items-baseline justify-between gap-2"
              >
                <span className="flex-none text-gray-400">{row.label}</span>
                <span className="overflow-hidden text-ellipsis whitespace-nowrap tabular-nums">
                  {row.detail}
                </span>
              </li>
            ))}
          </ul>
          <div className="flex items-center justify-end gap-2">
            <Button
              isIconOnly
              aria-label={paused ? t("继续下载") : t("暂停下载")}
              className="h-8 w-8 min-w-8"
              size="sm"
              title={paused ? t("继续下载") : t("暂停下载")}
              variant="flat"
              onPress={() => void togglePause()}
            >
              {paused ? <Play20Regular /> : <Pause20Regular />}
            </Button>
            <Button
              color="danger"
              size="sm"
              startContent={<Dismiss20Regular />}
              variant="flat"
              onPress={() => void cancel()}
            >
              {t("取消")}
            </Button>
          </div>
        </>
      ) : (
        <div className="flex flex-col items-center gap-3">
          <span className="text-center text-xs leading-relaxed text-gray-400">
            {phase === PHASE_COMPLETED
              ? t("{0} 已就绪", { "0": versionId })
              : phase === 4
                ? snap?.Detail || t("下载失败，可在下载页重试")
                : t("暂无下载任务")}
          </span>
          <Button
            color="primary"
            size="sm"
            startContent={<FolderOpen20Regular />}
            variant="flat"
            onPress={() => navigateToPage("download")}
          >
            {t("打开下载页")}
          </Button>
        </div>
      )}
    </HomeCard>
  );
};

export default DownloadCard;
