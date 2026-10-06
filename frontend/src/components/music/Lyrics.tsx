/*
 * 歌词组件与加载 Hook（音乐页右栏 / 全屏播放页共用）。
 * - useLrc：读同目录同名 .lrc 并解析（parseLrc）；
 * - LyricLines：逐行渲染 + 当前行动态高亮 + 自动滚动（用户翻看 3s 内暂停跟随）+
 *   点击歌词行跳转进度（传入 onSeekMs 时启用）。
 */
import type { LrcDoc } from "../../lib/lrc";

import React, { useEffect, useMemo, useRef, useState } from "react";

import { ReadTextFile } from "../../../wailsjs/go/bindings/SystemAPI";
import { activeLrcIndex, parseLrc } from "../../lib/lrc";
import { t } from "../../i18n";

/** 加载指定曲目的 .lrc（换曲即重载；找不到/解析失败时 missing = true）。 */
export function useLrc(path: string): { lrc: LrcDoc | null; missing: boolean } {
  const [lrc, setLrc] = useState<LrcDoc | null>(null);
  const [missing, setMissing] = useState(false);

  useEffect(() => {
    let alive = true;

    setLrc(null);
    setMissing(false);
    if (!path) return;
    const lrcPath = path.replace(/\.[^.\\/]+$/, "") + ".lrc";

    ReadTextFile(lrcPath)
      .then((text) => {
        if (!alive) return;
        const doc = text ? parseLrc(text) : null;

        if (doc) setLrc(doc);
        else setMissing(true);
      })
      .catch(() => {
        if (alive) setMissing(true);
      });

    return () => {
      alive = false;
    };
  }, [path]);

  return { lrc, missing };
}

export const LyricLines: React.FC<{
  lrc: LrcDoc | null;
  missing: boolean;
  positionMs: number;
  onSeekMs?: (ms: number) => void;
  className?: string;
  lineClassName?: string;
}> = ({ lrc, missing, positionMs, onSeekMs, className, lineClassName }) => {
  const activeIndex = useMemo(
    () => (lrc ? activeLrcIndex(lrc.lines, positionMs) : -1),
    [lrc, positionMs],
  );

  // 自动滚动：当前行居中；用户滚轮/触摸翻看 3s 内暂停跟随
  const boxRef = useRef<HTMLDivElement | null>(null);
  const activeLineRef = useRef<HTMLButtonElement | null>(null);
  const followPausedUntil = useRef(0);

  useEffect(() => {
    if (activeIndex < 0) return;
    if (Date.now() < followPausedUntil.current) return;
    const box = boxRef.current;
    const line = activeLineRef.current;

    if (!box || !line) return;
    box.scrollTo({
      top: line.offsetTop - box.clientHeight / 2 + line.clientHeight / 2,
      behavior: "smooth",
    });
  }, [activeIndex, lrc]);

  return (
    <div
      ref={boxRef}
      className={`nya-lyrics-box overflow-y-auto px-4 py-6 ${className ?? ""}`}
      onTouchMove={() => {
        followPausedUntil.current = Date.now() + 3000;
      }}
      onWheel={() => {
        followPausedUntil.current = Date.now() + 3000;
      }}
    >
      {lrc ? (
        <div className="flex flex-col gap-3">
          {lrc.lines.map((line, i) => (
            <button
              key={`${line.timeMs}-${i}`}
              ref={i === activeIndex ? activeLineRef : undefined}
              className={`nya-lyric-line cursor-pointer text-left text-[15px] leading-relaxed transition-all duration-300 ${
                i === activeIndex
                  ? "scale-[1.03] font-semibold text-primary"
                  : "text-gray-400 hover:text-gray-500 dark:hover:text-gray-300"
              } ${lineClassName ?? ""}`}
              style={{ opacity: i === activeIndex ? 1 : 0.75 }}
              type="button"
              onClick={() => onSeekMs?.(line.timeMs)}
            >
              {line.text || "· · ·"}
            </button>
          ))}
        </div>
      ) : (
        <div className="flex h-full items-center justify-center text-center text-[13px] text-gray-400">
          {missing
            ? t("未找到歌词（可将同名 .lrc 文件放在音乐同目录）")
            : t("歌词加载中…")}
        </div>
      )}
    </div>
  );
};
