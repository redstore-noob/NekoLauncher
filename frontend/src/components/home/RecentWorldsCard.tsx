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
import type { world } from "../../../wailsjs/go/models";

import React, { useCallback, useEffect, useState } from "react";
import { Button, Spinner } from "@heroui/react";
import {
  ArrowClockwise20Regular as RefreshIcon,
  Globe20Regular,
  Play20Filled,
  Warning20Regular,
} from "@fluentui/react-icons";

import { GetRecentWorlds } from "../../../wailsjs/go/bindings/WorldAPI";
import {
  errorMessage,
  formatRelativeTimeFrom,
  localFileUrl,
} from "../../lib/home";
import { asArray } from "../../lib/guards";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 展示的存档条数（列表高度有限，取最近 3 个） */
const WORLD_LIMIT = 3;

interface RecentWorldsCardProps {
  /** 变化时重新扫描存档（主页在启动阶段变化后自增） */
  reloadKey: number;
  /** 启动流程进行中（准备/运行）：禁止再次启动 */
  isBusy: boolean;
  /** 当前选中的实例 Id，用于高亮所属存档 */
  selectedVersion: string;
  /** 仅切换实例（不启动） */
  onSelectVersion: (versionId: string) => void;
  /** 切换实例并启动到指定世界；返回错误信息（null = 已发起启动） */
  onLaunchWorld: (
    versionId: string,
    worldName: string,
  ) => Promise<string | null>;
}

/**
 * 最近世界卡片：WorldAPI.GetRecentWorlds 扫描全部实例的 saves，
 * 点条目切换所属实例，点 ▶ 直接启动到该世界（Quick Play）。
 */
const RecentWorldsCard: React.FC<RecentWorldsCardProps> = ({
  reloadKey,
  isBusy,
  selectedVersion,
  onSelectVersion,
  onLaunchWorld,
}) => {
  const [worlds, setWorlds] = useState<world.WorldInfo[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [launchError, setLaunchError] = useState("");
  const [launchingId, setLaunchingId] = useState("");

  const reload = useCallback(async () => {
    setIsLoading(true);
    setLoadError("");
    try {
      // `?? []` 只能拦 null，形状不对的非空数据（对象冒充数组）也会带走卡片
      setWorlds(asArray<world.WorldInfo>(await GetRecentWorlds(WORLD_LIMIT)));
    } catch (err) {
      console.error(err);
      setWorlds([]);
      setLoadError(errorMessage(err));
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload, reloadKey]);

  const handleLaunch = async (world: world.WorldInfo) => {
    setLaunchError("");
    setLaunchingId(world.OwnerVersionId);
    try {
      // 提取存档文件夹名称（DirectoryPath 的最后一段）
      const worldName = world.DirectoryPath.split(/[/\\]/).pop() || world.Name;
      const failed = await onLaunchWorld(world.OwnerVersionId, worldName);

      if (failed) setLaunchError(failed);
    } finally {
      setLaunchingId("");
    }
  };

  return (
    <HomeCard
      action={
        <Button
          isIconOnly
          className="h-7 w-7 min-w-7 text-gray-400"
          isDisabled={isLoading}
          radius="full"
          size="sm"
          title={t("重新扫描存档")}
          variant="light"
          onClick={() => void reload()}
        >
          <RefreshIcon />
        </Button>
      }
      icon={<Globe20Regular />}
      label={t("最近存档")}
      tileClass="from-emerald-400 via-teal-500 to-cyan-500 shadow-teal-500/30"
      value={
        isLoading
          ? t("扫描中…")
          : worlds.length > 0
            ? t("{0} 个世界", { "0": worlds.length })
            : t("暂无存档")
      }
      valueClass="bg-gradient-to-r from-emerald-500 via-teal-500 to-cyan-500"
    >
      {isLoading ? (
        <div className="flex h-10 items-center gap-2 rounded-lg bg-default-100/80 px-3 text-xs text-gray-500">
          <Spinner size="sm" />
        </div>
      ) : loadError ? (
        <div className="flex items-start gap-2 rounded-lg bg-danger/10 px-3 py-2.5 text-xs text-danger">
          <span className="mt-px flex-none">
            <Warning20Regular className="h-4 w-4" />
          </span>
          <span className="break-all">{loadError}</span>
        </div>
      ) : worlds.length === 0 ? (
        <div className="nya-enter nya-stagger-1 rounded-lg bg-black/5 px-4 py-3 text-center text-xs leading-relaxed text-gray-400 dark:bg-white/5">
          {t("暂无存档")}
        </div>
      ) : (
        <ul className="flex flex-col gap-1">
          {worlds.map((item, index) => {
            const isSelected = item.OwnerVersionId === selectedVersion;

            return (
              <li
                key={item.DirectoryPath || item.Name}
                className={`nya-enter nya-stagger-${index + 1}`}
              >
                <div
                  className={`
                    group flex w-full items-center gap-1 rounded-lg transition-all
                    ${isSelected ? "bg-primary/10" : "hover:bg-default-100/80"}
                  `}
                >
                  {/* 点条目：切换到该存档所属实例 */}
                  <button
                    className="flex min-w-0 flex-1 cursor-pointer items-center gap-2.5 rounded-lg px-2 py-1.5 text-left"
                    title={t("切换到实例 {0}", { "0": item.OwnerVersionId })}
                    type="button"
                    onClick={() => onSelectVersion(item.OwnerVersionId)}
                  >
                    {item.IconPath ? (
                      <img
                        alt=""
                        className="size-8 flex-none rounded-lg object-cover shadow-sm [image-rendering:pixelated]"
                        src={localFileUrl(item.IconPath)}
                      />
                    ) : (
                      <span className="flex size-8 flex-none items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-500 dark:text-emerald-300">
                        <Globe20Regular className="h-4 w-4" />
                      </span>
                    )}
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="overflow-hidden text-xs font-medium text-ellipsis whitespace-nowrap">
                        {item.Name}
                      </span>
                      <span className="overflow-hidden text-[10px] text-gray-400 text-ellipsis whitespace-nowrap">
                        {item.OwnerVersionId} ·{" "}
                        {formatRelativeTimeFrom(item.LastPlayed)}
                      </span>
                    </span>
                  </button>

                  {/* ▶：切换实例并直接启动到该存档（Quick Play） */}
                  <Button
                    isIconOnly
                    className="mr-1 h-7 w-7 min-w-7 flex-none text-emerald-600 opacity-60 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 dark:text-emerald-300"
                    isDisabled={isBusy || isLoading}
                    radius="full"
                    size="sm"
                    title={t("快速进入该存档（Minecraft 1.20+）")}
                    variant="light"
                    onClick={() => void handleLaunch(item)}
                  >
                    {launchingId === item.OwnerVersionId ? (
                      <Spinner size="sm" />
                    ) : (
                      <Play20Filled className="h-4 w-4" />
                    )}
                  </Button>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      {launchError ? (
        <div className="nya-enter flex items-start gap-2 rounded-lg bg-danger/10 px-3 py-2 text-[11px] text-danger">
          <span className="mt-px flex-none">
            <Warning20Regular className="h-4 w-4" />
          </span>
          <span className="break-all">{launchError}</span>
        </div>
      ) : null}
    </HomeCard>
  );
};

export default RecentWorldsCard;
