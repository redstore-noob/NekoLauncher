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
import { Button, Select, SelectItem, Input } from "@heroui/react";

import { asArray } from "../../lib/guards";
import { popoverMotionProps } from "../../lib/motion";
import {
  GetAllDownloadSources,
  GetActiveDownloadSourceName,
  SaveActiveDownloadSource,
  GetFallbackDownloadSourceName,
  SaveFallbackDownloadSource,
  GetParallelDownloads,
  GetSpeedLimitKbps,
  SaveParallelDownloads,
  SaveSpeedLimitKbps,
} from "../../../wailsjs/go/bindings/DownloadAPI";
import { t } from "../../i18n";

import Section, { SettingRow } from "./Section";

const DownloadSection: React.FC = () => {
  const [sources, setSources] = useState<download.DownloadSource[]>([]);
  const [active, setActive] = useState("");
  const [fallback, setFallback] = useState("");
  const [parallel, setParallel] = useState(4);
  const [speedLimit, setSpeedLimit] = useState(0);
  const [saveHint, setSaveHint] = useState("");

  useEffect(() => {
    (async () => {
      const [list, a, f, p, s] = await Promise.all([
        GetAllDownloadSources(),
        GetActiveDownloadSourceName(),
        GetFallbackDownloadSourceName(),
        GetParallelDownloads(),
        GetSpeedLimitKbps(),
      ]);

      setSources(asArray(list));
      setActive(a ?? "");
      setFallback(f ?? "");
      setParallel(p || 4);
      setSpeedLimit(Number(s) || 0);
    })();
  }, []);

  const changeActive = async (name: string) => {
    const src = sources.find((s) => s.Name === name);

    if (src) {
      await SaveActiveDownloadSource(src);
      setActive(name);
    }
  };

  const changeFallback = async (name: string) => {
    if (name === "__none__") {
      await SaveFallbackDownloadSource(
        null as unknown as download.DownloadSource,
      );
      setFallback("");

      return;
    }
    const src = sources.find((s) => s.Name === name);

    if (src) {
      await SaveFallbackDownloadSource(src);
      setFallback(name);
    }
  };

  return (
    <Section
      aliases={[
        t("下载"),
        t("源"),
        t("镜像"),
        t("线程"),
        t("并发"),
        t("回退"),
        "download",
      ]}
      title={t("下载")}
    >
      <SettingRow label={t("下载源")}>
        <Select
          className="w-56 min-w-0 max-w-full [&_*]:min-w-0"
          items={sources}
          popoverProps={{ motionProps: popoverMotionProps }}
          selectedKeys={[active]}
          size="sm"
          onSelectionChange={(keys) =>
            changeActive(Array.from(keys)[0] as string)
          }
        >
          {(item) => <SelectItem key={item.Name}>{item.Name}</SelectItem>}
        </Select>
      </SettingRow>

      <SettingRow label={t("自动回退源")}>
        <Select
          className="w-56 min-w-0 max-w-full [&_*]:min-w-0"
          items={[{ Name: "__none__", __label: t("禁用") } as any, ...sources]}
          popoverProps={{ motionProps: popoverMotionProps }}
          selectedKeys={[fallback || "__none__"]}
          size="sm"
          onSelectionChange={(keys) =>
            changeFallback(Array.from(keys)[0] as string)
          }
        >
          {(item: any) => (
            <SelectItem key={item.Name}>{item.__label ?? item.Name}</SelectItem>
          )}
        </Select>
      </SettingRow>

      <SettingRow label={t("并行下载线程数")}>
        <Input
          className="w-24 min-w-0 max-w-full [&_*]:min-w-0"
          isInvalid={parallel < 1 || parallel > 64}
          max={64}
          min={1}
          size="sm"
          type="number"
          value={String(parallel)}
          onValueChange={(v) => {
            // 夹在 1~64：空串/非法输入回落 4，避免 0 或离谱值被存下去
            const n = Math.round(Number(v));

            setParallel(Number.isFinite(n) ? Math.min(64, Math.max(1, n)) : 4);
          }}
        />
      </SettingRow>

      <SettingRow
        hint={t("0 表示不限速，所有下载任务共享总带宽")}
        label={t("下载限速（KB/s）")}
      >
        <Input
          className="w-32 min-w-0 max-w-full [&_*]:min-w-0"
          isInvalid={speedLimit < 0}
          min={0}
          size="sm"
          type="number"
          value={String(speedLimit)}
          onValueChange={(v) => {
            const n = Math.round(Number(v));

            setSpeedLimit(
              Number.isFinite(n) ? Math.min(1048576, Math.max(0, n)) : 0,
            );
          }}
        />
      </SettingRow>

      <div className="flex items-center gap-2 pt-1">
        <Button
          color="primary"
          size="sm"
          onPress={async () => {
            await SaveParallelDownloads(parallel);
            await SaveSpeedLimitKbps(speedLimit);
            setSaveHint(
              speedLimit > 0
                ? t("已保存：{0} 线程，限速 {1} KB/s", {
                    "0": parallel,
                    "1": speedLimit,
                  })
                : t("已保存：{0} 线程，不限速", { "0": parallel }),
            );
            setTimeout(() => setSaveHint(""), 2000);
          }}
        >
          {t("保存下载设置")}
        </Button>
        {saveHint ? (
          <span className="text-xs text-primary">{saveHint}</span>
        ) : null}
      </div>
    </Section>
  );
};

export default DownloadSection;
