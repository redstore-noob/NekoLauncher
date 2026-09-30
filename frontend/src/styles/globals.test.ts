/*
 * globals.css 浮层选择器的回归测试。
 *
 * 为什么需要这个测试：CSS 选择器指向一个**不存在的属性值**时不会报任何错，
 * 组件照样渲染，只是样式静默失效——构建、类型检查、lint 全都绿。此前
 * `[data-slot="popover"]` 就是这样潜伏了很久：Select 把该属性传给
 * FreeSoloPopover，但 Popover 内部的 getDialogProps 会把它覆盖成
 * `[data-slot="base"]`，于是这条规则从未命中过任何元素，
 * Select 面板的毛玻璃与圆角一直是丢的。
 *
 * 因此这里断言的不是"CSS 长什么样"，而是**CSS 用到的槽位与 HeroUI 实际会
 * 渲染出来的槽位一致**。读取的是已安装的 HeroUI 源码，所以升级依赖导致
 * 槽位改名（例如 HeroUI v3 把 content 改成了 select-popover）时，
 * 这个测试会失败并提醒我们去改 CSS，而不是让外观悄悄坏掉。
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

      for (const match of code.matchAll(
        /"data-slot"\s*:\s*"([a-zA-Z-]+)"/g,
      )) {
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

describe("globals.css 的 data-slot 选择器", () => {
  it("确实扫到了 HeroUI 渲染用的槽位（防止测试本身失效）", () => {
    // 如果 HeroUI 换了挂属性的写法，这个测试会先在这里失败，
    // 而不是伪装成"CSS 没问题"。
    expect(renderedSlots.size).toBeGreaterThan(0);
    expect(renderedSlots.has("content")).toBe(true);
  });

  it("CSS 用到的每个槽位都是 HeroUI 真的会渲染出来的", () => {
    expect(cssSlots.size).toBeGreaterThan(0);

    const bogus = [...cssSlots].filter((slot) => !renderedSlots.has(slot));

    // 失败信息直接给出可用的槽位，省去再翻一遍 node_modules
    expect(
      bogus,
      `globals.css 引用了 HeroUI 不会渲染的槽位：${bogus.join(", ")}。` +
        `可用槽位：${[...renderedSlots].sort().join(", ")}`,
    ).toEqual([]);
  });

  it("浮层面板不要再用已失效的 popover 槽位", () => {
    // 回归护栏：Select 的 popover 属性会被 Popover 覆盖成 base，
    // 这个值永远不会出现在 DOM 上。
    expect(cssSlots.has("popover")).toBe(false);
  });
});
