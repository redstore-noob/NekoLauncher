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
 * 场景壁纸(WebWallGL)音频源的单元测试。
 *
 * 这里不碰 WebGL/DOM 渲染,只验证"从桥取频谱 → 平滑 → 分频段能量"这条纯计算
 * 链路:它是壁纸音频可视化能不能动起来的根因,也是最容易写出抖动/卡死 bug 的地方。
 */
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { AUDIO_BINS, AudioFeed } from "./we-audio-feed";

type AudioWindow = { __nekolauncherAudioBridge?: () => number[] | null };

function setBridge(bridge: AudioWindow["__nekolauncherAudioBridge"]) {
  (window as unknown as AudioWindow).__nekolauncherAudioBridge = bridge;
}

/** 造一条"低频有能量、高频为空"的频谱(接近真实鼓点) */
function lowEndSpectrum(value: number, lowBins = 8): number[] {
  return Array.from({ length: AUDIO_BINS }, (_, i) =>
    i < lowBins ? value : 0,
  );
}

describe("AudioFeed", () => {
  beforeEach(() => {
    setBridge(undefined);
  });

  afterEach(() => {
    setBridge(undefined);
  });

  it("桥不可用时保持静音态(全 0,audibility = 0)", () => {
    const feed = new AudioFeed();
    const frame = feed.sample(0);

    expect(frame.audibility).toBe(0);
    expect(frame.level).toBe(0);
    expect(frame.spectrum.length).toBe(AUDIO_BINS);
    expect(Array.from(frame.spectrum).every((v) => v === 0)).toBe(true);
  });

  it("按 ~30Hz 节流:间隔未到时不重复取桥数据", () => {
    let calls = 0;

    setBridge(() => {
      calls += 1;

      return lowEndSpectrum(1);
    });

    const feed = new AudioFeed();

    feed.sample(0);
    feed.sample(10); // 距上次 10ms,应被节流
    feed.sample(20);
    expect(calls).toBe(1);
    feed.sample(40); // 超过 33ms,重新采样
    expect(calls).toBe(2);
  });

  it("频谱做平滑:首帧只走到平滑系数那么多,多帧后逼近真值", () => {
    setBridge(() => lowEndSpectrum(1));
    const feed = new AudioFeed();
    const first = feed.sample(0).spectrum[0];

    expect(first).toBeGreaterThan(0);
    expect(first).toBeLessThan(1);
    // 连续采样若干帧(间隔 40ms 绕过节流)
    for (let i = 1; i <= 30; i++) feed.sample(i * 40);
    expect(feed.sample(31 * 40).spectrum[0]).toBeGreaterThan(0.95);
  });

  it("分频段能量:低频信号的 low 明显高于 high", () => {
    setBridge(() => lowEndSpectrum(1));
    const feed = new AudioFeed();

    // 多帧平滑后读数稳定
    for (let i = 0; i <= 40; i++) feed.sample(i * 40);
    const frame = feed.sample(41 * 40);

    expect(frame.audibility).toBe(1);
    expect(frame.low).toBeGreaterThan(frame.high);
    expect(frame.high).toBe(0);
    // level 是 128 桶均值:8/128 的低频能量 ≈ 0.06 量级
    expect(frame.level).toBeGreaterThan(0);
    expect(frame.level).toBeLessThan(frame.low);
  });

  it("音乐停止(桥返回 null)时平滑衰减回静音,而不是硬切", () => {
    setBridge(() => lowEndSpectrum(1));
    const feed = new AudioFeed();

    for (let i = 0; i <= 40; i++) feed.sample(i * 40);
    expect(feed.sample(41 * 40).level).toBeGreaterThan(0);

    setBridge(() => null);
    const firstSilent = feed.sample(42 * 40);

    // 第一帧仍是衰减中的非零值,audibility 已归零
    expect(firstSilent.audibility).toBe(0);
    expect(firstSilent.spectrum[0]).toBeGreaterThan(0);
    expect(firstSilent.spectrum[0]).toBeLessThan(1);
    for (let i = 43; i <= 90; i++) feed.sample(i * 40);
    expect(feed.sample(91 * 40).spectrum[0]).toBeLessThan(0.01);
  });

  it("桥返回超长/超短数组都能用:多出的桶丢弃、缺少的桶按 0", () => {
    setBridge(() => [1, 1, 1]);
    const feed = new AudioFeed();

    for (let i = 0; i <= 40; i++) feed.sample(i * 40);
    const shortFrame = feed.sample(41 * 40);

    expect(shortFrame.spectrum.length).toBe(AUDIO_BINS);
    expect(shortFrame.spectrum[3]).toBe(0);
    expect(
      Array.from(shortFrame.spectrum)
        .slice(0, 3)
        .every((value: number) => value > 0),
    ).toBe(true);

    setBridge(() => new Array(AUDIO_BINS * 2).fill(1));
    const feed2 = new AudioFeed();

    for (let i = 0; i <= 40; i++) feed2.sample(i * 40);
    expect(feed2.sample(41 * 40).spectrum.length).toBe(AUDIO_BINS);
  });

  it("桥抛异常时不影响渲染循环(视为静音)", () => {
    setBridge(() => {
      throw new Error("bridge boom");
    });
    const feed = new AudioFeed();

    const frame = feed.sample(0);

    expect(frame.audibility).toBe(0);
    expect(frame.level).toBe(0);
  });

  it("越界/非法数值被夹到 0..1", () => {
    setBridge(() => [-5, 2, Number.NaN, 0.5]);
    const feed = new AudioFeed();

    for (let i = 0; i <= 40; i++) feed.sample(i * 40);
    const frame = feed.sample(41 * 40);

    expect(frame.spectrum[0]).toBe(0);
    expect(frame.spectrum[1]).toBeGreaterThan(0.95);
    expect(frame.spectrum[2]).toBe(0);
    expect(frame.spectrum[3]).toBeGreaterThan(0.4);
  });
});
