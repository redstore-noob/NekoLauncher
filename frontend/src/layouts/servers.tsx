/*
 * 服务器页（开服）：指挥中心单页流 —— 顶部状态横幅（运行数/总数/在线玩家 +
 * 新建/导入/刷新）→ 横贯页顶的服务器列表面板（筛选标签 + 搜索）→ 选中服务器的
 * 详情下挂在同页（控制台/配置/文件/资源导入/内容/玩家/备份/启动参数），不弹窗不跳转。
 *
 * 后端走 ServerHostAPI（internal/mcserver）：进程管理、配置读写、文件与
 * 存档/模组/插件导入均为真实实现；创建时强制 EULA 确认（同意才写 eula=true）。
 */
import type { ReactNode } from "react";
import type { mcserver, models } from "../../wailsjs/go/models";

type ServerFileEntry = mcserver.FileEntry;
type ServerProperty = mcserver.Property;
type ServerSummary = mcserver.ServerInfo;

import React, {
  memo,
  useCallback,
  useEffect,
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
  Progress,
  Select,
  SelectItem,
  Switch,
  Textarea,
} from "@heroui/react";
import {
  Add20Regular,
  ArrowExportLtr20Regular,
  ArrowImport20Regular,
  ArrowLeft20Regular,
  Folder20Regular,
  Server20Regular,
  ArrowDownload20Regular,
  ArrowSync20Regular,
  CheckmarkCircle20Regular,
  Person20Regular,
  Search20Regular,
} from "@fluentui/react-icons";
import { AnimatePresence, motion, type Variants } from "framer-motion";

import { ModalShell, modalBehaviorProps } from "../components/modal-shell";
import CodeFileModal from "../components/CodeFileModal";
import { confirm, notify } from "../components/overlay/dialog";
import SwitchTransition, {
  useSwitchDirection,
} from "../components/screen-transition";
import { selectPopoverProps } from "../lib/motion";
import { TRANSITION_EASINGS } from "../lib/motion";

/* 左列列表项专用进出场：只做透明度，不动 height（与实例页同款） */
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

import { startVisiblePoll } from "../lib/visibility";
import { t } from "../i18n";
import { GetJavaPaths } from "../../wailsjs/go/bindings/ConfigAPI";
import {
  OpenPath,
  SaveFile,
  SelectFile,
} from "../../wailsjs/go/bindings/SystemAPI";
import {
  CreateServer,
  DeleteServer,
  ExportServer,
  ListServerContent,
  SetServerContentEnabled,
  DeleteServerContent,
  SearchServerContent,
  InstallServerContentFromModrinth,
  ServerContentKindForCore,
  GetServerPlayers,
  SaveServerWhitelist,
  SaveServerOps,
  SaveServerBanned,
  SetServerWhitelistEnabled,
  RunServerPlayerCommand,
  GetBackupSettings,
  ListServerBackups,
  CreateServerBackup,
  DeleteServerBackup,
  RestoreServerBackup,
  SaveBackupSettings,
  GetServerProperties,
  ImportServerMod,
  ImportServerNekoser,
  ImportServerPlugin,
  ImportServerWorld,
  ListCoreVersions,
  ListServerFiles,
  ListServerMCVersions,
  ListServers,
  PollServer,
  ReadServerTextFile,
  RequiredJavaMajor,
  SendServerCommand,
  GetServerLaunchOptions,
  SaveServerLaunchOptions,
  SetServerProperties,
  StartServer,
  RestartServer,
  GetServerAutoRestart,
  SetServerAutoRestart,
  StopServer,
  WriteServerTextFile,
} from "../../wailsjs/go/bindings/ServerHostAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";

type TabKey =
  | "console"
  | "config"
  | "files"
  | "import"
  | "content"
  | "players"
  | "backup"
  | "java";

/** 详情页标签顺序（切换方向按它推导） */
const DETAIL_TABS: TabKey[] = [
  "console",
  "config",
  "files",
  "import",
  "content",
  "players",
  "backup",
  "java",
];

/** Aikar 优化参数（服务端社区标准 GC 调优集，适用 <12GB 堆） */
const AIKAR_FLAGS = [
  "-XX:+UseG1GC",
  "-XX:+ParallelRefProcEnabled",
  "-XX:MaxGCPauseMillis=200",
  "-XX:+UnlockExperimentalVMOptions",
  "-XX:+DisableExplicitGC",
  "-XX:+AlwaysPreTouch",
  "-XX:G1NewSizePercent=30",
  "-XX:G1MaxNewSizePercent=40",
  "-XX:G1HeapRegionSize=8M",
  "-XX:G1ReservePercent=20",
  "-XX:G1HeapWastePercent=5",
  "-XX:G1MixedGCCountTarget=4",
  "-XX:InitiatingHeapOccupancyPercent=15",
  "-XX:G1MixedGCLiveThresholdPercent=90",
  "-XX:G1RSetUpdatingPauseTimePercent=5",
  "-XX:SurvivorRatio=32",
  "-XX:+PerfDisableSharedMem",
  "-XX:MaxTenuringThreshold=1",
  "-Dusing.aikars.flags=https://mcflags.emc.gs",
  "-Daikars.new.flags=true",
];

/** 快速更改分组：新手最常动的键，置顶展示；这些键不再出现在下面的分组里 */
const QUICK_PROPERTY_KEYS = [
  "motd",
  "max-players",
  "server-port",
  "online-mode",
  "white-list",
  "gamemode",
  "difficulty",
  "pvp",
];

/** server.properties 键的分组（其余进「其他设置」，默认折叠） */
const PROPERTY_GROUPS: Array<{ id: string; titleKey: string; keys: string[] }> =
  [
    {
      id: "network",
      titleKey: "网络与安全",
      keys: [
        "server-port",
        "server-ip",
        "motd",
        "max-players",
        "online-mode",
        "enforce-secure-profile",
        "prevent-proxy-connections",
        "enable-query",
        "enable-rcon",
        "snooper-enabled",
      ],
    },
    {
      id: "players",
      titleKey: "玩家与权限",
      keys: [
        "white-list",
        "enforce-whitelist",
        "op-permission-level",
        "player-idle-timeout",
        "view-distance",
        "simulation-distance",
        "max-world-size",
        "rate-limit",
      ],
    },
    {
      id: "world",
      titleKey: "世界",
      keys: [
        "level-name",
        "level-seed",
        "level-type",
        "generate-structures",
        "allow-nether",
        "hardcore",
        "spawn-protection",
        "force-gamemode",
        "spawn-monsters",
        "spawn-animals",
        "spawn-npcs",
        "max-build-height",
      ],
    },
    {
      id: "gameplay",
      titleKey: "游戏玩法",
      keys: [
        "gamemode",
        "difficulty",
        "pvp",
        "allow-flight",
        "enable-command-block",
        "enable-status",
        "hide-online-players",
      ],
    },
  ];

/**
 * 常见属性的友好名称与一句话解释；没有收录的键回退显示原始键名。
 * 解释只描述「是什么、改了会怎样」，不重复 vanilla wiki 的全部取值细节。
 */
const PROPERTY_META: Record<string, { label: string; desc?: string }> = {
  // 网络与安全
  "server-port": {
    label: "服务器端口",
    desc: "玩家连接用的 TCP 端口，默认 25565",
  },
  "server-ip": {
    label: "绑定地址",
    desc: "留空监听所有网卡；仅本机测试可填 127.0.0.1",
  },
  motd: {
    label: "服务器简介",
    desc: "服务器列表里显示的一行介绍，支持颜色代码",
  },
  "max-players": { label: "最大在线人数" },
  "online-mode": {
    label: "正版账号验证",
    desc: "开启后仅正版（微软）账号可加入；离线模式或外置登录需关闭",
  },
  "enforce-secure-profile": {
    label: "强制安全档案",
    desc: "要求玩家使用微软签名的聊天档案，关闭正版验证时须一并关闭",
  },
  "prevent-proxy-connections": {
    label: "拦截代理连接",
    desc: "尝试识别并拒绝经代理/VPN 加入的玩家",
  },
  "enable-query": {
    label: "开启 Query 协议",
    desc: "向外部查询工具暴露服务器状态信息",
  },
  "enable-rcon": {
    label: "开启远程控制",
    desc: "允许通过 RCON 密码远程执行指令",
  },
  "snooper-enabled": {
    label: "匿名数据上报",
    desc: "向 Mojang 发送匿名服务器数据",
  },
  // 玩家与权限
  "white-list": { label: "白名单" },
  "enforce-whitelist": {
    label: "强制白名单",
    desc: "运行期间也踢出不在白名单里的玩家",
  },
  "op-permission-level": {
    label: "管理员权限等级",
    desc: "OP 玩家的指令权限等级（1-4）",
  },
  "player-idle-timeout": {
    label: "挂机踢出时间",
    desc: "玩家无操作多少分钟后被踢（0 为不踢）",
  },
  "view-distance": {
    label: "视野距离",
    desc: "发送给客户端区块的半径（格），越大越吃带宽与性能",
  },
  "simulation-distance": {
    label: "模拟距离",
    desc: "实体与方块更新的半径（格），影响刷怪与农场",
  },
  "max-world-size": {
    label: "世界边界半径",
    desc: "世界可生成的最大范围（格）",
  },
  "rate-limit": {
    label: "数据包频率限制",
    desc: "每秒允许玩家发送的数据包数，超出断开",
  },
  // 世界
  "level-name": {
    label: "世界名称",
    desc: "存档目录名，改名等于切换到另一个世界",
  },
  "level-seed": {
    label: "世界种子",
    desc: "决定地形生成的种子，留空随机；只对新生成的世界生效",
  },
  "level-type": {
    label: "世界类型",
    desc: "地形生成器类型，如 default / flat / large_biomes",
  },
  "generate-structures": {
    label: "生成结构",
    desc: "是否生成村庄、要塞等结构（仅新世界）",
  },
  "allow-nether": { label: "允许下界" },
  hardcore: {
    label: "极限模式",
    desc: "玩家死亡后永久旁观，无法复活（仅新世界）",
  },
  "spawn-protection": {
    label: "出生点保护范围",
    desc: "出生点周围非 OP 不可破坏的半径（0 为关闭）",
  },
  "force-gamemode": { label: "强制游戏模式" },
  "spawn-monsters": { label: "生成敌对生物" },
  "spawn-animals": { label: "生成动物" },
  "spawn-npcs": { label: "生成村民" },
  "max-build-height": {
    label: "最大建筑高度",
    desc: "玩家可放置/破坏方块的最高高度",
  },
  // 游戏玩法
  gamemode: {
    label: "默认游戏模式",
    desc: "新玩家加入时的模式：0 生存 1 创造 2 冒险 3 旁观",
  },
  difficulty: {
    label: "难度",
    desc: "peaceful/easy/normal/hard，影响怪物伤害与饥饿",
  },
  pvp: { label: "玩家对战", desc: "是否允许玩家互相伤害" },
  "allow-flight": {
    label: "允许飞行",
    desc: "生存模式下是否允许飞行（安装飞行 mod 时需开启）",
  },
  "enable-command-block": {
    label: "启用命令方块",
  },
  "enable-status": {
    label: "对外显示状态",
    desc: "是否响应服务器列表的状态请求",
  },
  "hide-online-players": {
    label: "隐藏在线列表",
  },
};

const CORE_LABELS: Record<string, string> = {
  vanilla: "Vanilla",
  paper: "Paper",
  neoforge: "NeoForge",
  fabric: "Fabric",
};

const CORE_OPTIONS = Object.keys(CORE_LABELS);

const EMPTY_FORM = {
  name: "",
  core: "paper",
  mcVersion: "",
  coreVersion: "",
  port: "25565",
  maxPlayers: "20",
  javaPath: "",
};

function isBooleanValue(value: string): boolean {
  return value === "true" || value === "false";
}

function formatSize(bytes: number): string {
  if (bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let index = 0;

  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }

  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

// shortJavaPath 收缩 Java 绝对路径，只保留末三段（…\jdk-21\bin\java.exe），
// 供下拉项在弹窗宽度内完整展示版本与位置。
function shortJavaPath(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);

  return parts.length > 3 ? "…\\" + parts.slice(-3).join("\\") : path;
}

/**
 * 控制台日志高亮：按服务器真实输出形态分派解析（正则预编译，行渲染零分配）：
 * - Paper/原版控制台：`[22:45:33 INFO]: 正文`（时间与级别同括号）；
 * - Forge/Fabric 经典：`[12:34:56] [线程/级别]: 正文`；
 * - 现代引导日志：`2026-09-20T14:45:33.188Z ServerMain WARN 正文`
 *   （ISO 时间 线程 级别，Paper 启动早期与 log4j 默认格式）；
 * - JVM 裸前缀：`WARNING: …` / `ERROR: …`；
 * - 异常堆栈：`java.xxx.Exception: …` / `Caused by …` 红，`at …` 帧暗灰。
 * 时间戳统一淡灰；级别段按级别上色（INFO 蓝 / WARN 琥珀 / ERROR 红）；
 * 正文 WARN/ERROR 整行染色；聊天行（`<玩家>` 开头）绿色。
 */
const LEVEL_WORD = "INFO|WARN|WARNING|ERROR|FATAL|SEVERE|DEBUG|TRACE";

const RE_PAPER_CONSOLE = new RegExp(
  `^\\[(\\d{2}:\\d{2}:\\d{2})\\s+(${LEVEL_WORD})\\]\\s*:?\\s*`,
  "i",
);
const RE_CLASSIC = new RegExp(
  `^\\[(\\d{2}:\\d{2}:\\d{2})\\]\\s*\\[([^\\]/]+)\\/(${LEVEL_WORD})\\]\\s*:?\\s*`,
  "i",
);
const RE_BOOTSTRAP = new RegExp(
  `^(\\d{4}-\\d{2}-\\d{2}T[\\d:.]+Z?)\\s+(\\S+)\\s+(${LEVEL_WORD})\\s+`,
  "i",
);
const RE_BARE_LEVEL = /^(WARNING|ERROR|SEVERE|FATAL)\b[:：]?\s*/i;
const RE_STACK_FRAME = /^\s*(?:at\s|\.\.\.\s*\d+\s*more)/;
const RE_EXCEPTION = /^(?:[\w.$]*(?:Exception|Error|Throwable)\b|Caused by\b)/;
const RE_CHAT_LINE = /^<[^>]+>/;

/** 级别词 → 归一桶（FATAL/SEVERE 归 ERROR，TRACE 归 DEBUG） */
function levelBucket(raw: string): string {
  const v = raw.trim().toUpperCase();

  if (v === "INFO") return "INFO";
  if (v === "WARN" || v === "WARNING") return "WARNING";
  if (v === "ERROR" || v === "FATAL" || v === "SEVERE") return "ERROR";
  if (v === "DEBUG" || v === "TRACE") return "DEBUG";

  return "OTHER";
}

interface ParsedLogLine {
  time: string;
  tag: string;
  level: string;
  body: string;
}

function parseConsoleLine(line: string): ParsedLogLine {
  let m: RegExpExecArray | null;

  if ((m = RE_PAPER_CONSOLE.exec(line))) {
    return {
      time: `[${m[1]}] `,
      tag: m[2],
      level: levelBucket(m[2]),
      body: line.slice(m[0].length),
    };
  }
  if ((m = RE_CLASSIC.exec(line))) {
    return {
      time: `[${m[1]}] `,
      tag: `${m[2]}/${m[3]}`,
      level: levelBucket(m[3]),
      body: line.slice(m[0].length),
    };
  }
  if ((m = RE_BOOTSTRAP.exec(line))) {
    return {
      time: `${m[1]} `,
      tag: `${m[2]} ${m[3]}`,
      level: levelBucket(m[3]),
      body: line.slice(m[0].length),
    };
  }
  if ((m = RE_BARE_LEVEL.exec(line))) {
    return {
      time: "",
      tag: m[1],
      level: levelBucket(m[1]),
      body: line.slice(m[0].length),
    };
  }
  if (RE_STACK_FRAME.test(line)) {
    return { time: "", tag: "", level: "STACK", body: line };
  }
  if (RE_EXCEPTION.test(line)) {
    return { time: "", tag: "", level: "ERROR", body: line };
  }

  return { time: "", tag: "", level: "OTHER", body: line };
}

// 控制台配色与主题相反：浅色主题下是灰色毛玻璃（深色文字），
// 深色主题下是白色毛玻璃（同为深色文字），因此两个分支都用深色调。
const LEVEL_BODY_CLASS: Record<string, string> = {
  INFO: "text-gray-700 dark:text-gray-800",
  WARNING: "text-amber-700 dark:text-amber-600",
  ERROR: "text-red-700 dark:text-red-600",
  DEBUG: "text-gray-500 dark:text-gray-500",
  STACK: "text-gray-400 dark:text-gray-400",
  OTHER: "text-gray-700 dark:text-gray-800",
};

const LEVEL_TAG_CLASS: Record<string, string> = {
  INFO: "text-blue-700 dark:text-blue-600",
  WARNING: "text-amber-800 dark:text-amber-600 font-semibold",
  ERROR: "text-red-700 dark:text-red-600 font-semibold",
  DEBUG: "text-gray-500 dark:text-gray-500",
  STACK: "text-gray-400 dark:text-gray-400",
  OTHER: "text-gray-500 dark:text-gray-500",
};

/**
 * 控制台单行日志（memo）。
 * 每秒 PollServer 都会刷新控制台，500 行时逐行重新解析+重渲染是掉帧大头；
 * 行文本没变就跳过渲染（日志只追加，历史行内容稳定）。
 */
const ConsoleLine = memo(function ConsoleLine({ line }: { line: string }) {
  const { time, tag, level, body } = parseConsoleLine(line);
  const bodyClass = RE_CHAT_LINE.test(body.trimStart())
    ? "text-emerald-700 dark:text-emerald-700"
    : (LEVEL_BODY_CLASS[level] ?? LEVEL_BODY_CLASS.OTHER);
  const tagClass = LEVEL_TAG_CLASS[level] ?? LEVEL_TAG_CLASS.OTHER;

  return (
    <div className="whitespace-pre-wrap break-all">
      {time ? (
        <span className="text-gray-500 dark:text-gray-400">{time}</span>
      ) : null}
      {tag ? <span className={tagClass}>{tag} </span> : null}
      <span className={bodyClass}>{body}</span>
    </div>
  );
});

function statusChip(status: string): ReactNode {
  if (status === "running") {
    return (
      <Chip color="success" size="sm" variant="flat">
        ● {t("运行中")}
      </Chip>
    );
  }
  if (status === "starting") {
    return (
      <Chip color="warning" size="sm" variant="flat">
        {t("启动中")}
      </Chip>
    );
  }
  if (status === "stopping") {
    return (
      <Chip color="warning" size="sm" variant="flat">
        {t("停止中")}
      </Chip>
    );
  }

  return (
    <Chip size="sm" variant="flat">
      {t("已停止")}
    </Chip>
  );
}

const ServersPage: React.FC = () => {
  const [servers, setServers] = useState<ServerSummary[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [tab, setTab] = useState<TabKey>("console");
  // 标签序号 → 切换方向（实例页同款滑动切换）
  const tabDirection = useSwitchDirection(DETAIL_TABS.indexOf(tab));
  const [busy, setBusy] = useState(false);
  const [runtime, setRuntime] = useState<{
    Status: string;
    Players: number;
    CPUPercent: number;
    MemoryMB: number;
    LogLines: string[];
  } | null>(null);

  // 控制台
  const [command, setCommand] = useState("");
  const consoleRef = useRef<HTMLDivElement | null>(null);

  // 配置（server.properties）
  const [properties, setProperties] = useState<ServerProperty[]>([]);
  const [propsDirty, setPropsDirty] = useState<Record<string, string>>({});
  const [collapsedGroups, setCollapsedGroups] = useState<
    Record<string, boolean>
  >({
    other: true,
  });

  // JVM 启动参数（图形化编辑）
  const [launchMemoryMax, setLaunchMemoryMax] = useState("");
  const [launchMemoryMin, setLaunchMemoryMin] = useState("0");
  const [launchExtraArgs, setLaunchExtraArgs] = useState("");

  // 文件
  const [filesPath, setFilesPath] = useState("");
  const [files, setFiles] = useState<ServerFileEntry[]>([]);
  const [editorFile, setEditorFile] = useState<string | null>(null);
  const [editorContent, setEditorContent] = useState("");

  // 服务端内容（mods / plugins）
  const [contentKind, setContentKind] = useState("mods");
  const [contentEntries, setContentEntries] = useState<
    mcserver.ServerContentEntry[]
  >([]);
  const [contentLoading, setContentLoading] = useState(false);
  const [contentBusy, setContentBusy] = useState(false);
  const [contentQuery, setContentQuery] = useState("");
  const [contentResults, setContentResults] = useState<
    models.ModrinthProject[]
  >([]);
  const [contentSearching, setContentSearching] = useState(false);
  const [contentInstalling, setContentInstalling] = useState("");
  const [contentError, setContentError] = useState("");

  // 玩家与权限
  const [players, setPlayers] = useState<mcserver.ServerPlayers | null>(null);
  const [playerLoading, setPlayerLoading] = useState(false);
  const [playerBusy, setPlayerBusy] = useState(false);
  const [playerName, setPlayerName] = useState("");
  const [broadcast, setBroadcast] = useState("");

  // 崩溃自动重启
  const [autoRestart, setAutoRestart] = useState(false);

  // 备份
  const [backups, setBackups] = useState<mcserver.BackupInfo[]>([]);
  const [backupLoading, setBackupLoading] = useState(false);
  const [backupBusy, setBackupBusy] = useState(false);
  const [backupSettings, setBackupSettings] = useState<mcserver.BackupSettings>(
    {
      Enabled: false,
      IntervalHours: 6,
      KeepCount: 5,
      KeepDays: 0,
    },
  );

  // 创建流程（表单 → EULA 确认）
  const [createOpen, setCreateOpen] = useState(false);
  const [eulaStep, setEulaStep] = useState(false);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState(EMPTY_FORM);
  // 创建表单的高级项（端口/玩家数/Java）默认收起，有合理默认值新手不必碰
  const [createAdvancedOpen, setCreateAdvancedOpen] = useState(false);
  // 启动参数页的高级项（额外 JVM 参数/命令行预览）默认收起
  const [jvmAdvancedOpen, setJvmAdvancedOpen] = useState(false);
  // NekoSer 整包导出/导入进度（后端 mcserver:transfer-progress 事件驱动）
  const [transfer, setTransfer] = useState<{
    phase: string;
    percent: number;
  } | null>(null);

  useEffect(() => {
    // EventsOn 的返回值在部分环境（纯浏览器 mock）不是函数，包一层保证 cleanup 可调用
    const off = EventsOn(
      "mcserver:transfer-progress",
      (payload: { Phase: string; Done: number; Total: number }) => {
        const percent =
          payload.Total > 0
            ? Math.min(100, Math.round((payload.Done / payload.Total) * 100))
            : 0;

        setTransfer(percent >= 100 ? null : { phase: payload.Phase, percent });
      },
    );

    return () => {
      if (typeof off === "function") off();
    };
  }, []);
  const [javaOptions, setJavaOptions] = useState<
    { path: string; version: string }[]
  >([]);
  // 创建中 MC 版本所需的最低 Java 主版本（0 = 未知，不展示提示）
  const [requiredMajor, setRequiredMajor] = useState(0);
  // 版本数据：核心支持的 MC 版本（ComboBox）与该 MC 版本下的服务端版本（Listbox）
  const [mcVersions, setMcVersions] = useState<string[]>([]);
  const [mcLoading, setMcLoading] = useState(false);
  const [coreVersions, setCoreVersions] = useState<string[]>([]);
  const [coreVersionLoading, setCoreVersionLoading] = useState(false);

  const selected = servers.find((server) => server.ID === selectedId) ?? null;

  // 指挥中心单页流：列表侧的筛选标签与搜索（只影响展示，不动轮询与选中逻辑）
  const [serverFilter, setServerFilter] = useState<
    "all" | "running" | "stopped"
  >("all");
  const [serverSearch, setServerSearch] = useState("");
  const filteredServers = useMemo(() => {
    const query = serverSearch.trim().toLowerCase();

    return servers.filter((server) => {
      if (serverFilter === "running" && server.Status === "stopped")
        return false;
      if (serverFilter === "stopped" && server.Status !== "stopped")
        return false;
      if (
        query &&
        !`${server.Name} ${server.MCVersion} ${server.Core}`
          .toLowerCase()
          .includes(query)
      )
        return false;

      return true;
    });
  }, [servers, serverFilter, serverSearch]);
  const runningCount = useMemo(
    () => servers.filter((server) => server.Status !== "stopped").length,
    [servers],
  );
  const playersOnline = useMemo(
    () =>
      servers.reduce(
        (sum, server) =>
          server.Status === "running" ? sum + server.Players : sum,
        0,
      ),
    [servers],
  );

  // 列表刷新：并发去重 + 失败可见 + 指数退避。
  // 之前失败被 catch 吞掉，后端持续挂掉时用户永远看着旧列表且零提示。
  const listInFlightRef = useRef(false);
  const listFailStreakRef = useRef(0);
  const listRetryAtRef = useRef(0);
  const listErrorShownRef = useRef(false);

  const refreshList = useCallback(async () => {
    if (listInFlightRef.current) return; // 初始加载/3 秒轮询/操作后刷新可能撞车
    listInFlightRef.current = true;
    try {
      const list = await ListServers();

      // 防御非数组返回（纯浏览器 mock / 后端异常），避免下方 servers.find 崩溃
      setServers(Array.isArray(list) ? list : []);
      if (listErrorShownRef.current) {
        listErrorShownRef.current = false;
        notify.success(t("服务器列表已恢复"));
      }
      listFailStreakRef.current = 0;
      listRetryAtRef.current = 0;
    } catch (ex) {
      listFailStreakRef.current += 1;
      const backoff = Math.min(
        3000 * 2 ** (listFailStreakRef.current - 1),
        30000,
      );

      listRetryAtRef.current = Date.now() + backoff;
      // 只提示一次，之后安静退避（3s→6s→12s→…封顶 30s），避免刷屏
      if (!listErrorShownRef.current) {
        listErrorShownRef.current = true;
        notify.error(
          t("服务器列表刷新失败：{0}（将降低重试频率，恢复后自动继续）", {
            "0": (ex as Error)?.message ?? ex,
          }),
        );
      }
    } finally {
      listInFlightRef.current = false;
    }
  }, []);

  useEffect(() => {
    void refreshList();
    void GetJavaPaths()
      .then((items) =>
        setJavaOptions(
          items
            .filter((item) => item.JavaPath)
            .map((item) => ({
              path: item.JavaPath,
              version: item.JavaVersion,
            })),
        ),
      )
      .catch(() => setJavaOptions([]));
  }, [refreshList]);

  // 创建弹窗里选择的 MC 版本变化时，查询其 Java 版本要求用于提示
  useEffect(() => {
    if (!createOpen || !form.mcVersion) {
      setRequiredMajor(0);

      return;
    }
    let cancelled = false;

    RequiredJavaMajor(form.mcVersion)
      .then((major) => {
        if (!cancelled) setRequiredMajor(major);
      })
      .catch(() => {
        if (!cancelled) setRequiredMajor(0);
      });

    return () => {
      cancelled = true;
    };
  }, [createOpen, form.mcVersion]);

  // 核心支持的 MC 版本（打开创建弹窗或切换核心时拉取）
  useEffect(() => {
    if (!createOpen || eulaStep) {
      return;
    }
    let cancelled = false;

    setMcLoading(true);
    setMcVersions([]);
    ListServerMCVersions(form.core)
      .then((list) => {
        if (cancelled) return;
        setMcVersions(list);
        setForm((prev) => ({ ...prev, mcVersion: list[0] ?? "" }));
      })
      .catch((ex) => {
        if (!cancelled) {
          notify.error(
            t("版本列表获取失败：{0}", { "0": (ex as Error)?.message ?? ex }),
          );
        }
      })
      .finally(() => {
        if (!cancelled) setMcLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [createOpen, eulaStep, form.core]);

  // 该 MC 版本下的服务端版本列表（Vanilla 无此概念，自动选中最新）
  useEffect(() => {
    if (!createOpen || eulaStep || form.core === "vanilla" || !form.mcVersion) {
      setCoreVersions([]);

      return;
    }
    let cancelled = false;

    setCoreVersionLoading(true);
    setCoreVersions([]);
    ListCoreVersions(form.core, form.mcVersion)
      .then((list) => {
        if (cancelled) return;
        setCoreVersions(list);
        setForm((prev) => ({ ...prev, coreVersion: list[0] ?? "" }));
      })
      .catch(() => {
        if (!cancelled) setCoreVersions([]);
      })
      .finally(() => {
        if (!cancelled) setCoreVersionLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [createOpen, eulaStep, form.core, form.mcVersion]);

  // 列表状态轮询（刷新失败时按 listRetryAtRef 退避，手动操作不受影响；
  // 窗口隐藏时暂停，回前台立即补一拍）
  useEffect(
    () =>
      startVisiblePoll(() => {
        if (Date.now() < listRetryAtRef.current) return;
        void refreshList();
      }, 3000),
    [refreshList],
  );

  // 选中服务器的日志/占用轮询。
  // 游标与在途标记都是 effect 局部：1 秒节拍遇慢响应时，两次 PollServer 会带
  // 同一游标并发（日志重复/乱序），且旧服务器的游标会污染切换后的新服务器。
  useEffect(() => {
    if (!selectedId) {
      setRuntime(null);

      return;
    }
    let cursor = 0;
    let inFlight = false;

    setRuntime(null);
    const tick = async () => {
      if (inFlight) return; // 上一拍还没回来：跳过本拍而不是并发补发
      inFlight = true;
      try {
        const snapshot = await PollServer(selectedId, cursor);

        cursor = snapshot.NextCursor;
        if (
          snapshot.Status === "stopped" &&
          !snapshot.LogLines.length &&
          cursor === 0
        ) {
          setRuntime(null);

          return;
        }
        // 后端只回传游标之后的新行：前端负责累积（上限 500 行）
        setRuntime((prev) => {
          const merged = [...(prev?.LogLines ?? []), ...snapshot.LogLines];

          return {
            Status: snapshot.Status,
            Players: snapshot.Players,
            CPUPercent: snapshot.CPUPercent,
            MemoryMB: snapshot.MemoryMB,
            LogLines:
              merged.length > 500 ? merged.slice(merged.length - 500) : merged,
          };
        });
        // 状态/人数没变就保持数组引用不变：每秒都换新数组会让列表页白白重渲染
        setServers((prev) => {
          const idx = prev.findIndex((server) => server.ID === selectedId);

          if (idx < 0) return prev;
          const target = prev[idx];

          if (
            target.Status === snapshot.Status &&
            target.Players === snapshot.Players
          )
            return prev;
          const next = [...prev];

          next[idx] = {
            ...target,
            Status: snapshot.Status,
            Players: snapshot.Players,
          };

          return next;
        });
      } catch {
        /* 服务器被删等场景：忽略 */
      } finally {
        inFlight = false;
      }
    };

    return startVisiblePoll(tick, 1000);
  }, [selectedId]);

  // 控制台自动滚动
  const logLength = runtime?.LogLines.length ?? 0;

  useEffect(() => {
    const box = consoleRef.current;

    if (!box) return;
    // 只在自己贴着底部时跟随：无条件滚动会让用户没法往上翻看启动报错
    const atBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 24;

    if (atBottom) box.scrollTop = box.scrollHeight;
  }, [logLength, tab]);

  // ---- 动作 ----
  const doStart = async (server: ServerSummary) => {
    setBusy(true);
    try {
      await StartServer(server.ID);
      await refreshList();
    } catch (ex) {
      notify.error(t("启动失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setBusy(false);
    }
  };

  const doStop = async (server: ServerSummary, force = false) => {
    if (force) {
      const ok = await confirm(
        t("强制停止"),
        t(
          "将立即结束服务器进程，不给保存世界的机会，可能丢失未落盘的进度。仅在服务器卡死、软停止无效时使用。",
        ),
        { confirmLabel: t("强制停止"), severity: "danger" },
      );

      if (!ok) return;
    }
    setBusy(true);
    try {
      await StopServer(server.ID, force);
      await refreshList();
    } catch (ex) {
      notify.error(t("停止失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setBusy(false);
    }
  };

  const toggleAutoRestart = async (enabled: boolean) => {
    if (!selectedId) return;
    try {
      await SetServerAutoRestart(selectedId, enabled);
      setAutoRestart(enabled);
      notify.success(
        enabled ? t("已开启崩溃自动重启") : t("已关闭崩溃自动重启"),
      );
    } catch (ex) {
      notify.error(t("操作失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  const doRestart = async (server: ServerSummary) => {
    setBusy(true);
    try {
      await RestartServer(server.ID);
      await refreshList();
    } catch (ex) {
      notify.error(t("重启失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setBusy(false);
    }
  };

  const doDelete = async (server: ServerSummary) => {
    const ok = await confirm(
      t("删除服务器"),
      t("将删除「{0}」的整个服务器目录，包括存档与配置，无法撤销。", {
        "0": server.Name,
      }),
      { confirmLabel: t("删除"), severity: "warning" },
    );

    if (!ok) return;
    try {
      await DeleteServer(server.ID);
      if (selectedId === server.ID) setSelectedId(null);
      await refreshList();
      notify.success(t("已删除「{0}」", { "0": server.Name }));
    } catch (ex) {
      notify.error(t("删除失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  const sendCommand = async (raw?: string) => {
    const text = (raw ?? command).trim();

    if (!text || !selectedId) return;
    try {
      await SendServerCommand(selectedId, text);
      setCommand("");
    } catch (ex) {
      notify.error(t("发送失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  // ---- 配置（server.properties） ----
  const loadProperties = useCallback(async (id: string) => {
    try {
      setProperties(await GetServerProperties(id));
      setPropsDirty({});
    } catch (ex) {
      notify.error(
        t("读取配置失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
  }, []);

  useEffect(() => {
    if (selectedId && tab === "config") void loadProperties(selectedId);
  }, [selectedId, tab, loadProperties]);

  // ---- JVM 启动参数 ----
  const loadLaunchOptions = useCallback(async (id: string) => {
    try {
      const options = await GetServerLaunchOptions(id);

      setLaunchMemoryMax(String(options.MemoryMB || ""));
      setLaunchMemoryMin(String(options.MemoryMinMB || 0));
      setLaunchExtraArgs((options.ExtraJavaArgs ?? []).join("\n"));
    } catch (ex) {
      notify.error(
        t("读取启动参数失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
  }, []);

  useEffect(() => {
    if (selectedId && tab === "java") void loadLaunchOptions(selectedId);
  }, [selectedId, tab, loadLaunchOptions]);

  const saveLaunchOptions = async () => {
    if (!selectedId) return;
    const extras = launchExtraArgs
      .split("\n")
      .flatMap((line) => line.trim().split(/\s+/))
      .filter(Boolean);

    try {
      await SaveServerLaunchOptions(selectedId, {
        MemoryMB: Number(launchMemoryMax) || 0,
        MemoryMinMB: Number(launchMemoryMin) || 0,
        ExtraJavaArgs: extras,
      });
      await loadLaunchOptions(selectedId);
      notify.success(t("启动参数已保存"));
    } catch (ex) {
      notify.error(t("保存失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  /** 命令行预览（与后端 StartServer 拼接顺序一致） */
  const launchPreview = useMemo(() => {
    const parts: string[] = ["java"];
    const max = Number(launchMemoryMax) || 0;
    const min = Number(launchMemoryMin) || 0;

    if (max > 0) parts.push(`-Xmx${max}M`);
    if (min > 0) parts.push(`-Xms${min}M`);
    parts.push(
      ...launchExtraArgs
        .split("\n")
        .flatMap((line) => line.trim().split(/\s+/))
        .filter(Boolean),
    );
    parts.push(
      "-jar",
      selected?.Core === "vanilla" ? "server.jar" : "...",
      "nogui",
    );

    return parts.join(" ");
  }, [launchMemoryMax, launchMemoryMin, launchExtraArgs, selected?.Core]);

  const dirtyCount = Object.keys(propsDirty).length;

  const saveProperties = async () => {
    if (!selectedId || dirtyCount === 0) return;
    try {
      await SetServerProperties(
        selectedId,
        properties.map((property) => ({
          Key: property.Key,
          Value: propsDirty[property.Key] ?? property.Value,
        })),
      );
      await loadProperties(selectedId);
      notify.success(t("配置已保存"));
    } catch (ex) {
      notify.error(t("保存失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  const groupedProperties = useMemo(() => {
    // 快速更改组置顶；其键从常规分组剔除，避免同一配置出现两份
    const groups: Array<{
      id: string;
      title: string;
      items: ServerProperty[];
    }> = [
      {
        id: "quick",
        title: t("快速更改"),
        items: QUICK_PROPERTY_KEYS.flatMap((key) =>
          properties.filter((property) => property.Key === key),
        ),
      },
      ...PROPERTY_GROUPS.map((group) => ({
        id: group.id,
        title: t(group.titleKey),
        items: properties.filter(
          (property) =>
            group.keys.includes(property.Key) &&
            !QUICK_PROPERTY_KEYS.includes(property.Key),
        ),
      })),
    ];
    const known = new Set([
      ...PROPERTY_GROUPS.flatMap((group) => group.keys),
      ...QUICK_PROPERTY_KEYS,
    ]);

    groups.push({
      id: "other",
      title: t("其他设置"),
      items: properties.filter((property) => !known.has(property.Key)),
    });

    return groups;
  }, [properties]);

  // ---- 文件 ----
  const loadFiles = useCallback(async (id: string, path: string) => {
    try {
      setFiles(await ListServerFiles(id, path));
    } catch (ex) {
      notify.error(
        t("读取目录失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
  }, []);

  useEffect(() => {
    if (selectedId && tab === "files") {
      setEditorFile(null);
      setFilesPath("");
      void loadFiles(selectedId, "");
    }
  }, [selectedId, tab, loadFiles]);

  const openFile = async (name: string) => {
    if (!selectedId) return;
    const relative = filesPath ? `${filesPath}/${name}` : name;

    try {
      setEditorContent(await ReadServerTextFile(selectedId, relative));
      setEditorFile(relative);
    } catch (ex) {
      notify.error(
        t("读取文件失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
  };

  const saveFile = async () => {
    if (!selectedId || !editorFile) return;
    try {
      await WriteServerTextFile(selectedId, editorFile, editorContent);
      notify.success(t("已保存"));
    } catch (ex) {
      notify.error(t("保存失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  // ---- 资源导入 ----
  const importVia = async (kind: "world" | "mod" | "plugin") => {
    if (!selectedId) return;
    const titles = {
      world: t("选择存档压缩包"),
      mod: t("选择模组文件"),
      plugin: t("选择插件文件"),
    };
    let archive = "";

    try {
      archive = await SelectFile(
        titles[kind],
        kind === "world" ? t("存档压缩包") : t("模组/插件文件"),
        kind === "world" ? "*.zip" : "*.jar",
      );
    } catch {
      return; // 用户取消
    }
    if (!archive) return;

    try {
      if (kind === "world") await ImportServerWorld(selectedId, archive);
      else if (kind === "mod") await ImportServerMod(selectedId, archive);
      else await ImportServerPlugin(selectedId, archive);
      notify.success(t("导入成功，重启服务器后生效"));
    } catch (ex) {
      notify.error(t("导入失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  // ---- NekoSer 整包（.nekoser = zip 容器） ----
  const exportNekoser = async () => {
    if (!selected || transfer) return;
    let dest = "";

    try {
      dest = await SaveFile(
        t("导出服务器"),
        `${selected.Name}.nekoser`,
        t("NekoSer 服务器包"),
        "*.nekoser",
      );
    } catch {
      return; // 用户取消
    }
    if (!dest) return;

    setTransfer({ phase: "export", percent: 0 });
    try {
      await ExportServer(selected.ID, dest);
      notify.success(t("已导出到 {0}", { "0": dest }));
    } catch (ex) {
      notify.error(t("导出失败：{0}", { "0": (ex as Error)?.message ?? ex }));
      setTransfer(null);
    }
  };

  const importNekoser = async () => {
    if (transfer) return;
    let archive = "";

    try {
      archive = await SelectFile(
        t("导入服务器"),
        t("NekoSer 服务器包"),
        "*.nekoser",
      );
    } catch {
      return; // 用户取消
    }
    if (!archive) return;

    setTransfer({ phase: "import", percent: 0 });
    try {
      const id = await ImportServerNekoser(archive);

      await refreshList();
      setSelectedId(id);
      notify.success(t("导入成功"));
    } catch (ex) {
      notify.error(t("导入失败：{0}", { "0": (ex as Error)?.message ?? ex }));
      setTransfer(null);
    }
  };

  // ---- 创建（表单 → EULA → 后端） ----
  const createConfirmed = async () => {
    setCreating(true);
    try {
      const id = await CreateServer({
        Name: form.name.trim(),
        Core: form.core,
        MCVersion: form.mcVersion.trim(),
        CoreVersion: form.coreVersion,
        Port: Number(form.port) || 25565,
        MaxPlayers: Number(form.maxPlayers) || 20,
        JavaPath: form.javaPath,
        AcceptEULA: true,
      });

      setCreateOpen(false);
      setEulaStep(false);
      setCreateAdvancedOpen(false);
      setForm(EMPTY_FORM);
      await refreshList();
      setSelectedId(id);
      notify.success(t("已创建「{0}」", { "0": form.name.trim() }));
    } catch (ex) {
      notify.error(t("创建失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setCreating(false);
    }
  };

  // ---- 服务端内容 ----
  const refreshContent = useCallback(async () => {
    if (!selectedId) return;
    setContentLoading(true);
    try {
      const list = await ListServerContent(selectedId, contentKind);

      setContentEntries(Array.isArray(list) ? list : []);
    } catch {
      setContentEntries([]);
    } finally {
      setContentLoading(false);
    }
  }, [selectedId, contentKind]);

  useEffect(() => {
    if (!selectedId || tab !== "content") return;
    void refreshContent();
    // 目录类型由服务器核心决定（Paper → plugins，Fabric/NeoForge → mods）
    void ServerContentKindForCore(selected?.Core ?? "")
      .then((kind) => {
        if (kind) setContentKind(kind);
      })
      .catch(() => undefined);
  }, [selectedId, tab, refreshContent, selected?.Core]);

  const searchContent = async () => {
    if (!selectedId || !selected) return;
    setContentSearching(true);
    setContentError("");
    try {
      const list = await SearchServerContent(
        contentQuery.trim(),
        selected.MCVersion,
        selected.Core,
        20,
      );

      setContentResults(Array.isArray(list) ? list : []);
      if (!list || list.length === 0) setContentError(t("没有找到匹配的项目"));
    } catch (ex) {
      setContentResults([]);
      setContentError(
        t("搜索失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    } finally {
      setContentSearching(false);
    }
  };

  const installContent = async (project: models.ModrinthProject) => {
    if (!selectedId || !selected) return;
    setContentInstalling(project.project_id);
    try {
      const fileName = await InstallServerContentFromModrinth(
        selectedId,
        contentKind,
        project.project_id,
        selected.MCVersion,
        selected.Core,
      );

      notify.success(
        t("已安装 {0}（重启服务器后生效）", { "0": fileName ?? "" }),
      );
      await refreshContent();
    } catch (ex) {
      notify.error(t("安装失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setContentInstalling("");
    }
  };

  const toggleContent = async (
    entry: mcserver.ServerContentEntry,
    enabled: boolean,
  ) => {
    if (!selectedId) return;
    setContentBusy(true);
    try {
      await SetServerContentEnabled(
        selectedId,
        contentKind,
        entry.FileName,
        enabled,
      );
      await refreshContent();
    } catch (ex) {
      notify.error(t("操作失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setContentBusy(false);
    }
  };

  const removeContent = async (entry: mcserver.ServerContentEntry) => {
    if (!selectedId) return;
    const ok = await confirm(
      t("删除内容"),
      t("将删除「{0}」，无法撤销。", { "0": entry.FileName }),
      { confirmLabel: t("删除"), severity: "warning" },
    );

    if (!ok) return;
    setContentBusy(true);
    try {
      await DeleteServerContent(selectedId, contentKind, entry.FileName);
      await refreshContent();
      notify.success(t("已删除"));
    } catch (ex) {
      notify.error(t("删除失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setContentBusy(false);
    }
  };

  // ---- 玩家与权限 ----
  const refreshPlayers = useCallback(async () => {
    if (!selectedId) return;
    setPlayerLoading(true);
    try {
      const value = await GetServerPlayers(selectedId);

      setPlayers(value ?? null);
    } catch {
      setPlayers(null);
    } finally {
      setPlayerLoading(false);
    }
  }, [selectedId]);

  useEffect(() => {
    if (!selectedId || tab !== "players") return;
    void refreshPlayers();
  }, [selectedId, tab, refreshPlayers]);

  // 选中服务器时读取"崩溃自动重启"开关（开关存在 server.json 里）
  useEffect(() => {
    if (!selectedId) return;
    void GetServerAutoRestart(selectedId)
      .then((value: boolean) => setAutoRestart(!!value))
      .catch(() => setAutoRestart(false));
  }, [selectedId]);

  /**
   * 名单改动：服务器运行中优先用指令（服务端自己决定 UUID，正版/离线都对得上），
   * 已停止时直接改名单文件（离线 UUID 由后端补齐）。
   */
  const applyPlayerChange = async (
    command: string,
    fallback: () => Promise<void>,
    successText: string,
  ) => {
    if (!selectedId) return;
    setPlayerBusy(true);
    try {
      if (players?.Running) await RunServerPlayerCommand(selectedId, command);
      else await fallback();
      notify.success(successText);
      await refreshPlayers();
    } catch (ex) {
      notify.error(t("操作失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setPlayerBusy(false);
    }
  };

  const addWhitelist = async () => {
    if (!selectedId) return;
    const name = playerName.trim();

    if (!name) return;
    await applyPlayerChange(
      `whitelist add ${name}`,
      async () => {
        const existing = players?.Whitelist ?? [];

        if (existing.some((item) => item.name === name)) return;
        await SaveServerWhitelist(selectedId, [
          ...existing.map((item) => ({
            uuid: item.uuid,
            name: item.name,
          })),
          { uuid: "", name: name },
        ]);
      },
      t("已把 {0} 加入白名单", { "0": name }),
    );
    setPlayerName("");
  };

  const removeWhitelist = async (name: string) => {
    if (!selectedId) return;
    await applyPlayerChange(
      `whitelist remove ${name}`,
      async () => {
        const remaining = (players?.Whitelist ?? []).filter(
          (item) => item.name !== name,
        );

        await SaveServerWhitelist(
          selectedId,
          remaining.map((item) => ({ uuid: item.uuid, name: item.name })),
        );
      },
      t("已把 {0} 移出白名单", { "0": name }),
    );
  };

  const makeOp = async (name: string) => {
    if (!selectedId) return;
    await applyPlayerChange(
      `op ${name}`,
      async () => {
        const existing = players?.Ops ?? [];

        if (existing.some((item) => item.name === name)) return;
        await SaveServerOps(selectedId, [
          ...existing.map((item) => ({
            uuid: item.uuid,
            name: item.name,
            level: item.level,
            bypassesPlayerLimit: item.bypassesPlayerLimit,
          })),
          { uuid: "", name: name, level: 4, bypassesPlayerLimit: false },
        ]);
      },
      t("已把 {0} 设为管理员", { "0": name }),
    );
    setPlayerName("");
  };

  const removeOp = async (name: string) => {
    if (!selectedId) return;
    await applyPlayerChange(
      `deop ${name}`,
      async () => {
        const remaining = (players?.Ops ?? []).filter(
          (item) => item.name !== name,
        );

        await SaveServerOps(
          selectedId,
          remaining.map((item) => ({
            uuid: item.uuid,
            name: item.name,
            level: item.level,
            bypassesPlayerLimit: item.bypassesPlayerLimit,
          })),
        );
      },
      t("已取消 {0} 的管理员", { "0": name }),
    );
  };

  const banPlayer = async (name: string) => {
    if (!selectedId) return;
    const ok = await confirm(
      t("封禁玩家"),
      t("将把「{0}」加入封禁名单。", {
        "0": name,
      }),
      { confirmLabel: t("封禁"), severity: "warning" },
    );

    if (!ok) return;
    await applyPlayerChange(
      `ban ${name}`,
      async () => {
        const existing = players?.Banned ?? [];

        await SaveServerBanned(selectedId, [
          ...existing.map((item) => ({ ...item })),
          {
            uuid: "",
            name: name,
            created: "",
            source: "NekoLauncher",
            expires: "forever",
            reason: t("由启动器封禁"),
          },
        ]);
      },
      t("已封禁 {0}", { "0": name }),
    );
  };

  const unbanPlayer = async (name: string) => {
    if (!selectedId) return;
    await applyPlayerChange(
      `pardon ${name}`,
      async () => {
        const remaining = (players?.Banned ?? []).filter(
          (item) => item.name !== name,
        );

        await SaveServerBanned(
          selectedId,
          remaining.map((item) => ({ ...item })),
        );
      },
      t("已解封 {0}", { "0": name }),
    );
  };

  const kickPlayer = async (name: string) => {
    if (!selectedId || !players?.Running) return;
    await applyPlayerChange(
      `kick ${name}`,
      async () => undefined,
      t("已踢出 {0}", { "0": name }),
    );
  };

  const sendBroadcast = async () => {
    if (!selectedId) return;
    const message = broadcast.trim();

    if (!message || !players?.Running) return;
    try {
      await RunServerPlayerCommand(selectedId, `say ${message}`);
      setBroadcast("");
      notify.success(t("已广播"));
    } catch (ex) {
      notify.error(t("广播失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  const toggleWhitelist = async (enabled: boolean) => {
    if (!selectedId) return;
    try {
      await SetServerWhitelistEnabled(selectedId, enabled);
      await refreshPlayers();
      notify.success(enabled ? t("已开启白名单") : t("已关闭白名单"));
    } catch (ex) {
      notify.error(t("操作失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  // ---- 备份 ----
  const refreshBackups = useCallback(async () => {
    if (!selectedId) return;
    setBackupLoading(true);
    try {
      const list = await ListServerBackups(selectedId);

      setBackups(Array.isArray(list) ? list : []);
    } catch {
      /* 目录还没创建等情况：按空列表处理 */
      setBackups([]);
    } finally {
      setBackupLoading(false);
    }
  }, [selectedId]);

  useEffect(() => {
    if (!selectedId || tab !== "backup") return;
    void refreshBackups();
    void GetBackupSettings()
      .then((value) => {
        if (value) setBackupSettings(value);
      })
      .catch(() => undefined);
  }, [selectedId, tab, refreshBackups]);

  const saveBackupSettings = async () => {
    try {
      await SaveBackupSettings(backupSettings);
      notify.success(t("备份策略已保存"));
    } catch (ex) {
      notify.error(t("保存失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  const runBackupNow = async () => {
    if (!selectedId) return;
    setBackupBusy(true);
    try {
      const info = await CreateServerBackup(selectedId);

      notify.success(t("已创建备份 {0}", { "0": info?.Name ?? "" }));
      await refreshBackups();
    } catch (ex) {
      notify.error(t("备份失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setBackupBusy(false);
    }
  };

  const restoreBackup = async (name: string) => {
    if (!selectedId) return;
    const ok = await confirm(
      t("恢复备份"),
      t(
        "将用「{0}」覆盖当前服务器目录（存档、配置、mods 全部回到那一刻）。恢复前会自动留一份当前状态的安全备份。",
        { "0": name },
      ),
      { confirmLabel: t("恢复"), severity: "danger" },
    );

    if (!ok) return;
    setBackupBusy(true);
    try {
      await RestoreServerBackup(selectedId, name);
      notify.success(t("已恢复备份 {0}", { "0": name }));
      await refreshBackups();
    } catch (ex) {
      notify.error(t("恢复失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setBackupBusy(false);
    }
  };

  const removeBackup = async (name: string) => {
    if (!selectedId) return;
    const ok = await confirm(
      t("删除备份"),
      t("将删除备份「{0}」，无法撤销。", {
        "0": name,
      }),
      { confirmLabel: t("删除"), severity: "warning" },
    );

    if (!ok) return;
    try {
      await DeleteServerBackup(selectedId, name);
      await refreshBackups();
      notify.success(t("已删除备份"));
    } catch (ex) {
      notify.error(t("删除失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  return (
    <div className="relative h-full w-full flex flex-col overflow-hidden">
      {/* ============ 指挥横幅：汇总状态 + 全局动作 ============ */}
      <div className="px-6 pt-5 pb-1 flex-shrink-0">
        <div className="rounded-large border nya-border nya-panel px-4 py-3 flex flex-wrap items-center gap-x-4 gap-y-2">
          {/* 状态徽章即筛选器（再点一次取消）；在线玩家只做展示 */}
          <div className="flex flex-wrap items-center gap-1.5">
            <Chip
              className="cursor-pointer"
              color={serverFilter === "running" ? "success" : "default"}
              size="sm"
              variant={serverFilter === "running" ? "solid" : "flat"}
              onClick={() =>
                setServerFilter(serverFilter === "running" ? "all" : "running")
              }
            >
              {t("运行中 {0}", { "0": runningCount })}
            </Chip>
            <Chip
              className="cursor-pointer"
              size="sm"
              variant={serverFilter === "all" ? "solid" : "flat"}
              onClick={() => setServerFilter("all")}
            >
              {t("共 {0} 台服务器", { "0": servers.length })}
            </Chip>
            <Chip
              className="cursor-pointer"
              size="sm"
              variant={serverFilter === "stopped" ? "solid" : "flat"}
              onClick={() =>
                setServerFilter(serverFilter === "stopped" ? "all" : "stopped")
              }
            >
              {t("已停止 {0}", { "0": servers.length - runningCount })}
            </Chip>
            <Chip
              color={playersOnline > 0 ? "primary" : "default"}
              size="sm"
              variant="flat"
            >
              {t("在线玩家 {0}", { "0": playersOnline })}
            </Chip>
          </div>

          <div className="flex-1" />

          <div className="flex flex-wrap items-center gap-1.5">
            <Button
              size="sm"
              startContent={<ArrowSync20Regular />}
              variant="flat"
              onPress={() => void refreshList()}
            >
              {t("刷新")}
            </Button>
            <Button
              size="sm"
              startContent={<ArrowImport20Regular />}
              variant="flat"
              onPress={() => void importNekoser()}
            >
              {t("导入服务器包")}
            </Button>
            <Button
              color="primary"
              size="sm"
              startContent={<Add20Regular />}
              variant="flat"
              onPress={() => setCreateOpen(true)}
            >
              {t("新建服务器")}
            </Button>
          </div>
        </div>
      </div>

      {/* 主体：左右两栏布局 —— 左列服务器列表，右列详情面板 */}
      <div className="flex-1 min-h-0 flex gap-4 px-6 pb-5">
        {/* ============ 左列：服务器列表面板 ============ */}
        <div className="nya-panel flex w-[360px] min-h-0 flex-shrink-0 flex-col overflow-hidden rounded-large border nya-border p-2">
          <div className="flex flex-none items-center gap-2 px-2 pb-1.5 pt-1">
            <span className="text-[11px] text-gray-400">
              {filteredServers.length} / {servers.length}
            </span>
            <div className="flex-1" />
            <Input
              aria-label={t("搜索服务器")}
              className="w-40 max-w-full"
              size="sm"
              startContent={<Search20Regular className="h-4 w-4" />}
              value={serverSearch}
              variant="flat"
              onValueChange={setServerSearch}
            />
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto flex flex-col gap-0.5">
            {servers.length === 0 ? (
              <div className="flex flex-1 items-center justify-center p-6 text-center text-[11px] text-gray-400">
                {t("暂无服务器，点击横幅右侧「新建服务器」开始")}
              </div>
            ) : filteredServers.length === 0 ? (
              <div className="px-4 py-6 text-center text-[11px] text-gray-400">
                {t("没有符合筛选条件的服务器")}
              </div>
            ) : (
              <AnimatePresence initial={false}>
                {filteredServers.map((server) => (
                  <motion.div
                    key={server.ID}
                    layout
                    animate="center"
                    exit="exit"
                    initial="enter"
                    variants={LIST_FADE}
                  >
                    <button
                      className={`flex w-full cursor-pointer flex-col gap-1 rounded-lg px-2.5 py-2 text-left transition-colors ${
                        server.ID === selectedId
                          ? "bg-primary/10 font-semibold text-blue-600 dark:text-blue-300"
                          : "text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800"
                      }`}
                      onClick={() => setSelectedId(server.ID)}
                    >
                      <div className="flex items-center gap-2">
                        <span className="min-w-0 flex-1 truncate text-[13px]">
                          {server.Name}
                        </span>
                        {statusChip(server.Status)}
                      </div>
                      <div className="flex items-center gap-1.5 text-[10px] font-normal text-gray-400">
                        <span>{CORE_LABELS[server.Core] ?? server.Core}</span>
                        <span>·</span>
                        <span>{server.MCVersion}</span>
                        <span>·</span>
                        <span>:{server.Port}</span>
                        {server.Status === "running" ? (
                          <>
                            <span>·</span>
                            <span className="tabular-nums">
                              {server.Players}/{server.MaxPlayers}
                            </span>
                          </>
                        ) : null}
                      </div>
                    </button>
                  </motion.div>
                ))}
              </AnimatePresence>
            )}
          </div>
        </div>

        {/* ============ 右列：详情面板 ============ */}
        <div className="nya-panel flex min-w-0 flex-1 flex-col overflow-hidden rounded-large border nya-border">
          {!selected || !selectedId ? (
            <div className="flex flex-1 items-center justify-center p-8">
              <span className="text-xs leading-relaxed text-gray-400">
                {t("点击左侧列表中的服务器，即可查看控制台与配置")}
              </span>
            </div>
          ) : (
            <>
              {/* 详情头部：名称 + 状态 + 占用 + 操作按钮 */}
              <div className="flex flex-none flex-col gap-2 border-b nya-border px-5 pb-3 pt-4">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="truncate text-lg font-semibold text-gray-800 dark:text-gray-200">
                    {selected.Name}
                  </span>
                  {statusChip(selected.Status)}
                  {selected.Status === "running" ? (
                    <span className="text-xs text-gray-400 tabular-nums">
                      {t("在线")} {selected.Players}/{selected.MaxPlayers}
                    </span>
                  ) : null}
                  <div className="ml-auto flex items-center gap-1.5">
                    {selected.Status === "stopped" ? (
                      <Button
                        color="primary"
                        isDisabled={busy}
                        size="sm"
                        variant="flat"
                        onPress={() => void doStart(selected)}
                      >
                        {t("启动")}
                      </Button>
                    ) : (
                      <>
                        <Button
                          color="danger"
                          isDisabled={busy || selected.Status === "stopping"}
                          isLoading={selected.Status === "stopping"}
                          size="sm"
                          variant="flat"
                          onPress={() => void doStop(selected)}
                        >
                          {selected.Status === "stopping"
                            ? t("停止中")
                            : t("停止")}
                        </Button>
                        {selected.Status !== "stopping" ? (
                          <>
                            <Button
                              isDisabled={busy}
                              size="sm"
                              variant="light"
                              onPress={() => void doRestart(selected)}
                            >
                              {t("重启")}
                            </Button>
                            <Button
                              color="danger"
                              isDisabled={busy}
                              size="sm"
                              variant="light"
                              onPress={() => void doStop(selected, true)}
                            >
                              {t("强制停止")}
                            </Button>
                          </>
                        ) : null}
                      </>
                    )}
                    <Button
                      color="danger"
                      isDisabled={selected.Status !== "stopped"}
                      size="sm"
                      variant="light"
                      onPress={() => void doDelete(selected)}
                    >
                      {t("删除")}
                    </Button>
                  </div>
                </div>
                <div className="flex flex-wrap items-center gap-2 text-[11px] text-gray-400">
                  <span>{CORE_LABELS[selected.Core] ?? selected.Core}</span>
                  <span>·</span>
                  <span>{selected.MCVersion}</span>
                  <span>·</span>
                  <span>
                    {t("端口")} {selected.Port}
                  </span>
                  {runtime && selected.Status !== "stopped" ? (
                    <>
                      <span>·</span>
                      <span className="tabular-nums">
                        {t("CPU")} {runtime.CPUPercent.toFixed(1)}%
                      </span>
                      <span>·</span>
                      <span className="tabular-nums">
                        {t("内存")} {runtime.MemoryMB.toFixed(0)} MB
                      </span>
                    </>
                  ) : null}
                </div>
              </div>

              {/* Tab 栏 */}
              <div className="flex flex-none items-center gap-5 px-5 pb-1 pt-2">
                {(
                  [
                    ["console", t("控制台")],
                    ["config", t("配置")],
                    ["files", t("文件")],
                    ["import", t("资源导入")],
                    ["content", t("内容")],
                    ["players", t("玩家")],
                    ["backup", t("备份")],
                    ["java", t("启动参数")],
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

              {/* Tab 内容 */}
              <div className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto px-5 pb-5 pt-2">
                <SwitchTransition
                  activeKey={tab}
                  className="flex min-h-full flex-col"
                  direction={tabDirection}
                  variant="instance"
                >
                  {/* 控制台 */}
                  {tab === "console" ? (
                    <div className="flex h-full min-h-[280px] flex-col gap-2">
                      <div
                        ref={consoleRef}
                        className="nya-scroll min-h-0 flex-1 overflow-y-auto rounded-lg border border-gray-500/40 bg-gray-500/40 p-3 font-mono text-[11px] leading-relaxed text-gray-700 backdrop-blur-xl dark:border-white/40 dark:bg-white/80 dark:text-gray-800"
                      >
                        {logLength === 0 ? (
                          <div className="py-6 text-center text-gray-500 dark:text-gray-500">
                            {selected.Status === "stopped"
                              ? t("服务器未运行")
                              : t("暂无日志")}
                          </div>
                        ) : (
                          runtime?.LogLines.map((line, index) => (
                            <ConsoleLine
                              key={`${index}-${line.slice(0, 20)}`}
                              line={line}
                            />
                          ))
                        )}
                      </div>
                      <div className="flex items-center gap-1.5">
                        <Button
                          isDisabled={selected.Status !== "running"}
                          size="sm"
                          variant="flat"
                          onPress={() => void sendCommand("list")}
                        >
                          {t("列出玩家")}
                        </Button>
                        <Button
                          isDisabled={selected.Status !== "running"}
                          size="sm"
                          variant="flat"
                          onPress={() => void sendCommand("save-all")}
                        >
                          {t("保存世界")}
                        </Button>
                        <Input
                          className="min-w-0 flex-1"
                          isDisabled={selected.Status !== "running"}
                          placeholder={
                            selected.Status === "running"
                              ? t("输入指令")
                              : t("服务器未运行")
                          }
                          size="sm"
                          value={command}
                          variant="bordered"
                          onKeyDown={(event) => {
                            if (event.key === "Enter") void sendCommand();
                          }}
                          onValueChange={setCommand}
                        />
                        <Button
                          isDisabled={selected.Status !== "running"}
                          size="sm"
                          variant="flat"
                          onPress={() => void sendCommand()}
                        >
                          {t("发送")}
                        </Button>
                      </div>
                    </div>
                  ) : null}

                  {/* 配置（server.properties） */}
                  {tab === "config" ? (
                    <div className="flex flex-col gap-3">
                      <div className="flex flex-wrap items-center gap-2">
                        <Button
                          color="primary"
                          isDisabled={dirtyCount === 0}
                          size="sm"
                          variant="flat"
                          onPress={() => void saveProperties()}
                        >
                          {t("保存修改")}
                          {dirtyCount > 0 ? ` (${dirtyCount})` : ""}
                        </Button>
                        {selected.Status !== "stopped" ? (
                          <span className="text-[11px] text-warning">
                            {t("服务器运行中时修改不会生效，请先停止。")}
                          </span>
                        ) : null}
                      </div>
                      {groupedProperties.map((group) => {
                        const collapsed = collapsedGroups[group.id] ?? false;

                        return (
                          <div
                            key={group.id}
                            className={`rounded-lg border ${
                              group.id === "quick"
                                ? "border-primary/30"
                                : "border-gray-100 dark:border-gray-800/60"
                            }`}
                          >
                            <button
                              className="flex w-full cursor-pointer items-center justify-between px-4 py-2.5 text-left"
                              onClick={() =>
                                setCollapsedGroups((prev) => ({
                                  ...prev,
                                  [group.id]: !collapsed,
                                }))
                              }
                            >
                              <span className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                                {group.title}
                              </span>
                              <span className="text-[11px] text-gray-400">
                                {group.items.length}
                              </span>
                            </button>
                            <AnimatePresence initial={false}>
                              {!collapsed ? (
                                <motion.div
                                  animate={{ height: "auto", opacity: 1 }}
                                  className="overflow-hidden"
                                  exit={{ height: 0, opacity: 0 }}
                                  initial={{ height: 0, opacity: 0 }}
                                  transition={{
                                    duration: 0.2,
                                    ease: "easeInOut",
                                  }}
                                >
                                  <div className="flex flex-col gap-2 border-t border-gray-100 px-4 py-3 dark:border-gray-800/60">
                                    {group.items.length === 0 ? (
                                      <div className="text-center text-[11px] text-gray-400">
                                        {t("没有可显示的配置项")}
                                      </div>
                                    ) : (
                                      group.items.map((property) => {
                                        const value =
                                          propsDirty[property.Key] ??
                                          property.Value;
                                        const meta =
                                          PROPERTY_META[property.Key];
                                        const update = (next: string) =>
                                          setPropsDirty((prev) => ({
                                            ...prev,
                                            [property.Key]: next,
                                          }));

                                        return (
                                          <div
                                            key={property.Key}
                                            className="flex items-center gap-3"
                                          >
                                            <span
                                              className="min-w-0 flex-1"
                                              title={property.Key}
                                            >
                                              <span className="block truncate text-[12px] text-gray-600 dark:text-gray-300">
                                                {meta
                                                  ? t(meta.label)
                                                  : property.Key}
                                              </span>
                                              {meta?.desc ? (
                                                <span className="block truncate text-[10px] leading-tight text-gray-400 dark:text-gray-500">
                                                  {t(meta.desc)}
                                                </span>
                                              ) : null}
                                            </span>
                                            {isBooleanValue(property.Value) ? (
                                              <Switch
                                                isSelected={value === "true"}
                                                size="sm"
                                                onValueChange={(checked) =>
                                                  update(
                                                    checked ? "true" : "false",
                                                  )
                                                }
                                              />
                                            ) : (
                                              <Input
                                                className="max-w-[240px] flex-none"
                                                size="sm"
                                                value={value}
                                                variant="bordered"
                                                onValueChange={update}
                                              />
                                            )}
                                          </div>
                                        );
                                      })
                                    )}
                                  </div>
                                </motion.div>
                              ) : null}
                            </AnimatePresence>
                          </div>
                        );
                      })}
                    </div>
                  ) : null}

                  {/* 文件 */}
                  {tab === "files" ? (
                    <div className="flex flex-col gap-2">
                      <div className="flex flex-wrap items-center gap-1.5">
                        {filesPath ? (
                          <Button
                            isIconOnly
                            aria-label={t("上一级")}
                            size="sm"
                            variant="flat"
                            onPress={() => {
                              const parent = filesPath
                                .split("/")
                                .slice(0, -1)
                                .join("/");

                              setFilesPath(parent);
                              setEditorFile(null);
                              void loadFiles(selectedId, parent);
                            }}
                          >
                            <ArrowLeft20Regular />
                          </Button>
                        ) : null}
                        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-gray-400">
                          /{filesPath}
                        </span>
                        <Button
                          size="sm"
                          startContent={<Folder20Regular />}
                          variant="flat"
                          onPress={() =>
                            void (async () => {
                              try {
                                await OpenPath(`mc-servers/${selectedId}`);
                              } catch {
                                notify.error(t("打开文件夹失败"));
                              }
                            })()
                          }
                        >
                          {t("打开服务器目录")}
                        </Button>
                      </div>
                      <div className="rounded-lg border border-gray-100 dark:border-gray-800/60">
                        {files.length === 0 ? (
                          <div className="py-6 text-center text-[11px] text-gray-400">
                            {t("文件夹为空")}
                          </div>
                        ) : (
                          files.map((file) => (
                            <button
                              key={file.Name}
                              className="flex w-full cursor-pointer items-center gap-2 px-4 py-1.5 text-left text-[12px] transition-colors hover:bg-default-100 dark:hover:bg-gray-800"
                              onClick={() => {
                                if (file.IsDir) {
                                  const next = filesPath
                                    ? `${filesPath}/${file.Name}`
                                    : file.Name;

                                  setFilesPath(next);
                                  setEditorFile(null);
                                  void loadFiles(selectedId, next);
                                } else {
                                  void openFile(file.Name);
                                }
                              }}
                            >
                              <span className="w-4 flex-none text-center">
                                {file.IsDir ? "📁" : "📄"}
                              </span>
                              <span className="min-w-0 flex-1 truncate text-gray-700 dark:text-gray-300">
                                {file.Name}
                              </span>
                              {!file.IsDir ? (
                                <span className="flex-none text-[10px] text-gray-400 tabular-nums">
                                  {formatSize(file.Size)}
                                </span>
                              ) : null}
                            </button>
                          ))
                        )}
                      </div>
                    </div>
                  ) : null}

                  {/* 资源导入 */}
                  {tab === "import" ? (
                    <div className="flex flex-col gap-3">
                      <div className="flex items-center gap-3 rounded-lg border border-primary/30 px-4 py-3">
                        <div className="min-w-0 flex-1">
                          <div className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                            {t("导出 NekoSer 包")}
                          </div>
                          <div className="truncate text-[11px] text-gray-400">
                            {t(
                              "迁移/备份整包服务器（含存档与配置）；需先停止服务器",
                            )}
                          </div>
                        </div>
                        <Button
                          isDisabled={selected.Status !== "stopped"}
                          size="sm"
                          startContent={<ArrowExportLtr20Regular />}
                          variant="flat"
                          onPress={() => void exportNekoser()}
                        >
                          {t("导出")}
                        </Button>
                      </div>
                      {[
                        {
                          key: "world" as const,
                          title: t("导入存档"),
                          hint: t("选择存档压缩包（zip），解压到当前世界目录"),
                          enabled: true,
                        },
                        {
                          key: "mod" as const,
                          title: t("导入模组"),
                          hint: t(
                            "复制 mod jar 到 mods/ 目录（仅 Fabric/NeoForge）",
                          ),
                          enabled:
                            selected.Core === "fabric" ||
                            selected.Core === "neoforge",
                        },
                        {
                          key: "plugin" as const,
                          title: t("导入插件"),
                          hint: t("复制插件 jar 到 plugins/ 目录（仅 Paper）"),
                          enabled: selected.Core === "paper",
                        },
                      ].map((item) => (
                        <div
                          key={item.key}
                          className="flex items-center gap-3 rounded-lg border border-gray-100 px-4 py-3 dark:border-gray-800/60"
                        >
                          <div className="min-w-0 flex-1">
                            <div className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                              {item.title}
                            </div>
                            <div className="truncate text-[11px] text-gray-400">
                              {item.hint}
                            </div>
                          </div>
                          <Button
                            isDisabled={!item.enabled}
                            size="sm"
                            variant="flat"
                            onPress={() => void importVia(item.key)}
                          >
                            {t("选择文件")}
                          </Button>
                        </div>
                      ))}
                      <div className="text-[11px] text-gray-400">
                        {t("导入完成后重启服务器生效。")}
                      </div>
                    </div>
                  ) : null}

                  {/* JVM 启动参数 */}
                  {/* 服务端内容（mods / plugins） */}
                  {tab === "content" ? (
                    <div className="flex flex-col gap-3">
                      <div className="flex flex-col gap-3 rounded-lg border nya-border p-4">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                            {contentKind === "plugins"
                              ? t("从 Modrinth 安装插件")
                              : t("从 Modrinth 安装模组")}
                          </span>
                          <span className="text-[11px] text-gray-400">
                            {t("只列适配 {0} 的版本", {
                              "0": `${selected.Core} ${selected.MCVersion}`,
                            })}
                          </span>
                        </div>

                        <div className="flex items-center gap-1.5">
                          <Input
                            className="min-w-0 flex-1"
                            placeholder={t(
                              "搜索名称，例如 sodium / essentials",
                            )}
                            size="sm"
                            value={contentQuery}
                            variant="bordered"
                            onKeyDown={(event) => {
                              if (event.key === "Enter") void searchContent();
                            }}
                            onValueChange={setContentQuery}
                          />
                          <Button
                            isLoading={contentSearching}
                            size="sm"
                            startContent={
                              contentSearching ? undefined : <Search20Regular />
                            }
                            variant="flat"
                            onPress={() => void searchContent()}
                          >
                            {t("搜索")}
                          </Button>
                        </div>

                        {contentError ? (
                          <span className="text-[11px] text-danger">
                            {contentError}
                          </span>
                        ) : null}

                        {contentResults.length > 0 ? (
                          <div className="nya-scroll flex max-h-64 flex-col gap-1 overflow-y-auto">
                            {contentResults.map((project) => (
                              <div
                                key={project.project_id}
                                className="flex items-center gap-2 rounded-lg border nya-border px-3 py-1.5"
                              >
                                <span className="min-w-0 flex-1">
                                  <span className="block truncate text-[12px] text-gray-700 dark:text-gray-300">
                                    {project.title}
                                  </span>
                                  <span className="block truncate text-[10px] text-gray-400">
                                    {project.description}
                                  </span>
                                </span>
                                <Button
                                  isDisabled={contentInstalling !== ""}
                                  isLoading={
                                    contentInstalling === project.project_id
                                  }
                                  size="sm"
                                  variant="flat"
                                  onPress={() => void installContent(project)}
                                >
                                  {t("安装")}
                                </Button>
                              </div>
                            ))}
                          </div>
                        ) : null}
                      </div>

                      <div className="flex flex-col gap-2">
                        <div className="flex items-center gap-2">
                          <span className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                            {contentKind === "plugins"
                              ? t("已安装插件")
                              : t("已安装模组")}
                          </span>
                          <span className="text-[11px] text-gray-400">
                            {contentEntries.length} {t("个")}
                          </span>
                          <span className="ml-auto text-[11px] text-gray-400">
                            {t("改动在重启服务器后生效")}
                          </span>
                          <Button
                            isLoading={contentLoading}
                            size="sm"
                            startContent={
                              contentLoading ? undefined : (
                                <ArrowSync20Regular />
                              )
                            }
                            variant="light"
                            onPress={() => void refreshContent()}
                          >
                            {t("刷新")}
                          </Button>
                        </div>

                        {contentEntries.length === 0 ? (
                          <div className="rounded-lg border border-dashed border-gray-300/80 px-4 py-6 text-center text-[11px] text-gray-400 dark:border-gray-700">
                            {t("暂无内容，可从上方搜索安装")}
                          </div>
                        ) : (
                          contentEntries.map((entry) => (
                            <div
                              key={entry.FileName}
                              className="flex flex-wrap items-center gap-2 rounded-lg border nya-border px-3.5 py-2.5"
                            >
                              <Switch
                                aria-label={entry.Name}
                                color="primary"
                                isDisabled={contentBusy}
                                isSelected={entry.Enabled}
                                size="sm"
                                onValueChange={(value) =>
                                  void toggleContent(entry, value)
                                }
                              />
                              <span className="min-w-0 flex-1">
                                <span className="block truncate text-[12px] text-gray-700 dark:text-gray-300">
                                  {entry.Name}
                                </span>
                                <span className="block truncate font-mono text-[10px] text-gray-400">
                                  {entry.FileName} ·{" "}
                                  {formatSize(Number(entry.SizeBytes))}
                                </span>
                              </span>
                              <Button
                                color="danger"
                                isDisabled={contentBusy}
                                size="sm"
                                variant="light"
                                onPress={() => void removeContent(entry)}
                              >
                                {t("删除")}
                              </Button>
                            </div>
                          ))
                        )}
                      </div>
                    </div>
                  ) : null}

                  {/* 玩家与权限 */}
                  {tab === "players" ? (
                    <div className="flex flex-col gap-3">
                      {/* 在线 + 广播 */}
                      <div className="flex flex-col gap-3 rounded-lg border nya-border p-4">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                            {t("在线玩家")}
                          </span>
                          <span className="text-[11px] text-gray-400">
                            {players?.Running
                              ? (players?.Online?.length ?? 0) > 0
                                ? `${players?.Online?.length ?? 0} ${t("人")}`
                                : t("当前没有玩家在线")
                              : t("服务器未运行（在线列表需要 RCON）")}
                          </span>
                          <Button
                            className="ml-auto"
                            isLoading={playerLoading}
                            size="sm"
                            startContent={
                              playerLoading ? undefined : <ArrowSync20Regular />
                            }
                            variant="light"
                            onPress={() => void refreshPlayers()}
                          >
                            {t("刷新")}
                          </Button>
                        </div>

                        <div className="flex flex-wrap gap-1.5">
                          {(players?.Online ?? []).map((name) => (
                            <span
                              key={name}
                              className="flex items-center gap-1.5 rounded-full border nya-border px-2.5 py-1 text-[11px]"
                            >
                              <Person20Regular />
                              {name}
                              <button
                                className="cursor-pointer text-gray-400 hover:text-danger"
                                onClick={() => void kickPlayer(name)}
                              >
                                {t("踢出")}
                              </button>
                              <button
                                className="cursor-pointer text-gray-400 hover:text-danger"
                                onClick={() => void banPlayer(name)}
                              >
                                {t("封禁")}
                              </button>
                            </span>
                          ))}
                        </div>

                        <div className="flex items-center gap-1.5">
                          <Input
                            className="min-w-0 flex-1"
                            isDisabled={!players?.Running}
                            placeholder={t("输入要广播的内容")}
                            size="sm"
                            value={broadcast}
                            variant="bordered"
                            onKeyDown={(event) => {
                              if (event.key === "Enter") void sendBroadcast();
                            }}
                            onValueChange={setBroadcast}
                          />
                          <Button
                            isDisabled={!players?.Running || !broadcast.trim()}
                            size="sm"
                            variant="flat"
                            onPress={() => void sendBroadcast()}
                          >
                            {t("广播")}
                          </Button>
                        </div>
                      </div>

                      {/* 名单 */}
                      <div className="flex flex-col gap-3 rounded-lg border nya-border p-4">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                            {t("白名单")}
                          </span>
                          <Switch
                            aria-label={t("白名单")}
                            color="primary"
                            isDisabled={!players}
                            isSelected={!!players?.WhitelistEnabled}
                            size="sm"
                            onValueChange={(value) =>
                              void toggleWhitelist(value)
                            }
                          />
                          <span className="text-[11px] text-gray-400">
                            {players?.Running
                              ? t("服务器运行中：改动会作为指令立即生效。")
                              : t(
                                  "服务器已停止：改动直接写入名单文件（离线 UUID 自动补齐）。",
                                )}
                          </span>
                        </div>

                        <div className="flex items-center gap-1.5">
                          <Input
                            className="min-w-0 flex-1"
                            placeholder={t(
                              "玩家名（1–16 位字母、数字或下划线）",
                            )}
                            size="sm"
                            value={playerName}
                            variant="bordered"
                            onKeyDown={(event) => {
                              if (event.key === "Enter") void addWhitelist();
                            }}
                            onValueChange={setPlayerName}
                          />
                          <Button
                            isDisabled={!playerName.trim()}
                            size="sm"
                            variant="flat"
                            onPress={() => void addWhitelist()}
                          >
                            {t("加入白名单")}
                          </Button>
                          <Button
                            isDisabled={!playerName.trim()}
                            size="sm"
                            variant="flat"
                            onPress={() => void makeOp(playerName.trim())}
                          >
                            {t("设为管理员")}
                          </Button>
                        </div>

                        {[
                          {
                            key: "whitelist",
                            title: t("白名单"),
                            items: (players?.Whitelist ?? []).map((item) => ({
                              name: item.name,
                              detail: item.uuid,
                              remove: () => void removeWhitelist(item.name),
                            })),
                          },
                          {
                            key: "ops",
                            title: t("管理员（OP）"),
                            items: (players?.Ops ?? []).map((item) => ({
                              name: item.name,
                              detail: `${t("权限等级")} ${item.level}`,
                              remove: () => void removeOp(item.name),
                            })),
                          },
                          {
                            key: "banned",
                            title: t("封禁名单"),
                            items: (players?.Banned ?? []).map((item) => ({
                              name: item.name,
                              detail: item.reason || t("无理由"),
                              remove: () => void unbanPlayer(item.name),
                            })),
                          },
                        ].map((group) => (
                          <div
                            key={group.key}
                            className="flex flex-col gap-1.5"
                          >
                            <span className="text-[12px] font-medium text-gray-600 dark:text-gray-300">
                              {group.title} · {group.items.length}
                            </span>
                            {group.items.length === 0 ? (
                              <span className="text-[11px] text-gray-400">
                                {t("（空）")}
                              </span>
                            ) : (
                              <div className="flex flex-col gap-1">
                                {group.items.map((item) => (
                                  <div
                                    key={`${group.key}-${item.name}`}
                                    className="flex items-center gap-2 rounded-lg border nya-border px-3 py-1.5"
                                  >
                                    <span className="min-w-0 flex-1 truncate text-[12px] text-gray-700 dark:text-gray-300">
                                      {item.name}
                                    </span>
                                    <span className="truncate font-mono text-[10px] text-gray-400">
                                      {item.detail}
                                    </span>
                                    <Button
                                      color="danger"
                                      isDisabled={playerBusy}
                                      size="sm"
                                      variant="light"
                                      onPress={item.remove}
                                    >
                                      {t("移除")}
                                    </Button>
                                  </div>
                                ))}
                              </div>
                            )}
                          </div>
                        ))}
                      </div>
                    </div>
                  ) : null}

                  {/* 备份 */}
                  {tab === "backup" ? (
                    <div className="flex flex-col gap-3">
                      {/* 策略 */}
                      <div className="flex flex-col gap-3 rounded-lg border nya-border p-4">
                        <div className="flex flex-wrap items-center gap-3">
                          <div className="min-w-0 flex-1">
                            <div className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                              {t("自动备份")}
                            </div>
                            <div className="text-[11px] text-gray-400">
                              {t(
                                "开启后每 {0} 小时自动备份一次；每次备份前都会先把世界刷盘。",
                                { "0": String(backupSettings.IntervalHours) },
                              )}
                            </div>
                          </div>
                          <Switch
                            aria-label={t("自动备份")}
                            color="primary"
                            isSelected={backupSettings.Enabled}
                            size="sm"
                            onValueChange={(value) =>
                              setBackupSettings((prev) => ({
                                ...prev,
                                Enabled: value,
                              }))
                            }
                          />
                        </div>

                        <div className="grid grid-cols-3 gap-3">
                          <Input
                            label={t("间隔（小时）")}
                            min={1}
                            size="sm"
                            type="number"
                            value={String(backupSettings.IntervalHours)}
                            variant="bordered"
                            onValueChange={(value) =>
                              setBackupSettings((prev) => ({
                                ...prev,
                                IntervalHours: Math.max(1, Number(value) || 1),
                              }))
                            }
                          />
                          <Input
                            label={t("保留份数（0 = 不限）")}
                            min={0}
                            size="sm"
                            type="number"
                            value={String(backupSettings.KeepCount)}
                            variant="bordered"
                            onValueChange={(value) =>
                              setBackupSettings((prev) => ({
                                ...prev,
                                KeepCount: Math.max(0, Number(value) || 0),
                              }))
                            }
                          />
                          <Input
                            label={t("保留天数（0 = 不限）")}
                            min={0}
                            size="sm"
                            type="number"
                            value={String(backupSettings.KeepDays)}
                            variant="bordered"
                            onValueChange={(value) =>
                              setBackupSettings((prev) => ({
                                ...prev,
                                KeepDays: Math.max(0, Number(value) || 0),
                              }))
                            }
                          />
                        </div>

                        <div className="flex flex-wrap items-center gap-2">
                          <Button
                            color="primary"
                            isLoading={backupBusy}
                            size="sm"
                            startContent={
                              backupBusy ? undefined : (
                                <ArrowDownload20Regular />
                              )
                            }
                            variant="flat"
                            onPress={() => void runBackupNow()}
                          >
                            {t("立即备份")}
                          </Button>
                          <Button
                            isDisabled={backupBusy}
                            size="sm"
                            startContent={<CheckmarkCircle20Regular />}
                            variant="flat"
                            onPress={() => void saveBackupSettings()}
                          >
                            {t("保存备份策略")}
                          </Button>
                          <span className="text-[11px] text-gray-400">
                            {selected.Status === "stopped"
                              ? t("服务器已停止，备份会直接打包目录。")
                              : t(
                                  "服务器运行中：将先暂停世界写入（save-off）再打包，完成后自动恢复。",
                                )}
                          </span>
                        </div>
                      </div>

                      {/* 列表 */}
                      <div className="flex flex-col gap-2">
                        <div className="flex items-center gap-2">
                          <span className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                            {t("备份列表")}
                          </span>
                          <span className="text-[11px] text-gray-400">
                            {backups.length} {t("份")}
                          </span>
                          <Button
                            className="ml-auto"
                            isLoading={backupLoading}
                            size="sm"
                            startContent={
                              backupLoading ? undefined : <ArrowSync20Regular />
                            }
                            variant="light"
                            onPress={() => void refreshBackups()}
                          >
                            {t("刷新")}
                          </Button>
                        </div>

                        {backups.length === 0 ? (
                          <div className="rounded-lg border border-dashed border-gray-300/80 px-4 py-6 text-center text-[11px] text-gray-400 dark:border-gray-700">
                            {t("暂无备份")}
                          </div>
                        ) : (
                          backups.map((item) => (
                            <div
                              key={item.Name}
                              className="flex flex-wrap items-center gap-2 rounded-lg border nya-border px-3.5 py-2.5"
                            >
                              <span className="min-w-0 flex-1">
                                <span className="block truncate font-mono text-[12px] text-gray-700 dark:text-gray-300">
                                  {item.Name}
                                </span>
                                <span className="block text-[10px] text-gray-400">
                                  {new Date(
                                    Number(item.CreatedAt) * 1000,
                                  ).toLocaleString()}{" "}
                                  · {formatSize(Number(item.SizeBytes))}
                                </span>
                              </span>
                              <Chip
                                color={item.Hot ? "warning" : "default"}
                                size="sm"
                                variant="flat"
                              >
                                {item.Hot ? t("热备份") : t("冷备份")}
                              </Chip>
                              <Button
                                isDisabled={backupBusy}
                                size="sm"
                                variant="flat"
                                onPress={() => void restoreBackup(item.Name)}
                              >
                                {t("恢复")}
                              </Button>
                              <Button
                                color="danger"
                                isDisabled={backupBusy}
                                size="sm"
                                variant="light"
                                onPress={() => void removeBackup(item.Name)}
                              >
                                {t("删除")}
                              </Button>
                            </div>
                          ))
                        )}
                      </div>
                    </div>
                  ) : null}

                  {/* 启动参数 */}
                  {tab === "java" ? (
                    <div className="flex flex-col gap-3">
                      <div className="flex flex-wrap items-center gap-2">
                        <Button
                          color="primary"
                          size="sm"
                          variant="flat"
                          onPress={() => void saveLaunchOptions()}
                        >
                          {t("保存启动参数")}
                        </Button>
                        {selected.Status !== "stopped" ? (
                          <span className="text-[11px] text-warning">
                            {t("服务器运行中时修改不会生效，请先停止。")}
                          </span>
                        ) : null}
                      </div>

                      {/* 崩溃自动重启 */}
                      <div className="flex items-center gap-3 rounded-lg border nya-border px-4 py-3">
                        <div className="min-w-0 flex-1">
                          <div className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                            {t("崩溃自动重启")}
                          </div>
                          <div className="text-[11px] text-gray-400">
                            {t(
                              "进程异常退出时自动重新启动（10 分钟内最多 3 次，手动停止不触发）",
                            )}
                          </div>
                        </div>
                        <Switch
                          aria-label={t("崩溃自动重启")}
                          color="primary"
                          isSelected={autoRestart}
                          size="sm"
                          onValueChange={(value) =>
                            void toggleAutoRestart(value)
                          }
                        />
                      </div>
                      {/* 内存 */}
                      <div className="grid grid-cols-2 gap-3">
                        <Input
                          endContent={
                            <span className="text-[11px] text-gray-400">
                              MB
                            </span>
                          }
                          isInvalid={
                            !!launchMemoryMax && Number(launchMemoryMax) <= 0
                          }
                          label={t("最大内存（-Xmx，0 = 不限制）")}
                          size="sm"
                          type="number"
                          value={launchMemoryMax}
                          variant="bordered"
                          onValueChange={setLaunchMemoryMax}
                        />
                        <Input
                          endContent={
                            <span className="text-[11px] text-gray-400">
                              MB
                            </span>
                          }
                          isInvalid={
                            !!launchMemoryMin &&
                            Number(launchMemoryMin) >
                              (Number(launchMemoryMax) || Infinity)
                          }
                          label={t("初始内存（-Xms，0 = 不设置）")}
                          size="sm"
                          type="number"
                          value={launchMemoryMin}
                          variant="bordered"
                          onValueChange={setLaunchMemoryMin}
                        />
                      </div>

                      {/* 高级参数（额外 JVM 参数 + 命令行预览）：默认收起 */}
                      <button
                        className="flex w-full cursor-pointer items-center justify-between rounded-lg px-1 py-1 text-left"
                        onClick={() => setJvmAdvancedOpen((v) => !v)}
                      >
                        <span className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                          {t("高级参数")}
                        </span>
                        <span
                          className={`text-[11px] text-gray-400 transition-transform ${jvmAdvancedOpen ? "rotate-180" : ""}`}
                        >
                          ▾
                        </span>
                      </button>
                      <AnimatePresence initial={false}>
                        {jvmAdvancedOpen ? (
                          <motion.div
                            animate={{ height: "auto", opacity: 1 }}
                            className="overflow-hidden"
                            exit={{ height: 0, opacity: 0 }}
                            initial={{ height: 0, opacity: 0 }}
                            transition={{ duration: 0.2, ease: "easeInOut" }}
                          >
                            <div className="flex flex-col gap-3 pb-1">
                              {/* 预设 + 额外参数 */}
                              <div>
                                <div className="mb-1 flex flex-wrap items-center justify-between gap-2">
                                  <span className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                                    {t("额外 JVM 参数（每行一个）")}
                                  </span>
                                  <div className="flex gap-1.5">
                                    <Button
                                      size="sm"
                                      variant="flat"
                                      onPress={() =>
                                        setLaunchExtraArgs(
                                          AIKAR_FLAGS.join("\n"),
                                        )
                                      }
                                    >
                                      {t("Aikar 优化参数")}
                                    </Button>
                                    <Button
                                      size="sm"
                                      variant="light"
                                      onPress={() => setLaunchExtraArgs("")}
                                    >
                                      {t("清空")}
                                    </Button>
                                  </div>
                                </div>
                                <Textarea
                                  classNames={{
                                    input: "font-mono text-[11px]",
                                  }}
                                  minRows={4}
                                  placeholder={t("如 -XX:+UseG1GC")}
                                  size="sm"
                                  value={launchExtraArgs}
                                  variant="bordered"
                                  onValueChange={setLaunchExtraArgs}
                                />
                                <div className="mt-1 text-[11px] text-gray-400">
                                  {t(
                                    "预设为社区标准的 Aikar GC 调优参数，适合 12GB 以下的堆。",
                                  )}
                                </div>
                              </div>

                              {/* 命令行预览 */}
                              <div>
                                <div className="mb-1 text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                                  {t("启动命令预览")}
                                </div>
                                <div className="nya-scroll overflow-x-auto rounded-lg bg-black/85 p-3 font-mono text-[11px] leading-relaxed text-gray-200 dark:bg-white/80 dark:text-gray-800">
                                  {launchPreview}
                                </div>
                              </div>
                            </div>
                          </motion.div>
                        ) : null}
                      </AnimatePresence>
                    </div>
                  ) : null}
                </SwitchTransition>
              </div>
            </>
          )}
        </div>
      </div>

      {/* ============ 创建服务器（表单 → EULA 确认） ============ */}
      <Modal
        isOpen={createOpen}
        size="md"
        {...modalBehaviorProps}
        onClose={() => !creating && setCreateOpen(false)}
      >
        <ModalContent>
          <ModalShell
            icon={<Server20Regular />}
            subtitle={eulaStep ? t("创建前需要同意 Minecraft EULA") : undefined}
            title={t("新建服务器")}
            onClose={() => !creating && setCreateOpen(false)}
          >
            {eulaStep ? (
              <div className="flex flex-col gap-3">
                <div className="rounded-lg bg-warning/10 px-4 py-3 text-[12px] leading-relaxed text-gray-700 dark:text-gray-300">
                  {t(
                    "创建服务器即代表你同意 Minecraft 最终用户许可协议（EULA）：不得将服务器用于商业盈利等用途。同意后启动器会自动在服务器目录写入 eula=true。",
                  )}
                  <div className="mt-1">
                    <a
                      className="cursor-pointer text-primary underline"
                      href="https://aka.ms/MinecraftEULA"
                      onClick={(event) => {
                        event.preventDefault();
                        void OpenPath("https://aka.ms/MinecraftEULA");
                      }}
                    >
                      {t("查看 EULA 全文")}
                    </a>
                  </div>
                </div>
                <div className="flex justify-end gap-2">
                  <Button
                    isDisabled={creating}
                    size="sm"
                    variant="light"
                    onPress={() => setEulaStep(false)}
                  >
                    {t("拒绝")}
                  </Button>
                  <Button
                    color="primary"
                    isLoading={creating}
                    size="sm"
                    onPress={() => void createConfirmed()}
                  >
                    {t("同意并创建")}
                  </Button>
                </div>
              </div>
            ) : (
              <div className="flex flex-col gap-3">
                <Input
                  label={t("名称")}
                  placeholder={t("我的服务器")}
                  size="sm"
                  value={form.name}
                  variant="bordered"
                  onValueChange={(value) =>
                    setForm((prev) => ({ ...prev, name: value }))
                  }
                />
                <div className="flex flex-col gap-1.5">
                  <span className="text-[12px] text-gray-500 dark:text-gray-400">
                    {t("核心")}
                  </span>
                  <div className="grid grid-cols-4 gap-1.5">
                    {CORE_OPTIONS.map((core) => (
                      <button
                        key={core}
                        className={`cursor-pointer rounded-lg px-2 py-1.5 text-[12px] font-semibold transition-colors ${
                          form.core === core
                            ? "bg-primary/15 text-primary ring-1 ring-primary/40"
                            : "text-gray-600 hover:bg-default-100 dark:text-gray-300 dark:hover:bg-gray-800"
                        }`}
                        onClick={() => setForm((prev) => ({ ...prev, core }))}
                      >
                        {CORE_LABELS[core]}
                      </button>
                    ))}
                  </div>
                </div>
                <div className="flex flex-col gap-1.5">
                  <span className="text-[12px] text-gray-500 dark:text-gray-400">
                    {t("MC 版本")}
                  </span>
                  <Select
                    aria-label={t("MC 版本")}
                    isLoading={mcLoading}
                    items={mcVersions.map((v) => ({ key: v }))}
                    popoverProps={selectPopoverProps}
                    selectedKeys={form.mcVersion ? [form.mcVersion] : []}
                    size="sm"
                    variant="bordered"
                    onSelectionChange={(keys) => {
                      const key = [...keys][0];

                      if (key !== undefined) {
                        setForm((prev) => ({
                          ...prev,
                          mcVersion: String(key),
                        }));
                      }
                    }}
                  >
                    {(item) => (
                      <SelectItem key={item.key} textValue={item.key}>
                        {item.key}
                      </SelectItem>
                    )}
                  </Select>
                </div>
                {form.core !== "vanilla" ? (
                  <div className="flex flex-col gap-1.5">
                    <span className="text-[12px] text-gray-500 dark:text-gray-400">
                      {t("服务端版本")}
                    </span>
                    <div className="nya-scroll h-32 overflow-y-auto rounded-lg border border-gray-200 p-1 dark:border-gray-800">
                      {coreVersionLoading ? (
                        <div className="py-6 text-center text-[11px] text-gray-400">
                          {t("正在获取版本列表…")}
                        </div>
                      ) : coreVersions.length === 0 ? (
                        <div className="py-6 text-center text-[11px] text-gray-400">
                          {t("该版本暂无可用构建")}
                        </div>
                      ) : (
                        coreVersions.map((version) => (
                          <button
                            key={version}
                            className={`flex w-full cursor-pointer items-center justify-between rounded-lg px-3 py-1 text-left font-mono text-[12px] transition-colors ${
                              form.coreVersion === version
                                ? "bg-primary/15 font-semibold text-primary"
                                : "text-gray-600 hover:bg-default-100 dark:text-gray-300 dark:hover:bg-gray-800"
                            }`}
                            onClick={() =>
                              setForm((prev) => ({
                                ...prev,
                                coreVersion: version,
                              }))
                            }
                          >
                            <span>{version}</span>
                            {form.coreVersion === version ? (
                              <span className="text-[10px]">✓</span>
                            ) : null}
                          </button>
                        ))
                      )}
                    </div>
                  </div>
                ) : null}
                <button
                  className="flex w-full cursor-pointer items-center justify-between rounded-lg px-1 py-1 text-left"
                  onClick={() => setCreateAdvancedOpen((v) => !v)}
                >
                  <span className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                    {t("高级选项")}
                  </span>
                  <span
                    className={`text-[11px] text-gray-400 transition-transform ${createAdvancedOpen ? "rotate-180" : ""}`}
                  >
                    ▾
                  </span>
                </button>
                <AnimatePresence initial={false}>
                  {createAdvancedOpen ? (
                    <motion.div
                      animate={{ height: "auto", opacity: 1 }}
                      className="overflow-hidden"
                      exit={{ height: 0, opacity: 0 }}
                      initial={{ height: 0, opacity: 0 }}
                      transition={{ duration: 0.2, ease: "easeInOut" }}
                    >
                      <div className="flex flex-col gap-3 pb-1">
                        <div className="flex gap-3">
                          <Input
                            label={t("端口")}
                            size="sm"
                            value={form.port}
                            variant="bordered"
                            onValueChange={(value) =>
                              setForm((prev) => ({ ...prev, port: value }))
                            }
                          />
                          <Input
                            label={t("最大玩家数")}
                            size="sm"
                            value={form.maxPlayers}
                            variant="bordered"
                            onValueChange={(value) =>
                              setForm((prev) => ({
                                ...prev,
                                maxPlayers: value,
                              }))
                            }
                          />
                        </div>
                        <Select
                          label={t("Java 运行时")}
                          popoverProps={selectPopoverProps}
                          selectedKeys={[form.javaPath || "__auto__"]}
                          size="sm"
                          variant="bordered"
                          onSelectionChange={(keys) => {
                            const key = [...keys][0];

                            if (key !== undefined) {
                              setForm((prev) => ({
                                ...prev,
                                javaPath: key === "__auto__" ? "" : String(key),
                              }));
                            }
                          }}
                        >
                          <SelectItem key="__auto__">
                            {t("自动选择（按 MC 版本匹配）")}
                          </SelectItem>
                          <>
                            {javaOptions.map((java) => (
                              <SelectItem key={java.path}>
                                {java.version
                                  ? t("Java {0} · {1}", {
                                      "0": java.version,
                                      "1": shortJavaPath(java.path),
                                    })
                                  : shortJavaPath(java.path)}
                              </SelectItem>
                            ))}
                          </>
                        </Select>
                        {requiredMajor ? (
                          <div className="text-[11px] text-gray-500 dark:text-gray-400">
                            {t("Minecraft {0} 需要 Java {1} 及以上。", {
                              "0": form.mcVersion,
                              "1": String(requiredMajor),
                            })}
                          </div>
                        ) : null}
                      </div>
                    </motion.div>
                  ) : null}
                </AnimatePresence>
                <div className="mt-1 flex justify-end gap-2 border-t border-gray-100 pt-3 dark:border-gray-800/60">
                  <Button
                    size="sm"
                    variant="light"
                    onPress={() => setCreateOpen(false)}
                  >
                    {t("取消")}
                  </Button>
                  <Button
                    color="primary"
                    isDisabled={
                      !form.name.trim() ||
                      !form.mcVersion ||
                      (form.core !== "vanilla" && !form.coreVersion)
                    }
                    size="sm"
                    onPress={() => setEulaStep(true)}
                  >
                    {t("下一步")}
                  </Button>
                </div>
              </div>
            )}
          </ModalShell>
        </ModalContent>
      </Modal>

      {/* NekoSer 整包传输进度（导出/导入共用，完成后由终值事件隐藏） */}
      <AnimatePresence>
        {transfer ? (
          <motion.div
            animate={{ opacity: 1, y: 0 }}
            className="nya-panel absolute bottom-5 left-1/2 z-30 w-72 -translate-x-1/2 rounded-large border nya-border px-4 py-3"
            exit={{ opacity: 0, y: 12 }}
            initial={{ opacity: 0, y: 12 }}
          >
            <div className="mb-1.5 flex items-center justify-between text-[12px] font-medium text-gray-700 dark:text-gray-300">
              <span>
                {transfer.phase === "export"
                  ? t("正在导出服务器包")
                  : t("正在导入服务器包")}
              </span>
              <span className="tabular-nums text-gray-400">
                {transfer.percent}%
              </span>
            </div>
            <Progress
              aria-label={t("传输进度")}
              size="sm"
              value={transfer.percent}
            />
          </motion.div>
        ) : null}
      </AnimatePresence>

      {/* 文件查看 / 编辑遮罩弹层（语法高亮 + 补全，语言按扩展名推断） */}
      <CodeFileModal
        key={editorFile}
        fileName={editorFile ?? ""}
        isOpen={!!editorFile}
        value={editorContent}
        onClose={() => setEditorFile(null)}
        onSave={() => saveFile()}
        onValueChange={setEditorContent}
      />
    </div>
  );
};

export default ServersPage;
