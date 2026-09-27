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
 * 内置资源包模板：整个包的文件骨架（pack.mcmeta / pack.png / 模型 / lang / 贴图），
 * 一键铺开后用户直接在文件树里改。贴图用 canvas 程序化生成，避免仓库里塞二进制。
 */
import type { PackFile } from "./types";

import { t } from "../../../i18n";

import {
  buildPackMetaText,
  createId,
  DEFAULT_PACK_FORMAT,
  makeCanvas,
} from "./types";

interface TemplateEntry {
  path: string;
  text?: string;
  png?: {
    size: number;
    paint: (ctx: CanvasRenderingContext2D, size: number) => void;
  };
}

export interface ResourcePackTemplate {
  id: string;
  name: string;
  description: string;
  /** 入口卡片磁贴渐变（与创作中心同风格） */
  tileClass: string;
  meta: { description: string; packFormat: number };
  entries: TemplateEntry[];
}

/** 确定性伪随机（LCG），保证每次生成的示例贴图一致 */
function makeRandom(seed: number): () => number {
  let state = seed >>> 0;

  return () => {
    state = (state * 1664525 + 1013904223) >>> 0;

    return state / 4294967296;
  };
}

function fillNoise(
  ctx: CanvasRenderingContext2D,
  size: number,
  base: string,
  accents: string[],
  count: number,
  seed: number,
) {
  const random = makeRandom(seed);

  ctx.fillStyle = base;
  ctx.fillRect(0, 0, size, size);
  for (let index = 0; index < count; index += 1) {
    const x = Math.floor(random() * size);
    const y = Math.floor(random() * size);

    ctx.fillStyle = accents[Math.floor(random() * accents.length)];
    ctx.fillRect(x, y, 1, 1);
  }
}

function fillVerticalGradient(
  ctx: CanvasRenderingContext2D,
  size: number,
  top: string,
  bottom: string,
) {
  const gradient = ctx.createLinearGradient(0, 0, 0, size);

  gradient.addColorStop(0, top);
  gradient.addColorStop(1, bottom);
  ctx.fillStyle = gradient;
  ctx.fillRect(0, 0, size, size);
}

/** pack.png：渐变底 + 居中字母，作为资源包列表图标的占位 */
function packIcon(letter: string, top: string, bottom: string) {
  return (ctx: CanvasRenderingContext2D, size: number) => {
    fillVerticalGradient(ctx, size, top, bottom);
    ctx.fillStyle = "rgba(255,255,255,0.92)";
    ctx.font = `bold ${Math.round(size * 0.56)}px "Microsoft YaHei", sans-serif`;
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.fillText(letter, size / 2, size * 0.54);
  };
}

function metaEntry(meta: {
  description: string;
  packFormat: number;
}): TemplateEntry {
  const text = buildPackMetaText({
    description: meta.description,
    packFormat: meta.packFormat,
    supportedFormats:
      meta.packFormat >= 18
        ? { min: Math.max(18, meta.packFormat - 2), max: meta.packFormat }
        : null,
  });

  return { path: "pack.mcmeta", text };
}

const LANG_EXAMPLE_ZH: Record<string, string> = {
  "block.minecraft.stone": t("石头"),
  "block.minecraft.dirt": t("泥土"),
  "block.minecraft.grass_block": t("草方块"),
  "block.minecraft.diamond_ore": t("钻石矿石"),
  "item.minecraft.diamond_sword": t("钻石剑"),
  "item.minecraft.apple": t("苹果"),
};

const LANG_EXAMPLE_EN: Record<string, string> = {
  "block.minecraft.stone": "Stone",
  "block.minecraft.dirt": "Dirt",
  "block.minecraft.grass_block": "Grass Block",
  "block.minecraft.diamond_ore": "Diamond Ore",
  "item.minecraft.diamond_sword": "Diamond Sword",
  "item.minecraft.apple": "Apple",
};

export const RESOURCE_PACK_TEMPLATES: ResourcePackTemplate[] = [
  {
    id: "blank",
    name: t("空白资源包骨架"),
    description: t(
      "pack.mcmeta + pack.png + 示例模型 / lang / 贴图，改起来最省心",
    ),
    tileClass: "from-slate-400 via-slate-500 to-gray-600 shadow-slate-500/30",
    meta: { description: t("我的资源包"), packFormat: DEFAULT_PACK_FORMAT },
    entries: [
      metaEntry({
        description: t("我的资源包"),
        packFormat: DEFAULT_PACK_FORMAT,
      }),
      {
        path: "pack.png",
        png: { size: 128, paint: packIcon("N", "#38bdf8", "#6366f1") },
      },
      {
        path: "assets/minecraft/lang/zh_cn.json",
        text: `${JSON.stringify(LANG_EXAMPLE_ZH, null, 2)}\n`,
      },
      {
        path: "assets/minecraft/lang/en_us.json",
        text: `${JSON.stringify(LANG_EXAMPLE_EN, null, 2)}\n`,
      },
      {
        path: "assets/minecraft/blockstates/example_block.json",
        text: `${JSON.stringify(
          { variants: { "": { model: "minecraft:block/example_block" } } },
          null,
          2,
        )}\n`,
      },
      {
        path: "assets/minecraft/models/block/example_block.json",
        text: `${JSON.stringify(
          {
            parent: "block/cube_all",
            textures: { all: "minecraft:block/example_block" },
          },
          null,
          2,
        )}\n`,
      },
      {
        path: "assets/minecraft/models/item/example_item.json",
        text: `${JSON.stringify(
          {
            parent: "item/generated",
            textures: { layer0: "minecraft:item/example_item" },
          },
          null,
          2,
        )}\n`,
      },
      {
        path: "assets/minecraft/textures/block/example_block.png",
        png: {
          size: 16,
          paint: (ctx, size) =>
            fillNoise(ctx, size, "#7f8c8d", ["#95a5a6", "#6b7778"], 90, 7),
        },
      },
      {
        path: "assets/minecraft/textures/item/example_item.png",
        png: {
          size: 16,
          paint: (ctx, size) =>
            fillNoise(ctx, size, "#f1c40f", ["#f39c12", "#f7dc6f"], 70, 21),
        },
      },
    ],
  },
  {
    id: "blocks",
    name: t("方块贴图替换"),
    description: t(
      "直接覆盖原版石头 / 泥土 / 草顶 / 木板贴图，做出自己的材质风格",
    ),
    tileClass: "from-emerald-400 via-teal-500 to-cyan-500 shadow-teal-500/30",
    meta: { description: t("方块材质包"), packFormat: DEFAULT_PACK_FORMAT },
    entries: [
      metaEntry({
        description: t("方块材质包"),
        packFormat: DEFAULT_PACK_FORMAT,
      }),
      {
        path: "pack.png",
        png: { size: 128, paint: packIcon("B", "#34d399", "#0ea5e9") },
      },
      {
        path: "assets/minecraft/textures/block/stone.png",
        png: {
          size: 16,
          paint: (ctx, size) =>
            fillNoise(ctx, size, "#8a8a8a", ["#7c7c7c", "#999999"], 110, 3),
        },
      },
      {
        path: "assets/minecraft/textures/block/dirt.png",
        png: {
          size: 16,
          paint: (ctx, size) =>
            fillNoise(ctx, size, "#79553a", ["#8a6245", "#6b4a33"], 100, 11),
        },
      },
      {
        path: "assets/minecraft/textures/block/grass_block_top.png",
        png: {
          size: 16,
          paint: (ctx, size) =>
            fillNoise(ctx, size, "#5fa54a", ["#6fbb57", "#4f8f3d"], 120, 17),
        },
      },
      {
        path: "assets/minecraft/textures/block/oak_planks.png",
        png: {
          size: 16,
          paint: (ctx, size) => {
            ctx.fillStyle = "#b8894f";
            ctx.fillRect(0, 0, size, size);
            const random = makeRandom(29);

            ctx.fillStyle = "#a97b45";
            for (let y = 0; y < size; y += 4) {
              ctx.fillRect(0, y, size, 1);
            }
            ctx.fillStyle = "#c69963";
            for (let index = 0; index < 24; index += 1) {
              ctx.fillRect(
                Math.floor(random() * size),
                Math.floor(random() * size),
                2,
                1,
              );
            }
          },
        },
      },
    ],
  },
  {
    id: "glow",
    name: t("发光矿石"),
    description: t(
      "深色石底 + 高亮矿点，让钻石 / 金矿石更醒目（可再叠加发光着色器）",
    ),
    tileClass: "from-amber-400 via-orange-500 to-rose-500 shadow-orange-500/30",
    meta: { description: t("发光矿石"), packFormat: DEFAULT_PACK_FORMAT },
    entries: [
      metaEntry({
        description: t("发光矿石"),
        packFormat: DEFAULT_PACK_FORMAT,
      }),
      {
        path: "pack.png",
        png: { size: 128, paint: packIcon("G", "#f59e0b", "#ef4444") },
      },
      {
        path: "assets/minecraft/textures/block/diamond_ore.png",
        png: {
          size: 16,
          paint: (ctx, size) => {
            fillNoise(ctx, size, "#6e6e6e", ["#5c5c5c", "#7d7d7d"], 120, 5);
            const random = makeRandom(41);

            ctx.fillStyle = "#7df9ff";
            for (let index = 0; index < 14; index += 1) {
              const x = Math.floor(random() * (size - 2)) + 1;
              const y = Math.floor(random() * (size - 2)) + 1;

              ctx.fillRect(x, y, 2, 2);
            }
          },
        },
      },
      {
        path: "assets/minecraft/textures/block/deepslate_diamond_ore.png",
        png: {
          size: 16,
          paint: (ctx, size) => {
            fillNoise(ctx, size, "#4b4b52", ["#3f3f45", "#585862"], 120, 13);
            const random = makeRandom(53);

            ctx.fillStyle = "#5ff0ff";
            for (let index = 0; index < 12; index += 1) {
              const x = Math.floor(random() * (size - 2)) + 1;
              const y = Math.floor(random() * (size - 2)) + 1;

              ctx.fillRect(x, y, 2, 2);
            }
          },
        },
      },
      {
        path: "assets/minecraft/textures/block/gold_ore.png",
        png: {
          size: 16,
          paint: (ctx, size) => {
            fillNoise(ctx, size, "#6e6e6e", ["#5c5c5c", "#7d7d7d"], 120, 5);
            const random = makeRandom(67);

            ctx.fillStyle = "#ffe066";
            for (let index = 0; index < 14; index += 1) {
              const x = Math.floor(random() * (size - 2)) + 1;
              const y = Math.floor(random() * (size - 2)) + 1;

              ctx.fillRect(x, y, 2, 2);
            }
          },
        },
      },
    ],
  },
  {
    id: "lang",
    name: t("语言 / 汉化"),
    description: t("从原版常用词条起步，覆盖方块与物品名称做自己的汉化或梗名"),
    tileClass:
      "from-violet-400 via-purple-500 to-fuchsia-500 shadow-purple-500/30",
    meta: { description: t("自定义汉化"), packFormat: DEFAULT_PACK_FORMAT },
    entries: [
      metaEntry({
        description: t("自定义汉化"),
        packFormat: DEFAULT_PACK_FORMAT,
      }),
      {
        path: "pack.png",
        png: { size: 128, paint: packIcon("L", "#a78bfa", "#d946ef") },
      },
      {
        path: "assets/minecraft/lang/zh_cn.json",
        text: `${JSON.stringify(LANG_EXAMPLE_ZH, null, 2)}\n`,
      },
      {
        path: "assets/minecraft/lang/en_us.json",
        text: `${JSON.stringify(LANG_EXAMPLE_EN, null, 2)}\n`,
      },
    ],
  },
];

/** 把模板铺开成可编辑工程文件（PNG 用 canvas 程序化生成） */
export function buildTemplateFiles(template: ResourcePackTemplate): PackFile[] {
  return template.entries.map((entry) => {
    if (entry.png) {
      const canvas = makeCanvas(entry.png.size, entry.png.size);
      const ctx = canvas.getContext("2d");

      if (ctx) entry.png.paint(ctx, entry.png.size);

      return { id: createId("tpl"), path: entry.path, kind: "png", canvas };
    }

    return {
      id: createId("tpl"),
      path: entry.path,
      kind: "text",
      text: entry.text ?? "",
    };
  });
}
