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
 * 资源库目录：把常见的原版资源路径（方块 / 物品 / 实体 / 界面 / 语言…）
 * 映射到中文名与英文 id，供"资源查找"按中文或英文 id 检索后一键创建文件。
 * 不是全量数据，覆盖常用创作场景即可；找不到时仍可手填路径。
 */

export interface CatalogEntry {
  path: string;
  /** 中文名（用于检索与展示） */
  zh: string;
  /** 英文 id / 关键词 */
  en: string;
  kind: "png" | "text";
  category: string;
  /** PNG 默认边长 */
  size?: number;
}

const TEX = "assets/minecraft/textures";
const BLOCKSTATES = "assets/minecraft/blockstates";
const MODELS_BLOCK = "assets/minecraft/models/block";
const MODELS_ITEM = "assets/minecraft/models/item";
const LANG = "assets/minecraft/lang";

/** [英文 id, 中文名] */
const BLOCKS: Array<[string, string]> = [
  ["stone", t("石头")],
  ["granite", t("花岗岩")],
  ["diorite", t("闪长岩")],
  ["andesite", t("安山岩")],
  ["deepslate", t("深板岩")],
  ["cobblestone", t("圆石")],
  ["mossy_cobblestone", t("苔石")],
  ["bedrock", t("基岩")],
  ["dirt", t("泥土")],
  ["coarse_dirt", t("粗泥")],
  ["rooted_dirt", t("缠根泥土")],
  ["grass_block_top", t("草方块顶部")],
  ["grass_block_side", t("草方块侧面")],
  ["podzol_top", t("灰化土顶部")],
  ["mycelium", t("菌丝体")],
  ["sand", t("沙子")],
  ["red_sand", t("红沙")],
  ["gravel", t("沙砾")],
  ["clay", t("黏土")],
  ["bricks", t("红砖块")],
  ["stone_bricks", t("石砖")],
  ["mossy_stone_bricks", t("苔石砖")],
  ["cracked_stone_bricks", t("裂纹石砖")],
  ["chiseled_stone_bricks", t("雕纹石砖")],
  ["sandstone", t("砂岩")],
  ["red_sandstone", t("红砂岩")],
  ["obsidian", t("黑曜石")],
  ["crying_obsidian", t("哭泣的黑曜石")],
  ["netherrack", t("下界岩")],
  ["soul_sand", t("灵魂沙")],
  ["soul_soil", t("灵魂土")],
  ["basalt_side", t("玄武岩侧面")],
  ["blackstone", t("黑石")],
  ["end_stone", t("末地石")],
  ["purpur_block", t("紫珀块")],
  ["prismarine", t("海晶石")],
  ["dark_prismarine", t("暗海晶石")],
  ["sea_lantern", t("海晶灯")],
  ["glowstone", t("荧石")],
  ["shroomlight", t("菌光体")],
  ["glass", t("玻璃")],
  ["tinted_glass", t("染色玻璃")],
  ["ice", t("冰")],
  ["packed_ice", t("浮冰")],
  ["blue_ice", t("蓝冰")],
  ["snow", t("雪")],
  ["sponge", t("海绵")],
  ["wet_sponge", t("湿海绵")],
  ["slime_block", t("黏液块")],
  ["honey_block_side", t("蜂蜜块侧面")],
  ["terracotta", t("陶瓦")],
  ["white_terracotta", t("白色陶瓦")],
  ["oak_planks", t("橡木木板")],
  ["spruce_planks", t("云杉木板")],
  ["birch_planks", t("白桦木板")],
  ["jungle_planks", t("丛林木板")],
  ["acacia_planks", t("金合欢木板")],
  ["dark_oak_planks", t("深色橡木木板")],
  ["mangrove_planks", t("红树木板")],
  ["crimson_planks", t("绯红木板")],
  ["warped_planks", t("诡异木板")],
  ["oak_log", t("橡木原木")],
  ["oak_log_top", t("橡木原木顶")],
  ["spruce_log", t("云杉原木")],
  ["birch_log", t("白桦原木")],
  ["oak_leaves", t("橡树树叶")],
  ["spruce_leaves", t("云杉树叶")],
  ["iron_ore", t("铁矿石")],
  ["deepslate_iron_ore", t("深层铁矿石")],
  ["copper_ore", t("铜矿石")],
  ["gold_ore", t("金矿石")],
  ["deepslate_gold_ore", t("深层金矿石")],
  ["diamond_ore", t("钻石矿石")],
  ["deepslate_diamond_ore", t("深层钻石矿石")],
  ["emerald_ore", t("绿宝石矿石")],
  ["deepslate_emerald_ore", t("深层绿宝石矿石")],
  ["redstone_ore", t("红石矿石")],
  ["deepslate_redstone_ore", t("深层红石矿石")],
  ["lapis_ore", t("青金石矿石")],
  ["deepslate_lapis_ore", t("深层青金石矿石")],
  ["coal_ore", t("煤矿石")],
  ["deepslate_coal_ore", t("深层煤矿石")],
  ["ancient_debris_side", t("远古残骸侧面")],
  ["iron_block", t("铁块")],
  ["gold_block", t("金块")],
  ["diamond_block", t("钻石块")],
  ["emerald_block", t("绿宝石块")],
  ["lapis_block", t("青金石块")],
  ["redstone_block", t("红石块")],
  ["coal_block", t("煤炭块")],
  ["copper_block", t("铜块")],
  ["netherite_block", t("下界合金块")],
  ["quartz_block_side", t("石英块侧面")],
  ["bookshelf", t("书架")],
  ["crafting_table_top", t("工作台顶部")],
  ["crafting_table_front", t("工作台正面")],
  ["furnace_front", t("熔炉正面")],
  ["furnace_top", t("熔炉顶部")],
  ["chest", t("箱子")],
  ["ender_chest", t("末影箱")],
  ["trapped_chest", t("陷阱箱")],
  ["tnt", t("炸药")],
  ["tnt_top", t("炸药顶部")],
  ["cactus_side", t("仙人掌侧面")],
  ["pumpkin_side", t("南瓜侧面")],
  ["pumpkin_top", t("南瓜顶部")],
  ["carved_pumpkin", t("雕刻南瓜")],
  ["jack_o_lantern", t("南瓜灯")],
  ["melon_side", t("西瓜侧面")],
  ["hay_block_side", t("干草块侧面")],
  ["iron_bars", t("铁栏杆")],
  ["glass_pane_top", t("玻璃板顶部")],
  ["torch", t("火把")],
  ["lantern", t("灯笼")],
  ["redstone_lamp", t("红石灯")],
  ["note_block", t("音符盒")],
  ["jukebox_side", t("唱片机侧面")],
  ["beacon", t("信标")],
  ["enchanting_table_top", t("附魔台顶部")],
  ["anvil_top", t("铁砧顶部")],
  ["brewing_stand", t("酿造台")],
  ["cauldron_top", t("炼药锅顶部")],
  ["hopper_outside", t("漏斗外侧")],
  ["piston_top", t("活塞顶")],
  ["piston_side", t("活塞侧面")],
  ["sticky_piston", t("粘性活塞")],
  ["observer_front", t("侦测器正面")],
  ["dispenser_front", t("发射器正面")],
  ["dropper_front", t("投掷器正面")],
  ["command_block", t("命令方块")],
  ["crimson_nylium", t("绯红菌岩")],
  ["warped_nylium", t("诡异菌岩")],
  ["sculk", t("幽匿块")],
  ["sculk_catalyst_side", t("幽匿催发体侧面")],
];

const ITEMS: Array<[string, string]> = [
  ["diamond_sword", t("钻石剑")],
  ["iron_sword", t("铁剑")],
  ["golden_sword", t("金剑")],
  ["netherite_sword", t("下界合金剑")],
  ["wooden_sword", t("木剑")],
  ["stone_sword", t("石剑")],
  ["bow", t("弓")],
  ["crossbow", t("弩")],
  ["arrow", t("箭")],
  ["spectral_arrow", t("光灵箭")],
  ["shield", t("盾牌")],
  ["trident", t("三叉戟")],
  ["diamond_pickaxe", t("钻石镐")],
  ["iron_pickaxe", t("铁镐")],
  ["golden_pickaxe", t("金镐")],
  ["netherite_pickaxe", t("下界合金镐")],
  ["diamond_axe", t("钻石斧")],
  ["diamond_shovel", t("钻石锹")],
  ["diamond_hoe", t("钻石锄")],
  ["shears", t("剪刀")],
  ["flint_and_steel", t("打火石")],
  ["fishing_rod", t("钓鱼竿")],
  ["carrot_on_a_stick", t("胡萝卜钓竿")],
  ["elytra", t("鞘翅")],
  ["totem_of_undying", t("不死图腾")],
  ["apple", t("苹果")],
  ["golden_apple", t("金苹果")],
  ["enchanted_golden_apple", t("附魔金苹果")],
  ["bread", t("面包")],
  ["cooked_beef", t("牛排")],
  ["cooked_porkchop", t("熟猪排")],
  ["carrot", t("胡萝卜")],
  ["potato", t("马铃薯")],
  ["baked_potato", t("烤马铃薯")],
  ["melon_slice", t("西瓜片")],
  ["cookie", t("曲奇")],
  ["diamond", t("钻石")],
  ["emerald", t("绿宝石")],
  ["iron_ingot", t("铁锭")],
  ["gold_ingot", t("金锭")],
  ["copper_ingot", t("铜锭")],
  ["netherite_ingot", t("下界合金锭")],
  ["coal", t("煤炭")],
  ["charcoal", t("木炭")],
  ["redstone", t("红石粉")],
  ["glowstone_dust", t("荧石粉")],
  ["gunpowder", t("火药")],
  ["stick", t("木棍")],
  ["flint", t("燧石")],
  ["string", t("线")],
  ["feather", t("羽毛")],
  ["leather", t("皮革")],
  ["rabbit_hide", t("兔子皮")],
  ["paper", t("纸")],
  ["book", t("书")],
  ["writable_book", t("书与笔")],
  ["enchanted_book", t("附魔书")],
  ["name_tag", t("命名牌")],
  ["bucket", t("桶")],
  ["water_bucket", t("水桶")],
  ["lava_bucket", t("熔岩桶")],
  ["milk_bucket", t("牛奶桶")],
  ["ender_pearl", t("末影珍珠")],
  ["ender_eye", t("末影之眼")],
  ["blaze_rod", t("烈焰棒")],
  ["blaze_powder", t("烈焰粉")],
  ["bone", t("骨头")],
  ["bone_meal", t("骨粉")],
  ["slime_ball", t("黏液球")],
  ["magma_cream", t("岩浆膏")],
  ["phantom_membrane", t("幻翼膜")],
  ["nautilus_shell", t("鹦鹉螺壳")],
  ["heart_of_the_sea", t("海洋之心")],
  ["prismarine_shard", t("海晶碎片")],
  ["nether_star", t("下界之星")],
  ["experience_bottle", t("附魔之瓶")],
  ["potion", t("药水")],
  ["splash_potion", t("喷溅药水")],
  ["lingering_potion", t("滞留药水")],
  ["honey_bottle", t("蜂蜜瓶")],
  ["clock", t("钟")],
  ["compass", t("指南针")],
  ["recovery_compass", t("追溯指针")],
  ["map", t("地图")],
  ["filled_map", t("已填充地图")],
  ["lead", t("拴绳")],
  ["saddle", t("鞍")],
  ["minecart", t("矿车")],
  ["oak_boat", t("橡木船")],
  ["armor_stand", t("盔甲架")],
];

/** [entity 下相对路径, 中文名] */
const ENTITIES: Array<[string, string]> = [
  ["steve", t("玩家（Steve）")],
  ["alex", t("玩家（Alex）")],
  ["zombie/zombie", t("僵尸")],
  ["creeper/creeper", t("苦力怕")],
  ["skeleton/skeleton", t("骷髅")],
  ["enderman/enderman", t("末影人")],
  ["villager/villager", t("村民")],
  ["cow/cow", t("牛")],
  ["pig/pig", t("猪")],
  ["sheep/sheep", t("羊")],
  ["chicken/chicken", t("鸡")],
  ["wolf/wolf", t("狼")],
  ["cat/cat_black", t("黑猫")],
  ["spider/spider", t("蜘蛛")],
  ["slime/slime", t("史莱姆")],
  ["horse/horse_white", t("白马")],
];

const GUI: Array<[string, string]> = [
  ["gui/sprites/hud/hotbar.png", t("快捷栏")],
  ["gui/sprites/hud/hotbar_selection.png", t("快捷栏选中框")],
  ["gui/sprites/hud/crosshair.png", t("准星")],
  ["gui/options_background.png", t("设置界面背景")],
  ["gui/title/background/panorama_0.png", t("主菜单背景")],
];

const EXTRAS: Array<[string, string, string, "png" | "text"]> = [
  ["pack.mcmeta", t("资源包信息"), "pack meta description pack_format", "text"],
  ["pack.png", t("资源包图标"), "pack icon preview", "png"],
  [`${LANG}/zh_cn.json`, t("简体中文语言"), "lang zh_cn translation", "text"],
  [`${LANG}/en_us.json`, t("英文语言"), "lang en_us translation", "text"],
  [`${LANG}/zh_tw.json`, t("繁体中文语言"), "lang zh_tw translation", "text"],
  [
    "assets/minecraft/sounds.json",
    t("音效事件表"),
    "sounds sound events",
    "text",
  ],
  [
    "assets/minecraft/font/default.json",
    t("默认字体定义"),
    "font default",
    "text",
  ],
];

function buildCatalog(): CatalogEntry[] {
  const entries: CatalogEntry[] = [];

  for (const [id, zh] of BLOCKS) {
    entries.push({
      path: `${TEX}/block/${id}.png`,
      zh,
      en: id,
      kind: "png",
      category: t("方块贴图"),
      size: 16,
    });
    entries.push({
      path: `${BLOCKSTATES}/${id}.json`,
      zh,
      en: id,
      kind: "text",
      category: t("方块状态"),
    });
    entries.push({
      path: `${MODELS_BLOCK}/${id}.json`,
      zh,
      en: id,
      kind: "text",
      category: t("方块模型"),
    });
  }

  for (const [id, zh] of ITEMS) {
    entries.push({
      path: `${TEX}/item/${id}.png`,
      zh,
      en: id,
      kind: "png",
      category: t("物品贴图"),
      size: 16,
    });
    entries.push({
      path: `${MODELS_ITEM}/${id}.json`,
      zh,
      en: id,
      kind: "text",
      category: t("物品模型"),
    });
  }

  for (const [id, zh] of ENTITIES) {
    entries.push({
      path: `${TEX}/entity/${id}.png`,
      zh,
      en: id.split("/").pop() ?? id,
      kind: "png",
      category: t("实体贴图"),
      size: 64,
    });
  }

  for (const [path, zh] of GUI) {
    entries.push({
      path: `${TEX}/${path}`,
      zh,
      en:
        path
          .split("/")
          .pop()
          ?.replace(/\.png$/, "") ?? path,
      kind: "png",
      category: t("界面"),
      size: 16,
    });
  }

  for (const [path, zh, en, kind] of EXTRAS) {
    entries.push({
      path,
      zh,
      en,
      kind,
      category: t("其他"),
      size: kind === "png" ? 128 : undefined,
    });
  }

  return entries;
}

export const CATALOG: CatalogEntry[] = buildCatalog();

/** 按中文名 / 英文 id / 路径检索，空查询返回前面的常用条目。 */
export function searchCatalog(query: string, limit = 80): CatalogEntry[] {
  const keyword = query.trim().toLowerCase();

  if (!keyword) return CATALOG.slice(0, limit);

  const scored: Array<{ entry: CatalogEntry; score: number }> = [];

  for (const entry of CATALOG) {
    const zh = entry.zh.toLowerCase();
    const en = entry.en.toLowerCase();
    const path = entry.path.toLowerCase();
    let score = -1;

    if (zh.startsWith(keyword)) score = 0;
    else if (zh.includes(keyword)) score = 1;
    else if (en === keyword) score = 2;
    else if (en.startsWith(keyword)) score = 3;
    else if (en.includes(keyword)) score = 4;
    else if (path.includes(keyword)) score = 5;

    if (score >= 0) scored.push({ entry, score });
  }

  scored.sort(
    (left, right) =>
      left.score - right.score || left.entry.zh.localeCompare(right.entry.zh),
  );

  return scored.slice(0, limit).map((item) => item.entry);
}

/** 新建文本文件时的模板内容（按路径推断类型）。 */
export function defaultTextForPath(path: string): string {
  const blockstate = /\/blockstates\/([^/]+)\.json$/.exec(path);

  if (blockstate) {
    const id = blockstate[1];

    return `${JSON.stringify(
      { variants: { "": { model: `minecraft:block/${id}` } } },
      null,
      2,
    )}\n`;
  }

  const blockModel = /\/models\/block\/([^/]+)\.json$/.exec(path);

  if (blockModel) {
    const id = blockModel[1];

    return `${JSON.stringify(
      {
        parent: "block/cube_all",
        textures: { all: `minecraft:block/${id}` },
      },
      null,
      2,
    )}\n`;
  }

  const itemModel = /\/models\/item\/([^/]+)\.json$/.exec(path);

  if (itemModel) {
    const id = itemModel[1];

    return `${JSON.stringify(
      {
        parent: "item/generated",
        textures: { layer0: `minecraft:item/${id}` },
      },
      null,
      2,
    )}\n`;
  }

  if (path.endsWith("lang/zh_cn.json")) {
    return `${JSON.stringify({ "item.minecraft.example": "示例物品" }, null, 2)}\n`;
  }
  if (path.endsWith("lang/en_us.json")) {
    return `${JSON.stringify({ "item.minecraft.example": "Example Item" }, null, 2)}\n`;
  }
  if (path.endsWith("sounds.json")) {
    return `${JSON.stringify(
      { "example.sound": { sounds: ["example/sound"] } },
      null,
      2,
    )}\n`;
  }
  if (path.endsWith("pack.mcmeta")) {
    return `${JSON.stringify(
      { pack: { pack_format: 34, description: "我的资源包" } },
      null,
      2,
    )}\n`;
  }

  return path.endsWith(".json") || path.endsWith(".mcmeta") ? "{\n  \n}\n" : "";
}
