/*
 * 浮层动效的回归测试。
 *
 * 为什么需要这个测试：浮层"打开时闪一下"是个**只在视觉上出现**的 bug——
 * 类型检查、lint、构建全绿，只有盯着下拉面板看才看得出来。它的成因是
 * 面板所在的 motion 层在入场时从 `opacity: 0` 淡入：整层半透明的这几帧里，
 * 面板背后的页面内容会透出来。
 *
 * HeroUI 默认的 TRANSITION_VARIANTS.scaleSpringOpacity 就是这个写法，
 * 而 lib/motion.ts 里的 popoverMotionProps / dropdownMotionProps 会**整份替换**
 * 掉那套 variants（HeroUI 把 ...motionProps 展开在 variants 之后），
 * 所以只要有人在 preset 里写回 `opacity: 0`，用到这套 preset 的浮层
 * 会一起重新闪起来，且不会有任何编译期提示。
 *
 * 注意适用范围：**Select / Autocomplete 已经不用这套 preset 了** —— 它们恢复成
 * HeroUI 原版写法（含原版淡入），闪是当时明确接受的取舍。这里守的是仍在用的
 * 那几个：Dropdown（`dropdownMotionProps`）与 Popover（`popoverMotionProps`）。
 *
 * 因此这里断言的是契约本身：入场（initial / enter）不得把整层透明度压到 1 以下。
 */
import { describe, expect, it } from "vitest";

import {
  dropdownMotionProps,
  popoverMotionProps,
  TRANSITION_VARIANTS,
} from "./motion";

/** 取出一个 motionProps 上实际生效的 variants 表 */
function variantsOf(props: { variants?: unknown }) {
  return props.variants as Record<string, Record<string, unknown>>;
}

describe("浮层入场不得整层淡入", () => {
  it.each([
    ["popoverMotionProps", popoverMotionProps],
    ["dropdownMotionProps", dropdownMotionProps],
  ])("%s 的 initial / enter 都是不透明的", (_name, props) => {
    const variants = variantsOf(props);

    for (const key of ["initial", "enter"]) {
      expect(
        variants[key],
        `${key} 变体不存在，说明 variants 表被换掉了`,
      ).toBeTruthy();
      expect(
        variants[key].opacity,
        `${key} 变体把 opacity 设成了 ${String(variants[key].opacity)}；` +
          `浮层只要在入场时半透明，背后页面就会透出来（面板"闪一下"）。` +
          `入场请显式写 opacity: 1，淡出只保留在 exit 上。`,
      ).toBe(1);
    }
  });

  it("关闭仍然淡出（别为了修入场把退场也砍了）", () => {
    for (const props of [popoverMotionProps, dropdownMotionProps]) {
      expect(variantsOf(props).exit.opacity).toBe(0);
    }
  });

  it("HeroUI 原版 preset 确实会淡入（这个 bug 的来源，别误当成没事）", () => {
    const stock = TRANSITION_VARIANTS.scaleSpringOpacity as unknown as Record<
      string,
      Record<string, unknown>
    >;

    expect(stock.initial.opacity).toBe(0);
  });
});
