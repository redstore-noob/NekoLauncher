/*
 * 插件高危动作的两道闸：**确认**（NekoPrompt）与**公示**（左下角 NekoAlert）。
 *
 * 权限声明只让用户在安装页看得出插件请求了什么，看不出它此刻正在做什么。这里补上：
 *  - 会拉起进程 / 写磁盘 / 改写启动参数的动作用 NekoPrompt 让用户**当场同意**，
 *    拒绝即以错误结束（插件不能把"用户拒绝"当成成功）；
 *  - 撤销得掉但值得知道的动作（切实例 / 启停内容 / 注入全局样式）只公示不拦；
 *  - 同意按（插件 + 动作）记在**本次运行**内：批处理（一次装 20 个 mod）只问一次；
 *    拒绝同样记下，避免插件用对话框刷屏——重新加载插件即可重新询问。
 *
 * 三条硬约束：
 *  - **公示是宿主行为，插件关不掉**，它存在的意义正是让插件不能悄悄改东西；
 *  - **NekoAlert 是单例**：同一插件同一动作 4 秒只弹一条，全局 3 秒最多 4 条，
 *    否则用户除了闪屏什么都读不到；
 *  - **只公示/确认"改"**：读类 API 不在这里——危险读走权限与宿主侧路径校验
 *    （见 docs/Extensions_Guide.md §5），逐次弹窗只会把真正该看的东西淹掉。
 *
 * 文案走 i18n（zh-CN 原文即词典 key，其余语言缺条目时自动回落中文）。
 */
import type { PluginManifest } from "./types";

import { notify, showDialogBox } from "../components/overlay/dialog";
import { t } from "../i18n";

/** 用户对一次确认的回答 */
export type SensitiveAnswer = "once" | "session" | "deny";

/** 高敏感动作的规格 */
export interface SensitiveActionSpec {
  /** 警示级别：改磁盘 / 改全局状态用 warning，只改"当前选择"用 info */
  level: "info" | "warning";
  /** confirm = 先弹 NekoPrompt 拿同意；announce = 只公示不拦 */
  gate: "confirm" | "announce";
  /** 动作的中文短名（确认框与错误文案里用） */
  label: string;
  /** 公示文案模板：{0} 插件名，{1} 细节 */
  template: string;
  /** 确认框正文（gate=confirm 时必填）：说清"允许之后它能干什么" */
  prompt?: string;
}

/**
 * 需要确认 / 公示的动作表。这份表就是"哪些插件行为必须让用户知道、哪些必须先点头"
 * 的唯一定义——加动作时同时改这里和调用点，api-gate 测试会盯着调用点是否真的过闸。
 *
 * 分级口径：
 *  - `confirm`：会拉起进程、会写磁盘、会改写启动参数（等同决定下次执行什么程序）；
 *  - `announce`：撤销得掉、且宿主界面自己也允许无确认地做（启停 mod / 换实例 / 换主题）。
 */
export const SENSITIVE_ACTIONS = {
  /** 发起游戏启动 */
  launch: {
    level: "warning",
    gate: "confirm",
    label: "启动游戏",
    template: "插件「{0}」发起了游戏启动{1}",
    prompt: "该插件请求直接启动游戏（会拉起 Minecraft 进程）。",
  },
  /** 下载 / 安装内容（写磁盘） */
  "install-content": {
    level: "warning",
    gate: "confirm",
    label: "下载 / 安装内容",
    template: "插件「{0}」正在下载 / 安装{1}",
    prompt:
      "该插件请求下载内容并写入你的实例目录（Mod / 资源包 / 整合包 / Java 运行时）。",
  },
  /** 写实例启动档案：Java 路径、包装命令、JVM 参数都在里面 */
  "modify-instance": {
    level: "warning",
    gate: "confirm",
    label: "改写实例启动档案",
    template: "插件「{0}」保存了实例启动档案{1}",
    prompt:
      "该插件请求改写实例启动档案：Java 路径、包装命令、JVM 参数。这些会在下次启动时生效——等同于由它决定启动什么程序。",
  },
  /** 写启动器全局启动设置：同上，但作用于所有实例 */
  "modify-settings": {
    level: "warning",
    gate: "confirm",
    label: "改写启动器设置",
    template: "插件「{0}」保存了启动器设置",
    prompt:
      "该插件请求改写启动器全局设置：Java 路径、包装命令、JVM / 游戏参数、环境变量。这些会在下次启动时生效——等同于由它决定启动什么程序。",
  },
  /** 启用 / 禁用实例内容（重命名用户文件，撤销得掉） */
  "toggle-content": {
    level: "warning",
    gate: "announce",
    label: "启停实例内容",
    template: "插件「{0}」{1}",
  },
  /** 注入全局 CSS（影响整个启动器外观，卸载即失效） */
  "global-styles": {
    level: "warning",
    gate: "announce",
    label: "注入全局样式",
    template: "插件「{0}」注入了全局样式{1}",
  },
  /** 切换"当前选中"实例（只改选择，不改文件） */
  "switch-instance": {
    level: "info",
    gate: "announce",
    label: "切换当前实例",
    template: "插件「{0}」把当前实例切到{1}",
  },
} as const satisfies Record<string, SensitiveActionSpec>;

export type SensitiveAction = keyof typeof SENSITIVE_ACTIONS;

/** 确认框的公共尾注：把"同意一次 / 同意一轮 / 拒绝"的后果说清 */
const DECISION_FOOTER =
  "同意后本次运行内不再询问；拒绝会让这个动作在本次运行内直接失败（重新加载插件可重新询问）。";

/** 公示条展示时长：比普通提示长一点，够读完"谁 + 做了什么 + 对象" */
export const SENSITIVE_ALERT_DURATION_MS = 5000;

/** 闸门：确认 + 公示 + 本次运行内的决定记录 */
export interface SensitiveGate {
  /** 过闸：需要时先弹确认框，被拒绝就抛错；随后按级别公示 */
  request: (
    manifest: PluginManifest,
    action: SensitiveAction,
    detail?: string,
  ) => Promise<void>;
  /** 只公示不过闸（announce 级动作的同步调用点用） */
  announce: (
    manifest: PluginManifest,
    action: SensitiveAction,
    detail?: string,
  ) => boolean;
  /** 忘掉某插件的全部决定（插件重新加载时调用） */
  forget: (pluginId: string) => void;
}

export interface SensitiveGateOptions {
  now?: () => number;
  show?: (message: string, level: SensitiveActionSpec["level"]) => void;
  /** 注入确认框（默认走 NekoPrompt）；测试用它模拟用户的选择 */
  ask?: (
    manifest: PluginManifest,
    spec: SensitiveActionSpec,
    message: string,
  ) => Promise<SensitiveAnswer>;
  /** 同一插件同一动作的公示重申窗口（ms） */
  dedupeWindowMs?: number;
  /** 全局公示突发窗口（ms）与窗口内上限 */
  burstWindowMs?: number;
  burstMax?: number;
}

/** 默认确认框：NekoPrompt，三个按钮，默认落在"拒绝"上（回车不该等于同意） */
async function askWithPrompt(
  manifest: PluginManifest,
  _spec: SensitiveActionSpec,
  message: string,
): Promise<SensitiveAnswer> {
  const result = await showDialogBox({
    title: t("插件「{0}」请求确认", { "0": manifest.name || manifest.id }),
    message,
    severity: "warning",
    buttons: [
      { label: t("允许一次"), id: "once" },
      { label: t("本次运行内允许"), id: "session" },
      { label: t("拒绝"), id: "deny", default: true },
    ],
  });

  if (result === "once") return "once";
  if (result === "session") return "session";

  return "deny";
}

/** buildPrompt 组装确认框正文：动作说明 + 本次对象 + 公共尾注 */
export function buildPrompt(
  spec: SensitiveActionSpec,
  detail?: string,
): string {
  return [
    spec.prompt ?? "",
    detail ? t("本次操作：{0}", { "0": detail }) : "",
    DECISION_FOOTER,
  ]
    .filter(Boolean)
    .join("\n");
}

/** deniedError 拒绝时的错误（插件必须能区分"被拒绝"与"调用成功"） */
function deniedError(manifest: PluginManifest, spec: SensitiveActionSpec) {
  return new Error(
    t("插件「{0}」的「{1}」操作被用户拒绝", {
      "0": manifest.name || manifest.id,
      "1": spec.label,
    }),
  );
}

export function createSensitiveGate(
  options: SensitiveGateOptions = {},
): SensitiveGate {
  const now = options.now ?? (() => Date.now());
  const show =
    options.show ??
    ((message, level) =>
      level === "warning"
        ? notify.warning(message, SENSITIVE_ALERT_DURATION_MS)
        : notify.info(message, SENSITIVE_ALERT_DURATION_MS));
  const ask = options.ask ?? askWithPrompt;
  const dedupeWindowMs = options.dedupeWindowMs ?? 4000;
  const burstWindowMs = options.burstWindowMs ?? 3000;
  const burstMax = options.burstMax ?? 4;

  /** (插件id + 动作) → 本次运行内的决定（只在用户选了"本次运行内允许/拒绝"时写入） */
  const decisions = new Map<string, "allow" | "deny">();
  /** 在途的确认框：同一动作的并发调用共用一个框，不会弹出一串 */
  const asking = new Map<string, Promise<SensitiveAnswer>>();
  /** (插件id + 动作) → 上次公示时间 */
  const lastByKey = new Map<string, number>();
  /** 最近公示时间戳（仅保留突发窗口内的） */
  const recent: number[] = [];

  const specOf = (action: SensitiveAction): SensitiveActionSpec | undefined =>
    SENSITIVE_ACTIONS[action];

  const announce = (
    manifest: PluginManifest,
    action: SensitiveAction,
    detail?: string,
  ): boolean => {
    const spec = specOf(action);

    if (!spec) return false;

    const key = `${manifest.id}\u0000${action}`;
    const stamp = now();
    const last = lastByKey.get(key);

    if (last !== undefined && stamp - last < dedupeWindowMs) return false;

    while (recent.length > 0 && stamp - recent[0] >= burstWindowMs) {
      recent.shift();
    }
    if (recent.length >= burstMax) return false;

    lastByKey.set(key, stamp);
    recent.push(stamp);

    const message = t(spec.template, {
      "0": manifest.name || manifest.id,
      "1": detail ?? "",
    });

    show(message.trim(), spec.level);

    return true;
  };

  return {
    announce,
    forget: (pluginId: string) => {
      const prefix = `${pluginId}\u0000`;

      for (const key of [...decisions.keys()]) {
        if (key.startsWith(prefix)) decisions.delete(key);
      }
    },
    request: async (manifest, action, detail) => {
      const spec = specOf(action);

      if (!spec) return;

      const key = `${manifest.id}\u0000${action}`;

      if (spec.gate === "confirm") {
        const decided = decisions.get(key);

        if (decided === "deny") throw deniedError(manifest, spec);
        if (decided !== "allow") {
          // 并发调用共用一个确认框：插件一口气发起 5 次，用户只被问一次
          let pending = asking.get(key);

          if (!pending) {
            pending = ask(manifest, spec, buildPrompt(spec, detail));
            asking.set(key, pending);
            void pending.finally(() => asking.delete(key));
          }

          const answer = await pending;

          if (answer === "deny") {
            decisions.set(key, "deny");
            throw deniedError(manifest, spec);
          }
          if (answer === "session") decisions.set(key, "allow");
        }
      }

      announce(manifest, action, detail);
    },
  };
}

/** 应用内单例：所有插件共用一份决定记录与公示节流 */
const sensitiveGate = createSensitiveGate();

/**
 * requireSensitiveApproval 过闸：需要时先弹 NekoPrompt 拿同意，随后公示。
 * 被拒绝时抛错——调用点必须让这个错误冒出去，不能吞掉当成成功。
 */
export function requireSensitiveApproval(
  manifest: PluginManifest,
  action: SensitiveAction,
  detail?: string,
): Promise<void> {
  return sensitiveGate.request(manifest, action, detail);
}

/**
 * announceSensitive 只公示不过闸（announce 级动作的同步调用点用）。
 * 返回是否真的弹了。
 */
export function announceSensitive(
  manifest: PluginManifest,
  action: SensitiveAction,
  detail?: string,
): boolean {
  return sensitiveGate.announce(manifest, action, detail);
}

/** forgetSensitiveDecisions 插件重新加载时忘掉它的决定（拒绝过也能重新被询问） */
export function forgetSensitiveDecisions(pluginId: string): void {
  sensitiveGate.forget(pluginId);
}

/** sensitiveDetail 统一细节格式：非空时包成中文括号，空串则整段消失 */
export function sensitiveDetail(text: string | undefined): string {
  const trimmed = String(text ?? "").trim();

  return trimmed ? `（${trimmed}）` : "";
}

/** sensitiveFileName 从路径里取文件名（路径分隔符两种都认） */
export function sensitiveFileName(path: string): string {
  const parts = String(path ?? "").split(/[\\/]/);

  return parts[parts.length - 1] || String(path ?? "");
}
