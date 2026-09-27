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
import type { instance } from "../../../wailsjs/go/models";

import React, { useCallback, useEffect, useState } from "react";
import { Button } from "@heroui/react";
import {
  ArrowClockwise20Regular as RefreshIcon,
  Camera20Regular,
  Image20Regular,
} from "@fluentui/react-icons";

import { ListScreenshots } from "../../../wailsjs/go/bindings/SystemAPI";
import {
  errorMessage,
  formatRelativeTimeFrom,
  localFileUrl,
} from "../../lib/home";
import { asArray } from "../../lib/guards";
import ImageViewerModal from "../ImageViewerModal";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 展示的截图数量（3 列 × 3 行） */
const SCREENSHOT_LIMIT = 9;

/**
 * 截图墙卡片：展示全部实例最近的 screenshots（按修改时间倒序），
 * 图片经 /localfile 中转加载；点击打开应用内图片查看器
 * （缩放/平移/另存为）。
 */
const ScreenshotWallCard: React.FC = () => {
  const [shots, setShots] = useState<instance.ScreenshotInfo[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState("");
  /** 当前在查看器里打开的截图（null = 关闭） */
  const [viewing, setViewing] = useState<instance.ScreenshotInfo | null>(null);

  const reload = useCallback(async () => {
    setIsLoading(true);
    setError("");
    try {
      setShots(
        asArray<instance.ScreenshotInfo>(
          await ListScreenshots(SCREENSHOT_LIMIT),
        ),
      );
    } catch (err) {
      console.error(err);
      setShots([]);
      setError(errorMessage(err));
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return (
    <HomeCard
      action={
        <Button
          isIconOnly
          aria-label={t("重新扫描截图")}
          className="h-7 w-7 min-w-7 text-gray-400"
          isDisabled={isLoading}
          radius="full"
          size="sm"
          title={t("重新扫描截图")}
          variant="light"
          onPress={() => void reload()}
        >
          <RefreshIcon />
        </Button>
      }
      icon={<Image20Regular />}
      label={t("截图墙")}
      tileClass="from-purple-400 via-violet-500 to-indigo-500 shadow-violet-500/30"
      value={
        isLoading
          ? t("扫描中…")
          : shots.length > 0
            ? t("{0} 张截图", { "0": shots.length })
            : t("暂无截图")
      }
      valueClass="bg-gradient-to-r from-purple-500 via-violet-500 to-indigo-500"
    >
      {error ? (
        <div className="rounded-2xl bg-danger/10 px-4 py-3 text-xs leading-relaxed text-danger">
          {error}
        </div>
      ) : shots.length > 0 ? (
        <div className="grid grid-cols-3 gap-1.5">
          {shots.map((shot, index) => (
            <button
              key={shot.Path}
              className={`nya-enter nya-stagger-${Math.min(index + 1, 9)} group relative aspect-square cursor-pointer overflow-hidden rounded-xl bg-black/5 dark:bg-white/10`}
              title={t("{0} · {1}", {
                "0": shot.Name,
                "1": formatRelativeTimeFrom(shot.ModifiedAt),
              })}
              type="button"
              onClick={() => setViewing(shot)}
            >
              <img
                alt={shot.Name}
                className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
                loading="lazy"
                src={localFileUrl(shot.Path)}
              />
            </button>
          ))}
        </div>
      ) : (
        <div className="flex flex-col items-center gap-2 rounded-2xl bg-black/5 px-4 py-4 text-center text-xs leading-relaxed text-gray-400 dark:bg-white/5">
          <span className="flex size-10 items-center justify-center rounded-xl bg-black/5 text-gray-400 dark:bg-white/10">
            <Camera20Regular className="h-5 w-5" />
          </span>
          {isLoading ? t("正在扫描实例的截图…") : t("游戏里按 F2 截图")}
        </div>
      )}

      {/* 应用内图片查看器：缩放 / 平移 / 另存为 */}
      <ImageViewerModal
        fileName={viewing?.Name ?? ""}
        isOpen={viewing !== null}
        sourcePath={viewing?.Path}
        src={viewing ? localFileUrl(viewing.Path) : ""}
        onClose={() => setViewing(null)}
      />
    </HomeCard>
  );
};

export default ScreenshotWallCard;
