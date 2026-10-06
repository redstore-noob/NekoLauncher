/*
 * Copyright 2024 Next UI
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
/*
 * 实例管理页（移植自旧版 views/VersionsView.vue，对应 Avalonia VersionManagerPage.axaml）：
 * 指挥中心单页流 —— 顶部状态横幅（目录/计数/全部管理动作）+ 封顶实例列表面板（带搜索）+
 * 选中实例详情下挂在同页（概览 / 启动设置 / 内容 / 游戏设置 / 管理），不弹窗不跳转。
 * - 实例图标：ContentAPI.GetInstanceVisual（自定义图标/版本图标，本地路径经 /localfile 中转）；
 * - 概览：InstanceAPI.GetVersionDetails（加载器/基础版本/隔离状态/内容目录/Java 要求）；
 * - 启动设置：ConfigAPI.GetVersionProfile / SaveVersionProfile（版本隔离、独立内存、
 *   跟随全局高级设置、窗口尺寸、Java、额外 JVM/游戏参数）；
 * - 内容：详情自带的 Mods/ResourcePacks/Shaders/Saves + ContentAPI.ToggleContentEntry /
 *   ExportSave / DeleteSave；内容工具条上的「检查更新」（X-4）经
 *   ContentAPI.CheckInstanceContentUpdates 批量比对 Modrinth 哈希，
 *   可更新条目带角标，详情弹层可打开下载页 / 复制地址 / 另存 / 备份后替换；
 * - 管理：InstanceAPI.RenameInstance / DeleteInstance（两步确认）、打开文件夹。
 */
import type {
  content,
  download,
  instance,
  launch,
} from "../../wailsjs/go/models";
import type { Variants } from "framer-motion";

import React, {
  useCallback,
  useDeferredValue,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Button,
  Chip,
  Input,
  Modal,
  ModalContent,
  Select,
  SelectItem,
  Slider,
  Spinner,
  Switch,
  Tab,
  Tabs,
  Textarea,
} from "@heroui/react";
import {
  Folder20Regular as FolderIcon,
  ArrowClockwise20Regular as RefreshIcon,
  ArrowImport20Regular as ImportIcon,
  ArrowSwap20Regular as VersionIcon,
  Bot20Regular as BotIcon,
  Branch20Regular as ProvenanceIcon,
  ChevronDown20Regular as ChevronDownIcon,
  ChevronUp20Regular as ChevronUpIcon,
  Copy20Regular as CopyIcon,
  Edit20Regular as RenameIcon,
  History20Regular as RewindIcon,
  ArrowSync20Regular as UpdateIcon,
  Play20Regular as PlayIcon,
  Search20Regular,
  Save20Regular as SaveIcon,
} from "@fluentui/react-icons";
import { AnimatePresence, motion } from "framer-motion";

import { selectPopoverProps } from "../lib/motion";
import {
  CopyInstance,
  DeleteInstance,
  GetCurrentInstanceSnapshot,
  GetInstanceDisplayVersion,
  GetVersionDetails,
  RefreshInstances,
  RenameInstance,
  ScanImportableInstances,
  SelectInstance,
} from "../../wailsjs/go/bindings/InstanceAPI";
import { ModalShell, modalBehaviorProps } from "../components/modal-shell";
import {
  ContextMenuOverlay,
  type ContextMenuItem,
  type ContextMenuState,
} from "../components/context-menu";
import { navigateToPage } from "../lib/navigation";
import RewindDialog, {
  type RewindKind,
} from "../components/instance/RewindDialog";
import ModVersionDialog, {
  type ModVersionTarget,
} from "../components/instance/ModVersionDialog";
import { useLaunchProvenance } from "../components/launch/LaunchProvenancePanel";
import { TRANSITION_EASINGS } from "../lib/motion";
import SwitchTransition, {
  useSwitchDirection,
} from "../components/screen-transition";
import {
  GetVersionProfile,
  SaveVersionProfile,
  GetProfileFolders,
  SaveGameDirectory,
  AddProfileFolder,
} from "../../wailsjs/go/bindings/ConfigAPI";
import {
  ApplyContentUpdate,
  CheckInstanceContentUpdates,
  DeleteSave,
  DownloadContentUpdate,
  ExportSave,
  GetInstanceVisual,
  ImportSave,
  LookupModNameTranslations,
  RefreshModNameTranslations,
  RemoveCustomIcon,
  SetCustomIcon,
  ToggleContentEntry,
} from "../../wailsjs/go/bindings/ContentAPI";
import {
  GetMemorySliderMaximum,
  LaunchVersion,
} from "../../wailsjs/go/bindings/LauncherAPI";
import {
  OpenInExplorer,
  OpenPath,
  ReadTextFile,
  SaveFile,
  SelectDirectory,
  SelectFile,
  WriteTextFile,
} from "../../wailsjs/go/bindings/SystemAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { config } from "../../wailsjs/go/models";
import { asObject, asArray } from "../lib/guards";
import {
  badgeToneFor,
  downloadLink,
  findUpdateFor,
  hasModpackIssues,
  indexUpdateFiles,
  isUpdatable,
  modpackSummary,
  projectPageURL,
  replaceConfirmMessage,
  shouldShowBadge,
  suggestedFileName,
  summarizeResult,
  versionTransition,
} from "../lib/instanceUpdates";
import { alert, confirm, notify } from "../components/overlay/dialog";
import { PageActionSlot } from "../plugin";
import { t } from "../i18n";

type ContentTab = "已安装模组" | "资源包" | "光影包" | "游戏存档";
type Visual = { IconPath: string; FallbackGlyph: string };

/* 左列列表项专用进出场：只做透明度，**不动 height**。
 *
 * 为什么不复用 lib/motion 的 listItemVariants：那一份用 height: 0 → auto
 * 折叠。framer 在动画起始时测量一次内容高度并写成内联 height，而本列表的
 * 第二行（自定义名）来自 GetInstanceDisplayVersion 的**异步**返回——
 * 28 行并发、各自计时，异步到达后行内容变高，旧的内联 height 却不会跟着变，
 * 再被 overflow-hidden 裁掉，于是每个实例条目占据的垂直空间小于内容所需高度，
 * 整列被系统性压扁。
 *
 * 改为只过渡透明度；高度折叠交给外层的 CSS grid（0fr ↔ 1fr），
 * 由布局引擎在每次布局时重新计算，不存在"过期尺寸"。 */
const LIST_FADE: Variants = {
  enter: { opacity: 0 },
  center: {
    opacity: 1,
    transition: { duration: 0.25, ease: TRANSITION_EASINGS.easeOut },
  },
  exit: {
    opacity: 0,
    transition: { duration: 0.2, ease: TRANSITION_EASINGS.easeIn },
  },
};

// key 为语言无关的标识（用于状态与比较），label 为原文，展示时再 t()

// ---------- 游戏设置（options.txt） ----------
type GameSettingType = "boolean" | "slider" | "select" | "text";

interface GameSettingItem {
  key: string;
  label: string;
  type: GameSettingType;
  category: string;
  min?: number;
  max?: number;
  step?: number;
  options?: { value: string; label: string }[];
  suffix?: string;
  /** 显示值与实际值的比例（如音频实际0-1，显示0-100，则valueScale=100） */
  valueScale?: number;
}

const GAME_SETTINGS: GameSettingItem[] = [
  // 视频设置
  {
    key: "renderDistance",
    label: "渲染距离",
    type: "slider",
    category: "视频",
    min: 2,
    max: 32,
    step: 1,
    suffix: " 区块",
  },
  {
    key: "simulationDistance",
    label: "模拟距离",
    type: "slider",
    category: "视频",
    min: 5,
    max: 32,
    step: 1,
    suffix: " 区块",
  },
  {
    key: "entityDistance",
    label: "实体距离",
    type: "slider",
    category: "视频",
    min: 0.5,
    max: 5,
    step: 0.5,
    suffix: "x",
  },
  {
    key: "fov",
    label: "视野",
    type: "slider",
    category: "视频",
    min: 30,
    max: 110,
    step: 1,
    suffix: "°",
  },
  {
    key: "guiScale",
    label: "界面大小",
    type: "select",
    category: "视频",
    options: [
      { value: "0", label: "自动" },
      { value: "1", label: "小" },
      { value: "2", label: "中" },
      { value: "3", label: "大" },
      { value: "4", label: "最大" },
    ],
  },
  {
    key: "graphics",
    label: "图像品质",
    type: "select",
    category: "视频",
    options: [
      { value: "fast", label: "流畅" },
      { value: "fancy", label: "高品质" },
      { value: "fabulous", label: "极佳" },
    ],
  },
  {
    key: "particles",
    label: "粒子效果",
    type: "select",
    category: "视频",
    options: [
      { value: "all", label: "全部" },
      { value: "decreased", label: "减少" },
      { value: "minimal", label: "最少" },
    ],
  },
  {
    key: "maxFps",
    label: "最大帧率",
    type: "select",
    category: "视频",
    options: [
      { value: "30", label: "30 FPS" },
      { value: "60", label: "60 FPS" },
      { value: "75", label: "75 FPS" },
      { value: "120", label: "120 FPS" },
      { value: "144", label: "144 FPS" },
      { value: "165", label: "165 FPS" },
      { value: "240", label: "240 FPS" },
      { value: "260", label: "无限制" },
    ],
  },
  {
    key: "brightness",
    label: "亮度",
    type: "slider",
    category: "视频",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "mipmapLevels",
    label: "Mipmap 等级",
    type: "slider",
    category: "视频",
    min: 0,
    max: 4,
    step: 1,
  },
  {
    key: "biomeBlendRadius",
    label: "生物群系过渡",
    type: "slider",
    category: "视频",
    min: 0,
    max: 7,
    step: 1,
  },
  {
    key: "cloudType",
    label: "云",
    type: "select",
    category: "视频",
    options: [
      { value: "fast", label: "快速" },
      { value: "fancy", label: "高品质" },
      { value: "off", label: "关闭" },
    ],
  },
  { key: "fullscreen", label: "全屏", type: "boolean", category: "视频" },
  { key: "vsync", label: "垂直同步", type: "boolean", category: "视频" },
  { key: "bobView", label: "视角摇晃", type: "boolean", category: "视频" },
  {
    key: "entityShadows",
    label: "实体阴影",
    type: "boolean",
    category: "视频",
  },
  {
    key: "screenEffectScale",
    label: "屏幕效果强度",
    type: "slider",
    category: "视频",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },

  // 控制设置
  {
    key: "mouseSensitivity",
    label: "鼠标灵敏度",
    type: "slider",
    category: "控制",
    min: 0,
    max: 200,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "invertYMouse",
    label: "反转 Y 轴",
    type: "boolean",
    category: "控制",
  },
  { key: "autoJump", label: "自动跳跃", type: "boolean", category: "控制" },
  {
    key: "touchscreen",
    label: "触控屏模式",
    type: "boolean",
    category: "控制",
  },
  {
    key: "discrete_mouse_scroll",
    label: "离散鼠标滚轮",
    type: "boolean",
    category: "控制",
  },

  // 游戏设置
  {
    key: "difficulty",
    label: "难度",
    type: "select",
    category: "游戏",
    options: [
      { value: "0", label: "和平" },
      { value: "1", label: "简单" },
      { value: "2", label: "普通" },
      { value: "3", label: "困难" },
    ],
  },
  {
    key: "language",
    label: "语言",
    type: "select",
    category: "游戏",
    options: [
      { value: "zh_cn", label: "简体中文" },
      { value: "zh_tw", label: "繁體中文" },
      { value: "en_us", label: "English (US)" },
      { value: "en_gb", label: "English (UK)" },
      { value: "ja_jp", label: "日本語" },
      { value: "ko_kr", label: "한국어" },
      { value: "fr_fr", label: "Français" },
      { value: "de_de", label: "Deutsch" },
      { value: "es_es", label: "Español" },
      { value: "ru_ru", label: "Русский" },
      { value: "pt_br", label: "Português (Brasil)" },
      { value: "it_it", label: "Italiano" },
    ],
  },
  {
    key: "showAutosaveIndicator",
    label: "显示自动保存提示",
    type: "boolean",
    category: "游戏",
  },
  {
    key: "reducedDebugInfo",
    label: "减少调试信息",
    type: "boolean",
    category: "游戏",
  },
  {
    key: "hideLightningFlashes",
    label: "隐藏闪电效果",
    type: "boolean",
    category: "游戏",
  },
  {
    key: "advancedItemTooltips",
    label: "高级物品提示",
    type: "boolean",
    category: "游戏",
  },
  {
    key: "pauseOnLostFocus",
    label: "失焦时暂停",
    type: "boolean",
    category: "游戏",
  },
  {
    key: "useNativeTransport",
    label: "使用原生传输",
    type: "boolean",
    category: "游戏",
  },
  {
    key: "allowServerListing",
    label: "允许服务器列表",
    type: "boolean",
    category: "游戏",
  },

  // 聊天设置
  {
    key: "chatVisibility",
    label: "聊天可见性",
    type: "select",
    category: "聊天",
    options: [
      { value: "full", label: "全部" },
      { value: "system", label: "仅系统" },
      { value: "hidden", label: "隐藏" },
    ],
  },
  { key: "chatColors", label: "聊天颜色", type: "boolean", category: "聊天" },
  { key: "chatLinks", label: "聊天链接", type: "boolean", category: "聊天" },
  {
    key: "chatOpacity",
    label: "聊天不透明度",
    type: "slider",
    category: "聊天",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "chatLineSpacing",
    label: "聊天行间距",
    type: "slider",
    category: "聊天",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "textBackgroundOpacity",
    label: "文字背景不透明度",
    type: "slider",
    category: "聊天",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "backgroundForChatOnly",
    label: "仅聊天背景",
    type: "boolean",
    category: "聊天",
  },

  // 多人游戏
  {
    key: "realmsNotifications",
    label: "领域通知",
    type: "boolean",
    category: "多人游戏",
  },
  {
    key: "hideServerAddress",
    label: "隐藏服务器地址",
    type: "boolean",
    category: "多人游戏",
  },

  // 音频（主音量+常见分类）
  {
    key: "soundCategory_master",
    label: "主音量",
    type: "slider",
    category: "音频",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "soundCategory_music",
    label: "音乐",
    type: "slider",
    category: "音频",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "soundCategory_record",
    label: "唱片",
    type: "slider",
    category: "音频",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "soundCategory_weather",
    label: "天气",
    type: "slider",
    category: "音频",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "soundCategory_block",
    label: "方块",
    type: "slider",
    category: "音频",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "soundCategory_hostile",
    label: "敌对生物",
    type: "slider",
    category: "音频",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "soundCategory_friendly",
    label: "友好生物",
    type: "slider",
    category: "音频",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "soundCategory_players",
    label: "玩家",
    type: "slider",
    category: "音频",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "soundCategory_ambient",
    label: "环境",
    type: "slider",
    category: "音频",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
  {
    key: "soundCategory_voice",
    label: "语音",
    type: "slider",
    category: "音频",
    min: 0,
    max: 100,
    step: 1,
    suffix: "%",
    valueScale: 100,
  },
];

const GAME_SETTING_CATEGORIES = [
  "视频",
  "控制",
  "游戏",
  "聊天",
  "多人游戏",
  "音频",
];

function parseOptionsFile(text: string): Record<string, string> {
  const result: Record<string, string> = {};

  for (const line of text.split(/\r?\n/)) {
    const trimmed = line.trim();

    if (!trimmed || trimmed.startsWith("#")) continue;
    const idx = trimmed.indexOf(":");

    if (idx > 0) {
      const key = trimmed.slice(0, idx).trim();
      const value = trimmed.slice(idx + 1).trim();

      result[key] = value;
    }
  }

  return result;
}

function serializeOptionsFile(
  options: Record<string, string>,
  originalText: string,
): string {
  const lines = originalText.split(/\r?\n/);
  const usedKeys = new Set<string>();
  const result: string[] = [];

  for (const line of lines) {
    const trimmed = line.trim();

    if (!trimmed || trimmed.startsWith("#")) {
      result.push(line);
      continue;
    }
    const idx = trimmed.indexOf(":");

    if (idx > 0) {
      const key = trimmed.slice(0, idx).trim();

      if (key in options) {
        result.push(`${key}:${options[key]}`);
        usedKeys.add(key);
      } else {
        result.push(line);
      }
    } else {
      result.push(line);
    }
  }
  for (const [key, value] of Object.entries(options)) {
    if (!usedKeys.has(key)) {
      result.push(`${key}:${value}`);
    }
  }

  return result.join("\n");
}

const CONTENT_TABS: { key: ContentTab; label: string }[] = [
  { key: "已安装模组", label: "已安装模组" },
  { key: "资源包", label: "资源包" },
  { key: "光影包", label: "光影包" },
  { key: "游戏存档", label: "游戏存档" },
];

/** 详情页标签顺序（切换方向按它推导） */
const DETAIL_TABS = [
  "overview",
  "launch",
  "java",
  "content",
  "gamesettings",
  "manage",
] as const;

function toLocalFileUrl(path?: string | null): string | null {
  return path ? `/localfile?path=${encodeURIComponent(path)}` : null;
}

// gameicon:{key} 是后端的内置图标资源符号。优先使用 public/instance-icons
// 下的图片文件（用户可自行放置/替换），文件缺失或加载失败时回落 emoji
const GAMEICON_GLYPHS: Record<string, string> = {
  vanilla: "⛏️",
  fabric: "🧩",
  forge: "🔥",
  neoforge: "⚙️",
  liteloader: "🪶",
  command_block: "🧱",
  old_version: "📜",
  snapshot_version: "🔬",
};

/** 内置图标键（与后端 defaultInstanceIconPath 及下载页版本类型对应） */
const BUILTIN_ICON_KEYS = [
  "vanilla",
  "fabric",
  "forge",
  "neoforge",
  "liteloader",
  "command_block",
  "old_version",
  "snapshot_version",
] as const;

const GAMEICON_LABELS: Record<string, string> = {
  vanilla: "原版",
  fabric: "Fabric",
  forge: "Forge",
  neoforge: "NeoForge",
  liteloader: "LiteLoader",
  command_block: "通用",
  old_version: "远古版本",
  snapshot_version: "快照版本",
};

/** 内置图标的图片地址（文件由用户放在 frontend/public/instance-icons 下） */
function gameiconUrl(key: string): string | null {
  return (BUILTIN_ICON_KEYS as readonly string[]).includes(key)
    ? `/instance-icons/${key}.png`
    : null;
}

function instanceIconGlyph(
  visual: Visual | undefined,
  versionId: string,
): string {
  const path = visual?.IconPath ?? "";

  if (path.startsWith("gameicon:")) {
    return GAMEICON_GLYPHS[path.slice("gameicon:".length)] ?? "📦";
  }
  const fallback = visual?.FallbackGlyph ?? "";

  // 后端历史残留的 Avalonia 图标字体名，前端无对应字体
  return fallback === "material:Apps"
    ? "📦"
    : fallback || (versionId[0] || "?").toUpperCase();
}

function instanceIconUrl(
  visual: Visual | undefined,
  broken: boolean,
): string | null {
  if (broken) return null;
  const path = visual?.IconPath ?? "";

  if (!path || path.startsWith("gameicon:")) return null;

  return toLocalFileUrl(path);
}

function joinPath(dir: string | undefined | null, name: string): string {
  if (!dir) return name;

  // 后端（Go）在所有平台上都接受 "/" 作为分隔符，而 "\" 只在 Windows 上是分隔符：
  // 在 Linux/macOS 上传 "\saves" 会被当成文件名的一部分，凭空造出 "…\saves" 这种
  // 平级目录（存档导入进去后列表又看不到）。
  return dir.replace(/[\\/]+$/, "") + "/" + name;
}

/** 内置图标磁贴：优先渲染 instance-icons 下的图片，缺失回落 emoji */
function BuiltInIcon({ glyphKey }: { glyphKey: string }) {
  const url = gameiconUrl(glyphKey);
  const [broken, setBroken] = React.useState(false);

  if (url && !broken) {
    return (
      <img
        alt=""
        className="h-7 w-7 object-contain"
        src={url}
        onError={() => setBroken(true)}
      />
    );
  }

  return <span className="text-base">{GAMEICON_GLYPHS[glyphKey] ?? "📦"}</span>;
}

function formatTime(value: unknown): string {
  if (!value) return "—";
  const date = new Date(value as string);

  return isNaN(date.getTime()) ? String(value) : date.toLocaleDateString();
}

const InstancesPage: React.FC = () => {
  // 启动参数溯源面板：全局单例，这里只取打开入口
  const { open: openProvenance } = useLaunchProvenance();

  // ---------- 列表与目录 ----------
  const [snap, setSnap] = useState<instance.GameInstanceSnapshot | null>(null);
  const [folders, setFolders] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [status, setStatus] = useState("");
  const [selected, setSelected] = useState("");
  const [details, setDetails] = useState<instance.GameVersionDetails | null>(
    null,
  );
  const [visuals, setVisuals] = useState<Record<string, Visual>>({});
  const [brokenIcons, setBrokenIcons] = useState<Record<string, boolean>>({});
  const [displayNames, setDisplayNames] = useState<Record<string, string>>({});

  // ---------- 启动设置（实例档案） ----------
  const [profile, setProfile] = useState<config.GameVersionProfile | null>(
    null,
  );
  const [jvmText, setJvmText] = useState("");
  const [gameText, setGameText] = useState("");
  const [envText, setEnvText] = useState("");
  const [memoryMax, setMemoryMax] = useState(4096);
  const [saving, setSaving] = useState(false);
  const [advancedSettingsExpanded, setAdvancedSettingsExpanded] =
    useState(false);

  // ---------- Java 设置（实例级别） ----------
  const [javaConfig, setJavaConfig] = useState<config.JavaConfig>({
    JavaExecutable: "",
    MinMemoryMB: 0,
    MaxMemoryMB: 0,
    AdditionalJvmArguments: [],
    AdditionalGameArguments: [],
  });
  const [javaConfigJvmText, setJavaConfigJvmText] = useState("");
  const [javaConfigGameText, setJavaConfigGameText] = useState("");
  const [javaSaving, setJavaSaving] = useState(false);

  // 实例进程优先级下拉的可选项（label 走 i18n，组件内取译名）
  const priorityKeys = ["normal", "low", "belownormal", "abovenormal", "high"];
  const priorityLabel = (key: string) => {
    switch (key) {
      case "low":
        return t("低");
      case "belownormal":
        return t("低于普通");
      case "abovenormal":
        return t("高于普通");
      case "high":
        return t("高");
      default:
        return t("普通");
    }
  };

  // ---------- 内容与管理 ----------
  const [tab, setTab] = useState<
    "overview" | "launch" | "java" | "content" | "gamesettings" | "manage"
  >("overview");
  const [contentTab, setContentTab] = useState<ContentTab>("已安装模组");
  // 游戏设置
  const [gameOptions, setGameOptions] = useState<Record<string, string>>({});
  const [gameOptionsRaw, setGameOptionsRaw] = useState("");
  const [gameOptionsLoaded, setGameOptionsLoaded] = useState(false);
  const [gameOptionsDirty, setGameOptionsDirty] = useState(false);
  const [gameSettingCategory, setGameSettingCategory] = useState("视频");
  const [gameEditMode, setGameEditMode] = useState<"visual" | "raw">("visual");
  const [rawOptionsText, setRawOptionsText] = useState("");

  // ---------- 游戏设置函数 ----------
  const loadGameOptions = useCallback(async () => {
    if (!details) return;
    try {
      const optionsPath = details.ContentDirectory + "/options.txt";
      // 超时保护：5秒后强制完成，避免一直转圈
      const timeoutMs = 5000;
      const timeoutPromise = new Promise<string>((_, reject) =>
        setTimeout(() => reject(new Error("timeout")), timeoutMs),
      );
      const text = await Promise.race([
        ReadTextFile(optionsPath),
        timeoutPromise,
      ]);

      setGameOptionsRaw(text);
      setGameOptions(parseOptionsFile(text));
      setRawOptionsText(text);
      setGameOptionsLoaded(true);
      setGameOptionsDirty(false);
    } catch {
      // 文件不存在或读取失败时，显示空表单
      setGameOptionsRaw("");
      setGameOptions({});
      setRawOptionsText("");
      setGameOptionsLoaded(true);
      setGameOptionsDirty(false);
    }
  }, [details]);

  const saveGameOptions = useCallback(async () => {
    if (!details) return;
    const optionsPath = details.ContentDirectory + "/options.txt";
    const text =
      gameEditMode === "raw"
        ? rawOptionsText
        : serializeOptionsFile(gameOptions, gameOptionsRaw);

    await WriteTextFile(optionsPath, text);
    setGameOptionsRaw(text);
    setGameOptions(
      gameEditMode === "raw" ? parseOptionsFile(text) : gameOptions,
    );
    setRawOptionsText(text);
    setGameOptionsDirty(false);
  }, [details, gameOptions, gameOptionsRaw, gameEditMode, rawOptionsText]);

  const setGameOption = useCallback((key: string, value: string) => {
    setGameOptions((prev) => ({ ...prev, [key]: value }));
    setGameOptionsDirty(true);
  }, []);

  // 实例详情加载完成后自动加载游戏设置
  useEffect(() => {
    if (details) {
      setGameOptionsLoaded(false);
      void loadGameOptions();
    } else {
      setGameOptionsLoaded(false);
      setGameOptions({});
      setGameOptionsRaw("");
      setGameOptionsDirty(false);
    }
  }, [details, loadGameOptions]);

  // 标签序号 → 切换方向
  const tabDirection = useSwitchDirection(DETAIL_TABS.indexOf(tab));
  const contentDirection = useSwitchDirection(
    CONTENT_TABS.findIndex((t) => t.key === contentTab),
  );
  // 实例列表序号 → 切换方向（选中项在列表中的上下移动）
  const versions = useMemo(() => snap?.VersionIds ?? [], [snap]);
  // 指挥中心单页流：列表搜索 + 下挂详情的展开/收起
  const [versionSearch, setVersionSearch] = useState("");
  const [expandedInstance, setExpandedInstance] = useState(true);
  const filteredVersions = useMemo(() => {
    const q = versionSearch.trim().toLowerCase();

    if (!q) return versions;

    return versions.filter(
      (v) =>
        v.toLowerCase().includes(q) ||
        (displayNames[v] ?? "").toLowerCase().includes(q),
    );
  }, [versions, versionSearch, displayNames]);
  const instanceDirection = useSwitchDirection(
    versions.findIndex((v) => v === selected),
  );
  // 选中高亮块的几何信息：像 HeroUI 的 cursor 一样在列表项之间平移
  const listRef = useRef<HTMLDivElement | null>(null);
  const itemRefs = useRef<Record<string, HTMLButtonElement | null>>({});
  const [highlight, setHighlight] = useState<{
    top: number;
    height: number;
  } | null>(null);
  // 左列折叠状态：首屏的行展开（0fr→1fr）推迟到展示名解析就绪之后再播。
  // 展示名（加载器实例第二行）是逐实例异步返回的：此前 rAF 后立即展开，
  // 名字随后一个个到货、行一个个变高，整列反复下移——"进页面列表莫名动一下"。
  // 批量解析完一次性上屏，展开时行高就是最终高度，零抖动；
  // 800ms 兜底防止个别读取挂住把列表永远留在折叠态。
  const [rowsReady, setRowsReady] = useState(false);
  const namesSeedKeyRef = useRef("");

  useLayoutEffect(() => {
    if (!versions.length) return;
    const seedKey = versions.join("\u0000");

    if (namesSeedKeyRef.current === seedKey) return;
    namesSeedKeyRef.current = seedKey;

    // 列表改变时先重置折叠状态，等显示名称批量解析完再展开
    setRowsReady(false);

    let alive = true;
    const ensure = () => {
      if (alive) setRowsReady(true);
    };
    const fallback = setTimeout(ensure, 800);

    void Promise.allSettled(
      versions.map((v) =>
        GetInstanceDisplayVersion(v).then((d) => ({ id: v, display: d })),
      ),
    ).then((results) => {
      clearTimeout(fallback);
      if (!alive) return;
      const merged: Record<string, string> = {};

      for (const result of results) {
        if (
          result.status === "fulfilled" &&
          result.value.display &&
          result.value.display !== result.value.id
        )
          merged[result.value.id] = result.value.display;
      }
      // 一次性合并：逐条 setState 会让行高随每个到货的名字各跳一次
      if (Object.keys(merged).length > 0)
        setDisplayNames((prev) => ({ ...prev, ...merged }));
      setRowsReady(true);
    });

    return () => {
      alive = false;
      clearTimeout(fallback);
    };
  }, [versions]);

  // 选中项 / 列表变化后测量高亮块几何，供下方位移动画使用（下一帧再量一次，
  // 覆盖列表布局动画尚未落定的情况）。用 rect 差值测量，不依赖 offsetParent。
  useLayoutEffect(() => {
    const container = listRef.current;
    const node = itemRefs.current[selected];

    if (!container || !node) {
      setHighlight(null);

      return;
    }

    const measure = () => {
      const containerRect = container.getBoundingClientRect();
      const nodeRect = node.getBoundingClientRect();

      // 列表用 gap-0.5(0.125rem=2px)，选中项之前的每个 gap 都会累积偏移。
      // 计算选中项在可见列表中的序号，补偿之前所有 gap 的总高度。
      const selectedIndex = filteredVersions.indexOf(selected);
      const gapOffset = selectedIndex > 0 ? selectedIndex * 2 : 0; // gap-0.5 = 2px

      setHighlight({
        top: nodeRect.top - containerRect.top + container.scrollTop - gapOffset,
        height: nodeRect.height,
      });
    };

    // 首次进入页面时 rowsReady 会从 false→true 触发折叠动画(0fr→1fr, 250ms)，
    // 必须等动画完成后再测量，否则拿到的是压缩状态的错误高度/位置。
    // 后续切换实例时 rowsReady 恒为 true，动画已结束，可以立即测量。
    if (!rowsReady) {
      return; // 动画尚未开始，等下一轮
    }

    const initialDelay = setTimeout(measure, 260); // 等折叠动画(250ms)结束

    // 跟随真实尺寸变化：自定义名是异步返回的，行内容变高后 pill 也必须跟着变。
    // 之前只依赖 [selected, versions.length, loading]，异步到货时不会重测，
    // pill 会停留在旧高度。
    const observer = new ResizeObserver(measure);

    observer.observe(node);
    if (node.firstElementChild) observer.observe(node.firstElementChild);

    return () => {
      clearTimeout(initialDelay);
      observer.disconnect();
    };
  }, [selected, versions.length, loading, rowsReady, filteredVersions]);
  const [contentSearch, setContentSearch] = useState("");
  const [contentBusy, setContentBusy] = useState("");
  const [newName, setNewName] = useState("");
  // 右键菜单与由它打开的重命名/复制输入弹层
  const [ctxMenu, setCtxMenu] = useState<ContextMenuState | null>(null);
  const [menuAction, setMenuAction] = useState<null | "rename" | "copy">(null);
  // 拖拽文件安装：拖入时高亮 + zip 类别选择弹层
  /** 内容搜索框外层容器（Ctrl+F 用，HeroUI Input 本体 ref 不可靠） */
  const contentSearchRef = useRef<HTMLDivElement | null>(null);
  const [copyName, setCopyName] = useState("");
  const [copying, setCopying] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [rewindEntry, setRewindEntry] = useState<{
    kind: RewindKind;
    name: string;
    path: string;
  } | null>(null);

  // ---------- 内容更新检测（X-4） ----------
  // 结果按实例 ID 记住：切标签/切实例回来时角标还在，不用重复联网。
  const [updateResults, setUpdateResults] = useState<
    Record<string, download.ContentUpdateCheckResult>
  >({});
  const [updateChecking, setUpdateChecking] = useState(false);
  const [updateProgress, setUpdateProgress] = useState("");
  const [updateDetail, setUpdateDetail] =
    useState<download.ContentUpdateFile | null>(null);
  const [updateActionBusy, setUpdateActionBusy] = useState("");
  // 版本管理弹窗（升级/降级）：从更新详情或内容行进入
  const [versionTarget, setVersionTarget] = useState<ModVersionTarget | null>(
    null,
  );

  // ---------- 模组中文名 ----------
  // 译名来自 MC百科（mcmod.cn）搜索匹配（后端持久缓存）。key 为模组文件名；
  // 先秒回缓存命中的部分，未命中的由后端限流补查，补到后经事件触发重查。
  const [modNames, setModNames] = useState<Record<string, string>>({});

  // 译名效果：先秒回缓存命中；未命中的交给后端限流补查，落盘后收到
  // "modname:updated" 事件再重查一次（重查只是读缓存，开销可忽略）。
  useEffect(() => {
    const names = (details?.Mods ?? [])
      .map((m) => m.Name)
      .filter((n): n is string => !!n);

    if (names.length === 0) {
      setModNames({});

      return;
    }
    let cancelled = false;
    const refresh = () => {
      void LookupModNameTranslations(names)
        .then((map) => {
          if (!cancelled && map && Object.keys(map).length > 0) {
            setModNames(map);
          }
        })
        .catch(() => {
          /* 译名查询失败不影响列表展示 */
        });
    };

    refresh();
    void RefreshModNameTranslations(names).catch(() => {});
    const off = EventsOn("modname:updated", refresh);

    return () => {
      cancelled = true;
      off();
    };
  }, [details]);

  // ---------- 导入其它启动器 ----------
  const [importOpen, setImportOpen] = useState(false);
  const [importLoading, setImportLoading] = useState(false);
  const [importBusy, setImportBusy] = useState("");
  const [importList, setImportList] = useState<instance.ImportableInstance[]>(
    [],
  );

  const memorySliderMax = useCallback(async () => {
    try {
      setMemoryMax(await GetMemorySliderMaximum());
    } catch {
      /* 保持默认 */
    }
  }, []);

  // ---------- 数据装载 ----------
  const loadSnapshot = useCallback(async (folder?: string) => {
    const snap =
      folder !== undefined
        ? await RefreshInstances(folder)
        : await GetCurrentInstanceSnapshot();

    setSnap(snap);

    return snap;
  }, []);

  const loadVisuals = useCallback(async (versionIds: string[]) => {
    const entries = await Promise.all(
      versionIds.map(async (v) => {
        try {
          const visual = await GetInstanceVisual(v, "");

          // 后端异常/浏览器 mock 可能回 null：跳过该图标而不是炸掉整轮加载
          return visual ? ([v, visual] as const) : null;
        } catch {
          return null;
        }
      }),
    );
    const next: Record<string, Visual> = {};

    for (const entry of entries) {
      if (entry)
        next[entry[0]] = {
          IconPath: entry[1].IconPath,
          FallbackGlyph: entry[1].FallbackGlyph,
        };
    }
    setVisuals(next);
    setBrokenIcons({});
  }, []);

  // ---------- 并发守卫 ----------
  // selectVersion 序号：快速连点切换实例时，慢的旧响应会晚到并覆盖
  // details/profile/后端选中态，只有"最后一次点击"的响应允许落盘。
  const selectionSeqRef = useRef(0);
  // reloadAll 重入保护：instance:changed 事件与用户操作可能并发触发多轮
  // 扫描，交错写 snap/folders/visuals。在途时把新请求合并成收尾的一次补跑，
  // 所有调用方都 await 到"本轮连同补跑全部结束"。
  const reloadBusyRef = useRef(false);
  const reloadQueuedRef = useRef<{ folder?: string } | null>(null);
  const reloadRunningRef = useRef<Promise<void> | null>(null);

  const selectVersion = useCallback(
    async (versionId: string, mcDir: string) => {
      const seq = ++selectionSeqRef.current;

      setSelected(versionId);
      setDetails(null);
      setProfile(null);
      setConfirmDelete(false);
      setNewName(versionId);
      setCopyName(`${versionId}-copy`);
      setContentSearch("");
      // 切换实例时清掉上一个实例的更新结果，避免把 A 的角标显示在 B 的文件上
      setUpdateDetail(null);
      setUpdateProgress("");
      try {
        const d = await GetVersionDetails(versionId);

        if (seq !== selectionSeqRef.current) return; // 已被更新的点击超越
        setDetails(d);
      } catch (ex) {
        if (seq !== selectionSeqRef.current) return;
        setStatus(
          t("读取实例详情失败：{0}", { "0": (ex as Error)?.message ?? ex }),
        );
      }
      try {
        const loaded = asObject<config.GameVersionProfile>(
          await GetVersionProfile(mcDir, versionId),
        );

        if (seq !== selectionSeqRef.current) return;
        setProfile(loaded);
        setJvmText((loaded?.AdditionalJvmArguments ?? []).join("\n"));
        setGameText((loaded?.AdditionalGameArguments ?? []).join("\n"));
        setEnvText((loaded?.AdditionalEnvironmentVariables ?? []).join("\n"));
      } catch (ex) {
        if (seq !== selectionSeqRef.current) return;
        setStatus(
          t("读取实例设置失败：{0}", { "0": (ex as Error)?.message ?? ex }),
        );
      }
      // 加载实例 Java 配置
      try {
        const { GetInstanceJavaConfig } = await import(
          "../../wailsjs/go/bindings/InstanceAPI"
        );
        const loaded = await GetInstanceJavaConfig(versionId);

        if (seq !== selectionSeqRef.current) return;
        if (loaded) {
          setJavaConfig({
            JavaExecutable: loaded.JavaExecutable || "",
            MinMemoryMB: loaded.MinMemoryMB || 0,
            MaxMemoryMB: loaded.MaxMemoryMB || 0,
            AdditionalJvmArguments: loaded.AdditionalJvmArguments || [],
            AdditionalGameArguments: loaded.AdditionalGameArguments || [],
          });
          setJavaConfigJvmText(
            (loaded.AdditionalJvmArguments || []).join("\n"),
          );
          setJavaConfigGameText(
            (loaded.AdditionalGameArguments || []).join("\n"),
          );
        }
      } catch (ex) {
        // Java 配置加载失败不阻塞，使用默认值
        console.warn("加载 Java 配置失败:", ex);
      }
      if (seq !== selectionSeqRef.current) return; // 别让旧选择覆盖新选择
      try {
        await SelectInstance(versionId);
      } catch {
        /* 选中失败不阻塞浏览 */
      }
    },
    [],
  );

  const reloadAll = useCallback(
    async (folder?: string) => {
      const run = async (target?: string) => {
        setLoading(true);
        setStatus("");
        try {
          const snap = await loadSnapshot(target);

          setFolders(asArray(await GetProfileFolders()));
          const ids = snap.VersionIds ?? [];

          await loadVisuals(ids);
          // 展示名（加载器实例的第二行）由上方 rowsReady 的批量效果统一解析：
          // 首次进列表等它就绪再展开行（防逐条变高抖动），后续变更去重后增量合并
          if (ids.length > 0) {
            await selectVersion(
              snap.SelectedVersionId && ids.includes(snap.SelectedVersionId)
                ? snap.SelectedVersionId
                : ids[0],
              snap.MinecraftDirectory,
            );
          } else {
            setSelected("");
            setDetails(null);
            setProfile(null);
          }
        } catch (ex) {
          setStatus(
            t("读取实例列表失败：{0}", { "0": (ex as Error)?.message ?? ex }),
          );
        } finally {
          setLoading(false);
        }
      };

      if (reloadBusyRef.current) {
        // 已有扫描在途：合并为结束时的一次补跑（folder 以最新请求为准），
        // 并挂在同一轮链上，让调用方的 await 语义不变。
        reloadQueuedRef.current = { folder };

        return reloadRunningRef.current;
      }
      reloadBusyRef.current = true;
      const chain = (async () => {
        try {
          await run(folder);
          while (reloadQueuedRef.current) {
            const next = reloadQueuedRef.current;

            reloadQueuedRef.current = null;
            await run(next.folder);
          }
        } finally {
          reloadBusyRef.current = false;
          reloadRunningRef.current = null;
        }
      })();

      reloadRunningRef.current = chain;
      await chain;
    },
    [loadSnapshot, loadVisuals, selectVersion],
  );

  useEffect(() => {
    void memorySliderMax();
    void reloadAll();
    const changed = () => {
      void reloadAll();
    };

    const offChanged = EventsOn("instance:changed", changed);

    // 保存 / 添加的游戏目录下没有任何版本时，后端推送温和提示（不是错误）。
    const offEmptyDirectory = EventsOn(
      "instance:emptyDirectory",
      (path: string) => {
        setStatus(
          t("该目录下没有发现 Minecraft 版本：{0}", { "0": path ?? "" }),
        );
      },
    );

    return () => {
      offChanged();
      offEmptyDirectory();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 检查更新的进度（后端 content:updateProgress）：只用于按钮旁的一行提示，
  // 阶段文案由 Go 侧给出（算哈希 → 查 Modrinth → 比对整合包清单 → 汇总）。
  useEffect(() => {
    const offProgress = EventsOn(
      "content:updateProgress",
      (payload: { phase?: string; message?: string }) => {
        setUpdateProgress(payload?.message ?? "");
      },
    );

    return () => offProgress();
  }, []);

  // ---------- 操作 ----------
  const onFolderChange = async (folder: string) => {
    if (!folder || folder === snap?.MinecraftDirectory) return;
    await SaveGameDirectory(folder);
    await reloadAll(folder);
  };

  const addFolder = async () => {
    let path = "";

    try {
      path = await SelectDirectory(t("选择 Minecraft 根目录"));
    } catch {
      /* 用户取消 */
    }
    if (!path) return;
    if (!(await AddProfileFolder(path))) {
      setStatus(t("添加失败：该文件夹可能不包含有效的 Minecraft 版本。"));

      return;
    }
    await SaveGameDirectory(path);
    await reloadAll(path);
  };

  const openVersionFolder = async () => {
    if (!snap || !selected) return;
    const dir =
      details?.VersionDirectory ||
      joinPath(joinPath(snap.MinecraftDirectory, "versions"), selected);

    try {
      await OpenInExplorer(dir);
    } catch (ex) {
      setStatus(
        t("打开文件夹失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
  };

  const openGameFolder = async () => {
    if (!snap?.MinecraftDirectory) return;
    try {
      await OpenInExplorer(snap.MinecraftDirectory);
    } catch (ex) {
      setStatus(
        t("打开文件夹失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
  };

  const saveJavaConfig = async () => {
    if (!selected) return;
    setJavaSaving(true);
    try {
      const { SaveInstanceJavaConfig } = await import(
        "../../wailsjs/go/bindings/InstanceAPI"
      );

      const config: config.JavaConfig = {
        JavaExecutable: javaConfig.JavaExecutable.trim(),
        MinMemoryMB: javaConfig.MinMemoryMB,
        MaxMemoryMB: javaConfig.MaxMemoryMB,
        AdditionalJvmArguments: javaConfigJvmText
          .split("\n")
          .map((line) => line.trim())
          .filter((line) => line.length > 0),
        AdditionalGameArguments: javaConfigGameText
          .split("\n")
          .map((line) => line.trim())
          .filter((line) => line.length > 0),
      };

      await SaveInstanceJavaConfig(selected, config);
      notify.success(t("Java 设置已保存"));
    } catch (ex) {
      notify.error(t("保存失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setJavaSaving(false);
    }
  };

  const resetJavaConfig = async () => {
    if (!selected) return;
    try {
      const { DeleteInstanceJavaConfig } = await import(
        "../../wailsjs/go/bindings/InstanceAPI"
      );

      await DeleteInstanceJavaConfig(selected);

      // 重置状态
      setJavaConfig({
        JavaExecutable: "",
        MinMemoryMB: 0,
        MaxMemoryMB: 0,
        AdditionalJvmArguments: [],
        AdditionalGameArguments: [],
      });
      setJavaConfigJvmText("");
      setJavaConfigGameText("");

      notify.success(t("已重置为默认设置"));
    } catch (ex) {
      notify.error(t("重置失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  const saveProfile = async () => {
    if (!profile) return;
    setSaving(true);
    try {
      const next = config_profile_clone(profile);

      next.AdditionalJvmArguments = jvmText
        .split("\n")
        .map((s) => s.trim())
        .filter(Boolean);
      next.AdditionalGameArguments = gameText
        .split("\n")
        .map((s) => s.trim())
        .filter(Boolean);
      next.AdditionalEnvironmentVariables = envText
        .split("\n")
        .map((s) => s.trim())
        .filter(Boolean);
      if (!(await SaveVersionProfile(next))) {
        setStatus(t("保存失败：设置不合法（窗口尺寸/内存取值超界）。"));

        return;
      }
      setProfile(next);
      setStatus(t("实例设置已保存。"));
    } finally {
      setSaving(false);
    }
  };

  const renameInstance = async () => {
    const name = newName.trim();

    if (!selected || !name || name === selected) return;
    try {
      const finalId = await RenameInstance(selected, name);

      setStatus(t("已重命名为 {0}。", { "0": finalId }));
      setSelected("");
      await reloadAll();
      if (finalId) void selectVersion(finalId, snap?.MinecraftDirectory ?? "");
    } catch (ex) {
      setStatus(t("重命名失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  const copyInstance = async () => {
    const name = copyName.trim();

    if (!selected || !name || name === selected) return;
    setCopying(true);
    try {
      const finalId = await CopyInstance(selected, name);

      setStatus(t("已复制为 {0}。", { "0": finalId }));
      await reloadAll();
      if (finalId) void selectVersion(finalId, snap?.MinecraftDirectory ?? "");
    } catch (ex) {
      setStatus(t("复制失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setCopying(false);
    }
  };

  // ---------- 实例图标 ----------
  /** 写图标偏好（gameicon:x / "custom" / null=自动）并刷新列表图标 */
  const applyIconOverride = async (override: string | null) => {
    if (!profile) return false;
    const next = config_profile_clone(profile);

    next.InstanceIconOverride = override ?? undefined;
    if (!(await SaveVersionProfile(next))) {
      setStatus(t("图标偏好保存失败。"));

      return false;
    }
    setProfile(next);
    await loadVisuals(versions);

    return true;
  };

  const pickCustomIcon = async () => {
    if (!snap || !selected) return;
    let path = "";

    try {
      path = await SelectFile(
        t("选择实例图标"),
        t("图片文件"),
        "*.png;*.jpg;*.jpeg;*.webp;*.bmp;*.gif",
      );
    } catch {
      /* 用户取消 */
    }
    if (!path) return;
    try {
      await SetCustomIcon(snap.MinecraftDirectory, selected, path);
      if (await applyIconOverride("custom")) {
        setStatus(t("实例图标已更新。"));
      }
    } catch (ex) {
      setStatus(t("设置图标失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  const resetInstanceIcon = async () => {
    if (!snap || !selected) return;
    try {
      RemoveCustomIcon(snap.MinecraftDirectory, selected);
    } catch {
      /* 没有自定义图标时忽略 */
    }
    if (await applyIconOverride(null)) {
      setStatus(t("已恢复自动图标。"));
    }
  };

  // ---------- 右键快捷菜单 ----------
  // 右键先顺手选中该实例（详情/权限等动作依赖 selected），再弹菜单。

  /** 启动指定实例；失败且像 Java 问题时给"去配置 Java"的直达引导。 */
  const launchInstance = async (versionId: string) => {
    let result: launch.LaunchResult;

    try {
      result = await LaunchVersion(versionId, "", null, "");
    } catch (ex) {
      alert(t("启动失败：{0}", { "0": (ex as Error)?.message ?? ex }), {
        severity: "danger",
      });

      return;
    }
    if (result?.Success) {
      setStatus(result.Message || t("启动指令已发出。"));

      return;
    }
    const message = result?.Message || t("启动失败");
    const isJavaProblem = /java/i.test(message);

    if (isJavaProblem) {
      const agreed = await confirm(t("缺少可用的 Java"), message, {
        confirmLabel: t("去配置 Java"),
        severity: "warning",
      });

      if (agreed) navigateToPage("settings", "java");
    } else {
      alert(message, { severity: "danger" });
    }
  };

  const openInstanceMenu = (versionId: string, e: React.MouseEvent) => {
    if (snap) void selectVersion(versionId, snap.MinecraftDirectory);
    const items: ContextMenuItem[] = [
      {
        key: "launch",
        label: t("启动"),
        icon: <PlayIcon />,
        onSelect: () => void launchInstance(versionId),
      },
      { key: "d1", divider: true },
      {
        key: "rename",
        label: t("重命名…"),
        icon: <RenameIcon />,
        onSelect: () => {
          setNewName(versionId);
          setMenuAction("rename");
        },
      },
      {
        key: "copy",
        label: t("复制实例…"),
        icon: <CopyIcon />,
        onSelect: () => {
          setCopyName(t("{0}-副本", { "0": versionId }));
          setMenuAction("copy");
        },
      },
      { key: "d2", divider: true },
      {
        key: "versionFolder",
        label: t("打开版本文件夹"),
        icon: <FolderIcon />,
        onSelect: () => {
          const dir = details?.VersionDirectory
            ? details.VersionDirectory
            : joinPath(
                joinPath(snap?.MinecraftDirectory, "versions"),
                versionId,
              );

          void OpenInExplorer(dir).catch(() => {});
        },
      },
      {
        key: "gameFolder",
        label: t("打开游戏文件夹"),
        icon: <FolderIcon />,
        onSelect: () => {
          if (snap?.MinecraftDirectory) {
            void OpenInExplorer(snap.MinecraftDirectory).catch(() => {});
          }
        },
      },
      { key: "d3", divider: true },
      {
        key: "checkUpdate",
        label: t("检查更新"),
        icon: <UpdateIcon />,
        onSelect: () => void checkContentUpdates(),
      },
    ];

    setCtxMenu({ x: e.clientX, y: e.clientY, items });
  };

  /** 模组行右键：版本管理 / 交给 AI 分析（带上中文名上下文）。 */
  const openModMenu = (
    entry: content.GameContentEntry,
    e: React.MouseEvent,
  ) => {
    // 与列表行同一展示格式 "(中文名) 原名"
    const zh = modNames[entry.Name];
    const displayName = zh ? `(${zh}) ${entry.Name}` : entry.Name;

    setCtxMenu({
      x: e.clientX,
      y: e.clientY,
      items: [
        {
          key: "versionManager",
          label: t("版本管理…"),
          icon: <UpdateIcon />,
          onSelect: () => openVersionManager(entry),
        },
        { key: "d1", divider: true },
        {
          key: "ai",
          label: t("让 AI 分析"),
          icon: <BotIcon />,
          onSelect: () =>
            navigateToPage(
              "ai",
              t(
                "帮我分析当前实例里的模组「{0}」：它是做什么的、和其他常见模组有没有已知的兼容性问题？",
                { "0": displayName },
              ),
            ),
        },
      ],
    });
  };

  // 拖拽安装已全局化：FileDropOverlay 统一注册 Wails OnFileDrop（含 zip
  // 类别嗅探与选择弹层），任何页面拖入都会装进当前实例并触发 instance:changed
  // 刷新本页内容列表。

  // ---------- 列表键盘操作与快捷键 ----------
  // Ctrl+F 聚焦内容搜索框；实例列表支持 ↑/↓ 切换、双击空白处去下载页。

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "f") {
        const input = contentSearchRef.current?.querySelector("input");

        if (input) {
          e.preventDefault();
          input.focus();
          input.select();
        }
      }
    };

    window.addEventListener("keydown", onKey);

    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const handleListKeyDown = (e: React.KeyboardEvent) => {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    e.preventDefault();
    const index = filteredVersions.indexOf(selected);
    const next =
      e.key === "ArrowDown"
        ? filteredVersions[Math.min(filteredVersions.length - 1, index + 1)]
        : filteredVersions[Math.max(0, index - 1)];

    if (next && next !== selected && snap) {
      setExpandedInstance(true);
      void selectVersion(next, snap.MinecraftDirectory);
    }
  };

  const deleteInstance = async () => {
    if (!selected || !snap) return;
    try {
      await DeleteInstance(selected, snap.MinecraftDirectory);
      setStatus(t("已删除实例 {0}。", { "0": selected }));
      setSelected("");
      // 必须带目录强制后端重扫：无参的 reloadAll 走缓存快照，已删实例会残留
      await reloadAll(snap.MinecraftDirectory);
    } catch (ex) {
      setStatus(t("删除失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  // ---------- 导入其它启动器 ----------
  const openImport = async () => {
    setImportOpen(true);
    setImportLoading(true);
    try {
      setImportList(await ScanImportableInstances());
    } catch (ex) {
      setImportList([]);
      setStatus(t("扫描失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setImportLoading(false);
    }
  };

  // 注册（不复制文件）：把外部实例目录加入游戏目录列表并切换过去。
  const registerImport = async (item: instance.ImportableInstance) => {
    if (item.Registered) return;
    setImportBusy(item.Path);
    try {
      if (!(await AddProfileFolder(item.Path))) {
        setStatus(t("导入失败：路径无效或已存在。"));

        return;
      }
      await SaveGameDirectory(item.Path);
      await reloadAll(item.Path);
      setImportList((prev) =>
        prev.map((x) =>
          x.Path === item.Path ? { ...x, Registered: true } : x,
        ),
      );
      setStatus(
        t("已导入 {0} 实例「{1}」。", { "0": item.Provider, "1": item.Name }),
      );
    } catch (ex) {
      setStatus(t("导入失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setImportBusy("");
    }
  };

  // ---------- 内容列表 ----------
  const contentEntries: content.GameContentEntry[] = useMemo(() => {
    if (!details) return [];
    switch (contentTab) {
      case "已安装模组":
        return details.Mods ?? [];
      case "资源包":
        return details.ResourcePacks ?? [];
      case "光影包":
        return details.Shaders ?? [];
      case "游戏存档":
        return details.Saves ?? [];
    }
  }, [details, contentTab]);

  // 搜索词降优先级：输入框始终即时响应，几百行的过滤与列表重渲染
  // 走后台优先级、可被打断——大模组列表下打字不掉帧
  const deferredSearch = useDeferredValue(contentSearch);

  const filteredContent = useMemo(() => {
    const q = deferredSearch.trim().toLowerCase();

    if (!q) return contentEntries;

    return contentEntries.filter(
      (e) =>
        (e.Name || "").toLowerCase().includes(q) ||
        // 中文名（MC百科译名）也参与过滤：输入"钠"能找到 sodium-*.jar
        (modNames[e.Name] || "").toLowerCase().includes(q),
    );
  }, [contentEntries, deferredSearch, modNames]);

  const contentSummary = useMemo(() => {
    const total = contentEntries.length;
    const disabled = contentEntries.filter((e) => e.IsDisabled).length;

    return t("共 {0} 项{1}", {
      "0": total,
      "1": disabled > 0 ? `，已禁用 ${disabled} 项` : "",
    });
  }, [contentEntries]);

  const toggleContent = async (
    entry: content.GameContentEntry,
    enabled: boolean,
  ) => {
    setContentBusy(entry.SourcePath);
    try {
      // 后端语义：ToggleContentEntry(path, disable) —— 第二参为"禁用"
      await ToggleContentEntry(entry.SourcePath, !enabled);
      if (snap) {
        setDetails(await GetVersionDetails(selected));
      }
    } catch (ex) {
      setStatus(t("切换失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setContentBusy("");
    }
  };

  // ---------- 内容更新检测（X-4） ----------

  /** 当前实例的检测结果（没查过则为 undefined）。 */
  const updateResult = selected ? updateResults[selected] : undefined;

  /** 磁盘路径 → 检测条目，供列表渲染 O(1) 查。 */
  const updateIndex = useMemo(
    () => indexUpdateFiles(updateResult?.Files),
    [updateResult],
  );

  /** 某列表条目的更新检测结果（没查过返回 null）。 */
  const updateFor = useCallback(
    (entry: content.GameContentEntry) =>
      findUpdateFor(updateIndex, entry.SourcePath),
    [updateIndex],
  );

  const checkContentUpdates = async () => {
    if (!snap || !selected) {
      setStatus(t("请先选择一个实例。"));

      return;
    }
    setUpdateChecking(true);
    setUpdateProgress(t("正在准备检查…"));
    try {
      const result = await CheckInstanceContentUpdates(
        snap.SourcePath ?? "",
        snap.MinecraftDirectory ?? "",
        selected,
      );

      setUpdateResults((prev) => ({ ...prev, [selected]: result }));
      setUpdateProgress("");
      for (const notice of result.Notices ?? []) {
        if (notice) alert(notice, { severity: "warning" });
      }
      const summary = summarizeResult(result, t);

      if (summary) setStatus(t(summary));
      // 有缺失/被改动的整合包文件时单独提醒：这比"有更新"更需要处理
      if (hasModpackIssues(result)) {
        alert(t(modpackSummary(result)), { severity: "warning" });
      }
    } catch (ex) {
      setUpdateProgress("");
      setStatus(t("检查更新失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setUpdateChecking(false);
    }
  };

  // ---------- 一键全部更新 ----------
  // 预发布（pre/alpha/beta/rc 等）默认跳过，避免把稳定环境推向测试版。
  const isPrereleaseVersion = (version: string): boolean =>
    /(?:^|[-_.+])(?:pre|alpha|beta|rc|candidate)(?:[-_.+0-9]|$)/i.test(version);

  const updatableFiles = useMemo(
    () => (updateResult?.Files ?? []).filter((f) => isUpdatable(f)),
    [updateResult],
  );
  const bulkUpdatableFiles = useMemo(
    () =>
      updatableFiles.filter((f) => !isPrereleaseVersion(f.LatestVersion ?? "")),
    [updatableFiles],
  );
  const skippedPrereleaseCount =
    updatableFiles.length - bulkUpdatableFiles.length;

  const [bulkUpdating, setBulkUpdating] = useState(false);

  const updateAll = async () => {
    if (bulkUpdating || !selected || bulkUpdatableFiles.length === 0) return;
    const agreed = await confirm(
      t("全部更新"),
      t(
        "将按顺序为 {0} 个文件下载新版本并备份替换，期间请勿关闭启动器。跳过预发布版本。",
        { "0": bulkUpdatableFiles.length },
      ),
      { confirmLabel: t("开始更新"), severity: "warning" },
    );

    if (!agreed) return;
    setBulkUpdating(true);
    let okCount = 0;
    let failedCount = 0;
    let bulkFirstError = "";

    try {
      for (const entry of bulkUpdatableFiles) {
        setUpdateProgress(
          t("正在更新 {0}…（{1}/{2}）", {
            "0": entry.FileName,
            "1": okCount + failedCount + 1,
            "2": bulkUpdatableFiles.length,
          }),
        );
        try {
          await ApplyContentUpdate(
            downloadLink(entry),
            entry.FilePath,
            entry.SHA1 ?? "",
          );
          okCount++;
        } catch (ex) {
          failedCount++;
          // 留下首条失败原因：只有"失败 N 个"的话，用户分不清是网络问题
          // 还是作者禁止第三方分发（403），无从决定下一步
          if (!bulkFirstError) {
            bulkFirstError = (ex as Error)?.message ?? String(ex);
          }
        }
      }
      try {
        setDetails(await GetVersionDetails(selected));
      } catch {
        /* 列表刷新失败不打断提示 */
      }
      setUpdateResults((prev) => {
        const next = { ...prev };

        delete next[selected];

        return next;
      });
      const message = t("全部更新完成：成功 {0} 个，失败 {1} 个。", {
        "0": okCount,
        "1": failedCount,
      });

      setUpdateProgress("");
      alert(
        failedCount > 0 && bulkFirstError
          ? `${message}\n${t("首个失败原因：{0}", { "0": bulkFirstError })}`
          : message,
        { severity: failedCount > 0 ? "warning" : "success" },
      );
    } finally {
      setBulkUpdating(false);
    }
  };

  const copyDownloadURL = async (entry: download.ContentUpdateFile) => {
    const link = downloadLink(entry);

    if (!link) {
      alert(t("这个条目没有可用的下载地址。"), { severity: "warning" });

      return;
    }
    try {
      await navigator.clipboard.writeText(link);
      setStatus(t("下载地址已复制到剪贴板。"));
    } catch {
      // 剪贴板不可用（权限/非安全上下文）时退化为展示，绝不静默失败
      alert(link, { severity: "info" });
    }
  };

  const openDownloadPage = async (entry: download.ContentUpdateFile) => {
    const page = projectPageURL(entry) || downloadLink(entry);

    if (!page) {
      alert(t("这个条目没有可打开的地址。"), { severity: "warning" });

      return;
    }
    try {
      await OpenPath(page);
    } catch (ex) {
      setStatus(t("打开链接失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  /** 把新版本另存到用户选定的位置（不动原文件）。 */
  const saveUpdateAs = async (entry: download.ContentUpdateFile) => {
    let target = "";

    try {
      target = await SaveFile(
        t("保存新版本"),
        suggestedFileName(entry),
        t("内容文件"),
        "*.*",
      );
    } catch {
      /* 用户取消 */
    }
    if (!target) return;
    setUpdateActionBusy(entry.FilePath);
    try {
      const applied = await DownloadContentUpdate(
        downloadLink(entry),
        target,
        entry.SHA1 ?? "",
      );

      setStatus(applied?.Message || t("新版本已保存。"));
      alert(applied?.Message || t("新版本已保存。"), { severity: "success" });
    } catch (ex) {
      setStatus(t("保存失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setUpdateActionBusy("");
    }
  };

  /**
   * 一键替换：**先确认、后备份、再替换**。
   * 后端会把原文件改名为 <文件名>.bak-<时间戳>，失败自动回滚；
   * 这里必须把这件事先告诉用户（确认文案见 replaceConfirmMessage）。
   */
  const applyUpdate = async (entry: download.ContentUpdateFile) => {
    const agreed = await confirm(
      t("替换为新版本"),
      replaceConfirmMessage(entry),
      {
        confirmLabel: t("备份并替换"),
        severity: "warning",
      },
    );

    if (!agreed) return;
    setUpdateActionBusy(entry.FilePath);
    try {
      const applied = await ApplyContentUpdate(
        downloadLink(entry),
        entry.FilePath,
        entry.SHA1 ?? "",
      );

      setDetails(await GetVersionDetails(selected));
      setUpdateDetail(null);
      setStatus(applied?.Message || t("已替换为新版本。"));
      alert(applied?.Message || t("已替换为新版本。"), { severity: "success" });
      // 替换后旧结论失效：提示用户重新检查
      setUpdateResults((prev) => {
        const next = { ...prev };

        delete next[selected];

        return next;
      });
    } catch (ex) {
      setStatus(t("替换失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setUpdateActionBusy("");
    }
  };

  /** 打开版本管理弹窗（升级/降级）。从更新详情或内容行两种入口进来。 */
  const openVersionManager = (
    target: download.ContentUpdateFile | content.GameContentEntry | null,
  ) => {
    if (!target) return;
    const filePath = "FilePath" in target ? target.FilePath : target.SourcePath;
    const fileName = "FileName" in target ? target.FileName : target.Name;
    const projectName = "ProjectName" in target ? target.ProjectName || "" : "";
    const kindLabel =
      "KindLabel" in target
        ? target.KindLabel
        : (CONTENT_TABS.find((tab) => tab.key === contentTab)?.label ?? "");

    setUpdateDetail(null);
    setVersionTarget({
      FilePath: filePath,
      FileName: fileName,
      ProjectName: projectName,
      KindLabel: kindLabel,
    });
  };

  /** 版本切换成功：与"备份并替换"相同的刷新语义（详情重读 + 更新结论作废）。 */
  const onVersionSwitched = async (message: string) => {
    setStatus(message);
    alert(message, { severity: "success" });
    try {
      setDetails(await GetVersionDetails(selected));
    } catch {
      /* 列表刷新失败不打断提示 */
    }
    setUpdateResults((prev) => {
      const next = { ...prev };

      delete next[selected];

      return next;
    });
  };

  const exportSave = async (entry: content.GameContentEntry) => {
    let target = "";

    try {
      target = await SaveFile(
        t("导出存档"),
        `${entry.Name}.zip`,
        t("存档压缩包"),
        "*.zip",
      );
      if (!target) return;
      const saved = await ExportSave(entry.SourcePath, target);

      setStatus(t("存档已导出：{0}", { "0": saved }));
    } catch (ex) {
      setStatus(t("导出失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  const importSave = async () => {
    if (!details) return;
    let archive = "";

    try {
      archive = await SelectFile(t("导入存档"), t("存档压缩包"), "*.zip");
    } catch {
      /* 用户取消 */
    }
    if (!archive) return;
    setContentBusy("import-save");
    try {
      const imported = await ImportSave(
        archive,
        joinPath(details.ContentDirectory, "saves"),
      );

      setDetails(await GetVersionDetails(selected));
      setStatus(t("存档已导入：{0}", { "0": imported }));
    } catch (ex) {
      setStatus(t("导入失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setContentBusy("");
    }
  };

  const deleteSave = async (entry: content.GameContentEntry) => {
    try {
      await DeleteSave(entry.SourcePath);
      if (snap) setDetails(await GetVersionDetails(selected));
      setStatus(t("已删除存档 {0}。", { "0": entry.Name }));
    } catch (ex) {
      setStatus(t("删除失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  // Rewind 回滚后存档内容已变，刷新当前实例详情
  const refreshSelectedDetails = useCallback(async () => {
    if (!selected) return;
    try {
      setDetails(await GetVersionDetails(selected));
    } catch {
      /* 刷新失败不阻塞弹窗 */
    }
  }, [selected]);

  // ---------- 渲染辅助 ----------
  const detailRows = useMemo(() => {
    if (!details) return [];

    return [
      { label: t("基础版本"), value: details.BaseGameVersion || "Minecraft" },
      {
        label: t("加载器"),
        value: details.LoaderName
          ? `${details.LoaderName} ${details.LoaderVersion ?? ""}`
          : t("原版"),
      },
      { label: t("版本类型"), value: details.VersionType },
      { label: t("发布时间"), value: formatTime(details.ReleaseTime) },
      {
        label: t("版本隔离"),
        value: details.IsIsolated
          ? t("已隔离（{0}）", { "0": details.LayoutProvider })
          : t("共享目录"),
      },
      { label: t("内容目录"), value: details.ContentDirectory },
      { label: t("Java 要求"), value: details.JavaRequirement },
    ];
  }, [details]);

  // 渲染实例图标：自定义/实例自带文件 → /localfile 图片；gameicon: 符号 →
  // public/instance-icons 下的内置图片（缺失回落 emoji）；均加载失败回落 glyph
  const renderInstanceIcon = (versionId: string, size: string) => {
    const visual = visuals[versionId];
    const broken = !!brokenIcons[versionId];
    const path = visual?.IconPath ?? "";
    const url = instanceIconUrl(visual, broken);
    const gameicon =
      !url && !broken && path.startsWith("gameicon:")
        ? gameiconUrl(path.slice("gameicon:".length))
        : null;

    if (url || gameicon) {
      // 内置图标比例不一（方/宽/窄），用 contain 完整显示；自定义截图用 cover
      return (
        <div
          className={`${size} flex-shrink-0 items-center justify-center overflow-hidden rounded-md ${gameicon ? "" : "bg-gray-200 dark:bg-gray-700"}`}
        >
          <img
            alt=""
            className={`h-full w-full ${gameicon ? "object-contain" : "object-cover"}`}
            src={(url ?? gameicon) as string}
            onError={() =>
              setBrokenIcons((prev) => ({ ...prev, [versionId]: true }))
            }
          />
        </div>
      );
    }

    return (
      <div
        className={`${size} flex-shrink-0 flex items-center justify-center rounded-md bg-blue-100 dark:bg-blue-900/50 text-blue-600 dark:text-blue-300 text-sm`}
      >
        {instanceIconGlyph(visual, versionId)}
      </div>
    );
  };

  const contentCount = (list?: content.GameContentEntry[]) =>
    (list ?? []).length;

  return (
    <div className="relative h-full w-full flex flex-col overflow-hidden">
      {/* 拖拽安装的视觉提示与落点处理都在全局 FileDropOverlay（任何页面拖入都生效） */}
      {/* ==== 指挥横幅：目录选择 + 全部管理动作 ==== */}
      <div className="px-6 pt-5 pb-2 flex-shrink-0">
        <div className="rounded-large border nya-border nya-panel px-4 py-3 flex flex-wrap items-center gap-x-4 gap-y-2">
          {/* 左：游戏目录上下文 + 扫描状态 */}
          <div className="flex flex-wrap items-center gap-1.5">
            <Select
              aria-label={t("游戏目录")}
              className="min-w-[200px] max-w-[320px]"
              items={folders.map((f) => ({ key: f }))}
              popoverProps={selectPopoverProps}
              selectedKeys={
                snap?.MinecraftDirectory ? [snap.MinecraftDirectory] : []
              }
              size="sm"
              variant="bordered"
              onSelectionChange={(keys) =>
                void onFolderChange(String(Array.from(keys)[0] ?? ""))
              }
            >
              {(item) => <SelectItem key={item.key}>{item.key}</SelectItem>}
            </Select>
            <Button
              isIconOnly
              aria-label={t("添加目录")}
              size="sm"
              title={t("添加游戏目录")}
              variant="flat"
              onPress={() => void addFolder()}
            >
              ＋
            </Button>
            {loading ? (
              <Chip size="sm" variant="flat">
                {t("正在扫描实例…")}
              </Chip>
            ) : null}
            {snap?.ErrorMessage ? (
              <Chip color="danger" size="sm" variant="flat">
                {t("扫描失败")}
              </Chip>
            ) : null}
          </div>

          <div className="flex-1" />

          {/* 右：全局管理动作 */}
          <div className="flex flex-wrap items-center gap-1.5">
            <Button
              size="sm"
              startContent={<ImportIcon />}
              variant="flat"
              onPress={() => void openImport()}
            >
              {t("导入其他启动器")}
            </Button>
            <Button
              size="sm"
              startContent={<RefreshIcon />}
              variant="flat"
              onPress={() => void reloadAll()}
            >
              {t("重新扫描")}
            </Button>
            <Button
              size="sm"
              startContent={<FolderIcon />}
              variant="flat"
              onPress={() => void openGameFolder()}
            >
              {t("打开游戏目录")}
            </Button>
            <Button
              isDisabled={!snap?.MinecraftDirectory}
              size="sm"
              startContent={<RewindIcon />}
              variant="flat"
              onPress={() => {
                if (!snap?.MinecraftDirectory) return;
                setRewindEntry({
                  kind: "instance",
                  name: snap.SelectedVersionId || snap.MinecraftDirectory,
                  path: snap.MinecraftDirectory,
                });
              }}
            >
              Rewind
            </Button>
            {/* 插件页面按钮插槽：没有插件注册时组件直接返回 null，不占位 */}
            <PageActionSlot pageId="instances" />
          </div>
        </div>
      </div>

      {/* 指挥中心单页流：横幅之下改为左右两列布局 */}
      <div className="flex min-h-0 flex-1 gap-4 px-6 pb-5">
        {/* 左列：实例列表面板 */}
        <div className="flex w-[360px] min-h-0 flex-shrink-0 flex-col rounded-large border nya-border nya-panel p-2">
          <div className="flex items-center gap-2 px-2 pb-1.5 pt-1">
            <span className="text-[11px] text-gray-400">
              {filteredVersions.length} / {versions.length}
            </span>
            <div className="flex-1" />
            <Input
              aria-label={t("搜索实例")}
              className="w-40 max-w-full"
              size="sm"
              startContent={<Search20Regular className="h-4 w-4" />}
              value={versionSearch}
              variant="flat"
              onValueChange={setVersionSearch}
            />
          </div>
          <div
            ref={listRef}
            className="relative flex-1 overflow-y-auto flex flex-col gap-0.5 outline-none"
            // 显式 listbox 角色：这个列表支持 ↑/↓ 键切换与双击（a11y 要求）
            role="listbox"
            tabIndex={0}
            onDoubleClick={(e) => {
              // 双击列表空白处 → 去下载页装新实例
              if (e.target === e.currentTarget) navigateToPage("download");
            }}
            onKeyDown={handleListKeyDown}
          >
            {highlight ? (
              <motion.div
                key={`highlight-${selected}`}
                aria-hidden
                animate={{
                  opacity: 1,
                  x: 0,
                  transition: {
                    opacity: { duration: 0.2, delay: rowsReady ? 0.26 : 0 },
                    x: {
                      duration: 0.3,
                      delay: rowsReady ? 0.26 : 0,
                      ease: [0.22, 1, 0.36, 1],
                    },
                  },
                }}
                className="nya-instance-pill pointer-events-none absolute left-0 right-0 z-0 overflow-hidden rounded-lg bg-blue-100/70 dark:bg-blue-900/30"
                initial={{ opacity: 0, x: -20 }}
                style={{ height: highlight.height, top: highlight.top }}
              >
                {/* 无卡列表的选中态：一条左侧强调线，不靠阴影抬升 */}
                <span className="absolute inset-y-0 left-0 w-[3px] bg-blue-500" />
              </motion.div>
            ) : null}
            <AnimatePresence initial={false}>
              {filteredVersions.map((v) => (
                <motion.div
                  key={v}
                  layout
                  animate="center"
                  exit="exit"
                  initial="enter"
                  variants={LIST_FADE}
                >
                  {/* 高度折叠交给 CSS grid（0fr ↔ 1fr）：行高由布局引擎在每次
                      布局时按内容真实高度重算，不会像 framer 的 height: 0→auto
                      那样把"动画起点测得的旧高度"写死成内联 height——异步返回的
                      自定义名会让内容变高，旧写法会把条目下半截裁掉。
                      min-h-0 是 0fr 能真正塌陷到底的前提。 */}
                  <div
                    className={`grid transition-[grid-template-rows] duration-[250ms] ease-[cubic-bezier(0.22,1,0.36,1)] ${
                      rowsReady ? "grid-rows-[1fr]" : "grid-rows-[0fr]"
                    }`}
                  >
                    <div className="min-h-0 overflow-hidden">
                      <button
                        ref={(el) => {
                          itemRefs.current[v] = el;
                        }}
                        className={`relative flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left cursor-pointer ${
                          v === selected
                            ? "font-semibold text-blue-600 dark:text-blue-300"
                            : "text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800"
                        }`}
                        onClick={() => {
                          setExpandedInstance(true);
                          if (snap)
                            void selectVersion(v, snap.MinecraftDirectory);
                        }}
                        onContextMenu={(e) => openInstanceMenu(v, e)}
                      >
                        <span className="relative z-10 flex flex-shrink-0">
                          {renderInstanceIcon(v, "w-7 h-7")}
                        </span>
                        {/* 主显示实例自己的名称；版本号降级为次行小字 */}
                        <span className="relative z-10 min-w-0 flex-1">
                          <span className="block truncate text-[13px] leading-tight">
                            {v}
                          </span>
                          {displayNames[v] && displayNames[v] !== v ? (
                            <span className="block truncate text-[10px] leading-tight font-normal text-gray-400">
                              {displayNames[v]}
                            </span>
                          ) : null}
                        </span>
                      </button>
                    </div>
                  </div>
                </motion.div>
              ))}
            </AnimatePresence>
            {!loading && versions.length === 0 ? (
              <div className="px-2 py-6 text-center text-xs">
                {snap?.ErrorMessage ? (
                  // 扫描失败和"确实没装实例"要分得开：以前两者都显示"没有已安装的实例"
                  <span className="text-danger">
                    {t("扫描实例失败：{0}", { "0": snap.ErrorMessage })}
                  </span>
                ) : (
                  <span className="text-gray-400">{t("没有已安装的实例")}</span>
                )}
              </div>
            ) : !loading && filteredVersions.length === 0 ? (
              <div className="px-2 py-6 text-center text-xs text-gray-400">
                {t("没有名称匹配的实例")}
              </div>
            ) : null}
          </div>
        </div>

        {/* 右列：实例详情面板 */}
        {selected && expandedInstance ? (
          <div className="nya-border flex min-w-0 flex-1 flex-col overflow-hidden rounded-xl border px-4 py-3">
            <SwitchTransition
              activeKey={selected}
              className="flex min-h-0 flex-1 flex-col"
              direction={instanceDirection}
              variant="instance"
            >
              {!details ? (
                <div className="flex flex-1 items-center justify-center gap-2 text-sm text-gray-400">
                  <Spinner size="sm" /> {t("正在读取实例详情…")}
                </div>
              ) : (
                <div className="nya-instance-stagger flex min-h-0 flex-1 flex-col overflow-hidden">
                  {/* 头部信息区域 */}
                  <div className="nya-border mb-3 flex flex-shrink-0 items-center gap-3 border-b pb-3">
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-lg font-semibold text-gray-800 dark:text-gray-200">
                        {selected}
                      </div>
                      <div className="truncate text-[11px] text-gray-400">
                        {displayNames[selected] ||
                          details.BaseGameVersion ||
                          "Minecraft"}
                        {details.IsExternallyManaged
                          ? t(" · 外部启动器实例")
                          : ""}
                      </div>
                    </div>
                    {selected !== snap?.SelectedVersionId ? (
                      <Button
                        color="primary"
                        size="sm"
                        variant="flat"
                        onPress={() => void SelectInstance(selected)}
                      >
                        {t("设为当前")}
                      </Button>
                    ) : (
                      <Chip color="primary" size="sm" variant="flat">
                        {t("当前实例")}
                      </Chip>
                    )}
                    <Button
                      size="sm"
                      startContent={<FolderIcon />}
                      variant="flat"
                      onPress={() => void openVersionFolder()}
                    >
                      {t("打开文件夹")}
                    </Button>
                    <Button
                      aria-label={t("收起详情")}
                      size="sm"
                      startContent={<ChevronUpIcon />}
                      variant="flat"
                      onPress={() => setExpandedInstance(false)}
                    >
                      {t("收起")}
                    </Button>
                  </div>

                  {/* 标签页导航 */}
                  <div className="flex flex-shrink-0 items-center gap-5 pb-1">
                    {(
                      [
                        ["overview", t("概览")],
                        ["launch", t("启动设置")],
                        ["java", t("Java 设置")],
                        ["content", t("内容")],
                        ["gamesettings", t("游戏设置")],
                        ["manage", t("管理")],
                      ] as const
                    ).map(([key, label]) => (
                      <button
                        key={key}
                        className={`cursor-pointer border-b-2 px-0.5 pt-1 pb-2 text-[13px] transition-colors ${
                          tab === key
                            ? "border-blue-500 font-semibold text-blue-600 dark:text-blue-300"
                            : "border-transparent text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100"
                        }`}
                        onClick={() => setTab(key)}
                      >
                        {label}
                      </button>
                    ))}
                  </div>

                  {/* 标签页内容区域 */}
                  <div className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto pr-1">
                    <SwitchTransition
                      activeKey={tab}
                      className="flex min-h-full flex-col"
                      direction={tabDirection}
                    >
                      {/* ===== 概览 ===== */}
                      {tab === "overview" && (
                        <div className="flex flex-col gap-1.5 pt-2">
                          {detailRows.map((row) => (
                            <div
                              key={row.label}
                              className="grid grid-cols-[130px_1fr] gap-3"
                            >
                              <span className="text-[13px] text-gray-400">
                                {row.label}
                              </span>
                              <span className="break-all text-[13px] text-gray-700 dark:text-gray-200">
                                {row.value || "—"}
                              </span>
                            </div>
                          ))}
                          <div className="grid grid-cols-[130px_1fr] gap-3 pt-1">
                            <span className="text-[13px] text-gray-400">
                              {t("内容统计")}
                            </span>
                            <span className="text-[13px] text-gray-700 dark:text-gray-200">
                              {t("模组")} {contentCount(details.Mods)}{" "}
                              {t("· 资源包")}{" "}
                              {contentCount(details.ResourcePacks)}{" "}
                              {t("· 光影")} {contentCount(details.Shaders)}{" "}
                              {t("· 存档")} {contentCount(details.Saves)}
                            </span>
                          </div>
                        </div>
                      )}

                      {/* ===== 启动设置 ===== */}
                      {tab === "launch" && profile && (
                        <div className="flex flex-col gap-3 pt-2">
                          {/* 窗口设置 */}
                          <div className="nya-border rounded-lg border p-3">
                            <div className="mb-2 text-[13px] font-medium text-gray-700 dark:text-gray-200">
                              {t("窗口设置")}
                            </div>
                            <div className="mb-3 grid grid-cols-2 gap-3">
                              <div className="flex flex-col gap-1.5">
                                <label className="text-[13px] text-gray-600 dark:text-gray-300">
                                  {t("窗口宽度")}
                                </label>
                                <Input
                                  placeholder="1920"
                                  size="sm"
                                  type="number"
                                  value={
                                    profile.WindowWidth > 0
                                      ? String(profile.WindowWidth)
                                      : ""
                                  }
                                  variant="bordered"
                                  onValueChange={(v) => {
                                    const num = parseInt(v, 10);

                                    setProfile({
                                      ...profile,
                                      WindowWidth: isNaN(num) ? 0 : num,
                                    });
                                  }}
                                />
                              </div>
                              <div className="flex flex-col gap-1.5">
                                <label className="text-[13px] text-gray-600 dark:text-gray-300">
                                  {t("窗口高度")}
                                </label>
                                <Input
                                  placeholder="1080"
                                  size="sm"
                                  type="number"
                                  value={
                                    profile.WindowHeight > 0
                                      ? String(profile.WindowHeight)
                                      : ""
                                  }
                                  variant="bordered"
                                  onValueChange={(v) => {
                                    const num = parseInt(v, 10);

                                    setProfile({
                                      ...profile,
                                      WindowHeight: isNaN(num) ? 0 : num,
                                    });
                                  }}
                                />
                              </div>
                            </div>
                            <label className="flex items-center gap-2 text-[13px] text-gray-700 dark:text-gray-200">
                              <Switch
                                isSelected={!!profile.LaunchFullscreen}
                                size="sm"
                                onValueChange={(v) =>
                                  setProfile({
                                    ...profile,
                                    LaunchFullscreen: v,
                                  })
                                }
                              />
                              {t("全屏启动")}
                            </label>
                          </div>

                          {/* 版本隔离 */}
                          <label className="flex items-center gap-2 text-[13px] text-gray-700 dark:text-gray-200">
                            <Switch
                              isSelected={
                                // 未显式设置（null）时回落到后端解析出的实际布局：
                                // 全局默认为隔离，直接把 null 当 false 会让开关与真实行为相反
                                profile.IsVersionIsolationEnabled ??
                                details?.IsIsolated ??
                                true
                              }
                              size="sm"
                              onValueChange={(v) =>
                                setProfile({
                                  ...profile,
                                  IsVersionIsolationEnabled: v,
                                })
                              }
                            />
                            {t("版本隔离（模组、存档等分到实例目录）")}
                          </label>

                          {/* ===== 高级设置（折叠） ===== */}
                          <div className="nya-border my-2 border-t" />

                          <div className="nya-border rounded-lg border">
                            <button
                              className="flex w-full cursor-pointer items-center justify-between rounded px-4 py-3 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-gray-800"
                              type="button"
                              onClick={() =>
                                setAdvancedSettingsExpanded(
                                  !advancedSettingsExpanded,
                                )
                              }
                            >
                              <span className="flex items-center gap-2">
                                <span>🔧</span>
                                <span>{t("高级设置")}</span>
                                {!advancedSettingsExpanded ? (
                                  <span className="text-xs text-gray-400">
                                    {t("（进程优先级、包装命令、环境变量等）")}
                                  </span>
                                ) : null}
                              </span>
                              {advancedSettingsExpanded ? (
                                <ChevronUpIcon className="h-5 w-5" />
                              ) : (
                                <ChevronDownIcon className="h-5 w-5" />
                              )}
                            </button>

                            {advancedSettingsExpanded ? (
                              <div className="nya-border flex flex-col gap-3 border-t px-4 pt-3 pb-4">
                                {/* 警告 */}
                                <div className="nya-border rounded-lg border bg-yellow-500/10 px-4 py-3">
                                  <div className="flex items-start gap-3">
                                    <span className="mt-0.5 text-yellow-600">
                                      ⚠️
                                    </span>
                                    <div className="flex-1 text-xs leading-relaxed text-gray-600 dark:text-gray-300">
                                      {t(
                                        "以下选项需要一定技术了解，不确定时请保持默认设置。",
                                      )}
                                    </div>
                                  </div>
                                </div>

                                {/* 跟随全局设置 */}
                                <label className="flex items-center gap-2 text-[13px] text-gray-700 dark:text-gray-200">
                                  <Switch
                                    isSelected={
                                      !!profile.FollowGlobalAdvancedSettings
                                    }
                                    size="sm"
                                    onValueChange={(v) =>
                                      setProfile({
                                        ...profile,
                                        FollowGlobalAdvancedSettings: v,
                                      })
                                    }
                                  />

                                  {t(
                                    "跟随全局高级启动设置（关闭后才能自定义）",
                                  )}
                                </label>

                                {/* 进程优先级 */}
                                <div className="flex flex-col gap-1.5">
                                  <label className="text-[13px] text-gray-600 dark:text-gray-300">
                                    {t("进程优先级（Windows）")}
                                  </label>
                                  <Select
                                    isDisabled={
                                      !!profile.FollowGlobalAdvancedSettings
                                    }
                                    popoverProps={selectPopoverProps}
                                    selectedKeys={[
                                      profile.ProcessPriority || "normal",
                                    ]}
                                    size="sm"
                                    onSelectionChange={(keys) =>
                                      setProfile({
                                        ...profile,
                                        ProcessPriority:
                                          (Array.from(keys)[0] as string) ||
                                          "normal",
                                      })
                                    }
                                  >
                                    {priorityKeys.map((key) => (
                                      <SelectItem key={key}>
                                        {priorityLabel(key)}
                                      </SelectItem>
                                    ))}
                                  </Select>
                                </div>

                                {/* 包装命令 */}
                                <div className="flex flex-col gap-1.5">
                                  <label className="text-[13px] text-gray-600 dark:text-gray-300">
                                    {t("包装命令")}
                                  </label>
                                  <Input
                                    isDisabled={
                                      !!profile.FollowGlobalAdvancedSettings
                                    }
                                    placeholder="gamemoderun %command%"
                                    size="sm"
                                    value={profile.WrapperCommand ?? ""}
                                    variant="bordered"
                                    onValueChange={(v) =>
                                      setProfile({
                                        ...profile,
                                        WrapperCommand: v,
                                      })
                                    }
                                  />
                                </div>

                                {/* 环境变量 */}
                                <div>
                                  <div className="mb-1 text-[13px] text-gray-600 dark:text-gray-300">
                                    {t(
                                      "附加环境变量（每行一个，格式 KEY=VALUE）",
                                    )}
                                  </div>
                                  <Textarea
                                    isDisabled={
                                      !!profile.FollowGlobalAdvancedSettings
                                    }
                                    minRows={2}
                                    size="sm"
                                    value={envText}
                                    variant="bordered"
                                    onValueChange={setEnvText}
                                  />
                                </div>

                                {/* 参数溯源 */}
                                <div className="nya-border flex items-start gap-2.5 border-t pt-3">
                                  <span className="mt-0.5 flex-none text-primary">
                                    <ProvenanceIcon />
                                  </span>
                                  <div className="min-w-0 flex-1">
                                    <div className="text-[13px] font-medium text-gray-700 dark:text-gray-200">
                                      {t("启动参数是怎么来的？")}
                                    </div>
                                    <div className="mt-0.5 text-xs leading-relaxed text-gray-500 dark:text-gray-400">
                                      {t(
                                        "逐条列出上次启动时每个参数由谁添加，以及哪些被后面的同名参数覆盖。",
                                      )}
                                    </div>
                                  </div>
                                  <Button
                                    className="flex-none"
                                    size="sm"
                                    variant="flat"
                                    onPress={openProvenance}
                                  >
                                    {t("参数溯源")}
                                  </Button>
                                </div>
                              </div>
                            ) : null}
                          </div>

                          {/* 保存 */}
                          <div className="nya-border my-2 border-t" />

                          <div className="flex items-center gap-2">
                            <Button
                              color="primary"
                              isLoading={saving}
                              size="sm"
                              onPress={() => void saveProfile()}
                            >
                              {t("保存启动设置")}
                            </Button>
                          </div>
                        </div>
                      )}

                      {/* ===== Java 设置 ===== */}
                      {tab === "java" && (
                        <div className="flex flex-col gap-4 pt-2">
                          <div className="text-[11px] font-semibold uppercase tracking-wider text-gray-400">
                            {t("Java 运行时")}
                          </div>

                          <div className="flex flex-col gap-2">
                            <label className="text-xs text-gray-600 dark:text-gray-300">
                              {t("Java 可执行文件路径")}
                            </label>
                            <Input
                              classNames={{
                                inputWrapper: "bg-default-100/60",
                              }}
                              placeholder={t("留空使用全局设置或自动检测")}
                              size="sm"
                              value={javaConfig.JavaExecutable}
                              onValueChange={(v) =>
                                setJavaConfig({
                                  ...javaConfig,
                                  JavaExecutable: v,
                                })
                              }
                            />
                            <div className="text-[10px] text-gray-400">
                              {t(
                                "例如: C:\\Program Files\\Java\\jdk-21\\bin\\javaw.exe",
                              )}
                            </div>
                          </div>

                          <div className="nya-border my-2 border-t" />

                          <div className="text-[11px] font-semibold uppercase tracking-wider text-gray-400">
                            {t("内存设置")}
                          </div>

                          <div className="flex flex-col gap-2">
                            <label className="text-xs text-gray-600 dark:text-gray-300">
                              {t("最小内存（-Xms）")}
                            </label>
                            <div className="flex items-center gap-3">
                              <Input
                                className="w-32"
                                classNames={{
                                  inputWrapper: "bg-default-100/60",
                                  input: "text-right",
                                }}
                                endContent={
                                  <span className="text-xs text-gray-400">
                                    MB
                                  </span>
                                }
                                placeholder="0"
                                size="sm"
                                type="number"
                                value={
                                  javaConfig.MinMemoryMB > 0
                                    ? javaConfig.MinMemoryMB.toString()
                                    : ""
                                }
                                onValueChange={(v) =>
                                  setJavaConfig({
                                    ...javaConfig,
                                    MinMemoryMB: parseInt(v) || 0,
                                  })
                                }
                              />
                              <span className="text-xs text-gray-400">
                                {t("0 = 使用全局设置，推荐 512-2048 MB")}
                              </span>
                            </div>
                          </div>

                          <div className="flex flex-col gap-2">
                            <label className="text-xs text-gray-600 dark:text-gray-300">
                              {t("最大内存（-Xmx）")}
                            </label>
                            <div className="flex items-center gap-3">
                              <Input
                                className="w-32"
                                classNames={{
                                  inputWrapper: "bg-default-100/60",
                                  input: "text-right",
                                }}
                                endContent={
                                  <span className="text-xs text-gray-400">
                                    MB
                                  </span>
                                }
                                placeholder="0"
                                size="sm"
                                type="number"
                                value={
                                  javaConfig.MaxMemoryMB > 0
                                    ? javaConfig.MaxMemoryMB.toString()
                                    : ""
                                }
                                onValueChange={(v) => {
                                  // 钳到物理内存上限，避免配出用不了的 -Xmx
                                  const n = parseInt(v) || 0;

                                  setJavaConfig({
                                    ...javaConfig,
                                    MaxMemoryMB:
                                      n > 0 ? Math.min(n, memoryMax) : 0,
                                  });
                                }}
                              />
                              <span className="text-xs text-gray-400">
                                {t("0 = 使用全局设置，推荐 4096-8192 MB")}
                              </span>
                            </div>
                          </div>

                          <div className="nya-border my-2 border-t" />

                          <div className="text-[11px] font-semibold uppercase tracking-wider text-gray-400">
                            {t("JVM 参数")}
                          </div>

                          <div className="flex flex-col gap-2">
                            <label className="text-xs text-gray-600 dark:text-gray-300">
                              {t("附加 JVM 参数（每行一个）")}
                            </label>
                            <Textarea
                              classNames={{
                                inputWrapper: "bg-default-100/60",
                              }}
                              minRows={5}
                              placeholder={t(
                                "例如:\n-XX:+UseG1GC\n-XX:MaxGCPauseMillis=50\n-Dfml.readTimeout=180",
                              )}
                              value={javaConfigJvmText}
                              onValueChange={setJavaConfigJvmText}
                            />
                            <div className="text-[10px] text-gray-400">
                              {t(
                                "这些参数会追加到启动器自动优化参数和全局设置之后",
                              )}
                            </div>
                          </div>

                          <div className="nya-border my-2 border-t" />

                          <div className="text-[11px] font-semibold uppercase tracking-wider text-gray-400">
                            {t("游戏参数")}
                          </div>

                          <div className="flex flex-col gap-2">
                            <label className="text-xs text-gray-600 dark:text-gray-300">
                              {t("附加游戏参数（每行一个）")}
                            </label>
                            <Textarea
                              classNames={{
                                inputWrapper: "bg-default-100/60",
                              }}
                              minRows={3}
                              placeholder={t(
                                "例如:\n--fullscreen\n--width 1920\n--height 1080",
                              )}
                              value={javaConfigGameText}
                              onValueChange={setJavaConfigGameText}
                            />
                          </div>

                          <div className="nya-border my-2 border-t" />

                          <div className="flex items-center gap-2">
                            <Button
                              color="primary"
                              isLoading={javaSaving}
                              size="sm"
                              onPress={() => void saveJavaConfig()}
                            >
                              {t("保存 Java 设置")}
                            </Button>
                            <Button
                              size="sm"
                              variant="flat"
                              onPress={() => void resetJavaConfig()}
                            >
                              {t("重置为默认")}
                            </Button>
                            <div className="flex-1" />
                            <span className="text-[10px] text-gray-400">
                              {t("配置文件: java_config.yaml")}
                            </span>
                          </div>
                        </div>
                      )}

                      {/* ===== 内容 ===== */}
                      {tab === "content" && (
                        <div className="flex min-h-full flex-col pt-2">
                          <div className="flex flex-wrap items-center gap-2 pb-2">
                            <div className="flex flex-wrap items-center gap-4">
                              {CONTENT_TABS.map((tab) => (
                                <button
                                  key={tab.key}
                                  className={`cursor-pointer py-1 text-xs transition-colors ${
                                    contentTab === tab.key
                                      ? "font-semibold text-gray-900 dark:text-gray-100"
                                      : "text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
                                  }`}
                                  onClick={() => setContentTab(tab.key)}
                                >
                                  {t(tab.label)}
                                </button>
                              ))}
                            </div>
                            <div
                              ref={contentSearchRef}
                              className="flex min-w-0 flex-1"
                            >
                              <Input
                                aria-label={t("搜索{0}名称…", {
                                  "0": t(contentTab),
                                })}
                                className="min-w-[140px] w-full flex-1"
                                placeholder={t("搜索{0}名称…（Ctrl+F）", {
                                  "0": t(contentTab),
                                })}
                                size="sm"
                                value={contentSearch}
                                variant="bordered"
                                onValueChange={setContentSearch}
                              />
                            </div>
                            {contentTab === "游戏存档" ? (
                              <Button
                                className="flex-shrink-0"
                                isLoading={contentBusy === "import-save"}
                                size="sm"
                                startContent={<ImportIcon />}
                                variant="flat"
                                onPress={() => void importSave()}
                              >
                                {t("导入存档")}
                              </Button>
                            ) : (
                              <Button
                                className="flex-shrink-0"
                                isDisabled={updateChecking}
                                isLoading={updateChecking}
                                size="sm"
                                startContent={
                                  updateChecking ? undefined : <UpdateIcon />
                                }
                                title={t(
                                  "用文件哈希向 Modrinth 查询这些内容是否有新版本（不会自动改动文件）",
                                )}
                                variant="flat"
                                onPress={() => void checkContentUpdates()}
                              >
                                {t("检查更新")}
                              </Button>
                            )}
                            <span className="flex-shrink-0 text-xs text-gray-400">
                              {contentSummary}
                            </span>
                          </div>
                          {/* 更新检测结论：摘要 + 进度 + 整合包清单比对 */}
                          {contentTab !== "游戏存档" &&
                          (updateChecking || updateProgress || updateResult) ? (
                            <div className="flex flex-wrap items-center gap-2 pb-2 text-[11px]">
                              {updateChecking ? <Spinner size="sm" /> : null}
                              <span className="text-gray-500 dark:text-gray-400">
                                {updateChecking
                                  ? updateProgress || t("正在检查更新…")
                                  : summarizeResult(updateResult, t)}
                              </span>
                              {/* 一键全部更新：有可更新文件时出现（预发布默认跳过） */}
                              {!updateChecking &&
                              bulkUpdatableFiles.length > 0 ? (
                                <Button
                                  className="min-w-0 h-6 px-2.5"
                                  color="primary"
                                  isDisabled={bulkUpdating}
                                  isLoading={bulkUpdating}
                                  size="sm"
                                  variant="flat"
                                  onPress={() => void updateAll()}
                                >
                                  {t("全部更新（{0}）", {
                                    "0": bulkUpdatableFiles.length,
                                  })}
                                </Button>
                              ) : null}
                              {!updateChecking && skippedPrereleaseCount > 0 ? (
                                <span className="text-gray-400">
                                  {t("已跳过 {0} 个预发布版本", {
                                    "0": skippedPrereleaseCount,
                                  })}
                                </span>
                              ) : null}
                              {updateResult?.Modpack?.Present ? (
                                <button
                                  className="cursor-pointer text-left text-gray-500 underline decoration-dotted dark:text-gray-400"
                                  title={t("查看整合包清单比对明细")}
                                  onClick={() =>
                                    alert(
                                      t(
                                        updateResult.Modpack.StatusText ??
                                          "整合包清单比对没有结论。",
                                      ),
                                      updateResult.Modpack.Status === "issues"
                                        ? { severity: "warning" }
                                        : { severity: "info" },
                                    )
                                  }
                                >
                                  {t("整合包清单：{0}", {
                                    "0": t(
                                      updateResult.Modpack.StatusText ??
                                        "无结论",
                                    ),
                                  })}
                                </button>
                              ) : null}
                            </div>
                          ) : null}
                          <SwitchTransition
                            activeKey={contentTab}
                            className="flex flex-1 flex-col"
                            direction={contentDirection}
                          >
                            {filteredContent.length === 0 ? (
                              <div className="flex flex-1 items-center justify-center text-[13px] text-gray-400">
                                {t("没有匹配的")}
                                {t(contentTab)}
                              </div>
                            ) : (
                              <div className="flex flex-1 flex-col gap-1">
                                {filteredContent.map((entry) => (
                                  <div
                                    key={entry.SourcePath}
                                    className="flex items-center gap-3 rounded-lg px-3 py-2 transition-colors hover:bg-primary/10"
                                    onContextMenu={(e) => {
                                      if (contentTab === "已安装模组")
                                        openModMenu(entry, e);
                                    }}
                                  >
                                    <span className="w-8 h-8 flex-shrink-0 flex items-center justify-center rounded-lg bg-gray-200 dark:bg-gray-700">
                                      {entry.IconPath ? (
                                        <img
                                          alt=""
                                          className="h-full w-full object-cover"
                                          src={
                                            toLocalFileUrl(entry.IconPath) ?? ""
                                          }
                                        />
                                      ) : (
                                        entry.FallbackGlyph || "📦"
                                      )}
                                    </span>
                                    <div className="min-w-0 flex-1">
                                      {(() => {
                                        // 展示格式统一为 "(中文名) 原名"：
                                        // 标题与文件名是同一个字符串。
                                        const zhName =
                                          contentTab === "已安装模组"
                                            ? modNames[entry.Name]
                                            : undefined;
                                        const displayName = zhName
                                          ? `(${zhName}) ${entry.Name}`
                                          : entry.Name;

                                        return (
                                          <>
                                            <div
                                              className={`truncate text-[13px] font-semibold text-gray-800 dark:text-gray-200 ${entry.IsDisabled ? "line-through opacity-60" : ""}`}
                                              title={displayName}
                                            >
                                              {displayName}
                                            </div>
                                            <div className="truncate text-[11px] text-gray-400">
                                              {entry.MetadataLine}
                                            </div>
                                          </>
                                        );
                                      })()}
                                    </div>
                                    {/* 更新角标：可更新 / 未知 / 检查失败 各有区分，
                                        点击打开明细弹层 */}
                                    {contentTab !== "游戏存档" &&
                                    shouldShowBadge(updateFor(entry)) ? (
                                      <button
                                        className="flex-shrink-0 cursor-pointer"
                                        title={t(
                                          updateFor(entry)?.StatusText ?? "",
                                        )}
                                        onClick={() =>
                                          setUpdateDetail(updateFor(entry))
                                        }
                                      >
                                        <Chip
                                          color={
                                            isUpdatable(updateFor(entry))
                                              ? "warning"
                                              : badgeToneFor(
                                                    updateFor(entry),
                                                  ) === "failed"
                                                ? "danger"
                                                : "default"
                                          }
                                          size="sm"
                                          variant="flat"
                                        >
                                          {isUpdatable(updateFor(entry))
                                            ? t("可更新 {0}", {
                                                "0":
                                                  updateFor(entry)
                                                    ?.LatestVersion ??
                                                  t("新版本"),
                                              })
                                            : badgeToneFor(updateFor(entry)) ===
                                                "failed"
                                              ? t("检查失败")
                                              : t("未知")}
                                        </Chip>
                                      </button>
                                    ) : null}
                                    {contentTab === "游戏存档" ? (
                                      <div className="flex flex-shrink-0 items-center gap-1.5">
                                        <Button
                                          size="sm"
                                          startContent={<RewindIcon />}
                                          variant="flat"
                                          onPress={() =>
                                            setRewindEntry({
                                              kind: "save",
                                              name: entry.Name,
                                              path: entry.SourcePath,
                                            })
                                          }
                                        >
                                          Rewind
                                        </Button>
                                        <Button
                                          size="sm"
                                          variant="flat"
                                          onPress={() => void exportSave(entry)}
                                        >
                                          {t("导出")}
                                        </Button>
                                        <Button
                                          color="danger"
                                          size="sm"
                                          variant="flat"
                                          onPress={() => void deleteSave(entry)}
                                        >
                                          {t("删除")}
                                        </Button>
                                      </div>
                                    ) : (
                                      <div className="flex flex-shrink-0 items-center gap-2">
                                        <Button
                                          isIconOnly
                                          aria-label={t("版本管理")}
                                          size="sm"
                                          title={t("版本管理（升级 / 降级）")}
                                          variant="light"
                                          onPress={() =>
                                            openVersionManager(entry)
                                          }
                                        >
                                          <VersionIcon />
                                        </Button>
                                        <span className="text-[10px] text-gray-400">
                                          {entry.IsDisabled
                                            ? t("已禁用")
                                            : t("已启用")}
                                        </span>
                                        <Switch
                                          aria-label={t("启用或禁用")}
                                          isDisabled={
                                            contentBusy === entry.SourcePath
                                          }
                                          isSelected={!entry.IsDisabled}
                                          size="sm"
                                          onValueChange={(v) =>
                                            void toggleContent(entry, v)
                                          }
                                        />
                                      </div>
                                    )}
                                  </div>
                                ))}
                              </div>
                            )}
                          </SwitchTransition>
                        </div>
                      )}

                      {/* ===== 游戏设置 ===== */}
                      {tab === "gamesettings" && (
                        <div className="flex flex-col gap-4">
                          {/* 分类切换 + 操作按钮 */}
                          <div className="flex items-center justify-between">
                            <div className="flex flex-wrap gap-1">
                              {GAME_SETTING_CATEGORIES.map((cat) => (
                                <button
                                  key={cat}
                                  className={`rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                                    gameSettingCategory === cat
                                      ? "bg-primary text-primary-foreground"
                                      : "bg-default-100/60 text-gray-500 hover:bg-default-200/60"
                                  }`}
                                  onClick={() => setGameSettingCategory(cat)}
                                >
                                  {t(cat)}
                                </button>
                              ))}
                            </div>
                            <div className="flex items-center gap-2">
                              <Tabs
                                aria-label={t("游戏设置编辑模式")}
                                classNames={{
                                  cursor: "w-full",
                                }}
                                selectedKey={gameEditMode}
                                size="sm"
                                onSelectionChange={(key) => {
                                  if (key === "raw") {
                                    setRawOptionsText(gameOptionsRaw);
                                    setGameEditMode("raw");
                                  } else {
                                    if (
                                      gameEditMode === "raw" &&
                                      gameOptionsDirty
                                    ) {
                                      // 从raw同步回visual
                                      setGameOptions(
                                        parseOptionsFile(rawOptionsText),
                                      );
                                    }
                                    setGameEditMode("visual");
                                  }
                                }}
                              >
                                <Tab key="visual" title={t("可视化")} />
                                <Tab key="raw" title={t("原始编辑")} />
                              </Tabs>
                              <Button
                                size="sm"
                                startContent={
                                  <RefreshIcon className="h-4 w-4" />
                                }
                                variant="flat"
                                onPress={() => void loadGameOptions()}
                              >
                                {t("重新加载")}
                              </Button>
                              <Button
                                color="primary"
                                isDisabled={!gameOptionsDirty}
                                size="sm"
                                startContent={<SaveIcon className="h-4 w-4" />}
                                onPress={() => void saveGameOptions()}
                              >
                                {t("保存")}
                                {gameOptionsDirty && (
                                  <span className="ml-1 text-[10px] opacity-80">
                                    ●
                                  </span>
                                )}
                              </Button>
                            </div>
                          </div>

                          {/* 设置项列表 */}
                          {gameEditMode === "visual" && !gameOptionsLoaded ? (
                            <div className="flex items-center justify-center py-12 text-gray-400">
                              <Spinner size="sm" />
                              <span className="ml-2 text-sm">
                                {t("加载中...")}
                              </span>
                            </div>
                          ) : gameEditMode === "visual" ? (
                            <div className="grid grid-cols-1 gap-x-6 md:grid-cols-2">
                              {GAME_SETTINGS.filter(
                                (s) => s.category === gameSettingCategory,
                              ).map((setting) => {
                                const value = gameOptions[setting.key];

                                return (
                                  <div
                                    key={setting.key}
                                    className="nya-border flex flex-col gap-1.5 border-b py-3"
                                  >
                                    <div className="flex items-center justify-between">
                                      <span className="text-xs font-medium text-gray-600">
                                        {t(setting.label)}
                                      </span>
                                      {setting.type === "boolean" && (
                                        <Switch
                                          isSelected={value === "true"}
                                          size="sm"
                                          onValueChange={(v) =>
                                            setGameOption(
                                              setting.key,
                                              v ? "true" : "false",
                                            )
                                          }
                                        />
                                      )}
                                    </div>
                                    {setting.type === "slider" &&
                                      (() => {
                                        const scale = setting.valueScale ?? 1;
                                        const actualVal =
                                          value !== undefined
                                            ? parseFloat(value)
                                            : (setting.min ?? 0) / scale;
                                        const displayVal = actualVal * scale;

                                        return (
                                          <div className="flex items-center gap-3">
                                            <Slider
                                              showTooltip
                                              aria-label={t(setting.label)}
                                              classNames={{
                                                track: "bg-transparent!",
                                                filler: "bg-primary",
                                              }}
                                              color="primary"
                                              fillOffset={setting.min}
                                              getValue={(v) =>
                                                `${Math.round((Array.isArray(v) ? v[0] : v) * 100) / 100}${setting.suffix ?? ""}`
                                              }
                                              maxValue={setting.max ?? 100}
                                              minValue={setting.min ?? 0}
                                              size="sm"
                                              step={setting.step ?? 1}
                                              value={displayVal}
                                              onChange={(v) => {
                                                const display = Array.isArray(v)
                                                  ? v[0]
                                                  : v;

                                                setGameOption(
                                                  setting.key,
                                                  String(display / scale),
                                                );
                                              }}
                                            />
                                            <span className="w-16 flex-shrink-0 text-right text-xs tabular-nums text-gray-500">
                                              {Math.round(displayVal * 100) /
                                                100}
                                              {setting.suffix ?? ""}
                                            </span>
                                          </div>
                                        );
                                      })()}
                                    {setting.type === "select" && (
                                      <Select
                                        aria-label={t(setting.label)}
                                        className="w-full"
                                        placeholder={t("未设置")}
                                        popoverProps={selectPopoverProps}
                                        selectedKeys={value ? [value] : []}
                                        size="sm"
                                        variant="bordered"
                                        onSelectionChange={(keys) => {
                                          const key = Array.from(keys)[0];

                                          if (
                                            key !== undefined &&
                                            key !== null
                                          ) {
                                            setGameOption(
                                              setting.key,
                                              String(key),
                                            );
                                          }
                                        }}
                                      >
                                        {(setting.options ?? []).map((opt) => (
                                          <SelectItem key={opt.value}>
                                            {t(opt.label)}
                                          </SelectItem>
                                        ))}
                                      </Select>
                                    )}
                                    {setting.type === "text" && (
                                      <Input
                                        size="sm"
                                        value={value ?? ""}
                                        onValueChange={(v) =>
                                          setGameOption(setting.key, v)
                                        }
                                      />
                                    )}
                                  </div>
                                );
                              })}
                            </div>
                          ) : null}

                          {/* 原始编辑模式 */}
                          {gameEditMode === "raw" && (
                            <div className="flex flex-col gap-2">
                              <div className="text-[11px] text-gray-400">
                                {t(
                                  "直接编辑 options.txt 原始内容，支持所有设置项（包括 Mod 添加的自定义选项）",
                                )}
                              </div>
                              <Textarea
                                aria-label={t("options.txt 原始内容")}
                                classNames={{
                                  input:
                                    "font-mono text-[11px] leading-relaxed resize-y",
                                }}
                                maxRows={26}
                                minRows={18}
                                radius="lg"
                                spellCheck={false}
                                value={rawOptionsText}
                                variant="bordered"
                                onValueChange={(v) => {
                                  setRawOptionsText(v);
                                  setGameOptionsDirty(true);
                                }}
                              />
                            </div>
                          )}

                          {/* 提示 */}
                          <div className="text-[11px] text-gray-400">
                            {gameEditMode === "visual"
                              ? t("未列出的设置项可切换到「原始编辑」模式")
                              : t("格式为 key:value，每行一个")}
                          </div>
                        </div>
                      )}

                      {/* ===== 管理 ===== */}
                      {tab === "manage" && (
                        <div className="flex flex-col gap-4 pt-2">
                          <div>
                            <div className="mb-1.5 text-[13px] text-gray-600 dark:text-gray-300">
                              {t("实例图标")}
                            </div>
                            <div className="flex items-center gap-3">
                              <span className="flex-shrink-0">
                                {renderInstanceIcon(selected, "w-14 h-14")}
                              </span>
                              <div className="flex min-w-0 flex-1 flex-col gap-2">
                                {/* 内置加载器图标：点击即用 */}
                                <div className="flex flex-wrap gap-1.5">
                                  {BUILTIN_ICON_KEYS.map((key) => {
                                    const active =
                                      profile?.InstanceIconOverride ===
                                      `gameicon:${key}`;

                                    return (
                                      <button
                                        key={key}
                                        className={`flex h-9 w-9 cursor-pointer items-center justify-center overflow-hidden rounded-lg border transition-colors ${
                                          active
                                            ? "border-primary bg-primary/10"
                                            : "border-gray-200 hover:bg-gray-100 dark:border-gray-700 dark:hover:bg-gray-800"
                                        }`}
                                        title={t(GAMEICON_LABELS[key] ?? key)}
                                        onClick={() =>
                                          void applyIconOverride(
                                            `gameicon:${key}`,
                                          )
                                        }
                                      >
                                        <BuiltInIcon glyphKey={key} />
                                      </button>
                                    );
                                  })}
                                  {profile?.InstanceIconOverride ===
                                  "custom" ? (
                                    <span className="flex h-9 items-center rounded-lg border border-primary bg-primary/10 px-2 text-[11px] text-primary">
                                      {t("自定义图片")}
                                    </span>
                                  ) : null}
                                </div>
                                <div className="flex flex-wrap gap-2">
                                  <Button
                                    size="sm"
                                    variant="flat"
                                    onPress={() => void pickCustomIcon()}
                                  >
                                    {t("选择图片…")}
                                  </Button>
                                  <Button
                                    size="sm"
                                    variant="light"
                                    onPress={() => void resetInstanceIcon()}
                                  >
                                    {t("恢复自动")}
                                  </Button>
                                </div>
                                <div className="text-[11px] text-gray-400">
                                  {t("图片需 8MB 以内")}
                                </div>
                              </div>
                            </div>
                          </div>

                          <div>
                            <div className="mb-1.5 text-[13px] text-gray-600 dark:text-gray-300">
                              {t("实例名称（将重命名版本文件夹）")}
                            </div>
                            <div className="flex gap-2">
                              <Input
                                aria-label={t("实例名称（将重命名版本文件夹）")}
                                className="min-w-0 flex-1"
                                placeholder={t("输入新的实例名称")}
                                size="sm"
                                value={newName}
                                variant="bordered"
                                onValueChange={setNewName}
                              />
                              <Button
                                isDisabled={
                                  !newName.trim() || newName.trim() === selected
                                }
                                size="sm"
                                variant="flat"
                                onPress={() => void renameInstance()}
                              >
                                {t("重命名")}
                              </Button>
                            </div>
                          </div>

                          <div>
                            <div className="mb-1.5 text-[13px] text-gray-600 dark:text-gray-300">
                              {t(
                                "复制实例（拷贝版本文件夹与启动设置，不复制共享的模组与存档）",
                              )}
                            </div>
                            <div className="flex gap-2">
                              <Input
                                aria-label={t("副本名称")}
                                className="min-w-0 flex-1"
                                placeholder={`${selected}-copy`}
                                size="sm"
                                value={copyName}
                                variant="bordered"
                                onValueChange={setCopyName}
                              />
                              <Button
                                isDisabled={
                                  !copyName.trim() ||
                                  copyName.trim() === selected
                                }
                                isLoading={copying}
                                size="sm"
                                variant="flat"
                                onPress={() => void copyInstance()}
                              >
                                {t("创建副本")}
                              </Button>
                            </div>
                            <div className="mt-1 text-[11px] text-gray-400">
                              {t(
                                "版本隔离实例会连同其模组、配置与存档一起复制；共享目录实例只复制版本文件。",
                              )}
                            </div>
                          </div>

                          <div className="nya-border border-t pt-3">
                            <div className="mb-1 text-[13px] font-semibold text-red-500">
                              {t("危险操作")}
                            </div>
                            <Button
                              color="danger"
                              size="sm"
                              variant={confirmDelete ? "solid" : "flat"}
                              onPress={() => {
                                if (confirmDelete) {
                                  void deleteInstance();
                                } else {
                                  setConfirmDelete(true);
                                }
                              }}
                            >
                              {confirmDelete
                                ? t("确认删除？再次点击执行")
                                : t("删除实例")}
                            </Button>
                            <div className="mt-1 text-[11px] text-gray-400">
                              {t("删除后不可恢复。")}
                            </div>
                          </div>
                        </div>
                      )}
                    </SwitchTransition>
                  </div>
                </div>
              )}
            </SwitchTransition>
          </div>
        ) : (
          <div className="flex min-w-0 flex-1 flex-shrink-0 flex-wrap items-center justify-center gap-3 rounded-large border nya-border nya-panel px-4 py-3 text-xs text-gray-400">
            {selected ? (
              <>
                <span>{t("已收起 {0} 的详情。", { "0": selected })}</span>
                <Button
                  size="sm"
                  startContent={<ChevronDownIcon />}
                  variant="flat"
                  onPress={() => setExpandedInstance(true)}
                >
                  {t("展开详情")}
                </Button>
              </>
            ) : loading ? (
              <span className="flex items-center gap-2">
                <Spinner size="sm" /> {t("正在扫描实例…")}
              </span>
            ) : versions.length === 0 ? (
              <span>
                {t("还没有已安装的实例，")}
                <button
                  className="ml-1 text-blue-500 hover:underline cursor-pointer"
                  onClick={() => navigateToPage("download")}
                >
                  {t("去下载页安装一个")}
                </button>
              </span>
            ) : (
              <span>{t("点击上方列表中的实例即可查看它的详情")}</span>
            )}
          </div>
        )}
      </div>

      {/* 底部状态栏 */}
      {status ? (
        <div className="px-6 pb-3 break-all text-[11px] text-gray-400 flex-shrink-0">
          {status}
        </div>
      ) : null}

      {/* 导入其他启动器 */}
      <Modal
        isOpen={importOpen}
        size="2xl"
        onClose={() => setImportOpen(false)}
        {...modalBehaviorProps}
      >
        <ModalContent>
          <ModalShell
            icon={<ImportIcon />}
            subtitle={t(
              "扫描 Prism / MultiMC / CurseForge / Modrinth / ATLauncher 实例，注册后直接使用（不复制文件）",
            )}
            title={t("导入其他启动器")}
            onClose={() => setImportOpen(false)}
          >
            {importLoading ? (
              <div className="flex items-center justify-center gap-2 py-8 text-sm text-gray-400">
                <Spinner size="sm" /> {t("正在扫描…")}
              </div>
            ) : importList.length === 0 ? (
              <div className="py-8 text-center text-sm text-gray-400">
                {t("未检测到可导入的实例")}
              </div>
            ) : (
              <div className="flex max-h-[420px] flex-col gap-1 overflow-y-auto">
                {importList.map((item) => (
                  <div
                    key={item.Path}
                    className="flex items-center gap-3 rounded-lg px-3 py-2 hover:bg-primary/10"
                  >
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="truncate text-[13px] font-semibold text-gray-800 dark:text-gray-200">
                          {item.Name}
                        </span>
                        <Chip size="sm" variant="flat">
                          {item.Provider}
                        </Chip>
                        {item.GameVersion ? (
                          <span className="text-[11px] text-gray-400">
                            {item.GameVersion}
                          </span>
                        ) : null}
                      </div>
                      <div className="truncate text-[10px] text-gray-400">
                        {item.Path}
                      </div>
                    </div>
                    <Button
                      color={item.Registered ? "success" : "primary"}
                      isDisabled={item.Registered || importBusy === item.Path}
                      isLoading={importBusy === item.Path}
                      size="sm"
                      variant="flat"
                      onPress={() => void registerImport(item)}
                    >
                      {item.Registered ? t("已导入") : t("导入")}
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </ModalShell>
        </ModalContent>
      </Modal>

      {/* Rewind：存档 / 实例快照时间线（实例入口附带存档清单，可直接切换查看各存档） */}
      <RewindDialog
        isOpen={!!rewindEntry}
        kind={rewindEntry?.kind ?? "save"}
        saves={
          rewindEntry?.kind === "instance" ? (details?.Saves ?? []) : undefined
        }
        targetName={rewindEntry?.name ?? ""}
        targetPath={rewindEntry?.path ?? null}
        onClose={() => setRewindEntry(null)}
        onRestored={() => void refreshSelectedDetails()}
      />

      {/* 更新明细：最新版本号 + 可落地动作（打开下载页 / 复制地址 / 另存 / 备份后替换） */}
      <Modal
        isOpen={!!updateDetail}
        size="lg"
        onClose={() => setUpdateDetail(null)}
        {...modalBehaviorProps}
      >
        <ModalContent>
          <ModalShell
            icon={<UpdateIcon />}
            subtitle={updateDetail?.FileName ?? ""}
            title={t("更新详情")}
            onClose={() => setUpdateDetail(null)}
          >
            {updateDetail ? (
              <div className="flex flex-col gap-3 text-[13px]">
                <div className="flex flex-wrap items-center gap-2">
                  <Chip size="sm" variant="flat">
                    {updateDetail.KindLabel || updateDetail.Kind}
                  </Chip>
                  {isUpdatable(updateDetail) ? (
                    <Chip color="warning" size="sm" variant="flat">
                      {t("可更新")}
                    </Chip>
                  ) : badgeToneFor(updateDetail) === "failed" ? (
                    <Chip color="danger" size="sm" variant="flat">
                      {t("检查失败")}
                    </Chip>
                  ) : (
                    <Chip size="sm" variant="flat">
                      {t("未知")}
                    </Chip>
                  )}
                </div>

                <div className="flex flex-col gap-1 text-gray-600 dark:text-gray-300">
                  <div>
                    {t("项目：{0}", {
                      "0": updateDetail.ProjectName || t("未能识别"),
                    })}
                  </div>
                  <div>
                    {t("当前文件：{0}（{1}）", {
                      "0": updateDetail.FileName,
                      "1": updateDetail.SizeText || t("大小未知"),
                    })}
                  </div>
                  {versionTransition(updateDetail) ? (
                    <div>
                      {t("版本：{0}", {
                        "0": versionTransition(updateDetail),
                      })}
                    </div>
                  ) : null}
                  {updateDetail.LatestFileName ? (
                    <div>
                      {t("新文件：{0}（{1}）", {
                        "0": updateDetail.LatestFileName,
                        "1": updateDetail.DownloadSizeText || t("大小未知"),
                      })}
                    </div>
                  ) : null}
                  {updateDetail.ReleaseDate ? (
                    <div>
                      {t("发布日期：{0}", { "0": updateDetail.ReleaseDate })}
                    </div>
                  ) : null}
                  {updateDetail.GameVersionsText ? (
                    <div>
                      {t("支持的 Minecraft 版本：{0}", {
                        "0": updateDetail.GameVersionsText,
                      })}
                    </div>
                  ) : null}
                  {updateDetail.LoadersText ? (
                    <div>
                      {t("支持的加载器：{0}", {
                        "0": updateDetail.LoadersText,
                      })}
                    </div>
                  ) : null}
                  {updateDetail.SHA1 ? (
                    <div className="truncate text-[11px] text-gray-400">
                      {t("SHA-1：{0}", { "0": updateDetail.SHA1 })}
                    </div>
                  ) : null}
                </div>

                {/* 结论说明：未知/失败的原因在这里如实展示，不做美化 */}
                <div className="rounded-lg bg-gray-100 px-3 py-2 text-[12px] text-gray-600 dark:bg-gray-800 dark:text-gray-300">
                  {t(updateDetail.StatusText || "没有更多信息。")}
                </div>

                <div className="flex flex-wrap items-center gap-2">
                  <Button
                    size="sm"
                    startContent={<VersionIcon />}
                    variant="flat"
                    onPress={() => openVersionManager(updateDetail)}
                  >
                    {t("全部版本 / 降级")}
                  </Button>
                  <Button
                    isDisabled={
                      !projectPageURL(updateDetail) &&
                      !downloadLink(updateDetail)
                    }
                    size="sm"
                    variant="flat"
                    onPress={() => void openDownloadPage(updateDetail)}
                  >
                    {t("打开下载页")}
                  </Button>
                  <Button
                    isDisabled={!downloadLink(updateDetail)}
                    size="sm"
                    variant="flat"
                    onPress={() => void copyDownloadURL(updateDetail)}
                  >
                    {t("复制下载地址")}
                  </Button>
                  <Button
                    isDisabled={!downloadLink(updateDetail)}
                    isLoading={updateActionBusy === updateDetail.FilePath}
                    size="sm"
                    variant="flat"
                    onPress={() => void saveUpdateAs(updateDetail)}
                  >
                    {t("另存新版本…")}
                  </Button>
                  <Button
                    color="warning"
                    isDisabled={!isUpdatable(updateDetail)}
                    isLoading={updateActionBusy === updateDetail.FilePath}
                    size="sm"
                    variant="flat"
                    onPress={() => void applyUpdate(updateDetail)}
                  >
                    {t("备份并替换")}
                  </Button>
                </div>
                <div className="text-[11px] text-gray-400">
                  {t(
                    "「备份并替换」会先把原文件改名为 <文件名>.bak-<时间戳>，再写入新版本；失败会自动回滚。",
                  )}
                </div>
              </div>
            ) : null}
          </ModalShell>
        </ModalContent>
      </Modal>

      {/* Mod/资源包/光影包版本管理（升级与降级共用一条备份+替换管线） */}
      <ModVersionDialog
        minecraftDirectory={snap?.MinecraftDirectory ?? ""}
        open={!!versionTarget}
        sourcePath={snap?.SourcePath ?? ""}
        target={versionTarget}
        versionId={selected}
        onClose={() => setVersionTarget(null)}
        onSwitched={(message) => void onVersionSwitched(message)}
      />

      {/* 右键菜单浮层 */}
      <ContextMenuOverlay state={ctxMenu} onClose={() => setCtxMenu(null)} />

      {/* 右键菜单触发的重命名/复制输入弹层（复用设置页的重命名/复制逻辑） */}
      <Modal
        isOpen={menuAction !== null}
        size="sm"
        onClose={() => setMenuAction(null)}
        {...modalBehaviorProps}
      >
        <ModalContent>
          {(onClose) => (
            <ModalShell
              title={menuAction === "rename" ? t("重命名实例") : t("复制实例")}
              onClose={onClose}
            >
              <div className="flex flex-col gap-3">
                <Input
                  /* eslint-disable-next-line jsx-a11y/no-autofocus -- 弹出即输入的场景需要立即聚焦 */
                  autoFocus
                  aria-label={t("实例名称")}
                  placeholder={t("输入新的实例名称")}
                  size="sm"
                  value={menuAction === "rename" ? newName : copyName}
                  variant="bordered"
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      const action = menuAction;

                      onClose();
                      setMenuAction(null);
                      if (action === "rename") void renameInstance();
                      else if (action === "copy") void copyInstance();
                    }
                  }}
                  onValueChange={
                    menuAction === "rename" ? setNewName : setCopyName
                  }
                />
                <div className="flex justify-end gap-2">
                  <Button size="sm" variant="flat" onPress={onClose}>
                    {t("取消")}
                  </Button>
                  <Button
                    color="primary"
                    size="sm"
                    onPress={() => {
                      const action = menuAction;

                      onClose();
                      setMenuAction(null);
                      if (action === "rename") void renameInstance();
                      else if (action === "copy") void copyInstance();
                    }}
                  >
                    {menuAction === "rename" ? t("重命名") : t("复制")}
                  </Button>
                </div>
              </div>
            </ModalShell>
          )}
        </ModalContent>
      </Modal>
    </div>
  );
};

// GameVersionProfile 深拷贝（避免滑块拖动直接写后端对象引用）
function config_profile_clone(
  profile: config.GameVersionProfile,
): config.GameVersionProfile {
  return config.GameVersionProfile.createFrom({
    ...profile,
    AdditionalJvmArguments: [...(profile.AdditionalJvmArguments ?? [])],
    AdditionalGameArguments: [...(profile.AdditionalGameArguments ?? [])],
  });
}

export default InstancesPage;
