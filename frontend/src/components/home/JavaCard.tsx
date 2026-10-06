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
import type { config, download } from "../../../wailsjs/go/models";

import React, { useCallback, useEffect, useState } from "react";
import { Button, Chip } from "@heroui/react";
import {
  ArrowDownload20Regular,
  ArrowClockwise20Regular as RefreshIcon,
  DrinkCoffee20Regular,
} from "@fluentui/react-icons";

import { GetJavaPaths } from "../../../wailsjs/go/bindings/ConfigAPI";
import { GetInstalledJavaRuntimes } from "../../../wailsjs/go/bindings/DownloadAPI";
import { navigateToPage } from "../../lib/navigation";
import { asArray } from "../../lib/guards";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 列表里最多展示的已保存 Java 条数（其余折叠为“等 N 个”） */
const JAVA_ROW_LIMIT = 3;

/** 路径 → 尾部可读名（取最后一段目录 + 文件名，避免长路径撑爆卡片） */
function pathTail(path: string): string {
  const segments = String(path ?? "")
    .split(/[\\/]/)
    .filter(Boolean);
  const tail = segments.slice(-2).join("\\");

  return tail || path;
}

/**
 * Java 环境卡片：汇总「设置 → Java」保存的本机 Java 与下载页安装的
 * 托管运行时；列表展示版本徽章，右上按钮直达下载页获取托管运行时。
 */
const JavaCard: React.FC = () => {
  const [paths, setPaths] = useState<config.JavaPathItem[]>([]);
  const [runtimes, setRuntimes] = useState<download.InstalledJavaRuntime[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  const reload = useCallback(async () => {
    setIsLoading(true);
    try {
      const [savedPaths, managedRuntimes] = await Promise.all([
        GetJavaPaths(),
        GetInstalledJavaRuntimes(),
      ]);

      setPaths(asArray(savedPaths));
      setRuntimes(asArray(managedRuntimes));
    } catch (err) {
      console.error(t("读取 Java 环境失败"), err);
      setPaths([]);
      setRuntimes([]);
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const total = paths.length + runtimes.length;
  const shownPaths = paths.slice(0, JAVA_ROW_LIMIT);
  const hiddenCount = paths.length - shownPaths.length;

  return (
    <HomeCard
      action={
        <div className="flex items-center gap-1">
          <Button
            isIconOnly
            aria-label={t("打开 Java 下载页")}
            className="h-7 w-7 min-w-7 text-gray-400"
            radius="full"
            size="sm"
            title={t("打开 Java 下载页")}
            variant="light"
            onPress={() => navigateToPage("download", "java")}
          >
            <ArrowDownload20Regular />
          </Button>
          <Button
            isIconOnly
            aria-label={t("刷新 Java 列表")}
            className="h-7 w-7 min-w-7 text-gray-400"
            isDisabled={isLoading}
            radius="full"
            size="sm"
            title={t("刷新 Java 列表")}
            variant="light"
            onPress={() => void reload()}
          >
            <RefreshIcon />
          </Button>
        </div>
      }
      icon={<DrinkCoffee20Regular />}
      label={t("Java 环境")}
      tileClass="from-orange-400 via-amber-500 to-yellow-500 shadow-amber-500/30"
      value={
        isLoading
          ? t("检测中…")
          : total > 0
            ? t("{0} 个 Java", { "0": total })
            : t("未找到")
      }
      valueClass="bg-gradient-to-r from-orange-500 via-amber-500 to-yellow-500"
    >
      {isLoading ? (
        <div className="rounded-lg bg-black/5 px-4 py-3 text-center text-xs text-gray-400 dark:bg-white/5">
          {t("正在读取已保存的 Java…")}
        </div>
      ) : total > 0 ? (
        <ul className="flex flex-col gap-2">
          {shownPaths.map((item, index) => (
            <li
              key={item.JavaPath}
              className={`nya-enter nya-stagger-${index + 1} flex items-center gap-2`}
            >
              <span className="flex size-8 flex-none items-center justify-center rounded-lg bg-black/5 text-gray-400 dark:bg-white/10">
                <DrinkCoffee20Regular className="h-4 w-4" />
              </span>
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="overflow-hidden text-xs text-ellipsis whitespace-nowrap">
                  {pathTail(item.JavaPath)}
                </span>
              </span>
              {index === 0 ? (
                <Chip color="primary" size="sm" variant="flat">
                  {t("默认")}
                </Chip>
              ) : null}
              <Chip size="sm" variant="flat">
                {item.JavaVersion || t("未知版本")}
              </Chip>
            </li>
          ))}
          {runtimes.length > 0 ? (
            <li
              className={`nya-enter nya-stagger-${Math.min(shownPaths.length, JAVA_ROW_LIMIT) + 1} flex items-center justify-between gap-2`}
            >
              <span className="text-xs text-gray-400">
                {t("托管运行时（下载页安装）")}
              </span>
              <Chip color="secondary" size="sm" variant="flat">
                {runtimes.length} {t("个")}
              </Chip>
            </li>
          ) : null}
          {hiddenCount > 0 ? (
            <li className="text-[11px] text-gray-400">
              {t("还有")} {hiddenCount} {t("个，可在 设置 → Java运行时 查看")}
            </li>
          ) : null}
        </ul>
      ) : (
        <div className="flex flex-col items-center gap-3">
          <span className="text-center text-xs leading-relaxed text-gray-400">
            {t("没有可用的 Java")}
          </span>
          <Button
            color="primary"
            size="sm"
            startContent={<ArrowDownload20Regular />}
            variant="flat"
            onPress={() => navigateToPage("download", "java")}
          >
            {t("去下载 Java")}
          </Button>
        </div>
      )}
    </HomeCard>
  );
};

export default JavaCard;
