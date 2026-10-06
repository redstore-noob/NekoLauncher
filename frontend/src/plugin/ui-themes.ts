/*
 * 界面主题注册表：默认主题之外的皮肤（本体 vendored 插件、第三方插件经
 * api.registerUiTheme）在这里注册，外观设置据 registry 渲染主题列表。
 *
 * 主题 = 名字 + apply(on) 开关。约定 apply 只操作 <html> 的 data-* 属性门控，
 * 主题 CSS 随主包（vendored）或插件样式通道（第三方）静态在场，因此切换
 * 零延迟、无加载闪变；"默认主题"不注册（它就是全部主题都关闭的状态）。
 */

export interface UiTheme {
  /** 稳定 id：存进 launcherUiTheme，勿改名（同插件 id 的持久性要求） */
  id: string;
  /** 展示名（i18n 原文，展示处经 t()） */
  name: string;
  /** 开/关主题。开时写自己的属性门控；关时必须清干净（选中其它主题时由
   * 背景层统一逐个调 apply(false) 复位，主题自己不用管别人的事） */
  apply(on: boolean): void;
}

/** DEFAULT_UI_THEME_ID 默认主题：全部注册主题都关闭的本体原貌 */
export const DEFAULT_UI_THEME_ID = "default";

const registry = new Map<string, UiTheme>();

/** 当前选中的主题 id（applyUiTheme 记录；注册表变动时据此决定是否回落默认） */
let currentThemeId = DEFAULT_UI_THEME_ID;

/** 订阅通知：插件注册/注销主题时通知外观设置重渲染主题列表 */
const listeners = new Set<() => void>();

/** 快照缓存：useSyncExternalStore 的 getSnapshot 必须返回稳定引用 */
let snapshot: UiTheme[] = [];

function refreshSnapshot(): void {
  snapshot = [...registry.values()];
}

function notifyChange(): void {
  refreshSnapshot();
  for (const listener of listeners) listener();
}

/** subscribeUiThemes 订阅注册表变化；返回取消订阅函数（useSyncExternalStore 用） */
export function subscribeUiThemes(listener: () => void): () => void {
  listeners.add(listener);

  return () => {
    listeners.delete(listener);
  };
}

/** registerUiTheme 注册一个界面主题；同 id 重复注册覆盖（热重载友好） */
export function registerUiTheme(theme: UiTheme): void {
  registry.set(theme.id, theme);
  notifyChange();
}

/** unregisterUiTheme 注销主题；若它正被选中，先全关（含清掉被摘主题自己的
 * 属性门控）再从注册表移除。插件卸载/停用时的自动摘除走这里 */
export function unregisterUiTheme(id: string): void {
  const removed = registry.get(id);

  if (!removed) return;

  if (currentThemeId === id) {
    currentThemeId = DEFAULT_UI_THEME_ID;

    // 全部复位：被摘主题清掉自己的属性门控，其余主题本就应为关，一并复位兜底
    for (const theme of registry.values()) {
      theme.apply(false);
    }
  }

  registry.delete(id);
  notifyChange();
}

/** getUiThemes 全部已注册主题（注册序，默认主题不在其中）。返回缓存快照，
 * 引用在下一次注册表变动前保持稳定（供 useSyncExternalStore 使用） */
export function getUiThemes(): UiTheme[] {
  return snapshot;
}

export function getUiTheme(id: string): UiTheme | undefined {
  return registry.get(id);
}

/** applyUiTheme 应用指定主题：先把所有主题复位，再点亮选中的（id 不存在 =
 * 默认主题，等价于全关）。记录 currentThemeId 供注销时回落判断 */
export function applyUiTheme(id: string): void {
  currentThemeId = registry.has(id) ? id : DEFAULT_UI_THEME_ID;

  for (const theme of registry.values()) {
    theme.apply(theme.id === currentThemeId);
  }
}
