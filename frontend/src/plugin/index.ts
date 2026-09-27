/*
 * 插件系统入口：注册表、宿主 API、加载器与内置扩展点统一从这里导出。
 * 宿主自身只依赖这个入口，不直接引用内部文件。
 */
export {
  DEFAULT_PAGE_ID,
  DEFAULT_WIDGET_IDS,
  pageById,
  registerPage,
  registerWidget,
  unregisterPlugin,
  usePages,
  useWidgets,
  widgetById,
} from "./registry";
export { createPluginApi, PLUGIN_API_VERSION } from "./api";
export { PageHost, WidgetHost } from "./render";
export {
  loadPlugins,
  reloadPlugins,
  setPluginEnabled,
  usePluginRuntimeStates,
} from "./loader";
export type {
  PluginLoadResult,
  PluginRuntimeState,
  PluginRuntimeStatus,
} from "./loader";
export { registerBuiltins } from "./builtins";
export type {
  PageDefinition,
  PluginApi,
  PluginActivate,
  PluginManifest,
  SelectedAccountSummary,
  WidgetDefinition,
  WidgetRenderContext,
} from "./types";
