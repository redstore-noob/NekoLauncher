/*
 * 莫奈（Monet）取色：从当前启动器背景图里挑出一个"能当主题色"的颜色。
 *
 * 思路对齐 Material You 的 dynamic color：不是简单取平均色或出现最多的颜色，
 * 而是
 *   1. 丢弃近灰、过暗、过亮的像素（它们铺满画面却没有色相信息）；
 *   2. 按色相分桶投票，票权用色度（chroma）加权，并轻微偏好中间调——
 *      这样画面里一小块鲜艳的晚霞也能压过一大片灰蓝天空；
 *   3. 相邻桶一起合并，再取圆均值（色相是环形的，直接算术平均会把
 *      350° 与 10° 算成 180°）；
 *   4. 最后把彩度/明度规整到"当主色好看"的区间，否则高饱和大红图会得到
 *      刺眼的纯红、雾蒙蒙的图会得到几乎看不见的灰。
 *
 * 取不到彩色（纯黑白壁纸）时返回 null，调用方保留用户原来的主题色。
 */

/** 采样边长：够统计色相分布，又快到可以在背景换图时随时重跑。 */
const SAMPLE_SIZE = 64;

/** 色度下限（0-255）：低于它视为灰阶，不参与投票。 */
const MIN_CHROMA = 22;
/** 亮度上下限（0-255）：死黑与死白同样没有色相参考价值。 */
const MIN_LUMA = 28;
const MAX_LUMA = 232;
/** 色相分桶数（每桶 15°）。 */
const HUE_BUCKETS = 24;
/** 有效像素占比低于它就不硬凑颜色。 */
const MIN_CONFIDENCE = 0.02;

export interface MonetColor {
  /** #rrggbb */
  hex: string;
  /** 参与投票的像素占比（0-1），可用于判断这次取色有多可靠 */
  confidence: number;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function rgbToHsl(
  r: number,
  g: number,
  b: number,
): { h: number; s: number; l: number } {
  const rn = r / 255;
  const gn = g / 255;
  const bn = b / 255;
  const max = Math.max(rn, gn, bn);
  const min = Math.min(rn, gn, bn);
  const l = (max + min) / 2;
  let h = 0;
  let s = 0;

  if (max !== min) {
    const d = max - min;

    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    if (max === rn) h = (gn - bn) / d + (gn < bn ? 6 : 0);
    else if (max === gn) h = (bn - rn) / d + 2;
    else h = (rn - gn) / d + 4;
    h /= 6;
  }

  return { h: h * 360, s, l };
}

/** HSL（h: 0-360，s/l: 0-1）→ #rrggbb */
export function hslToHex(h: number, s: number, l: number): string {
  const c = (1 - Math.abs(2 * l - 1)) * s;
  const hp = (((h % 360) + 360) % 360) / 60;
  const x = c * (1 - Math.abs((hp % 2) - 1));
  let rgb: [number, number, number];

  if (hp < 1) rgb = [c, x, 0];
  else if (hp < 2) rgb = [x, c, 0];
  else if (hp < 3) rgb = [0, c, x];
  else if (hp < 4) rgb = [0, x, c];
  else if (hp < 5) rgb = [x, 0, c];
  else rgb = [c, 0, x];
  const m = l - c / 2;

  return (
    "#" +
    rgb
      .map((v) =>
        Math.round(clamp(v + m, 0, 1) * 255)
          .toString(16)
          .padStart(2, "0"),
      )
      .join("")
  );
}

/** 逐像素投票，挑出最能代表画面"色彩性格"的色相。 */
function pickFromPixels(data: Uint8ClampedArray): MonetColor | null {
  const weights = new Float64Array(HUE_BUCKETS);
  const sinSum = new Float64Array(HUE_BUCKETS);
  const cosSum = new Float64Array(HUE_BUCKETS);
  const satSum = new Float64Array(HUE_BUCKETS);
  const lightSum = new Float64Array(HUE_BUCKETS);
  let total = 0;
  let valid = 0;

  for (let i = 0; i + 3 < data.length; i += 4) {
    total += 1;
    const r = data[i];
    const g = data[i + 1];
    const b = data[i + 2];
    const chroma = Math.max(r, g, b) - Math.min(r, g, b);
    const luma = 0.2126 * r + 0.7152 * g + 0.0722 * b;

    if (chroma < MIN_CHROMA || luma < MIN_LUMA || luma > MAX_LUMA) continue;

    const { h, s, l } = rgbToHsl(r, g, b);
    // 票权 = 色度 × 中间调偏好：鲜艳的、不太暗也不太亮的像素更"像主题色"
    const weight = chroma * (1 - Math.abs(l - 0.5) * 0.6);
    const bucket = Math.round((h / 360) * HUE_BUCKETS) % HUE_BUCKETS;
    const rad = (h * Math.PI) / 180;

    weights[bucket] += weight;
    sinSum[bucket] += Math.sin(rad) * weight;
    cosSum[bucket] += Math.cos(rad) * weight;
    satSum[bucket] += s * weight;
    lightSum[bucket] += l * weight;
    valid += 1;
  }

  if (total === 0 || valid / total < MIN_CONFIDENCE) return null;

  let best = 0;

  for (let i = 1; i < HUE_BUCKETS; i += 1) {
    if (weights[i] > weights[best]) best = i;
  }
  // 合并左右邻桶：色相是连续的，边界上的颜色会被切到相邻两桶里
  let weight = 0;
  let sin = 0;
  let cos = 0;
  let sat = 0;
  let light = 0;

  for (const offset of [-1, 0, 1]) {
    const idx = (best + offset + HUE_BUCKETS) % HUE_BUCKETS;

    weight += weights[idx];
    sin += sinSum[idx];
    cos += cosSum[idx];
    sat += satSum[idx];
    light += lightSum[idx];
  }
  if (weight <= 0) return null;

  const hue = ((Math.atan2(sin, cos) * 180) / Math.PI + 360) % 360;
  const sourceSat = sat / weight;
  const sourceLight = light / weight;
  // 规整到"当主色好看"的区间：保留色相，压掉过饱和与过暗/过亮
  const finalSat = clamp(0.1 + sourceSat * 0.85, 0.3, 0.85);
  const finalLight = clamp(0.34 + sourceLight * 0.3, 0.38, 0.6);

  return {
    hex: hslToHex(hue, finalSat, finalLight),
    confidence: valid / total,
  };
}

function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();

    // 同源（/localfile、/wwwallpaper）可直接读像素；必应的第三方兜底地址
    // 若不带 CORS 头，这里会加载失败——只影响取色，不影响背景本身
    image.crossOrigin = "anonymous";
    image.decoding = "async";
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("background image load failed"));
    image.src = url;
  });
}

/**
 * 从背景图 URL 提取主题色；取不到彩色（加载失败、跨域受限、纯灰阶）时返回 null。
 */
export async function extractThemeColor(
  url: string,
): Promise<MonetColor | null> {
  if (!url || typeof document === "undefined") return null;
  try {
    const image = await loadImage(url);

    if (!image.naturalWidth || !image.naturalHeight) return null;
    const canvas = document.createElement("canvas");

    canvas.width = SAMPLE_SIZE;
    canvas.height = SAMPLE_SIZE;
    const ctx = canvas.getContext("2d", { willReadFrequently: true });

    if (!ctx) return null;
    // 按 cover 缩放铺满采样画布：留白会把黑边当成"很暗的颜色"投进统计
    const scale = Math.max(
      SAMPLE_SIZE / image.naturalWidth,
      SAMPLE_SIZE / image.naturalHeight,
    );
    const width = image.naturalWidth * scale;
    const height = image.naturalHeight * scale;

    ctx.drawImage(
      image,
      (SAMPLE_SIZE - width) / 2,
      (SAMPLE_SIZE - height) / 2,
      width,
      height,
    );

    return pickFromPixels(
      ctx.getImageData(0, 0, SAMPLE_SIZE, SAMPLE_SIZE).data,
    );
  } catch {
    // 跨域图片读像素会抛 SecurityError，加载失败会 reject：一律当作取不到
    return null;
  }
}
