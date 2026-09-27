/*
 * 日历小组件：当月月历网格。
 * - 今日用主题色圆点高亮；周末与法定节假日显示为红色（节假日表复用
 *   CountdownCard 的 HOLIDAYS_BY_YEAR，覆盖 2025-2028）；
 * - 标题行可前后翻月，翻走后出现「回到今天」一键复位；
 * - 纯前端计算，每分钟自刷新以处理跨天。
 */
import React, { useEffect, useMemo, useState } from "react";
import {
  CalendarLtr20Regular,
  ChevronLeft20Regular,
  ChevronRight20Regular,
  Target20Regular,
} from "@fluentui/react-icons";
import { Button } from "@heroui/react";

import { t } from "../../i18n";

import HomeCard from "./HomeCard";
import { holidayTable } from "./CountdownCard";

/** 表头单字（索引与 Date.getDay() 对齐：0 = 周日） */
const WEEKDAY_LETTERS_KEYS = ["日", "一", "二", "三", "四", "五", "六"];

function sameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

/** 查某天的法定节假日名（不在表内返回 undefined） */
function holidayNameFor(date: Date): string | undefined {
  const month = date.getMonth() + 1;
  const day = date.getDate();

  for (const [m, d, name] of holidayTable(date.getFullYear())) {
    if (m === month && d === day) return name;
  }

  return undefined;
}

interface MonthCell {
  date: Date;
  /** 是否属于正在查看的月份（前后补位日为 false，显示为淡色） */
  inMonth: boolean;
  isToday: boolean;
  holiday?: string;
}

/** 从周日起头的 6×7 网格（42 格，保证卡片高度稳定） */
function buildMonthCells(view: Date, today: Date): MonthCell[] {
  const first = new Date(view.getFullYear(), view.getMonth(), 1);
  const start = new Date(
    first.getFullYear(),
    first.getMonth(),
    1 - first.getDay(),
  );
  const cells: MonthCell[] = [];

  for (let index = 0; index < 42; index++) {
    const date = new Date(
      start.getFullYear(),
      start.getMonth(),
      start.getDate() + index,
    );

    cells.push({
      date,
      inMonth: date.getMonth() === view.getMonth(),
      isToday: sameDay(date, today),
      holiday: holidayNameFor(date),
    });
  }

  return cells;
}

const CalendarCard: React.FC = () => {
  const [now, setNow] = useState(() => new Date());
  /** 相对当前月的偏移：0 = 本月 */
  const [offset, setOffset] = useState(0);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 60000);

    return () => window.clearInterval(timer);
  }, []);

  const view = useMemo(
    () => new Date(now.getFullYear(), now.getMonth() + offset, 1),
    [now, offset],
  );
  const cells = useMemo(() => buildMonthCells(view, now), [view, now]);
  const weekdayLetters = useMemo(
    () => WEEKDAY_LETTERS_KEYS.map((key) => t(key)),

    [],
  );

  return (
    <HomeCard
      action={
        <div className="flex items-center gap-1">
          {offset !== 0 ? (
            <Button
              isIconOnly
              aria-label={t("回到今天")}
              size="sm"
              title={t("回到今天")}
              variant="flat"
              onPress={() => setOffset(0)}
            >
              <Target20Regular />
            </Button>
          ) : null}
          <Button
            isIconOnly
            aria-label={t("上个月")}
            size="sm"
            variant="flat"
            onPress={() => setOffset((value) => value - 1)}
          >
            <ChevronLeft20Regular />
          </Button>
          <Button
            isIconOnly
            aria-label={t("下个月")}
            size="sm"
            variant="flat"
            onPress={() => setOffset((value) => value + 1)}
          >
            <ChevronRight20Regular />
          </Button>
        </div>
      }
      icon={<CalendarLtr20Regular />}
      label={t("日历")}
      value={t("{0} 年 {1} 月", {
        "0": view.getFullYear(),
        "1": view.getMonth() + 1,
      })}
    >
      <div className="grid grid-cols-7 gap-y-0.5 text-center text-[11px]">
        {weekdayLetters.map((letter, index) => (
          <span
            key={letter + index}
            className={`pb-0.5 ${
              index === 0 || index === 6 ? "text-rose-400/90" : "text-gray-400"
            }`}
          >
            {letter}
          </span>
        ))}
        {cells.map((cell) => {
          const weekday = cell.date.getDay();
          const isWeekend = weekday === 0 || weekday === 6;
          const numberClass = cell.isToday
            ? "flex size-6 items-center justify-center rounded-full bg-primary font-bold text-primary-foreground"
            : cell.inMonth
              ? cell.holiday || isWeekend
                ? "flex size-6 items-center justify-center rounded-full text-rose-500 dark:text-rose-400"
                : "flex size-6 items-center justify-center rounded-full text-gray-700 dark:text-gray-300"
              : "flex size-6 items-center justify-center rounded-full text-gray-400/40 dark:text-gray-600";

          return (
            <div
              key={cell.date.getTime()}
              className="flex flex-col items-center"
              title={cell.holiday}
            >
              <span className={numberClass}>{cell.date.getDate()}</span>
              {/* 占位小点：节假日显示红点，其余透明，保证行高一致 */}
              <span
                className={`my-0.5 size-1 rounded-full ${
                  cell.holiday && cell.inMonth
                    ? "bg-rose-400"
                    : "bg-transparent"
                }`}
              />
            </div>
          );
        })}
      </div>
    </HomeCard>
  );
};

export default CalendarCard;
