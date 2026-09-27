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
import type { config } from "../../../wailsjs/go/models";

import React, { useEffect, useState } from "react";
import { Button, Input } from "@heroui/react";
import { Flag20Regular, Trophy20Regular } from "@fluentui/react-icons";

import { GetValue, SetValue } from "../../../wailsjs/go/bindings/ConfigAPI";
import { formatPlaytime } from "../../lib/home";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 目标时长（小时）的 launcher.yaml 键 */
const GOAL_HOURS_KEY = "homeGoalHours";
/** 目标小时数上限：防手滑输入离谱值 */
const GOAL_HOURS_MAX = 100000;

interface GoalCardProps {
  /** 游玩统计记录（与「游玩统计」卡片同源） */
  records: config.PlaytimeRecord[];
  /** 游戏运行中：卡片呼吸光晕 */
  isGameRunning: boolean;
}

/**
 * 成就目标卡片：设定一个累计游玩时长目标（小时），
 * 用游玩统计数据画进度条；达成后换上奖杯与庆祝态。
 * 数据口径与「游玩统计」一致：全部实例的总时长。
 */
const GoalCard: React.FC<GoalCardProps> = ({ records, isGameRunning }) => {
  // null = 配置加载中；0 = 未设置目标
  const [goalHours, setGoalHours] = useState<number | null>(null);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");

  useEffect(() => {
    GetValue(GOAL_HOURS_KEY)
      .then((raw) => {
        const parsed = Number(raw);

        setGoalHours(Number.isFinite(parsed) && parsed > 0 ? parsed : 0);
      })
      .catch(() => setGoalHours(0));
  }, []);

  const totalSeconds = records.reduce(
    (sum, record) => sum + record.PlaytimeSeconds,
    0,
  );
  const totalHours = totalSeconds / 3600;
  const goal = goalHours ?? 0;
  const percent = goal > 0 ? Math.min(100, (totalHours / goal) * 100) : 0;
  const achieved = goal > 0 && totalSeconds >= goal * 3600;
  const remainingHours = Math.max(0, goal - totalHours);

  const saveGoal = async () => {
    const parsed = Number(draft);

    if (!Number.isFinite(parsed) || parsed <= 0 || parsed > GOAL_HOURS_MAX) {
      return;
    }
    setEditing(false);
    setGoalHours(parsed);
    try {
      await SetValue(GOAL_HOURS_KEY, String(parsed));
    } catch {
      /* 持久化失败不影响本次会话 */
    }
  };

  const startEditing = () => {
    setDraft(goal > 0 ? String(goal) : "");
    setEditing(true);
  };

  return (
    <HomeCard
      icon={achieved ? <Trophy20Regular /> : <Flag20Regular />}
      label={t("游玩目标")}
      live={isGameRunning}
      tileClass="from-emerald-400 via-green-500 to-teal-500 shadow-emerald-500/30"
      value={
        goal > 0
          ? t("{0} / {1} 小时", { "0": totalHours.toFixed(1), "1": goal })
          : t("{0} 小时", { "0": totalHours.toFixed(1) })
      }
      valueClass="bg-gradient-to-r from-emerald-500 via-green-500 to-teal-500"
    >
      {editing ? (
        <div className="flex items-center gap-2">
          <Input
            aria-label={t("目标小时数")}
            classNames={{
              inputWrapper:
                "bg-default-100/80 data-[hover=true]:bg-default-200",
            }}
            placeholder={t("例如 100（小时）")}
            radius="full"
            size="sm"
            type="number"
            value={draft}
            onValueChange={setDraft}
          />
          <Button
            color="primary"
            isDisabled={!(Number(draft) > 0 && Number(draft) <= GOAL_HOURS_MAX)}
            size="sm"
            variant="flat"
            onPress={() => void saveGoal()}
          >
            {t("保存")}
          </Button>
          <Button
            className="text-gray-400"
            size="sm"
            variant="light"
            onPress={() => setEditing(false)}
          >
            {t("取消")}
          </Button>
        </div>
      ) : goal > 0 ? (
        <>
          {/* 进度条 + 百分比 */}
          <div className="flex items-center gap-2.5">
            <div className="h-2 flex-1 overflow-hidden rounded-full bg-black/10 dark:bg-white/10">
              <div
                className={`nya-bar h-full rounded-full transition-[width] duration-700 ease-out ${
                  achieved ? "bg-amber-500" : "bg-primary"
                }`}
                style={{ width: `${Math.max(2, percent)}%` }}
              />
            </div>
            <span
              className={`flex-none text-[11px] font-semibold tabular-nums ${
                achieved ? "text-amber-600 dark:text-amber-300" : "text-primary"
              }`}
            >
              {percent.toFixed(0)}%
            </span>
          </div>

          {achieved ? (
            <div className="rounded-xl bg-amber-500/10 px-3 py-2 text-center text-xs leading-relaxed text-amber-600 dark:text-amber-300">
              {t("目标达成！")}
            </div>
          ) : (
            <div className="text-center text-[11px] text-gray-400">
              {t("还差")} {formatPlaytime(Math.round(remainingHours * 3600))}{" "}
              {t("达成目标")}
            </div>
          )}

          <div className="flex items-center justify-end gap-2">
            <Button
              color={achieved ? "primary" : "default"}
              size="sm"
              variant="flat"
              onPress={startEditing}
            >
              {achieved ? t("下一个目标") : t("修改目标")}
            </Button>
          </div>
        </>
      ) : (
        <div className="flex flex-col items-center gap-3">
          <span className="text-center text-xs leading-relaxed text-gray-400">
            {t("暂无目标")}
          </span>
          <Button
            color="primary"
            size="sm"
            startContent={<Flag20Regular />}
            variant="flat"
            onPress={startEditing}
          >
            {t("设定目标")}
          </Button>
        </div>
      )}
    </HomeCard>
  );
};

export default GoalCard;
