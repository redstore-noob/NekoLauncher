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
import { Button } from "@heroui/react";
import {
  ArrowDownload20Regular,
  FolderOpen20Regular,
} from "@fluentui/react-icons";

import { GetCurrentDownloadSnapshot } from "../../../wailsjs/go/bindings/DownloadAPI";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { navigateToPage } from "../../lib/navigation";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

// GameDownloadPhase：0 空闲 / 1 准备 / 2 下载 / 3 完成 / 4 失败 / 5 取消
const PHASE_PREPARING = 1;
const PHASE_DOWNLOADING = 2;
const PHASE_COMPLETED = 3;

/**
 * 下载任务卡片（快捷入口）：只显示轻量状态——
 * 下载进度、暂停/取消等一律收敛到右下角下载中心，这里不再重复渲染。
 * 有任务进行中时显示"下载中…点击查看"，点击直达下载页（进度看右下角下载中心）。
 */
const DownloadCard: React.FC = () => {
  const [snap, setSnap] = useState<download.GameDownloadSnapshot | null>(null);

  useEffect(() => {
    GetCurrentDownloadSnapshot()
      .then((s) => {
        if (s) setSnap(s);
      })
      .catch(() => {
        /* 下载服务未初始化时忽略 */
      });
    // 逐个退订：EventsOff 会连全局下载中心 / 下载页的订阅一起清掉
    const offProgress = EventsOn(
      "download:progress",
      (s: download.GameDownloadSnapshot) => setSnap(s),
    );

    return () => {
      offProgress();
    };
  }, []);

  const phase = snap?.Phase ?? 0;
  const active = phase === PHASE_PREPARING || phase === PHASE_DOWNLOADING;
  const versionId = snap?.VersionID || "Minecraft";

  const value =
    !snap || phase === 0
      ? t("空闲")
      : active
        ? t("下载中，点击查看")
        : phase === PHASE_COMPLETED
          ? t("下载完成")
          : phase === 4
            ? t("下载失败")
            : t("已取消");

  return (
    <HomeCard
      icon={<ArrowDownload20Regular />}
      label={t("下载任务")}
      live={active}
      tileClass="from-sky-400 via-blue-500 to-indigo-500 shadow-blue-500/30"
      value={value}
      valueClass="bg-gradient-to-r from-sky-500 via-blue-500 to-indigo-500"
    >
      {active ? (
        <div className="flex flex-col items-center gap-3">
          <span className="text-center text-xs leading-relaxed text-gray-400">
            {t("{0} 正在下载，进度见右下角的下载中心", { "0": versionId })}
          </span>
          <Button
            color="primary"
            size="sm"
            startContent={<ArrowDownload20Regular />}
            variant="flat"
            onPress={() => navigateToPage("download")}
          >
            {t("查看进度")}
          </Button>
        </div>
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
