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
 * 轻量代码编辑器纯逻辑：JSON 令牌化（高亮用）、光标行列换算（补全定位用）、
 * 补全候选生成。不依赖任何第三方编辑器库。
 */

export type TokenType =
  | "key"
  | "string"
  | "number"
  | "boolean"
  | "null"
  | "punct"
  | "comment"
  | "plain"
  | "space";

export interface CodeToken {
  type: TokenType;
  value: string;
}

/** 令牌类型 → 展示色（Tailwind 类） */
export const TOKEN_CLASS: Record<TokenType, string> = {
  key: "text-sky-600 dark:text-sky-400",
  string: "text-emerald-600 dark:text-emerald-400",
  number: "text-amber-600 dark:text-amber-400",
  boolean: "text-violet-600 dark:text-violet-400",
  null: "text-violet-600 dark:text-violet-400",
  punct: "text-gray-400 dark:text-gray-500",
  comment: "text-gray-500 dark:text-gray-500 italic",
  plain: "text-gray-800 dark:text-gray-200",
  space: "",
};

const JSON_TOKEN_REGEX =
  /"(?:\\.|[^"\\])*"|[{}[\],:]|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|\btrue\b|\bfalse\b|\bnull\b|\s+|[^\s{}[\],:"]+/g;

/** 把 JSON 文本切成带类型的令牌，拼接结果与原文一致。 */
export function tokenizeJson(input: string): CodeToken[] {
  const tokens: CodeToken[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  JSON_TOKEN_REGEX.lastIndex = 0;
  while ((match = JSON_TOKEN_REGEX.exec(input)) !== null) {
    if (match.index > lastIndex) {
      tokens.push({
        type: "plain",
        value: input.slice(lastIndex, match.index),
      });
    }
    const value = match[0];
    let type: TokenType = "plain";

    if (value.startsWith('"')) {
      let cursor = match.index + value.length;

      while (cursor < input.length && /\s/.test(input[cursor])) cursor += 1;
      type = input[cursor] === ":" ? "key" : "string";
    } else if (value === "true" || value === "false") {
      type = "boolean";
    } else if (value === "null") {
      type = "null";
    } else if (/^[{}[\],:]$/.test(value)) {
      type = "punct";
    } else if (/^-?\d/.test(value)) {
      type = "number";
    } else if (/^\s+$/.test(value)) {
      type = "space";
    }
    tokens.push({ type, value });
    lastIndex = JSON_TOKEN_REGEX.lastIndex;
  }
  if (lastIndex < input.length) {
    tokens.push({ type: "plain", value: input.slice(lastIndex) });
  }

  return tokens;
}

/** 无高亮的纯文本令牌（无对应词法的文件） */
export function tokenizePlain(input: string): CodeToken[] {
  return input ? [{ type: "plain", value: input }] : [];
}

// ---------------------------------------------------------------------------
// YAML：逐行解析「缩进 (- )? key: value # 注释」。不做完整 YAML 语法分析，
// 只覆盖配置文件的真实形态（bukkit.yml / spigot.yml / paper 配置等），
// 目标是高亮与补全可用，不是校验器。
// ---------------------------------------------------------------------------

/** 标量（单行值）分类：布尔 / null / 数字 / 带引号字符串 / 其余按纯文本。
 *  首尾空白单独成 space 令牌，保证拼接结果与原文逐字一致。 */
function classifyScalar(raw: string): CodeToken[] {
  const tokens: CodeToken[] = [];
  let value = raw;
  // 行内注释：引号外的 " #"（配置文件里 # 几乎不会出现在值内，够用）
  const commentAt = findInlineComment(value);
  let comment = "";

  if (commentAt >= 0) {
    comment = value.slice(commentAt);
    value = value.slice(0, commentAt);
  }
  const leading = /^\s*/.exec(value)![0];

  if (leading) {
    tokens.push({ type: "space", value: leading });
    value = value.slice(leading.length);
  }
  const trailing = /\s*$/.exec(value)?.[0] ?? "";
  const core = value.slice(0, value.length - trailing.length);

  if (core !== "") {
    const quoted =
      /^"((?:\\.|[^"\\])*)"$/.exec(core) || /^'([^']*)'$/.exec(core);

    if (quoted) {
      tokens.push({ type: "string", value: core });
    } else if (
      core === "true" ||
      core === "false" ||
      core === "on" ||
      core === "off"
    ) {
      tokens.push({ type: "boolean", value: core });
    } else if (core === "null" || core === "~") {
      tokens.push({ type: "null", value: core });
    } else if (/^-?\d+(\.\d+)?$/.test(core)) {
      tokens.push({ type: "number", value: core });
    } else {
      tokens.push({ type: "plain", value: core });
    }
  }
  if (trailing) tokens.push({ type: "space", value: trailing });
  if (comment) tokens.push({ type: "comment", value: comment });

  return tokens;
}

/** 行内注释起点：跳过成对引号内容后再找 #。无注释返回 -1。 */
function findInlineComment(line: string): number {
  let quote: string | null = null;

  for (let i = 0; i < line.length; i++) {
    const ch = line[i];

    if (quote) {
      if (ch === quote) quote = null;
    } else if (ch === '"' || ch === "'") {
      quote = ch;
    } else if (ch === "#" && (i === 0 || /\s/.test(line[i - 1]))) {
      return i;
    }
  }

  return -1;
}

function tokenizeYamlLine(line: string): CodeToken[] {
  const tokens: CodeToken[] = [];
  const indent = /^\s*/.exec(line)![0];
  let rest = line.slice(indent.length);

  if (indent) tokens.push({ type: "space", value: indent });

  // 列表项 "- "
  if (rest.startsWith("-")) {
    tokens.push({ type: "punct", value: "-" });
    rest = rest.slice(1);
    const sp = /^[ \t]*/.exec(rest)![0];

    if (sp) tokens.push({ type: "space", value: sp });
    rest = rest.slice(sp.length);
  }
  if (rest === "") return tokens;
  if (rest.startsWith("#")) {
    tokens.push({ type: "comment", value: rest });

    return tokens;
  }

  // 键：带引号或裸文本，后跟 ":"
  const quotedKey = /^"((?:\\.|[^"\\])*)"\s*:/.exec(rest);

  if (quotedKey) {
    const raw = rest.slice(0, quotedKey[0].length);
    const colonAt = raw.lastIndexOf(":");

    tokens.push({ type: "key", value: raw.slice(0, colonAt) });
    tokens.push({ type: "punct", value: ":" });
    tokens.push(...classifyScalar(raw.slice(colonAt + 1)));

    return tokens;
  }
  const bareKey = /^([^:'#"][^:]*?):/.exec(rest);

  if (bareKey && bareKey[1].trim() !== "") {
    tokens.push({ type: "key", value: bareKey[1] });
    tokens.push({ type: "punct", value: ":" });
    tokens.push(...classifyScalar(rest.slice(bareKey[0].length)));

    return tokens;
  }

  tokens.push(...classifyScalar(rest));

  return tokens;
}

/** YAML 令牌化（按行，保留换行符，拼接结果与原文一致）。 */
export function tokenizeYaml(input: string): CodeToken[] {
  const tokens: CodeToken[] = [];
  const lines = input.split("\n");

  lines.forEach((line, index) => {
    tokens.push(...tokenizeYamlLine(line));
    if (index < lines.length - 1) tokens.push({ type: "space", value: "\n" });
  });

  return tokens;
}

// ---------------------------------------------------------------------------
// 键值对（server.properties / .ini / .conf / .toml）：
// 「key=value」「key = value」「# 注释」「[小节]」「key: value」。
// ---------------------------------------------------------------------------

function tokenizeKVLine(line: string): CodeToken[] {
  const tokens: CodeToken[] = [];
  const indent = /^\s*/.exec(line)![0];
  let rest = line.slice(indent.length);

  if (indent) tokens.push({ type: "space", value: indent });
  if (rest === "") return tokens;
  if (rest.startsWith("#") || rest.startsWith(";") || rest.startsWith("//")) {
    tokens.push({ type: "comment", value: rest });

    return tokens;
  }
  // [section]
  const section = /^\[[^\]]*\]\s*$/.exec(rest);

  if (section) {
    tokens.push({ type: "key", value: rest });

    return tokens;
  }
  const kv = /^([^=:]*?)([=:])(.*)$/.exec(rest);

  if (kv && kv[1].trim() !== "") {
    const keyLeading = /^\s*/.exec(kv[1])![0];

    if (keyLeading) tokens.push({ type: "space", value: keyLeading });
    tokens.push({ type: "key", value: kv[1].slice(keyLeading.length) });
    tokens.push({ type: "punct", value: kv[2] });
    tokens.push(...classifyScalar(kv[3]));

    return tokens;
  }
  tokens.push(...classifyScalar(rest));

  return tokens;
}

/** 键值对格式令牌化（按行，保留换行符，拼接结果与原文一致）。 */
export function tokenizeKV(input: string): CodeToken[] {
  const tokens: CodeToken[] = [];
  const lines = input.split("\n");

  lines.forEach((line, index) => {
    tokens.push(...tokenizeKVLine(line));
    if (index < lines.length - 1) tokens.push({ type: "space", value: "\n" });
  });

  return tokens;
}

/** 编辑器语言类型：json / yaml / kv（properties 等键值对）/ plain */
export type CodeLanguage = "json" | "yaml" | "kv" | "plain";

/** 按文件扩展名推断编辑器语言（服务器目录里常见的配置类型）。 */
export function detectLanguage(fileName: string): CodeLanguage {
  const name = fileName.toLowerCase();

  if (name.endsWith(".json") || name.endsWith(".mcmeta")) return "json";
  if (name.endsWith(".yml") || name.endsWith(".yaml")) return "yaml";
  if (
    name.endsWith(".properties") ||
    name.endsWith(".conf") ||
    name.endsWith(".ini") ||
    name.endsWith(".cfg") ||
    name.endsWith(".toml") ||
    name.endsWith(".env")
  )
    return "kv";

  return "plain";
}

/** 统一入口：按语言分派词法。 */
export function tokenize(input: string, language: CodeLanguage): CodeToken[] {
  switch (language) {
    case "json":
      return tokenizeJson(input);
    case "yaml":
      return tokenizeYaml(input);
    case "kv":
      return tokenizeKV(input);
    default:
      return tokenizePlain(input);
  }
}

/** 光标在文本中的行列（0 基）；用于把补全下拉定位到光标处。 */
export function caretLineCol(
  value: string,
  index: number,
): { line: number; col: number } {
  const before = value.slice(0, index);
  const lastNewline = before.lastIndexOf("\n");

  return {
    line: before.split("\n").length - 1,
    col: index - (lastNewline + 1),
  };
}

/** 光标前正在输入的词（字母 / 数字 / _ - : . /） */
export function wordAtCaret(value: string, index: number): string {
  const before = value.slice(0, index);
  const match = /[A-Za-z0-9_:./-]+$/.exec(before);

  return match ? match[0] : "";
}

/** 静态 JSON 补全词典（Minecraft 资源包常见键 / 值片段） */
const JSON_DICTIONARY = [
  "parent",
  "textures",
  "elements",
  "display",
  "gui",
  "head",
  "fixed",
  "ground",
  "thirdperson_righthand",
  "thirdperson_lefthand",
  "firstperson_righthand",
  "firstperson_lefthand",
  "model",
  "variants",
  "multipart",
  "when",
  "apply",
  "from",
  "to",
  "rotation",
  "origin",
  "uv",
  "faces",
  "north",
  "south",
  "east",
  "west",
  "up",
  "down",
  "texture",
  "tintindex",
  "rotation",
  "cullface",
  "ambientocclusion",
  "shade",
  "overrides",
  "predicate",
  "layer0",
  "layer1",
  "layer2",
  "layer3",
  "all",
  "side",
  "top",
  "bottom",
  "front",
  "end",
  "particle",
  "block/",
  "item/",
  "minecraft:block/",
  "minecraft:item/",
  "minecraft:item/generated",
  "minecraft:block/cube_all",
  "minecraft:block/cube_column",
  "minecraft:block/cube",
  "minecraft:builtin/generated",
  "pack_format",
  "supported_formats",
  "min_inclusive",
  "max_inclusive",
  "description",
  "credit",
  "texture_size",
  "filter",
  "block",
  "item",
  "include",
  "exclude",
  "en_us",
  "zh_cn",
];

/** 从文档里已有的 key 提取候选（补全当前文件自身的字段） */
function documentKeys(value: string): string[] {
  const keys = new Set<string>();
  const regex = /"((?:\\.|[^"\\])*)"\s*:/g;
  let match: RegExpExecArray | null;

  while ((match = regex.exec(value)) !== null) {
    keys.add(match[1]);
  }

  return [...keys];
}

/** YAML 文档里已有的键（含缩进裸键与 "- 键:" 列表键） */
function documentYamlKeys(value: string): string[] {
  const keys = new Set<string>();
  const regex = /^\s*(?:-\s+)?([A-Za-z0-9_.-]+):/gm;
  let match: RegExpExecArray | null;

  while ((match = regex.exec(value)) !== null) {
    keys.add(match[1]);
  }

  return [...keys];
}

/** 键值对文档里已有的键（key= / key: 开头行） */
function documentKVKeys(value: string): string[] {
  const keys = new Set<string>();
  const regex = /^\s*([A-Za-z0-9_.-]+)\s*[=:]/gm;
  let match: RegExpExecArray | null;

  while ((match = regex.exec(value)) !== null) {
    keys.add(match[1]);
  }

  return [...keys];
}

/** YAML 常见补全词典：Bukkit/Spigot/Paper 配置键 + 通用值 */
const YAML_DICTIONARY = [
  "settings",
  "spawn-limits",
  "monsters",
  "animals",
  "water-animals",
  "water-ambient",
  "ambient",
  "ticks-per",
  "autosave",
  "monster-spawns",
  "animal-spawns",
  "chunk-gc",
  "period-in-ticks",
  "aliases",
  "minimum-players",
  "maximum-players",
  "world-settings",
  "default",
  "verbose",
  "mob-spawner-tick-rate",
  "item-merge-radius",
  "experience-merge-radius",
  "view-distance",
  "simulation-distance",
  "merge-radius",
  "item",
  "exp",
  "arrow-despawn-rate",
  "enable-zombies",
  "zombie-aggressive-towards-villager",
  "nerf-spawner-mobs",
  "hopper",
  "transfer",
  "cooldown-when-full",
  "amount",
  "max-tick-time",
  "tile",
  "entity",
  "anti-xray",
  "enabled",
  "engine-mode",
  "hide-blocks",
  "replacement-blocks",
  "plugins",
  "load",
  "late-load",
  "version",
  "type",
  "author",
  "main",
  "api-version",
  "name",
  "description",
  "website",
  "depend",
  "softdepend",
  "commands",
  "usage",
  "permission",
  "aliases",
  "default",
  "true",
  "false",
  "on",
  "off",
  "WARN",
  "INFO",
  "SEVERE",
  "ALL",
];

/** server.properties / .toml / .ini 常见补全词典 */
const KV_DICTIONARY = [
  "enable-jmx-monitoring",
  "rcon.port",
  "level-seed",
  "gamemode",
  "enable-command-block",
  "motd",
  "query.port",
  "pvp",
  "difficulty",
  "network-compression-threshold",
  "require-resource-pack",
  "max-tick-time",
  "use-native-transport",
  "enable-status",
  "online-mode",
  "enable-rcon",
  "max-players",
  "resource-pack",
  "resource-pack-prompt",
  "spawn-protection",
  "simulation-distance",
  "sync-chunk-writes",
  "enable-query",
  "rate-limit",
  "max-chained-neighbor-updates",
  "rcon.password",
  "view-distance",
  "server-ip",
  "allow-flight",
  "white-list",
  "level-name",
  "level-type",
  "allow-nether",
  "force-gamemode",
  "enable-whitelist",
  "broadcast-rcon-to-ops",
  "spawn-monsters",
  "log-ips",
  "hardcore",
  "wrap-around-gamemode",
  "function-permission-level",
  "text-filtering-config",
  "player-idle-timeout",
  "force-gamemode",
  "rcon.port",
  "op-permission-level",
  "snooper-enabled",
  "prevent-proxy-connections",
  "hide-online-players",
  "server-port",
  "enforce-secure-profile",
  "initial-disabled-packs",
  "true",
  "false",
  "survival",
  "creative",
  "adventure",
  "spectator",
  "peaceful",
  "easy",
  "normal",
  "hard",
  "minecraft\\normal",
  "normal",
  "vanilla",
  "paper",
  "fabric",
  "neoforge",
];

/** 生成补全候选：静态词典 + 文档内已有 key，按前缀过滤并去重。 */
export function buildCompletions(
  value: string,
  language: CodeLanguage,
  prefix: string,
): string[] {
  let pool: string[];

  switch (language) {
    case "json":
      pool = [...new Set([...documentKeys(value), ...JSON_DICTIONARY])];
      break;
    case "yaml":
      pool = [...new Set([...documentYamlKeys(value), ...YAML_DICTIONARY])];
      break;
    case "kv":
      pool = [...new Set([...documentKVKeys(value), ...KV_DICTIONARY])];
      break;
    default:
      return [];
  }
  const keyword = prefix.toLowerCase();
  const matches = keyword
    ? pool.filter(
        (item) =>
          item.toLowerCase().startsWith(keyword) &&
          item.toLowerCase() !== keyword,
      )
    : [];

  return matches.sort((left, right) => {
    if (left.length !== right.length) return left.length - right.length;

    return left.localeCompare(right);
  });
}
