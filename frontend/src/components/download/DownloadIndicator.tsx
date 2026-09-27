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
/*
 * 全局下载进度条：订阅 download:progress / download:pauseChanged，
 * 让任意页面都能看到 Minecraft 安装/下载进度。活动任务显示进度与暂停/取消，
 * 终态显示结果与「打开下载页」，关闭后在下一次任务前不再出现。
 */
import type { download } from "../../../wailsjs/go/models";

import React, { useEffect, useState } from "react";
import { Button, Progress } from "@heroui/react";
import {
  Dismiss20Regular,
  FolderOpen20Regular,
  Pause20Regular,
  Play20Regular,
} from "@fluentui/react-icons";
import { AnimatePresence, motion } from "framer-motion";

import { indicatorVariants } from "../../lib/motion";
import {
  CancelDownload,
  GetCurrentDownloadSnapshot,
  IsDownloadPaused,
  PauseDownload,
  ResumeDownload,
} from "../../../wailsjs/go/bindings/DownloadAPI";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { t } from "../../i18n";

// GameDownloadPhase：0 空闲 / 1 准备 / 2 下载 / 3 完成 / 4 失败 / 5 取消
const PHASE_PREPARING = 1;
const PHASE_DOWNLOADING = 2;
const PHASE_COMPLETED = 3;

function formatBytes(n?: number | null): string {
  if (!n) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let i = 0;
  let v = n;

  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }

  return `${v.toFixed(1)} ${units[i]}`;
}

interface DownloadIndicatorProps {
  /** 点击「打开下载页」时切换页面 */
  onOpenDownloads?: () => void;
}

const DownloadIndicator: React.FC<DownloadIndicatorProps> = ({
  onOpenDownloads,
}) => {
  const [snap, setSnap] = useState<download.GameDownloadSnapshot | null>(null);
  const [paused, setPaused] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    GetCurrentDownloadSnapshot()
      .then((s) => {
        if (s) setSnap(s);
      })
      .catch(() => {
        /* 忽略 */
      });
    IsDownloadPaused()
      .then((p) => setPaused(!!p))
      .catch(() => {
        /* 忽略 */
      });
    // 只退订自己注册的回调：EventsOff(事件名) 会清掉该事件的全部监听，
    // 把主页下载卡片 / 下载页的订阅一起干掉。
    const offProgress = EventsOn(
      "download:progress",
      (s: download.GameDownloadSnapshot) => {
        setSnap(s);
        setDismissed(false);
      },
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
  const terminal = phase >= PHASE_COMPLETED;

  const percent = Math.max(0, Math.min(100, snap?.Percentage ?? 0));
  const detail = [
    snap?.StageName,
    snap?.Detail,
    active
      ? `${formatBytes(snap?.CompletedBytes)}/${formatBytes(snap?.TotalBytes)} · ${formatBytes(snap?.BytesPerSecond)}/s`
      : "",
  ]
    .filter(Boolean)
    .join(" · ");

  const togglePause = async () => {
    try {
      if (paused) await ResumeDownload();
      else await PauseDownload();
    } catch {
      /* 状态变更失败不阻断界面 */
    }
  };

  return (
    <AnimatePresence>
      {snap && !dismissed && (active || terminal) ? (
        <motion.div
          animate="center"
          className="fixed right-4 bottom-4 z-40 w-[320px] max-w-[calc(100vw-2rem)] rounded-2xl border nya-border nya-panel-strong shadow-lg backdrop-blur-md"
          exit="exit"
          initial="enter"
          variants={indicatorVariants}
        >
          <div className="flex items-start gap-2 px-4 pt-3 pb-2">
            <button
              className="min-w-0 flex-1 cursor-pointer text-left"
              type="button"
              onClick={onOpenDownloads}
            >
              <span className="block truncate text-[13px] font-semibold text-gray-800 dark:text-gray-200">
                {snap.VersionID || "Minecraft"}
                {active
                  ? phase === PHASE_PREPARING
                    ? t(" · 准备中")
                    : t(" · 下载中")
                  : ""}
              </span>
              <span className="block truncate text-[11px] text-gray-400">
                {detail}
              </span>
            </button>
            <button
              aria-label={t("关闭")}
              className="flex size-6 flex-none items-center justify-center rounded-md text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800 dark:hover:text-gray-200"
              title={t("关闭")}
              type="button"
              onClick={() => setDismissed(true)}
            >
              <Dismiss20Regular />
            </button>
          </div>

          {active && (
            <div className="px-4 pb-2">
              <Progress
                aria-label={t("下载进度")}
                color={paused ? "warning" : "primary"}
                isIndeterminate={percent <= 0}
                size="sm"
                value={percent}
              />
            </div>
          )}

          <div className="flex items-center justify-end gap-2 px-4 pb-3">
            {active ? (
              <>
                <Button
                  isIconOnly
                  aria-label={paused ? t("继续下载") : t("暂停下载")}
                  size="sm"
                  variant="flat"
                  onPress={() => void togglePause()}
                >
                  {paused ? <Play20Regular /> : <Pause20Regular />}
                </Button>
                <Button
                  color="danger"
                  size="sm"
                  variant="flat"
                  onPress={() => void CancelDownload()}
                >
                  {t("取消")}
                </Button>
              </>
            ) : (
              <>
                {phase === PHASE_COMPLETED && (
                  <Button
                    color="success"
                    size="sm"
                    startContent={<FolderOpen20Regular />}
                    variant="flat"
                    onPress={onOpenDownloads}
                  >
                    {t("打开下载页")}
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="light"
                  onPress={() => setDismissed(true)}
                >
                  {t("关闭")}
                </Button>
              </>
            )}
          </div>
        </motion.div>
      ) : null}
    </AnimatePresence>
  );
};

export default DownloadIndicator;
