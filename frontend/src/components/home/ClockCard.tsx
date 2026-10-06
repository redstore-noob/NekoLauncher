/*
 * 时钟卡片：大号数字时钟 + 日期星期，秒级刷新（定时器对齐到下一秒整点，
 * 避免挂载时刻偏移导致的时间跳变）。
 */
import React, { useEffect, useState } from "react";

import { t } from "../../i18n";

import HomeCard from "./HomeCard";

const WEEKDAYS = [
  t("星期日"),
  t("星期一"),
  t("星期二"),
  t("星期三"),
  t("星期四"),
  t("星期五"),
  t("星期六"),
];

const ClockCard: React.FC = () => {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    // 先校正到下一整秒再按秒跳动，数字变化与系统秒严格同步
    const delay = 1000 - (Date.now() % 1000) + 5;

    const tick = () => {
      setNow(new Date());
      timer = window.setTimeout(tick, 1000);
    };
    let timer = window.setTimeout(tick, delay);

    return () => window.clearTimeout(timer);
  }, []);

  const hh = String(now.getHours()).padStart(2, "0");
  const mm = String(now.getMinutes()).padStart(2, "0");
  const ss = String(now.getSeconds()).padStart(2, "0");
  const date = t("{m} 月 {d} 日", {
    m: now.getMonth() + 1,
    d: now.getDate(),
  });

  return (
    <HomeCard value={`${hh}:${mm}:${ss}`}>
      <div className="flex items-baseline justify-between text-xs text-gray-400">
        <span>
          {date} {WEEKDAYS[now.getDay()]}
        </span>
      </div>
    </HomeCard>
  );
};

export default ClockCard;
