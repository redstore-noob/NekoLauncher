import { t } from "../../../i18n"; /*
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
 * 资源包制作器 · 共享类型与纯工具。
 *
 * 工程内每个文件是一个 PackFile：PNG 以隐藏 canvas 为唯一数据源（可绘制），
 * 文本存字符串（可编辑），二进制存 base64（原样透传）。这里只放不依赖 React
 * 的类型、画布helper、pack.mcmeta 解析/生成与文件树构建，便于单测与复用。
 */

export type PackFileKind = "png" | "text" | "binary";

export interface PackFile {
  id: string;
  /** 包内相对路径（正斜杠） */
  path: string;
  kind: PackFileKind;
  /** PNG 数据源（kind === "png"） */
  canvas?: HTMLCanvasElement;
  /** 文本内容（kind === "text"） */
  text?: string;
  /** 二进制内容 base64（kind === "binary"） */
  binaryBase64?: string;
}

/** 单张贴图的撤销 / 重做栈（存 dataURL 快照） */
export interface TextureHistory {
  undo: string[];
  redo: string[];
}

export interface PackMeta {
  description: string;
  packFormat: number;
  supportedFormats: { min: number; max: number } | null;
}

/** MC 版本区间 → 资源包 pack_format（新版优先；覆盖不到的版本手填） */
export const PACK_FORMAT_PRESETS: Array<{ label: string; format: number }> = [
  { label: "1.21.7 – 1.21.8", format: 81 },
  { label: "1.21.6", format: 80 },
  { label: "1.21.5", format: 71 },
  { label: "1.21.4", format: 61 },
  { label: "1.21.2 – 1.21.3", format: 42 },
  { label: "1.21 – 1.21.1", format: 34 },
  { label: "1.20.5 – 1.20.6", format: 32 },
  { label: "1.20.3 – 1.20.4", format: 22 },
  { label: "1.20.2", format: 18 },
  { label: "1.20 – 1.20.1", format: 15 },
  { label: "1.19.4", format: 12 },
  { label: "1.19 – 1.19.2", format: 9 },
  { label: "1.18 – 1.18.2", format: 8 },
  { label: "1.17", format: 7 },
  { label: "1.16.2 – 1.16.5", format: 6 },
  { label: "1.15 – 1.16.1", format: 5 },
  { label: "1.13 – 1.14.4", format: 4 },
  { label: "1.11 – 1.12.2", format: 3 },
];

export const DEFAULT_PACK_FORMAT = 81;

/** 新建 PNG 的候选尺寸 */
export const TEXTURE_SIZE_OPTIONS = [8, 16, 32, 64, 128, 256];

let idCounter = 0;

export function createId(prefix = "f"): string {
  idCounter += 1;

  return `${prefix}-${Date.now().toString(36)}-${idCounter}`;
}

/** 归一化包内路径：反斜杠转正斜杠、去掉开头斜杠与首尾空白 */
export function normalizePath(raw: string): string {
  return raw
    .trim()
    .replace(/\\/g, "/")
    .replace(/^\/+/, "")
    .replace(/\/+/g, "/");
}

/** 小写扩展名（含点），无扩展名返回空串 */
export function fileExtension(path: string): string {
  const name = path.split("/").pop() ?? "";
  const index = name.lastIndexOf(".");

  return index > 0 ? name.slice(index).toLowerCase() : "";
}

export function isJsonPath(path: string): boolean {
  const ext = fileExtension(path);

  return ext === ".json" || ext === ".mcmeta";
}

export function makeCanvas(width: number, height: number): HTMLCanvasElement {
  const canvas = document.createElement("canvas");

  canvas.width = Math.max(1, Math.round(width));
  canvas.height = Math.max(1, Math.round(height));

  return canvas;
}

export function loadImage(source: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();

    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error(t("图片加载失败")));
    image.src = source;
  });
}

/** 用 base64（裸或 data URI）PNG 还原出一张可编辑 canvas */
export async function canvasFromPngBase64(
  encoded: string,
): Promise<HTMLCanvasElement> {
  const source = encoded.startsWith("data:")
    ? encoded
    : `data:image/png;base64,${encoded}`;
  const image = await loadImage(source);
  const canvas = makeCanvas(image.width, image.height);
  const ctx = canvas.getContext("2d");

  if (ctx) ctx.drawImage(image, 0, 0);

  return canvas;
}

/** 从 URL（如同源 /localfile）加载图片为 canvas */
export async function canvasFromUrl(
  source: string,
): Promise<HTMLCanvasElement> {
  const image = await loadImage(source);
  const canvas = makeCanvas(image.width, image.height);
  const ctx = canvas.getContext("2d");

  if (ctx) ctx.drawImage(image, 0, 0);

  return canvas;
}

/** 导出 canvas 为 data URI（后端 decodeResourcePackPng 同时接受裸 base64） */
export function canvasToPngDataUri(canvas: HTMLCanvasElement): string {
  return canvas.toDataURL("image/png");
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;

  return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}

/** 估算工程导出体积（PNG 用尺寸近似，文本 / 二进制按内容长度估算，避免反复编码） */
export function estimateProjectBytes(files: PackFile[]): number {
  let total = 0;

  for (const file of files) {
    if (file.kind === "png" && file.canvas) {
      // 像素画压缩率不固定，用原始像素数的一半做一个稳定的量级估算
      total += Math.max(
        512,
        Math.floor((file.canvas.width * file.canvas.height) / 2),
      );
    } else if (file.kind === "text") {
      total += (file.text?.length ?? 0) * 3;
    } else if (file.kind === "binary") {
      total += Math.floor(((file.binaryBase64?.length ?? 0) * 3) / 4);
    }
  }

  return total;
}

/** hex #rrggbb → [r,g,b] */
export function hexToRgb(hex: string): [number, number, number] {
  const value = hex.replace("#", "");

  return [
    parseInt(value.slice(0, 2), 16) || 0,
    parseInt(value.slice(2, 4), 16) || 0,
    parseInt(value.slice(4, 6), 16) || 0,
  ];
}

export function rgbToHex(r: number, g: number, b: number): string {
  return `#${[r, g, b]
    .map((value) =>
      Math.max(0, Math.min(255, value)).toString(16).padStart(2, "0"),
    )
    .join("")}`;
}

/** 解析 pack.mcmeta 文本（非法 / 缺失时回退默认） */
export function parsePackMeta(text: string | undefined): PackMeta {
  const fallback: PackMeta = {
    description: "",
    packFormat: DEFAULT_PACK_FORMAT,
    supportedFormats: null,
  };

  if (!text) return fallback;
  try {
    const raw = JSON.parse(text) as { pack?: Record<string, unknown> };
    const pack = raw.pack ?? {};
    const format =
      typeof pack.pack_format === "number"
        ? pack.pack_format
        : DEFAULT_PACK_FORMAT;
    let description = "";

    if (typeof pack.description === "string") {
      description = pack.description;
    } else if (pack.description && typeof pack.description === "object") {
      description = JSON.stringify(pack.description);
    }

    let supportedFormats: PackMeta["supportedFormats"] = null;
    const supported = pack.supported_formats;

    if (
      supported &&
      typeof supported === "object" &&
      !Array.isArray(supported)
    ) {
      const min = Number((supported as Record<string, unknown>).min_inclusive);
      const max = Number((supported as Record<string, unknown>).max_inclusive);

      if (Number.isFinite(min) && Number.isFinite(max)) {
        supportedFormats = { min, max };
      }
    } else if (Array.isArray(supported) && supported.length > 0) {
      const numbers = supported.filter(
        (value): value is number => typeof value === "number",
      );

      if (numbers.length > 0) {
        supportedFormats = {
          min: Math.min(...numbers),
          max: Math.max(...numbers),
        };
      }
    }

    return { description, packFormat: format, supportedFormats };
  } catch {
    return fallback;
  }
}

/** 由结构化信息生成 pack.mcmeta 文本 */
export function buildPackMetaText(meta: PackMeta): string {
  const pack: Record<string, unknown> = {
    pack_format: meta.packFormat,
    description: meta.description,
  };

  if (meta.supportedFormats) {
    pack.supported_formats = {
      min_inclusive: meta.supportedFormats.min,
      max_inclusive: meta.supportedFormats.max,
    };
  }

  return `${JSON.stringify({ pack }, null, 2)}\n`;
}

export interface PackTreeNode {
  name: string;
  path: string;
  isFile: boolean;
  children: PackTreeNode[];
  file?: PackFile;
}

/** 把扁平文件列表构建成按目录分组的树（目录在前，名称字典序） */
export function buildFileTree(files: PackFile[]): PackTreeNode[] {
  const root: PackTreeNode = {
    name: "",
    path: "",
    isFile: false,
    children: [],
  };

  for (const file of files) {
    const segments = file.path.split("/").filter(Boolean);
    let node = root;

    segments.forEach((segment, index) => {
      const isFile = index === segments.length - 1;
      const path = segments.slice(0, index + 1).join("/");
      let child = node.children.find(
        (candidate) =>
          candidate.name === segment && candidate.isFile === isFile,
      );

      if (!child) {
        child = { name: segment, path, isFile, children: [] };
        if (isFile) child.file = file;
        node.children.push(child);
      }
      node = child;
    });
  }

  const sortNodes = (nodes: PackTreeNode[]) => {
    nodes.sort((left, right) => {
      if (left.isFile !== right.isFile) return left.isFile ? 1 : -1;

      return left.name.localeCompare(right.name);
    });
    nodes.forEach((entry) => sortNodes(entry.children));
  };

  sortNodes(root.children);

  return root.children;
}
