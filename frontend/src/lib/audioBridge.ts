/*
 * 音乐前端播放桥（audio_bridge.go 的前端对端）。
 * Go 侧 music.Shared 只保留状态机 + 播放列表，实际发声由本模块的 <audio> 单例完成：
 *   - music:play   {filePath}      → 加载并播放该文件
 *   - music:pause / resume / stop  → 暂停 / 恢复 / 停止
 *   - music:seek   {positionMs}    → 跳转
 *   - music:volume {percent}       → 音量
 * 进度经 Music.ReportPlaybackProgress(positionMs, durationMs) ~250ms 节流回传；
 * 自然播完调 Music.NotifyTrackFinished()（触发 Go 侧自动切歌）。
 * 本地文件经 Go 侧 AssetServer 回退路由 /localfile?path=... 流式返回。
 */
import { useEffect, useState } from "react";

import { EventsOn } from "../../wailsjs/runtime/runtime";
import {
  ReportPlaybackProgress,
  NotifyTrackFinished,
  GetCurrentTrack,
} from "../../wailsjs/go/bindings/MusicAPI";
import { t } from "../i18n";

export interface AudioSnapshot {
  filePath: string;
  url: string;
  canPlay: boolean;
  playing: boolean;
  paused: boolean;
  positionMs: number;
  durationMs: number;
  error: string;
}

/** 可变共享状态（非 React state：音频回调高频更新，直接改字段后批量通知）。 */
export const audioState: AudioSnapshot = {
  filePath: "",
  url: "",
  canPlay: false,
  playing: false,
  paused: false,
  positionMs: 0,
  durationMs: 0,
  error: "",
};

type Listener = () => void;
const listeners = new Set<Listener>();

function notify() {
  listeners.forEach((l) => l());
}

/** React hook：订阅 audioState 变化（返回同一可变对象，读取方拿到的总是最新值）。 */
export function useAudioState(): AudioSnapshot {
  const [, setTick] = useState(0);

  useEffect(() => {
    const listener: Listener = () => setTick((t) => t + 1);

    listeners.add(listener);

    return () => {
      listeners.delete(listener);
    };
  }, []);

  return audioState;
}

let audio: HTMLAudioElement | null = null;
let lastReport = 0;

function ensureAudio(): HTMLAudioElement {
  if (audio) return audio;
  audio = new Audio();
  audio.preload = "auto";

  audio.addEventListener("timeupdate", () => {
    audioState.positionMs = Math.round(audio!.currentTime * 1000);
    audioState.durationMs = Number.isFinite(audio!.duration)
      ? Math.round(audio!.duration * 1000)
      : 0;
    if (!lastReport || Date.now() - lastReport >= 250) {
      lastReport = Date.now();
      ReportPlaybackProgress(
        audioState.positionMs,
        audioState.durationMs,
      ).catch(() => {
        /* ignore */
      });
    }
    notify();
  });
  audio.addEventListener("durationchange", () => {
    audioState.durationMs = Number.isFinite(audio!.duration)
      ? Math.round(audio!.duration * 1000)
      : 0;
    notify();
  });
  audio.addEventListener("playing", () => {
    audioState.playing = true;
    audioState.paused = false;
    notify();
  });
  audio.addEventListener("pause", () => {
    audioState.playing = false;
    audioState.paused = true;
    notify();
  });
  audio.addEventListener("ended", () => {
    audioState.playing = false;
    audioState.paused = false;
    audioState.positionMs = 0;
    notify();
    NotifyTrackFinished().catch(() => {
      /* ignore */
    });
  });
  audio.addEventListener("error", () => {
    audioState.canPlay = false;
    audioState.playing = false;
    audioState.error = t("音频加载失败：文件不存在或格式不受支持，暂不可播。");
    notify();
  });

  return audio;
}

/** 本地路径 → 应用内流式播放 URL（Go 侧 /localfile 回退路由）。 */
export function resolveTrackUrl(filePath: string): string {
  if (!filePath) return "";

  return `/localfile?path=${encodeURIComponent(filePath)}`;
}

function play(filePath: string | undefined) {
  const el = ensureAudio();

  audioState.filePath = filePath || "";
  audioState.url = resolveTrackUrl(filePath || "");
  audioState.canPlay = !!audioState.url;
  audioState.error = "";
  audioState.positionMs = 0;
  audioState.durationMs = 0;
  notify();
  if (!audioState.url) return;
  el.src = audioState.url;
  el.play().catch(() => {
    audioState.canPlay = false;
    audioState.error = t("音频加载失败：文件不存在或格式不受支持，暂不可播。");
    notify();
  });
}

let started = false;

/** 订阅 music:* 事件（模块加载即启动一次）。 */
export function startAudioBridge() {
  if (started || typeof window === "undefined") return;
  started = true;
  ensureAudio();

  EventsOn("music:play", (payload: { filePath?: string }) =>
    play(payload?.filePath),
  );
  EventsOn("music:pause", () => {
    audio?.pause();
  });
  EventsOn("music:resume", () => {
    const el = ensureAudio();

    // 前端可能已经不认识当前曲目（页面刷新/开发期热更新后 audio.src 为空），
    // 而 Go 侧状态机仍在"暂停中"：这时直接 play() 只会静默失败，改成向 Go 要
    // 当前曲目重新加载，否则用户会看到"在播放但没声音"。
    if (!el.src) {
      void GetCurrentTrack()
        .then((track) => {
          if (track?.FilePath) play(track.FilePath);
        })
        .catch(() => {
          /* 没有当前曲目：什么都不做 */
        });

      return;
    }
    el.play().catch(() => {
      /* ignore */
    });
  });
  EventsOn("music:stop", () => {
    if (!audio) return;
    audio.pause();
    audio.currentTime = 0;
    audioState.positionMs = 0;
    audioState.playing = false;
    audioState.paused = false;
    notify();
  });
  EventsOn("music:seek", (payload: { positionMs?: number }) => {
    if (audio && Number.isFinite(payload?.positionMs)) {
      audio.currentTime = Math.max(0, (payload?.positionMs ?? 0) / 1000);
      audioState.positionMs = payload?.positionMs ?? 0;
      notify();
    }
  });
  EventsOn("music:volume", (payload: { percent?: number }) => {
    if (audio && Number.isFinite(payload?.percent)) {
      audio.volume = Math.min(1, Math.max(0, (payload?.percent ?? 0) / 100));
    }
  });
}
