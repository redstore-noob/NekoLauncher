/*
 * AI 助手页面：轻量 Agent 聊天界面
 *
 * 布局参考 DeepSeek / ChatGPT：
 * - 左侧：聊天历史列表 + 新建对话 + 左下角 AI 设置
 * - 右侧：消息流 + 输入框
 *
 * Agent 工具循环：
 * - 模型按系统提示词输出 <tool_call>{JSON}</tool_call> 文本块
 * - 前端解析后经 Wails bindings 真实执行，结果以 <tool_result> 回填再请求
 * - 最多 MAX_TOOL_ROUNDS 轮，防止无限循环
 *
 * 权限模型（三个独立开关，见 AI 设置 → 实例操作权限）：
 * - allowFolderRead：关闭后查看实例文件夹类工具（列表/设置/打开文件夹）直接拒绝
 * - allowFolderWrite：关闭后修改实例文件夹类工具（安装/启停模组）直接拒绝
 * - allowModify：开启时修改类工具直接执行；关闭时先挂起为待批准卡片，批准后才执行
 *
 * 数据持久化：聊天记录与设置均存 localStorage（会话写入防抖），无需后端配合。
 * 纯逻辑（类型 / API 客户端 / 工具解析与执行 / 消息组装）在 src/lib/ai/ 下。
 */
import React, {
  memo,
  useState,
  useRef,
  useEffect,
  useMemo,
  useCallback,
} from "react";
import {
  Add20Regular as NewChatIcon,
  ArrowDown20Regular as ArrowDownIcon,
  Attach20Regular as AttachIcon,
  Send20Regular as SendIcon,
  ChevronDown20Regular as ChevronDownIcon,
  ChevronRight20Regular as ChevronRightIcon,
  Database20Regular as DatabaseIcon,
  WindowConsole20Regular as TerminalIcon,
  Checkmark20Regular as ApproveIcon,
  Chat20Regular as ChatIcon,
  Delete20Regular as DeleteIcon,
  Dismiss20Regular as RejectIcon,
  Edit20Regular as RenameIcon,
  Pin20Regular as PinIcon,
  PinOff20Regular as UnpinIcon,
  Eye20Regular as ReadOnlyIcon,
  Person20Regular as UserIcon,
  Settings20Regular as SettingsIcon,
  Sparkle20Regular as SparkleIcon,
  Warning20Regular as WarningIcon,
} from "@fluentui/react-icons";
import { motion, AnimatePresence } from "framer-motion";
import { Button, Textarea, Tooltip } from "@heroui/react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { listItemVariants } from "../lib/motion";
import { t } from "../i18n";
import { consumePendingDetail } from "../lib/navigation";
import AiSettingsDialog from "../components/ai/AiSettingsDialog";
import NekoAgentIcon from "../components/neko-agent-icon";
import {
  AiSettings,
  Attachment,
  ChatMessage,
  ChatSession,
  NativeToolCall,
  ToolCall,
} from "../lib/ai/types";
import {
  BUILTIN_PROVIDERS,
  DEFAULT_SETTINGS,
  STORAGE_KEYS,
  AI_SECRET_STORAGE_KEY,
  resolveBaseUrl,
} from "../lib/ai/providers";
import { HasSecret, StoreSecret } from "../../wailsjs/go/bindings/SystemAPI";
import {
  deriveTitle,
  estimateSessionTokens,
  estimateTokens,
  formatTime,
  loadFromStorage,
  probeOpenAiModels,
  saveToStorage,
  uid,
} from "../lib/ai/utils";
import { buildApiMessages, buildSystemPrompt } from "../lib/ai/compose";
import { streamAnthropicChat } from "../lib/ai/anthropic";
import { ANTHROPIC_TOOLS, OPENAI_TOOLS } from "../lib/ai/tools";
import { streamOpenAiChat } from "../lib/ai/openai";
import {
  CONFIRM_TOOLS,
  FOLDER_READ_TOOLS,
  FOLDER_WRITE_TOOLS,
  KEEP_RECENT_MESSAGES,
  MAX_TOOL_ROUNDS,
  SUMMARY_SYSTEM_PROMPT,
} from "../lib/ai/agent-config";
import { classifyApiError, formatErrorMessage } from "../lib/ai/errors";
import {
  parseToolCalls,
  stripToolBlocks,
  toolArgsOf,
  truncateMiddle,
} from "../lib/ai/tool-parse";
import { executeTool, resolveToolContext } from "../lib/ai/tool-exec";

/**
 * 消息正文的 Markdown 渲染（memo）。
 * 流式刷新时历史消息内容不变，跳过 remark→react 的整树重解析——
 * 这是长对话流式输出掉帧的大头；流式气泡本身每拍内容都在变，不走这里。
 */
const MarkdownBody = memo(function MarkdownBody({
  source,
}: {
  source: string;
}) {
  return <ReactMarkdown remarkPlugins={[remarkGfm]}>{source}</ReactMarkdown>;
});

const AiPage: React.FC = () => {
  const [sessions, setSessions] = useState<ChatSession[]>([]);
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [settings, setSettings] = useState<AiSettings>(DEFAULT_SETTINGS);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [input, setInput] = useState("");
  const [isSending, setIsSending] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  /** 正在流式生成的消息内容（用于实时显示） */
  const [streamingContent, setStreamingContent] = useState("");
  /** 当前是否正在流式输出 */
  const [isStreaming, setIsStreaming] = useState(false);
  // 流式批渲染：token 先到缓冲区，80ms 合并一次 setState。逐 token setState
  // 会让整棵消息树每字一刷，快模型/长回复下直接掉帧；12.5 次/秒肉眼仍是连贯流。
  const streamBufRef = useRef("");
  const streamFlushTimerRef = useRef<number | null>(null);

  const appendStreamChunk = useCallback((chunk: string) => {
    streamBufRef.current += chunk;
    if (streamFlushTimerRef.current !== null) return;
    streamFlushTimerRef.current = window.setTimeout(() => {
      streamFlushTimerRef.current = null;
      if (!streamBufRef.current) return;
      const pending = streamBufRef.current;

      streamBufRef.current = "";
      setStreamingContent((prev) => prev + pending);
    }, 80);
  }, []);

  const resetStreaming = useCallback(() => {
    streamBufRef.current = "";
    if (streamFlushTimerRef.current !== null) {
      window.clearTimeout(streamFlushTimerRef.current);
      streamFlushTimerRef.current = null;
    }
    setStreamingContent("");
  }, []);
  /** 待发送的附件列表 */
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>(
    [],
  );
  /** 正在重命名的会话 ID 与草稿 */
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameDraft, setRenameDraft] = useState("");
  /** 正在编辑重发的用户消息 ID 与草稿 */
  const [editingMessageId, setEditingMessageId] = useState<string | null>(null);
  const [editDraft, setEditDraft] = useState("");
  const [expandedToolCalls, setExpandedToolCalls] = useState<Set<string>>(
    new Set(),
  );
  /** 用户是否停留在消息列表底部附近（贴底才自动跟随，上翻阅读时不打扰） */
  const [showScrollToBottom, setShowScrollToBottom] = useState(false);

  /** 消息列表滚动容器：滚动只作用于它自己，绝不触碰祖先容器 */
  const messagesScrollRef = useRef<HTMLDivElement>(null);
  const isNearBottomRef = useRef(true);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const abortControllerRef = useRef<AbortController | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  /** 等待用户批准的工具调用（toolCallId → resolver），批准/拒绝/总是允许按钮 resolve */
  const approvalWaitersRef = useRef<
    Map<string, (approved: boolean | "always") => void>
  >(new Map());
  /** 本应用运行期内被用户「总是允许」的修改类工具（重启后重置，避免永久授权） */
  const alwaysAllowedToolsRef = useRef<Set<string>>(new Set());
  /** alwaysAllowedToolsRef 的镜像状态，供设置弹窗展示与撤销 */
  const [alwaysAllowedList, setAlwaysAllowedList] = useState<string[]>([]);
  /** 正在压缩历史摘要（给打字指示器换文案用） */
  const [isCompacting, setIsCompacting] = useState(false);
  /** 到达工具轮数上限而暂停的会话：显示「继续执行」按钮 */
  const [continueTarget, setContinueTarget] = useState<string | null>(null);
  /** Agent 循环跨多次 await 运行，读设置走 ref，避免闭包里的旧值 */
  const settingsRef = useRef(settings);
  /** API Key 后端恢复是否完成：完成前不动后端存储，避免挂载期用默认空键覆盖存量 */
  const aiKeyHydratedRef = useRef(false);
  /** 后端已确认接住当前 API Key：为真时 localStorage 副本剥离明文键 */
  const [aiKeyStored, setAiKeyStored] = useState(false);
  /** 历史压缩请求的中止器：压缩没有可中止通道的话，供应商挂起时 isSending
   * 会卡死且停止按钮无效（abortControllerRef 此时是 null） */
  const compactionAbortRef = useRef<AbortController | null>(null);

  // 派生数据（必须在 useEffect 之前声明）
  const activeSession = useMemo(
    () => sessions.find((s) => s.id === activeSessionId) ?? null,
    [sessions, activeSessionId],
  );

  const currentProvider = useMemo(
    () =>
      BUILTIN_PROVIDERS.find((p) => p.id === settings.providerId) ??
      BUILTIN_PROVIDERS[0],
    [settings.providerId],
  );

  const effectiveBaseUrl = resolveBaseUrl(
    currentProvider,
    settings.customBaseUrl,
  );

  // ------------------------------------------------------------------
  // 本地引擎探测：设置弹窗打开且选中本地提供商时，自动 GET /models 列出可用模型
  // ------------------------------------------------------------------

  type LocalProbeState =
    | { status: "idle" }
    | { status: "probing" }
    | { status: "online"; models: string[] }
    | { status: "offline" };

  const [localProbe, setLocalProbe] = useState<LocalProbeState>({
    status: "idle",
  });
  /** 递增序号防止地址快速变更时旧探测结果覆盖新结果 */
  const localProbeSeqRef = useRef(0);

  const runLocalProbe = useCallback(async () => {
    const provider = BUILTIN_PROVIDERS.find(
      (p) => p.id === settings.providerId,
    );

    if (!provider?.local) return;

    const baseUrl = settings.customBaseUrl.trim() || provider.baseUrl;
    const seq = ++localProbeSeqRef.current;

    setLocalProbe({ status: "probing" });
    try {
      const models = await probeOpenAiModels(baseUrl);

      if (seq === localProbeSeqRef.current)
        setLocalProbe({ status: "online", models });
    } catch {
      if (seq === localProbeSeqRef.current)
        setLocalProbe({ status: "offline" });
    }
  }, [settings.providerId, settings.customBaseUrl]);

  useEffect(() => {
    if (!settingsOpen || !currentProvider.local) {
      setLocalProbe({ status: "idle" });

      return;
    }
    // 地址输入每敲一个字符都会触发 effect，防抖 400ms 再探测
    const timer = setTimeout(() => runLocalProbe(), 400);

    return () => clearTimeout(timer);
  }, [settingsOpen, currentProvider, runLocalProbe]);

  // 上下文使用统计
  const contextUsage = useMemo(() => {
    const msgs = (activeSession?.messages ?? []).filter(
      (m) => m.role !== "tool_result",
    );
    const summaryTokens = activeSession?.summary
      ? estimateTokens(activeSession.summary)
      : 0;
    const used =
      estimateSessionTokens(msgs, settings.systemPrompt) + summaryTokens;
    const pct = Math.min(100, (used / settings.contextWindow) * 100);

    return {
      usedTokens: used,
      totalTokens: settings.contextWindow,
      percent: pct,
      messageCount: msgs.length,
      isNear: pct > 80,
      isOver: pct > 100,
    };
  }, [activeSession, settings.systemPrompt, settings.contextWindow]);

  useEffect(() => {
    // 其它页面（实例右键「让 AI 分析」、帮助页「AI 诊断」）带来的预填问题
    const preset = consumePendingDetail("ai");

    if (preset) setInput(preset);
    const loadedSessions = loadFromStorage<ChatSession[]>(
      STORAGE_KEYS.sessions,
      [],
    );
    const loadedSettings = loadFromStorage<AiSettings>(
      STORAGE_KEYS.settings,
      DEFAULT_SETTINGS,
    );
    const loadedActive = loadFromStorage<string | null>(
      STORAGE_KEYS.activeSession,
      null,
    );

    // 存储损坏时可能是任意 JSON（如 null/对象），做类型防御
    const safeSessions = Array.isArray(loadedSessions)
      ? loadedSessions.filter(
          (s): s is ChatSession =>
            !!s && typeof s.id === "string" && Array.isArray(s.messages),
        )
      : [];

    setSessions(safeSessions);
    setSettings({ ...DEFAULT_SETTINGS, ...loadedSettings });

    if (loadedActive && safeSessions.some((s) => s.id === loadedActive)) {
      setActiveSessionId(loadedActive);
    } else if (safeSessions.length > 0) {
      setActiveSessionId(safeSessions[0].id);
    }
  }, []);

  // API Key 状态：key 明文不再回读进 JS（请求经 /ai-proxy 由 Go 侧注入），
  // 这里只查"已保存"标记。旧版本键明文存在 localStorage 里
  // （loadedSettings.apiKey 已在上面合入状态），在这里顺势迁入后端。
  useEffect(() => {
    let alive = true;

    void (async () => {
      let stored = false;

      try {
        stored = await HasSecret(AI_SECRET_STORAGE_KEY);
      } catch {
        /* 后端不可用：保持未保存状态 */
      }
      if (!alive) return;
      aiKeyHydratedRef.current = true;
      setAiKeyStored(stored);
      const current = settingsRef.current;

      if (current.apiKey) {
        // localStorage 里的旧明文键（或本次刚输入的键）→ 迁入/刷新后端存储
        StoreSecret(AI_SECRET_STORAGE_KEY, current.apiKey)
          .then((ok) => {
            if (alive) setAiKeyStored(!!ok);
          })
          .catch(() => {
            if (alive) setAiKeyStored(false);
          });
      }
    })();

    return () => {
      alive = false;
    };
  }, []);

  // settingsRef 与 settings 保持同步
  useEffect(() => {
    settingsRef.current = settings;
  }, [settings]);

  // 卸载时中断在途流式请求并放行所有挂起的审批 Promise：
  // 否则切走页面后 Agent 循环还在继续消耗网络与 CPU，且结果反正会随组件销毁丢失
  useEffect(() => {
    const waiters = approvalWaitersRef.current;

    return () => {
      abortControllerRef.current?.abort();
      compactionAbortRef.current?.abort();
      if (streamFlushTimerRef.current !== null) {
        window.clearTimeout(streamFlushTimerRef.current);
        streamFlushTimerRef.current = null;
      }
      for (const waiter of waiters.values()) waiter(false);
      waiters.clear();
    };
  }, []);

  // 会话里可能积累大段对话与工具结果，全量序列化同步写 localStorage
  // 会在流式期间高频触发掉帧 —— 防抖 600ms 合并写入，卸载时兜底落盘一次
  const sessionsRef = useRef(sessions);

  useEffect(() => {
    sessionsRef.current = sessions;
    const timer = setTimeout(
      () => saveToStorage(STORAGE_KEYS.sessions, sessions),
      600,
    );

    return () => clearTimeout(timer);
  }, [sessions]);

  useEffect(
    () => () => saveToStorage(STORAGE_KEYS.sessions, sessionsRef.current),
    [],
  );

  // API Key 变更 → 写后端加密存储。恢复完成前不动存储（防挂载期默认空键
  // 把存量覆盖掉）；清空键时传空串，用户主动清除要能持久化。
  useEffect(() => {
    if (!aiKeyHydratedRef.current) return;
    let alive = true;

    StoreSecret(AI_SECRET_STORAGE_KEY, settings.apiKey)
      .then((ok) => {
        if (alive) setAiKeyStored(!!ok);
      })
      .catch(() => {
        if (alive) setAiKeyStored(false);
      });

    return () => {
      alive = false;
    };
  }, [settings.apiKey]);

  useEffect(() => {
    // 后端确认接住明文键后，localStorage 副本就剥离它（不再明文落盘）；
    // 后端不可用时保留明文键——宁明文不丢数据。
    saveToStorage(
      STORAGE_KEYS.settings,
      aiKeyStored ? { ...settings, apiKey: "" } : settings,
    );
  }, [settings, aiKeyStored]);

  useEffect(() => {
    saveToStorage(STORAGE_KEYS.activeSession, activeSessionId);
  }, [activeSessionId]);

  const createSession = useCallback(() => {
    const newSession: ChatSession = {
      id: uid(),
      title: t("新对话"),
      messages: [],
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };

    setSessions((prev) => [newSession, ...prev]);
    setActiveSessionId(newSession.id);
  }, []);

  const deleteSession = useCallback(
    (id: string) => {
      // 直接基于当前值计算，避免把 setActiveSessionId 混进 setState updater
      const next = sessions.filter((s) => s.id !== id);

      setSessions(next);
      if (activeSessionId === id) {
        setActiveSessionId(next.length > 0 ? next[0].id : null);
      }
    },
    [sessions, activeSessionId],
  );

  const renameSession = useCallback((id: string, title: string) => {
    const trimmed = title.trim();

    if (!trimmed) return;
    setSessions((prev) =>
      prev.map((s) => (s.id === id ? { ...s, title: trimmed } : s)),
    );
  }, []);

  const togglePinSession = useCallback((id: string) => {
    setSessions((prev) =>
      prev.map((s) => (s.id === id ? { ...s, pinned: !s.pinned } : s)),
    );
  }, []);

  /** 展示顺序：置顶优先，其余按最近更新时间 */
  const sortedSessions = useMemo(
    () =>
      [...sessions].sort(
        (a, b) =>
          Number(b.pinned ?? false) - Number(a.pinned ?? false) ||
          b.updatedAt - a.updatedAt,
      ),
    [sessions],
  );

  const updateSessionMessages = useCallback(
    (sessionId: string, updater: (msgs: ChatMessage[]) => ChatMessage[]) => {
      setSessions((prev) =>
        prev.map((s) => {
          if (s.id !== sessionId) return s;
          const messages = updater(s.messages);

          return { ...s, messages, updatedAt: Date.now() };
        }),
      );
    },
    [],
  );

  const handleFileSelect = useCallback((files: FileList | null) => {
    if (!files || files.length === 0) return;

    Array.from(files).forEach((file) => {
      // 限制单个文件 10MB
      if (file.size > 10 * 1024 * 1024) {
        console.warn(t("文件 {name} 超过 10MB，已跳过", { name: file.name }));

        return;
      }

      const reader = new FileReader();

      reader.onload = () => {
        const attachment: Attachment = {
          id: uid(),
          name: file.name,
          size: file.size,
          type: file.type,
          dataUrl: reader.result as string,
          isImage: file.type.startsWith("image/"),
        };

        setPendingAttachments((prev) => [...prev, attachment]);
      };
      reader.readAsDataURL(file);
    });
  }, []);

  const removeAttachment = useCallback((id: string) => {
    setPendingAttachments((prev) => prev.filter((a) => a.id !== id));
  }, []);

  const toggleToolCall = useCallback((id: string) => {
    setExpandedToolCalls((prev) => {
      const next = new Set(prev);

      if (next.has(id)) next.delete(id);
      else next.add(id);

      return next;
    });
  }, []);

  // 格式化文件大小
  const formatFileSize = (bytes: number): string => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;

    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  // ------------------------------------------------------------------
  // Agent 循环：单次模型调用
  // ------------------------------------------------------------------

  /** 解析当前设置对应的 API 端点（Agent 循环与摘要压缩共用） */
  const activeEndpoint = useCallback(() => {
    const cur = settingsRef.current;
    const provider = BUILTIN_PROVIDERS.find((p) => p.id === cur.providerId);
    const openaiBaseUrl = resolveBaseUrl(provider, cur.customBaseUrl);
    // Anthropic 格式优先走供应商专用的兼容端点（如 DeepSeek 的 /anthropic）
    const baseUrl =
      cur.apiFormat === "anthropic" && provider?.anthropicBaseUrl
        ? provider.anthropicBaseUrl
        : openaiBaseUrl;

    return { cur, baseUrl, provider };
  }, []);

  /** 发起一次流式请求。返回是否被用户中断；网络/HTTP 错误向上抛出。 */
  const callModelOnce = useCallback(
    async (
      convo: ChatMessage[],
      onChunk: (chunk: string) => void,
      summary?: string,
    ): Promise<{
      text: string;
      nativeToolCalls: NativeToolCall[];
      aborted: boolean;
      truncated?: boolean;
    }> => {
      const { cur, baseUrl } = activeEndpoint();

      const controller = new AbortController();

      abortControllerRef.current = controller;
      setIsStreaming(true);
      let full = "";

      try {
        const messages = buildApiMessages(convo, cur.contextWindow, summary);
        // 系统提示词动态拼上当前权限状态，模型能提前知道哪些工具可用
        const systemPrompt = buildSystemPrompt(cur);
        const onDelta = (chunk: string) => {
          full += chunk;
          onChunk(chunk);
        };

        if (cur.apiFormat === "anthropic") {
          const result = await streamAnthropicChat(
            baseUrl,
            cur.model,
            systemPrompt,
            messages,
            cur.temperature,
            onDelta,
            controller.signal,
            ANTHROPIC_TOOLS,
          );

          return { ...result, aborted: false };
        }
        const result = await streamOpenAiChat(
          baseUrl,
          cur.model,
          [{ role: "system", content: systemPrompt }, ...messages],
          cur.temperature,
          onDelta,
          controller.signal,
          OPENAI_TOOLS,
        );

        return { ...result, aborted: false };
      } catch (err) {
        const rawMsg = err instanceof Error ? err.message : String(err);
        const isAbort =
          rawMsg.includes("AbortError") ||
          rawMsg.includes("aborted") ||
          (err instanceof DOMException && err.name === "AbortError");

        if (isAbort) return { text: full, nativeToolCalls: [], aborted: true };
        // 流中途断掉（网络闪断/供应商中断）时把已生成的半截内容挂在错误上：
        // 上层 catch 据此保留部分回复，而不是让用户眼看着内容蒸发后整条重发
        if (full.trim() && err && typeof err === "object") {
          (err as { partialText?: string }).partialText = full;
        }
        throw err;
      } finally {
        setIsStreaming(false);
        abortControllerRef.current = null;
      }
    },
    [activeEndpoint],
  );

  /** 挂起等待用户批准，由批准（true）/拒绝（false）/总是允许（"always"）按钮 resolve */
  const requestApproval = useCallback(
    (toolCallId: string): Promise<boolean | "always"> => {
      return new Promise<boolean | "always">((resolve) => {
        approvalWaitersRef.current.set(toolCallId, resolve);
      });
    },
    [],
  );

  const resolveApproval = useCallback(
    (toolCallId: string, approved: boolean | "always") => {
      const waiter = approvalWaitersRef.current.get(toolCallId);

      if (waiter) {
        approvalWaitersRef.current.delete(toolCallId);
        waiter(approved);
      }
    },
    [],
  );

  /** 记录「总是允许」：ref 供 Agent 循环即时读取，state 镜像供设置弹窗展示/撤销 */
  const allowToolAlways = useCallback((name: string) => {
    alwaysAllowedToolsRef.current.add(name);
    setAlwaysAllowedList(Array.from(alwaysAllowedToolsRef.current));
  }, []);

  /** 撤销本运行期内全部「总是允许」授权 */
  const resetAlwaysAllowed = useCallback(() => {
    alwaysAllowedToolsRef.current.clear();
    setAlwaysAllowedList([]);
  }, []);

  // ------------------------------------------------------------------
  // Agent 循环：请求 → 解析 <tool_call> → 执行 → 回填结果 → 继续
  // ------------------------------------------------------------------

  const runAgentTurn = useCallback(
    async (sessionId: string, convo: ChatMessage[], summary?: string) => {
      const working = [...convo];

      setContinueTarget(null);

      for (let round = 0; round < MAX_TOOL_ROUNDS; round++) {
        resetStreaming();
        let responseText: string;
        let nativeToolCalls: NativeToolCall[] = [];
        let aborted: boolean;

        try {
          const result = await callModelOnce(
            working,
            appendStreamChunk,
            summary,
          );

          responseText = result.text;
          // Anthropic 的 max_tokens 截断此前的表现是无声断尾——补一个标记
          if (result.truncated)
            responseText += `\n\n*(${t("回复因长度限制被截断，可让 AI 继续输出")})*`;
          nativeToolCalls = result.nativeToolCalls;
          aborted = result.aborted;
        } catch (err) {
          // 网络中断时先保留已生成的部分回复（与手动停止同语义），再附错误卡片
          const partial = (err as { partialText?: string })?.partialText ?? "";

          if (partial.trim()) {
            const partialMsg: ChatMessage = {
              id: uid(),
              role: "assistant",
              content: `${partial}\n\n*(${t("连接中断，以下为已生成的部分内容")})*`,
              timestamp: Date.now(),
            };

            updateSessionMessages(sessionId, (msgs) => [...msgs, partialMsg]);
          }
          const classified = classifyApiError(err);
          const errMsg: ChatMessage = {
            id: uid(),
            role: "assistant",
            content: formatErrorMessage(classified),
            timestamp: Date.now(),
          };

          updateSessionMessages(sessionId, (msgs) => [...msgs, errMsg]);

          return;
        }

        if (aborted) {
          // 用户主动停止：已生成的部分内容保存下来，不再继续
          if (responseText.trim()) {
            const partialMsg: ChatMessage = {
              id: uid(),
              role: "assistant",
              content: `${responseText}\n\n*(${t("已手动停止生成")})*`,
              timestamp: Date.now(),
            };

            updateSessionMessages(sessionId, (msgs) => [...msgs, partialMsg]);
          }

          return;
        }

        if (!responseText.trim() && nativeToolCalls.length === 0) {
          const errMsg: ChatMessage = {
            id: uid(),
            role: "assistant",
            content:
              `❌ **${t("模型返回了空内容")}**\n\n` +
              t(
                "请检查模型名称与 API 格式是否匹配（如 Anthropic 格式需选择 Claude 系列模型）。",
              ),
            timestamp: Date.now(),
          };

          updateSessionMessages(sessionId, (msgs) => [...msgs, errMsg]);

          return;
        }

        // 优先用原生 function calling 的结构化调用；没有时回退解析文本协议
        // （parseToolCalls 同时兼容 <tool_call> 与泄漏成文本的 DSML/tool▁call 格式）
        let toolCalls: ToolCall[];

        if (nativeToolCalls.length > 0) {
          toolCalls = nativeToolCalls.map((c) => ({
            id: uid(),
            name: c.name,
            args: c.arguments || "{}",
            status: "pending" as const,
            callId: c.id || undefined,
          }));
        } else {
          const { calls, malformed } = parseToolCalls(responseText);

          toolCalls = calls.map((c) => ({
            id: uid(),
            name: c.name,
            args: c.rawArgs,
            status: "pending" as const,
          }));

          if (malformed > 0) {
            toolCalls.push({
              id: uid(),
              name: "parse_error",
              args: "{}",
              status: "error" as const,
              result: t(
                '{count} 个工具调用块 JSON 解析失败，请严格按 <tool_call>{"name":"…","args":{…}}</tool_call> 格式输出',
                { count: malformed },
              ),
            });
          }
        }

        const aiMsg: ChatMessage = {
          id: uid(),
          role: "assistant",
          // 保留原始文本（含 <tool_call> 块），模型下一轮需要看到自己发过的调用
          content: responseText,
          timestamp: Date.now(),
          toolCalls: toolCalls.length > 0 ? toolCalls : undefined,
        };

        updateSessionMessages(sessionId, (msgs) => [...msgs, aiMsg]);

        if (toolCalls.length === 0) return; // 纯文本回复，回合结束

        const setToolCallStatus = (
          toolCallId: string,
          status: ToolCall["status"],
          result?: string,
        ) => {
          updateSessionMessages(sessionId, (msgs) =>
            msgs.map((m) =>
              m.id === aiMsg.id
                ? {
                    ...m,
                    toolCalls: m.toolCalls?.map((x) =>
                      x.id === toolCallId
                        ? {
                            ...x,
                            status,
                            ...(result !== undefined ? { result } : {}),
                          }
                        : x,
                    ),
                  }
                : m,
            ),
          );
        };

        const ctx = await resolveToolContext();
        /** 每个调用的执行结果（按 toolCall.id 记录，回填时按原顺序取出） */
        const outcomes = new Map<string, { ok: boolean; result: string }>();
        /** 受权限开关控制的调用直接返回拒绝文案（不进入批准流程） */
        const permissionDenied = (tc: ToolCall): string | null => {
          if (
            FOLDER_READ_TOOLS.has(tc.name) &&
            !settingsRef.current.allowFolderRead
          ) {
            return "权限不足：用户未开启「查看实例文件夹」权限，该工具被拒绝。请不要重试，告知用户可在 AI 设置 → 实例操作权限中开启";
          }
          if (
            FOLDER_WRITE_TOOLS.has(tc.name) &&
            !settingsRef.current.allowFolderWrite
          ) {
            return "权限不足：用户未开启「修改实例文件夹」权限，该工具被拒绝。请不要重试，告知用户可在 AI 设置 → 实例操作权限中开启";
          }

          return null;
        };
        const executeAndRecord = async (tc: ToolCall) => {
          setToolCallStatus(tc.id, "running");
          const executed = await executeTool(tc.name, toolArgsOf(tc.args), ctx);

          setToolCallStatus(
            tc.id,
            executed.ok ? "success" : "error",
            executed.result,
          );
          outcomes.set(tc.id, executed);
        };

        // 第一波：无需批准的调用并行执行（相互独立的工具没有依赖关系）
        const directCalls = toolCalls.filter(
          (tc) =>
            tc.name !== "parse_error" &&
            !permissionDenied(tc) &&
            !(
              CONFIRM_TOOLS.has(tc.name) &&
              !settingsRef.current.allowModify &&
              !alwaysAllowedToolsRef.current.has(tc.name)
            ),
        );

        await Promise.all(directCalls.map(executeAndRecord));

        // 第二波：解析错误与权限拒绝就地出结果；修改类调用逐个挂起等批准
        for (const tc of toolCalls) {
          if (outcomes.has(tc.id)) continue;

          if (tc.name === "parse_error") {
            outcomes.set(tc.id, {
              ok: false,
              result: tc.result ?? "工具调用格式错误",
            });
            continue;
          }
          const denied = permissionDenied(tc);

          if (denied) {
            setToolCallStatus(
              tc.id,
              "error",
              FOLDER_READ_TOOLS.has(tc.name)
                ? t("权限未开启：查看实例文件夹")
                : t("权限未开启：修改实例文件夹"),
            );
            outcomes.set(tc.id, { ok: false, result: denied });
            continue;
          }
          if (
            CONFIRM_TOOLS.has(tc.name) &&
            !settingsRef.current.allowModify &&
            !alwaysAllowedToolsRef.current.has(tc.name)
          ) {
            // 只读模式：修改类工具挂起等待批准；"总是允许"后运行期内免批
            const decision = await requestApproval(tc.id);

            if (decision === "always") {
              allowToolAlways(tc.name);
            } else if (!decision) {
              setToolCallStatus(tc.id, "error", t("用户拒绝执行"));
              outcomes.set(tc.id, {
                ok: false,
                result: "用户拒绝执行该操作，请不要重复尝试",
              });
              continue;
            }
          }
          await executeAndRecord(tc);
        }

        const results = toolCalls.map((tc) => ({
          tc,
          ...(outcomes.get(tc.id) ?? { ok: false, result: "" }),
        }));

        // 回填：assistant（含调用原文）+ 每个工具的结果，继续下一轮
        working.push(aiMsg);
        for (const r of results) {
          const payload = JSON.stringify({
            tool: r.tc.name,
            ok: r.ok,
            result: r.result,
          });
          const toolResultMsg: ChatMessage = {
            id: uid(),
            role: "tool_result",
            // 原生调用走 role=tool 消息（纯 JSON）；文本协议保留 <tool_result> 包裹
            content: r.tc.callId
              ? payload
              : `<tool_result>\n${payload}\n</tool_result>`,
            timestamp: Date.now(),
            toolCallId: r.tc.callId,
          };

          working.push(toolResultMsg);
          updateSessionMessages(sessionId, (msgs) => [...msgs, toolResultMsg]);
        }
      }

      const limitMsg: ChatMessage = {
        id: uid(),
        role: "assistant",
        content: t(
          "⚠️ 已连续执行 {count} 轮工具调用，本轮自动暂停。如需继续，请再发一条消息。",
          { count: MAX_TOOL_ROUNDS },
        ),
        timestamp: Date.now(),
      };

      updateSessionMessages(sessionId, (msgs) => [...msgs, limitMsg]);
      setContinueTarget(sessionId);
    },
    [
      allowToolAlways,
      appendStreamChunk,
      callModelOnce,
      requestApproval,
      resetStreaming,
      updateSessionMessages,
    ],
  );

  // ------------------------------------------------------------------
  // 会话压缩：接近上下文上限时把较早的历史摘要成一段"记忆"
  // ------------------------------------------------------------------

  /**
   * 把 convo 中较早的消息压缩成摘要：保留最近 KEEP_RECENT_MESSAGES 条原文，
   * 其余发给模型总结后存入 session.summary 并从消息列表移除。
   * 摘要调用失败时静默放弃（null）——压缩只是优化，不该打断正常对话。
   */
  const compactHistory = useCallback(
    async (
      sessionId: string,
      convo: ChatMessage[],
    ): Promise<{ kept: ChatMessage[]; summary: string } | null> => {
      const visible = convo.filter((m) => m.role !== "tool_result");

      if (visible.length <= KEEP_RECENT_MESSAGES) return null;
      const keptVisible = visible.slice(-KEEP_RECENT_MESSAGES);
      const keptIds = new Set(keptVisible.map((m) => m.id));
      const old = visible.filter((m) => !keptIds.has(m.id));

      if (old.length === 0) return null;

      const transcript = truncateMiddle(
        old
          .map((m) => {
            if (m.role === "user") return `用户: ${m.content}`;
            if (m.role === "assistant")
              return `助手: ${stripToolBlocks(m.content)}`;

            return "";
          })
          .filter(Boolean)
          .join("\n\n"),
        12000,
      );
      const { cur, baseUrl, provider } = activeEndpoint();

      // key 已存后端时表单里是空的（明文不回读），视同已配置
      if (
        (!cur.apiKey.trim() && !aiKeyStored && !provider?.local) ||
        !baseUrl.trim() ||
        !cur.model.trim()
      )
        return null;
      const prompt = `${transcript}`;
      const existing = sessions.find((s) => s.id === sessionId)?.summary;

      try {
        setIsCompacting(true);
        // 压缩请求必须有中止通道：供应商挂起/本地引擎假死时，停止按钮
        // 也要能救回来（此前 signal 传 undefined，isSending 一卡几分钟）
        const compaction = new AbortController();

        compactionAbortRef.current = compaction;
        const result =
          cur.apiFormat === "anthropic"
            ? await streamAnthropicChat(
                baseUrl,
                cur.model,
                SUMMARY_SYSTEM_PROMPT,
                [{ role: "user", content: prompt }],
                0.2,
                () => {},
                compaction.signal,
                undefined,
              )
            : await streamOpenAiChat(
                baseUrl,
                cur.model,
                [
                  { role: "system", content: SUMMARY_SYSTEM_PROMPT },
                  { role: "user", content: prompt },
                ],
                0.2,
                () => {},
                compaction.signal,
                undefined,
              );
        const text = result.text.trim();

        if (!text) return null;
        const summary = existing ? `${existing}\n\n${text}` : text;
        const notice: ChatMessage = {
          id: uid(),
          role: "action",
          content: "",
          actionStatus: "approved",
          actionLabel: t("已自动压缩较早的对话历史以释放上下文"),
          timestamp: Date.now(),
        };

        updateSessionMessages(sessionId, () => [...keptVisible, notice]);

        return { kept: keptVisible, summary };
      } catch {
        return null;
      } finally {
        compactionAbortRef.current = null;
        setIsCompacting(false);
      }
    },
    [activeEndpoint, aiKeyStored, sessions, updateSessionMessages],
  );

  /**
   * 统一执行一个对话回合：先按需压缩历史，再跑 Agent 循环。
   * sendMessage 与消息编辑重发共用，保证压缩策略一致。
   */
  const executeTurn = useCallback(
    async (sessionId: string, convo: ChatMessage[]) => {
      setIsSending(true);
      resetStreaming();
      let working = [...convo];
      let summary = sessions.find((s) => s.id === sessionId)?.summary;

      try {
        const { cur } = activeEndpoint();
        const estimate =
          estimateSessionTokens(working, buildSystemPrompt(cur)) +
          (summary ? estimateTokens(summary) : 0);

        if (estimate > cur.contextWindow * 0.8) {
          const compacted = await compactHistory(sessionId, working);

          if (compacted) {
            working = compacted.kept;
            summary = compacted.summary;
          }
        }
        await runAgentTurn(sessionId, working, summary);
      } catch (err) {
        // runAgentTurn 内部已兜底，这里防御性地不再抛出
        console.error("AI agent turn failed:", err);
        const errMsg: ChatMessage = {
          id: uid(),
          role: "assistant",
          content: formatErrorMessage(classifyApiError(err)),
          timestamp: Date.now(),
        };

        updateSessionMessages(sessionId, (msgs) => [...msgs, errMsg]);
      } finally {
        setIsSending(false);
        resetStreaming();
        abortControllerRef.current = null;
      }
    },
    [
      activeEndpoint,
      compactHistory,
      resetStreaming,
      runAgentTurn,
      sessions,
      updateSessionMessages,
    ],
  );

  /** 收集未填写的必填 AI 配置项（发送与编辑重发共用；本地引擎免 API Key） */
  const missingConfigItems = useCallback(() => {
    const { cur, baseUrl, provider } = activeEndpoint();
    const missing: string[] = [];

    if (!provider?.local && !cur.apiKey.trim() && !aiKeyStored)
      missing.push(t("API Key"));
    if (!baseUrl.trim()) missing.push(t("API 地址"));
    if (!cur.model.trim()) missing.push(t("模型名称"));

    return missing;
  }, [activeEndpoint, aiKeyStored]);

  // 发送消息（真实 API 流式调用 + Agent 工具循环，未配置直接报错）
  const sendMessage = useCallback(async () => {
    const text = input.trim();

    if (!text || isSending) return;

    let sessionId = activeSessionId;
    // 历史必须取"本条消息之前"的消息；新建会话时 activeSession 还是旧会话，须置空
    let priorMessages: ChatMessage[] =
      sessionId === activeSessionId ? (activeSession?.messages ?? []) : [];

    if (!sessionId) {
      const newSession: ChatSession = {
        id: uid(),
        title: deriveTitle(text),
        messages: [],
        createdAt: Date.now(),
        updatedAt: Date.now(),
      };

      setSessions((prev) => [newSession, ...prev]);
      sessionId = newSession.id;
      setActiveSessionId(sessionId);
      priorMessages = [];
    }

    const userMsg: ChatMessage = {
      id: uid(),
      role: "user",
      content: text,
      timestamp: Date.now(),
      attachments:
        pendingAttachments.length > 0 ? [...pendingAttachments] : undefined,
    };

    updateSessionMessages(sessionId, (msgs) => [...msgs, userMsg]);
    // 清空待发送附件
    setPendingAttachments([]);

    setSessions((prev) =>
      prev.map((s) => {
        if (s.id !== sessionId || s.messages.length > 0) return s;

        return { ...s, title: deriveTitle(text) };
      }),
    );

    setInput("");

    // 配置检查：未配置直接报错，不再有模拟兜底
    const missing = missingConfigItems();

    if (missing.length > 0) {
      const errMsg: ChatMessage = {
        id: uid(),
        role: "assistant",
        content:
          `❌ **${t("尚未配置 AI")}**\n\n` +
          t("以下配置项未填写：{items}", { items: missing.join("、") }) +
          `\n\n` +
          t("请在「AI 设置」中补全后再试。"),
        timestamp: Date.now(),
      };

      updateSessionMessages(sessionId, (msgs) => [...msgs, errMsg]);

      return;
    }

    await executeTurn(sessionId, [...priorMessages, userMsg]);
  }, [
    input,
    isSending,
    activeSessionId,
    activeSession,
    executeTurn,
    missingConfigItems,
    updateSessionMessages,
    pendingAttachments,
  ]);

  /** 编辑历史用户消息并重发：截掉该消息之后的所有内容，替换文本后重跑回合 */
  const resendFrom = useCallback(
    async (sessionId: string, msgId: string, newContent: string) => {
      const session = sessions.find((s) => s.id === sessionId);
      const text = newContent.trim();

      if (!session || isSending || !text) return;
      const index = session.messages.findIndex((m) => m.id === msgId);

      if (index < 0) return;
      const target = session.messages[index];

      if (target.role !== "user") return;
      const convo: ChatMessage[] = [
        ...session.messages.slice(0, index),
        { ...target, content: text },
      ];

      updateSessionMessages(sessionId, () => convo);
      setEditingMessageId(null);

      if (missingConfigItems().length > 0) return;
      await executeTurn(sessionId, convo);
    },
    [
      executeTurn,
      isSending,
      missingConfigItems,
      sessions,
      updateSessionMessages,
    ],
  );

  const stopStreaming = useCallback(() => {
    abortControllerRef.current?.abort();
    // 压缩阶段 abortControllerRef 是 null：停止按钮也要能中断压缩请求
    compactionAbortRef.current?.abort();
  }, []);

  /** 工具轮数上限暂停后一键继续：以用户身份发一条「继续」并重跑回合 */
  const continueTurn = useCallback(async () => {
    const sessionId = activeSessionId;

    if (!sessionId || isSending) return;
    const msg: ChatMessage = {
      id: uid(),
      role: "user",
      content: t("继续"),
      timestamp: Date.now(),
    };

    updateSessionMessages(sessionId, (msgs) => [...msgs, msg]);
    await executeTurn(sessionId, [...(activeSession?.messages ?? []), msg]);
  }, [
    activeSession,
    activeSessionId,
    executeTurn,
    isSending,
    updateSessionMessages,
  ]);

  // 输入框键盘处理
  const handleKeyDown = (e: React.KeyboardEvent) => {
    // 中文/日文等输入法组词回车不应发送（isComposing 或 keyCode 229）
    if (e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229) return;
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void sendMessage();
    }
  };

  // ------------------------------------------------------------------
  // 消息区滚动：只滚消息列表容器自身
  // ------------------------------------------------------------------
  // 之前用 scrollIntoView：规范上它会滚动"所有可滚动祖先"（含 overflow:hidden
  // 容器），不同 WebView 版本下可能把整个界面一起滚走。改为直接设置容器
  // scrollTop，作用范围被锁死在消息列表内部。

  /** 用户贴底才跟随；上翻阅读时不打扰，并浮出"回到底部"按钮 */
  const handleListScroll = useCallback(() => {
    const el = messagesScrollRef.current;

    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 120;

    isNearBottomRef.current = nearBottom;
    setShowScrollToBottom(!nearBottom);
  }, []);

  const scrollToBottom = useCallback((smooth = false) => {
    const el = messagesScrollRef.current;

    if (!el) return;
    el.scrollTo({ top: el.scrollHeight, behavior: smooth ? "smooth" : "auto" });
    isNearBottomRef.current = true;
    setShowScrollToBottom(false);
  }, []);

  // 新消息到达时跟随（贴底状态才滚）
  useEffect(() => {
    if (isNearBottomRef.current) scrollToBottom();
  }, [activeSession?.messages.length, isSending, scrollToBottom]);

  // 流式输出期间逐块跟随（同样只在贴底状态）
  useEffect(() => {
    if (isStreaming && isNearBottomRef.current) scrollToBottom();
  }, [streamingContent, isStreaming, scrollToBottom]);

  // 切换会话：回到贴底状态并滚到底
  useEffect(() => {
    isNearBottomRef.current = true;
    setShowScrollToBottom(false);
    // 等消息渲染完再滚
    requestAnimationFrame(() => scrollToBottom());
  }, [activeSessionId, scrollToBottom]);

  // 自动调整输入框高度
  useEffect(() => {
    const ta = textareaRef.current;

    if (!ta) return;
    ta.style.height = "auto";
    ta.style.height = `${Math.min(ta.scrollHeight, 160)}px`;
  }, [input]);

  // 切换供应商时更新默认模型和 API 格式
  const handleProviderChange = (providerId: string) => {
    const provider = BUILTIN_PROVIDERS.find((p) => p.id === providerId);

    setSettings((prev) => ({
      ...prev,
      providerId,
      model: provider?.defaultModel ?? prev.model,
      apiFormat: provider?.apiFormat ?? prev.apiFormat,
      // 本地引擎之间端口各不相同，切换时清空手填地址回落预设，避免拿旧地址连新引擎
      customBaseUrl: provider?.local ? "" : prev.customBaseUrl,
    }));
  };

  return (
    <section className="relative flex h-full w-full min-h-0 overflow-hidden">
      {/* 左侧会话栏：悬浮式毛玻璃面板——聊天区占满整行，背景图从栏后一路透出；
          浮在消息之上用 nya-panel-strong（更实的底色，防止下层文字透上来发花）。
          折叠 = 宽度 + 透明度一起收（绝对定位后宽度为 0 仍会残留边框线），
          折叠态顺带 pointer-events:none 防止透明面板挡住下面的点击。
          z-10：低于全局侧栏的 z-20（Sidebar.tsx），自动隐藏侧栏滑出时盖在
          本面板之上，而不是反过来挡住导航。 */}
      <motion.div
        animate={{
          width: sidebarCollapsed ? 0 : 280,
          opacity: sidebarCollapsed ? 0 : 1,
        }}
        className="absolute inset-y-3 left-3 z-10 overflow-hidden rounded-large border nya-border nya-panel-strong shadow-lg"
        style={{ pointerEvents: sidebarCollapsed ? "none" : "auto" }}
        transition={{ duration: 0.3, ease: [0.4, 0, 0.2, 1] }}
      >
        <div className="flex h-full w-[280px] flex-col">
          <div className="flex-shrink-0 p-3">
            <Button
              fullWidth
              color="primary"
              startContent={<NewChatIcon />}
              onPress={createSession}
            >
              {t("新建对话")}
            </Button>
          </div>

          <div className="nya-scroll min-h-0 flex-1 overflow-y-auto px-2">
            {sessions.length === 0 ? (
              <div className="my-12 flex flex-col items-center gap-3 text-center">
                <div className="flex size-14 items-center justify-center rounded-lg bg-gradient-to-br from-default-200 to-default-100">
                  <ChatIcon className="h-7 w-7 text-gray-400" />
                </div>
                <span className="text-[13px] font-medium text-gray-500">
                  {t("暂无对话")}
                </span>
                <Button
                  color="primary"
                  size="sm"
                  startContent={<NewChatIcon />}
                  variant="flat"
                  onPress={createSession}
                >
                  {t("新建对话")}
                </Button>
              </div>
            ) : (
              <AnimatePresence initial={false}>
                {sortedSessions.map((session) => {
                  const isActive = session.id === activeSessionId;
                  const isRenaming = session.id === renamingId;

                  return (
                    <motion.div
                      key={session.id}
                      animate="center"
                      className="mb-1"
                      exit="exit"
                      initial="enter"
                      variants={listItemVariants}
                    >
                      <div
                        className={`group flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 transition-all ${
                          isActive
                            ? "bg-primary/15 text-primary"
                            : "hover:bg-default-100/70"
                        }`}
                        role="button"
                        tabIndex={0}
                        onClick={() => {
                          if (!isRenaming) setActiveSessionId(session.id);
                        }}
                        onKeyDown={(e) => {
                          if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault();
                            setActiveSessionId(session.id);
                          }
                        }}
                      >
                        {session.pinned ? (
                          <PinIcon className="h-3.5 w-3.5 flex-shrink-0 text-primary opacity-80" />
                        ) : (
                          <ChatIcon className="h-4 w-4 flex-shrink-0 opacity-70" />
                        )}
                        <div className="min-w-0 flex-1">
                          {isRenaming ? (
                            <input
                              /* eslint-disable-next-line jsx-a11y/no-autofocus -- 重命名弹出的行内输入框需要立即聚焦 */
                              autoFocus
                              className="w-full rounded-md border border-primary/40 bg-default-100 px-1.5 py-0.5 text-sm outline-none"
                              value={renameDraft}
                              onBlur={() => {
                                renameSession(session.id, renameDraft);
                                setRenamingId(null);
                              }}
                              onChange={(e) => setRenameDraft(e.target.value)}
                              onClick={(e) => e.stopPropagation()}
                              onKeyDown={(e) => {
                                if (e.key === "Enter") {
                                  e.preventDefault();
                                  renameSession(session.id, renameDraft);
                                  setRenamingId(null);
                                } else if (e.key === "Escape") {
                                  setRenamingId(null);
                                }
                              }}
                            />
                          ) : (
                            <>
                              <div className="truncate text-sm font-medium">
                                {session.title}
                              </div>
                              <div className="truncate text-[11px] text-gray-400">
                                {formatTime(session.updatedAt)} ·{" "}
                                {
                                  session.messages.filter(
                                    (m) => m.role !== "tool_result",
                                  ).length
                                }{" "}
                                {t("条消息")}
                              </div>
                            </>
                          )}
                        </div>
                        <Tooltip content={t("置顶/取消置顶")} delay={300}>
                          <Button
                            isIconOnly
                            className={`min-w-6 h-6 ${
                              session.pinned
                                ? "text-primary"
                                : "opacity-0 group-hover:opacity-100"
                            }`}
                            size="sm"
                            variant="light"
                            onClick={(e) => {
                              e.stopPropagation();
                              togglePinSession(session.id);
                            }}
                          >
                            {session.pinned ? (
                              <UnpinIcon className="h-3.5 w-3.5" />
                            ) : (
                              <PinIcon className="h-3.5 w-3.5 text-gray-400" />
                            )}
                          </Button>
                        </Tooltip>
                        <Tooltip content={t("重命名")} delay={300}>
                          <Button
                            isIconOnly
                            className="min-w-6 h-6 opacity-0 group-hover:opacity-100"
                            size="sm"
                            variant="light"
                            onClick={(e) => {
                              e.stopPropagation();
                              setRenameDraft(session.title);
                              setRenamingId(session.id);
                            }}
                          >
                            <RenameIcon className="h-3.5 w-3.5 text-gray-400" />
                          </Button>
                        </Tooltip>
                        <Tooltip content={t("删除对话")} delay={300}>
                          <Button
                            isIconOnly
                            className="min-w-6 h-6 opacity-0 group-hover:opacity-100"
                            size="sm"
                            variant="light"
                            onClick={(e) => {
                              e.stopPropagation();
                              deleteSession(session.id);
                            }}
                          >
                            <DeleteIcon className="h-3.5 w-3.5 text-gray-400" />
                          </Button>
                        </Tooltip>
                      </div>
                    </motion.div>
                  );
                })}
              </AnimatePresence>
            )}
          </div>

          <div className="flex-shrink-0 border-t nya-border p-3">
            <div
              className={`mb-2 flex items-center gap-2 rounded-lg px-2.5 py-1.5 text-[11px] ${
                !settings.allowFolderRead && !settings.allowFolderWrite
                  ? "bg-danger/10 text-danger"
                  : settings.allowModify
                    ? "bg-success/10 text-success"
                    : "bg-warning/10 text-warning"
              }`}
            >
              {!settings.allowFolderRead && !settings.allowFolderWrite ? (
                <>
                  <RejectIcon className="h-3.5 w-3.5" />
                  <span>{t("已禁止访问实例文件夹")}</span>
                </>
              ) : settings.allowModify ? (
                <>
                  <SparkleIcon className="h-3.5 w-3.5" />
                  <span>{t("已授权直接修改")}</span>
                </>
              ) : (
                <>
                  <ReadOnlyIcon className="h-3.5 w-3.5" />
                </>
              )}
            </div>

            <Button
              fullWidth
              startContent={<SettingsIcon />}
              variant="flat"
              onPress={() => setSettingsOpen(true)}
            >
              {t("AI 设置")}
            </Button>
          </div>
        </div>
      </motion.div>

      {/* 右侧聊天区：占满整行，标题栏让出悬浮面板的宽度（280 面板 + 12 左距 + 12 缝），
          与面板折叠动画同曲线，折叠按钮不跳位 */}
      <div className="flex min-h-0 flex-1 flex-col">
        <motion.header
          animate={{ paddingLeft: sidebarCollapsed ? 12 : 304 }}
          className="flex flex-shrink-0 items-center gap-2.5 border-b nya-border py-2.5 pr-3"
          transition={{ duration: 0.3, ease: [0.4, 0, 0.2, 1] }}
        >
          {/* 侧边栏折叠按钮：整合到标题栏，位置固定不随侧边栏跳动 */}
          <Tooltip
            content={sidebarCollapsed ? t("展开会话列表") : t("收起会话列表")}
            delay={200}
            placement="bottom"
          >
            <Button
              isIconOnly
              className="flex-shrink-0 min-w-8 h-8 text-gray-400 hover:text-primary hover:bg-primary/10 transition-colors"
              radius="full"
              variant="light"
              onPress={() => setSidebarCollapsed((v) => !v)}
            >
              <motion.span
                animate={{ rotate: sidebarCollapsed ? 0 : 180 }}
                className="text-base leading-none"
                transition={{ duration: 0.3, ease: "easeInOut" }}
              >
                {sidebarCollapsed ? "›" : "‹"}
              </motion.span>
            </Button>
          </Tooltip>

          {/* 分隔线 */}
          <div className="h-6 w-px bg-default-200 flex-shrink-0" />

          {/* AI 图标：与其它页面标题区同规格（size-9 圆角方块 + primary/15 底） */}
          <div className="flex size-9 flex-shrink-0 items-center justify-center rounded-lg bg-primary/15 text-primary">
            <NekoAgentIcon className="h-5 w-5" />
          </div>

          {/* 标题信息 */}
          <div className="min-w-0 flex-1">
            <div className="truncate text-[15px] font-semibold leading-tight text-gray-800 dark:text-gray-200">
              {activeSession?.title ?? t("NekoAgent喵")}
            </div>
            <div className="truncate text-[11px] text-gray-400 leading-tight mt-0.5">
              {currentProvider.name} · {settings.model || t("未设置模型")}
            </div>
          </div>

          {/* 权限徽标：一眼看清 AI 当前能做什么（点徽标直接打开设置） */}
          <div className="flex flex-shrink-0 items-center gap-1">
            {(
              [
                {
                  key: "read",
                  on: settings.allowFolderRead,
                  onLabel: t("查看开"),
                  offLabel: t("查看关"),
                  tip: t("查看实例文件夹权限"),
                },
                {
                  key: "write",
                  on: settings.allowFolderWrite,
                  onLabel: t("修改开"),
                  offLabel: t("修改关"),
                  tip: t("修改实例文件夹权限"),
                },
                {
                  key: "auto",
                  on: settings.allowModify,
                  onLabel: t("直改开"),
                  offLabel: t("需确认"),
                  tip: t("直接修改（免批准）权限"),
                },
              ] as const
            ).map((badge) => (
              <Tooltip
                key={badge.key}
                content={badge.tip}
                delay={300}
                placement="bottom"
              >
                <button
                  aria-label={badge.tip}
                  className={`rounded-md px-1.5 py-0.5 text-[11px] font-medium leading-none transition-colors ${
                    badge.on
                      ? "bg-success/15 text-success"
                      : "bg-default-100 text-gray-400"
                  }`}
                  type="button"
                  onClick={() => setSettingsOpen(true)}
                >
                  {badge.on ? badge.onLabel : badge.offLabel}
                </button>
              </Tooltip>
            ))}
          </div>

          {/* 消息数量 */}
          {activeSession && activeSession.messages.length > 0 && (
            <span className="flex-shrink-0 rounded-full bg-default-100 px-2 py-0.5 text-[11px] text-gray-500 font-medium">
              {
                activeSession.messages.filter((m) => m.role !== "tool_result")
                  .length
              }
            </span>
          )}
        </motion.header>

        {/* 消息区：外层定位容器让"回到底部"按钮悬浮在列表右下角 */}
        <div className="relative min-h-0 flex-1">
          <div
            ref={messagesScrollRef}
            className="nya-scroll h-full overflow-y-auto"
            onScroll={handleListScroll}
          >
            {!activeSession || activeSession.messages.length === 0 ? (
              <div className="flex h-full flex-col items-center justify-center gap-4 px-6 text-center">
                <motion.div
                  animate={{ y: [0, -6, 0] }}
                  className="flex size-16 items-center justify-center rounded-lg bg-gradient-to-br from-primary/20 to-secondary/10 text-primary"
                  transition={{
                    duration: 3,
                    repeat: Infinity,
                    ease: "easeInOut",
                  }}
                >
                  <NekoAgentIcon className="h-8 w-8" />
                </motion.div>
                <div>
                  <h2 className="text-xl font-semibold text-gray-800 dark:text-gray-200">
                    {t("你好，我是 NekoAgent喵~")}
                  </h2>
                  <p className="mt-1 max-w-md text-[13px] text-gray-500 dark:text-gray-400">
                    {t(
                      "我可以帮你管理 Minecraft 实例、解答问题、调整配置。输入消息开始对话吧！",
                    )}
                  </p>
                </div>
              </div>
            ) : (
              <div className="mx-auto max-w-3xl px-5 py-6">
                <AnimatePresence initial={false}>
                  {activeSession.messages
                    .filter((msg) => msg.role !== "tool_result")
                    .map((msg) => (
                      <motion.div
                        key={msg.id}
                        animate={{ opacity: 1, y: 0 }}
                        className="mb-5"
                        exit={{ opacity: 0 }}
                        initial={{ opacity: 0, y: 8 }}
                        transition={{ duration: 0.2 }}
                      >
                        {msg.role === "user" ? (
                          <div className="group flex flex-col items-end gap-1">
                            {editingMessageId === msg.id ? (
                              <div className="w-full max-w-[85%] rounded-lg border border-primary/40 bg-default-50 p-2">
                                <Textarea
                                  /* eslint-disable-next-line jsx-a11y/no-autofocus -- 编辑弹出的输入框需要立即聚焦 */
                                  autoFocus
                                  classNames={{
                                    input:
                                      "min-h-[60px] max-h-[160px] resize-none py-1.5",
                                    inputWrapper:
                                      "bg-transparent border-0 shadow-none",
                                  }}
                                  radius="none"
                                  value={editDraft}
                                  variant="flat"
                                  onValueChange={setEditDraft}
                                />
                                <div className="mt-1 flex justify-end gap-2">
                                  <Button
                                    size="sm"
                                    variant="flat"
                                    onPress={() => setEditingMessageId(null)}
                                  >
                                    {t("取消")}
                                  </Button>
                                  <Button
                                    color="primary"
                                    isDisabled={!editDraft.trim() || isSending}
                                    size="sm"
                                    startContent={
                                      <ApproveIcon className="h-3.5 w-3.5" />
                                    }
                                    onPress={() => {
                                      if (activeSession)
                                        void resendFrom(
                                          activeSession.id,
                                          msg.id,
                                          editDraft,
                                        );
                                    }}
                                  >
                                    {t("保存并重发")}
                                  </Button>
                                </div>
                              </div>
                            ) : (
                              <>
                                <button
                                  aria-label={t("编辑并重发")}
                                  className="mr-1 flex items-center gap-1 text-[11px] text-gray-400 opacity-0 transition-opacity hover:text-primary group-hover:opacity-100"
                                  type="button"
                                  onClick={() => {
                                    setEditDraft(msg.content);
                                    setEditingMessageId(msg.id);
                                  }}
                                >
                                  <RenameIcon className="h-3 w-3" />
                                  {t("编辑并重发")}
                                </button>
                                <div className="flex justify-end gap-3">
                                  <div className="max-w-[80%] rounded-lg rounded-tr-md bg-primary px-4 py-2.5 text-primary-foreground shadow-sm">
                                    {/* 附件预览 */}
                                    {msg.attachments &&
                                      msg.attachments.length > 0 && (
                                        <div className="mb-2 flex flex-wrap gap-2">
                                          {msg.attachments.map((att) =>
                                            att.isImage ? (
                                              <img
                                                key={att.id}
                                                alt={att.name}
                                                className="max-h-32 max-w-full rounded-lg object-cover border border-white/20"
                                                src={att.dataUrl}
                                              />
                                            ) : (
                                              <div
                                                key={att.id}
                                                className="flex items-center gap-2 rounded-lg bg-white/10 px-2.5 py-1.5 text-xs"
                                              >
                                                <AttachIcon className="h-3.5 w-3.5 flex-shrink-0" />
                                                <span className="max-w-[160px] truncate">
                                                  {att.name}
                                                </span>
                                                <span className="opacity-60">
                                                  {formatFileSize(att.size)}
                                                </span>
                                              </div>
                                            ),
                                          )}
                                        </div>
                                      )}
                                    <div className="whitespace-pre-wrap text-sm leading-relaxed">
                                      {msg.content}
                                    </div>
                                    <div className="mt-1 text-right text-[11px] opacity-60">
                                      {formatTime(msg.timestamp)}
                                    </div>
                                  </div>
                                  <div className="flex size-8 flex-shrink-0 items-center justify-center rounded-full bg-default-100">
                                    <UserIcon className="h-4 w-4 text-gray-500" />
                                  </div>
                                </div>
                              </>
                            )}
                          </div>
                        ) : msg.role === "action" ? (
                          <div className="flex gap-3">
                            <div className="flex size-8 flex-shrink-0 items-center justify-center rounded-full bg-warning/20">
                              <WarningIcon className="h-4 w-4 text-warning" />
                            </div>
                            <div
                              className={`max-w-[80%] rounded-lg rounded-tl-md border px-4 py-3 shadow-sm ${
                                msg.actionStatus === "approved"
                                  ? "border-success/30 bg-success/5"
                                  : msg.actionStatus === "rejected"
                                    ? "border-danger/30 bg-danger/5"
                                    : "border-warning/30 bg-warning/5"
                              }`}
                            >
                              <div className="flex items-center gap-2 text-xs font-medium">
                                {msg.actionStatus === "approved" && (
                                  <span className="text-success">
                                    {t("系统提示")}
                                  </span>
                                )}
                                {msg.actionStatus === "rejected" && (
                                  <span className="text-danger">
                                    {t("已拒绝")}
                                  </span>
                                )}
                                {msg.actionStatus === "pending" && (
                                  <span className="text-warning">
                                    {t("等待批准")}
                                  </span>
                                )}
                              </div>
                              <div className="mt-1 text-sm">
                                {msg.actionLabel}
                              </div>
                              {msg.actionStatus === "pending" &&
                                activeSession && (
                                  <div className="mt-2 flex gap-2">
                                    <Button
                                      color="success"
                                      size="sm"
                                      startContent={<ApproveIcon />}
                                      onPress={() =>
                                        updateSessionMessages(
                                          activeSession.id,
                                          (msgs) =>
                                            msgs.map((m) =>
                                              m.id === msg.id
                                                ? {
                                                    ...m,
                                                    actionStatus:
                                                      "approved" as const,
                                                  }
                                                : m,
                                            ),
                                        )
                                      }
                                    >
                                      {t("批准")}
                                    </Button>
                                    <Button
                                      color="danger"
                                      size="sm"
                                      startContent={<RejectIcon />}
                                      variant="flat"
                                      onPress={() =>
                                        updateSessionMessages(
                                          activeSession.id,
                                          (msgs) =>
                                            msgs.map((m) =>
                                              m.id === msg.id
                                                ? {
                                                    ...m,
                                                    actionStatus:
                                                      "rejected" as const,
                                                  }
                                                : m,
                                            ),
                                        )
                                      }
                                    >
                                      {t("拒绝")}
                                    </Button>
                                  </div>
                                )}
                            </div>
                          </div>
                        ) : (
                          <div className="flex gap-3">
                            <div className="flex size-8 flex-shrink-0 items-center justify-center rounded-full bg-gradient-to-br from-primary to-secondary text-primary-foreground">
                              <NekoAgentIcon className="h-4 w-4" />
                            </div>
                            <div className="max-w-[80%] rounded-lg rounded-tl-md border nya-border nya-panel px-4 py-2.5">
                              <div className="nya-markdown text-sm leading-relaxed">
                                <MarkdownBody
                                  source={stripToolBlocks(msg.content)}
                                />
                              </div>

                              {/* 工具调用展示（待批准的始终显示） */}
                              {msg.toolCalls && msg.toolCalls.length > 0 && (
                                <div className="mt-2 space-y-1.5">
                                  {msg.toolCalls
                                    .filter(
                                      (tc) =>
                                        settings.showToolCalls ||
                                        (tc.status === "pending" && !tc.result),
                                    )
                                    .map((tc) => {
                                      const isExpanded = expandedToolCalls.has(
                                        tc.id,
                                      );
                                      const statusColor =
                                        tc.status === "success"
                                          ? "text-success"
                                          : tc.status === "error"
                                            ? "text-danger"
                                            : tc.status === "running"
                                              ? "text-primary"
                                              : "text-warning";
                                      const statusText =
                                        tc.status === "success"
                                          ? t("成功")
                                          : tc.status === "error"
                                            ? t("失败")
                                            : tc.status === "running"
                                              ? t("执行中")
                                              : t("待批准");

                                      return (
                                        <div
                                          key={tc.id}
                                          className="rounded-lg border nya-border bg-default-50/50 overflow-hidden"
                                        >
                                          {/* 工具调用头部 */}
                                          <button
                                            className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left cursor-pointer hover:bg-default-100/50 transition-colors"
                                            type="button"
                                            onClick={() =>
                                              toggleToolCall(tc.id)
                                            }
                                          >
                                            <TerminalIcon
                                              className={`h-3.5 w-3.5 flex-shrink-0 ${statusColor}`}
                                            />
                                            <span className="text-xs font-mono font-medium flex-1 truncate">
                                              {tc.name}
                                            </span>
                                            <span
                                              className={`text-[11px] flex-shrink-0 ${
                                                statusColor
                                              } ${
                                                tc.status === "running" ||
                                                tc.status === "pending"
                                                  ? "animate-pulse"
                                                  : ""
                                              }`}
                                            >
                                              {statusText}
                                            </span>
                                            {isExpanded ? (
                                              <ChevronDownIcon className="h-3 w-3 flex-shrink-0 text-gray-400" />
                                            ) : (
                                              <ChevronRightIcon className="h-3 w-3 flex-shrink-0 text-gray-400" />
                                            )}
                                          </button>

                                          {/* 待批准：显示批准/拒绝按钮 */}
                                          {tc.status === "pending" && (
                                            <div className="border-t nya-border px-2.5 py-2 flex items-center gap-2">
                                              <WarningIcon className="h-3.5 w-3.5 flex-shrink-0 text-warning" />
                                              <span className="text-[11px] text-warning flex-1">
                                                {t(
                                                  "该操作需要你的批准才会执行",
                                                )}
                                              </span>
                                              <Button
                                                className="min-w-0 h-7 px-2.5"
                                                color="success"
                                                size="sm"
                                                startContent={
                                                  <ApproveIcon className="h-3 w-3" />
                                                }
                                                onPress={() =>
                                                  resolveApproval(tc.id, true)
                                                }
                                              >
                                                {t("批准")}
                                              </Button>
                                              <Button
                                                className="min-w-0 h-7 px-2.5"
                                                color="success"
                                                size="sm"
                                                startContent={
                                                  <ApproveIcon className="h-3 w-3" />
                                                }
                                                variant="flat"
                                                onPress={() =>
                                                  resolveApproval(
                                                    tc.id,
                                                    "always",
                                                  )
                                                }
                                              >
                                                {t("总是允许")}
                                              </Button>
                                              <Button
                                                className="min-w-0 h-7 px-2.5"
                                                color="danger"
                                                size="sm"
                                                startContent={
                                                  <RejectIcon className="h-3 w-3" />
                                                }
                                                variant="flat"
                                                onPress={() =>
                                                  resolveApproval(tc.id, false)
                                                }
                                              >
                                                {t("拒绝")}
                                              </Button>
                                            </div>
                                          )}

                                          {/* 展开的详情 */}
                                          {isExpanded && (
                                            <div className="border-t nya-border px-2.5 py-2 space-y-2">
                                              {/* 输入参数 */}
                                              <div>
                                                <div className="text-[11px] font-medium text-gray-400 mb-1">
                                                  {t("输入参数")}
                                                </div>
                                                <pre className="text-[11px] font-mono bg-default-100/60 rounded p-1.5 overflow-x-auto max-h-32 overflow-y-auto nya-scroll">
                                                  {tc.args || "—"}
                                                </pre>
                                              </div>
                                              {/* 输出结果 */}
                                              {tc.result !== undefined && (
                                                <div>
                                                  <div className="text-[11px] font-medium text-gray-400 mb-1">
                                                    {t("返回结果")}
                                                  </div>
                                                  <pre
                                                    className={`text-[11px] font-mono rounded p-1.5 overflow-x-auto max-h-40 overflow-y-auto nya-scroll ${
                                                      tc.status === "error"
                                                        ? "bg-danger/10 text-danger"
                                                        : "bg-default-100/60"
                                                    }`}
                                                  >
                                                    {tc.result || "—"}
                                                  </pre>
                                                </div>
                                              )}
                                            </div>
                                          )}
                                        </div>
                                      );
                                    })}
                                </div>
                              )}

                              <div className="mt-1 text-[11px] text-gray-400">
                                {formatTime(msg.timestamp)}
                              </div>
                            </div>
                          </div>
                        )}
                      </motion.div>
                    ))}
                </AnimatePresence>

                {/* 工具轮数上限暂停：一键继续 */}
                {continueTarget === activeSessionId && !isSending && (
                  <div className="mt-2 flex justify-center">
                    <Button
                      color="primary"
                      size="sm"
                      startContent={<SparkleIcon />}
                      variant="flat"
                      onPress={() => void continueTurn()}
                    >
                      {t("继续执行")}
                    </Button>
                  </div>
                )}

                {/* 正在生成中：流式内容或打字指示器 */}
                {isSending && (
                  <motion.div
                    animate={{ opacity: 1 }}
                    className="flex gap-3"
                    initial={{ opacity: 0 }}
                  >
                    <div className="flex size-8 flex-shrink-0 items-center justify-center rounded-full bg-gradient-to-br from-primary to-secondary text-primary-foreground">
                      <NekoAgentIcon className="h-4 w-4" />
                    </div>
                    <div className="max-w-[80%] rounded-lg rounded-tl-md border nya-border nya-panel px-4 py-2.5">
                      {isStreaming && streamingContent ? (
                        <div className="nya-markdown text-sm leading-relaxed">
                          <ReactMarkdown remarkPlugins={[remarkGfm]}>
                            {stripToolBlocks(streamingContent)}
                          </ReactMarkdown>
                          <span className="ml-0.5 inline-block w-1.5 h-4 bg-primary/60 animate-pulse align-middle" />
                        </div>
                      ) : isCompacting ? (
                        <div className="flex items-center gap-1.5 py-1 text-xs text-gray-400">
                          <SparkleIcon className="h-3.5 w-3.5 animate-pulse text-primary" />
                          {t("正在整理较早的对话…")}
                        </div>
                      ) : (
                        <div className="flex items-center gap-1.5 py-1">
                          {[0, 1, 2].map((i) => (
                            <motion.span
                              key={i}
                              animate={{
                                y: [0, -4, 0],
                                opacity: [0.4, 1, 0.4],
                              }}
                              className="size-2 rounded-full bg-primary"
                              transition={{
                                duration: 0.8,
                                repeat: Infinity,
                                delay: i * 0.15,
                              }}
                            />
                          ))}
                        </div>
                      )}
                    </div>
                  </motion.div>
                )}
              </div>
            )}
          </div>

          {/* 上翻阅读时浮出：一键回到底部 */}
          {showScrollToBottom && (
            <button
              aria-label={t("回到底部")}
              className="absolute bottom-4 right-6 z-10 flex size-9 items-center justify-center rounded-full border nya-border nya-panel text-gray-500 shadow-md transition-colors hover:text-primary"
              type="button"
              onClick={() => scrollToBottom(true)}
            >
              <ArrowDownIcon className="h-4 w-4" />
            </button>
          )}
        </div>

        <div className="flex-shrink-0 border-t nya-border p-4">
          <div className="mx-auto max-w-3xl">
            {/* 附件预览区 */}
            {pendingAttachments.length > 0 && (
              <div className="mb-2 flex flex-wrap gap-2">
                {pendingAttachments.map((att) => (
                  <div
                    key={att.id}
                    className="group relative flex items-center gap-2 rounded-lg border nya-border bg-default-100/50 px-2.5 py-1.5"
                  >
                    {att.isImage ? (
                      <img
                        alt={att.name}
                        className="h-8 w-8 rounded object-cover"
                        src={att.dataUrl}
                      />
                    ) : (
                      <AttachIcon className="h-4 w-4 text-gray-400" />
                    )}
                    <div className="flex flex-col">
                      <span className="max-w-[140px] truncate text-xs font-medium">
                        {att.name}
                      </span>
                      <span className="text-[11px] text-gray-400">
                        {formatFileSize(att.size)}
                      </span>
                    </div>
                    <Button
                      isIconOnly
                      className="absolute -right-1.5 -top-1.5 min-w-4 h-4 bg-danger text-white opacity-0 group-hover:opacity-100 transition-opacity"
                      size="sm"
                      variant="solid"
                      onPress={() => removeAttachment(att.id)}
                    >
                      <RejectIcon className="h-2.5 w-2.5" />
                    </Button>
                  </div>
                ))}
              </div>
            )}

            {/* 隐藏的文件输入 */}
            <input
              ref={fileInputRef}
              multiple
              accept="image/*,.pdf,.txt,.md,.json,.yaml,.yml,.toml,.cfg,.log,.zip,.jar"
              className="hidden"
              type="file"
              onChange={(e) => {
                handleFileSelect(e.target.files);
                e.target.value = "";
              }}
            />

            <div className="flex items-end gap-2 rounded-medium border nya-border nya-panel p-2 focus-within:border-primary/50">
              <Tooltip
                content={t("添加附件（图片/文件，单个最大 10MB）")}
                delay={300}
              >
                <Button
                  isIconOnly
                  className="flex-shrink-0 text-gray-400 hover:text-primary"
                  radius="full"
                  variant="light"
                  onPress={() => fileInputRef.current?.click()}
                >
                  <AttachIcon />
                </Button>
              </Tooltip>
              <Textarea
                ref={textareaRef}
                classNames={{
                  input: "min-h-[24px] max-h-[140px] resize-none py-1.5",
                  inputWrapper: "bg-transparent border-0 shadow-none",
                }}
                placeholder={t("输入消息... (Enter 发送，Shift+Enter 换行)")}
                radius="none"
                value={input}
                variant="flat"
                onKeyDown={handleKeyDown}
                onValueChange={setInput}
              />
              {isStreaming ? (
                <Button
                  isIconOnly
                  className="flex-shrink-0"
                  color="danger"
                  radius="full"
                  onPress={stopStreaming}
                >
                  <RejectIcon />
                </Button>
              ) : (
                <Button
                  isIconOnly
                  className="flex-shrink-0"
                  color="primary"
                  isDisabled={!input.trim() || isSending}
                  radius="full"
                  onPress={() => void sendMessage()}
                >
                  <SendIcon />
                </Button>
              )}
            </div>
            {/* 底部状态栏：权限提示 + 上下文使用 + 设置按钮 */}
            <div className="mt-2 flex items-center gap-3 px-1">
              {/* 权限提示 */}
              <span className="flex-shrink-0 text-[11px] text-gray-400">
                {!settings.allowFolderRead && !settings.allowFolderWrite
                  ? t("🚫 已禁止访问实例文件夹")
                  : settings.allowModify
                    ? t("⚠️ 可直接修改实例")
                    : t("🔒 需手动确认")}
              </span>

              {/* 分隔 */}
              <div className="h-3 w-px bg-default-200 flex-shrink-0" />

              {/* 上下文使用显示 */}
              <div className="flex flex-1 items-center gap-2 min-w-0">
                <DatabaseIcon className="h-3 w-3 flex-shrink-0 text-gray-400" />
                <div className="flex-1 h-1.5 bg-default-200 rounded-full overflow-hidden min-w-[60px]">
                  <div
                    className={`h-full rounded-full transition-all duration-300 ${
                      contextUsage.isOver
                        ? "bg-danger"
                        : contextUsage.isNear
                          ? "bg-warning"
                          : "bg-primary"
                    }`}
                    style={{ width: `${Math.min(100, contextUsage.percent)}%` }}
                  />
                </div>
                <span
                  className={`flex-shrink-0 text-[11px] tabular-nums ${
                    contextUsage.isOver
                      ? "text-danger"
                      : contextUsage.isNear
                        ? "text-warning"
                        : "text-gray-400"
                  }`}
                >
                  {contextUsage.usedTokens.toLocaleString()} /{" "}
                  {contextUsage.totalTokens.toLocaleString()} tokens
                </span>
                <span className="flex-shrink-0 text-[11px] text-gray-400">
                  · {contextUsage.messageCount} {t("条")}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* 设置弹窗（已拆到 components/ai/AiSettingsDialog.tsx） */}
      <AiSettingsDialog
        alwaysAllowedList={alwaysAllowedList}
        currentProvider={currentProvider}
        effectiveBaseUrl={effectiveBaseUrl}
        localProbe={localProbe}
        open={settingsOpen}
        settings={settings}
        onClose={() => setSettingsOpen(false)}
        onProviderChange={handleProviderChange}
        onReprobe={() => void runLocalProbe()}
        onResetAlwaysAllowed={resetAlwaysAllowed}
        onSave={() => {
          saveToStorage(STORAGE_KEYS.settings, settings);
          setSettingsOpen(false);
        }}
        onSettingsChange={setSettings}
      />
    </section>
  );
};

export default AiPage;
