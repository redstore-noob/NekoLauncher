/*
 * 日志级别解析与着色：运行日志小组件与日志查看弹层共用。
 *
 * 兼容两种日志格式：
 * - 启动器自身："[时间][级别]正文"，如 "[12:00:00][INFO] 正在启动"
 * - Minecraft 游戏进程："[时间] [线程/级别] [命名空间]: 正文"，
 *   如 "[12:34:56] [Render thread/INFO] [minecraft/Minecraft]: Backing up..."
 *   （原版 / Forge / Fabric / NeoForge 均为此形态，级别跟在线程名后的斜杠后）
 */

export type LogLevel =
  | "INFO"
  | "WARNING"
  | "ERROR"
  | "SUCCESS"
  | "DEBUG"
  | "OTHER";

/** 级别 → 行文字颜色（含深浅两套） */
export const LOG_LEVEL_CLASS: Record<LogLevel, string> = {
  INFO: "text-blue-600 dark:text-blue-400",
  WARNING: "text-amber-600 dark:text-amber-400",
  ERROR: "text-red-600 dark:text-red-400",
  SUCCESS: "text-emerald-600 dark:text-emerald-400",
  DEBUG: "text-gray-400 dark:text-gray-500",
  OTHER: "text-gray-600 dark:text-gray-300",
};

/** Minecraft 日志的 "[线程/级别]" 片段，级别允许 WARN/WARNING/FATAL 等变体；
 *  线程名与级别两侧允许空白（个别模组日志格式不严格） */
const MC_LEVEL_PATTERN =
  /\[\s*[^/\]]*?\/\s*(INFO|WARN|WARNING|ERROR|FATAL|SEVERE|DEBUG|TRACE)\s*\]/i;

/** 终端 ANSI 转义（个别模组/启动脚本会往 stdout 里混入颜色码，破坏方括号解析）。
 *  eslint-disable：ESC 控制字符正是要匹配的目标 */
// eslint-disable-next-line no-control-regex
const ANSI_ESCAPE_PATTERN = /\x1b\[[0-9;]*[A-Za-z]/g;

/** 解析一行日志的级别 */
export function parseLogLevel(line: string): LogLevel {
  const clean = line.replace(ANSI_ESCAPE_PATTERN, "");

  // Minecraft 格式：[线程/级别]（先试这个，避免被行首时间方括号干扰）
  const mc = MC_LEVEL_PATTERN.exec(clean);

  if (mc) return normalizeLevel(mc[1]);

  // 启动器格式：[时间][级别]正文（级别可能出现在前两个方括号中的任一个，
  // 例如 "[INFO][启动] xxx" / "[时间][INFO] xxx"——逐个找第一个能识别的）
  const brackets = clean.match(/\[[^\]]*\]/g);

  if (brackets) {
    for (const bracket of brackets.slice(0, 2)) {
      const level = normalizeLevel(bracket.slice(1, -1).trim());

      if (level !== "OTHER") return level;
    }
  }

  return "OTHER";
}

/** 级别文本（大小写/变体宽松匹配）→ 标准级别 */
function normalizeLevel(raw: string): LogLevel {
  switch (raw.trim().toUpperCase()) {
    case "INFO":
      return "INFO";
    case "WARN":
    case "WARNING":
      return "WARNING";
    case "ERROR":
    case "FATAL":
    case "SEVERE":
      return "ERROR";
    case "SUCCESS":
      return "SUCCESS";
    case "DEBUG":
    case "TRACE":
      return "DEBUG";
    default:
      return "OTHER";
  }
}

/**
 * 无级别标记行的兜底：包含错误特征词按 ERROR 着色。
 * （游戏输出的异常堆栈行——"at xxx.yyy"、"Caused by:"——通常不带级别标记）
 */
const ERROR_KEYWORD_PATTERN =
  /Exception|Error|FATAL|Caused by|失败|错误|crash|refused|timed out/i;

export function looksLikeErrorLine(line: string): boolean {
  return ERROR_KEYWORD_PATTERN.test(line);
}

/** 单行日志 → 着色 class（级别优先，无级别时错误特征词兜底） */
export function logLineClass(line: string): string {
  const level = parseLogLevel(line);

  if (level !== "OTHER") return LOG_LEVEL_CLASS[level];

  return looksLikeErrorLine(line)
    ? LOG_LEVEL_CLASS.ERROR
    : LOG_LEVEL_CLASS.OTHER;
}
