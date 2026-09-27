/*
 * 游戏崩溃提示。
 *
 * 后端（launch.GameLaunchService.observeProcess）在游戏进程退出时发布
 * GameLaunchPhaseExited 快照：手动停止 / 正常退出（退出码 0）/ 异常退出（退出码非 0）。
 * 这里只负责把"异常退出"这一种情况翻译成用户能看懂的弹窗。
 *
 * 之所以放在前端而不是 Go 侧弹原生对话框：崩溃发生在游戏进程侧，那时用户往往正在
 * 看启动器界面，用应用内统一的 NekoPrompt 展示可以顺带给出日志入口，且不打断
 * 已经排队的其它浮层（NekoPrompt 是全局单例，新请求会顶掉旧的）。
 */
import type { launch } from "../../wailsjs/go/models";

import { openLogViewer } from "../components/LogViewer";
import { showDialogBox } from "../components/overlay/dialog";
import { notify } from "../components/overlay/dialog";
import { DiagnoseCrash } from "../../wailsjs/go/bindings/LauncherAPI";
import { OpenPath } from "../../wailsjs/go/bindings/SystemAPI";
import { t } from "../i18n";

/** GameLaunchPhase 取值（与 Go 侧常量一致）。 */
export const LAUNCH_PHASE_EXITED = 4;

/** 崩溃判定结果：是否崩溃 + 退出码（无法解析时为 null）。 */
export interface CrashInfo {
  crashed: boolean;
  exitCode: number | null;
}

/**
 * 判断一次退出快照是否属于"异常退出"。
 *
 * 只认后端在 observeProcess 里给出的两种明确信号，避免误报：
 *   - 标题为"游戏异常退出"（手动停止与正常退出走的是另外两个标题）；
 *   - 或消息形如"退出代码：<非 0>"。
 * 后端的标题文案是中文硬编码，所以这里同时看退出码，不单纯依赖文案。
 */
export function detectCrash(snapshot: launch.GameLaunchSnapshot): CrashInfo {
  if (snapshot?.Phase !== LAUNCH_PHASE_EXITED) {
    return { crashed: false, exitCode: null };
  }

  const message = String(snapshot.Message ?? "");
  const matched = /退出代码[：:]\s*(-?\d+)/.exec(message);
  const exitCode = matched ? Number(matched[1]) : null;

  // 正常退出（退出码 0）与手动停止（后端标题为"游戏已停止"）不算崩溃
  const crashed =
    exitCode !== null
      ? exitCode !== 0
      : /异常/.test(String(snapshot.Title ?? ""));

  return { crashed, exitCode };
}

/** 已经提示过的快照 Revision，避免同一次崩溃被重复弹窗。 */
let lastNotifiedRevision = -1;

/** 诊断文本：复制给开发者/群里求助时用（纯文本，不依赖弹窗渲染）。 */
export function formatDiagnosis(
  diagnosis: launch.CrashDiagnosis | null,
  exitCode: number | null,
): string {
  const lines: string[] = [
    "NekoLauncher 崩溃诊断",
    `退出代码：${exitCode === null ? "未知" : exitCode}`,
  ];

  if (diagnosis?.Description) lines.push(`崩溃描述：${diagnosis.Description}`);
  if (diagnosis?.Exception) lines.push(`异常：${diagnosis.Exception}`);
  if (diagnosis?.ReportPath) lines.push(`崩溃报告：${diagnosis.ReportPath}`);
  if (diagnosis?.Suspected?.length) {
    lines.push("可能原因：");
    diagnosis.Suspected.forEach((item, index) => {
      const suggestion = diagnosis.Suggestions?.[index] ?? "";

      lines.push(`  - ${item}${suggestion ? `：${suggestion}` : ""}`);
    });
  }
  lines.push("（完整日志见启动器「运行日志」窗口）");

  return lines.join("\n");
}

async function copyDiagnosis(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);

    return true;
  } catch {
    try {
      const area = document.createElement("textarea");

      area.value = text;
      area.style.position = "fixed";
      area.style.opacity = "0";
      document.body.appendChild(area);
      area.select();
      const ok = document.execCommand("copy");

      area.remove();

      return ok;
    } catch {
      return false;
    }
  }
}

/**
 * 对退出快照按需弹出崩溃提示；返回是否弹了窗（便于调用方/测试判断）。
 * 同一 Revision 只会提示一次——launch:changed 可能因重连等原因重复送达。
 */
export function notifyCrashIfNeeded(
  snapshot: launch.GameLaunchSnapshot,
): boolean {
  const { crashed, exitCode } = detectCrash(snapshot);

  if (!crashed) return false;

  const revision = Number(snapshot?.Revision ?? 0);

  if (revision === lastNotifiedRevision) return false;
  lastNotifiedRevision = revision;

  const codeText = exitCode === null ? t("未知") : String(exitCode);

  // 先弹窗（不让用户等诊断），诊断回来后再把结论追加到同一次对话里不可行——
  // 所以这里先异步拿诊断，拿到就带上结论；拿不到（老版本/后端异常）退回原提示。
  void (async () => {
    let diagnosis: launch.CrashDiagnosis | null = null;

    try {
      diagnosis = await DiagnoseCrash();
    } catch {
      diagnosis = null;
    }

    const suspected = diagnosis?.Suspected ?? [];
    const suggestions = diagnosis?.Suggestions ?? [];
    const detail = suspected
      .map((item, index) => {
        const suggestion = suggestions[index] ?? "";

        return suggestion ? `· ${t(item)}\n  ${t(suggestion)}` : `· ${t(item)}`;
      })
      .join("\n");

    const message = detail
      ? t("Minecraft 进程以非零退出码结束（退出代码：{0}）。\n\n{1}\n\n{2}", {
          "0": codeText,
          "1": t(diagnosis?.Summary ?? ""),
          "2": detail,
        })
      : t(
          "Minecraft 进程以非零退出码结束（退出代码：{0}）。\n\n常见原因：Java 版本不匹配、内存分配不足、模组冲突或缺少前置。可打开启动日志查看具体报错。",
          { "0": codeText },
        );

    const buttons = [
      { label: t("查看日志"), id: "logs" },
      { label: t("复制诊断信息"), id: "copy" },
      ...(diagnosis?.ReportPath
        ? [{ label: t("打开崩溃报告"), id: "report" }]
        : []),
      { label: t("知道了"), id: "ok", default: true },
    ];

    const result = await showDialogBox({
      title: t("游戏异常退出"),
      message,
      severity: "error",
      buttons,
    });

    if (result === "logs") {
      openLogViewer();

      return;
    }
    if (result === "copy") {
      const ok = await copyDiagnosis(formatDiagnosis(diagnosis, exitCode));

      if (ok) notify.success(t("诊断信息已复制到剪贴板"));
      else notify.error(t("复制失败，请手动选中文本复制"));

      return;
    }
    if (result === "report" && diagnosis?.ReportPath) {
      try {
        await OpenPath(diagnosis.ReportPath);
      } catch {
        notify.error(t("打开崩溃报告失败"));
      }
    }
  })();

  return true;
}
