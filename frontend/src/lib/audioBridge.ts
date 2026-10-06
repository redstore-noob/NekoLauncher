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
  GetMusicVolume,
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
  bufferedMs: number; // 已缓冲到的位置（进度条缓冲段）
  rate: number; // 倍速（audio.playbackRate）
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
  bufferedMs: 0,
  rate: 1,
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

// --- 均衡器（Web Audio BiquadFilter，纯前端实现） ---------------------------
// audio 元素接一个懒创建的 AudioContext + 5 段 peaking 滤波链。
// MediaElementAudioSourceNode 对同一元素只能创建一次，因此关闭 EQ 时把全部
// 增益归零而不是断链（避免重建）。AudioContext 创建失败时静默降级直连输出。
export interface EqBand {
  freq: number;
  label: string;
}

export const EQ_BANDS: EqBand[] = [
  { freq: 60, label: "60Hz" },
  { freq: 230, label: "230Hz" },
  { freq: 910, label: "910Hz" },
  { freq: 3600, label: "3.6k" },
  { freq: 14000, label: "14k" },
];

export const EQ_PRESETS: Record<string, number[]> = {
  Off: [0, 0, 0, 0, 0],
  Pop: [-1, 2, 4, 2, -1],
  Rock: [4, 3, -1, -2, 2],
  Classical: [3, 2, 0, 2, 3],
  Vocal: [-2, -1, 3, 4, 1],
};

let audioCtx: AudioContext | null = null;
let eqNodes: BiquadFilterNode[] = [];
let eqAvailable = false; // Web Audio 链路是否可用（失败则直连输出）

function ensureEqChain(): boolean {
  if (!audio) return false;
  if (eqAvailable) return true;
  try {
    const Ctx =
      window.AudioContext ??
      (window as unknown as { webkitAudioContext?: typeof AudioContext })
        .webkitAudioContext;

    if (!Ctx) return false;
    audioCtx = new Ctx();
    const source = audioCtx.createMediaElementSource(audio);

    eqNodes = EQ_BANDS.map(() => {
      const node = audioCtx!.createBiquadFilter();

      node.type = "peaking";

      return node;
    });
    EQ_BANDS.forEach((band, i) => {
      eqNodes[i].frequency.value = band.freq;
      eqNodes[i].Q.value = 1.1;
      eqNodes[i].gain.value = 0;
    });
    let cur: AudioNode = source;

    for (const node of eqNodes) {
      cur.connect(node);
      cur = node;
    }
    cur.connect(audioCtx.destination);
    eqAvailable = true;

    return true;
  } catch {
    eqAvailable = false;

    return false;
  }
}

/** 设置均衡器增益（dB，长度须等于 EQ_BANDS）；未启用/失败时静默忽略。 */
export function applyEqGains(gains: number[]) {
  if (!ensureEqChain()) return;
  eqNodes.forEach((node, i) => {
    const v = Number.isFinite(gains[i]) ? gains[i] : 0;

    try {
      node.gain.setTargetAtTime(v, audioCtx!.currentTime, 0.05);
    } catch {
      node.gain.value = v;
    }
  });
}

// --- WE 网页壁纸频谱桥 -------------------------------------------------------
// Wallpaper Engine 网页壁纸的音频可视化依赖 wallpaperRegisterAudioListener
// (WE 喂 128 个 0..1 频谱值)。启动器没有系统音频捕获,但自己播放的音乐有
// 实时频谱:EQ 的 Web Audio 图建好后,从链头扇出一个 AnalyserNode 抽头
// (只读、不接 destination,完全不影响原音频路径),经 window 上的只读桥
// 暴露给同源的壁纸 iframe(后端注入的 polyfill 轮询它,见 webwallpaper_polyfill.go)。
// 图未建(EQ 从未启用,刻意不主动建图以防 AudioContext 暂停导致音乐静音)
// 或未在播放时返回 null,壁纸保持初始画面。
let spectrumAnalyser: AnalyserNode | null = null;
let spectrumData: Uint8Array | null = null;

function ensureSpectrumTap(): boolean {
  if (spectrumAnalyser) return true;
  if (!eqAvailable || !audioCtx || eqNodes.length === 0) return false;
  try {
    spectrumAnalyser = audioCtx.createAnalyser();
    spectrumAnalyser.fftSize = 256; // 128 个频率桶,与 WE 的音频回调长度对齐
    spectrumAnalyser.smoothingTimeConstant = 0.7;
    // 从 EQ 链头扇出;Peaking 增益为 0 时全通,抽头读到的是原始频谱
    eqNodes[0].connect(spectrumAnalyser);
    spectrumData = new Uint8Array(spectrumAnalyser.frequencyBinCount);

    return true;
  } catch {
    spectrumAnalyser = null;

    return false;
  }
}

/**
 * 当前音乐频谱(128 个 0..1 浮点);不在播放/图未建时为 null。
 * 注意:抽头(AnalyserNode)新建后有约 0.3-0.5s 的预热期,期间读数为 0——
 * 调用方按帧轮询时无须特殊处理,数据会自动到来(实测确认,勿误判为路由故障)。
 */
export function getWallpaperAudioSpectrum(): number[] | null {
  if (!audio || !audioState.playing || audioState.paused) return null;
  if (!ensureSpectrumTap() || !spectrumAnalyser || !spectrumData) return null;
  spectrumAnalyser.getByteFrequencyData(spectrumData);
  const out = new Array<number>(spectrumData.length);

  for (let i = 0; i < spectrumData.length; i++) out[i] = spectrumData[i] / 255;

  return out;
}

// 挂到 window 供同源 iframe 的 polyfill 访问:只读函数,无状态写入面
if (typeof window !== "undefined") {
  (window as unknown as Record<string, unknown>).__nekolauncherAudioBridge =
    getWallpaperAudioSpectrum;
}

function ensureAudio(): HTMLAudioElement {
  if (audio) return audio;
  audio = new Audio();
  audio.preload = "auto";
  // 注意：不设 crossOrigin——/localfile 是同源相对地址，接入 Web Audio 不会污染；

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
  audio.addEventListener("progress", updateBuffered);
  audio.addEventListener("durationchange", () => {
    updateBuffered();
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

/** 由 TimeRanges 计算缓冲末端（毫秒），无缓冲信息时 0。 */
function updateBuffered() {
  if (!audio) return;
  let end = 0;

  try {
    for (let i = 0; i < audio.buffered.length; i++) {
      if (audio.buffered.start(i) <= audio.currentTime + 0.5) {
        end = Math.max(end, audio.buffered.end(i));
      }
    }
  } catch {
    /* ignore */
  }
  audioState.bufferedMs = Math.round(end * 1000);
}

/** 设置倍速（0.5-2.0）；作用于 audio 元素并同步快照。 */
export function setAudioRate(rate: number) {
  const clamped = Math.min(2, Math.max(0.5, rate || 1));

  audioState.rate = clamped;
  if (audio) {
    try {
      audio.playbackRate = clamped;
    } catch {
      /* ignore */
    }
  }
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
  audioState.bufferedMs = 0;
  notify();
  if (!audioState.url) return;
  el.src = audioState.url;
  try {
    el.playbackRate = audioState.rate; // 换曲保留倍速
  } catch {
    /* ignore */
  }
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

  // 启动即恢复持久化音量（Go 侧仅在 SetMusicVolume 时广播 music:volume）
  GetMusicVolume()
    .then((v) => {
      if (audio && Number.isFinite(v)) {
        audio.volume = Math.min(1, Math.max(0, v / 100));
      }
    })
    .catch(() => {
      /* ignore */
    });

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
    audioState.bufferedMs = 0;
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
