/*
 * 敲木鱼卡片：点击/长按连击木鱼，功德 +1。
 * 敲击声由 Web Audio 实时合成（短噪声敲击 + 低频木腔共鸣），无需音频资源；
 * 功德计数（累计 + 今日）持久化在 localStorage，按天记录今日数。
 */
import React, { useCallback, useEffect, useRef, useState } from "react";
import { FoodFish20Regular } from "@fluentui/react-icons";
import { Button } from "@heroui/react";

import { t } from "../../i18n";

import HomeCard from "./HomeCard";

const MERIT_STORAGE_KEY = "nekolauncher-muyu-merit";

interface MeritRecord {
  /** 累计功德 */
  total: number;
  /** 今日功德所属日期（YYYY-MM-DD） */
  day: string;
  /** 今日功德 */
  today: number;
}

function loadMerit(): MeritRecord {
  try {
    const raw = localStorage.getItem(MERIT_STORAGE_KEY);

    if (raw) {
      const parsed = JSON.parse(raw) as Partial<MeritRecord>;
      const day = dateKey();

      return {
        total: Number(parsed.total) || 0,
        day,
        today: parsed.day === day ? Number(parsed.today) || 0 : 0,
      };
    }
  } catch {
    /* 损坏的记录按新档处理 */
  }

  return { total: 0, day: dateKey(), today: 0 };
}

function dateKey(now = new Date()): string {
  const y = now.getFullYear();
  const m = String(now.getMonth() + 1).padStart(2, "0");
  const d = String(now.getDate()).padStart(2, "0");

  return `${y}-${m}-${d}`;
}

/** Web Audio 合成木鱼声：噪声敲击头 + 430Hz 木腔共鸣尾。 */
function knockSound(ctx: AudioContext) {
  const now = ctx.currentTime;

  // 敲击头：10ms 白噪声经带通（~1100Hz），模拟木槌接触
  const noiseLength = Math.floor(ctx.sampleRate * 0.012);
  const noise = ctx.createBuffer(1, noiseLength, ctx.sampleRate);
  const samples = noise.getChannelData(0);

  for (let i = 0; i < noiseLength; i++) {
    samples[i] = (Math.random() * 2 - 1) * (1 - i / noiseLength);
  }
  const source = ctx.createBufferSource();

  source.buffer = noise;
  const bandpass = ctx.createBiquadFilter();

  bandpass.type = "bandpass";
  bandpass.frequency.value = 1100;
  bandpass.Q.value = 1.2;
  const knockGain = ctx.createGain();

  knockGain.gain.setValueAtTime(0.5, now);
  knockGain.gain.exponentialRampToValueAtTime(0.001, now + 0.05);
  source.connect(bandpass).connect(knockGain).connect(ctx.destination);
  source.start(now);

  // 共鸣尾：低频正弦快速衰减，模拟木腔 "笃"
  const tone = ctx.createOscillator();

  tone.type = "sine";
  tone.frequency.setValueAtTime(430, now);
  tone.frequency.exponentialRampToValueAtTime(280, now + 0.1);
  const toneGain = ctx.createGain();

  toneGain.gain.setValueAtTime(0.55, now);
  toneGain.gain.exponentialRampToValueAtTime(0.001, now + 0.18);
  tone.connect(toneGain).connect(ctx.destination);
  tone.start(now);
  tone.stop(now + 0.2);
}

/** 飘出的 "+1 功德" 气泡 */
interface MeritFloat {
  id: number;
  x: number;
}

const MuyuCard: React.FC = () => {
  const [merit, setMerit] = useState<MeritRecord>(loadMerit);
  const [floats, setFloats] = useState<MeritFloat[]>([]);
  const [knocking, setKnocking] = useState(false);
  const audioCtxRef = useRef<AudioContext | null>(null);
  const holdTimerRef = useRef<number | null>(null);
  const floatIdRef = useRef(0);

  useEffect(() => {
    // 存储被禁用/配额满时 setItem 会抛；读路径已兜底，写路径同样不能让它
    // 冒到 ErrorBoundary 去（那会把整张木鱼卡换成红色错误框）
    try {
      localStorage.setItem(MERIT_STORAGE_KEY, JSON.stringify(merit));
    } catch {
      /* 记不住就只在本次会话里累计 */
    }
  }, [merit]);

  useEffect(
    () => () => {
      if (holdTimerRef.current) window.clearInterval(holdTimerRef.current);
      void audioCtxRef.current?.close();
    },
    [],
  );

  const knock = useCallback(() => {
    if (!audioCtxRef.current) {
      const Ctx =
        window.AudioContext ??
        (window as never as { webkitAudioContext: typeof AudioContext })
          .webkitAudioContext;

      audioCtxRef.current = new Ctx();
    }
    void audioCtxRef.current.resume();
    knockSound(audioCtxRef.current);

    setKnocking(false);
    requestAnimationFrame(() => setKnocking(true)); // 重触发缩放动画
    setMerit((prev) => ({
      ...prev,
      total: prev.total + 1,
      today: prev.today + 1,
    }));
    const id = ++floatIdRef.current;

    setFloats((prev) => [
      ...prev.slice(-5),
      { id, x: 20 + Math.random() * 60 },
    ]);
    window.setTimeout(() => {
      setFloats((prev) => prev.filter((item) => item.id !== id));
    }, 900);
  }, []);

  // 长按连击：150ms 一下，松开即停
  const startHold = useCallback(() => {
    knock();
    if (holdTimerRef.current) window.clearInterval(holdTimerRef.current);
    holdTimerRef.current = window.setInterval(knock, 150);
  }, [knock]);

  const stopHold = useCallback(() => {
    if (holdTimerRef.current) {
      window.clearInterval(holdTimerRef.current);
      holdTimerRef.current = null;
    }
  }, []);

  return (
    <HomeCard
      action={
        <span className="rounded-full bg-default-100/80 px-2 py-0.5 text-[11px] text-gray-400 tabular-nums">
          {t("累计")} {merit.total.toLocaleString()}
        </span>
      }
      icon={<FoodFish20Regular />}
      label={t("电子木鱼")}
      value={merit.today.toLocaleString()}
    >
      <div className="relative flex select-none flex-col items-center">
        <Button
          data-widget-interactive
          aria-label={t("敲木鱼")}
          className={`
            relative flex size-28 cursor-pointer items-center justify-center
            rounded-full p-0 shadow-inner transition-transform duration-100 ease-out
            data-[hover=true]:bg-transparent data-[active=true]:scale-90
            ${knocking ? "scale-90" : "scale-100"}
          `}
          style={{
            background:
              "radial-gradient(circle at 34% 30%, #d8a06b 0%, #b97f4e 45%, #8f5a32 78%, #6e411f 100%)",
          }}
          variant="light"
          onContextMenu={(event) => event.preventDefault()}
          onPointerCancel={stopHold}
          onPointerDown={(event) => {
            event.preventDefault();
            startHold();
          }}
          onPointerLeave={stopHold}
          onPointerUp={stopHold}
        >
          {/* 木鱼腔体与音缝 */}
          <span className="pointer-events-none absolute inset-[14%] rounded-full border border-black/15 bg-gradient-to-b from-amber-200/25 to-transparent" />
          <span className="pointer-events-none absolute left-1/2 top-[38%] h-[10%] w-[46%] -translate-x-1/2 rounded-full bg-black/35 shadow-inner" />
          <span className="pointer-events-none absolute inset-x-[26%] top-[18%] h-[12%] rounded-full bg-white/25 blur-[3px]" />
        </Button>

        {/* 飘出的功德 */}
        {floats.map((item) => (
          <span
            key={item.id}
            className="nya-merit-float pointer-events-none absolute top-2 text-sm font-bold text-primary drop-shadow"
            style={{ left: `${item.x}%` }}
          >
            {t("+1 功德")}
          </span>
        ))}

        <span className="mt-2 text-[11px] text-gray-400">
          {t("点击或长按连敲 · 今日已积")} {merit.today.toLocaleString()}{" "}
          {t("功德")}
        </span>
      </div>
    </HomeCard>
  );
};

export default MuyuCard;
