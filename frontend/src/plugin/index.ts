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
  setLaunchCardOverride,
  unregisterPlugin,
  useLaunchCardOverride,
  usePages,
  useWidgets,
  widgetById,
} from "./registry";
export { createPluginApi, PLUGIN_API_VERSION } from "./api";
export { LaunchCardHost, PageActionSlot, PageHost, WidgetHost } from "./render";
export {
  isPluginActive,
  loadPlugins,
  reloadPlugins,
  setPluginEnabled,
  unloadPluginRuntime,
  usePluginRuntimeStates,
} from "./loader";
export type {
  PluginLoadResult,
  PluginRuntimeState,
  PluginRuntimeStatus,
} from "./loader";
export { registerBuiltins } from "./builtins";
export {
  clearPluginGrants,
  DEFAULT_OFF_PERMISSIONS,
  defaultPermissionGrant,
  disabledPermissions,
  hydratePluginGrants,
  isPermissionGranted,
  NETWORK_PERMISSION,
  PLUGIN_GRANTS_CONFIG_KEY,
  setPluginPermission,
  usePluginGrantsVersion,
} from "./grants";
export type {
  DownloadTaskKind,
  DownloadTaskPhase,
  DownloadTaskSummary,
  GameExitInfo,
  LaunchCardContext,
  LaunchCardDefinition,
  LogLineBatch,
  LogLineOptions,
  PageActionDefinition,
  PageDefinition,
  PluginApi,
  PluginActivate,
  PluginManifest,
  SelectedAccountSummary,
  WidgetDefinition,
  WidgetRenderContext,
} from "./types";
