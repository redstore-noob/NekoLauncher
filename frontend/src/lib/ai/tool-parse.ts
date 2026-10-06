/*
 * AI 助手逻辑层（由 layouts/ai.tsx 拆分）：工具调用解析：tool_call 文本块、泄漏的原生调用剥离与结果截断。
 */

import { MAX_TOOL_RESULT_CHARS } from "./agent-config";

// ------------------------------------------------------------------
// Agent 工具：<tool_call> 解析与真实执行
// ------------------------------------------------------------------

export interface ParsedToolCall {
  name: string;
  /** 原始 JSON 文本（用于 UI 展示与回写历史） */
  rawArgs: string;
  args: Record<string, unknown>;
}

export interface ParsedResponse {
  /** 去掉工具调用块后的正文 */
  cleanText: string;
  calls: ParsedToolCall[];
  /** 解析失败（JSON 损坏）的块数量 */
  malformed: number;
}

export function tryParseToolCall(body: string): ParsedToolCall | null {
  try {
    const obj = JSON.parse(body.trim()) as {
      name?: unknown;
      args?: unknown;
    };

    if (typeof obj?.name === "string" && obj.name) {
      return {
        name: obj.name,
        rawArgs: body.trim(),
        args:
          obj.args && typeof obj.args === "object" && !Array.isArray(obj.args)
            ? (obj.args as Record<string, unknown>)
            : {},
      };
    }
  } catch {
    /* 解析失败按 malformed 处理 */
  }

  return null;
}

/**
 * 统一解析工具参数：兼容两种形态——
 * 原生 function calling 的纯参数 JSON（{"path":"…"}）
 * 与文本协议的 {"name":"…","args":{…}} 包裹格式
 */
export function toolArgsOf(rawArgs: string): Record<string, unknown> {
  try {
    const parsed = JSON.parse(rawArgs) as unknown;

    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      const obj = parsed as { name?: unknown; args?: unknown };

      if (
        typeof obj.name === "string" &&
        obj.args &&
        typeof obj.args === "object" &&
        !Array.isArray(obj.args)
      ) {
        return obj.args as Record<string, unknown>;
      }

      return parsed as Record<string, unknown>;
    }
  } catch {
    /* 参数损坏按空参数处理 */
  }

  return {};
}

/** 解析模型回复中的 <tool_call> 块（含流式截断导致未闭合的块） */
export function parseToolCalls(text: string): ParsedResponse {
  const calls: ParsedToolCall[] = [];
  let malformed = 0;
  const accept = (body: string) => {
    const parsed = tryParseToolCall(body);

    if (parsed) calls.push(parsed);
    else malformed++;

    return "";
  };
  // 先抽取泄漏成纯文本的 DeepSeek 原生 DSML 标记（未声明原生 tools 时的典型症状）
  const dsml = extractLeakedNativeCalls(text);

  calls.push(...dsml.calls);
  const cleanText = dsml.text
    .replace(/<tool_call>([\s\S]*?)<\/tool_call>/g, (_m, body: string) =>
      accept(body),
    )
    .replace(/<tool_call>([\s\S]*)$/g, (_m, body: string) => accept(body))
    .replace(/<action_request>[\s\S]*?(?:<\/action_request>|$)/g, "")
    .replace(/\n{3,}/g, "\n\n")
    .trim();

  return { cleanText, calls, malformed };
}

/**
 * 抽取泄漏成纯文本的原生工具调用标记。
 * DeepSeek 等模型在请求未携带原生 tools 参数时，会把内部调用格式
 * （DSML / tool▁call 系列 special token 的文本形态）直接写进 content，
 * 这里把它们还原成结构化调用并从正文中剥离。
 */
export function extractLeakedNativeCalls(text: string): {
  text: string;
  calls: ParsedToolCall[];
} {
  const calls: ParsedToolCall[] = [];

  // 形态一（DSML，DeepSeek V3.2 等）：
  // <｜DSML｜invoke name="list_directory"><｜DSML｜parameter name="path" string="true">…</｜DSML｜parameter></｜DSML｜invoke>
  const text1 = text.replace(
    /<｜DSML｜invoke name="([^"]+)">([\s\S]*?)<\/｜DSML｜invoke>/g,
    (_m, name: string, body: string) => {
      const args: Record<string, unknown> = {};
      const paramRe =
        /<｜DSML｜parameter name="([^"]+)"([^>]*)>([\s\S]*?)<\/｜DSML｜parameter>/g;
      let pm: RegExpExecArray | null;

      while ((pm = paramRe.exec(body))) {
        const key = pm[1];
        const attrs = pm[2];
        const raw = pm[3];

        if (/string="true"/.test(attrs)) args[key] = raw;
        else {
          try {
            args[key] = JSON.parse(raw);
          } catch {
            args[key] = raw;
          }
        }
      }
      calls.push({ name, rawArgs: JSON.stringify({ name, args }), args });

      return "";
    },
  );

  // 形态二（classic，DeepSeek V3 等）：
  // <｜tool▁call▁begin｜>function<｜tool▁sep｜>NAME\n```json\n{…}\n```<｜tool▁call▁end｜>
  const text2 = text1.replace(
    /(?:<｜tool▁call▁begin｜>[^<]*)?<｜tool▁sep｜>\s*([\w.$-]+)\s*```(?:json)?\s*([\s\S]*?)```[\s\S]*?<｜tool▁call▁end｜>/g,
    (_m, name: string, argsText: string) => {
      try {
        const args = JSON.parse(argsText.trim()) as Record<string, unknown>;

        calls.push({
          name,
          rawArgs: JSON.stringify({ name, args }),
          args:
            args && typeof args === "object" && !Array.isArray(args)
              ? args
              : {},
        });
      } catch {
        /* 参数损坏，忽略该次调用 */
      }

      return "";
    },
  );

  // 剩余的包裹标签（calls/calls▁end 等开闭形式）与孤立标记直接剥掉
  const text3 = text2
    .replace(/<\/?｜DSML｜[^>]*>/g, "")
    .replace(/<\/?｜tool▁[a-z▁]*[｜>]>?/g, "");

  return { text: text3, calls };
}

/** 渲染时剥离工具调用块，避免 ReactMarkdown 吞掉标签导致内容"消失" */
export function stripToolBlocks(text: string): string {
  return parseToolCalls(text).cleanText;
}

/** 截断过长的工具结果，避免撑爆上下文 */
export function truncateForModel(
  text: string,
  max = MAX_TOOL_RESULT_CHARS,
): string {
  if (!text) return "";

  return text.length > max ? `${text.slice(0, max)}\n…(已截断)` : text;
}

/** 日志/文件类结果用中段截断：开头和结尾各留一段（异常常在头尾，中间多为重复堆栈） */
export function truncateMiddle(
  text: string,
  max = MAX_TOOL_RESULT_CHARS,
): string {
  if (text.length <= max) return text;

  const head = Math.floor(max * 0.4);
  const tail = max - head;

  return `${text.slice(0, head)}\n…(中间省略 ${text.length - max} 字符)…\n${text.slice(-tail)}`;
}
