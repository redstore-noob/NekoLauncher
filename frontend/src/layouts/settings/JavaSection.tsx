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

import React, { useEffect, useState } from "react";
import { Button, Chip } from "@heroui/react";

import { asArray } from "../../lib/guards";
import {
  GetJavaPaths,
  AddJava,
  RemoveJava,
  SetPrimaryJava,
  AutoDetectJava,
} from "../../../wailsjs/go/bindings/ConfigAPI";
import { DetectJavaMajorVersion } from "../../../wailsjs/go/bindings/LauncherAPI";
import {
  GetInstalledJavaRuntimes,
  DeleteJavaRuntime,
} from "../../../wailsjs/go/bindings/DownloadAPI";
import { SelectFile } from "../../../wailsjs/go/bindings/SystemAPI";
import { notify } from "../../components/overlay/dialog";
import { t } from "../../i18n";

import Section from "./Section";

const JavaSection: React.FC = () => {
  const [paths, setPaths] = useState<config.JavaPathItem[]>([]);
  const [runtimes, setRuntimes] = useState<download.InstalledJavaRuntime[]>([]);
  const [detecting, setDetecting] = useState(false);

  const reload = async () => {
    const [p, r] = await Promise.all([
      GetJavaPaths(),
      GetInstalledJavaRuntimes(),
    ]);

    setPaths(asArray(p));
    setRuntimes(asArray(r));
  };

  useEffect(() => {
    reload();
  }, []);

  /** 自动检索本机 Java 并并入列表：只新增，不动用户已配置的条目与主 Java。 */
  const autoDetect = async () => {
    setDetecting(true);
    try {
      const result = await AutoDetectJava();
      const added = asArray<string>(result?.Added);
      const found = asArray<string>(result?.Found);

      if (added.length > 0) {
        notify.success(t("已添加 {0} 个 Java", { "0": added.length }));
      } else if (found.length > 0) {
        notify.info(
          t("找到 {0} 个 Java，但都已经在列表里了", { "0": found.length }),
        );
      } else {
        notify.warning(
          t("没有找到可用的 Java，请确认已安装 JRE/JDK（或在下方手动添加）"),
        );
      }
      await reload();
    } catch (error) {
      notify.error(
        t("自动检索失败：{0}", {
          "0": error instanceof Error ? error.message : String(error),
        }),
      );
    } finally {
      setDetecting(false);
    }
  };

  const add = async () => {
    const picked = await SelectFile(
      t("选择java可执行文件"),
      "Java",
      "java.exe;java",
    );

    if (!picked) return;
    const major = await DetectJavaMajorVersion(picked);

    if (!major || major <= 0) {
      notify.error(
        t("无法识别该文件的 Java 版本，请确认选择的是有效的 java 可执行文件"),
      );

      return;
    }
    await AddJava(picked, String(major));
    await reload();
  };

  return (
    <Section
      aliases={[
        "java",
        "jre",
        "jdk",
        t("运行时"),
        t("运行环境"),
        t("托管"),
        t("默认"),
        t("路径"),
      ]}
      title={t("Java运行时")}
    >
      <div className="flex items-center justify-between">
        <div className="text-sm text-gray-800 dark:text-gray-200">
          {t("已保存的Java")}
        </div>
        <div className="flex items-center gap-2">
          <Button
            isLoading={detecting}
            size="sm"
            variant="flat"
            onPress={() => void autoDetect()}
          >
            {t("自动检索")}
          </Button>
          <Button size="sm" variant="flat" onPress={add}>
            {t("添加")}
          </Button>
        </div>
      </div>
      {paths.length === 0 ? (
        <div className="text-xs text-gray-400">{t("暂无")}</div>
      ) : (
        <ul className="space-y-1">
          {paths.map((item, idx) => (
            <li
              key={item.JavaPath}
              className="flex items-center gap-2 text-sm py-1"
            >
              <span className="truncate flex-1 text-gray-700 dark:text-gray-300">
                {item.JavaPath}
              </span>
              <Chip size="sm" variant="flat">
                {item.JavaVersion}
              </Chip>
              {idx === 0 ? (
                <Chip color="primary" size="sm" variant="flat">
                  {t("默认")}
                </Chip>
              ) : (
                <Button
                  size="sm"
                  variant="light"
                  onPress={async () => {
                    await SetPrimaryJava(item.JavaPath);
                    await reload();
                  }}
                >
                  {t("设为默认")}
                </Button>
              )}
              <Button
                color="danger"
                size="sm"
                variant="light"
                onPress={async () => {
                  await RemoveJava(item.JavaPath);
                  await reload();
                }}
              >
                {t("移除")}
              </Button>
            </li>
          ))}
        </ul>
      )}

      <div className="pt-4">
        <div className="text-sm text-gray-800 dark:text-gray-200 mb-2">
          {t("已安装的托管运行时")}
        </div>
        {runtimes.length === 0 ? (
          <div className="text-xs text-gray-400">{t("暂无")}</div>
        ) : (
          <ul className="space-y-1">
            {runtimes.map((rt, i) => (
              <li key={i} className="flex items-center gap-2 text-sm py-1">
                <span className="truncate flex-1 text-gray-700 dark:text-gray-300">
                  {rt.JavaExecutablePath || rt.DirectoryPath || t("(未知)")}
                </span>
                {rt.MajorVersion ? (
                  <Chip size="sm" variant="flat">
                    Java {rt.MajorVersion}
                  </Chip>
                ) : null}
                {rt.Vendor !== undefined && rt.Vendor !== null ? (
                  <Chip size="sm" variant="flat">
                    {vendorLabel(rt.Vendor)}
                  </Chip>
                ) : null}
                <Button
                  color="danger"
                  size="sm"
                  variant="light"
                  onPress={async () => {
                    if (rt.DirectoryPath) {
                      await DeleteJavaRuntime(rt.DirectoryPath);
                      await reload();
                    }
                  }}
                >
                  {t("删除")}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Section>
  );
};

// 与 Go JavaVendor 枚举一致：Zulu=0 / Oracle=1 / Temurin=2
function vendorLabel(vendor: number): string {
  switch (vendor) {
    case 0:
      return "Zulu";
    case 1:
      return "Oracle";
    case 2:
      return "Temurin";
    default:
      return `Vendor ${vendor}`;
  }
}

export default JavaSection;
