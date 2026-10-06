/*
 * AI 助手逻辑层（由 layouts/ai.tsx 拆分）：AI 助手共享类型：提供商 / 消息 / 会话 / 设置 / 工具调用。
 */

export interface AiProvider {
  id: string;
  name: string;
  baseUrl: string;
  defaultModel: string;
  builtin?: boolean;
  /** 默认 API 格式 */
  apiFormat: "openai" | "anthropic";
  /** Anthropic 格式专用 base URL（部分供应商的 Anthropic 兼容端点与 OpenAI 端点不同源） */
  anthropicBaseUrl?: string;
  /** 本地推理引擎（llama.cpp / Ollama / LM Studio）：免 API Key、可探测模型列表 */
  local?: boolean;
}

/** 附件 */
export interface Attachment {
  id: string;
  name: string;
  size: number;
  type: string;
  /** data URL，用于图片预览和发送 */
  dataUrl: string;
  /** 是否为图片 */
  isImage: boolean;
}

export interface ChatMessage {
  id: string;
  role: "user" | "assistant" | "system" | "action" | "tool_result";
  content: string;
  actionStatus?: "pending" | "approved" | "rejected" | "executed";
  actionLabel?: string;
  timestamp: number;
  /** 附件列表 */
  attachments?: Attachment[];
  /** 工具调用列表 */
  toolCalls?: ToolCall[];
  /** role=tool_result 时对应的原生工具调用 ID（文本协议回填时为空） */
  toolCallId?: string;
}

/** 工具调用 */
export interface ToolCall {
  id: string;
  name: string;
  args: string;
  result?: string;
  status: "pending" | "running" | "success" | "error";
  /** 原生 function calling 返回的调用 ID（OpenAI tool_call_id / Anthropic tool_use id） */
  callId?: string;
}

/** 原生工具调用（流式增量累积后的结果） */
export interface NativeToolCall {
  id: string;
  name: string;
  arguments: string;
}

export interface ChatSession {
  id: string;
  title: string;
  messages: ChatMessage[];
  createdAt: number;
  updatedAt: number;
  /** 置顶的会话排在列表最前 */
  pinned?: boolean;
  /**
   * 自动压缩生成的历史摘要：压缩发生时，被摘要覆盖的旧消息会从 messages
   * 中移除，摘要本身存在这里并在每次请求时作为首条消息回传给模型
   */
  summary?: string;
}

export interface AiSettings {
  providerId: string;
  apiKey: string;
  model: string;
  customBaseUrl: string;
  allowModify: boolean;
  /** 允许查看实例文件夹（模组/资源包/存档列表、游戏设置、打开文件夹） */
  allowFolderRead: boolean;
  /** 允许修改实例文件夹（安装模组、启用/禁用模组等写入操作） */
  allowFolderWrite: boolean;
  systemPrompt: string;
  temperature: number;
  /** API 格式：openai 兼容格式 / anthropic 格式 */
  apiFormat: "openai" | "anthropic";
  /** 上下文窗口大小(token) */
  contextWindow: number;
  /** 是否显示工具调用详情 */
  showToolCalls: boolean;
}
