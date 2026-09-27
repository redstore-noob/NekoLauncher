/*
 * 音乐播放控制卡片：与音乐页共享同一播放状态（Go 侧 music.Shared 状态机 +
 * audioBridge <audio> 单例），提供当前曲目展示、进度条与播放/切歌快捷控制。
 * 状态来源：audioBridge 的实时音频快照（进度/播放中）+ music:* 事件（曲目/状态机）。
 */
import type { music } from "../../../wailsjs/go/models";

import React, { useEffect, useState } from "react";
import {
  MusicNote220Regular,
  Pause20Regular,
  Play20Regular,
  Previous20Regular,
  Next20Regular,
  Stop20Regular,
} from "@fluentui/react-icons";
import { Button, Slider } from "@heroui/react";

import {
  GetCurrentTrack,
  NextTrack,
  PausePlayback,
  PlayTrack,
  PreviousTrack,
  ResumePlayback,
  SeekPlayback,
  StopPlayback,
} from "../../../wailsjs/go/bindings/MusicAPI";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { startAudioBridge, useAudioState } from "../../lib/audioBridge";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

type MusicTrack = music.MusicTrack;

function formatTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return "0:00";
  const total = Math.floor(seconds);

  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

/** 文件路径 → 去扩展名的曲目名（与音乐页一致） */
function trackTitle(track: MusicTrack | null): string {
  if (!track) return "";
  const base = track.FilePath.split(/[\\/]/).pop() ?? "";

  return base.replace(/\.[^.]+$/, "");
}

const MusicControlCard: React.FC = () => {
  const audio = useAudioState();
  const [track, setTrack] = useState<MusicTrack | null>(null);
  const [state, setState] = useState("Stopped");
  const [userDragging, setUserDragging] = useState(false);
  const [progressSec, setProgressSec] = useState(0);

  useEffect(() => {
    startAudioBridge();
    GetCurrentTrack()
      .then((t) => setTrack(t ?? null))
      .catch(() => {
        /* 播放器未初始化时忽略 */
      });
    const onTrack = (t: MusicTrack | null) => setTrack(t ?? null);
    const onState = (s: unknown) => setState(String(s));

    // 逐个退订：EventsOff 会连音乐页的订阅一起清掉
    const offTrack = EventsOn("music:trackChanged", onTrack);
    const offState = EventsOn("music:stateChanged", onState);

    return () => {
      offTrack();
      offState();
    };
  }, []);

  // 进度直接读播放桥的实时位置；progressSec 只在拖动期间生效，由 Slider 写入
  const isPlaying = audio.playing;
  const durationSec = audio.durationMs / 1000;
  const positionSec = userDragging ? progressSec : audio.positionMs / 1000;
  const seekable = state !== "Stopped" && durationSec > 0 && audio.canPlay;
  const title =
    track && state !== "Stopped" ? trackTitle(track) : t("未在播放");

  const togglePlayPause = () => {
    if (state === "Playing") {
      PausePlayback();
    } else if (state === "Paused") {
      void ResumePlayback();
    } else {
      GetCurrentTrack()
        .then((t) => {
          if (t) void PlayTrack(t);
        })
        .catch(() => {
          /* ignore */
        });
    }
  };

  return (
    <HomeCard
      icon={<MusicNote220Regular />}
      label={t("音乐控制")}
      live={isPlaying}
      value={title}
    >
      {/* 进度条 + 时间（拖动进度时不触发组件长按拖动） */}
      <div data-widget-interactive className="flex items-center gap-2.5">
        <span className="w-[38px] flex-none text-center font-mono text-[11px] text-gray-400 tabular-nums">
          {formatTime(positionSec)}
        </span>
        <div className="min-w-0 flex-1">
          <Slider
            aria-label={t("播放进度")}
            color="primary"
            isDisabled={!seekable}
            maxValue={durationSec > 0 ? durationSec : 100}
            minValue={0}
            size="sm"
            step={0.1}
            value={Math.min(positionSec, durationSec > 0 ? durationSec : 100)}
            onChange={(v) => {
              setUserDragging(true);
              setProgressSec(Array.isArray(v) ? v[0] : v);
            }}
            onChangeEnd={(v) => {
              setUserDragging(false);
              const sec = Array.isArray(v) ? v[0] : v;

              if (seekable && sec !== positionSec) {
                SeekPlayback(Math.round(sec * 1000)).catch(() => undefined);
              }
            }}
          />
        </div>
        <span className="w-[38px] flex-none text-right font-mono text-[11px] text-gray-400 tabular-nums">
          {durationSec > 0 ? formatTime(durationSec) : "--:--"}
        </span>
      </div>

      {/* 控制按钮 + 状态 */}
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-1.5">
          <Button
            isIconOnly
            aria-label={t("上一首")}
            className="min-w-8 h-8 w-8 active:scale-90"
            radius="full"
            size="sm"
            variant="flat"
            onPress={() => void PreviousTrack().catch(() => undefined)}
          >
            <Previous20Regular className="h-4 w-4" />
          </Button>
          <Button
            isIconOnly
            aria-label={isPlaying ? t("暂停") : t("播放")}
            className="min-w-10 h-10 w-10 shadow-md shadow-primary/25 active:scale-90"
            color="primary"
            radius="full"
            size="sm"
            onPress={togglePlayPause}
          >
            {isPlaying ? (
              <Pause20Regular />
            ) : (
              <span className="inline-flex translate-x-px">
                <Play20Regular />
              </span>
            )}
          </Button>
          <Button
            isIconOnly
            aria-label={t("下一首")}
            className="min-w-8 h-8 w-8 active:scale-90"
            radius="full"
            size="sm"
            variant="flat"
            onPress={() => void NextTrack().catch(() => undefined)}
          >
            <Next20Regular className="h-4 w-4" />
          </Button>
          <Button
            isIconOnly
            aria-label={t("停止")}
            className="min-w-8 h-8 w-8 text-gray-400 active:scale-90"
            isDisabled={state === "Stopped"}
            radius="full"
            size="sm"
            variant="light"
            onPress={() => StopPlayback()}
          >
            <Stop20Regular className="h-4 w-4" />
          </Button>
        </div>

        {/* 播放中的均衡器动画 */}
        <span className="flex items-end justify-center gap-[3px] pb-1 h-[20px]">
          {[0, 1, 2].map((i) => (
            <span
              key={i}
              className={`nya-eq-bar ${isPlaying ? "" : "[animation-play-state:paused] opacity-40"}`}
            />
          ))}
        </span>
      </div>
    </HomeCard>
  );
};

export default MusicControlCard;
