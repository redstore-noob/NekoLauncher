import { t } from "../../i18n"; /*
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
 * Minecraft 渐变彩色字生成纯逻辑：颜色解析、多点渐变插值、逐字着色，
 * 以及输出为各种格式（&#RRGGBB / §x§R… / &x&R… / MiniMessage）。
 * 不涉及 React，便于复用与测试。
 */

export interface GradientStop {
  id: string;
  color: string;
}

export type OutputFormat = "legacy" | "section" | "amp" | "minimessage";

export interface TextFormats {
  bold: boolean;
  italic: boolean;
  underline: boolean;
  strikethrough: boolean;
  obfuscated: boolean;
}

export interface GradientBuildOptions {
  text: string;
  stops: string[];
  format: OutputFormat;
  controlChar: "&" | "§";
  formats: TextFormats;
}

export const EMPTY_FORMATS: TextFormats = {
  bold: false,
  italic: false,
  underline: false,
  strikethrough: false,
  obfuscated: false,
};

export const OUTPUT_FORMAT_OPTIONS: Array<{
  value: OutputFormat;
  label: string;
  hint: string;
}> = [
  { value: "legacy", label: t("原版 · &#RRGGBB"), hint: t("1.16+ / 插件常用") },
  {
    value: "section",
    label: t("标准十六进制 · §x§R§R…"),
    hint: t("Bungee / 原版标准写法"),
  },
  {
    value: "amp",
    label: t("& 变体 · &x&R&R…"),
    hint: t("插件配置里用 & 的写法"),
  },
  {
    value: "minimessage",
    label: "MiniMessage · <#RRGGBB>",
    hint: "Paper / Velocity",
  },
];

/** 单色规范化：支持 #RGB / #RRGGBB / RRGGBB，失败返回 null。 */
export function normalizeHex(value: string): string | null {
  const raw = value.trim().replace(/^#/, "");

  if (/^[0-9a-fA-F]{6}$/.test(raw)) return `#${raw.toUpperCase()}`;
  if (/^[0-9a-fA-F]{3}$/.test(raw)) {
    const [r, g, b] = raw.split("");

    return `#${r}${r}${g}${g}${b}${b}`.toUpperCase();
  }

  return null;
}

function hexToRgb(hex: string): [number, number, number] {
  const raw = hex.replace("#", "");

  return [
    parseInt(raw.slice(0, 2), 16),
    parseInt(raw.slice(2, 4), 16),
    parseInt(raw.slice(4, 6), 16),
  ];
}

function rgbToHex(r: number, g: number, b: number): string {
  return `#${[r, g, b]
    .map((value) =>
      Math.max(0, Math.min(255, Math.round(value)))
        .toString(16)
        .padStart(2, "0"),
    )
    .join("")
    .toUpperCase()}`;
}

/**
 * 从任意文本解析颜色：支持 #RRGGBB / #RGB / RRGGBB、rgb()/rgba() 以及
 * 直接粘贴的 CSS 渐变（linear-gradient(#a, #b)）。按出现顺序去重返回。
 */
export function parseColors(input: string): string[] {
  const found: string[] = [];
  const push = (hex: string | null) => {
    if (hex && !found.includes(hex)) found.push(hex);
  };

  const rgbRegex = /rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})/gi;
  let match: RegExpExecArray | null;

  while ((match = rgbRegex.exec(input)) !== null) {
    push(rgbToHex(Number(match[1]), Number(match[2]), Number(match[3])));
  }

  const cleaned = input.replace(rgbRegex, " ");
  const hexRegex = /#?([0-9a-fA-F]{6}|[0-9a-fA-F]{3})\b/g;

  while ((match = hexRegex.exec(cleaned)) !== null) {
    const raw = match[1];

    push(normalizeHex(raw));
  }

  return found;
}

/** 在多点渐变上取 t∈[0,1] 处的颜色。 */
export function sampleGradient(stops: string[], t: number): string {
  if (stops.length === 0) return "#FFFFFF";
  if (stops.length === 1) return stops[0];
  const scaled = Math.max(0, Math.min(1, t)) * (stops.length - 1);
  const index = Math.min(stops.length - 2, Math.floor(scaled));
  const fraction = scaled - index;
  const from = hexToRgb(stops[index]);
  const to = hexToRgb(stops[index + 1]);

  return rgbToHex(
    from[0] + (to[0] - from[0]) * fraction,
    from[1] + (to[1] - from[1]) * fraction,
    from[2] + (to[2] - from[2]) * fraction,
  );
}

/**
 * 逐字着色：空白字符不参与渐变推进（不消耗颜色位置），返回与文本等长的
 * 颜色数组，空白处为 null。
 */
export function computeCharColors(
  text: string,
  stops: string[],
): Array<string | null> {
  const colorable: number[] = [];

  for (let index = 0; index < text.length; index += 1) {
    if (!/\s/.test(text[index])) colorable.push(index);
  }
  const result: Array<string | null> = new Array(text.length).fill(null);
  const total = colorable.length;

  colorable.forEach((charIndex, order) => {
    const t = total <= 1 ? 0 : order / (total - 1);

    result[charIndex] = sampleGradient(stops, t);
  });

  return result;
}

function colorCode(
  format: OutputFormat,
  hex: string,
  controlChar: "&" | "§",
): string {
  const raw = hex.replace("#", "").toUpperCase();

  switch (format) {
    case "section":
      return `§x${raw
        .split("")
        .map((char) => `§${char}`)
        .join("")}`;
    case "amp":
      return `&x${raw
        .split("")
        .map((char) => `&${char}`)
        .join("")}`;
    case "minimessage":
      return `<#${raw}>`;
    default:
      return `${controlChar}#${raw}`;
  }
}

function formatCodes(
  format: OutputFormat,
  controlChar: "&" | "§",
  formats: TextFormats,
): string {
  if (format === "minimessage") {
    let out = "";

    if (formats.obfuscated) out += "<obfuscated>";
    if (formats.bold) out += "<bold>";
    if (formats.strikethrough) out += "<strikethrough>";
    if (formats.underline) out += "<underlined>";
    if (formats.italic) out += "<italic>";

    return out;
  }

  const char = format === "section" ? "§" : controlChar;
  let out = "";

  if (formats.obfuscated) out += `${char}k`;
  if (formats.bold) out += `${char}l`;
  if (formats.strikethrough) out += `${char}m`;
  if (formats.underline) out += `${char}n`;
  if (formats.italic) out += `${char}o`;

  return out;
}

/** 生成最终输出字符串；每个有色字符前附带颜色码 + 样式码（颜色会重置样式）。 */
export function buildGradientOutput(options: GradientBuildOptions): string {
  const { text, stops, format, controlChar, formats } = options;
  const colors = computeCharColors(text, stops);
  const styles = formatCodes(format, controlChar, formats);
  let output = "";

  for (let index = 0; index < text.length; index += 1) {
    const color = colors[index];

    if (color) output += colorCode(format, color, controlChar) + styles;
    output += text[index];
  }

  return output;
}

/** 渐变预览用 CSS linear-gradient */
export function cssGradient(stops: string[]): string {
  const colors = stops.length > 0 ? stops : ["#FFFFFF"];

  return `linear-gradient(90deg, ${colors.join(", ")})`;
}
