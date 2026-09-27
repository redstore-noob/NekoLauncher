/*
 * lib/guards.ts 的测试（P3-6）。
 *
 * 这三个守卫是"后端绑定返回形状不对"与"卡片崩溃"之间唯一的防线：
 * Go 侧失败时可能返回 null / 对象 / 字符串，而 `?? 默认值` 只拦 null/undefined。
 */
import { describe, expect, it } from "vitest";

import { asArray, asObject, asText } from "./guards";

describe("asArray", () => {
  it("数组原样返回（含空数组）", () => {
    expect(asArray<number>([1, 2])).toEqual([1, 2]);
    expect(asArray<number>([])).toEqual([]);
  });

  it("非数组一律回落空数组，避免 .map() 把页面带走", () => {
    expect(asArray(null)).toEqual([]);
    expect(asArray(undefined)).toEqual([]);
    expect(asArray({ length: 1 })).toEqual([]);
    expect(asArray("abc")).toEqual([]);
    expect(asArray(42)).toEqual([]);
  });
});

describe("asText", () => {
  it("字符串原样返回", () => {
    expect(asText("abc")).toBe("abc");
    expect(asText("")).toBe("");
  });

  it("非字符串回落空串", () => {
    expect(asText(null)).toBe("");
    expect(asText(123)).toBe("");
    expect(asText({ toString: () => "x" })).toBe("");
  });
});

describe("asObject", () => {
  it("对象原样返回", () => {
    const value = { a: 1 };

    expect(asObject<{ a: number }>(value)).toBe(value);
  });

  it("null / 非对象回落 null，由调用方决定兜底展示", () => {
    expect(asObject(null)).toBeNull();
    expect(asObject(undefined)).toBeNull();
    expect(asObject("abc")).toBeNull();
    expect(asObject(7)).toBeNull();
  });
});
