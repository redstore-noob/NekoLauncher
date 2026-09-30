/*
 * 插件样式注入：两条来源共用一条通道——
 *   1. 清单 styles 字段声明的 CSS 文件（loader 在 activate 前调 injectPluginStyleFiles）；
 *   2. 运行时 api.styles.inject(css, key)（api.ts 校验权限后转发到 injectPluginStyle）。
 *
 * 每条样式以 <style data-plugin-id data-plugin-key> 挂到 document.head，作用于整个
 * 启动器（可自定义任意控件的样式）；同插件同 key 重复注入为替换（幂等）。
 * 卸载/重载/停用时 loader 调 removePluginStyles 按 data-plugin-id 整体摘除，
 * 插件不必自己清理。不用 adoptedStyleSheets 是因为 DevTools 里 <style> 元素
 * 更直观，作者排查"我的样式为什么没生效"时方便得多。
 */
import { t } from "../i18n";

/** 插件资源根路径（应用内路由，非磁盘路径），loader 的入口 URL 也用它拼 */
export const PLUGIN_ROOT = "/plugins/";

/** 各插件已注入的样式：<插件id, <key, <style> 元素>> */
const pluginStyleElements = new Map<string, Map<string, HTMLStyleElement>>();

/**
 * injectPluginStyle 注入（或按 key 替换）一条全局样式。
 * key 缺省为 "inline"；插件想单独更新/移除某条样式时自己起一个稳定 key。
 */
export function injectPluginStyle(
  id: string,
  css: string,
  key = "inline",
): void {
  const text = typeof css === "string" ? css : String(css ?? "");
  let bucket = pluginStyleElements.get(id);
  let element = bucket?.get(key);

  if (!element) {
    element = document.createElement("style");
    element.dataset.pluginId = id;
    element.dataset.pluginKey = key;
    if (!bucket) {
      bucket = new Map();
      pluginStyleElements.set(id, bucket);
    }
    bucket.set(key, element);
  }
  element.textContent = text;
  document.head.appendChild(element);
}

/** removePluginStyle 按 key 摘掉一条样式；不存在时静默返回 */
export function removePluginStyle(id: string, key: string): void {
  const bucket = pluginStyleElements.get(id);
  const element = bucket?.get(key);

  if (!element) return;
  element.remove();
  bucket!.delete(key);
  if (bucket!.size === 0) pluginStyleElements.delete(id);
}

/** removePluginStyles 摘掉某插件注入的全部样式（loader 卸载/重载/停用时调用） */
export function removePluginStyles(id: string): void {
  const bucket = pluginStyleElements.get(id);

  if (!bucket) return;
  for (const key of [...bucket.keys()]) removePluginStyle(id, key);
}

/**
 * injectPluginStyleFiles 注入清单 styles 声明的样式文件（相对插件目录的 .css）。
 * 单个文件缺失或读取失败只记警告并跳过——样式缺失不该让整个插件加载失败。
 * 路径先经 sanitizeStylePath 规范化，越界写法直接丢弃。
 */
export async function injectPluginStyleFiles(
  id: string,
  styles: string[],
): Promise<void> {
  for (const declared of styles) {
    const file = sanitizeStylePath(declared);

    if (!file) continue;
    try {
      const url = `${PLUGIN_ROOT}${encodeURIComponent(id)}/${file
        .split("/")
        .map((segment) => encodeURIComponent(segment))
        .join("/")}`;
      const response = await fetch(url);

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      injectPluginStyle(id, await response.text(), `file:${file}`);
    } catch (error) {
      console.warn(
        t("[plugins] {0} 的样式文件 {1} 注入失败：{2}", {
          "0": id,
          "1": file,
          "2": error instanceof Error ? error.message : String(error),
        }),
      );
    }
  }
}

/**
 * sanitizeStylePath 清单声明的样式路径只放行：非空、.css 结尾、不含 ".."、
 * 不以 "/" 开头的相对路径；其余返回空串（防御性：宿主侧 styleFiles 已滤一遍，
 * 这里兜底运行时 payload 的任意输入）。
 */
function sanitizeStylePath(declared: unknown): string {
  if (typeof declared !== "string") return "";

  const normalized = declared.trim().replace(/\\/g, "/");

  if (!normalized || !normalized.toLowerCase().endsWith(".css")) return "";
  if (normalized.startsWith("/") || normalized.includes("..")) return "";

  return normalized;
}
