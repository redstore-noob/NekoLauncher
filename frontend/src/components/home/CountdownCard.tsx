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
import React, { useEffect, useMemo, useState } from "react";
import { CalendarClock20Regular } from "@fluentui/react-icons";

import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 展示的目标数量：最近的 1 个大字 + 后续 3 个小行 */
const TARGET_LIMIT = 4;

/**
 * 法定节假日表（公历日期，按年显式列出避免农历推算误差）。
 * 覆盖 2025-2028；超出年份时该表为空，仅剩周末倒计时。
 */
const HOLIDAYS_BY_YEAR: Record<number, Array<[number, number, string]>> = {
  2025: [
    [1, 1, t("元旦")],
    [1, 29, t("春节")],
    [4, 4, t("清明节")],
    [5, 1, t("劳动节")],
    [5, 31, t("端午节")],
    [10, 1, t("国庆节")],
    [10, 6, t("中秋节")],
  ],
  2026: [
    [1, 1, t("元旦")],
    [2, 17, t("春节")],
    [4, 5, t("清明节")],
    [5, 1, t("劳动节")],
    [6, 19, t("端午节")],
    [9, 25, t("中秋节")],
    [10, 1, t("国庆节")],
  ],
  2027: [
    [1, 1, t("元旦")],
    [2, 6, t("春节")],
    [4, 5, t("清明节")],
    [5, 1, t("劳动节")],
    [6, 9, t("端午节")],
    [9, 15, t("中秋节")],
    [10, 1, t("国庆节")],
  ],
  2028: [
    [1, 1, t("元旦")],
    [1, 26, t("春节")],
    [4, 4, t("清明节")],
    [5, 1, t("劳动节")],
    [6, 22, t("端午节")],
    [10, 1, t("国庆节")],
    [10, 3, t("中秋节")],
  ],
};

interface CountdownTarget {
  name: string;
  /** 本地零点对齐的目标日期 */
  date: Date;
  /** 距今天的天数：0 = 今天 */
  days: number;
}

function daysUntil(date: Date, now: Date): number {
  const startOfToday = new Date(
    now.getFullYear(),
    now.getMonth(),
    now.getDate(),
  );

  return Math.round((date.getTime() - startOfToday.getTime()) / 86400000);
}

/** 下一个周六（含今天，供"周末倒计时"使用） */
function nextSaturday(now: Date): Date {
  const target = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const offset = (6 - target.getDay() + 7) % 7;

  target.setDate(target.getDate() + offset);

  return target;
}

/** 年份超表返回空数组（只剩周末倒计时）；日历卡片也用它标记节假日 */
export function holidayTable(year: number): Array<[number, number, string]> {
  return HOLIDAYS_BY_YEAR[year] ?? [];
}

function buildTargets(now: Date): CountdownTarget[] {
  const targets: CountdownTarget[] = [];

  // 法定节假日：本年 + 次年
  for (const year of [now.getFullYear(), now.getFullYear() + 1]) {
    for (const [month, day, name] of holidayTable(year)) {
      const date = new Date(year, month - 1, day);
      const days = daysUntil(date, now);

      if (days >= 0) targets.push({ date, days, name });
    }
  }

  // 周末倒计时：周六不在周末当天才计时（周末进行中时不显示）
  const saturday = nextSaturday(now);
  const saturdayDays = daysUntil(saturday, now);

  if (saturdayDays > 0) {
    targets.push({ date: saturday, days: saturdayDays, name: t("周末") });
  }

  // 同一天多条目（如节假日撞上周末）：保留排在前面的节假日
  targets.sort(
    (left, right) =>
      left.days - right.days || left.date.getTime() - right.date.getTime(),
  );
  const seenDays = new Set<number>();
  const unique: CountdownTarget[] = [];

  for (const target of targets) {
    if (seenDays.has(target.days)) continue;
    seenDays.add(target.days);
    unique.push(target);
    if (unique.length >= TARGET_LIMIT) break;
  }

  return unique;
}

function daysLabel(days: number): string {
  if (days <= 0) return t("就是今天");
  if (days === 1) return t("明天");

  return t("{0} 天后", { "0": days });
}

const WEEKDAYS = [
  t("周日"),
  t("周一"),
  t("周二"),
  t("周三"),
  t("周四"),
  t("周五"),
  t("周六"),
];

function dateLabel(date: Date): string {
  return t("{0} 月 {1} 日 · {2}", {
    "0": date.getMonth() + 1,
    "1": date.getDate(),
    "2": WEEKDAYS[date.getDay()],
  });
}

/**
 * 节日倒计时卡片：距离最近的周末/法定节假日还有几天。
 * 纯前端计算，本地时区零点对齐；每分钟刷新一次以处理跨天。
 */
const CountdownCard: React.FC = () => {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 60000);

    return () => window.clearInterval(timer);
  }, []);

  const targets = useMemo(() => buildTargets(now), [now]);
  const primary = targets[0];
  const rest = targets.slice(1);
  const isWeekendNow = now.getDay() === 0 || now.getDay() === 6;

  return (
    <HomeCard
      icon={<CalendarClock20Regular />}
      label={t("节日倒计时")}
      tileClass="from-rose-400 via-pink-500 to-fuchsia-500 shadow-pink-500/30"
      value={
        primary
          ? daysLabel(primary.days)
          : isWeekendNow
            ? t("周末进行中")
            : t("暂无目标")
      }
      valueClass="bg-gradient-to-r from-rose-500 via-pink-500 to-fuchsia-500"
    >
      {primary ? (
        <div className="flex flex-col gap-2.5">
          {/* 最近目标大字行 */}
          <div
            className={`nya-enter nya-stagger-1 flex items-baseline justify-between gap-2 ${
              primary.days === 0 ? "text-primary" : ""
            }`}
          >
            <span className="text-sm font-bold">{primary.name}</span>
            <span className="flex-none text-[11px] text-gray-400 tabular-nums">
              {dateLabel(primary.date)}
            </span>
          </div>

          {/* 后续目标小行 */}
          {rest.map((target, index) => (
            <div
              key={`${target.name}-${target.days}`}
              className={`nya-enter nya-stagger-${index + 2} flex items-baseline justify-between gap-2 text-xs`}
            >
              <span className="text-gray-500 dark:text-gray-400">
                {target.name}
              </span>
              <span className="flex-none text-[11px] text-gray-400 tabular-nums">
                {daysLabel(target.days)}
              </span>
            </div>
          ))}
        </div>
      ) : (
        <div className="rounded-lg bg-black/5 px-4 py-3 text-center text-xs leading-relaxed text-gray-400 dark:bg-white/5">
          {isWeekendNow ? t("周末进行中") : t("近期没有节假日")}
        </div>
      )}
    </HomeCard>
  );
};

export default CountdownCard;
