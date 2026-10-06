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
import React, { useEffect, useState } from "react";
import { Button } from "@heroui/react";

import { asArray } from "../../lib/guards";
import {
  GetProfileFolders,
  AddProfileFolder,
  RemoveProfileFolder,
} from "../../../wailsjs/go/bindings/ConfigAPI";
import { SelectDirectory } from "../../../wailsjs/go/bindings/SystemAPI";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { useI18n } from "../../i18n";

import Section, { SettingRow } from "./Section";

// 游戏目录列表：用户添加的目录 + 两个固定默认（平台默认 .minecraft 与
// Windows 的 ~/Desktop/.minecraft），彼此没有主次之分。当前扫描哪个目录
// 在实例页顶部的目录下拉里切换（选中即保存），本页只管理列表本身。
const GameDirectorySection: React.FC = () => {
  const { t } = useI18n();
  const [folders, setFolders] = useState<string[]>([]);
  // 后端保存 / 添加目录成功但没发现版本时推送的提示路径（温和提示，非错误）
  const [emptyHint, setEmptyHint] = useState("");

  useEffect(() => {
    const offEmptyDirectory = EventsOn(
      "instance:emptyDirectory",
      (path: string) => setEmptyHint(path ?? ""),
    );

    return () => offEmptyDirectory();
  }, []);

  const reload = async () => {
    setFolders(asArray(await GetProfileFolders()));
  };

  useEffect(() => {
    reload();
  }, []);

  const addFolder = async () => {
    const picked = await SelectDirectory(t("选择Minecraft目录"));

    if (picked) {
      await AddProfileFolder(picked);
      await reload();
    }
  };

  const removeFolder = async (path: string) => {
    await RemoveProfileFolder(path);
    await reload();
  };

  return (
    <Section
      aliases={[
        t("目录"),
        t("路径"),
        t("游戏目录"),
        t("额外扫描"),
        "directory",
        "path",
      ]}
      title={t("游戏目录")}
    >
      <SettingRow label={t("目录列表")}>
        <Button size="sm" variant="flat" onPress={addFolder}>
          {t("添加")}
        </Button>
      </SettingRow>

      {emptyHint ? (
        <div className="pt-1 text-xs text-amber-600 dark:text-amber-400">
          {t("该目录下没有发现 Minecraft 版本：{0}", { "0": emptyHint })}
        </div>
      ) : null}

      <div className="pt-2">
        {folders.length === 0 ? (
          <div className="text-xs text-gray-400">{t("暂无")}</div>
        ) : (
          <ul className="space-y-1">
            {folders.map((f) => (
              <li
                key={f}
                className="flex items-center justify-between gap-2 text-sm"
              >
                <span className="truncate text-gray-700 dark:text-gray-300">
                  {f}
                </span>
                <Button
                  color="danger"
                  size="sm"
                  variant="light"
                  onPress={() => removeFolder(f)}
                >
                  {t("移除")}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Section>
  );
};

export default GameDirectorySection;
