/*
 * TNO 主题（vendored 内置插件 · 路一收编）。
 *
 * 目录结构与第三方插件同构（manifest + activate 契约），但源码直接进仓库、
 * 静态打进主包：theme.css 经本模块的 import 进入 Vite 产物，首帧即在场，
 * 由 html[data-tno-theme="on"] 属性门控显隐（见 background.tsx 的
 * TNO_THEME_KEY / AppearanceSection 的「TNO 主题」开关），因此激活本身
 * 没有运行时成本，动态 import 的插件加载器不参与（无加载闪变）。
 *
 * 预留 activate(api)：将来主题要附带注册小组件/页面/设置项时，走与
 * 第三方插件完全相同的 PluginApi 通道（见 src/plugin/api.ts）。
 */
import "./theme.css";

import { registerUiTheme } from "../../plugin/ui-themes";

/** 与 plugin.yaml 同构的清单（vendored 版本，仅供宿主/文档引用，不进加载器索引） */
export const MANIFEST = {
  id: "tno-ui",
  name: "TNO 主题",
  version: "1.0.0",
  capabilities: { styles: true },
} as const;

export function activate(): void {
  /* 样式已随主包静态注入；主题经 registerUiTheme 进外观选择列表 */
}

registerUiTheme({
  id: MANIFEST.id,
  name: MANIFEST.name,
  apply(on) {
    document.documentElement.dataset.tnoTheme = on ? "on" : "off";
  },
});
