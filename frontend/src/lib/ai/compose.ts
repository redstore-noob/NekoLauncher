/*
 * AI 助手逻辑层（由 layouts/ai.tsx 拆分）：请求组装：附件多模态转换、上下文裁剪与动态权限系统提示词。
 */

import { AiSettings, Attachment, ChatMessage } from "./types";
import {
  MAX_ATTACHMENT_TEXT_CHARS,
  TEXT_ATTACHMENT_EXTENSIONS,
} from "./agent-config";
import { OpenAiMessage } from "./openai";
import { stripToolBlocks } from "./tool-parse";
import { estimateTokens } from "./utils";

// ------------------------------------------------------------------
// 消息 → API 请求格式（含附件多模态转换与上下文裁剪）
// ------------------------------------------------------------------

/** data URL 中的 base64 文本解码为 UTF-8 字符串 */
export function decodeDataUrlText(dataUrl: string): string | null {
  try {
    const base64 = dataUrl.split(",")[1] ?? "";
    const bytes = Uint8Array.from(atob(base64), (ch) => ch.charCodeAt(0));

    return new TextDecoder("utf-8", { fatal: false }).decode(bytes);
  } catch {
    return null;
  }
}

/** 把附件转换为注入消息文本的说明（图片走独立多模态块，这里只处理文本与二进制说明） */
export function attachmentTextParts(attachments: Attachment[]): {
  text: string;
  images: string[];
} {
  const images: string[] = [];
  const chunks: string[] = [];

  for (const att of attachments) {
    if (att.isImage) {
      images.push(att.dataUrl);
      continue;
    }
    const ext = att.name.slice(att.name.lastIndexOf(".")).toLowerCase();

    if (TEXT_ATTACHMENT_EXTENSIONS.has(ext)) {
      const text = decodeDataUrlText(att.dataUrl);
      const truncated =
        text && text.length > MAX_ATTACHMENT_TEXT_CHARS
          ? `${text.slice(0, MAX_ATTACHMENT_TEXT_CHARS)}\n…(内容过长已截断)`
          : text;

      chunks.push(
        `[附件文件 ${att.name}]\n\`\`\`\n${truncated ?? "(读取失败)"}\n\`\`\``,
      );
    } else {
      chunks.push(
        `[附件文件 ${att.name}]（二进制文件，无法直接读取内容，大小 ${formatFileSizeStatic(att.size)}）`,
      );
    }
  }

  return { text: chunks.join("\n\n"), images };
}

export function formatFileSizeStatic(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;

  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/** 组装发给模型的完整消息列表（含会话摘要、当前用户消息、工具结果与附件） */
export function buildApiMessages(
  convo: ChatMessage[],
  contextWindow: number,
  summary?: string,
): OpenAiMessage[] {
  const drafts: OpenAiMessage[] = [];

  for (const msg of convo) {
    if (msg.role === "user") {
      let text = msg.content;
      let images: string[] = [];

      if (msg.attachments?.length) {
        const { text: attText, images: attImages } = attachmentTextParts(
          msg.attachments,
        );

        images = attImages;
        if (attText) text = text ? `${text}\n\n${attText}` : attText;
      }
      drafts.push(
        images.length === 0
          ? { role: "user", content: text }
          : {
              role: "user",
              content: [
                ...(text ? [{ type: "text" as const, text }] : []),
                ...images.map((url) => ({
                  type: "image_url" as const,
                  image_url: { url },
                })),
              ],
            },
      );
    } else if (msg.role === "assistant") {
      const nativeCalls = (msg.toolCalls ?? []).filter((tc) => tc.callId);

      if (nativeCalls.length > 0) {
        // 原生 function calling：以 tool_calls 数组回填，正文剥离协议标记
        drafts.push({
          role: "assistant",
          content: stripToolBlocks(msg.content),
          tool_calls: nativeCalls.map((tc) => ({
            id: tc.callId as string,
            type: "function" as const,
            function: { name: tc.name, arguments: tc.args },
          })),
        });
      } else {
        // 文本协议：保留原始文本（含 <tool_call> 块），模型需要看到自己发过的调用
        drafts.push({ role: "assistant", content: msg.content });
      }
    } else if (msg.role === "tool_result") {
      drafts.push(
        msg.toolCallId
          ? {
              role: "tool",
              content: msg.content,
              tool_call_id: msg.toolCallId,
            }
          : { role: "user", content: msg.content },
      );
    }
    // system / action 卡片消息不参与请求
  }

  // 孤儿 assistant tool_calls（中途停止未执行）→ 补合成 tool 结果，避免服务端 400
  const patched: OpenAiMessage[] = [];
  let pendingIds: string[] = [];

  const flushPending = () => {
    for (const id of pendingIds) {
      patched.push({
        role: "tool",
        content: "（工具调用未执行：会话在此中断）",
        tool_call_id: id,
      });
    }
    pendingIds = [];
  };

  for (const d of drafts) {
    if (d.role === "assistant" && d.tool_calls?.length) {
      flushPending();
      patched.push(d);
      pendingIds = d.tool_calls.map((tc) => tc.id);
      continue;
    }
    if (d.role === "tool" && d.tool_call_id) {
      patched.push(d);
      pendingIds = pendingIds.filter((id) => id !== d.tool_call_id);
      continue;
    }
    flushPending();
    patched.push(d);
  }
  flushPending();

  // 按估算 token 裁剪历史（保留 40% 余量给回复），至少保留最后 2 条
  const tokenOf = (m: OpenAiMessage): number => {
    let n = 4;

    if (typeof m.content === "string") n += estimateTokens(m.content);
    else {
      for (const part of m.content) {
        n += part.type === "image_url" ? 800 : estimateTokens(part.text ?? "");
      }
    }
    if (m.tool_calls) {
      for (const tc of m.tool_calls) {
        n += estimateTokens(tc.function.arguments) + 16;
      }
    }

    return n;
  };
  const summaryTokens = summary ? estimateTokens(summary) + 8 : 0;
  const budget =
    Math.max(1024, contextWindow * 0.6) -
    summaryTokens -
    tokenOf(patched[patched.length - 1] ?? { role: "user", content: "" });
  let used = 0;
  let firstKept = patched.length - 1;

  for (let i = patched.length - 1; i >= 0; i--) {
    used += tokenOf(patched[i]);

    if (used > budget && i < patched.length - 2) {
      firstKept = i + 1;
      break;
    }
    firstKept = i;
  }
  let kept = patched.slice(Math.max(0, firstKept));

  // 裁剪边界可能切开 tool 结果与其 assistant 调用：丢弃开头的孤儿 tool 消息
  while (kept[0]?.role === "tool") {
    kept = kept.slice(1);
  }

  // 历史被压缩过时，摘要作为首条消息回传，模型能"记住"被裁掉的内容
  if (summary) {
    kept = [{ role: "user", content: `[会话摘要]\n${summary}` }, ...kept];
  }

  return kept;
}

/** 组装带动态权限状态的系统提示词（权限开关随时可改，不能写死在存储的提示词里） */
export function buildSystemPrompt(settings: AiSettings): string {
  const permissionStatus = [
    `- 查看实例文件夹：${settings.allowFolderRead ? "已开启" : "已关闭（相关工具会被直接拒绝）"}`,
    `- 修改实例文件夹：${settings.allowFolderWrite ? "已开启" : "已关闭（相关工具会被直接拒绝）"}`,
    `- 修改操作确认模式：${settings.allowModify ? "直接执行" : "每次修改需用户批准"}`,
  ].join("\n");

  return `${settings.systemPrompt}\n\n## 当前权限状态（用户可在 AI 设置 → 实例操作权限中修改）\n${permissionStatus}`;
}
