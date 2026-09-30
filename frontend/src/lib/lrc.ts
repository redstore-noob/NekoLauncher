/*
 * LRC 歌词解析：支持多时间戳行（[mm:ss.xx][mm:ss.xx] 歌词）、增强型逐字标签
 * （<mm:ss.xx>，直接剥离）与常见元数据头（[ti:]/[ar:]/[al:]/[offset:]）。
 * offset 为负表示歌词整体提前（不同播放器约定一致：显示时间 = 标签时间 + offset）。
 */

export interface LrcLine {
  timeMs: number;
  text: string;
}

export interface LrcDoc {
  lines: LrcLine[]; // 按时间升序
  title: string;
  artist: string;
}

const TIME_TAG = /\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]/g;
const WORD_TAG = /<\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?>/g;
const META_TAG = /^\[(ti|ar|al|by|offset):(.*)\]$/i;

/** 解析 LRC 文本；无有效歌词行时返回 null（由调用方回退到"暂无歌词"）。 */
export function parseLrc(text: string): LrcDoc | null {
  const lines: LrcLine[] = [];
  let title = "";
  let artist = "";
  let offsetMs = 0;

  for (const raw of text.split(/\r\n|\n|\r/)) {
    const meta = raw.trim().match(META_TAG);

    if (meta) {
      const value = meta[2].trim();

      if (meta[1].toLowerCase() === "ti") title = value;
      else if (meta[1].toLowerCase() === "ar") artist = value;
      else if (meta[1].toLowerCase() === "offset") {
        const n = Number(value);

        if (Number.isFinite(n)) offsetMs = n;
      }
      continue;
    }

    TIME_TAG.lastIndex = 0;
    const stamps: number[] = [];
    let match: RegExpExecArray | null;
    let lastEnd = 0;

    while ((match = TIME_TAG.exec(raw))) {
      if (match.index !== lastEnd) break; // 时间标签必须连续开头，否则是普通文本
      const min = Number(match[1]);
      const sec = Number(match[2]);
      const fracRaw = match[3] ?? "0";
      // 2 位按百分秒、3 位按毫秒处理
      const frac =
        fracRaw.length >= 3
          ? Number(fracRaw)
          : Number(fracRaw) * 10 ** (3 - fracRaw.length);

      stamps.push(min * 60_000 + sec * 1000 + frac);
      lastEnd = TIME_TAG.lastIndex;
    }
    if (stamps.length === 0) continue;

    const text2 = raw.slice(lastEnd).replace(WORD_TAG, "").trim();

    for (const timeMs of stamps) lines.push({ timeMs, text: text2 });
  }

  if (lines.length === 0) return null;
  // offset 直接并入每行时间，方便后续二分
  if (offsetMs !== 0) {
    for (const line of lines) line.timeMs += offsetMs;
  }
  lines.sort((a, b) => a.timeMs - b.timeMs);

  return { lines, title, artist };
}

/** 二分查找当前播放进度对应的歌词行下标（无行时 -1）。 */
export function activeLrcIndex(lines: LrcLine[], positionMs: number): number {
  let lo = 0;
  let hi = lines.length - 1;
  let result = -1;

  while (lo <= hi) {
    const mid = (lo + hi) >> 1;

    if (lines[mid].timeMs <= positionMs) {
      result = mid;
      lo = mid + 1;
    } else {
      hi = mid - 1;
    }
  }

  return result;
}
