/*
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
import type { update } from "../../wailsjs/go/models";

import { useEffect, useRef } from "react";

import {
  ApplyLauncherUpdate,
  DownloadLauncherUpdate,
} from "../../wailsjs/go/bindings/UpdateAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { t } from "../i18n";

import { notify, showDialogBox } from "./overlay/dialog";

/**
 * 启动时自动更新提示（全局常驻，挂在 Shell 里）。
 *
 * 后端启动几秒后按设置自动查一次 GitHub Releases，有新版就广播
 * update:available；这里弹确认框，用户点「立即更新」才下载并替换——
 * 自动检查 ≠ 自动安装，换启动器本体必须经用户确认。
 * 本平台装不了（无资产/非 Windows）时降级为一条提示（去设置→关于手动更新）。
 */
const AutoUpdateNotice: React.FC = () => {
  // 处理中标记：下载/替换期间若新一轮事件顶上来，不再重复弹窗
  const busy = useRef(false);

  useEffect(() => {
    const off = EventsOn("update:available", (result: update.CheckResult) => {
      void handleAvailable(result);
    });

    return () => {
      if (typeof off === "function") off();
    };
  }, []);

  const handleAvailable = async (result: update.CheckResult) => {
    if (busy.current || !result?.UpdateAvailable) return;

    // 本平台没有可直接替换的资产：不打扰，只给一条带指引的提示
    if (!result.Asset || !result.CanSelfUpdate) {
      if (result.ManualHint) {
        notify.info(
          t("发现新版本 {0}，可到 设置→关于 更新", {
            "0": result.LatestVersion,
          }),
          6000,
        );
      }

      return;
    }

    const prereleaseLabel = result.Prerelease ? `（${t("预发布版")}）` : "";
    const published = result.PublishedAt
      ? new Date(result.PublishedAt * 1000).toLocaleString()
      : "";

    const choice = await showDialogBox({
      title: t("发现新版本"),
      message:
        t("启动器有新版本 {0} 可用{1}，是否立即下载并重启更新？", {
          "0": result.LatestVersion,
          "1": prereleaseLabel,
        }) + (published ? `\n${t("发布于")} ${published}` : ""),
      severity: "info",
      buttons: [
        { label: t("稍后"), id: "later" },
        { label: t("立即更新"), id: "update", default: true },
      ],
    });

    if (choice !== "update") return;

    busy.current = true;
    try {
      notify.info(t("正在下载新版本 {0}…", { "0": result.LatestVersion }));
      const path = await DownloadLauncherUpdate(result.Asset);

      notify.info(t("下载完成，正在替换并重启…"));
      const started = await ApplyLauncherUpdate(path);

      if (!started) {
        notify.warning(
          t("新版本已下载：{0}，请手动替换后重启。", { "0": path }),
        );
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);

      notify.error(t("自动更新失败：{0}", { "0": message }), 6000);
    } finally {
      busy.current = false;
    }
  };

  return null;
};

export default AutoUpdateNotice;
