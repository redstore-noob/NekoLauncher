/*
 * 黑胶唱片播放页（Now Playing 全屏浮层）：
 * 左侧大黑胶唱片（封面作唱片芯，播放时旋转），右侧歌词面板；
 * 歌词逐行高亮随播放滚动、点击歌词行跳转进度；底部完整控制条
 * （快退/播放/快进/上下一首 + 进度 + 收藏）。
 * 歌词经 SystemAPI.ReadTextFile 读同目录同名 .lrc，封面经 lib/coverArt 提取。
 * 由音乐页底部栏的唱片按钮唤起，点击背景 / Esc / 关闭按钮退出。
 */
import type { music } from "../../../wailsjs/go/models";

import React, { useEffect, useRef, useState } from "react";
import { motion } from "framer-motion";
import { Button } from "@heroui/react";
import {
  MusicNote220Regular as MusicNoteIcon,
  Dismiss20Regular as CloseIcon,
  Previous20Regular as SkipPreviousIcon,
  Next20Regular as SkipNextIcon,
  Play20Regular as PlayIcon,
  Pause20Regular as PauseIcon,
  ArrowRotateCounterclockwise20Regular as RewindIcon,
  ArrowRotateClockwise20Regular as ForwardIcon,
  Heart20Regular as HeartIcon,
  Heart20Filled as HeartFilledIcon,
} from "@fluentui/react-icons";

import {
  NextTrack,
  PausePlayback,
  PreviousTrack,
  ResumePlayback,
  SeekPlayback,
} from "../../../wailsjs/go/bindings/MusicAPI";
import { useAudioState } from "../../lib/audioBridge";
import { getCoverUrl } from "../../lib/coverArt";
import { t } from "../../i18n";

import { useLrc, LyricLines } from "./Lyrics";

type MusicTrack = music.MusicTrack;

function trackTitle(track: MusicTrack | null): string {
  if (!track) return t("未在播放");
  const base = track.FilePath.split(/[\\/]/).pop() ?? "";

  return base.replace(/\.[^.]+$/, "");
}

function formatTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return "0:00";
  const total = Math.floor(seconds);

  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

const NowPlayingView: React.FC<{
  track: MusicTrack | null;
  isFavorite?: boolean;
  onToggleFavorite?: () => void;
  onClose: () => void;
  /** 当前播放队列（共享播放列表镜像），供右侧歌单曲展示 */
  queue?: MusicTrack[];
  /** 点播队列中的曲目 */
  onPlayTrack?: (track: MusicTrack) => void;
}> = ({
  track,
  isFavorite = false,
  onToggleFavorite,
  onClose,
  queue,
  onPlayTrack,
}) => {
  const audio = useAudioState();

  const [coverUrl, setCoverUrl] = useState<string | null>(null);
  const [coverFailed, setCoverFailed] = useState(false);
  const { lrc, missing: lrcMissing } = useLrc(track?.FilePath ?? "");

  // --- 封面加载（换曲即重载） ---------------------------------------------
  useEffect(() => {
    let alive = true;
    const path = track?.FilePath ?? "";

    setCoverUrl(null);
    setCoverFailed(false);
    if (!path) return;
    getCoverUrl(path)
      .then((url) => {
        if (alive) {
          setCoverUrl(url);
          setCoverFailed(!url);
        }
      })
      .catch(() => {
        if (alive) setCoverFailed(true);
      });

    return () => {
      alive = false;
    };
  }, [track?.FilePath]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };

    window.addEventListener("keydown", onKey);

    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const playing = audio.playing;
  const positionMs = audio.positionMs;
  const durationSec = audio.durationMs / 1000;
  const progressPct =
    durationSec > 0
      ? Math.min(100, (positionMs / 1000 / durationSec) * 100)
      : 0;
  const progressEnabled = durationSec > 0 && audio.canPlay;

  function togglePlayPause() {
    if (playing) PausePlayback();
    else void ResumePlayback().catch(() => undefined);
  }

  function nudge(deltaSec: number) {
    if (!progressEnabled) return;
    const target = Math.min(
      durationSec,
      Math.max(0, audio.positionMs / 1000 + deltaSec),
    );

    SeekPlayback(Math.round(target * 1000)).catch(() => undefined);
  }

  return (
    <motion.div
      animate={{ opacity: 1 }}
      className="nya-nowplaying fixed inset-0 z-50 flex flex-col items-center justify-center gap-6 overflow-hidden px-8"
      exit={{ opacity: 0 }}
      initial={{ opacity: 0 }}
      role="presentation"
      transition={{ duration: 0.25 }}
      onClick={onClose}
    >
      {/* 顶部标题与关闭：避开 40px 高的自定义标题栏（top-14 起步） */}
      <div className="absolute top-14 right-5 left-5 flex items-center">
        <span className="text-[11px] font-semibold tracking-widest text-gray-400 uppercase">
          {t("正在播放")}
        </span>
        <button
          aria-label={t("关闭")}
          className="ml-auto flex size-9 cursor-pointer items-center justify-center rounded-full text-gray-400 transition-colors hover:bg-default-100/60 hover:text-gray-600"
          type="button"
          onClick={onClose}
        >
          <CloseIcon />
        </button>
      </div>

      <div
        className="flex w-full max-w-5xl items-center justify-center gap-10"
        role="presentation"
        onClick={(e) => e.stopPropagation()}
      >
        {/* 黑胶唱片 */}
        <motion.div
          animate={{ scale: 1, rotate: 0 }}
          className="relative size-[280px] flex-none"
          exit={{ scale: 0.9 }}
          initial={{ scale: 0.9 }}
          transition={{ type: "spring", stiffness: 200, damping: 22 }}
        >
          {/* 唱片本体（沟槽纹理 + 旋转） */}
          <div
            className="nya-vinyl-disc absolute inset-0 rounded-full bg-gradient-to-br from-gray-800 to-black shadow-2xl shadow-black/40 dark:from-gray-900 dark:to-black"
            style={{ animationPlayState: playing ? "running" : "paused" }}
          >
            {/* 高光 */}
            <div className="absolute inset-0 rounded-full bg-[conic-gradient(from_0deg,transparent_0deg,rgba(255,255,255,0.07)_20deg,transparent_60deg,rgba(255,255,255,0.05)_160deg,transparent_220deg,rgba(255,255,255,0.07)_300deg,transparent_360deg)]" />
            {/* 封面作唱片芯 */}
            <div className="absolute inset-[26%] overflow-hidden rounded-full ring-2 ring-white/10">
              {coverUrl ? (
                <img
                  alt={trackTitle(track)}
                  className="size-full object-cover"
                  src={coverUrl}
                />
              ) : (
                <div className="flex size-full items-center justify-center bg-default-200 text-gray-500 dark:bg-gray-800">
                  {coverFailed ? (
                    <MusicNoteIcon className="h-8 w-8" />
                  ) : (
                    <span className="size-8 animate-pulse rounded-full bg-default-300/60 dark:bg-gray-700" />
                  )}
                </div>
              )}
            </div>
            {/* 中孔 */}
            <div className="absolute top-1/2 left-1/2 size-[14px] -translate-x-1/2 -translate-y-1/2 rounded-full bg-black/85 ring-4 ring-white/10" />
          </div>
        </motion.div>

        {/* 曲目信息 + 歌词 */}
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <div className="flex flex-col gap-1">
            <div className="flex items-center gap-2">
              <h2 className="min-w-0 flex-1 truncate text-2xl font-bold tracking-tight">
                {trackTitle(track)}
              </h2>
              {track && onToggleFavorite ? (
                <Button
                  isIconOnly
                  aria-label={isFavorite ? t("取消收藏") : t("收藏")}
                  className={`min-w-unit-9 h-unit-9 shrink-0 ${isFavorite ? "text-rose-500" : "text-gray-400"}`}
                  radius="full"
                  size="sm"
                  variant="light"
                  onPress={onToggleFavorite}
                >
                  {isFavorite ? <HeartFilledIcon /> : <HeartIcon />}
                </Button>
              ) : null}
            </div>
            <div className="flex items-center gap-2 text-[12px] text-gray-400">
              <span className="font-mono tabular-nums">
                {formatTime(positionMs / 1000)} / {formatTime(durationSec)}
              </span>
              {durationSec > 0 ? (
                <span className="h-1 min-w-16 flex-1 overflow-hidden rounded-full bg-default-200 dark:bg-gray-700">
                  <span
                    className="block h-full rounded-full bg-primary transition-[width] duration-300 ease-linear"
                    style={{ width: `${progressPct}%` }}
                  />
                </span>
              ) : null}
            </div>
          </div>

          {/* 歌词面板：逐行高亮 + 自动滚动 + 点击行跳转进度 */}
          <LyricLines
            className="h-[300px] rounded-lg"
            lrc={lrc}
            missing={lrcMissing || !track}
            positionMs={positionMs}
            onSeekMs={(ms) => SeekPlayback(ms).catch(() => undefined)}
          />

          {/* 底部控制条 */}
          <div className="flex flex-none items-center justify-center gap-1.5">
            <TooltipedButton label={t("上一首")} onPress={previousTrack}>
              <SkipPreviousIcon />
            </TooltipedButton>
            <TooltipedButton label={t("快退 10 秒")} onPress={() => nudge(-10)}>
              <RewindIcon />
            </TooltipedButton>
            <Button
              isIconOnly
              aria-label={playing ? t("暂停") : t("播放")}
              className="min-w-unit-11 h-unit-11 shadow-md shadow-primary/25 active:scale-90"
              color="primary"
              radius="full"
              onPress={togglePlayPause}
            >
              {playing ? (
                <PauseIcon />
              ) : (
                <span className="inline-flex translate-x-px">
                  <PlayIcon />
                </span>
              )}
            </Button>
            <TooltipedButton label={t("快进 10 秒")} onPress={() => nudge(10)}>
              <ForwardIcon />
            </TooltipedButton>
            <TooltipedButton label={t("下一首")} onPress={nextTrack}>
              <SkipNextIcon />
            </TooltipedButton>
          </div>
        </div>

        {/* 歌单列：当前队列、播放中的曲目高亮，点击切歌（窄窗口收起） */}
        {queue && queue.length > 0 && onPlayTrack ? (
          <PlayQueue
            currentPath={track?.FilePath ?? ""}
            playing={playing}
            queue={queue}
            onSelect={onPlayTrack}
          />
        ) : null}
      </div>
    </motion.div>
  );
};

/** 播放页右侧歌单列：滚动列表 + 当前曲目自动滚入视野。 */
const PlayQueue: React.FC<{
  queue: MusicTrack[];
  currentPath: string;
  playing: boolean;
  onSelect: (track: MusicTrack) => void;
}> = ({ queue, currentPath, playing, onSelect }) => {
  const currentRef = useRef<HTMLButtonElement | null>(null);

  // 打开/切歌时把当前曲目滚到视野中部
  useEffect(() => {
    currentRef.current?.scrollIntoView({ block: "center" });
  }, [currentPath]);

  return (
    <aside className="hidden h-[420px] w-60 flex-none flex-col gap-2 self-center lg:flex">
      <div className="flex items-baseline justify-between px-1">
        <span className="text-[11px] font-semibold tracking-widest text-gray-300 uppercase">
          {t("播放列表")}
        </span>
        <span className="text-[10px] text-gray-400 tabular-nums">
          {queue.length}
        </span>
      </div>
      <div className="nya-scroll min-h-0 flex-1 overflow-y-auto rounded-large">
        {queue.map((item, index) => {
          const active = item.FilePath === currentPath;

          return (
            <button
              key={item.FilePath}
              ref={active ? currentRef : undefined}
              className={`flex w-full cursor-pointer items-center gap-2 rounded-medium px-2.5 py-2 text-left transition-colors ${
                active
                  ? "bg-white/10 text-white"
                  : "text-gray-300 hover:bg-white/5 hover:text-white"
              }`}
              type="button"
              onClick={() => onSelect(item)}
            >
              <span className="w-4 flex-none text-center text-[10px] text-gray-400 tabular-nums">
                {active ? (
                  <span className="text-primary">{playing ? "♪" : "‖"}</span>
                ) : (
                  index + 1
                )}
              </span>
              <span className="min-w-0 flex-1 truncate text-xs">
                {trackTitle(item)}
              </span>
            </button>
          );
        })}
      </div>
    </aside>
  );
};

function previousTrack() {
  PreviousTrack().catch(() => undefined);
}
function nextTrack() {
  NextTrack().catch(() => undefined);
}

/** 全屏页底部控制条的幽灵按钮（扁平圆形，悬停微亮）。 */
const TooltipedButton: React.FC<{
  label: string;
  onPress: () => void;
  children: React.ReactNode;
}> = ({ label, onPress, children }) => (
  <Button
    isIconOnly
    aria-label={label}
    className="min-w-unit-10 h-unit-10 text-gray-400 active:scale-90"
    radius="full"
    variant="light"
    onPress={onPress}
  >
    {children}
  </Button>
);

export default NowPlayingView;
