/**
 * 文本对象的脚本沙箱。
 *
 * WE 的时钟/日期类文本用一段 JS 脚本驱动(scene.json text.script 字段),
 * 形如:
 *
 *   var lastString;
 *   export function update() {
 *     thisLayer.text = newString.replace('$', engine.userProperties.name);
 *   }
 *
 * 这里去掉 export 前缀后用 new Function 求值,注入 thisLayer(写回文本)与
 * engine(读用户属性)两个宿主对象。脚本源自创意工坊内容,与网页壁纸的
 * 任意 JS 同级信任;刻意不注入 window 之外的启动器 API。
 */

export interface TextScriptHost {
  /** 脚本写入的最新文本 */
  text: string;
  /** 用户属性(引擎侧只读视图) */
  userProperties: Record<string, unknown>;
}

export interface TextScript {
  /** 每帧/每秒调用;脚本内部自行判断是否需要更新 */
  update: () => void;
}

/** 编译文本脚本;无脚本或编译失败返回 null(回退静态文本)。 */
export function compileTextScript(
  script: string | undefined,
  host: TextScriptHost,
): TextScript | null {
  if (!script || !script.trim()) return null;
  try {
    // "export function update()" → "function update()";CommonJS 导出语法在浏览器里非法
    const body = script.replace(
      /\bexport\s+(?=(function|var|let|const|async)\b)/g,
      "",
    );
    const factory = new Function(
      "thisLayer",
      "engine",
      `"use strict";\n${body}\n;return { update: typeof update === "function" ? update : null };`,
    ) as (layer: unknown, engine: unknown) => { update: (() => void) | null };
    const exports = factory(host, { userProperties: host.userProperties });

    if (exports.update) {
      return { update: exports.update };
    }

    return null;
  } catch {
    // 壁纸脚本千奇百怪,编译失败按静态文本处理
    return null;
  }
}
