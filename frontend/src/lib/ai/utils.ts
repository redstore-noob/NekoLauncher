/*
 * AI 助手逻辑层（由 layouts/ai.tsx 拆分）：存储读写、时间/标题格式化、token 估算与本地引擎模型探测。
 */
import { t } from "../../i18n";

import { ChatMessage } from "./types";

// ------------------------------------------------------------------

export function uid(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

export function loadFromStorage<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);

    if (!raw) return fallback;

    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

export function saveToStorage(key: string, value: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* 存储满或被禁用时静默失败 */
  }
}

/**
 * 探测 OpenAI 兼容端点的模型列表（GET /models）。
 * llama.cpp / Ollama / LM Studio / 各云厂商的兼容端点均支持；
 * 连接失败、非 2xx、返回结构异常都按"探测失败"处理，由调用方展示离线指引。
 */
export async function probeOpenAiModels(baseUrl: string): Promise<string[]> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 4000);

  try {
    const response = await fetch(`${baseUrl.replace(/\/$/, "")}/models`, {
      signal: controller.signal,
    });

    if (!response.ok) throw new Error(`HTTP ${response.status}`);

    const json = (await response.json()) as {
      data?: Array<{ id?: unknown }>;
    };
    const models = Array.isArray(json?.data)
      ? json.data.map((m) => String(m?.id ?? "")).filter(Boolean)
      : [];

    return models.sort((a, b) => a.localeCompare(b));
  } finally {
    clearTimeout(timer);
  }
}

export function formatTime(ts: number): string {
  const d = new Date(ts);
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();

  if (sameDay) {
    return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  }

  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

export function deriveTitle(content: string): string {
  const trimmed = content.trim().replace(/\s+/g, " ");

  return trimmed.length > 20
    ? `${trimmed.slice(0, 20)}…`
    : trimmed || t("新对话");
}

/** 估算文本的 token 数（粗略：中文1.5/字，英文0.4/字符） */
export function estimateTokens(text: string): number {
  if (!text) return 0;
  let cn = 0,
    other = 0;

  for (const ch of text) {
    if (/[\u4e00-\u9fa5]/.test(ch)) cn++;
    else other++;
  }

  return Math.round(cn * 1.5 + other * 0.4);
}

/** 估算会话 token 使用量 */
export function estimateSessionTokens(
  messages: ChatMessage[],
  systemPrompt: string,
): number {
  let total = estimateTokens(systemPrompt) + 4;

  for (const msg of messages) {
    total += estimateTokens(msg.content) + 4;
    if (msg.toolCalls) {
      for (const tc of msg.toolCalls) {
        total += estimateTokens(tc.name) + estimateTokens(tc.args) + 8;
        if (tc.result) total += estimateTokens(tc.result) + 4;
      }
    }
    if (msg.attachments) total += msg.attachments.length * 100;
  }

  return total;
}
