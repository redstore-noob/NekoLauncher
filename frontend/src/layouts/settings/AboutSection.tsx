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
import type { update } from "../../../wailsjs/go/models";

import React, { useEffect, useState } from "react";
import { Button, Chip, Switch } from "@heroui/react";
import { Heart20Regular } from "@fluentui/react-icons";

import {
  GetFormattedVersion,
  ClearLogs,
} from "../../../wailsjs/go/bindings/SystemAPI";
import {
  GetStorageDirectory,
  IsPortableMode,
} from "../../../wailsjs/go/bindings/ConfigAPI";
import { OpenPage } from "../../../wailsjs/go/bindings/OnlineAPI";
import { OpenPath } from "../../../wailsjs/go/bindings/SystemAPI";
import {
  ApplyLauncherUpdate,
  CheckLauncherUpdate,
  DownloadLauncherUpdate,
  GetAutoUpdateEnabled,
  GetUpdateChannel,
  GetUpdateChannels,
  SaveAutoUpdateEnabled,
  SaveUpdateChannel,
} from "../../../wailsjs/go/bindings/UpdateAPI";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { useLogViewer } from "../../components/LogViewer";
import Contributors from "../../components/about/Contributors";
import LicenseDialog from "../../components/about/LicenseDialog";
import { notify } from "../../components/overlay/dialog";
import { asText } from "../../lib/guards";
import { t } from "../../i18n";

import Section, { SettingRow } from "./Section";

/** 赞助页地址（关于页底部入口） */
const SPONSOR_URL = "https://afdian.com/a/redstore-noob";

/** 关于页：关于与维护 + 开源致谢（许可证列表 / 贡献者名单）两个分区 */
const AboutSection: React.FC = () => {
  const [version, setVersion] = useState("");
  const [storage, setStorage] = useState("");
  const [cleared, setCleared] = useState<number | null>(null);
  const [licensesOpen, setLicensesOpen] = useState(false);
  const { openLogs } = useLogViewer();

  // ---- 启动器自身更新（X-1）----
  // 更新通道：stable=只看正式版 / preview=含预发布（默认，项目以 preview 发版为主）
  const [channel, setChannel] = useState("preview");
  const [channelOptions, setChannelOptions] = useState<
    { Value: string; Label: string }[]
  >([
    { Value: "stable", Label: "稳定版" },
    { Value: "preview", Label: "预览版" },
  ]);
  const [autoCheck, setAutoCheck] = useState(true);
  const [checking, setChecking] = useState(false);
  const [updateHint, setUpdateHint] = useState("");
  const [latest, setLatest] = useState<update.CheckResult | null>(null);
  const [downloading, setDownloading] = useState(false);
  const [portable, setPortable] = useState(false);
  const [progress, setProgress] = useState<{ done: number; total: number }>({
    done: 0,
    total: 0,
  });

  useEffect(() => {
    (async () => {
      // 后端异常时可能返回非字符串，洗一道避免把对象直接渲染进 React 树
      setVersion(asText(await GetFormattedVersion()));
      setStorage(asText(await GetStorageDirectory()));
      setPortable(await IsPortableMode().catch(() => false));
      // 更新偏好从配置恢复（默认开启+预览通道，见 internal/config 的回退值）
      setAutoCheck(await GetAutoUpdateEnabled().catch(() => true));
      setChannel(await GetUpdateChannel().catch(() => "preview"));
      const options = await GetUpdateChannels().catch(() => null);

      if (Array.isArray(options) && options.length > 0)
        setChannelOptions(options);
    })();
  }, []);

  // 开关变化即落盘；手动检查沿用同一份通道
  const toggleAutoCheck = (enabled: boolean) => {
    setAutoCheck(enabled);
    void SaveAutoUpdateEnabled(enabled).catch(() => undefined);
  };

  const chooseChannel = (next: string) => {
    setChannel(next);
    void SaveUpdateChannel(next).catch(() => undefined);
  };

  // 下载进度由后端推送（update:progress），前端只做展示
  useEffect(() => {
    const off = EventsOn(
      "update:progress",
      (payload: { downloaded?: number; total?: number }) => {
        setProgress({
          done: Math.max(0, payload?.downloaded ?? 0),
          total: Math.max(0, payload?.total ?? 0),
        });
      },
    );

    return () => {
      if (typeof off === "function") off();
    };
  }, []);

  const checkUpdate = async () => {
    setChecking(true);
    setUpdateHint(t("正在检查更新…"));
    setLatest(null);
    try {
      const result = await CheckLauncherUpdate(channel);

      setLatest(result);
      if (result.UpdateAvailable) {
        setUpdateHint("");
      } else {
        setUpdateHint(t("已是最新版本（{0}）", { "0": result.LatestVersion }));
      }
      // 有新版但本平台装不了：直接把原因摆出来，别让用户点半天没反应
      if (result.UpdateAvailable && result.ManualHint) {
        setUpdateHint(result.ManualHint);
      }
    } catch (error) {
      setUpdateHint(
        t("检查更新失败：{0}", {
          "0": error instanceof Error ? error.message : String(error),
        }),
      );
    } finally {
      setChecking(false);
    }
  };

  const downloadAndApply = async () => {
    if (!latest?.Asset) return;
    setDownloading(true);
    setProgress({ done: 0, total: latest.Asset.Size ?? 0 });
    setUpdateHint(t("正在下载新版本…"));
    try {
      const path = await DownloadLauncherUpdate(latest.Asset);

      setUpdateHint(t("下载完成，正在替换并重启…"));
      const started = await ApplyLauncherUpdate(path);

      if (!started) {
        setUpdateHint(
          t("新版本已下载：{0}，请手动替换后重启。", { "0": path }),
        );
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);

      notify.error(t("更新失败：{0}", { "0": message }));
      setUpdateHint(t("更新失败：{0}", { "0": message }));
    } finally {
      setDownloading(false);
    }
  };

  const progressText = () => {
    if (progress.total <= 0) return "";
    const percent = Math.min(
      100,
      Math.round((progress.done / progress.total) * 100),
    );

    return `${percent}%`;
  };

  return (
    <>
      <Section
        aliases={[
          t("关于"),
          t("版本"),
          t("更新"),
          t("日志"),
          t("存储"),
          t("维护"),
          "about",
        ]}
        title={t("关于与维护")}
      >
        <SettingRow label={t("版本")}>
          <span className="text-xs text-gray-600 dark:text-gray-300 tabular-nums">
            {version}
          </span>
        </SettingRow>

        <SettingRow label={t("自动检查更新")}>
          <Switch
            isSelected={autoCheck}
            size="sm"
            onValueChange={toggleAutoCheck}
          />
        </SettingRow>

        <SettingRow label={t("更新通道")}>
          <div className="flex items-center gap-1">
            {channelOptions.map((option) => (
              <Chip
                key={option.Value}
                className="cursor-pointer"
                color={channel === option.Value ? "primary" : "default"}
                size="sm"
                variant="flat"
                onClick={() => chooseChannel(option.Value)}
              >
                {t(option.Label)}
              </Chip>
            ))}
          </div>
        </SettingRow>

        <SettingRow hint={updateHint || undefined} label={t("检查更新")}>
          <div className="flex flex-wrap items-center justify-end gap-2">
            <Button
              isLoading={checking}
              size="sm"
              variant="flat"
              onPress={() => void checkUpdate()}
            >
              {t("检查更新")}
            </Button>
            {latest?.UpdateAvailable && latest.Asset && latest.CanSelfUpdate ? (
              <Button
                color="primary"
                isLoading={downloading}
                size="sm"
                variant="flat"
                onPress={() => void downloadAndApply()}
              >
                {downloading
                  ? t("正在下载 {0}", { "0": progressText() })
                  : t("下载并重启")}
              </Button>
            ) : null}
            {latest?.UpdateAvailable && !latest.Asset ? (
              <Button
                color="primary"
                size="sm"
                variant="flat"
                onPress={() => void OpenPage(latest.PageURL)}
              >
                {t("打开发布页")}
              </Button>
            ) : null}
          </div>
        </SettingRow>

        {latest?.UpdateAvailable ? (
          <div className="flex flex-col gap-1 rounded-lg border border-primary/30 bg-primary/5 px-3 py-2 text-[11px] leading-relaxed text-gray-600 dark:text-gray-300">
            <span className="font-medium text-primary">
              {t("发现新版本 {0}", { "0": latest.LatestVersion })}
              {latest.Prerelease ? ` · ${t("预发布版")}` : ""}
            </span>
            {latest.PublishedAt ? (
              <span className="text-gray-400">
                {t("发布于")}{" "}
                {new Date(latest.PublishedAt * 1000).toLocaleString()}
              </span>
            ) : null}
            {latest.Notes ? (
              <span className="max-h-32 overflow-y-auto whitespace-pre-line">
                {latest.Notes.slice(0, 2000)}
              </span>
            ) : null}
          </div>
        ) : null}

        <SettingRow
          hint={portable ? t("便携模式：数据跟着程序目录走") : undefined}
          label={t("存储目录")}
        >
          <div className="flex items-center gap-2">
            <Button size="sm" variant="light" onPress={() => OpenPath(storage)}>
              {t("打开")}
            </Button>
            <span className="text-xs text-gray-600 truncate max-w-[18rem]">
              {storage}
            </span>
            {portable ? (
              <Chip color="primary" size="sm" variant="flat">
                {t("便携模式")}
              </Chip>
            ) : null}
          </div>
        </SettingRow>

        <SettingRow
          hint={
            cleared != null ? t("已清除{0}个文件", { "0": cleared }) : undefined
          }
          label={t("运行日志")}
        >
          <div className="flex items-center gap-2">
            <Button size="sm" variant="flat" onPress={openLogs}>
              {t("查看")}
            </Button>
            <Button
              color="danger"
              size="sm"
              variant="flat"
              onPress={async () => {
                const n = await ClearLogs();

                setCleared(n);
                setTimeout(() => setCleared(null), 3000);
              }}
            >
              {t("清空")}
            </Button>
          </div>
        </SettingRow>
      </Section>

      <Section
        aliases={[
          t("开源"),
          t("许可"),
          "license",
          t("致谢"),
          t("贡献者"),
          "contributors",
          "github",
          t("团队"),
        ]}
        title={t("开源致谢")}
      >
        <SettingRow label={t("开源许可")}>
          <Button
            size="sm"
            variant="flat"
            onPress={() => setLicensesOpen(true)}
          >
            {t("查看许可列表")}
          </Button>
        </SettingRow>

        <SettingRow label={t("贡献者")}>
          <Contributors />
        </SettingRow>

        <SettingRow label={t("爱发电")}>
          <Button
            size="sm"
            startContent={<Heart20Regular />}
            variant="flat"
            onPress={() => void OpenPage(SPONSOR_URL).catch(() => undefined)}
          >
            {t("赞助支持喵～")}
          </Button>
        </SettingRow>

        <SettingRow label={t("模组中文名")}>
          <div className="max-w-md text-right text-[11px] text-gray-400">
            <p>
              {t(
                "「已安装模组」列表中的中文译名优先来自 SCL 社区译名数据集，未收录时通过模组文件名检索 MC百科（mcmod.cn）获得，数据与译名版权归 MC百科 所有。",
              )}
            </p>
            <a
              className="text-primary underline decoration-dotted"
              href="https://www.mcmod.cn"
              rel="noreferrer"
              target="_blank"
            >
              www.mcmod.cn
            </a>
          </div>
        </SettingRow>
      </Section>

      <LicenseDialog
        isOpen={licensesOpen}
        onClose={() => setLicensesOpen(false)}
      />
    </>
  );
};

export default AboutSection;
