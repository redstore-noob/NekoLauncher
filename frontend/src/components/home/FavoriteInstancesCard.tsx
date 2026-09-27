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
import React, { useCallback, useEffect, useState } from "react";
import { Button } from "@heroui/react";
import {
  Add20Regular,
  Dismiss20Regular,
  Play20Filled,
  Star20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";

import {
  EnsureDefaultMinecraftDirectory,
  GetInstalledVersionIds,
} from "../../../wailsjs/go/bindings/InstanceAPI";
import {
  GetGameDirectory,
  GetValue,
  SetValue,
} from "../../../wailsjs/go/bindings/ConfigAPI";
import { asArray } from "../../lib/guards";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 收藏列表的 launcher.yaml 键（JSON 字符串数组，元素为实例 Id） */
const FAVORITE_KEY = "homeFavoriteInstances";
/** 卡片高度有限，最多展示的收藏数 */
const FAVORITE_LIMIT = 6;

interface FavoriteInstancesCardProps {
  /** 启动流程进行中（准备/运行）：禁止再次启动 */
  isBusy: boolean;
  /** 主页当前选中的实例（点 + 收藏它） */
  selectedVersion: string;
  /** 切换实例并启动；返回错误信息（null = 已发起启动） */
  onLaunchVersion: (versionId: string) => Promise<string | null>;
}

/** 解析收藏配置；非字符串数组一律回落空数组 */
function parseFavorites(raw: string | null): string[] {
  if (!raw) return [];
  try {
    const parsed: unknown = JSON.parse(raw);

    if (!Array.isArray(parsed)) return [];

    return parsed.filter((id): id is string => typeof id === "string");
  } catch {
    return [];
  }
}

/**
 * 常用实例卡片：手动收藏的实例快捷启动，与「最近存档」（按时间）互补。
 * 收藏持久化在 launcher.yaml；列表只显示已安装的实例，卸载的自动隐藏。
 */
const FavoriteInstancesCard: React.FC<FavoriteInstancesCardProps> = ({
  isBusy,
  selectedVersion,
  onLaunchVersion,
}) => {
  const [favorites, setFavorites] = useState<string[]>([]);
  const [installed, setInstalled] = useState<string[]>([]);
  const [launchingId, setLaunchingId] = useState("");
  const [launchError, setLaunchError] = useState("");

  const reload = useCallback(async () => {
    try {
      const directory =
        (await GetGameDirectory().catch(() => "")) ||
        (await EnsureDefaultMinecraftDirectory().catch(() => ""));

      setInstalled(
        directory
          ? asArray<string>(await GetInstalledVersionIds(directory))
          : [],
      );
    } catch {
      setInstalled([]);
    }
    try {
      setFavorites(parseFavorites(await GetValue(FAVORITE_KEY)));
    } catch {
      setFavorites([]);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const persist = useCallback(async (next: string[]) => {
    setFavorites(next);
    try {
      await SetValue(FAVORITE_KEY, JSON.stringify(next));
    } catch {
      /* 持久化失败不影响本次会话 */
    }
  }, []);

  const addFavorite = () => {
    if (!selectedVersion || favorites.includes(selectedVersion)) return;
    void persist([...favorites, selectedVersion].slice(0, FAVORITE_LIMIT + 4));
  };

  const removeFavorite = (versionId: string) => {
    void persist(favorites.filter((id) => id !== versionId));
  };

  const launch = async (versionId: string) => {
    setLaunchError("");
    setLaunchingId(versionId);
    try {
      const failed = await onLaunchVersion(versionId);

      if (failed) setLaunchError(failed);
    } finally {
      setLaunchingId("");
    }
  };

  // 卸载掉的收藏自动隐藏（不从配置删除，重装后自动回来）
  const visible = favorites.filter((id) => installed.includes(id));

  return (
    <HomeCard
      action={
        <Button
          isIconOnly
          aria-label={t("收藏当前实例")}
          className="h-7 w-7 min-w-7 text-gray-400"
          isDisabled={!selectedVersion || favorites.includes(selectedVersion)}
          radius="full"
          size="sm"
          title={
            selectedVersion
              ? t("收藏「{0}」", { "0": selectedVersion })
              : t("先在启动页选择实例")
          }
          variant="light"
          onPress={addFavorite}
        >
          <Add20Regular />
        </Button>
      }
      icon={<Star20Regular />}
      label={t("常用实例")}
      tileClass="from-amber-400 via-yellow-500 to-lime-500 shadow-yellow-500/30"
      value={
        visible.length > 0
          ? t("{0} 个收藏", { "0": visible.length })
          : t("暂无收藏")
      }
      valueClass="bg-gradient-to-r from-amber-500 via-yellow-500 to-lime-500"
    >
      {visible.length > 0 ? (
        <ul className="flex flex-col gap-2">
          {visible.map((versionId, index) => (
            <li
              key={versionId}
              className={`nya-enter nya-stagger-${index + 1} flex items-center gap-2`}
            >
              <span className="flex size-8 flex-none items-center justify-center rounded-xl bg-black/5 text-gray-400 dark:bg-white/10">
                <Star20Regular className="h-4 w-4" />
              </span>
              <span className="min-w-0 flex-1 overflow-hidden text-xs font-medium text-ellipsis whitespace-nowrap">
                {versionId}
              </span>
              <Button
                isIconOnly
                aria-label={t("移除收藏 {0}", { "0": versionId })}
                className="h-7 w-7 min-w-7 text-gray-400"
                isDisabled={isBusy || launchingId === versionId}
                radius="full"
                size="sm"
                title={t("移除收藏")}
                variant="light"
                onPress={() => removeFavorite(versionId)}
              >
                <Dismiss20Regular />
              </Button>
              <Button
                isIconOnly
                aria-label={t("启动 {0}", { "0": versionId })}
                className="h-7 w-7 min-w-7 text-primary"
                color="primary"
                isDisabled={isBusy}
                isLoading={launchingId === versionId}
                radius="full"
                size="sm"
                title={t("启动 {0}", { "0": versionId })}
                variant="flat"
                onPress={() => void launch(versionId)}
              >
                {!isBusy && launchingId !== versionId ? (
                  <Play20Filled />
                ) : (
                  <span />
                )}
              </Button>
            </li>
          ))}
        </ul>
      ) : (
        <div className="rounded-2xl bg-black/5 px-4 py-3 text-center text-xs leading-relaxed text-gray-400 dark:bg-white/5">
          {t("点右上角 + 收藏当前实例")}
        </div>
      )}
      {launchError ? (
        <div className="flex items-start gap-2 rounded-xl bg-danger/10 px-3 py-2 text-xs text-danger">
          <span className="mt-px flex-none">
            <Warning20Regular className="h-4 w-4" />
          </span>
          <span className="break-all">{launchError}</span>
        </div>
      ) : null}
    </HomeCard>
  );
};

export default FavoriteInstancesCard;
