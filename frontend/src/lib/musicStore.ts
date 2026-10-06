/*
 * 音乐用户数据持久化（收藏 / 最近播放 / 上次播放曲目 / 倍速 / 均衡器）。
 * 复用 ConfigAPI.GetValue/SetValue 的键值机制（与 Go 侧 musicFolder 等键同一存储），
 * 全部纯前端实现，不新增 Go 绑定。所有键以 "music" 前缀隔离，损坏的 JSON 静默回落默认值。
 */
import type { music } from "../../wailsjs/go/models";

import { GetValue, SetValue } from "../../wailsjs/go/bindings/ConfigAPI";

type MusicTrack = music.MusicTrack;

const FAVORITES_KEY = "musicFavorites"; // string[]（FilePath）
const RECENT_KEY = "musicRecent"; // {p: FilePath, t: timestampMs}[]
const LAST_KEY = "musicLastTrack"; // FilePath 字符串
const RATE_KEY = "musicRate"; // "1.25"
const EQ_PRESET_KEY = "musicEqPreset"; // 预设名（Off/Pop/Rock/Classical/Vocal/Custom）
const EQ_GAINS_KEY = "musicEqGains"; // number[]（dB）

const RECENT_LIMIT = 100;

async function readJson<T>(key: string, fallback: T): Promise<T> {
  try {
    const raw = await GetValue(key);

    if (!raw) return fallback;

    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

async function writeJson(key: string, value: unknown): Promise<void> {
  try {
    await SetValue(key, JSON.stringify(value));
  } catch {
    /* 持久化失败不影响功能 */
  }
}

// --- 收藏 -------------------------------------------------------------------

export async function loadFavorites(): Promise<string[]> {
  return readJson<string[]>(FAVORITES_KEY, []);
}

export async function saveFavorites(paths: string[]): Promise<void> {
  return writeJson(FAVORITES_KEY, paths);
}

// --- 最近播放 ----------------------------------------------------------------

export interface RecentEntry {
  path: string;
  ts: number;
}

export async function loadRecent(): Promise<RecentEntry[]> {
  const list = await readJson<RecentEntry[]>(RECENT_KEY, []);

  return Array.isArray(list)
    ? list.filter((e) => e && typeof e.path === "string")
    : [];
}

/** 记录一次播放：置顶去重，超限裁剪。 */
export async function pushRecent(path: string): Promise<RecentEntry[]> {
  const list = (await loadRecent()).filter((e) => e.path !== path);

  list.unshift({ path, ts: Date.now() });
  const next = list.slice(0, RECENT_LIMIT);

  await writeJson(RECENT_KEY, next);

  return next;
}

export async function saveRecent(list: RecentEntry[]): Promise<void> {
  return writeJson(RECENT_KEY, list);
}

// --- 上次播放曲目 ------------------------------------------------------------

export async function loadLastTrackPath(): Promise<string> {
  try {
    return (await GetValue(LAST_KEY)) || "";
  } catch {
    return "";
  }
}

export async function saveLastTrackPath(path: string): Promise<void> {
  try {
    await SetValue(LAST_KEY, path);
  } catch {
    /* ignore */
  }
}

// --- 倍速 -------------------------------------------------------------------

export async function loadRate(): Promise<number> {
  const raw = await readJson<number | string>(RATE_KEY, 1);
  const n = Number(raw);

  return Number.isFinite(n) ? Math.min(2, Math.max(0.5, n)) : 1;
}

export async function saveRate(rate: number): Promise<void> {
  return writeJson(RATE_KEY, rate);
}

// --- 均衡器 -----------------------------------------------------------------

export async function loadEq(): Promise<{
  preset: string;
  gains: number[];
}> {
  const preset = await readJson<string>(EQ_PRESET_KEY, "Off");
  const gains = await readJson<number[]>(EQ_GAINS_KEY, [0, 0, 0, 0, 0]);

  return {
    preset: typeof preset === "string" ? preset : "Off",
    gains: Array.isArray(gains) && gains.length === 5 ? gains : [0, 0, 0, 0, 0],
  };
}

export async function saveEq(preset: string, gains: number[]): Promise<void> {
  await writeJson(EQ_PRESET_KEY, preset);
  await writeJson(EQ_GAINS_KEY, gains);
}

/** 按文件路径在曲目数组中查找（曲目唯一键 = FilePath）。 */
export function findTrack(
  tracks: MusicTrack[],
  path: string,
): MusicTrack | null {
  return tracks.find((t) => t.FilePath === path) ?? null;
}
