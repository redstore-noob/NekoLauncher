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
import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Button } from "@heroui/react";
import {
  ArrowClockwise20Regular as RefreshIcon,
  DocumentText20Regular,
  Open20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";

import { GetLogText } from "../../../wailsjs/go/bindings/LauncherAPI";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { useLogViewer } from "../LogViewer";
import { logLineClass } from "../../lib/logs";
import { asText } from "../../lib/guards";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 卡片里显示的日志尾部行数 */
const LOG_TAIL_LINES = 80;
/** 启动/运行期间的轮询间隔（ms）：快照事件只在状态变化时推送，日志需自行轮询 */
const LOG_POLL_INTERVAL_MS = 2000;

interface LaunchLogCardProps {
  /** 启动阶段：0 空闲 / 1 准备中 / 2 运行中 / 3 失败 / 4 已退出 */
  phase: number;
  /** 启动快照版本号，变化时立即重新拉取日志 */
  revision: number;
}

interface Diagnosis {
  title: string;
  hint: string;
  tone: "danger" | "warning";
}

/**
 * 日志归因规则：命中日志文本即给出可能原因与处理建议。
 * 顺序即优先级（先命中先返回）。
 */
const DIAGNOSIS_RULES: { test: RegExp; diagnosis: Diagnosis }[] = [
  {
    test: /UnsupportedClassVersionError|class file version/i,
    diagnosis: {
      title: t("Java 版本过低"),
      hint: t("该实例需要更高版本的 Java，可在「设置 → Java」里更换后重试。"),
      tone: "warning",
    },
  },
  {
    test: /OutOfMemoryError|Java heap space|GC overhead limit/i,
    diagnosis: {
      title: t("内存不足"),
      hint: t("可在「设置 → 内存」里调高最大内存，或关掉其他占内存的程序。"),
      tone: "warning",
    },
  },
  {
    test: /Could not find or load main class|NoClassDefFoundError|ClassNotFoundException/i,
    diagnosis: {
      title: t("缺少依赖库"),
      hint: t("版本文件可能不完整，建议到「实例」页重新下载该版本。"),
      tone: "danger",
    },
  },
  {
    test: /Failed to authenticate|InvalidCredentialsException|invalid_grant|401 Unauthorized/i,
    diagnosis: {
      title: t("账号凭据失效"),
      hint: t("登录状态已过期，请到「账户」页重新登录后再启动。"),
      tone: "warning",
    },
  },
  {
    test: /A fatal error has been detected|The game crashed|crash-reports/i,
    diagnosis: {
      title: t("游戏崩溃"),
      hint: t("完整报告在实例目录的 crash-reports 文件夹里，多半是模组冲突。"),
      tone: "danger",
    },
  },
  {
    test: /UnknownHostException|ConnectException|SocketException|timed out|连接超时/i,
    diagnosis: {
      title: t("网络异常"),
      hint: t("检查网络连接，或在「设置 → 下载」里更换下载源后重试。"),
      tone: "warning",
    },
  },
  {
    test: /Access is denied|Permission denied|拒绝访问/i,
    diagnosis: {
      title: t("文件权限不足"),
      hint: t(
        "文件可能被杀毒软件拦截或被占用，可尝试关闭实时防护或以管理员身份运行。",
      ),
      tone: "warning",
    },
  },
];

/** 取日志中最后一次记录的进程退出代码（后端在退出时写入「退出代码：N」） */
function lastExitCode(logText: string): number | null {
  const matches = logText.match(/退出代码：(-?\d+)/g);

  if (!matches || matches.length === 0) return null;
  const value = Number(/-?\d+/.exec(matches[matches.length - 1])?.[0]);

  return Number.isFinite(value) ? value : null;
}

/** 按优先级匹配日志，给出可能的失败原因；无匹配返回 null */
function diagnoseLog(logText: string): Diagnosis | null {
  for (const rule of DIAGNOSIS_RULES) {
    if (rule.test.test(logText)) return rule.diagnosis;
  }

  return null;
}

/** 阶段 → 状态文案与配色 */
function statusOf(
  phase: number,
  exitCode: number | null,
  stoppedManually: boolean,
): { text: string; tileClass: string; valueClass: string } {
  switch (phase) {
    case 1:
      return {
        text: t("启动中"),
        tileClass:
          "from-amber-400 via-orange-500 to-amber-500 shadow-orange-500/30",
        valueClass:
          "bg-gradient-to-r from-amber-500 via-orange-500 to-amber-500",
      };
    case 2:
      return {
        text: t("运行中"),
        tileClass:
          "from-emerald-400 via-teal-500 to-cyan-500 shadow-emerald-500/30",
        valueClass:
          "bg-gradient-to-r from-emerald-500 via-teal-500 to-cyan-500",
      };
    case 3:
      return {
        text: t("启动失败"),
        tileClass: "from-rose-400 via-red-500 to-orange-500 shadow-rose-500/30",
        valueClass: "bg-gradient-to-r from-rose-500 via-red-500 to-orange-500",
      };
    case 4:
      if (stoppedManually) {
        return {
          text: t("已停止"),
          tileClass:
            "from-slate-400 via-gray-500 to-zinc-500 shadow-gray-500/30",
          valueClass:
            "bg-gradient-to-r from-slate-500 via-gray-500 to-zinc-500",
        };
      }

      return exitCode !== null && exitCode !== 0
        ? {
            text: t("异常退出"),
            tileClass:
              "from-rose-400 via-red-500 to-orange-500 shadow-rose-500/30",
            valueClass:
              "bg-gradient-to-r from-rose-500 via-red-500 to-orange-500",
          }
        : {
            text: t("已退出"),
            tileClass:
              "from-slate-400 via-gray-500 to-zinc-500 shadow-gray-500/30",
            valueClass:
              "bg-gradient-to-r from-slate-500 via-gray-500 to-zinc-500",
          };
    default:
      return {
        text: t("未启动"),
        tileClass: "from-slate-400 via-gray-500 to-zinc-600 shadow-gray-500/30",
        valueClass: "bg-gradient-to-r from-slate-400 via-gray-500 to-zinc-500",
      };
  }
}

/**
 * 运行日志卡片：轮询 LauncherAPI.GetLogText 显示启动器与游戏进程输出尾部，
 * 游戏退出后从日志里取退出代码并给出可能的失败原因。
 */
const LaunchLogCard: React.FC<LaunchLogCardProps> = ({ phase, revision }) => {
  const { openLogs } = useLogViewer();
  const [logText, setLogText] = useState("");
  const [hasLoaded, setHasLoaded] = useState(false);
  const logBoxRef = useRef<HTMLDivElement | null>(null);

  const reload = useCallback(async () => {
    try {
      const text = await GetLogText();

      setLogText(asText(text));
    } catch {
      /* 未注入 ctx 时忽略 */
    } finally {
      setHasLoaded(true);
    }
  }, []);

  const isLive = phase === 1 || phase === 2;

  useEffect(() => {
    void reload();
    if (!isLive) return;
    const timer = window.setInterval(() => void reload(), LOG_POLL_INTERVAL_MS);

    return () => window.clearInterval(timer);
  }, [reload, revision, isLive]);

  // 逐行事件：后端每产出一行就推过来（launch:logLine），这里直接追加，
  // 日志窗口不再等到下一次轮询才刷新。2s 的全量轮询保留作重连兜底——
  // 万一事件漏了（页面后挂载、事件丢失），下一次轮询会把整段文本校准回来。
  useEffect(() => {
    const off = EventsOn(
      "launch:logLine",
      (payload: { Tag?: string; Line?: string }) => {
        const line = payload?.Line ?? "";

        if (!line) return;
        setLogText((prev) => (prev ? `${prev}\n${line}` : line));
        setHasLoaded(true);
      },
    );

    return () => {
      if (typeof off === "function") off();
    };
  }, []);

  const lines = useMemo(() => {
    if (!logText) return [];
    const all = logText.split("\n");

    return all.slice(-LOG_TAIL_LINES);
  }, [logText]);

  // 新日志总是推到最底部
  useEffect(() => {
    const box = logBoxRef.current;

    if (box) box.scrollTop = box.scrollHeight;
  }, [logText]);

  const exitCode = useMemo(() => lastExitCode(logText), [logText]);
  const stoppedManually = useMemo(
    () => /游戏进程已手动停止/.test(logText),
    [logText],
  );
  const diagnosis = useMemo(
    () => (phase === 3 || phase === 4 ? diagnoseLog(logText) : null),
    [phase, logText],
  );
  const status = statusOf(phase, exitCode, stoppedManually);

  return (
    <HomeCard
      action={
        <div className="flex items-center gap-1">
          <Button
            isIconOnly
            className="h-7 w-7 min-w-7 text-gray-400"
            radius="full"
            size="sm"
            title={t("查看完整日志")}
            variant="light"
            onClick={openLogs}
          >
            <Open20Regular />
          </Button>
          <Button
            isIconOnly
            className="h-7 w-7 min-w-7 text-gray-400"
            radius="full"
            size="sm"
            title={t("刷新日志")}
            variant="light"
            onClick={() => void reload()}
          >
            <RefreshIcon />
          </Button>
        </div>
      }
      icon={<DocumentText20Regular />}
      label={t("运行日志")}
      live={isLive}
      tileClass={status.tileClass}
      value={status.text}
      valueClass={status.valueClass}
    >
      {/* 退出代码 + 归因建议 */}
      {exitCode !== null && phase === 4 && !stoppedManually ? (
        <div className="nya-enter nya-stagger-1 flex items-center gap-1.5">
          <span
            className={`
              rounded-full px-2 py-0.5 text-[10px] font-medium tabular-nums
              ${
                exitCode === 0
                  ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-300"
                  : "bg-rose-500/10 text-rose-600 dark:text-rose-300"
              }
            `}
          >
            {t("退出代码")} {exitCode}
          </span>
          {exitCode !== 0 && !diagnosis ? (
            <span className="text-[10px] text-gray-400">
              {t("日志里没有明显错误")}
            </span>
          ) : null}
        </div>
      ) : null}

      {diagnosis ? (
        <div
          className={`
            nya-enter nya-stagger-2 flex items-start gap-2 rounded-2xl px-2.5 py-2
            ${
              diagnosis.tone === "danger"
                ? "bg-danger/10 text-danger"
                : "bg-warning-500/15 text-warning-600 dark:text-warning-400"
            }
          `}
        >
          <span className="mt-px flex-none">
            <Warning20Regular className="h-4 w-4" />
          </span>
          <span className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="text-[11px] font-semibold">
              {t("可能是")}
              {diagnosis.title}
            </span>
            <span className="text-[10px] leading-snug">{diagnosis.hint}</span>
          </span>
        </div>
      ) : phase === 3 ? (
        <div className="nya-enter nya-stagger-2 rounded-2xl bg-danger/10 px-2.5 py-2 text-[10px] leading-snug text-danger">
          {t("启动没有完成，日志末尾记录了原因。")}
        </div>
      ) : null}

      {/* 日志尾部 */}
      <div
        ref={logBoxRef}
        className="
          nya-scroll max-h-[96px] min-h-[56px] overflow-y-auto rounded-2xl
          bg-black/5 px-2.5 py-2 font-mono text-[10px] leading-relaxed
          dark:bg-white/5
        "
      >
        {lines.length > 0 ? (
          lines.map((line, index) => (
            <div
              key={`${index}-${line.slice(0, 16)}`}
              className={`break-all whitespace-pre-wrap ${logLineClass(line)}`}
            >
              {line || " "}
            </div>
          ))
        ) : (
          <div className="flex h-[56px] flex-col items-center justify-center gap-1 text-center font-sans text-[11px] leading-relaxed text-gray-400">
            {hasLoaded ? <>{t("还没有日志")}</> : t("正在读取日志…")}
          </div>
        )}
      </div>

      {logText ? (
        <div className="text-center text-[10px] text-gray-400 tabular-nums">
          {t("共 {0} 行 · 显示最后 {1} 行", {
            "0": logText.split("\n").length,
            "1": lines.length,
          })}
        </div>
      ) : null}
    </HomeCard>
  );
};

export default LaunchLogCard;
