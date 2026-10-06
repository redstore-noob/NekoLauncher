/*
 * AI 助手逻辑层（由 layouts/ai.tsx 拆分）：Anthropic Messages API：OpenAI 风格消息转换与流式请求。
 */

import { t } from "../../i18n";

import { OpenAiMessage, StreamResult } from "./openai";

// ------------------------------------------------------------------
// Anthropic Messages API 调用（支持流式）
// ------------------------------------------------------------------

/** Anthropic 内容块（含原生工具调用） */
export interface AnthropicContentBlock {
  type: "text" | "image" | "tool_use" | "tool_result";
  text?: string;
  source?: { type: "base64"; media_type: string; data: string };
  /** tool_use */
  id?: string;
  name?: string;
  input?: Record<string, unknown>;
  /** tool_result */
  tool_use_id?: string;
  is_error?: boolean;
  content?: string;
}

export interface AnthropicMessage {
  role: "user" | "assistant";
  content: string | AnthropicContentBlock[];
}

/** data URL → Anthropic 图片块 */
export function dataUrlToAnthropicImage(
  dataUrl: string,
): AnthropicContentBlock {
  const match = dataUrl.match(/^data:([^;,]+);base64,(.*)$/s);

  if (!match) return { type: "text", text: "[无法解析的图片附件]" };

  return {
    type: "image",
    source: { type: "base64", media_type: match[1], data: match[2] },
  };
}

/** OpenAI 风格消息 → Anthropic 消息（图片/工具块转换 + 角色交替合并） */
export function toAnthropicMessages(
  messages: OpenAiMessage[],
): AnthropicMessage[] {
  const merged: AnthropicMessage[] = [];
  /** 已发出 tool_use 但还没等到对应 tool_result 的调用 ID */
  let pendingToolUse: string[] = [];

  /** 把 tool_result 块并入上一条 user 消息（Anthropic 要求紧跟在 assistant 后） */
  const appendToolResults = (blocks: AnthropicContentBlock[]) => {
    const prev = merged[merged.length - 1];

    if (
      prev &&
      prev.role === "user" &&
      Array.isArray(prev.content) &&
      prev.content.some((b) => b.type === "tool_result")
    ) {
      prev.content.push(...blocks);
    } else {
      merged.push({ role: "user", content: blocks });
    }
  };

  for (const msg of messages) {
    if (msg.role === "system") continue; // system 单独传

    // 有 tool_use 还没回结果、下一条又不是 tool 结果 → 补合成结果，保证成对
    if (pendingToolUse.length > 0 && msg.role !== "tool") {
      appendToolResults(
        pendingToolUse.map((id) => ({
          type: "tool_result" as const,
          tool_use_id: id,
          content: "（工具调用未执行：会话在此中断）",
          is_error: true,
        })),
      );
      pendingToolUse = [];
    }

    // role=tool → user 消息里的 tool_result 块
    if (msg.role === "tool") {
      appendToolResults([
        {
          type: "tool_result",
          tool_use_id: msg.tool_call_id ?? "",
          content: typeof msg.content === "string" ? msg.content : "",
        },
      ]);
      pendingToolUse = pendingToolUse.filter((id) => id !== msg.tool_call_id);
      continue;
    }

    const role = msg.role === "assistant" ? "assistant" : "user";
    const blocks: AnthropicContentBlock[] = [];

    if (role === "assistant" && msg.tool_calls?.length) {
      if (typeof msg.content === "string" && msg.content) {
        blocks.push({ type: "text", text: msg.content });
      }
      for (const tc of msg.tool_calls) {
        let input: Record<string, unknown> = {};

        try {
          input = JSON.parse(tc.function.arguments || "{}");
        } catch {
          /* 参数损坏时按空对象处理 */
        }
        blocks.push({
          type: "tool_use",
          id: tc.id,
          name: tc.function.name,
          input,
        });
        pendingToolUse.push(tc.id);
      }
    } else if (typeof msg.content === "string") {
      if (msg.content) blocks.push({ type: "text", text: msg.content });
    } else {
      blocks.push(
        ...msg.content.map((part) =>
          part.type === "image_url" && part.image_url
            ? dataUrlToAnthropicImage(part.image_url.url)
            : { type: "text" as const, text: part.text ?? "" },
        ),
      );
    }

    if (blocks.length === 0) continue;

    const prev = merged[merged.length - 1];

    if (prev && prev.role === role) {
      // Anthropic 要求角色交替出现，连续同角色合并
      if (
        Array.isArray(prev.content) &&
        prev.content.some((b) => b.type === "tool_result")
      ) {
        prev.content.push(...blocks);
      } else {
        const flat: AnthropicContentBlock[] = [];

        if (typeof prev.content === "string") {
          if (prev.content) flat.push({ type: "text", text: prev.content });
        } else {
          flat.push(...prev.content);
        }
        flat.push(...blocks);
        prev.content = flat;
      }
    } else {
      merged.push({ role, content: blocks });
    }
  }

  // 收尾仍有孤儿 tool_use（如用户中断）→ 补合成结果
  if (pendingToolUse.length > 0) {
    appendToolResults(
      pendingToolUse.map((id) => ({
        type: "tool_result" as const,
        tool_use_id: id,
        content: "（工具调用未执行：会话在此中断）",
        is_error: true,
      })),
    );
  }

  return merged;
}

export async function streamAnthropicChat(
  baseUrl: string,
  apiKey: string,
  model: string,
  systemPrompt: string,
  messages: OpenAiMessage[],
  temperature: number,
  onChunk: (text: string) => void,
  signal?: AbortSignal,
  tools?: unknown[],
): Promise<StreamResult> {
  const url = `${baseUrl.replace(/\/$/, "")}/messages`;
  const response = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "x-api-key": apiKey,
      "anthropic-version": "2023-06-01",
      // 浏览器/WebView 直连 api.anthropic.com 必须带这个开关头，否则被 CORS 拦死
      // （Anthropic 官方文档要求的浏览器直连声明）。
      "anthropic-dangerous-direct-browser-access": "true",
    },
    body: JSON.stringify({
      model,
      system: systemPrompt,
      messages: toAnthropicMessages(messages),
      temperature,
      stream: true,
      max_tokens: 4096,
      ...(tools ? { tools } : {}),
    }),
    signal,
  });

  if (!response.ok) {
    const errText = await response.text().catch(() => "");

    throw new Error(
      `HTTP ${response.status}: ${errText || response.statusText}`,
    );
  }

  const reader = response.body?.getReader();

  if (!reader) throw new Error(t("无法读取响应流"));

  const decoder = new TextDecoder("utf-8");
  const result: StreamResult = { text: "", nativeToolCalls: [] };
  let buffer = "";

  /** 处理一行 SSE data，返回是否已中断 */
  const processLine = (rawLine: string): void => {
    const trimmed = rawLine.trim();

    if (!trimmed || !trimmed.startsWith("data:")) return;
    const data = trimmed.slice(5).trim();

    try {
      const parsed = JSON.parse(data);

      // error 事件：流中返回的错误对象
      if (parsed.type === "error") {
        throw new Error(
          parsed.error?.message
            ? String(parsed.error.message)
            : JSON.stringify(parsed),
        );
      }
      // content_block_start：工具调用块开始（携带 id/name）
      if (
        parsed.type === "content_block_start" &&
        parsed.content_block?.type === "tool_use"
      ) {
        result.nativeToolCalls.push({
          id: String(parsed.content_block.id ?? ""),
          name: String(parsed.content_block.name ?? ""),
          arguments: "",
        });
      }
      // content_block_delta：文本增量或工具入参 JSON 增量
      if (parsed.type === "content_block_delta") {
        if (parsed.delta?.type === "text_delta" && parsed.delta.text) {
          result.text += parsed.delta.text;
          onChunk(parsed.delta.text);
        } else if (
          parsed.delta?.type === "input_json_delta" &&
          parsed.delta.partial_json
        ) {
          const slot =
            result.nativeToolCalls[result.nativeToolCalls.length - 1];

          if (slot) slot.arguments += parsed.delta.partial_json;
        }
      }
      // message_delta：流结束事件——stop_reason=max_tokens 表示回复被长度截断，
      // 此前被静默吞掉，长回答会无声断尾
      if (
        parsed.type === "message_delta" &&
        parsed.delta?.stop_reason === "max_tokens"
      ) {
        result.truncated = true;
      }
    } catch (ex) {
      if (ex instanceof Error && !(ex instanceof SyntaxError)) throw ex;
    }
  };

  try {
    while (true) {
      const { done, value } = await reader.read();

      if (done) break;
      buffer += decoder.decode(value, { stream: true });

      const lines = buffer.split("\n");

      buffer = lines.pop() ?? "";

      for (const line of lines) {
        // Anthropic SSE 格式：event: xxx \n data: xxx
        if (line.trim().startsWith("event:")) continue;
        processLine(line);
      }
    }
    // 冲洗解码器并处理缓冲区中最后一段（无换行结尾）数据
    buffer += decoder.decode();
    if (buffer && !buffer.trim().startsWith("event:")) processLine(buffer);
  } finally {
    reader.releaseLock();
  }

  return result;
}
