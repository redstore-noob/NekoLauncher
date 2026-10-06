/*
 * AI 助手逻辑层（由 layouts/ai.tsx 拆分）：Agent 工具循环行为常量：轮数上限、权限工具集合、截断阈值等。
 */
// ------------------------------------------------------------------
// Agent 工具循环常量
// ------------------------------------------------------------------

/** 单次用户消息触发的最大工具调用轮数（防死循环；自主调查链路较长，放宽到 10） */
export const MAX_TOOL_ROUNDS = 10;
/** 自动压缩时保留原文的最近消息条数（不含工具结果），其余压缩成摘要 */
export const KEEP_RECENT_MESSAGES = 6;
/** 会话摘要的系统提示词（一次性调用，不走 Agent 工具循环） */
export const SUMMARY_SYSTEM_PROMPT =
  "你是会话摘要助手。把给定的 Launcher AI 助手对话历史压缩成简洁的中文摘要，保留：用户的目标与偏好、已执行的操作及其结果、关键数据（版本号、文件名、路径、设置值）、未完成的事项。直接输出摘要正文，不要寒暄或解释。";
/** 工具结果回填给模型时的最大字符数 */
export const MAX_TOOL_RESULT_CHARS = 6000;
/** allowModify=false 时需要用户批准的修改类工具 */
export const CONFIRM_TOOLS = new Set([
  "install_mod",
  "toggle_mod",
  "install_resource",
  "switch_instance",
  "launch_instance",
  "stop_game",
]);
/** 受「查看实例文件夹」权限控制的工具（allowFolderRead=false 时直接拒绝） */
export const FOLDER_READ_TOOLS = new Set([
  "get_instance_info",
  "list_mods",
  "list_resourcepacks",
  "list_shaders",
  "list_saves",
  "get_game_settings",
  "open_folder",
  "list_directory",
  "read_file",
  "analyze_mod_conflicts",
  "check_content_updates",
]);
/** 受「修改实例文件夹」权限控制的工具（allowFolderWrite=false 时直接拒绝） */
export const FOLDER_WRITE_TOOLS = new Set([
  "install_mod",
  "toggle_mod",
  "install_resource",
]);
/** read_file 拒绝读取的二进制扩展名（读出来只是乱码，浪费上下文） */
export const BINARY_FILE_EXTENSIONS = new Set([
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
export const MAX_ATTACHMENT_TEXT_CHARS = 4000;
/** 可以按文本读取内容发给模型的附件扩展名 */
export const TEXT_ATTACHMENT_EXTENSIONS = new Set([
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
