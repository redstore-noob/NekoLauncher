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
import React, { useEffect, useState } from "react";
import { Button, Select, SelectItem } from "@heroui/react";
import { ArrowClockwise20Regular } from "@fluentui/react-icons";

import {
  ClearValue,
  GetValue,
  SetValue,
} from "../../../wailsjs/go/bindings/ConfigAPI";
import { OOBE_COMPLETED_KEY } from "../../components/oobe/OobeProvider";
import { popoverMotionProps } from "../../lib/motion";
import { t } from "../../i18n";

import Section, { SettingRow } from "./Section";

/** 关闭按钮行为选项（launcher.yaml 的 closeAction 键；"ask" 或未设置 = 每次询问） */
const CLOSE_ACTIONS = [
  { key: "ask", label: "每次询问" },
  { key: "tray", label: "最小化到托盘" },
  { key: "exit", label: "直接退出" },
] as const;

const BehaviorSection: React.FC = () => {
  const [closeAction, setCloseAction] = useState<string>("ask");
  const [hint, setHint] = useState("");

  useEffect(() => {
    void GetValue("closeAction").then((saved) => {
      if (saved === "tray" || saved === "exit") setCloseAction(saved);
    });
  }, []);

  const change = async (key: string) => {
    setCloseAction(key);
    const ok = await SetValue("closeAction", key);

    setHint(ok ? t("已保存。") : t("保存失败。"));
  };

  // 重放首次引导：删掉完成标记后整页重载，OobeProvider 读不到键就会
  // 重新弹出欢迎三选页（含跟随小窗流程）。重载与语言切换同款做法。
  const replayOobe = async () => {
    const ok = await ClearValue(OOBE_COMPLETED_KEY);

    if (!ok) {
      setHint(t("操作失败，请重试。"));

      return;
    }
    window.location.reload();
  };

  return (
    <Section
      aliases={[
        t("关闭"),
        t("托盘"),
        t("退出"),
        t("最小化"),
        t("行为"),
        t("引导"),
        t("新手"),
        "tray",
        "close",
        "oobe",
      ]}
      title={t("启动器行为")}
    >
      <SettingRow
        hint={t("托盘常驻，可单击图标唤回窗口")}
        label={t("关闭按钮行为")}
      >
        <div className="flex items-center gap-2">
          <Select
            aria-label={t("关闭按钮行为")}
            className="w-44"
            items={CLOSE_ACTIONS.map((action) => ({
              key: action.key,
              label: t(action.label),
            }))}
            popoverProps={{ motionProps: popoverMotionProps }}
            selectedKeys={[closeAction]}
            size="sm"
            variant="bordered"
            onSelectionChange={(keys) => {
              const key = String(Array.from(keys)[0] ?? "");

              if (key) void change(key);
            }}
          >
            {(item: { key: string; label: string }) => (
              <SelectItem key={item.key}>{item.label}</SelectItem>
            )}
          </Select>
          {hint ? <span className="text-xs text-gray-400">{hint}</span> : null}
        </div>
      </SettingRow>
      <SettingRow
        hint={t("重新查看萌新 / 创作者引导，不影响任何现有配置")}
        label={t("首次启动引导")}
      >
        <Button
          size="sm"
          startContent={<ArrowClockwise20Regular />}
          variant="flat"
          onPress={() => void replayOobe()}
        >
          {t("重新运行引导")}
        </Button>
      </SettingRow>
    </Section>
  );
};

export default BehaviorSection;
