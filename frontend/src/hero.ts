/*
 * Copyright 2024 Next UI
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
import { heroui } from "@heroui/theme";

// HeroUI 基础主题。组件外观定制（浮层菜单毛玻璃等）不走这里——该版本插件
// 配置不支持组件插槽，统一定义在 globals.css 的 [data-slot=...] 选择器里。
//
// 注意：本文件不是「未被引用的死代码」。它由 src/styles/globals.css 的
// `@plugin '../hero.ts'` 指令加载（Tailwind v4 插件入口），
// 因此只在 TS 源码里搜索 import 是找不到引用方的。
export default heroui();
