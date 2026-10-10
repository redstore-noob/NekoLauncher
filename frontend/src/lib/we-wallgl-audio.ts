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
 * 把启动器的实时音乐频谱喂给 WebWallGL 的音频可视化。
 *
 * WebWallGL 的 AudioSource 契约是 `snapshot() -> { left, right }`,左右声道
 * 各 64 段 0..1 频谱(与 WE 的 128 值回调一致)。启动器没有系统级音频捕获,
 * 但自己播放的音乐有实时频谱——lib/audioBridge 早就把那条桥挂在 window 上
 * (网页壁纸的 polyfill 就是这么用的)。
 *
 * 这里复用场景渲染器那套 AudioFeed(节流 + 平滑 + 频段能量),把 128 桶对半
 * 拆成左右两个 64 段视图交给库。桥取不到数据(没在放音乐)时返回全 0:
 * 壁纸的音频效果停在静音态,画面照常渲染,不黑屏也不报错。
 */
import { AUDIO_BINS, AudioFeed } from "./we-audio-feed";

/** 单声道段数:WE 的 128 值回调就是左右各 64 */
const CHANNELS = AUDIO_BINS / 2;

export interface WebWallGLAudioSource {
  snapshot(): { left: Float32Array; right: Float32Array };
}

export function createAudioSource(): WebWallGLAudioSource {
  const feed = new AudioFeed();
  const left = new Float32Array(CHANNELS);
  const right = new Float32Array(CHANNELS);

  return {
    snapshot() {
      const frame = feed.sample(performance.now());

      // 128 桶对半:前半给左声道,后半给右声道(与 lib/client.js 的取法一致)
      left.set(frame.spectrum.subarray(0, CHANNELS));
      right.set(frame.spectrum.subarray(CHANNELS, AUDIO_BINS));

      return { left, right };
    },
  };
}
