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
 * 原版风格贴图种子：新建 / 资源库创建 PNG 时不再给空白画布，
 * 而是按文件名（贴图 id）程序化铺一层接近原版观感的底图
 * （石头噪点、木板横纹、矿石斑点、物品剪影……），用户直接在上面改。
 * 全部用 canvas 绘制，避免仓库里塞原版二进制素材。
 */
import { makeCanvas } from "./types";

/** 确定性伪随机（LCG），同一种子每次画出同一张图 */
function makeRandom(seed: number): () => number {
  let state = seed >>> 0;

  return () => {
    state = (state * 1664525 + 1013904223) >>> 0;

    return state / 4294967296;
  };
}

/** 字符串转稳定种子 */
function hashSeed(text: string): number {
  let hash = 2166136261;

  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }

  return hash >>> 0;
}

interface Painter {
  (ctx: CanvasRenderingContext2D, size: number): void;
}

/** 石头系噪点底（石头 / 深 板岩 / 圆石等灰岩类的通用底） */
function speckle(
  base: string,
  accents: string[],
  count: number,
  seed: number,
): Painter {
  return (ctx, size) => {
    const random = makeRandom(seed);

    ctx.fillStyle = base;
    ctx.fillRect(0, 0, size, size);
    for (let index = 0; index < count; index += 1) {
      ctx.fillStyle = accents[Math.floor(random() * accents.length)];
      ctx.fillRect(
        Math.floor(random() * size),
        Math.floor(random() * size),
        1,
        1,
      );
    }
  };
}

/** 矿石：石底 + 彩色矿点（2×2 成簇，接近原版矿石观感） */
function ore(gem: string, gemLight: string, seed: number): Painter {
  return (ctx, size) => {
    speckle("#8a8a8a", ["#7c7c7c", "#999999"], 110, seed)(ctx, size);
    const random = makeRandom(seed + 1);

    for (let cluster = 0; cluster < 3; cluster += 1) {
      const cx = Math.floor(random() * (size - 4)) + 1;
      const cy = Math.floor(random() * (size - 4)) + 1;

      ctx.fillStyle = gem;
      ctx.fillRect(cx, cy, 2, 2);
      ctx.fillRect(cx + 2, cy + 1, 1, 1);
      ctx.fillStyle = gemLight;
      ctx.fillRect(cx, cy, 1, 1);
    }
  };
}

/** 木板：横条纹 + 木纹点 */
function planks(
  base: string,
  line: string,
  grain: string,
  seed: number,
): Painter {
  return (ctx, size) => {
    ctx.fillStyle = base;
    ctx.fillRect(0, 0, size, size);
    ctx.fillStyle = line;
    for (let y = 0; y < size; y += 4) ctx.fillRect(0, y, size, 1);
    const random = makeRandom(seed);

    ctx.fillStyle = grain;
    for (let index = 0; index < 24; index += 1) {
      ctx.fillRect(
        Math.floor(random() * size),
        Math.floor(random() * size),
        2,
        1,
      );
    }
  };
}

/** 原木侧面：竖条纹树皮 */
function logSide(base: string, stripe: string, seed: number): Painter {
  return (ctx, size) => {
    ctx.fillStyle = base;
    ctx.fillRect(0, 0, size, size);
    const random = makeRandom(seed);

    ctx.fillStyle = stripe;
    for (let index = 0; index < size; index += 2) {
      if (random() > 0.35) ctx.fillRect(index, 0, 1, size);
    }
  };
}

/** 草 / 树叶系绿色噪点 */
function leafNoise(base: string, accents: string[], seed: number): Painter {
  return (ctx, size) =>
    speckle(base, accents, size * size * 0.45, seed)(ctx, size);
}

/** 物品：透明底 + 居中同色系像素剪影（剑 / 锹 / 锭……的通用占位） */
function itemSilhouette(main: string, accent: string, seed: number): Painter {
  return (ctx, size) => {
    ctx.clearRect(0, 0, size, size);
    const random = makeRandom(seed);
    const cx = size / 2;
    const half = Math.max(2, Math.round(size * 0.22));

    ctx.fillStyle = main;
    // 对角主体（剑刃 / 工具柄方向的抽象剪影）
    for (let step = 0; step < size - half; step += 1) {
      const x = Math.floor(cx - half / 2 + step * 0.4);
      const y = size - half - step;

      if (x < 0 || y < 0) break;
      ctx.fillRect(x, y, half, 1);
    }
    ctx.fillStyle = accent;
    for (let index = 0; index < 10; index += 1) {
      ctx.fillRect(
        Math.floor(random() * size),
        Math.floor(random() * size),
        1,
        1,
      );
    }
  };
}

/** 纯色系方块（玻璃 / 冰 / 陶瓦……）半透明平铺 + 边框高光 */
function pane(base: string, edge: string): Painter {
  return (ctx, size) => {
    ctx.clearRect(0, 0, size, size);
    ctx.fillStyle = base;
    ctx.fillRect(0, 0, size, size);
    ctx.fillStyle = edge;
    ctx.fillRect(0, 0, size, 1);
    ctx.fillRect(0, size - 1, size, 1);
    ctx.fillRect(0, 0, 1, size);
    ctx.fillRect(size - 1, 0, 1, size);
  };
}

/** 已知贴图 id → 原版风格绘制器（覆盖资源库里的常用方块 / 物品） */
const PAINTERS: Record<string, Painter> = {
  stone: speckle("#8a8a8a", ["#7c7c7c", "#999999"], 110, 3),
  granite: speckle("#9a6b58", ["#8a5d4c", "#a97b66"], 110, 31),
  diorite: speckle("#c8c8c9", ["#b5b5b6", "#dcdcdf"], 110, 32),
  andesite: speckle("#8f8f91", ["#828284", "#9c9c9e"], 110, 33),
  deepslate: speckle("#4b4b52", ["#3f3f45", "#585862"], 120, 13),
  cobblestone: speckle("#7d7d7d", ["#5f5f5f", "#9a9a9a"], 90, 34),
  mossy_cobblestone: speckle("#6f7d63", ["#54604a", "#8b987e"], 100, 35),
  bedrock: speckle("#575757", ["#333333", "#7a7a7a"], 130, 36),
  dirt: speckle("#79553a", ["#8a6245", "#6b4a33"], 100, 11),
  coarse_dirt: speckle("#7a5639", ["#8b6446", "#67462e"], 110, 37),
  grass_block_top: leafNoise("#5fa54a", ["#6fbb57", "#4f8f3d"], 17),
  grass_block_side: (ctx, size) => {
    speckle("#79553a", ["#8a6245", "#6b4a33"], 90, 11)(ctx, size);
    const random = makeRandom(18);
    const depth = Math.max(2, Math.round(size / 6));

    ctx.fillStyle = "#5fa54a";
    for (let x = 0; x < size; x += 1) {
      const h = depth - Math.floor(random() * 2);

      ctx.fillRect(x, 0, 1, h);
    }
  },
  sand: speckle("#dbcd9f", ["#e8dcba", "#cec091"], 110, 38),
  red_sand: speckle("#bd6a30", ["#cb7a3e", "#a95c26"], 110, 39),
  gravel: speckle("#837e7c", ["#6d6866", "#9b9694"], 120, 40),
  clay: speckle("#9fa4b1", ["#8f94a2", "#b0b5c1"], 100, 41),
  snow: speckle("#f4fbfb", ["#e8f2f2", "#ffffff"], 60, 42),
  obsidian: speckle("#1b1123", ["#2a1c38", "#120a1a"], 90, 43),
  netherrack: speckle("#6f3634", ["#5e2b29", "#814240"], 110, 44),
  soul_sand: speckle("#503a2e", ["#423025", "#5e4638"], 110, 45),
  end_stone: speckle("#dcdfa4", ["#ced192", "#e9ecb6"], 100, 46),
  glowstone: (ctx, size) => {
    speckle("#8f7a49", ["#7d6b3e", "#a08c57"], 100, 47)(ctx, size);
    const random = makeRandom(48);

    ctx.fillStyle = "#ffd97a";
    for (let index = 0; index < 10; index += 1) {
      ctx.fillRect(
        Math.floor(random() * (size - 2)),
        Math.floor(random() * (size - 2)),
        2,
        2,
      );
    }
  },
  glass: pane("rgba(200,230,235,0.35)", "rgba(225,245,248,0.85)"),
  ice: pane("rgba(140,190,235,0.55)", "rgba(180,220,250,0.9)"),
  oak_planks: planks("#b8894f", "#a97b45", "#c69963", 29),
  spruce_planks: planks("#7a5a35", "#6b4d2c", "#8a6a41", 49),
  birch_planks: planks("#d7c78e", "#c6b67c", "#e5d6a0", 50),
  jungle_planks: planks("#b07f60", "#a06f50", "#c09070", 51),
  acacia_planks: planks("#ba6337", "#a95428", "#ca7346", 52),
  dark_oak_planks: planks("#4f3218", "#402810", "#5f3f22", 53),
  oak_log: logSide("#6b5433", "#57432a", 54),
  oak_log_top: (ctx, size) => {
    ctx.fillStyle = "#b8945f";
    ctx.fillRect(0, 0, size, size);
    ctx.strokeStyle = "#9c7a4a";
    const c = size / 2;

    for (let r = 1; r < c; r += 2) {
      ctx.strokeRect(c - r, c - r, r * 2, r * 2);
    }
  },
  oak_leaves: leafNoise("#4a7a28", ["#568c30", "#3c6620"], 55),
  iron_ore: ore("#d8af93", "#e8c5ab", 61),
  copper_ore: ore("#c1683c", "#d98455", 62),
  gold_ore: ore("#fcc944", "#ffe066", 67),
  diamond_ore: ore("#5ce9e7", "#9df5f3", 5),
  emerald_ore: ore("#41cd34", "#77e66e", 63),
  lapis_ore: ore("#2f5ec9", "#5c85e0", 64),
  redstone_ore: ore("#c22f2f", "#e05555", 65),
  coal_ore: ore("#2c2c2c", "#474747", 66),
  ancient_debris_side: speckle("#5b4143", ["#4b3436", "#6d4f51"], 110, 68),
  iron_block: pane("#d8d8d8", "#efefef"),
  gold_block: pane("#f9d23e", "#ffe98c"),
  diamond_block: pane("#62e6df", "#9df3ee"),
  emerald_block: pane("#3ecc60", "#7fe394"),
  lapis_block: pane("#2b53b8", "#5c7fd6"),
  redstone_block: pane("#b01111", "#d43a3a"),
  coal_block: pane("#191919", "#303030"),
  netherite_block: pane("#443c3e", "#5a5052"),
  bookshelf: (ctx, size) => {
    planks("#b8894f", "#a97b45", "#c69963", 29)(ctx, size);
    const random = makeRandom(70);
    const colors = ["#a03a3a", "#3a6ea0", "#4aa03a", "#a0843a"];
    const band = Math.max(2, Math.round(size / 5));

    for (const y of [band, size - band * 2]) {
      for (let x = 0; x < size - 1; x += 2) {
        ctx.fillStyle = colors[Math.floor(random() * colors.length)];
        ctx.fillRect(x + 1, y, 2, band);
      }
    }
  },
  tnt: (ctx, size) => {
    ctx.fillStyle = "#c33";
    ctx.fillRect(0, 0, size, size);
    const band = Math.max(2, Math.round(size / 4));

    ctx.fillStyle = "#e6e6e6";
    ctx.fillRect(0, (size - band) / 2, size, band);
    ctx.fillStyle = "#333";
    ctx.font = `bold ${band}px sans-serif`;
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.fillText("TNT", size / 2, size / 2 + 1);
  },
  pumpkin_side: speckle("#c07615", ["#d18320", "#a86610"], 90, 71),
  pumpkin_top: (ctx, size) => {
    speckle("#c07615", ["#d18320", "#a86610"], 80, 71)(ctx, size);
    ctx.strokeStyle = "#9c5c0e";
    ctx.beginPath();
    ctx.moveTo(size / 2, 0);
    ctx.lineTo(size / 2, size);
    ctx.moveTo(0, size / 2);
    ctx.lineTo(size, size / 2);
    ctx.stroke();
  },
  melon_side: (ctx, size) => {
    leafNoise("#5f8f2f", ["#6fa03a", "#4f7f25"], 72)(ctx, size);
    ctx.strokeStyle = "#3c6620";
    for (let x = 1; x < size; x += 4) {
      ctx.beginPath();
      ctx.moveTo(x, 0);
      ctx.lineTo(x, size);
      ctx.stroke();
    }
  },
  cactus_side: (ctx, size) => {
    leafNoise("#0f6f1f", ["#138726", "#0b5718"], 73)(ctx, size);
    ctx.fillStyle = "#87c98a";
    for (let y = 1; y < size; y += 4) ctx.fillRect(1, y, 1, 1);
  },
  sculk: speckle("#0d2b2e", ["#081d20", "#144044"], 120, 74),
};

/** 物品关键词 → 配色（用于没有专属画器的物品贴图） */
const ITEM_TINTS: Array<[RegExp, string, string]> = [
  [/sword/, "#7fd3e8", "#4aa3bd"],
  [/pickaxe|axe|shovel|hoe|shears/, "#9aa7b0", "#6f7c86"],
  [/gold/, "#f6d33c", "#c9a41e"],
  [/iron/, "#d8d8d8", "#a8a8a8"],
  [/netherite/, "#4a4145", "#2f282b"],
  [/diamond/, "#5ce9e7", "#34c4c1"],
  [/apple/, "#e04444", "#b02222"],
  [/ingot|raw_/, "#d8d8d8", "#a8a8a8"],
  [/gem|pearl|eye|star|totem|shell|heart/, "#7ee06a", "#4fb53e"],
  [/potion|bottle/, "#8f5ce0", "#6a3ab8"],
  [/book|paper|map/, "#c9a15c", "#a37a38"],
  [/bucket/, "#b8bcc0", "#8f9396"],
  [/bow|crossbow|rod|stick|lead/, "#8a6a42", "#6b5030"],
];

/** 从路径推断贴图 id（stone.png → stone，entity 子目录取文件名） */
function textureIdOf(path: string): string {
  const name = path.replace(/\\/g, "/").split("/").pop() ?? "";

  return name.replace(/\.png$/i, "").toLowerCase();
}

/**
 * 给路径生成一张"原版风格"的种子画布：
 * 方块贴图按 id 匹配画器，未知的用按 id 调色的噪点；
 * 物品贴图用剪影 + 关键词配色；其余（实体 / 界面）给中性噪点底。
 */
export function seedVanillaCanvas(
  path: string,
  size: number,
): HTMLCanvasElement {
  const canvas = makeCanvas(size, size);
  const ctx = canvas.getContext("2d");

  if (!ctx) return canvas;
  const id = textureIdOf(path);
  const lower = path.replace(/\\/g, "/").toLowerCase();

  const painter =
    PAINTERS[id] ??
    (lower.includes("/block/") || lower.includes("/textures/block")
      ? speckle(
          tintOf(id),
          [shade(tintOf(id), -18), shade(tintOf(id), 12)],
          size * size * 0.42,
          hashSeed(id),
        )
      : lower.includes("/item/") || lower.includes("/textures/item")
        ? itemPainterFor(id)
        : lower.includes("/gui/") || lower.includes("/textures/gui")
          ? pane("rgba(30,30,30,0.15)", "rgba(255,255,255,0.35)")
          : speckle(
              tintOf(id),
              [shade(tintOf(id), 14), shade(tintOf(id), -10)],
              size * size * 0.4,
              hashSeed(id),
            ));

  painter(ctx, size);

  return canvas;
}

/** 由 id 哈希出一个稳定的中性色（未知方块的底色） */
function tintOf(id: string): string {
  const random = makeRandom(hashSeed(id));
  const hue = Math.floor(random() * 360);
  const sat = 8 + Math.floor(random() * 14);
  const light = 42 + Math.floor(random() * 16);

  return `hsl(${hue}, ${sat}%, ${light}%)`;
}

/** hsl 颜色明度偏移（简单正负偏移，用于噪点明暗） */
function shade(hsl: string, delta: number): string {
  const match = /hsl\((\d+),\s*(\d+)%,\s*(\d+)%\)/.exec(hsl);

  if (!match) return hsl;
  const light = Math.min(88, Math.max(8, Number(match[3]) + delta));

  return `hsl(${match[1]}, ${match[2]}%, ${light}%)`;
}

/** 未知物品贴图：按关键词选配色画剪影 */
function itemPainterFor(id: string): Painter {
  for (const [pattern, main, accent] of ITEM_TINTS) {
    if (pattern.test(id)) return itemSilhouette(main, accent, hashSeed(id));
  }

  return itemSilhouette("#c0c6cc", "#8f959b", hashSeed(id));
}
