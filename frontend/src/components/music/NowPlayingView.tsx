/*
 * 黑胶唱片播放页（Now Playing 全屏浮层）：
 * 左侧大黑胶唱片（封面作唱片芯，播放时旋转），右侧歌词面板；
 * 歌词经 SystemAPI.ReadTextFile 读同目录同名 .lrc，封面经 lib/coverArt 提取。
 * 由 music.tsx 底部栏的唱片按钮唤起，点击背景 / Esc / 关闭按钮退出。
 */
import type { music } from "../../../wailsjs/go/models";

import React, { useEffect, useMemo, useRef, useState } from "react";
import { motion } from "framer-motion";
import {
  MusicNote220Regular as MusicNoteIcon,
  Dismiss20Regular as CloseIcon,
} from "@fluentui/react-icons";

import { ReadTextFile } from "../../../wailsjs/go/bindings/SystemAPI";
import { useAudioState } from "../../lib/audioBridge";
import { getCoverUrl } from "../../lib/coverArt";
import { activeLrcIndex, parseLrc, type LrcDoc } from "../../lib/lrc";
import { t } from "../../i18n";

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
  onClose: () => void;
}> = ({ track, onClose }) => {
  const audio = useAudioState();

  const [coverUrl, setCoverUrl] = useState<string | null>(null);
  const [coverFailed, setCoverFailed] = useState(false);
  const [lrc, setLrc] = useState<LrcDoc | null>(null);
  const [lrcMissing, setLrcMissing] = useState(false);

  // --- 封面 / 歌词加载（换曲即重载） ---------------------------------------
  useEffect(() => {
    let alive = true;
    const path = track?.FilePath ?? "";

    setCoverUrl(null);
    setCoverFailed(false);
    setLrc(null);
    setLrcMissing(false);
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

    const lrcPath = path.replace(/\.[^.\\/]+$/, "") + ".lrc";

    ReadTextFile(lrcPath)
      .then((text) => {
        if (!alive) return;
        const doc = text ? parseLrc(text) : null;

        if (doc) setLrc(doc);
        else setLrcMissing(true);
      })
      .catch(() => {
        if (alive) setLrcMissing(true);
      });

    return () => {
      alive = false;
    };
  }, [track?.FilePath]);

  const positionMs = audio.positionMs;
  const durationSec = audio.durationMs / 1000;
  const activeIndex = useMemo(
    () => (lrc ? activeLrcIndex(lrc.lines, positionMs) : -1),
    [lrc, positionMs],
  );

  // --- 歌词自动滚动：当前行居中；用户滚轮翻看 3s 内暂停跟随 -----------------
  const lyricsBoxRef = useRef<HTMLDivElement | null>(null);
  const activeLineRef = useRef<HTMLParagraphElement | null>(null);
  const followPausedUntil = useRef(0);

  useEffect(() => {
    if (activeIndex < 0) return;
    if (Date.now() < followPausedUntil.current) return;
    const box = lyricsBoxRef.current;
    const line = activeLineRef.current;

    if (!box || !line) return;
    box.scrollTo({
      top: line.offsetTop - box.clientHeight / 2 + line.clientHeight / 2,
      behavior: "smooth",
    });
  }, [activeIndex, lrc]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };

    window.addEventListener("keydown", onKey);

    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const playing = audio.playing;
  const progressPct =
    durationSec > 0
      ? Math.min(100, (positionMs / 1000 / durationSec) * 100)
      : 0;

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
      {/* 顶部标题与关闭 */}
      <div className="absolute top-5 right-5 left-5 flex items-center">
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
        className="flex w-full max-w-4xl items-center justify-center gap-10"
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
                    <MusicNoteIcon className="w-8 h-8" />
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
            <h2 className="truncate text-2xl font-bold tracking-tight">
              {trackTitle(track)}
            </h2>
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

          {/* 歌词面板 */}
          <div
            ref={lyricsBoxRef}
            className="nya-lyrics-box h-[320px] overflow-y-auto rounded-2xl px-4 py-6"
            onWheel={() => {
              followPausedUntil.current = Date.now() + 3000;
            }}
          >
            {lrc ? (
              <div className="flex flex-col gap-3">
                {lrc.lines.map((line, i) => (
                  <p
                    key={`${line.timeMs}-${i}`}
                    ref={i === activeIndex ? activeLineRef : undefined}
                    className={`nya-lyric-line cursor-default text-[15px] leading-relaxed transition-all duration-300 ${
                      i === activeIndex
                        ? "scale-[1.03] font-semibold text-primary"
                        : "text-gray-400 hover:text-gray-500"
                    }`}
                    style={{ opacity: i === activeIndex ? 1 : 0.75 }}
                  >
                    {line.text || "· · ·"}
                  </p>
                ))}
              </div>
            ) : (
              <div className="flex h-full items-center justify-center text-[13px] text-gray-400">
                {lrcMissing
                  ? t("未找到歌词（可将同名 .lrc 文件放在音乐同目录）")
                  : t("歌词加载中…")}
              </div>
            )}
          </div>
        </div>
      </div>
    </motion.div>
  );
};

export default NowPlayingView;
