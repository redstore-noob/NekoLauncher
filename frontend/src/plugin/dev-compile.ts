/*
 * dev 模式运行时编译：把插件入口的 JSX 源码现场转成可在浏览器执行的 ESM。
 *
 * 设计边界（与规范一致）：
 *   - 只有清单声明 dev: true 的插件才走这条路径；生产分发物永远是编译后的
 *     index.js，宿主的生产加载路径上没有编译器；
 *   - Sucrase 懒加载：import() 触发 Vite 代码分割成独立 chunk，只有存在 dev
 *     插件时才会进内存，普通用户一个字节都不支付；
 *   - dev 源码里没有裸模块 import（浏览器解析不了，import map 尚未接入），
 *     前言把宿主注入的 api / h / Fragment / ui / icons 放进模块作用域，
 *     JSX 经 classic runtime 编译为对这些作用域标识符的调用。
 */

/** Sucrase 模块的进程内单例：首个 dev 插件触发加载，之后复用 */
let sucrasePromise: Promise<typeof import("sucrase")> | null = null;

function loadSucrase(): Promise<typeof import("sucrase")> {
  sucrasePromise ??= import("sucrase");

  return sucrasePromise;
}

/**
 * dev 模块顶部注入的作用域。
 * 编译产物不产生任何 import（classic JSX runtime），全靠这里暴露的标识符；
 * api 由 loader 在 import 之前挂到 globalThis 上（dev 插件串行加载，不会竞态）。
 */
const DEV_PREAMBLE = `\
const api = globalThis.__nekoPluginApi;
if (!api) throw new Error("dev 插件加载时序错误：宿主 API 未就绪");
const h = api.h;
const Fragment = api.Fragment;
const ui = api.ui;
const icons = api.icons;
`;

/** compileDevEntry 把 dev 插件的入口源码编译成可 import 的 ESM 文本 */
export async function compileDevEntry(
  source: string,
  filePath: string,
): Promise<string> {
  const { transform } = await loadSucrase();
  const { code } = transform(DEV_PREAMBLE + source, {
    transforms: ["jsx"],
    jsxRuntime: "classic",
    jsxPragma: "h",
    jsxFragmentPragma: "Fragment",
    filePath,
  });

  return code;
}
