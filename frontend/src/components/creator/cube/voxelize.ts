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
 * 3D 体素文字：把文本渲染到离屏 canvas，再按网格采样为"实心格子"，
 * 交给 Three.js 生成方块实例。优点是不依赖字体轮廓文件，任何语言（含中文）
 * 都能直接用系统字体生成。
 *
 * 灵感与功能对齐自 EaseCation/cube-3d-text（MIT）。
 */

export interface VoxelizeOptions {
  /** CSS font-family 值（如 sans-serif / "Microsoft YaHei"） */
  font: string;
  bold: boolean;
  /** 每个方块占用的像素边长：越小越精细、方块越多 */
  resolution: number;
}

export interface VoxelResult {
  /** 实心格子的网格坐标 [gx, gy]（gy 自顶向下） */
  cells: Array<[number, number]>;
  cols: number;
  rows: number;
}

const FONT_SIZE = 64;
const LINE_HEIGHT = 1.18;
const PADDING = 10;
const ALPHA_THRESHOLD = 40;
/** 方块总数上限，超过则自动调粗网格，避免卡顿 */
const MAX_CELLS = 60000;

/** 空结果 */
const EMPTY: VoxelResult = { cells: [], cols: 0, rows: 0 };

export function voxelizeText(
  text: string,
  options: VoxelizeOptions,
): VoxelResult {
  const lines = text.replace(/\r/g, "").split("\n");

  if (lines.every((line) => line.trim() === "")) return EMPTY;
  const canvas = document.createElement("canvas");
  const ctx = canvas.getContext("2d", { willReadFrequently: true });

  if (!ctx) return EMPTY;

  const fontSpec = `${options.bold ? "bold " : ""}${FONT_SIZE}px ${options.font}`;

  ctx.font = fontSpec;

  let maxWidth = 0;

  for (const line of lines) {
    maxWidth = Math.max(maxWidth, Math.ceil(ctx.measureText(line).width));
  }

  canvas.width = Math.max(1, maxWidth + PADDING * 2);
  canvas.height = Math.max(
    1,
    Math.ceil(lines.length * FONT_SIZE * LINE_HEIGHT) + PADDING * 2,
  );
  ctx.font = fontSpec;
  ctx.fillStyle = "#ffffff";
  ctx.textBaseline = "top";
  lines.forEach((line, index) => {
    ctx.fillText(line, PADDING, PADDING + index * FONT_SIZE * LINE_HEIGHT);
  });

  const { data } = ctx.getImageData(0, 0, canvas.width, canvas.height);

  // 计算有效像素包围盒
  let minX = canvas.width;
  let minY = canvas.height;
  let maxX = -1;
  let maxY = -1;

  for (let y = 0; y < canvas.height; y += 1) {
    for (let x = 0; x < canvas.width; x += 1) {
      if (data[(y * canvas.width + x) * 4 + 3] > ALPHA_THRESHOLD) {
        if (x < minX) minX = x;
        if (x > maxX) maxX = x;
        if (y < minY) minY = y;
        if (y > maxY) maxY = y;
      }
    }
  }

  if (maxX < 0) return EMPTY;

  // 网格过密时自动调粗
  let resolution = Math.max(1, Math.round(options.resolution));
  let cols = 0;
  let rows = 0;

  for (;;) {
    cols = Math.ceil((maxX - minX + 1) / resolution);
    rows = Math.ceil((maxY - minY + 1) / resolution);
    if (cols * rows <= MAX_CELLS || resolution >= 64) break;
    resolution += 2;
  }

  const cells: Array<[number, number]> = [];

  for (let gy = 0; gy < rows; gy += 1) {
    for (let gx = 0; gx < cols; gx += 1) {
      const startX = minX + gx * resolution;
      const startY = minY + gy * resolution;
      const endX = Math.min(startX + resolution, maxX + 1);
      const endY = Math.min(startY + resolution, maxY + 1);
      let filled = false;

      for (let y = startY; y < endY && !filled; y += 1) {
        for (let x = startX; x < endX; x += 1) {
          if (data[(y * canvas.width + x) * 4 + 3] > ALPHA_THRESHOLD) {
            filled = true;
            break;
          }
        }
      }
      if (filled) cells.push([gx, gy]);
    }
  }

  return { cells, cols, rows };
}
