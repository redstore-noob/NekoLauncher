/**
 * WE 场景值的解析与用户属性求值。
 *
 * WE 把向量存成 "x y z" 空格分隔字符串、颜色存成 "r g b[a]"(0-1 浮点)、
 * 用户可绑定的字段存成 {user: 属性名, value: 默认值}。这里统一成
 * "取原始值 → 解绑定 → 转类型" 的纯函数,渲染器与单测共用。
 */

import type { WEBound, WEProperty } from "./types";

/** 求值一个可能被用户属性绑定的值:绑定则取用户属性当前值,否则取字段原值。 */
export function resolveBound<T>(
  bound: WEBound<T> | undefined,
  properties: Record<string, WEProperty>,
): T | undefined {
  if (bound === undefined) return undefined;
  if (bound !== null && typeof bound === "object" && !Array.isArray(bound)) {
    const holder = bound as { user?: unknown; value?: unknown };

    if ("user" in holder) {
      const name = holder.user;

      if (typeof name === "string" && properties[name.toLowerCase()]) {
        return properties[name.toLowerCase()].value as T;
      }
    }

    return holder.value as T;
  }

  return bound as T;
}

/** 求值 visible 的组合框条件绑定:{"user": {"condition": "1", "name": "x"}} → 属性值 == condition。 */
export function resolveVisible(
  visible: WEBound<boolean> | undefined,
  properties: Record<string, WEProperty>,
): boolean {
  if (visible === undefined) return true;
  if (visible !== null && typeof visible === "object") {
    const holder = visible as { user?: unknown; value?: unknown };
    const user = holder.user;

    if (
      user !== null &&
      typeof user === "object" &&
      "condition" in (user as object)
    ) {
      const condition = (user as { condition?: string; name?: string })
        .condition;
      const name = (user as { name?: string }).name;
      const value = name ? properties[name.toLowerCase()]?.value : undefined;

      return String(value) === String(condition);
    }
    if (typeof user === "string") {
      const value = properties[user.toLowerCase()]?.value;

      return value === undefined ? Boolean(holder.value) : Boolean(value);
    }

    return Boolean(holder.value);
  }

  return Boolean(visible);
}

/** "1.0 2.0" → [1, 2];非字符串/解析失败返回 fallback。 */
export function parseVec(raw: unknown, fallback: number[] = []): number[] {
  if (typeof raw !== "string") return fallback;
  const parts = raw.trim().split(/\s+/).map(Number);

  if (parts.some((n) => !Number.isFinite(n))) return fallback;

  return parts;
}

/** "x y" → {x, y};WE 场景原点。 */
export function parseVec2(
  raw: unknown,
  fallback: [number, number] = [0, 0],
): [number, number] {
  const v = parseVec(raw, fallback as number[]);

  return [v[0] ?? fallback[0], v[1] ?? fallback[1]];
}

/** "r g b [a]"(0-1)→ [r, g, b, a](0-1);失败返回 fallback。 */
export function parseColor(
  raw: unknown,
  fallback: [number, number, number, number] = [1, 1, 1, 1],
): [number, number, number, number] {
  const v = parseVec(raw, fallback as number[]);

  return [v[0] ?? 1, v[1] ?? 1, v[2] ?? 1, v[3] ?? 1];
}

/** 用户属性颜色值("r g b")→ CSS rgb() 字符串。 */
export function colorToCss(raw: unknown): string {
  const [r, g, b] = parseColor(raw);
  const to255 = (c: number) => Math.round(Math.min(1, Math.max(0, c)) * 255);

  return `rgb(${to255(r)}, ${to255(g)}, ${to255(b)})`;
}

/** 常量着色器值:字符串向量保留原样(逐效果解析),数字直接透传。 */
export function resolveShaderConstant(
  value: WEBound<number | string> | undefined,
  properties: Record<string, WEProperty>,
): number | string | undefined {
  return resolveBound<number | string>(value, properties);
}

/** 把常量值统一成数字(向量取第一分量)。 */
export function constantNumber(value: unknown, fallback: number): number {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string") {
    const first = Number(value.trim().split(/\s+/)[0]);

    if (Number.isFinite(first)) return first;
  }

  return fallback;
}

/** 把常量值统一成 [x, y] 向量。 */
export function constantVec2(
  value: unknown,
  fallback: [number, number],
): [number, number] {
  const v = parseVec(value, fallback as number[]);

  return [v[0] ?? fallback[0], v[1] ?? fallback[1]];
}

/** 把常量值统一成 [x, y, z] 向量。 */
export function constantVec3(
  value: unknown,
  fallback: [number, number, number],
): [number, number, number] {
  const v = parseVec(value, fallback as number[]);

  return [v[0] ?? fallback[0], v[1] ?? fallback[1], v[2] ?? fallback[2]];
}
