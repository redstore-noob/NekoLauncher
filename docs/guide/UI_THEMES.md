# 界面主题开发指南

外观设置里的「界面主题」列表（`默认` + 各主题）由宿主的**主题注册表**驱动。本文说明
主题的注册契约与两种制作路径：收编进本体的 **vendored 插件**，以及第三方插件的
`api.registerUiTheme`。换肤用的 CSS 变量与语义类见
[CSS_STYLE_TABLE.md](CSS_STYLE_TABLE.md)。

## 1. 注册契约

注册表实现：[frontend/src/plugin/ui-themes.ts](../../frontend/src/plugin/ui-themes.ts)。

```ts
interface UiTheme {
  /** 稳定 id：存进 launcher.yaml 的 launcherUiTheme 键，发布后勿改名 */
  id: string;
  /** 展示名（zh-CN 原文，展示处经 t() 走 i18n） */
  name: string;
  /** 开/关主题。约定只操作 <html> 的 data-* 属性门控；关时必须清干净 */
  apply(on: boolean): void;
}

registerUiTheme({ id: "tno-ui", name: "TNO 主题", apply(on) { … } });
```

约定与语义：

- **「默认主题」不注册**——它就是全部主题都关闭的本体原貌，注册表里只有皮肤；
- `apply(on)` 只翻属性开关，**不做别的事**：主题 CSS 应当静态在场（随主包或插件
  样式通道），由 `html[data-<主题>]` 属性选择器门控，切换零延迟、首帧无闪；
- 关闭时宿主会逐个调 `apply(false)` 复位所有主题再点亮选中的，主题自己不用管别人；
- id 落盘持久化，改名 = 用户的已选主题回落默认。

## 2. 制作路径一：vendored 收编（当前开放）

参考实现：[frontend/src/plugins-vendored/tno-ui/](../../frontend/src/plugins-vendored/tno-ui/)。

1. 新建 `frontend/src/plugins-vendored/<主题id>/`，目录结构与第三方插件同构：
   - `theme.css`——主题本体，**全部规则挂在 `html[data-<主题id>="on"]` 门控下**；
   - `index.ts`——`MANIFEST` 清单常量 + `import "./theme.css"` + 模块顶层
     `registerUiTheme(...)`（见 tno-ui 的写法，`activate` 留空保持同契约）；
2. 在 [frontend/src/plugin/builtins.tsx](../../frontend/src/plugin/builtins.tsx) 加一行
   `import "../plugins-vendored/<主题id>";`——CSS 随主包静态分发，首帧即在场；
3. 完成。外观设置的主题列表自动出现新条目。

**为什么静态打包而不是运行时加载**：主题要在首帧前就绪（否则启动闪一次原貌），
而插件的运行时动态 import 天然晚于首帧。vendored 路径保住了"零闪变"，代价是主题
跟版本一起发、不能独立热更。

**换肤内容**：照 CSS_STYLE_TABLE「第一档（CSS 变量）」覆盖即可整体换色/换直角，
参考 tno-ui/theme.css——午夜蓝暗底（`--nya-surface-*`）、冷青主色阶梯
（`--heroui-primary-*`，需 `!important` 压过宿主内联写入的主题色）、全直角
（`--nya-radius-*` 八个档位一起清零）、CRT 扫描线（`body::before/::after`
属性门控覆盖层，`pointer-events: none`）。

## 3. 路径二：第三方插件（`api.registerUiTheme`）

插件在 `activate(api)` 里注册主题，出现在外观设置的「界面主题」列表：

```js
export default function activate(api) {
  // 样式本体走清单 styles 字段（推荐，存盘热生效）或 api.styles.inject；
  // apply 只操作 <html> 的 data-* 属性门控（与本文件 §1 契约一致）
  api.registerUiTheme({
    id: "midnight",              // 可省略，缺省用插件 id；宿主自动加 "<插件id>:" 前缀
    name: "午夜蓝",
    apply(on) { document.documentElement.dataset.midnight = on ? "on" : "off"; },
  });
}
```

规则（与插件系统的既有约定同构）：

- **需要权限 `styles`**——主题本质是全局样式覆盖，未声明直接抛错；
- `apply` 抛错被宿主隔离（记控制台警告），不会弄坏宿主与其它主题；
- 插件**卸载 / 重载 / 停用**时宿主自动摘除其全部主题；若被摘的是用户当前选中项，
  整体回落默认主题（全关），已选配置键保留原值——插件下次回来还能恢复；
- `api.unregisterUiTheme(id)` 可主动摘除自己的主题（传 `registerUiTheme` 返回的
  完整 id 或本插件内的短 id 均可）；
- 外观设置的主题列表随注册/摘除实时增删，无需刷新页面。

## 4. 既有开关与主题的关系

- **直角模式**（`launcherSquareCorners`）：独立的宿主开关，与主题正交——TNO 主题
  内部已清零圆角，两者叠加无冲突；
- **主题色 / 深色模式**：皮肤主题多为暗色底，用户在主题列表选中非默认主题时，
  宿主会把深浅模式切到深色（回默认不自动回切）。主题如果像 TNO 一样把亮色表面
  也压暗，深浅模式就只剩排版差异；
- **主题色阶梯**：`theme-color.tsx` 会把用户选的主色阶梯**内联**写在 `<html>` 上，
  内联优先级高于任何选择器——主题想固定自己的主色必须用 `!important`
  （tno-ui 即此写法），并在文档里注明这会盖过用户的主题色选择。
