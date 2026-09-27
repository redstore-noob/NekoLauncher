/*
 * Copyright 2026 烟花
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/**
 * 后端绑定数据的入口守卫。
 *
 * Go 绑定失败/空值时会返回 null，而 `?? 默认值` 只能拦住 null/undefined——
 * 一旦拿到形状不对的非空数据（如对象冒充数组），`.map()` / `.split()` 仍会把
 * 卡片带走。这里统一按"形状校验"兜底，入口处洗成可信数据。
 */

/** 保证得到数组：非数组（null / 对象 / 其它）一律回落为空数组 */
export function asArray<T>(value: unknown): T[] {
  return Array.isArray(value) ? (value as T[]) : [];
}

/** 保证得到字符串：非字符串（null / 对象 / 其它）一律回落为空串 */
export function asText(value: unknown): string {
  return typeof value === "string" ? value : "";
}

/** 保证得到对象：空值或非对象一律回落为 null，由调用方决定展示兜底 */
export function asObject<T extends object>(value: unknown): T | null {
  return value !== null && typeof value === "object" ? (value as T) : null;
}
