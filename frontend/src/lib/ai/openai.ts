/*
 * AI 助手逻辑层（由 layouts/ai.tsx 拆分）：OpenAI 兼容端点的流式对话请求（SSE + 原生 function calling）。
 */

import { t } from "../../i18n";

import { NativeToolCall } from "./types";

// OpenAI 兼容格式 API 调用（支持流式 SSE + 原生 function calling）
// ------------------------------------------------------------------

/** OpenAI 视觉内容块（图片走 image_url，data URL） */
export interface OpenAiContentPart {
  type: "text" | "image_url";
  text?: string;
  image_url?: { url: string };
}

/** 请求体里的 tool_calls（把上一轮的原生调用回填进历史） */
export interface OpenAiToolCallPayload {
  id: string;
  type: "function";
  function: { name: string; arguments: string };
}

export interface OpenAiMessage {
  role: "system" | "user" | "assistant" | "tool";
  content: string | OpenAiContentPart[];
  /** assistant 消息携带的原生工具调用（历史回填用） */
  tool_calls?: OpenAiToolCallPayload[];
  /** role=tool 消息对应的 tool_call_id */
  tool_call_id?: string;
}

/** 单次流式请求的返回：文本 + 原生工具调用 */
export interface StreamResult {
  text: string;
  nativeToolCalls: NativeToolCall[];
  /** 回复因长度限制（max_tokens）被截断时为 true，供 UI 提示 */
  truncated?: boolean;
}

export async function streamOpenAiChat(
  baseUrl: string,
  apiKey: string,
  model: string,
  messages: OpenAiMessage[],
  temperature: number,
  onChunk: (text: string) => void,
  signal?: AbortSignal,
  tools?: unknown[],
): Promise<StreamResult> {
  const url = `${baseUrl.replace(/\/$/, "")}/chat/completions`;
  const decoder = new TextDecoder("utf-8");

  // 部分第三方 OpenAI 兼容端点不支持 tools 参数（HTTP 4xx 拒绝），自动降级重试一次
  for (let attempt = 0; attempt < 2; attempt++) {
    const sendTools = tools && attempt === 0 ? tools : undefined;
    const response = await fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${apiKey}`,
      },
      body: JSON.stringify({
        model,
        messages,
        temperature,
        stream: true,
        ...(sendTools ? { tools: sendTools } : {}),
      }),
      signal,
    });

    if (
      !response.ok &&
      sendTools &&
      [400, 404, 415, 422].includes(response.status)
    ) {
      await response.text().catch(() => "");
      continue; // 不带 tools 重试
    }

    if (!response.ok) {
      const errText = await response.text().catch(() => "");

      throw new Error(
        `HTTP ${response.status}: ${errText || response.statusText}`,
      );
    }

    const reader = response.body?.getReader();

    if (!reader) throw new Error(t("无法读取响应流"));

    let result: StreamResult = { text: "", nativeToolCalls: [] };
    let buffer = "";

    /** 处理一行 SSE data（解析增量并累计），返回是否已中断 */
    const processLine = (rawLine: string): void => {
      const trimmed = rawLine.trim();

      if (!trimmed || !trimmed.startsWith("data:")) return;
      const data = trimmed.slice(5).trim();

      if (data === "[DONE]") return;
      try {
        const parsed = JSON.parse(data);

        // 部分供应商在 200 流里内联返回错误对象
        if (parsed.error) {
          throw new Error(
            typeof parsed.error.message === "string"
              ? parsed.error.message
              : JSON.stringify(parsed.error),
          );
        }
        const delta = parsed.choices?.[0]?.delta;

        if (typeof delta?.content === "string" && delta.content) {
          result.text += delta.content;
          onChunk(delta.content);
        }
        // 原生工具调用增量：按 index 槽位累积 name/arguments
        if (Array.isArray(delta?.tool_calls)) {
          for (const d of delta.tool_calls) {
            const index = typeof d.index === "number" ? d.index : 0;
            const slot = result.nativeToolCalls[index] ?? {
              id: "",
              name: "",
              arguments: "",
            };

            if (d.id) slot.id = d.id;
            if (d.function?.name) slot.name += d.function.name;
            if (d.function?.arguments) slot.arguments += d.function.arguments;
            result.nativeToolCalls[index] = slot;
          }
        }
      } catch (ex) {
        // 我们自己抛的错误要继续向上抛，只吞掉 JSON 解析失败
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
          processLine(line);
        }
      }
      // 冲洗解码器并处理缓冲区中最后一段（无换行结尾）数据
      buffer += decoder.decode();
      if (buffer) processLine(buffer);
    } finally {
      reader.releaseLock();
    }

    result.nativeToolCalls = result.nativeToolCalls.filter((c) => c.name);

    return result;
  }

  // 理论上不可达（循环内必然 return 或 throw）
  return { text: "", nativeToolCalls: [] };
}
