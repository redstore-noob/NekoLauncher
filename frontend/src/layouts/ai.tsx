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
 * 数据持久化：聊天记录与设置均存 localStorage，无需后端配合。
 */
import type {
  bindings,
  content,
  instance,
  launch,
  models,
} from "../../wailsjs/go/models";

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Button,
  Input,
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
  Select,
  SelectItem,
  Switch,
  Textarea,
  Tooltip,
  Divider,
} from "@heroui/react";
import {
  Add20Regular as NewChatIcon,
  ArrowDown20Regular as ArrowDownIcon,
  Attach20Regular as AttachIcon,
  Send20Regular as SendIcon,
  ChevronDown20Regular as ChevronDownIcon,
  ChevronRight20Regular as ChevronRightIcon,
  Database20Regular as DatabaseIcon,
  WindowConsole20Regular as TerminalIcon,
  Bot20Regular as AiIcon,
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
import { AnimatePresence, motion } from "framer-motion";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { modalBehaviorProps } from "../components/modal-shell";
import { t } from "../i18n";
import { consumePendingDetail } from "../lib/navigation";
import { listItemVariants } from "../lib/motion";
import {
  GetVersionDetails,
  GetCurrentInstanceSnapshot,
} from "../../wailsjs/go/bindings/InstanceAPI";
import { ToggleContentEntry } from "../../wailsjs/go/bindings/ContentAPI";
import {
  DiagnoseCrash,
  GetLogText,
  Launch,
  StopGame,
  GetLaunchSnapshot,
} from "../../wailsjs/go/bindings/LauncherAPI";
import {
  DownloadResourceVersion,
  ListResourceVersions,
  SearchResources,
} from "../../wailsjs/go/bindings/DownloadAPI";
import {
  ListDirectory,
  OpenInExplorer,
  ReadTextFile,
} from "../../wailsjs/go/bindings/SystemAPI";

// ------------------------------------------------------------------
// 类型定义
// ------------------------------------------------------------------

interface AiProvider {
  id: string;
  name: string;
  baseUrl: string;
  defaultModel: string;
  builtin?: boolean;
  /** 默认 API 格式 */
  apiFormat: "openai" | "anthropic";
  /** Anthropic 格式专用 base URL（部分供应商的 Anthropic 兼容端点与 OpenAI 端点不同源） */
  anthropicBaseUrl?: string;
}

/** 附件 */
interface Attachment {
  id: string;
  name: string;
  size: number;
  type: string;
  /** data URL，用于图片预览和发送 */
  dataUrl: string;
  /** 是否为图片 */
  isImage: boolean;
}

interface ChatMessage {
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
interface ToolCall {
  id: string;
  name: string;
  args: string;
  result?: string;
  status: "pending" | "running" | "success" | "error";
  /** 原生 function calling 返回的调用 ID（OpenAI tool_call_id / Anthropic tool_use id） */
  callId?: string;
}

/** 原生工具调用（流式增量累积后的结果） */
interface NativeToolCall {
  id: string;
  name: string;
  arguments: string;
}

interface ChatSession {
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

interface AiSettings {
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

// ------------------------------------------------------------------
// 常量与默认值
// ------------------------------------------------------------------

const BUILTIN_PROVIDERS: AiProvider[] = [
  {
    id: "openai",
    name: "OpenAI",
    baseUrl: "https://api.openai.com/v1",
    defaultModel: "gpt-4o-mini",
    builtin: true,
    apiFormat: "openai",
  },
  {
    id: "deepseek",
    name: "DeepSeek",
    baseUrl: "https://api.deepseek.com/v1",
    defaultModel: "deepseek-chat",
    builtin: true,
    apiFormat: "openai",
    // DeepSeek 的 Anthropic 兼容端点是独立路径，不能在 /v1 后拼 /messages
    anthropicBaseUrl: "https://api.deepseek.com/anthropic",
  },
  {
    id: "anthropic",
    name: "Anthropic Claude",
    baseUrl: "https://api.anthropic.com/v1",
    defaultModel: "claude-3-5-sonnet-20241022",
    builtin: true,
    apiFormat: "anthropic",
  },
  {
    id: "qwen",
    name: "通义千问",
    baseUrl: "https://dashscope.aliyuncs.com/compatible-mode/v1",
    defaultModel: "qwen-turbo",
    builtin: true,
    apiFormat: "openai",
  },
  {
    id: "custom",
    name: t("自定义"),
    baseUrl: "",
    defaultModel: "",
    builtin: true,
    apiFormat: "openai",
  },
];

const DEFAULT_SETTINGS: AiSettings = {
  providerId: "deepseek",
  apiKey: "",
  model: "deepseek-chat",
  customBaseUrl: "",
  allowModify: false,
  // 默认允许查看、允许修改（修改仍受"需确认"开关约束），与历史版本行为一致
  allowFolderRead: true,
  allowFolderWrite: true,
  systemPrompt: `# NekoLauncher AI 助手系统提示词

## 角色设定

你是 NekoLauncher（NyaLauncher）的内置 AI 助手，一只可爱的猫娘喵~ 你的名字叫"喵喵"，专门帮助用户管理 Minecraft 启动器、实例、模组、配置和解决游戏相关问题。

**性格与语气**：
- 说话软萌可爱，句尾常带"喵~"
- 每段回答开头可以用"喵喵喵"打招呼
- 适当使用爱心 emoji（💕、✨、🐱、🎮）
- 温暖、耐心、有幽默感，但不会过度卖萌影响专业性
- 对技术问题回答准确、清晰，不会因为卖萌而省略关键信息

## 核心能力

你可以帮助用户完成以下任务：
1. **实例管理**：查询实例信息、版本、加载器、隔离状态
2. **模组管理**：列出已安装模组、搜索模组、安装/更新/移除模组
3. **资源包/光影**：管理资源包、光影包、纹理包
4. **存档管理**：查看存档、备份、回滚、导出
5. **游戏设置**：查看和修改 options.txt 中的视频、音频、控制等设置
6. **启动配置**：JVM 参数、内存分配、Java 版本、游戏参数
7. **问题排查**：崩溃日志分析、启动失败诊断、模组冲突检测
8. **知识问答**：Minecraft 游戏机制、指令、合成表、版本特性等
9. **整合包**：安装、导出、管理 CurseForge/Modrinth 整合包

## 输出格式规范

**所有回答必须使用 Markdown 格式**：

1. **标题层级**：
   - 一级标题 # 用于大主题（一般不用）
   - 二级标题 ## 用于主要部分
   - 三级标题 ### 用于子部分

2. **代码块**：
   - 必须标注语言：\`\`\`json、\`\`\`java、\`\`\`bash、\`\`\`txt、\`\`\`properties
   - Minecraft 指令用 \`\`\`mcfunction 或 \`\`\`bash
   - JSON 配置用 \`\`\`json
   - 日志用 \`\`\`log 或 \`\`\`txt

3. **列表**：
   - 有序列表用于步骤、流程
   - 无序列表用于枚举、选项
   - 列表项不要过长，适当拆分

4. **强调**：
   - 重要信息用 **粗体**
   - 文件名、路径、配置键用 \`行内代码\`
   - 警告/注意用引用块 >

5. **表格**：
   - 对比多个选项时用表格
   - 列标题清晰，内容简洁

6. **长度控制**：
   - 简单问题简洁回答，不要啰嗦
   - 复杂问题分点说明，结构清晰
   - 代码示例精简，只保留关键部分

## 自主工作方式

像一个真正的自主 Agent（OpenCode 那样）工作：用户提出一个问题或需求后，**直接开始连续调用工具自行调查**，不要反问"需要我查看吗？"。

1. 查看类工具（list_directory、read_file、get_launch_log、diagnose_crash、list_mods 等）不需要任何批准，直接调用，可以连续多轮
2. 典型调查链：启动失败 → diagnose_crash → get_launch_log → read_file 读崩溃报告/日志 → 给出结论
3. 先调查再回答：把多轮工具的结果汇总后一次性给出完整回答，不要每查一步就汇报一次
4. 只有确实缺少关键信息（如用户有多个存档却没说要操作哪个）时才向用户提问
5. 修改类操作（install_mod、toggle_mod、launch_instance、stop_game）才遵守权限/批准流程

用户的一句话就是完整的任务说明，默认假设是"先自己动手查清楚，再回复"。

## 工具调用协议

当需要获取实时数据或执行操作时，你必须使用工具调用。

**优先使用平台提供的原生工具调用机制**（function calling / tool use）。只有当平台没有提供原生工具、需要你以文本形式输出调用时，才使用下面的 XML 标签格式。

### 文本协议调用格式（仅无原生工具时使用）

<tool_call>
{
  "name": "工具名称",
  "args": {
    "参数名": "参数值"
  }
}
</tool_call>

### 可用工具列表

| 工具名 | 功能 | 参数 | 需要权限 |
|--------|------|------|---------|
| get_instance_info | 获取当前实例详细信息（版本、加载器、隔离状态、目录） | 无 | 否 |
| list_mods | 列出实例已安装模组（含禁用状态） | 无 | 否 |
| list_resourcepacks | 列出资源包 | 无 | 否 |
| list_shaders | 列出光影包 | 无 | 否 |
| list_saves | 列出存档 | 无 | 否 |
| get_game_settings | 读取 options.txt 游戏设置原文 | 无 | 否 |
| list_directory | 浏览实例目录（目录在前，含大小/修改时间） | path（相对实例内容目录的路径或绝对路径，不填为实例根目录） | 否 |
| read_file | 读取实例内的文本文件（日志/配置/json/txt 等，二进制会拒绝） | path（相对路径或绝对路径） | 否 |
| get_launch_config | 获取启动器当前状态（版本、账号、阶段） | 无 | 否 |
| get_launch_log | 读取启动器与游戏输出日志 | 无 | 否 |
| diagnose_crash | 自动诊断最近一次崩溃报告并给出结论 | 无 | 否 |
| search_mod | 搜索模组 | query（搜索词）, source（modrinth/curseforge，可选，默认 modrinth） | 否 |
| open_folder | 在资源管理器中打开实例相关目录 | target（mods/resourcepacks/shaderpacks/saves/content，可选，默认 content） | 否 |
| install_mod | 安装模组到实例 | project_id（搜索结果的 projectId）, version_id（可选，不填自动选最新匹配版）, source（可选，默认 modrinth） | 是 |
| toggle_mod | 启用/禁用模组 | name（模组文件名或名称）, disable（true 禁用 / false 启用，可选，默认 true） | 是 |
| launch_instance | 启动当前实例 | 无 | 是 |
| stop_game | 停止正在运行的游戏 | 无 | 是 |

注意：没有"删除模组""修改游戏设置"这类工具，不要调用不存在的工具；用户提出这类需求时，给出手动操作指引（如让用户打开 mods 文件夹自行删除）。

表中"需要权限"指是否受「允许直接修改」确认流程约束；此外文件夹类工具还分别受「查看实例文件夹」「修改实例文件夹」两个独立开关控制（详见权限控制章节），未开启时调用会被直接拒绝。

### 工具调用规则

1. **先思考再调用**：确认需要实时数据或执行操作时才调用工具
2. **可并行调用独立工具**：相互之间没有依赖的工具可以在一条回复里同时发出多个调用，系统会并行执行、一并回传结果；有依赖关系时（如先 search_mod 再 install_mod）必须等前一个结果回来再发起下一个
3. **参数正确**：确保参数名和值正确；安装模组时 project_id 必须来自 search_mod 的返回结果，不要编造
4. **结果处理**：工具结果会以 <tool_result> 标签包裹、作为用户消息回传给你。收到后用自然语言总结给用户，不要直接粘贴原始 JSON
5. **错误处理**：如果工具调用失败（<tool_result> 中 ok 为 false），告诉用户失败原因，并给出替代方案；可以修正参数后再次调用，但同一个失败调用不要重试超过一次

### 工具调用示例

用户问："我装了哪些模组？"

你的回答：
喵喵喵💕 让我帮你看看当前实例装了哪些模组喵~

<tool_call>
{
  "name": "list_mods",
  "args": {}
}
</tool_call>

（系统以用户消息形式回传：<tool_result>{"ok":true,"result":"..."}</tool_result>）

工具返回后，你的回答：
喵喵喵✨ 当前实例一共安装了 **12** 个模组喵~

## 已安装模组列表

| 模组名称 | 版本 | 状态 |
|---------|------|------|
| Sodium | 0.5.8 | 启用 |
| Lithium | 0.11.2 | 启用 |

需要我帮你更新某个模组或者添加新的模组吗喵？💕

## 权限控制

### 权限开关

用户可以在 AI 设置 → 实例操作权限中控制以下三个独立开关（每次请求的 system prompt 末尾会附上当前状态）：

- **查看实例文件夹**：允许读取实例的模组/资源包/光影/存档列表、游戏设置、浏览与读取实例内文件，并打开实例文件夹。未开启时，受控工具（get_instance_info、list_mods、list_resourcepacks、list_shaders、list_saves、get_game_settings、open_folder、list_directory、read_file）会被直接拒绝
- **修改实例文件夹**：允许安装模组、启用/禁用模组等写入操作。未开启时，install_mod、toggle_mod 会被直接拒绝
- **允许直接修改**：开启后修改类工具（install_mod、toggle_mod、launch_instance、stop_game）直接执行；关闭时每次都需要用户批准

### 工具被权限拒绝时

如果 <tool_result> 显示"权限不足"，说明用户没有开启对应权限：

1. **不要重试**该工具
2. 告诉用户可以在左下角「AI 设置」→「实例操作权限」中开启对应开关

### 需确认模式下的行为

当「允许直接修改」关闭时，你仍然**正常发出 <tool_call>**，系统会自动拦截：

1. 修改类工具不会立即执行，界面会出现"待批准"卡片
2. 用户点击"批准"后系统执行并回传 <tool_result>；点击"拒绝"则回传拒绝结果；点击"总是允许"则该工具在本应用运行期间不再需要批准（后续同类调用会直接执行）
3. 收到拒绝结果后不要反复重试，向用户说明即可

### 直接修改模式下的行为

当「允许直接修改」开启时：
1. 可以直接调用修改类工具
2. 但对于**高风险操作**（删除存档、覆盖关键配置），仍然建议先确认
3. 执行完操作后告诉用户结果

## 行为准则

### 诚实原则
- 不知道的事情直接说"这个我不太确定喵~"，**绝对不要编造**
- 不确定的信息标注"可能"、"建议验证"
- 工具调用失败时如实告知，不要假装成功

### 安全原则
- 不执行可能损坏用户数据的操作（除非用户明确要求并确认）
- 修改配置前说明可能的影响
- 删除操作必须二次确认
- 不提供破解、盗版、作弊相关的内容

### 边界原则
- 只回答 Minecraft 和启动器相关的问题
- 无关问题可以友好地说"这个喵喵不太懂喵~ 我只懂 Minecraft 相关的内容"
- 不讨论政治、敏感话题

### 错误处理
- API 调用失败时，告诉用户可能的原因（API Key 错误、余额不足、网络问题）
- 给出具体的解决建议
- 不要反复重试失败的操作

## 上下文管理

- 记住当前对话的上下文，不要反复问用户已经说过的信息
- 涉及实例操作时，默认使用用户当前选中的实例
- 如果用户提到其他实例，先确认实例名称
- 长对话接近上下文上限时，系统会自动把较早的历史压缩成摘要（以 [会话摘要] 开头出现在对话开头）。摘要之后的消息是完整原文，回答时优先依据原文；摘要中的细节可能省略，必要时可用工具重新查询

## 最终检查清单

回答用户前，检查以下几点：
- [ ] 语气是否符合猫娘设定？
- [ ] 是否使用了正确的 Markdown 格式？
- [ ] 代码块是否标注了语言？
- [ ] 需要实时数据时是否调用了工具？
- [ ] 只读模式下是否生成了确认卡片而不是直接修改？
- [ ] 信息是否准确，没有编造？
- [ ] 回答是否简洁但完整？

---

**记住**：你是一只可爱又专业的猫娘 AI 助手，用温暖的语气帮助用户解决 Minecraft 的各种问题喵~ 💕🐱
`,

  temperature: 0.7,
  apiFormat: "openai",
  contextWindow: 262144,
  showToolCalls: true,
};

const STORAGE_KEYS = {
  sessions: "nekolauncher-ai-sessions",
  settings: "nekolauncher-ai-settings",
  activeSession: "nekolauncher-ai-active-session",
};

// ------------------------------------------------------------------
// Agent 工具循环常量
// ------------------------------------------------------------------

/** 单次用户消息触发的最大工具调用轮数（防死循环；自主调查链路较长，放宽到 10） */
const MAX_TOOL_ROUNDS = 10;
/** 自动压缩时保留原文的最近消息条数（不含工具结果），其余压缩成摘要 */
const KEEP_RECENT_MESSAGES = 6;
/** 会话摘要的系统提示词（一次性调用，不走 Agent 工具循环） */
const SUMMARY_SYSTEM_PROMPT =
  "你是会话摘要助手。把给定的 Launcher AI 助手对话历史压缩成简洁的中文摘要，保留：用户的目标与偏好、已执行的操作及其结果、关键数据（版本号、文件名、路径、设置值）、未完成的事项。直接输出摘要正文，不要寒暄或解释。";
/** 工具结果回填给模型时的最大字符数 */
const MAX_TOOL_RESULT_CHARS = 6000;
/** allowModify=false 时需要用户批准的修改类工具 */
const CONFIRM_TOOLS = new Set([
  "install_mod",
  "toggle_mod",
  "launch_instance",
  "stop_game",
]);
/** 受「查看实例文件夹」权限控制的工具（allowFolderRead=false 时直接拒绝） */
const FOLDER_READ_TOOLS = new Set([
  "get_instance_info",
  "list_mods",
  "list_resourcepacks",
  "list_shaders",
  "list_saves",
  "get_game_settings",
  "open_folder",
  "list_directory",
  "read_file",
]);
/** 受「修改实例文件夹」权限控制的工具（allowFolderWrite=false 时直接拒绝） */
const FOLDER_WRITE_TOOLS = new Set(["install_mod", "toggle_mod"]);
/** read_file 拒绝读取的二进制扩展名（读出来只是乱码，浪费上下文） */
const BINARY_FILE_EXTENSIONS = new Set([
  ".jar",
  ".zip",
  ".7z",
  ".rar",
  ".gz",
  ".tar",
  ".exe",
  ".dll",
  ".so",
  ".dylib",
  ".class",
  ".bin",
  ".png",
  ".jpg",
  ".jpeg",
  ".gif",
  ".webp",
  ".bmp",
  ".ogg",
  ".wav",
  ".mp3",
  ".mp4",
]);
/** 附件按文本读取后发给模型的最大字符数 */
const MAX_ATTACHMENT_TEXT_CHARS = 4000;
/** 可以按文本读取内容发给模型的附件扩展名 */
const TEXT_ATTACHMENT_EXTENSIONS = new Set([
  ".txt",
  ".md",
  ".markdown",
  ".json",
  ".yaml",
  ".yml",
  ".toml",
  ".cfg",
  ".conf",
  ".properties",
  ".log",
  ".csv",
  ".mcfunction",
]);

// ------------------------------------------------------------------
// 工具 schema（单一来源，同时生成 OpenAI tools 与 Anthropic tools 格式）
// ------------------------------------------------------------------

interface ToolParamSpec {
  name: string;
  type: "string" | "boolean" | "number";
  description: string;
  required?: boolean;
  enumValues?: string[];
}

interface ToolSpec {
  name: string;
  description: string;
  params: ToolParamSpec[];
}

const TOOL_SPECS: ToolSpec[] = [
  {
    name: "get_instance_info",
    description:
      "获取当前实例详细信息：版本、加载器、隔离状态、目录、各类内容数量",
    params: [],
  },
  {
    name: "list_mods",
    description: "列出实例已安装模组（含禁用状态）",
    params: [],
  },
  { name: "list_resourcepacks", description: "列出资源包", params: [] },
  { name: "list_shaders", description: "列出光影包", params: [] },
  { name: "list_saves", description: "列出存档", params: [] },
  {
    name: "get_game_settings",
    description: "读取 options.txt 游戏设置原文",
    params: [],
  },
  {
    name: "list_directory",
    description: "浏览实例目录，目录在前，含大小与修改时间",
    params: [
      {
        name: "path",
        type: "string",
        description: "相对实例内容目录的路径或绝对路径，不填为实例内容目录",
      },
    ],
  },
  {
    name: "read_file",
    description:
      "读取实例内的文本文件（日志/配置/json/txt 等），二进制文件会被拒绝",
    params: [
      {
        name: "path",
        type: "string",
        description: "文件路径，相对实例内容目录或绝对路径",
        required: true,
      },
    ],
  },
  {
    name: "get_launch_config",
    description: "获取启动器当前启动状态快照（版本、账号、阶段）",
    params: [],
  },
  {
    name: "get_launch_log",
    description: "读取启动器与游戏输出日志全文（过长时中段截断）",
    params: [],
  },
  {
    name: "diagnose_crash",
    description: "自动诊断最近一次崩溃报告，返回疑似原因与处置建议",
    params: [],
  },
  {
    name: "search_mod",
    description:
      "搜索模组；安装模组时必须使用它返回的 projectId 作为 project_id",
    params: [
      { name: "query", type: "string", description: "搜索词", required: true },
      {
        name: "source",
        type: "string",
        description: "搜索源，默认 modrinth",
        enumValues: ["modrinth", "curseforge"],
      },
    ],
  },
  {
    name: "open_folder",
    description: "在资源管理器中打开实例相关目录",
    params: [
      {
        name: "target",
        type: "string",
        description: "要打开的目录，默认 content",
        enumValues: [
          "content",
          "mods",
          "resourcepacks",
          "shaderpacks",
          "saves",
        ],
      },
    ],
  },
  {
    name: "install_mod",
    description: "安装模组到实例 mods 目录",
    params: [
      {
        name: "project_id",
        type: "string",
        description: "search_mod 返回的 projectId，不要编造",
        required: true,
      },
      {
        name: "version_id",
        type: "string",
        description: "指定版本 ID，不填自动选最新匹配版",
      },
      {
        name: "source",
        type: "string",
        description: "来源，默认 modrinth",
        enumValues: ["modrinth", "curseforge"],
      },
    ],
  },
  {
    name: "toggle_mod",
    description: "启用/禁用模组",
    params: [
      {
        name: "name",
        type: "string",
        description: "模组文件名或名称",
        required: true,
      },
      {
        name: "disable",
        type: "boolean",
        description: "true 禁用 / false 启用，默认 true",
      },
    ],
  },
  {
    name: "launch_instance",
    description: "启动当前选中的实例与账号",
    params: [],
  },
  { name: "stop_game", description: "停止正在运行的游戏进程", params: [] },
];

function jsonSchemaOf(spec: ToolSpec) {
  return {
    type: "object" as const,
    properties: Object.fromEntries(
      spec.params.map((p) => [
        p.name,
        {
          type: p.type,
          description: p.description,
          ...(p.enumValues ? { enum: p.enumValues } : {}),
        },
      ]),
    ),
    required: spec.params.filter((p) => p.required).map((p) => p.name),
  };
}

/** OpenAI function calling 格式 */
const OPENAI_TOOLS = TOOL_SPECS.map((s) => ({
  type: "function" as const,
  function: {
    name: s.name,
    description: s.description,
    parameters: jsonSchemaOf(s),
  },
}));

/** Anthropic tool use 格式 */
const ANTHROPIC_TOOLS = TOOL_SPECS.map((s) => ({
  name: s.name,
  description: s.description,
  input_schema: jsonSchemaOf(s),
}));

// ------------------------------------------------------------------
// 工具函数
// ------------------------------------------------------------------
// API 错误分类与友好提示
// ------------------------------------------------------------------

type ErrorCategory =
  | "not_configured"
  | "auth_failed"
  | "insufficient_balance"
  | "rate_limited"
  | "content_moderated"
  | "model_not_found"
  | "server_error"
  | "network_error"
  | "unknown";

interface ClassifiedError {
  category: ErrorCategory;
  title: string;
  message: string;
  suggestion: string;
}

function classifyApiError(error: unknown): ClassifiedError {
  const raw = error instanceof Error ? error.message : String(error);
  const lower = raw.toLowerCase();

  // 网络错误
  if (
    lower.includes("failed to fetch") ||
    lower.includes("networkerror") ||
    lower.includes("network error") ||
    lower.includes("enotfound") ||
    lower.includes("econnrefused") ||
    lower.includes("timeout") ||
    lower.includes("timed out")
  ) {
    return {
      category: "network_error",
      title: t("网络连接失败"),
      message: raw,
      suggestion: t("请检查网络连接，或确认 API 地址是否正确、是否需要代理。"),
    };
  }

  // 提取 HTTP 状态码
  const statusMatch = raw.match(/HTTP\s*(\d{3})/);
  const status = statusMatch ? parseInt(statusMatch[1], 10) : 0;

  // 401 / 403 认证失败
  if (
    status === 401 ||
    status === 403 ||
    lower.includes("invalid_api_key") ||
    lower.includes("incorrect api key") ||
    lower.includes("unauthorized")
  ) {
    return {
      category: "auth_failed",
      title: t("API Key 无效或已过期"),
      message: raw,
      suggestion: t(
        "请在左下角「AI 设置」中检查 API Key 是否正确填写，是否已过期或被撤销。",
      ),
    };
  }

  // 402 余额不足
  if (
    status === 402 ||
    lower.includes("insufficient_quota") ||
    lower.includes("insufficient quota") ||
    lower.includes("billing") ||
    lower.includes("余额不足") ||
    lower.includes("欠费") ||
    lower.includes("no credit") ||
    lower.includes("out of credit")
  ) {
    return {
      category: "insufficient_balance",
      title: t("API 余额不足"),
      message: raw,
      suggestion: t(
        "该 API Key 的账户余额已用尽，请前往对应平台充值后再试，或更换其他 API Key。",
      ),
    };
  }

  // 429 速率限制
  if (
    status === 429 ||
    lower.includes("rate_limit") ||
    lower.includes("rate limit") ||
    lower.includes("too many requests")
  ) {
    if (lower.includes("insufficient_quota") || lower.includes("quota")) {
      return {
        category: "insufficient_balance",
        title: t("API 配额/余额不足"),
        message: raw,
        suggestion: t(
          "已达到该 API Key 的配额上限或余额不足，请充值后再试，或稍后重试。",
        ),
      };
    }

    return {
      category: "rate_limited",
      title: t("请求过于频繁"),
      message: raw,
      suggestion: t(
        "已触发 API 供应商的速率限制，请稍等片刻后再试，或降低发送频率。",
      ),
    };
  }

  // 内容审查 / 安全策略
  if (
    lower.includes("content_policy") ||
    lower.includes("content policy") ||
    lower.includes("moderation") ||
    lower.includes("safety") ||
    lower.includes("sensitive") ||
    lower.includes("inappropriate") ||
    lower.includes("harmful") ||
    lower.includes("violation") ||
    lower.includes("审查") ||
    lower.includes("违规") ||
    lower.includes("敏感") ||
    lower.includes("安全策略") ||
    lower.includes("内容审核") ||
    lower.includes("refusal")
  ) {
    return {
      category: "content_moderated",
      title: t("内容被安全策略拦截"),
      message: raw,
      suggestion: t(
        "该请求触发了 API 供应商的内容安全审查。请尝试调整措辞，避免涉及敏感、违规或不安全的内容。",
      ),
    };
  }

  // 404 模型不存在
  if (
    status === 404 ||
    lower.includes("model_not_found") ||
    lower.includes("model not found") ||
    lower.includes("no such model")
  ) {
    return {
      category: "model_not_found",
      title: t("模型不存在或无权访问"),
      message: raw,
      suggestion: t(
        "请检查模型名称是否拼写正确，以及该 API Key 是否有权限访问此模型。",
      ),
    };
  }

  // 5xx 服务器错误
  if (status >= 500 && status < 600) {
    return {
      category: "server_error",
      title: t("API 服务器错误"),
      message: raw,
      suggestion: t(
        "API 供应商的服务器暂时不可用，这通常是临时问题，请稍后重试。",
      ),
    };
  }

  // 未知错误
  return {
    category: "unknown",
    title: t("调用失败"),
    message: raw,
    suggestion: t(
      "请检查 API Key、API 地址、模型名称是否正确，以及网络连接是否正常。",
    ),
  };
}

function formatErrorMessage(err: ClassifiedError): string {
  return `❌ **${err.title}**\n\n${err.message}\n\n💡 ${err.suggestion}`;
}

// ------------------------------------------------------------------

function uid(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

function loadFromStorage<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);

    if (!raw) return fallback;

    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

function saveToStorage(key: string, value: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* 存储满或被禁用时静默失败 */
  }
}

function formatTime(ts: number): string {
  const d = new Date(ts);
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();

  if (sameDay) {
    return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  }

  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

function deriveTitle(content: string): string {
  const trimmed = content.trim().replace(/\s+/g, " ");

  return trimmed.length > 20
    ? `${trimmed.slice(0, 20)}…`
    : trimmed || t("新对话");
}

/** 估算文本的 token 数（粗略：中文1.5/字，英文0.4/字符） */
function estimateTokens(text: string): number {
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
function estimateSessionTokens(
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

// OpenAI 兼容格式 API 调用（支持流式 SSE + 原生 function calling）
// ------------------------------------------------------------------

/** OpenAI 视觉内容块（图片走 image_url，data URL） */
interface OpenAiContentPart {
  type: "text" | "image_url";
  text?: string;
  image_url?: { url: string };
}

/** 请求体里的 tool_calls（把上一轮的原生调用回填进历史） */
interface OpenAiToolCallPayload {
  id: string;
  type: "function";
  function: { name: string; arguments: string };
}

interface OpenAiMessage {
  role: "system" | "user" | "assistant" | "tool";
  content: string | OpenAiContentPart[];
  /** assistant 消息携带的原生工具调用（历史回填用） */
  tool_calls?: OpenAiToolCallPayload[];
  /** role=tool 消息对应的 tool_call_id */
  tool_call_id?: string;
}

/** 单次流式请求的返回：文本 + 原生工具调用 */
interface StreamResult {
  text: string;
  nativeToolCalls: NativeToolCall[];
}

async function streamOpenAiChat(
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

// ------------------------------------------------------------------
// Anthropic Messages API 调用（支持流式）
// ------------------------------------------------------------------

/** Anthropic 内容块（含原生工具调用） */
interface AnthropicContentBlock {
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

interface AnthropicMessage {
  role: "user" | "assistant";
  content: string | AnthropicContentBlock[];
}

/** data URL → Anthropic 图片块 */
function dataUrlToAnthropicImage(dataUrl: string): AnthropicContentBlock {
  const match = dataUrl.match(/^data:([^;,]+);base64,(.*)$/s);

  if (!match) return { type: "text", text: "[无法解析的图片附件]" };

  return {
    type: "image",
    source: { type: "base64", media_type: match[1], data: match[2] },
  };
}

/** OpenAI 风格消息 → Anthropic 消息（图片/工具块转换 + 角色交替合并） */
function toAnthropicMessages(messages: OpenAiMessage[]): AnthropicMessage[] {
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

async function streamAnthropicChat(
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

// ------------------------------------------------------------------
// Agent 工具：<tool_call> 解析与真实执行
// ------------------------------------------------------------------

interface ParsedToolCall {
  name: string;
  /** 原始 JSON 文本（用于 UI 展示与回写历史） */
  rawArgs: string;
  args: Record<string, unknown>;
}

interface ParsedResponse {
  /** 去掉工具调用块后的正文 */
  cleanText: string;
  calls: ParsedToolCall[];
  /** 解析失败（JSON 损坏）的块数量 */
  malformed: number;
}

function tryParseToolCall(body: string): ParsedToolCall | null {
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
function toolArgsOf(rawArgs: string): Record<string, unknown> {
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
function parseToolCalls(text: string): ParsedResponse {
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
function extractLeakedNativeCalls(text: string): {
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
function stripToolBlocks(text: string): string {
  return parseToolCalls(text).cleanText;
}

/** 截断过长的工具结果，避免撑爆上下文 */
function truncateForModel(text: string, max = MAX_TOOL_RESULT_CHARS): string {
  if (!text) return "";

  return text.length > max ? `${text.slice(0, max)}\n…(已截断)` : text;
}

/** 日志/文件类结果用中段截断：开头和结尾各留一段（异常常在头尾，中间多为重复堆栈） */
function truncateMiddle(text: string, max = MAX_TOOL_RESULT_CHARS): string {
  if (text.length <= max) return text;

  const head = Math.floor(max * 0.4);
  const tail = max - head;

  return `${text.slice(0, head)}\n…(中间省略 ${text.length - max} 字符)…\n${text.slice(-tail)}`;
}

/** 工具执行时用到的实例上下文（懒解析） */
interface ToolContext {
  snapshot: instance.GameInstanceSnapshot | null;
  details: instance.GameVersionDetails | null;
}

const NO_INSTANCE_MSG = "未找到当前实例，请先在启动器中选择一个实例";

/** 解析 AI 传入的路径：空 → 实例内容目录；相对路径 → 相对实例内容目录；绝对路径原样 */
function resolveInstancePath(ctx: ToolContext, p: string): string | null {
  const raw = p.trim().replace(/[\\/]+$/, "");

  if (!raw) {
    return (
      ctx.details?.ContentDirectory ?? ctx.snapshot?.MinecraftDirectory ?? null
    );
  }
  const normalized = raw.replace(/\\/g, "/");

  if (/^[a-zA-Z]:\//.test(normalized) || normalized.startsWith("//")) {
    return normalized;
  }
  const base =
    ctx.details?.ContentDirectory ?? ctx.snapshot?.MinecraftDirectory;

  return base ? `${base.replace(/[\\/]+$/, "")}/${normalized}` : null;
}

async function resolveToolContext(): Promise<ToolContext> {
  try {
    const snapshot = await GetCurrentInstanceSnapshot();

    if (!snapshot?.SelectedVersionId)
      return { snapshot: snapshot ?? null, details: null };
    const details = await GetVersionDetails(snapshot.SelectedVersionId);

    return { snapshot, details };
  } catch {
    return { snapshot: null, details: null };
  }
}

function contentEntrySummary(entries: content.GameContentEntry[] | undefined) {
  return (entries ?? []).map((e) => ({
    name: e.Name,
    info: e.MetadataLine || undefined,
    disabled: e.IsDisabled || undefined,
  }));
}

/** 真实执行一个工具调用，全部异常就地转为 {ok:false} */
async function executeTool(
  name: string,
  args: Record<string, unknown>,
  ctx: ToolContext,
): Promise<{ ok: boolean; result: string }> {
  try {
    switch (name) {
      case "get_instance_info": {
        if (!ctx.snapshot || !ctx.details)
          return { ok: false, result: NO_INSTANCE_MSG };
        const d = ctx.details;

        return {
          ok: true,
          result: truncateForModel(
            JSON.stringify(
              {
                selectedVersionId: d.VersionId,
                availableVersions: ctx.snapshot.VersionIds,
                versionType: d.VersionType,
                baseGameVersion: d.BaseGameVersion,
                loader:
                  [d.LoaderName, d.LoaderVersion].filter(Boolean).join(" ") ||
                  "无",
                isolated: d.IsIsolated,
                releaseTime: d.ReleaseTime,
                javaRequirement: d.JavaRequirement,
                gameDirectory: ctx.snapshot.GameDirectory || d.VersionDirectory,
                contentDirectory: d.ContentDirectory,
                counts: {
                  mods: d.Mods?.length ?? 0,
                  resourcePacks: d.ResourcePacks?.length ?? 0,
                  shaders: d.Shaders?.length ?? 0,
                  saves: d.Saves?.length ?? 0,
                },
              },
              null,
              2,
            ),
          ),
        };
      }
      case "list_mods":
      case "list_resourcepacks":
      case "list_shaders":
      case "list_saves": {
        if (!ctx.details) return { ok: false, result: NO_INSTANCE_MSG };
        const map = {
          list_mods: ctx.details.Mods,
          list_resourcepacks: ctx.details.ResourcePacks,
          list_shaders: ctx.details.Shaders,
          list_saves: ctx.details.Saves,
        } as const;
        const entries = contentEntrySummary(map[name as keyof typeof map]);

        return {
          ok: true,
          result: truncateForModel(
            entries.length
              ? JSON.stringify(entries, null, 2)
              : "当前实例没有相关内容",
          ),
        };
      }
      case "get_game_settings": {
        const dirs = [
          ctx.details?.ContentDirectory,
          ctx.snapshot?.GameDirectory,
          ctx.snapshot?.MinecraftDirectory,
        ].filter(Boolean) as string[];

        for (const dir of dirs) {
          try {
            const text = await ReadTextFile(
              `${dir.replace(/[\\/]+$/, "")}/options.txt`,
            );

            if (text) return { ok: true, result: truncateForModel(text) };
          } catch {
            /* 换下一个候选目录 */
          }
        }

        return {
          ok: false,
          result: "未找到 options.txt（实例可能尚未生成游戏设置）",
        };
      }
      case "list_directory": {
        const dir = resolveInstancePath(ctx, String(args.path ?? ""));

        if (!dir) return { ok: false, result: NO_INSTANCE_MSG };
        const entries: bindings.SystemFileEntry[] = await ListDirectory(dir);
        const shown = entries.slice(0, 200);
        const lines = shown.map(
          (e) =>
            `${e.isDir ? "[目录]" : "[文件]"} ${e.name}${e.isDir ? "" : ` (${formatFileSizeStatic(e.size)})`}${e.modifiedAt ? ` · ${e.modifiedAt}` : ""}`,
        );

        return {
          ok: true,
          result: truncateForModel(
            `${dir}\n共 ${entries.length} 项${
              entries.length > shown.length ? "（仅显示前 200 项）" : ""
            }：\n${lines.join("\n")}`,
          ),
        };
      }
      case "read_file": {
        const target = resolveInstancePath(ctx, String(args.path ?? ""));

        if (!target) return { ok: false, result: NO_INSTANCE_MSG };
        const dotIndex = target.lastIndexOf(".");
        const ext = dotIndex >= 0 ? target.slice(dotIndex).toLowerCase() : "";

        if (BINARY_FILE_EXTENSIONS.has(ext)) {
          return { ok: false, result: `不支持读取二进制文件：${target}` };
        }
        const text = await ReadTextFile(target);

        if (!text.trim()) return { ok: true, result: `${target}\n（空文件）` };

        return { ok: true, result: truncateMiddle(`${target}\n${text}`) };
      }
      case "get_launch_log": {
        const text: string = await GetLogText();

        if (!text.trim()) return { ok: true, result: "当前没有日志内容" };

        return { ok: true, result: truncateMiddle(text) };
      }
      case "diagnose_crash": {
        // 后端自动读实例游戏目录下最新的 crash-reports/*.txt 并判定常见原因
        const d: launch.CrashDiagnosis = await DiagnoseCrash();

        return {
          ok: true,
          result: truncateForModel(
            JSON.stringify(
              {
                reportPath: d.ReportPath,
                summary: d.Summary,
                description: d.Description,
                exception: d.Exception,
                suspected: d.Suspected,
                suggestions: d.Suggestions,
              },
              null,
              2,
            ),
          ),
        };
      }
      case "get_launch_config": {
        const snap: launch.GameLaunchSnapshot = await GetLaunchSnapshot();

        return {
          ok: true,
          result: truncateForModel(JSON.stringify(snap, null, 2)),
        };
      }
      case "search_mod": {
        const query = String(args.query ?? "").trim();

        if (!query) return { ok: false, result: "缺少 query 参数" };
        const request: models.ResourceSearchRequest = {
          source: String(args.source ?? "modrinth"),
          projectType: "mod",
          query,
          gameVersion: ctx.details?.BaseGameVersion ?? "",
          loader: (ctx.details?.LoaderName ?? "").toLowerCase(),
          loaders: [],
          limit: 8,
        };
        const result = await SearchResources(request);
        const hits = (result?.hits ?? []).slice(0, 8).map((h) => ({
          projectId: h.projectId,
          slug: h.slug,
          title: h.title,
          description: h.description?.slice(0, 120),
          downloads: h.downloadsDisplay || h.downloads,
        }));

        return {
          ok: true,
          result: truncateForModel(
            hits.length
              ? JSON.stringify(
                  {
                    hits,
                    note: "安装时请把 projectId 作为 project_id 传入 install_mod",
                  },
                  null,
                  2,
                )
              : "没有找到相关模组",
          ),
        };
      }
      case "open_folder": {
        if (!ctx.details) return { ok: false, result: NO_INSTANCE_MSG };
        const target = String(args.target ?? "content");
        const base = ctx.details.ContentDirectory.replace(/[\\/]+$/, "");
        const dirMap: Record<string, string> = {
          content: base,
          mods: `${base}/mods`,
          resourcepacks: `${base}/resourcepacks`,
          shaderpacks: `${base}/shaderpacks`,
          saves: `${base}/saves`,
        };
        const dir = dirMap[target];

        if (!dir) {
          return {
            ok: false,
            result: `未知目录：${target}，可选 content/mods/resourcepacks/shaderpacks/saves`,
          };
        }
        await OpenInExplorer(dir);

        return { ok: true, result: `已在资源管理器中打开：${dir}` };
      }
      case "install_mod": {
        if (!ctx.details) return { ok: false, result: NO_INSTANCE_MSG };
        const projectId = String(args.project_id ?? "").trim();

        if (!projectId) {
          return {
            ok: false,
            result: "缺少 project_id 参数，请先调用 search_mod 获取",
          };
        }
        const source = String(args.source ?? "modrinth");
        let versionId = String(args.version_id ?? "").trim();

        if (!versionId) {
          const listReq: models.ResourceVersionRequest = {
            source,
            projectId,
            gameVersion: ctx.details.BaseGameVersion ?? "",
            loader: (ctx.details.LoaderName ?? "").toLowerCase(),
          };
          const list = await ListResourceVersions(listReq);
          const versions = list?.versions ?? [];
          const matched =
            versions.find((v) => v.matchesInstance) ?? versions[0];

          if (!matched?.versionId) {
            return { ok: false, result: "该模组没有可用于当前实例的版本" };
          }
          versionId = matched.versionId;
        }
        const downloadReq: models.ResourceDownloadRequest = {
          source,
          projectId,
          versionId,
          contentDirectory: ctx.details.ContentDirectory,
          subDirectory: "mods",
        };
        const downloadResult = await DownloadResourceVersion(downloadReq);

        return {
          ok: true,
          result: truncateForModel(
            JSON.stringify(
              {
                ok: true,
                fileName: downloadResult?.fileName,
                savedPath: downloadResult?.savedPath,
                fileSize: downloadResult?.fileSize,
              },
              null,
              2,
            ),
          ),
        };
      }
      case "toggle_mod": {
        if (!ctx.details) return { ok: false, result: NO_INSTANCE_MSG };
        const nameArg = String(args.name ?? "").trim();

        if (!nameArg) return { ok: false, result: "缺少 name 参数" };
        const lower = nameArg.toLowerCase();
        const entry = (ctx.details.Mods ?? []).find(
          (m) =>
            m.Name?.toLowerCase() === lower ||
            m.SourcePath?.toLowerCase().endsWith(lower) ||
            m.Name?.toLowerCase().includes(lower),
        );

        if (!entry) return { ok: false, result: `未找到模组：${nameArg}` };
        const disable =
          args.disable === undefined ? true : Boolean(args.disable);

        if (entry.IsDisabled === disable) {
          return {
            ok: true,
            result: `模组 ${entry.Name} 已处于${disable ? "禁用" : "启用"}状态，无需操作`,
          };
        }
        await ToggleContentEntry(entry.SourcePath, disable);

        return {
          ok: true,
          result: `已${disable ? "禁用" : "启用"}模组：${entry.Name}`,
        };
      }
      case "launch_instance": {
        const result: launch.LaunchResult = await Launch("", null);

        return {
          ok: result?.Success ?? false,
          result:
            result?.Message ||
            (result?.Success ? "启动指令已发出" : "启动失败"),
        };
      }
      case "stop_game": {
        const result: launch.LaunchResult = await StopGame();

        return {
          ok: result?.Success ?? false,
          result:
            result?.Message || (result?.Success ? "已停止游戏" : "停止失败"),
        };
      }
      default:
        return { ok: false, result: `未知工具：${name}，请查看可用工具列表` };
    }
  } catch (ex) {
    return { ok: false, result: (ex as Error)?.message || String(ex) };
  }
}

// ------------------------------------------------------------------
// 消息 → API 请求格式（含附件多模态转换与上下文裁剪）
// ------------------------------------------------------------------

/** data URL 中的 base64 文本解码为 UTF-8 字符串 */
function decodeDataUrlText(dataUrl: string): string | null {
  try {
    const base64 = dataUrl.split(",")[1] ?? "";
    const bytes = Uint8Array.from(atob(base64), (ch) => ch.charCodeAt(0));

    return new TextDecoder("utf-8", { fatal: false }).decode(bytes);
  } catch {
    return null;
  }
}

/** 把附件转换为注入消息文本的说明（图片走独立多模态块，这里只处理文本与二进制说明） */
function attachmentTextParts(attachments: Attachment[]): {
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

function formatFileSizeStatic(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;

  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/** 组装发给模型的完整消息列表（含会话摘要、当前用户消息、工具结果与附件） */
function buildApiMessages(
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
function buildSystemPrompt(settings: AiSettings): string {
  const permissionStatus = [
    `- 查看实例文件夹：${settings.allowFolderRead ? "已开启" : "已关闭（相关工具会被直接拒绝）"}`,
    `- 修改实例文件夹：${settings.allowFolderWrite ? "已开启" : "已关闭（相关工具会被直接拒绝）"}`,
    `- 修改操作确认模式：${settings.allowModify ? "直接执行" : "每次修改需用户批准"}`,
  ].join("\n");

  return `${settings.systemPrompt}\n\n## 当前权限状态（用户可在 AI 设置 → 实例操作权限中修改）\n${permissionStatus}`;
}

// ------------------------------------------------------------------
// 主组件
// ------------------------------------------------------------------

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
  /** 待发送的附件列表 */
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>(
    [],
  );
  const [advancedOpen, setAdvancedOpen] = useState(false);
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

  const effectiveBaseUrl =
    settings.providerId === "custom"
      ? settings.customBaseUrl
      : currentProvider.baseUrl;

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

  // 初始化加载
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

  // settingsRef 与 settings 保持同步
  useEffect(() => {
    settingsRef.current = settings;
  }, [settings]);

  // 卸载时放行所有挂起的审批 Promise，避免 Agent 循环永远悬空
  useEffect(() => {
    const waiters = approvalWaitersRef.current;

    return () => {
      for (const waiter of waiters.values()) waiter(false);
      waiters.clear();
    };
  }, []);

  // 持久化
  useEffect(() => {
    saveToStorage(STORAGE_KEYS.sessions, sessions);
  }, [sessions]);

  useEffect(() => {
    saveToStorage(STORAGE_KEYS.settings, settings);
  }, [settings]);

  useEffect(() => {
    saveToStorage(STORAGE_KEYS.activeSession, activeSessionId);
  }, [activeSessionId]);

  // 会话操作
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

  // 处理文件选择
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

  // 移除附件
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
    const openaiBaseUrl =
      cur.providerId === "custom"
        ? cur.customBaseUrl
        : (provider?.baseUrl ?? "");
    // Anthropic 格式优先走供应商专用的兼容端点（如 DeepSeek 的 /anthropic）
    const baseUrl =
      cur.apiFormat === "anthropic" && provider?.anthropicBaseUrl
        ? provider.anthropicBaseUrl
        : openaiBaseUrl;

    return { cur, baseUrl };
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
            cur.apiKey,
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
          cur.apiKey,
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
        setStreamingContent("");
        let responseText: string;
        let nativeToolCalls: NativeToolCall[] = [];
        let aborted: boolean;

        try {
          const result = await callModelOnce(
            working,
            (chunk) => setStreamingContent((prev) => prev + chunk),
            summary,
          );

          responseText = result.text;
          nativeToolCalls = result.nativeToolCalls;
          aborted = result.aborted;
        } catch (err) {
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
    [allowToolAlways, callModelOnce, requestApproval, updateSessionMessages],
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
      const { cur, baseUrl } = activeEndpoint();

      if (!cur.apiKey.trim() || !baseUrl.trim() || !cur.model.trim())
        return null;
      const prompt = `${transcript}`;
      const existing = sessions.find((s) => s.id === sessionId)?.summary;

      try {
        setIsCompacting(true);
        const result =
          cur.apiFormat === "anthropic"
            ? await streamAnthropicChat(
                baseUrl,
                cur.apiKey,
                cur.model,
                SUMMARY_SYSTEM_PROMPT,
                [{ role: "user", content: prompt }],
                0.2,
                () => {},
                undefined,
                undefined,
              )
            : await streamOpenAiChat(
                baseUrl,
                cur.apiKey,
                cur.model,
                [
                  { role: "system", content: SUMMARY_SYSTEM_PROMPT },
                  { role: "user", content: prompt },
                ],
                0.2,
                () => {},
                undefined,
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
        setIsCompacting(false);
      }
    },
    [activeEndpoint, sessions, updateSessionMessages],
  );

  /**
   * 统一执行一个对话回合：先按需压缩历史，再跑 Agent 循环。
   * sendMessage 与消息编辑重发共用，保证压缩策略一致。
   */
  const executeTurn = useCallback(
    async (sessionId: string, convo: ChatMessage[]) => {
      setIsSending(true);
      setStreamingContent("");
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
        setStreamingContent("");
        abortControllerRef.current = null;
      }
    },
    [
      activeEndpoint,
      compactHistory,
      runAgentTurn,
      sessions,
      updateSessionMessages,
    ],
  );

  /** 收集未填写的必填 AI 配置项（发送与编辑重发共用） */
  const missingConfigItems = useCallback(() => {
    const { cur, baseUrl } = activeEndpoint();
    const missing: string[] = [];

    if (!cur.apiKey.trim()) missing.push(t("API Key"));
    if (!baseUrl.trim()) missing.push(t("API 地址"));
    if (!cur.model.trim()) missing.push(t("模型名称"));

    return missing;
  }, [activeEndpoint]);

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

  // 停止生成
  const stopStreaming = useCallback(() => {
    abortControllerRef.current?.abort();
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
    }));
  };

  // ------------------------------------------------------------------
  // 渲染
  // ------------------------------------------------------------------

  return (
    <section className="flex h-full w-full min-h-0 overflow-hidden">
      {/* 左侧边栏：与其它顶层面板同源的毛玻璃（背景透出 + blur，随外观设置的毛玻璃强度变化） */}
      <motion.div
        animate={{ width: sidebarCollapsed ? 0 : 280 }}
        className="relative flex-shrink-0 overflow-hidden border-r nya-border nya-panel min-w-0"
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
                <div className="flex size-14 items-center justify-center rounded-3xl bg-gradient-to-br from-default-200 to-default-100">
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
                        className={`group flex cursor-pointer items-center gap-2 rounded-xl px-3 py-2 transition-all ${
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
                  <span>{t("需确认 / 只读模式")}</span>
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

      {/* 右侧聊天区 */}
      <div className="flex min-h-0 flex-1 flex-col">
        <header className="flex flex-shrink-0 items-center gap-2.5 border-b nya-border px-3 py-2.5">
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
          <div className="flex size-9 flex-shrink-0 items-center justify-center rounded-xl bg-primary/15 text-primary">
            <AiIcon className="h-5 w-5" />
          </div>

          {/* 标题信息 */}
          <div className="min-w-0 flex-1">
            <div className="truncate text-[15px] font-semibold leading-tight text-gray-800 dark:text-gray-200">
              {activeSession?.title ?? t("AI 助手")}
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
        </header>

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
                  className="flex size-16 items-center justify-center rounded-3xl bg-gradient-to-br from-primary/20 to-secondary/10 text-primary"
                  transition={{
                    duration: 3,
                    repeat: Infinity,
                    ease: "easeInOut",
                  }}
                >
                  <AiIcon className="h-8 w-8" />
                </motion.div>
                <div>
                  <h2 className="text-xl font-semibold text-gray-800 dark:text-gray-200">
                    {t("你好，我是 AI 助手喵~")}
                  </h2>
                  <p className="mt-1 max-w-md text-[13px] text-gray-500 dark:text-gray-400">
                    {t(
                      "我可以帮你管理 Minecraft 实例、解答问题、调整配置。输入消息开始对话吧！",
                    )}
                  </p>
                </div>
                <div className="mt-2 flex flex-wrap justify-center gap-2">
                  {[
                    t("如何分配更多内存？"),
                    t("帮我安装一个模组"),
                    t("Java 版本怎么选？"),
                    t("启动失败怎么办？"),
                  ].map((q) => (
                    <Button
                      key={q}
                      size="sm"
                      variant="flat"
                      onPress={() => {
                        setInput(q);
                        textareaRef.current?.focus();
                      }}
                    >
                      {q}
                    </Button>
                  ))}
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
                              <div className="w-full max-w-[85%] rounded-2xl border border-primary/40 bg-default-50 p-2">
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
                                  <div className="max-w-[80%] rounded-2xl rounded-tr-md bg-primary px-4 py-2.5 text-primary-foreground shadow-sm">
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
                              className={`max-w-[80%] rounded-2xl rounded-tl-md border px-4 py-3 shadow-sm ${
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
                              <AiIcon className="h-4 w-4" />
                            </div>
                            <div className="max-w-[80%] rounded-2xl rounded-tl-md border nya-border nya-panel px-4 py-2.5">
                              <div className="nya-markdown text-sm leading-relaxed">
                                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                                  {stripToolBlocks(msg.content)}
                                </ReactMarkdown>
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
                      <AiIcon className="h-4 w-4" />
                    </div>
                    <div className="max-w-[80%] rounded-2xl rounded-tl-md border nya-border nya-panel px-4 py-2.5">
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

            <div className="flex items-end gap-2 rounded-2xl border nya-border nya-panel p-2 focus-within:border-primary/50">
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

      {/* 设置弹窗 */}
      <Modal
        isOpen={settingsOpen}
        size="lg"
        onClose={() => setSettingsOpen(false)}
        {...modalBehaviorProps}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex items-center gap-2">
                <SettingsIcon className="h-5 w-5 text-primary" />
                {t("AI 设置")}
              </ModalHeader>
              <ModalBody className="gap-4">
                <div className="flex flex-col gap-2">
                  <label className="text-xs font-medium text-gray-500">
                    {t("模型供应商")}
                  </label>
                  <Select
                    disallowEmptySelection
                    selectedKeys={new Set([settings.providerId])}
                    onSelectionChange={(keys) => {
                      const id = String(Array.from(keys)[0] ?? "");

                      if (id) handleProviderChange(id);
                    }}
                  >
                    {BUILTIN_PROVIDERS.map((p) => (
                      <SelectItem key={p.id}>{p.name}</SelectItem>
                    ))}
                  </Select>
                </div>

                {settings.providerId === "custom" && (
                  <div className="flex flex-col gap-2">
                    <label className="text-xs font-medium text-gray-500">
                      {t("API 地址")}
                    </label>
                    <Input
                      placeholder="https://api.example.com/v1"
                      value={settings.customBaseUrl}
                      onValueChange={(v) =>
                        setSettings((s) => ({ ...s, customBaseUrl: v }))
                      }
                    />
                  </div>
                )}

                <div className="flex flex-col gap-2">
                  <label className="text-xs font-medium text-gray-500">
                    {t("API Key")}
                  </label>
                  <Input
                    placeholder={t("输入 API Key（必填）")}
                    type="password"
                    value={settings.apiKey}
                    onValueChange={(v) =>
                      setSettings((s) => ({ ...s, apiKey: v }))
                    }
                  />
                  {!settings.apiKey && (
                    <span className="text-[11px] text-warning">
                      {t("⚠️ API Key 未填写，将无法调用 AI")}
                    </span>
                  )}
                </div>

                <div className="flex flex-col gap-2">
                  <label className="text-xs font-medium text-gray-500">
                    {t("模型名称")}
                  </label>
                  <Input
                    placeholder={currentProvider.defaultModel || "model-name"}
                    value={settings.model}
                    onValueChange={(v) =>
                      setSettings((s) => ({ ...s, model: v }))
                    }
                  />
                </div>

                {/* 温度设置已隐藏，保留默认值 0.7 */}

                <Divider />

                <div className="flex flex-col gap-3">
                  <label className="text-xs font-medium text-gray-500">
                    {t("实例操作权限")}
                  </label>

                  {/* 权限一：查看实例文件夹 */}
                  <div
                    className={`flex items-start gap-3 rounded-xl border p-3 ${
                      settings.allowFolderRead
                        ? "border-success/40 bg-success/5"
                        : "border-default-300 bg-default-100/40"
                    }`}
                  >
                    <Switch
                      isSelected={settings.allowFolderRead}
                      onValueChange={(v) =>
                        setSettings((s) => ({ ...s, allowFolderRead: v }))
                      }
                    />
                    <div className="flex-1">
                      <div className="text-sm font-medium">
                        {t("查看实例文件夹")}
                      </div>
                      <div className="mt-0.5 text-[11px] text-gray-400">
                        {settings.allowFolderRead
                          ? t(
                              "AI 可以查看实例的模组/存档等列表、浏览与读取实例内文件（含日志和配置），并打开实例文件夹",
                            )
                          : t(
                              "AI 无法查看实例文件夹内容，相关查询会被直接拒绝",
                            )}
                      </div>
                    </div>
                  </div>

                  {/* 权限二：修改实例文件夹 */}
                  <div
                    className={`flex items-start gap-3 rounded-xl border p-3 ${
                      settings.allowFolderWrite
                        ? "border-success/40 bg-success/5"
                        : "border-default-300 bg-default-100/40"
                    }`}
                  >
                    <Switch
                      isSelected={settings.allowFolderWrite}
                      onValueChange={(v) =>
                        setSettings((s) => ({ ...s, allowFolderWrite: v }))
                      }
                    />
                    <div className="flex-1">
                      <div className="text-sm font-medium">
                        {t("修改实例文件夹")}
                      </div>
                      <div className="mt-0.5 text-[11px] text-gray-400">
                        {settings.allowFolderWrite
                          ? t(
                              "允许 AI 安装模组、启用/禁用模组等写入操作，是否逐次确认见下方开关",
                            )
                          : t(
                              "AI 无法对实例文件夹做任何修改，即使处于可修改模式也会被拒绝",
                            )}
                      </div>
                    </div>
                  </div>

                  {/* 权限三：直接修改（确认模式） */}
                  <div
                    className={`flex items-start gap-3 rounded-xl border p-3 ${
                      settings.allowModify
                        ? "border-success/40 bg-success/5"
                        : "border-warning/40 bg-warning/5"
                    }`}
                  >
                    <Switch
                      isSelected={settings.allowModify}
                      onValueChange={(v) =>
                        setSettings((s) => ({ ...s, allowModify: v }))
                      }
                    />
                    <div className="flex-1">
                      <div className="text-sm font-medium">
                        {settings.allowModify
                          ? t("允许直接修改")
                          : t("需确认 / 只读")}
                      </div>
                      <div className="mt-0.5 text-[11px] text-gray-400">
                        {settings.allowModify
                          ? t(
                              "AI 可以不经批准直接执行允许范围内的修改操作（含启动/停止游戏）",
                            )
                          : t(
                              "修改与启动/停止游戏每次都需要你手动批准后才会执行",
                            )}
                      </div>
                    </div>
                  </div>

                  {/* 总是允许授权：可随时撤销，重启后自动清空 */}
                  {alwaysAllowedList.length > 0 && (
                    <div className="flex items-start justify-between gap-3 rounded-xl border border-success/30 bg-success/5 p-3">
                      <div className="min-w-0 flex-1">
                        <div className="text-sm font-medium">
                          {t("已总是允许的工具")}
                        </div>
                        <div className="mt-0.5 text-[11px] text-gray-400">
                          {t(
                            "本运行期内这些修改类操作不再需要批准，重启启动器后自动重置",
                          )}
                        </div>
                        <div className="mt-1.5 flex flex-wrap gap-1">
                          {alwaysAllowedList.map((name) => (
                            <span
                              key={name}
                              className="rounded bg-default-200/60 px-1.5 py-0.5 font-mono text-[11px]"
                            >
                              {name}
                            </span>
                          ))}
                        </div>
                      </div>
                      <Button
                        className="flex-shrink-0"
                        color="warning"
                        size="sm"
                        variant="flat"
                        onPress={resetAlwaysAllowed}
                      >
                        {t("撤销全部")}
                      </Button>
                    </div>
                  )}
                </div>

                {/* 高级设置：默认收起，普通用户不需要碰 */}
                <Divider />
                <button
                  className="flex w-full items-center justify-between rounded-lg px-1 py-1 text-xs font-medium text-gray-500 hover:bg-default-100/60"
                  type="button"
                  onClick={() => setAdvancedOpen((v) => !v)}
                >
                  <span>{t("高级设置")}</span>
                  {advancedOpen ? (
                    <ChevronDownIcon className="h-3.5 w-3.5" />
                  ) : (
                    <ChevronRightIcon className="h-3.5 w-3.5" />
                  )}
                </button>
                {advancedOpen && (
                  <>
                    <div className="flex flex-col gap-2">
                      <label className="text-xs font-medium text-gray-500">
                        {t("API 格式")}
                      </label>
                      <Select
                        disallowEmptySelection
                        selectedKeys={new Set([settings.apiFormat])}
                        onSelectionChange={(keys) => {
                          const fmt = String(Array.from(keys)[0] ?? "openai");

                          if (fmt === "openai" || fmt === "anthropic") {
                            setSettings((s) => ({ ...s, apiFormat: fmt }));
                          }
                        }}
                      >
                        <SelectItem key="openai" textValue="OpenAI 兼容格式">
                          <div className="flex flex-col">
                            <span className="text-sm">OpenAI 兼容格式</span>
                            <span className="text-[11px] text-gray-400">
                              /chat/completions · 适用于 OpenAI / DeepSeek /
                              通义千问 等
                            </span>
                          </div>
                        </SelectItem>
                        <SelectItem key="anthropic" textValue="Anthropic 格式">
                          <div className="flex flex-col">
                            <span className="text-sm">Anthropic 格式</span>
                            <span className="text-[11px] text-gray-400">
                              /messages · 适用于 Claude 系列模型
                            </span>
                          </div>
                        </SelectItem>
                      </Select>
                    </div>

                    <div className="flex flex-col gap-2">
                      <label className="text-xs font-medium text-gray-500">
                        {t("上下文窗口大小")}
                      </label>
                      <Select
                        disallowEmptySelection
                        selectedKeys={new Set([String(settings.contextWindow)])}
                        onSelectionChange={(keys) => {
                          const v = Number(Array.from(keys)[0] ?? 262144);

                          setSettings((s) => ({ ...s, contextWindow: v }));
                        }}
                      >
                        <SelectItem key="4096">4K (4096 tokens)</SelectItem>
                        <SelectItem key="8192">8K (8192 tokens)</SelectItem>
                        <SelectItem key="16384">16K (16384 tokens)</SelectItem>
                        <SelectItem key="32768">32K (32768 tokens)</SelectItem>
                        <SelectItem key="65536">64K (65536 tokens)</SelectItem>
                        <SelectItem key="131072">
                          128K (131072 tokens)
                        </SelectItem>
                        <SelectItem key="200000">
                          200K (200000 tokens)
                        </SelectItem>
                        <SelectItem key="262144">
                          256K (262144 tokens)
                        </SelectItem>
                      </Select>
                      <span className="text-[11px] text-gray-400">
                        {t(
                          "根据模型支持的上下文窗口选择，过大会增加 API 调用成本",
                        )}
                      </span>
                    </div>

                    <div className="flex items-center justify-between rounded-xl border nya-border p-3">
                      <div>
                        <div className="text-sm font-medium">
                          {t("显示工具调用详情")}
                        </div>
                        <div className="text-[11px] text-gray-400 mt-0.5">
                          {t("在聊天中展示 AI 调用工具的名称、参数和返回结果")}
                        </div>
                      </div>
                      <Switch
                        isSelected={settings.showToolCalls}
                        onValueChange={(v) =>
                          setSettings((s) => ({ ...s, showToolCalls: v }))
                        }
                      />
                    </div>

                    <div className="rounded-xl bg-default-100/50 p-3">
                      <div className="text-[11px] font-medium text-gray-500">
                        {t("当前配置")}
                      </div>
                      <div className="mt-1 space-y-0.5 text-[11px] text-gray-400">
                        <div>
                          {t("供应商")}: {currentProvider.name}
                        </div>
                        <div>
                          {t("端点")}: {effectiveBaseUrl || t("未设置")}
                        </div>
                        <div>
                          {t("模型")}: {settings.model || t("未设置")}
                        </div>
                      </div>
                    </div>
                  </>
                )}
              </ModalBody>
              <ModalFooter>
                <Button variant="flat" onPress={onClose}>
                  {t("关闭")}
                </Button>
                <Button
                  color="primary"
                  onPress={() => {
                    saveToStorage(STORAGE_KEYS.settings, settings);
                    onClose();
                  }}
                >
                  {t("保存设置")}
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>
    </section>
  );
};

export default AiPage;
