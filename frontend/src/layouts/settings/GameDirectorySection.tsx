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
import { Button, Input } from "@heroui/react";

import { asArray, asText } from "../../lib/guards";
import {
  GetGameDirectory,
  SaveGameDirectory,
  ClearGameDirectory,
  GetProfileFolders,
  AddProfileFolder,
  RemoveProfileFolder,
} from "../../../wailsjs/go/bindings/ConfigAPI";
import { SelectDirectory } from "../../../wailsjs/go/bindings/SystemAPI";
import { useI18n } from "../../i18n";

import Section, { SettingRow } from "./Section";

const GameDirectorySection: React.FC = () => {
  const { t } = useI18n();
  const [gameDir, setGameDir] = useState("");
  const [folders, setFolders] = useState<string[]>([]);
  const [confirmClear, setConfirmClear] = useState(false);

  const reload = async () => {
    const [dir, list] = await Promise.all([
      GetGameDirectory(),
      GetProfileFolders(),
    ]);

    setGameDir(asText(dir));
    setFolders(asArray(list));
  };

  useEffect(() => {
    reload();
  }, []);

  const pickDir = async () => {
    const picked = await SelectDirectory(t("选择Minecraft目录"));

    if (picked) {
      await SaveGameDirectory(picked);
      await reload();
    }
  };

  const addFolder = async () => {
    const picked = await SelectDirectory(t("选择额外扫描的目录"));

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
        t("主目录"),
        t("额外扫描"),
        "directory",
        "path",
      ]}
      title={t("游戏目录")}
    >
      <SettingRow label={t("主目录")}>
        <div className="flex items-center gap-2">
          <Input
            readOnly
            className="w-64 max-w-full min-w-0"
            placeholder={t("未设置")}
            size="sm"
            value={gameDir}
          />
          <Button size="sm" variant="flat" onPress={pickDir}>
            {t("选择")}
          </Button>
          <Button
            color={confirmClear ? "danger" : "default"}
            size="sm"
            variant={confirmClear ? "flat" : "light"}
            onPress={async () => {
              // 两步确认：清除会让实例扫描回到默认目录，误触代价不小
              if (!confirmClear) {
                setConfirmClear(true);
                setTimeout(() => setConfirmClear(false), 3000);

                return;
              }
              setConfirmClear(false);
              await ClearGameDirectory();
              await reload();
            }}
          >
            {confirmClear ? t("确认清除？") : t("清除")}
          </Button>
        </div>
      </SettingRow>

      <div className="pt-2">
        <div className="flex items-center justify-between mb-2">
          <div className="text-sm text-gray-800 dark:text-gray-200">
            {t("额外扫描的目录")}
          </div>
          <Button size="sm" variant="flat" onPress={addFolder}>
            {t("添加")}
          </Button>
        </div>
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
