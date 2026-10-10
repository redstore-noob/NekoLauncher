/*
 * Copyright 2026 烟花
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/**
 * 场景壁纸的音频数据源。
 *
 * WE 场景里的效果分两类会读音频:一类是 AUDIOPROCESSING 效果(把频谱塞进
 * shader),一类是音频条(audio bars)。WE 本体从系统混音设备抓 128 桶频谱
 * (左右声道各 64)推给效果;启动器没有系统级音频捕获,但自己播放的音乐有
 * 实时频谱——lib/audioBridge.ts 已经为网页壁纸把频谱经
 * window.__nekolauncherAudioBridge 暴露出来。
 *
 * 本模块把那条桥加工成场景渲染器能直接用的形态:
 *   - 每帧(高帧率下节流到 ~30Hz)采样一次,做一次线性插值平滑,
 *     免得 spectrum 抖成噪点(WE 的音频回调本身也是带时间常数的);
 *   - 同时算出低/中/高频段能量与总和,供只有一两个参数的简单效果用
 *     (WE 的 audio_movement / pulse 这类效果只需要"响度");
 *   - 预分配数组,渲染循环里零分配(每帧 new 一个 128 数组会喂饱 GC)。
 *
 * 取不到数据(没在放音乐 / 浏览器里裸跑预览)时 audibility 回落 0:
 * 效果按"静音"渲染,画面照常显示,不会黑屏也不报错。
 */

/** 频段数量:与 WE 的 128 值回调、audioBridge 的 fftSize=256 对齐 */
export const AUDIO_BINS = 128;

/** 采样节流(ms):约 30Hz。频谱数据本身按 ~43Hz 刷新,再密也读不到新值 */
const SAMPLE_INTERVAL_MS = 33;

/** 平滑系数:0.35 ≈ 新值占三成半;再高会抖,再低音频条会糊成一团 */
const SMOOTHING = 0.35;

/** 低/中/高频段划分(桶下标):音乐的低音鼓点、人声、齿音大致落在这里 */
const BAND_LOW_END = 8;
const BAND_MID_END = 48;

export interface AudioFrame {
  /** 128 个 0..1 的平滑频谱(与 WE 回调同长度) */
  readonly spectrum: Float32Array;
  /** 频谱均值(0..1):"现在有多响",给只需要一个标量的效果用 */
  readonly level: number;
  /** 低频段均值(0..1):鼓点 */
  readonly low: number;
  /** 中频段均值(0..1):人声/主旋律 */
  readonly mid: number;
  /** 高频段均值(0..1):齿音/镲 */
  readonly high: number;
  /** 是否真的取到了数据(false = 静音态,调用方按 0 处理即可) */
  readonly audibility: number;
}

/** 桥函数签名(见 audioBridge.ts 挂在 window 上的只读函数) */
type SpectrumBridge = () => number[] | null | undefined;

function readBridge(): SpectrumBridge | null {
  if (typeof window === "undefined") return null;
  const bridge = (window as unknown as { __nekolauncherAudioBridge?: unknown })
    .__nekolauncherAudioBridge;

  return typeof bridge === "function" ? (bridge as SpectrumBridge) : null;
}

/**
 * 取一次频谱。桥(由 audioBridge 挂到 window)读的是 Web Audio 的实时数据,
 * 理论上不会抛,但它是跨模块的松散契约:真抛了也只当静音,绝不能把渲染
 * 循环带崩(壁纸会整块黑掉)。
 */
function safeReadBridge(): number[] | null {
  try {
    return readBridge()?.() ?? null;
  } catch {
    return null;
  }
}

export class AudioFeed {
  private readonly spectrum = new Float32Array(AUDIO_BINS);
  private lastSampleAt = 0;
  private primed = false;
  /** 每实例一个帧对象:频谱数组就是实例自己的那一条(零分配) */
  private frame: AudioFrame = {
    spectrum: this.spectrum,
    level: 0,
    low: 0,
    mid: 0,
    high: 0,
    audibility: 0,
  };

  /**
   * 取当前音频帧。nowMs 用 performance.now()(渲染循环里已有)。
   * 返回的是同一个对象/数组引用,调用方按帧读取即可,不要存起来跨帧比。
   */
  sample(nowMs: number): AudioFrame {
    // 首次调用必采一次,之后按 ~30Hz 节流
    if (this.primed && nowMs - this.lastSampleAt < SAMPLE_INTERVAL_MS) {
      return this.frame;
    }
    this.primed = true;
    this.lastSampleAt = nowMs;

    const raw = safeReadBridge();

    if (!raw || raw.length === 0) {
      // 静音:把平滑值往 0 收(而不是硬切),音乐暂停时画面不会闪跳
      this.decay();

      return this.frame;
    }
    const count = Math.min(AUDIO_BINS, raw.length);

    for (let i = 0; i < AUDIO_BINS; i++) {
      const target = i < count ? clamp01(raw[i]) : 0;

      this.spectrum[i] += (target - this.spectrum[i]) * SMOOTHING;
    }
    const low = bandMean(this.spectrum, 0, BAND_LOW_END);
    const mid = bandMean(this.spectrum, BAND_LOW_END, BAND_MID_END);
    const high = bandMean(this.spectrum, BAND_MID_END, AUDIO_BINS);
    // 整体响度:直接取全桶均值,低频能量大所以听感上更贴近实际音量
    const level = bandMean(this.spectrum, 0, AUDIO_BINS);

    return Object.assign(this.frame, {
      level,
      low,
      mid,
      high,
      audibility: 1,
    });
  }

  /** 静音态:频谱与能量按平滑衰减到 0,采样节流照旧 */
  private decay(): void {
    const decay = 1 - SMOOTHING;

    for (let i = 0; i < AUDIO_BINS; i++) this.spectrum[i] *= decay;
    Object.assign(this.frame, {
      level: bandMean(this.spectrum, 0, AUDIO_BINS),
      low: bandMean(this.spectrum, 0, BAND_LOW_END),
      mid: bandMean(this.spectrum, BAND_LOW_END, BAND_MID_END),
      high: bandMean(this.spectrum, BAND_MID_END, AUDIO_BINS),
      audibility: 0,
    });
  }
}

function bandMean(data: Float32Array, from: number, to: number): number {
  if (to <= from) return 0;
  let sum = 0;

  for (let i = from; i < to; i++) sum += data[i];

  return sum / (to - from);
}

function clamp01(value: number): number {
  if (!Number.isFinite(value)) return 0;

  return value < 0 ? 0 : value > 1 ? 1 : value;
}
