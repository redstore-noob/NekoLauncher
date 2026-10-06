/*
 * globals.css 不得再"够"HeroUI 槽位的回归测试。
 *
 * 为什么需要这个测试：CSS 选择器指向一个**不存在的属性值**时不会报任何错，
 * 组件照样渲染，只是样式静默失效——构建、类型检查、lint 全都绿。此前
 * `[data-slot="popover"]` 就是这样潜伏了很久：Select 把该属性传给
 * FreeSoloPopover，但 Popover 内部的 getDialogProps 会把它覆盖成
 * `[data-slot="base"]`，于是那条圆角规则从未命中过任何元素，
 * Select 面板从 Pre-Beta 1 到 0.3.1 一直是直角。
 *
 * 结论不是"把选择器写对"，而是**不再从 CSS 够组件内部**：槽位名是字符串，
 * 只能靠测试兜；而 HeroUI 的 `classNames` / `className` prop 有 TS 类型，
 * 写错是编译错误。所以现在组件外观一律走公开 props，globals.css 里
 * 只保留本项目自己的 `.nya-*` 类。
 *
 * 这个测试负责守住这条政策：一旦有人在 globals.css 里写回 `[data-slot=...]`，
 * 它会失败并列出 HeroUI 当前真正会渲染的槽位（升级依赖后槽位会改名，
 * 例如 HeroUI v3 把 `content` 改成了 `select-popover`）。
 */
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";

import { describe, expect, it } from "vitest";

const SRC_DIR = resolve(__dirname, "..");
const GLOBALS_CSS = join(SRC_DIR, "styles", "globals.css");
const HEROUI_MODULES = resolve(SRC_DIR, "..", "node_modules", "@heroui");

/**
 * 在已安装的 HeroUI 组件源码里收集所有会渲染到 DOM 上的 data-slot 值。
 * 只认 `"data-slot": "xxx"` 这种字面量写法（组件里给 DOM 挂属性的方式），
 * 避免把 tv() 槽位定义里的键名也算进来——那些是 classNames 的键，
 * 不一定会变成 DOM 属性。
 */
function collectRenderedSlots(dir: string): Set<string> {
  const slots = new Set<string>();

  const walk = (current: string) => {
    let entries: string[];

    try {
      entries = readdirSync(current);
    } catch {
      return; // 包不存在时跳过，由下面的断言兜底
    }

    for (const entry of entries) {
      const full = join(current, entry);

      if (statSync(full).isDirectory()) {
        walk(full);

        continue;
      }

      if (!/\.(js|mjs)$/.test(entry)) continue;

      const code = readFileSync(full, "utf8");

      for (const match of code.matchAll(/"data-slot"\s*:\s*"([a-zA-Z-]+)"/g)) {
        slots.add(match[1]);
      }
    }
  };

  walk(dir);

  return slots;
}

/**
 * 取出 globals.css 里所有 [data-slot="xxx"] 用到的值。
 *
 * 先剥掉注释再扫描：globals.css 的注释里会**提到**失效的槽位名
 * （正是为了解释为什么不能用它），把注释也算进来的话，
 * 这条护栏会被自己的说明文字绊倒。
 */
function collectCssSlots(css: string): Set<string> {
  const slots = new Set<string>();
  const withoutComments = css.replace(/\/\*[\s\S]*?\*\//g, "");

  for (const match of withoutComments.matchAll(
    /\[data-slot=["']?([a-zA-Z-]+)["']?\]/g,
  )) {
    slots.add(match[1]);
  }

  return slots;
}

const css = readFileSync(GLOBALS_CSS, "utf8");
const cssSlots = collectCssSlots(css);
const renderedSlots = collectRenderedSlots(HEROUI_MODULES);

describe("globals.css 不得再够 HeroUI 的槽位", () => {
  it("确实扫到了 HeroUI 渲染用的槽位（防止测试本身失效）", () => {
    // 如果 HeroUI 换了挂属性的写法，这个测试会先在这里失败，
    // 而不是伪装成"CSS 没问题"。
    expect(renderedSlots.size).toBeGreaterThan(0);
    expect(renderedSlots.has("content")).toBe(true);
  });

  it("CSS 里没有任何 [data-slot=...] 选择器", () => {
    // 政策：组件外观只走 HeroUI 公开 props（classNames / className / 主题变量），
    // CSS 不再引用 data-slot。理由见本文件头与 globals.css 里的注释。
    // 若确有必要重新引入，请连同这条断言一起讨论，别只把这个测试改绿。
    const used = [...cssSlots];

    expect(
      used,
      `globals.css 引用了 HeroUI 的槽位：${used.join(", ")}。` +
        `这类选择器写错不会报错、只会静默失效（历史上就这样丢过 Select 的圆角）。` +
        `请改用 HeroUI 的 classNames / className，或在本文件里用本项目自己的类。` +
        `HeroUI 当前会渲染的槽位：${[...renderedSlots].sort().join(", ")}`,
    ).toEqual([]);
  });

  it("注释里提到已失效的槽位不会被算成规则", () => {
    // collectCssSlots 先剥注释再扫描：globals.css 的注释里会**提到**
    // `[data-slot="popover"]` 这类名字（正是为了解释为什么不能用它），
    // 把注释也算进来的话，这条护栏会被自己的说明文字绊倒。
    expect(css).toContain("data-slot");
    expect(cssSlots.size).toBe(0);
  });
});
