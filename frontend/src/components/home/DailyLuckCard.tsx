/*
 * 今日运气卡片：以「本地日期 + 设备码」为随机种子的确定性每日运势。
 * 同一天、同一台设备上结果恒定（刷新不重摇），跨天 0 点自动重摇。
 * 种子经 GetDeviceId（注册表 MachineGuid 摘要）获取，读取失败时回退浏览器指纹，
 * 保证任何情况下都能出结果。
 */
import React, { useEffect, useMemo, useState } from "react";
import {
  CalendarLtr20Regular,
  Color20Regular,
  NumberSymbol20Regular,
} from "@fluentui/react-icons";

import { GetDeviceId } from "../../../wailsjs/go/bindings/SystemAPI";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 运势档位（权重仿御神签：吉多凶少） */
const LUCK_LEVELS: Array<{
  label: string;
  weight: number;
  score: [number, number];
}> = [
  { label: "大吉", weight: 12, score: [90, 100] },
  { label: "中吉", weight: 20, score: [75, 89] },
  { label: "小吉", weight: 20, score: [60, 74] },
  { label: "吉", weight: 24, score: [45, 59] },
  { label: "末吉", weight: 10, score: [30, 44] },
  { label: "凶", weight: 10, score: [12, 29] },
  { label: "大凶", weight: 4, score: [0, 11] },
];

/** 宜 / 忌 清单（Minecraft 风味） */
const GOOD_AT = [
  "下矿挖钻石",
  "附魔装备",
  "精修农场",
  "与村民交易",
  "搭建铁路",
  "钓鱼",
  "探索地牢",
  "养猪养牛",
  "整理箱子",
  "浇灌农田",
  "挑战末影龙",
  "盖一栋新房子",
  "研究红石机关",
  "远足寻找要塞",
];
const BAD_AT = [
  "空手走悬崖",
  "招惹末影人",
  "雷雨天举三叉戟",
  "在基岩层上方搭床",
  "对着苦力帕贴脸",
  "熬夜肝材质包",
  "不带火把下矿",
  "岩浆边上泡脚",
  "把所有鸡蛋放进一个箱子",
  "跟凋灵比嗓门",
  "用木镐挖黑曜石",
  "在深暗之域里大声唱歌",
];
/** 幸运色（名称 + 值，展示为色卡） */
const LUCKY_COLORS: Array<{ name: string; hex: string }> = [
  { name: "草方块绿", hex: "#7CBD4C" },
  { name: "钻石青", hex: "#5DECDB" },
  { name: "下界红", hex: "#B02E26" },
  { name: "末地紫", hex: "#A64EA4" },
  { name: "金苹果黄", hex: "#F5C33B" },
  { name: "海洋蓝", hex: "#3C44A9" },
  { name: "雪傀儡白", hex: "#F0F0F0" },
  { name: "猫晶红", hex: "#F38BAA" },
];

/** 本地日期键：YYYY-MM-DD（跨天重摇的依据） */
function dateKey(now = new Date()): string {
  const y = now.getFullYear();
  const m = String(now.getMonth() + 1).padStart(2, "0");
  const d = String(now.getDate()).padStart(2, "0");

  return `${y}-${m}-${d}`;
}

/** xmur3 字符串哈希 → 32 位种子 */
function hashSeed(text: string): number {
  let h = 1779033703 ^ text.length;

  for (let i = 0; i < text.length; i++) {
    h = Math.imul(h ^ text.charCodeAt(i), 3432918353);
    h = (h << 13) | (h >>> 19);
  }
  h = Math.imul(h ^ (h >>> 16), 2246822507);
  h = Math.imul(h ^ (h >>> 13), 3266489909);

  return (h ^= h >>> 16) >>> 0;
}

/** mulberry32 确定性 PRNG */
function mulberry32(seed: number): () => number {
  let a = seed;

  return () => {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);

    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;

    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

interface DailyFortune {
  level: string;
  score: number;
  good: string;
  bad: string;
  luckyColor: { name: string; hex: string };
  luckyNumber: number;
}

/** 由日期 + 设备码推算当日运势（纯函数，同种子恒定结果） */
function computeFortune(day: string, deviceId: string): DailyFortune {
  const rand = mulberry32(hashSeed(`${day}|${deviceId}`));
  const total = LUCK_LEVELS.reduce((sum, level) => sum + level.weight, 0);
  let ticket = rand() * total;
  let level = LUCK_LEVELS[0];

  for (const candidate of LUCK_LEVELS) {
    ticket -= candidate.weight;
    if (ticket < 0) {
      level = candidate;
      break;
    }
  }
  const [min, max] = level.score;
  const pick = <T,>(list: T[]): T => list[Math.floor(rand() * list.length)];

  return {
    level: level.label,
    score: Math.round(min + rand() * (max - min)),
    good: pick(GOOD_AT),
    bad: pick(BAD_AT),
    luckyColor: pick(LUCKY_COLORS),
    luckyNumber: 1 + Math.floor(rand() * 99),
  };
}

/** 运势档位配色（大吉到凶：绿 → 红） */
function levelClass(label: string): string {
  switch (label) {
    case "大吉":
      return "bg-emerald-500/15 text-emerald-600 dark:text-emerald-300";
    case "大凶":
      return "bg-danger/15 text-danger";
    case "凶":
      return "bg-warning-500/15 text-warning-600 dark:text-warning-400";
    default:
      return "bg-primary/15 text-primary";
  }
}

const DailyLuckCard: React.FC = () => {
  const [deviceId, setDeviceId] = useState("");
  const [today, setToday] = useState(() => dateKey());
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    GetDeviceId()
      .then((id) => setDeviceId(id || "local-fallback"))
      .catch(() => setDeviceId("local-fallback"));
  }, []);

  // 跨天重摇：对准下一个 0 点刷新（误差分钟级即可）
  useEffect(() => {
    const timer = window.setInterval(() => {
      const key = dateKey();

      setNow(new Date());
      if (key !== today) setToday(key);
    }, 30_000);

    return () => window.clearInterval(timer);
  }, [today]);

  const fortune = useMemo(
    () => (deviceId ? computeFortune(today, deviceId) : null),
    [today, deviceId],
  );

  const weekDay = t(
    ["周日", "周一", "周二", "周三", "周四", "周五", "周六"][now.getDay()],
  );

  return (
    <HomeCard
      icon={<CalendarLtr20Regular />}
      label={t("今日运气")}
      value={
        fortune ? (
          <span
            className={`rounded-lg px-2.5 py-0.5 text-xl ${levelClass(fortune.level)}`}
          >
            {t(fortune.level)}
          </span>
        ) : (
          "…"
        )
      }
    >
      {fortune ? (
        <>
          <div className="flex flex-col gap-1.5">
            <div className="flex items-baseline justify-between gap-2">
              <span className="text-xs font-medium">
                {today} {weekDay}
              </span>
              <span className="flex-none text-[11px] text-gray-400 tabular-nums">
                {t("运势值")} {fortune.score}
              </span>
            </div>
            <div className="h-1.5 overflow-hidden rounded-full bg-black/10 dark:bg-white/10">
              <div
                className="nya-bar h-full rounded-full transition-[width] duration-700 ease-out"
                style={{ width: `${Math.max(4, fortune.score)}%` }}
              />
            </div>
          </div>

          <ul className="flex flex-col gap-1.5 text-xs">
            <li className="nya-enter nya-stagger-1 flex items-center gap-2">
              <span className="flex-none rounded-md bg-emerald-500/15 px-1.5 py-0.5 text-[11px] font-semibold text-emerald-600 dark:text-emerald-300">
                {t("宜")}
              </span>
              <span className="truncate">{t(fortune.good)}</span>
            </li>
            <li className="nya-enter nya-stagger-2 flex items-center gap-2">
              <span className="flex-none rounded-md bg-danger/15 px-1.5 py-0.5 text-[11px] font-semibold text-danger">
                {t("忌")}
              </span>
              <span className="truncate">{t(fortune.bad)}</span>
            </li>
          </ul>

          <div className="flex items-center justify-between gap-2 rounded-lg bg-default-100/80 px-3 py-2 text-[11px] text-gray-400">
            <span className="flex items-center gap-1.5">
              <NumberSymbol20Regular className="h-3.5 w-3.5" />

              {t("幸运数字")}
              <b className="text-gray-600 tabular-nums dark:text-gray-200">
                {fortune.luckyNumber}
              </b>
            </span>
            <span className="flex items-center gap-1.5">
              <Color20Regular className="h-3.5 w-3.5" />
              <span
                className="inline-block size-3 flex-none rounded-full ring-1 ring-black/10 dark:ring-white/20"
                style={{ backgroundColor: fortune.luckyColor.hex }}
              />
              {t(fortune.luckyColor.name)}
            </span>
          </div>
        </>
      ) : (
        <div className="py-3 text-center text-xs text-gray-400">
          {t("正在摇签…")}
        </div>
      )}
    </HomeCard>
  );
};

export default DailyLuckCard;
