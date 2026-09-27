/*
 * 测试环境准备（vitest setupFiles）：
 *   - 固定语言为 zh-CN：原文即译文，断言里的字符串就是源码里的中文，稳定且可读；
 *   - 挂上 wails runtime 的浏览器兜底（wails-mock），避免任何绑定调用把测试打崩。
 */
import "./wails-mock";

import { setActiveLocale } from "./i18n";

setActiveLocale("zh-CN");
