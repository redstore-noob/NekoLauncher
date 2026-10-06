/*
 * 启动日志的增量流（onLogLine 的实现）。
 *
 * 宿主没有"日志行"事件：日志只有 LauncherAPI.GetLogText()（整份文本），启动器
 * 自己的日志卡片也是轮询 + 取尾部。于是这里做同样的事，但为插件加上三件必要的事：
 *
 *  - **增量**：记住已消费到的字符位，每次只把新增部分切成行推给订阅者（不是每次
 *    把整份日志重发一遍）；
 *  - **窗口化**：日志爆发时（模组加载刷屏）单批只保留末尾窗口，并如实报告丢了多少行；
 *  - **背压**：上一拍还没返回就跳过这一拍；窗口隐藏时暂停轮询，回到前台补一拍
 *    （复用 lib/visibility 的既有约定）。
 *
 * 只推送**已完整**的行：最后一个换行之后没写完的残片先攒着，等换行到了再算一行。
 * 所有插件共用同一条轮询（单例 + 订阅计数起停），不是每个插件各拉一份。
 */
import type { LogLineBatch, LogLineOptions } from "./types";

import { GetLogText } from "../../wailsjs/go/bindings/LauncherAPI";
import { startVisiblePoll } from "../lib/visibility";

/** 轮询间隔：与启动器日志卡片的 2s 相比收紧到 1s，插件面板有实时观感又不至于压垮 IO */
const POLL_INTERVAL_MS = 1000;
/** 单批行数上限：超过就只保留末尾这么多行 */
const MAX_BATCH_LINES = 200;
/** 单批字符上限：超长行（堆栈）也算得动 */
const MAX_BATCH_CHARS = 256 * 1024;
/** tailLines 上限 */
const MAX_TAIL_LINES = 2000;

interface Subscription {
  listener: (batch: LogLineBatch) => void;
  tailLines: number;
}

/** LogStream 一条日志流：订阅者、字符光标与行切分状态都在这里 */
export interface LogStream {
  /** 订阅新增行；返回取消函数。首个订阅者会先给流"定标"（见下方 prime） */
  subscribe: (
    listener: (batch: LogLineBatch) => void,
    options?: LogLineOptions,
  ) => () => void;
  /** 跑一拍：读日志 → 切新增行 → 批量投递。定时器与测试都调它 */
  poll: () => Promise<void>;
  /** 当前订阅者数量（单例据此起停定时器） */
  size: () => number;
}

/** splitLog 把日志文本切成"完整行 + 末尾残片" */
function splitLog(text: string): { lines: string[]; carry: string } {
  const parts = text.split(/\r?\n/);
  const carry = parts.pop() ?? "";

  return { lines: parts, carry };
}

/** clampTailLines 收敛 tailLines 到 [0, MAX_TAIL_LINES] */
function clampTailLines(value: number | undefined): number {
  const parsed = Number(value ?? 0);

  if (!isFinite(parsed) || parsed <= 0) return 0;

  return Math.min(MAX_TAIL_LINES, Math.floor(parsed));
}

/**
 * createLogStream 造一条日志流。readLog 注入是为了让测试不必碰 Wails 绑定；
 * 应用内用 subscribeLogLines 的单例（绑 GetLogText + 可见性轮询）。
 */
export function createLogStream(readLog: () => Promise<string>): LogStream {
  const subscriptions = new Set<Subscription>();
  /** 已消费到的字符位 */
  let cursor = 0;
  /** 未结束的残片 */
  let carry = "";
  /** 累计完整行数 */
  let totalLines = 0;
  /** 是否已定标：定标前不投递增量，只用于把光标摆到当前日志末尾 */
  let primed = false;
  let priming = false;
  /** 背压：上一拍未返回时跳过这一拍 */
  let polling = false;

  const deliver = (sub: Subscription, batch: LogLineBatch) => {
    try {
      sub.listener(batch);
    } catch (error) {
      // 单个插件的回调抛错不影响其它订阅者
      console.error("[plugins] onLogLine 回调抛错：", error);
    }
  };

  /** 给单个订阅者补发尾部（订阅时 tailLines > 0） */
  const deliverTail = async (sub: Subscription) => {
    try {
      const text = await readLog();
      const { lines } = splitLog(text);
      const tail = lines.slice(-sub.tailLines);

      deliver(sub, {
        lines: tail,
        droppedLines: Math.max(0, lines.length - tail.length),
        totalLines: lines.length,
        rotated: false,
      });
    } catch {
      // 读不到日志就当作没有尾部可补，订阅仍然有效（只等新行）
    }
  };

  /** prime 定标：把光标摆到当前日志末尾，并给需要尾部的订阅者补一段 */
  const prime = async () => {
    if (priming || primed) return;
    priming = true;
    try {
      const text = await readLog();
      const { lines, carry: tail } = splitLog(text);

      cursor = text.length;
      // 未写完的最后一行要留着：下一个换行到达时它是"续上"而不是空行
      carry = tail;
      totalLines = lines.length;
      primed = true;
      for (const sub of subscriptions) {
        if (sub.tailLines > 0) {
          const tail = lines.slice(-sub.tailLines);

          deliver(sub, {
            lines: tail,
            droppedLines: Math.max(0, lines.length - tail.length),
            totalLines: lines.length,
            rotated: false,
          });
        }
      }
    } catch {
      // 定标失败：保持未定标，下一次订阅/轮询再试
    } finally {
      priming = false;
    }
  };

  const poll = async () => {
    if (polling) return;
    if (!primed) {
      await prime();

      return;
    }
    polling = true;
    try {
      const text = await readLog();
      let rotated = false;

      // 日志被清空或换文件（变短）→ 从头重读，并如实标记
      if (text.length < cursor) {
        rotated = true;
        cursor = 0;
        carry = "";
        totalLines = 0;
      }
      const chunk = text.slice(cursor);

      cursor = text.length;
      if (chunk === "" && !rotated) return;

      const { lines, carry: nextCarry } = splitLog(carry + chunk);

      carry = nextCarry;
      totalLines += lines.length;

      let window = lines;
      let droppedLines = 0;

      if (window.length > MAX_BATCH_LINES) {
        droppedLines = window.length - MAX_BATCH_LINES;
        window = window.slice(-MAX_BATCH_LINES);
      }
      // 超长行（单行几 MB 的堆栈）再按字符数截一遍：宁可丢字符，不卡住插件
      let chars = 0;
      let from = window.length;

      while (from > 0) {
        chars += window[from - 1].length + 1;
        if (chars > MAX_BATCH_CHARS) break;
        from--;
      }
      if (from > 0) {
        droppedLines += from;
        window = window.slice(from);
      }
      if (window.length === 0 && !rotated) return;

      const batch: LogLineBatch = {
        lines: window,
        droppedLines,
        totalLines,
        rotated,
      };

      for (const sub of subscriptions) deliver(sub, batch);
    } catch {
      // 读取失败：跳过这一拍，下一拍照常
    } finally {
      polling = false;
    }
  };

  return {
    subscribe: (listener, options) => {
      const sub: Subscription = {
        listener,
        tailLines: clampTailLines(options?.tailLines),
      };
      const first = subscriptions.size === 0;

      subscriptions.add(sub);
      if (first && !primed) {
        void prime();
      } else if (sub.tailLines > 0) {
        void deliverTail(sub);
      }

      return () => {
        subscriptions.delete(sub);
      };
    },
    poll,
    size: () => subscriptions.size,
  };
}

/** 应用内单例与其轮询定时器（订阅计数归零即停，回到零再订阅会重新起） */
let logStream: LogStream | null = null;
let stopPolling: (() => void) | null = null;

/**
 * subscribeLogLines 订阅启动日志新增行（应用内共享一条轮询）。
 * 返回取消函数；宿主在插件卸载/重载时自动调用（见 api.ts 的 onLogLine）。
 */
export function subscribeLogLines(
  listener: (batch: LogLineBatch) => void,
  options?: LogLineOptions,
): () => void {
  logStream ??= createLogStream(() => GetLogText());
  const stream = logStream;
  const cancel = stream.subscribe(listener, options);

  stopPolling ??= startVisiblePoll(() => void stream.poll(), POLL_INTERVAL_MS);

  return () => {
    cancel();
    if (stream.size() === 0 && stopPolling) {
      stopPolling();
      stopPolling = null;
    }
  };
}
