/*
 * 插件加载器：从磁盘上的插件目录把插件拉进来，跑它们的注册函数。
 *
 * 插件资源由 Go 侧的 /plugins/ 路由投递（见 internal/bindings/plugin_handler.go）：
 *   GET /plugins/                 → { "plugins": [...], "disabled": [...] }
 *                                  索引载荷直接携带规范化后的清单（YAML 已在宿主侧解析），disabled 是其中被停用的
 *   GET /plugins/<id>/plugin.yaml  → 原始清单文件（loader 不再使用，走索引载荷）
 *   GET /plugins/<id>/<entry>     → 入口模块（ESM）
 *
 * 失败隔离：任一插件加载/执行出错只记日志并跳过，不影响其它插件与启动器本体；
 * 失败原因会记进运行时状态，供插件页展示。
 *
 * dev 模式：清单声明 dev: true 的插件入口是 JSX 源码，经 Sucrase 现场编译
 * （见 dev-compile.ts，懒加载）后走同一条 activate 契约；dev 插件串行加载，
 * 避免多个模块竞争全局 api 交接。生产插件不受影响，永远只加载编译后的 JS。
 */
import type { PluginApi, PluginManifest } from "./types";

import { useSyncExternalStore } from "react";

import { SetPluginDisabled } from "../../wailsjs/go/bindings/PluginAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { t } from "../i18n";

import { createPluginApi, PLUGIN_API_VERSION } from "./api";
import { compileDevEntry } from "./dev-compile";
import { runPluginCleanups, seedPluginSettings } from "./api";
import {
  activePluginIDs,
  isPluginActive,
  markPluginActive,
  unregisterPlugin,
} from "./registry";
import {
  PLUGIN_ROOT,
  injectPluginStyleFiles,
  removePluginStyles,
} from "./styles";

/** dev 插件经全局交接拿到宿主 API：loader 在 import dev 模块前设置，
 * dev 插件串行加载（见 devLoadChain）保证同一时刻只有一个模块在读它。 */
declare global {
  var __nekoPluginApi: PluginApi | undefined;
}

/**
 * 按运行时 URL 动态 import。
 *
 * 这里刻意用 new Function 把 import 藏起来，而不是直接写 `import(url)`：打包器会对它
 * 能看到的动态 import 注入查询参数（Vite 下变成 `?import`，对非源码资源直接 500），
 * 而插件路径只有运行时才知道，必须原样交给浏览器。
 */
const importModule = new Function("url", "return import(url)") as (
  url: string,
) => Promise<{ default?: unknown; activate?: unknown }>;

export interface PluginLoadResult {
  id: string;
  ok: boolean;
  error?: string;
}

/** 插件运行时状态：loaded=已加载 / failed=加载失败 / disabled=被停用 */
export type PluginRuntimeStatus = "loaded" | "failed" | "disabled";

export interface PluginRuntimeState {
  status: PluginRuntimeStatus;
  error?: string;
}

/** 已成功加载的插件 id 集合由 registry 的 activePlugins 维护（跨模块守卫需要） */

const runtimeStates = new Map<string, PluginRuntimeState>();
const stateListeners = new Set<() => void>();
let runtimeSnapshot: Record<string, PluginRuntimeState> = {};

function publishRuntime(id: string, state: PluginRuntimeState) {
  runtimeStates.set(id, state);
  runtimeSnapshot = Object.fromEntries(runtimeStates);
  stateListeners.forEach((listener) => listener());
}

function clearRuntime() {
  runtimeStates.clear();
  runtimeSnapshot = {};
  stateListeners.forEach((listener) => listener());
}

/**
 * 订阅各插件的运行时状态（插件页展示加载失败原因用）。
 * 这是运行时的真相：磁盘上的清单信息由 Go 侧的 PluginAPI 提供，两边在页面上拼起来。
 */
export function usePluginRuntimeStates(): Record<string, PluginRuntimeState> {
  return useSyncExternalStore(
    (listener) => {
      stateListeners.add(listener);

      return () => {
        stateListeners.delete(listener);
      };
    },
    () => runtimeSnapshot,
    () => runtimeSnapshot,
  );
}

/** loadPlugins 拉取并激活全部插件（跳过被停用的）；返回每个插件的加载结果 */
export async function loadPlugins(): Promise<PluginLoadResult[]> {
  bindPluginStyleWatcher();
  const { ids, disabled } = await fetchPluginIndex();

  disabled.forEach((id) => publishRuntime(id, { status: "disabled" }));

  return Promise.all(
    ids.filter((id) => !disabled.has(id)).map((id) => loadOne(id)),
  );
}

/**
 * unloadPlugin 卸载一个插件：先执行它的清理函数（事件订阅退订、插件自定义
 * 清理），再摘掉注册表项与活跃标记。卸载/重载的所有路径都必须走这里。
 */
function unloadPlugin(id: string): void {
  runPluginCleanups(id);
  removePluginStyles(id);
  unregisterPlugin(id);
  markPluginActive(id, false);
}

export { isPluginActive } from "./registry";

/**
 * unloadPluginRuntime 摘除单个插件的前端运行时（清理函数、样式、注册项）。
 * 供管理页在删除磁盘文件之前先调用：卸载流程先摘运行时再删目录，避免
 * 中间窗口里插件的事件订阅还活着、去 fetch 已被删除的资源。
 */
export function unloadPluginRuntime(id: string): void {
  unloadPlugin(id);
  publishRuntime(id, { status: "disabled" });
}

/** reloadPlugins 摘掉全部已注册插件后重新加载；用于插件页的"重新加载" */
export async function reloadPlugins(): Promise<PluginLoadResult[]> {
  for (const id of activePluginIDs()) unloadPlugin(id);
  clearRuntime();

  return loadPlugins();
}

/**
 * setPluginEnabled 启用/停用一个插件。
 *
 * 停用只摘掉它的注册项（布局里的小组件 id 会保留），启用则重新加载它。
 * 停用状态由 Go 侧写入配置，下次启动直接不加载。
 */
export async function setPluginEnabled(
  id: string,
  enabled: boolean,
): Promise<PluginLoadResult | null> {
  await SetPluginDisabled(id, !enabled);
  if (!enabled) {
    unloadPlugin(id);
    publishRuntime(id, { status: "disabled" });

    return null;
  }

  // 启用可能失败（清单坏 / 入口抛错）：把结果交回调用方展示，不能静默成功
  return loadOne(id);
}

/** 索引携带的规范化清单（YAML 已在宿主侧解析校验）：loadOne 直接查表，不再拉原始清单文件 */
let indexManifests = new Map<string, PluginManifest>();

async function fetchPluginIndex(): Promise<{
  ids: string[];
  disabled: Set<string>;
}> {
  try {
    const response = await fetch(PLUGIN_ROOT, {
      headers: { Accept: "application/json" },
    });

    if (!response.ok) {
      console.warn(
        t("[plugins] 读取插件索引失败（HTTP {0}），当作没有插件处理", {
          "0": response.status,
        }),
      );
      indexManifests = new Map();

      return { ids: [], disabled: new Set() };
    }
    const payload = (await response.json()) as {
      plugins?: PluginManifest[];
      disabled?: unknown;
    };

    indexManifests = new Map(
      (Array.isArray(payload?.plugins) ? payload.plugins : [])
        .filter(
          (manifest): manifest is PluginManifest =>
            !!manifest &&
            typeof manifest === "object" &&
            typeof manifest.id === "string",
        )
        .map((manifest) => [manifest.id, manifest]),
    );

    return {
      ids: [...indexManifests.keys()],
      disabled: new Set(toStringList(payload?.disabled)),
    };
  } catch (error) {
    // 开发服务器下没有这个路由，当作"没有插件"；但真实异常（后端未就绪等）
    // 不能与"确实没有插件"无法区分，至少落一条警告
    console.warn(
      t("[plugins] 读取插件索引失败：{0}，当作没有插件处理", {
        "0": error instanceof Error ? error.message : String(error),
      }),
    );
    indexManifests = new Map();

    return { ids: [], disabled: new Set() };
  }
}

function toStringList(value: unknown): string[] {
  if (!Array.isArray(value)) return [];

  return value.filter(
    (item): item is string => typeof item === "string" && item.length > 0,
  );
}

/**
 * bindPluginStyleWatcher 监听后端样式监听器的 plugin:styles:changed 事件：
 * 作者改插件 CSS 存盘后自动重新拉取并重注入，不用手点「重新加载」。
 * 只处理"已加载且清单确实声明了该文件"的插件——停用/卸载/无关插件的
 * 样式变化一律忽略（loadPlugins 幂等绑定，全程只挂一次）。
 */
let styleWatcherBound = false;

function bindPluginStyleWatcher(): void {
  if (styleWatcherBound) return;
  styleWatcherBound = true;

  EventsOn(
    "plugin:styles:changed",
    (payload: { pluginId?: string; files?: string[] } | undefined) => {
      const id = payload?.pluginId;

      if (!id || !isPluginActive(id)) return;
      const declared = indexManifests.get(id)?.styles ?? [];
      const changed = new Set(toStringList(payload?.files));
      const hit = declared.filter((file) => changed.has(file));

      // guard 复查：fetch CSS 期间插件被停用/卸载的话，迟到的响应直接丢弃
      if (hit.length > 0) void injectPluginStyleFiles(id, hit, isPluginActive);
    },
  );
}

/** dev 插件加载互斥链：全局 api 交接要求同一时刻只编译/导入一个 dev 模块 */
let devLoadChain: Promise<unknown> = Promise.resolve();

function enqueueDevModule<T>(task: () => Promise<T>): Promise<T> {
  const run = devLoadChain.then(task, task);

  devLoadChain = run.catch(() => undefined);

  return run;
}

/**
 * loadDevModule 加载一个 dev 插件的入口：拉取 JSX 源码 → Sucrase 现场编译 →
 * blob URL 动态 import，返回编译产物的模块命名空间。模块顶层的
 * `const api = globalThis.__nekoPluginApi` 由前言注入，activate 收到的也是
 * 同一个 api 对象。
 */
async function loadDevModule(
  entryUrl: string,
  api: PluginApi,
): Promise<{ default?: unknown; activate?: unknown }> {
  const previous = globalThis.__nekoPluginApi;

  globalThis.__nekoPluginApi = api;
  try {
    const response = await fetch(entryUrl);

    if (!response.ok) {
      throw new Error(
        t("读取 {0} 失败（HTTP {1}）", {
          "0": "入口源码",
          "1": response.status,
        }),
      );
    }
    const source = await response.text();
    let code: string;

    try {
      code = await compileDevEntry(
        source,
        entryUrl.split("/").pop() ?? "index.jsx",
      );
    } catch (error) {
      // 转换失败几乎都是"收到的不是源码"（路由/回退页/缓存页）：
      // 附上内容开头，一眼看出宿主实际拿到了什么
      throw new Error(
        `${error instanceof Error ? error.message : String(error)}；收到内容开头：${JSON.stringify(source.slice(0, 120))}`,
      );
    }
    // 编译产物不落盘：dev 插件随时在改，blob 一次性的
    const blobUrl = URL.createObjectURL(
      new Blob([code], { type: "text/javascript" }),
    );

    try {
      return await importModule(blobUrl);
    } finally {
      URL.revokeObjectURL(blobUrl);
    }
  } finally {
    // 归还/清掉全局：只有仍是自己设置的才动它，避免误伤后续交接
    if (globalThis.__nekoPluginApi === api) {
      globalThis.__nekoPluginApi = previous;
    }
  }
}

async function loadOne(id: string): Promise<PluginLoadResult> {
  try {
    // 清单来自索引缓存；缓存里没有（启动后新装的插件）时刷新一次索引
    let manifest = indexManifests.get(id);

    if (!manifest) {
      await fetchPluginIndex();
      manifest = indexManifests.get(id);
    }
    if (!manifest) {
      throw new Error(t("插件清单缺失或非法：{0}", { "0": id }));
    }

    if (manifest.apiVersion !== PLUGIN_API_VERSION) {
      throw new Error(
        t("API 版本不匹配：宿主为 {0}，插件声明 {1}", {
          "0": PLUGIN_API_VERSION,
          "1": manifest.apiVersion,
        }),
      );
    }

    // dev 插件的入口默认是 JSX 源码，运行时编译；生产插件仍是编译后的 JS
    const entry = manifest.entry || (manifest.dev ? "index.jsx" : "index.js");
    const entryUrl = `${PLUGIN_ROOT}${encodeURIComponent(id)}/${entry
      .split("/")
      .map((segment) => encodeURIComponent(segment))
      .join("/")}`;

    // 重载同名插件：先摘掉旧注册项，再创建新 api。顺序不能反——
    // runPluginCleanups 会从登记表里删掉该插件的清理集合，若放在
    // createPluginApi 之后，删掉的就是新 api 刚登记的集合，此后卸载
    // 找不到任何清理函数（事件订阅与插件定时器全部泄漏）。
    if (isPluginActive(id)) unloadPlugin(id);

    // 生产插件直接原生 import 编译后的 JS；dev 插件走"拉源码 → 现场编译 →
    // blob import"，拿到的是编译产物的模块命名空间。
    // 浏览器的 "Failed to fetch dynamically imported module" 不带上下文，
    // 这里补上实际入口名，方便发现"dev 插件没写 dev: true"这类配置错位。
    const api = createPluginApi(manifest);
    const module = await (manifest.dev
      ? enqueueDevModule(() => loadDevModule(entryUrl, api))
      : importModule(entryUrl).catch((error: unknown) => {
          throw new Error(
            `${error instanceof Error ? error.message : String(error)}（入口：${entry}；dev 插件的入口应为 index.jsx 且清单需声明 dev: true）`,
          );
        }));
    const activate = module.default ?? module.activate;

    if (typeof activate !== "function") {
      throw new Error(
        t(
          "插件入口没有导出函数（需要 export default 或 export function activate）",
        ),
      );
    }

    // 重载同名插件的旧注册项已在前面摘除；这里先挂活跃标记再注入样式：
    // 样式文件的 fetch 是异步的，isPluginActive 由此对注入路径生效
    markPluginActive(id, true);
    // 清单 styles 先于 activate 注入：插件激活时它的样式已经生效
    await injectPluginStyleFiles(id, manifest.styles ?? [], isPluginActive);
    // settings 种子先于 activate 写入：插件激活时就能读到自己的默认设置
    await seedPluginSettings(manifest, api);
    await activate(api);
    publishRuntime(id, { status: "loaded" });

    return { id, ok: true };
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);

    console.warn(t("[plugins] {0} 加载失败：{1}", { "0": id, "1": message }));
    // 注册到一半失败时清掉已注册的部分，避免留下半个插件
    // （unloadPlugin 内部会同时摘掉活跃标记）
    unloadPlugin(id);
    publishRuntime(id, { status: "failed", error: message });

    return { id, ok: false, error: message };
  }
}
