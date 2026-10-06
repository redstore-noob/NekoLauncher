# 可定制样式速查（插件 CSS 契约）

插件可以通过清单 `styles` 字段（见 [QUICKSTART.md](QUICKSTART.md)）或
`api.styles.inject`（§6，需 `styles` 权限）注入全局 CSS，自定义启动器任意控件的样式。
本文档列出**宿主承诺支持的定制锚点**——除此之外的类名（Tailwind 工具类、HeroUI 内部
结构）能改但不构成契约，升级可能悄悄失效。

想做可在外观设置里被选中的**界面主题**（整体换肤、随选择持久化），见
[UI_THEMES.md](UI_THEMES.md)。

> **兼容承诺**：下文「第一档（CSS 变量）」与「第二档（`nya-*` 语义类）」中的每一项
> 都是宿主公共接口。改名/删除/语义变更会在更新日志中明确标注；新增只增不改。
> 其余一切选择器均为实现细节，随时可能变化。

## 1. 使用方式回顾

```yaml
# plugin.yaml：静态样式（推荐，存盘即热生效）
styles:
  - theme.css
```

```js
// 运行时动态样式（需 capabilities 声明 styles: true）
api.styles.inject(":root { --nya-glass-alpha: 0.9 }", "glass");
api.styles.remove("glass");
```

## 2. 第一档：CSS 变量（最稳，换肤首选）

宿主的主题系统由这些变量驱动，覆盖它们即可整体换色，不受类名变动影响。
**格式均为 HSL 通道值**（如 `212 100% 47%`，不含 `hsl()` 包裹），使用时写作
`hsl(var(--nya-surface-1) / 0.8)`。

### 主题色（HeroUI）

| 变量 | 含义 |
|---|---|
| `--heroui-primary` | 主题主色（500 档），影响全部强调色元素 |
| `--heroui-primary-50` … `--heroui-primary-900` | 主色明暗阶梯（50/100/200/300/400/500/600/700/800/900） |
| `--heroui-primary-foreground` | 主色上的前程色（按钮文字），亮主色应给深色 |

### 启动器自有变量（nya）

| 变量 | 含义 | 明暗主题 |
|---|---|---|
| `--nya-surface-1` | 一级表面色（侧边栏、面板底色） | 自动切换 |
| `--nya-surface-2` | 二级表面色（更亮的浮层面板） | 自动切换 |
| `--nya-border-c` | 通用描边色 | 自动切换 |
| `--nya-surface-1-light` / `--nya-surface-1-dark` | 一级表面的亮/暗值 | 手动分别覆盖 |
| `--nya-surface-2-light` / `--nya-surface-2-dark` | 二级表面的亮/暗值 | 手动分别覆盖 |
| `--nya-border-light` / `--nya-border-dark` | 描边的亮/暗值 | 手动分别覆盖 |
| `--nya-shell` | 最外层底壳色（RGB 通道，如 `3 7 18`） | 自动切换 |
| `--nya-blur-scale` | 毛玻璃模糊半径倍率（缺省 `1`，调小降模糊省性能） | 无关 |
| `--nya-glass-alpha` | 表面不透明度（缺省 `0.8`，配合 blur 做玻璃质感） | 无关 |
| `--nya-radius-sm` ~ `--nya-radius-3xl` | Tailwind 档圆角基础值（`rounded-sm`~`rounded-3xl` 一一对应；缺省与 Tailwind 原刻度一致，`0.25rem`~`1.5rem`） | 无关 |
| `--nya-radius-medium` / `--nya-radius-large` | HeroUI 档圆角（`rounded-medium`=12px / `rounded-large`=14px；HeroUI 组件插槽样式也消费这两档） | 无关 |

**示例——全局换成绿色主题 + 更实的毛玻璃：**

```css
:root {
  --heroui-primary: 142 71% 45%;
  --heroui-primary-foreground: 0 0% 100%;
  --nya-glass-alpha: 0.92;
}
```

只覆盖 `--heroui-primary` 时，500 档阶梯与表面色不会自动跟随（它们由宿主按所选
主题色计算写入）——想成套换色请把主色阶梯和 `nya` 表面变量一起覆盖，或直接改用
设置页的预设主题色。

**示例——全局圆角调整：** 全部控件的 `rounded-*` 圆角都转发到 `--nya-radius-*`
变量（Tailwind 档经 `@theme` 转发、HeroUI 档在未分层 `:root` 转发，组件约 488 处
调用点、含 HeroUI 插槽样式），因此改一组变量即可全局生效：

```css
:root {
  --nya-radius-lg: 0.375rem; /* 只调某一档：rounded-lg 单独变小 */
}
/* 整体直角化：全部档位清零（宿主内置的「直角模式」开关即此写法，
 * 见 <html data-square-corners="true"> 门控；vendored 的 TNO 主题同样
 * 在自己的属性门控里清零这套变量） */
:root {
  --nya-radius-sm: 0px;
  --nya-radius-md: 0px;
  --nya-radius-lg: 0px;
  --nya-radius-xl: 0px;
  --nya-radius-2xl: 0px;
  --nya-radius-3xl: 0px;
  --nya-radius-medium: 0px;
  --nya-radius-large: 0px;
}
```

注意两点：

- **正圆不在这套 token 里**：`rounded-full` 是写死的超大半径（头像、开关旋钮、
  进度条等），保持正圆语义；如确需改方请按选择器覆盖（见第 4 档说明）——
  但开关旋钮压平会不可用，三思。
- 主题插件做"整主题直角"时请把上面 8 个变量一起清零，只清 Tailwind 档会漏掉
  HeroUI 组件（弹窗、按钮、卡片壳）。

## 3. 第二档：`nya-*` 语义类

宿主自己命名的语义类名，定义集中在 [frontend/src/styles/globals.css](../../frontend/src/styles/globals.css)。

### 容器与面板

| 类名 | 对应控件 |
|---|---|
| `.nya-panel` | 通用面板容器（毛玻璃 + 描边） |
| `.nya-panel-strong` | 强面板（不透明度更高） |
| `.nya-panel-inner` | 面板内嵌的次级内容块 |
| `.nya-border` | 通用描边（颜色取 `--nya-border-c`） |
| `.nya-neon-card` | 主页小组件卡片壳（插件小组件也被自动装入此壳） |
| `.nya-bg-scrim` | 背景图上的压暗遮罩 |

### 侧边栏

| 类名 | 对应控件 |
|---|---|
| `.nya-sidebar` | 侧边栏本体 |
| `.nya-sidebar-item` | 侧边栏按钮项（含进场动画） |
| `.nya-sidebar-item-active` | 当前选中项 |
| `.nya-sidebar-icon` | 项内图标（悬停/选中弹跳动画） |
| `.nya-sidebar-cursor` | 选中项背后的滑动光标 |

### 弹窗与过渡

| 类名 | 对应控件 |
|---|---|
| `.nya-modal-backdrop` | 弹窗遮罩层 |
| `.nya-modal-surface` | 弹窗面板本体 |
| `.nya-modal-enter` | 弹窗进场动画 |
| `.nya-enter` / `.nya-stagger-1`~`3` | 通用进场动画与逐级延迟 |
| `.nya-card-in` | 卡片进场动画 |
| `.nya-bg-fade` | 背景图淡入 |

### 其它

| 类名 | 对应控件 |
|---|---|
| `.nya-scroll` | 自定义滚动条区域（`::-webkit-scrollbar` 可覆写） |
| `.nya-bar` / `.nya-hold-bar` | 进度条 / 长按进度条 |
| `.nya-markdown` | Markdown 渲染容器（`p`/`h1`~`h6`/`code`/`pre`/`table` 等子选择器） |
| `.nya-eq-bar` / `.nya-vinyl` / `.nya-cover-glow` | 音乐播放器均衡条 / 黑胶 / 封面光晕 |
| `.nya-instance-stagger` | 实例列表逐级进场（选中高亮直接画在选中按钮上，无独立类） |
| `.nya-drag-ghost` / `.nya-drop-line` | 小组件拖动的幽灵条与落点指示线 |
| `.nya-mc-obfuscated` | MC 风格乱码字符效果 |

## 4. 第三档：HeroUI 组件（不构成契约，后果自负）

HeroUI 组件暴露 `data-slot` 属性，可以不依赖 Tailwind 类名地选中：

```css
/* 例：所有按钮改方角 */
[data-slot="button"] { border-radius: 6px; }
```

这些属性来自上游库、宿主不可控，且组件内部结构（嵌套的 slot、伪元素）没有
稳定承诺。适合个人微调，**不要**在发布给他人的插件里依赖第三档选择器。

## 5. 明确不建议做的

- **覆盖 `body` / `#app` 之外的全局 reset**：会波及 Wails 的 WebView 容器行为；
- **`!important` 满天飞**：插件样式的 `<style>` 挂载顺序晚于宿主样式，同特异性
  下本来就是插件赢，滥用 `!important` 只会让用户其它插件没法再改回；
- **依赖第三档选择器做主题分发**：你的用户会在某次升级后回来找你。

## 6. 调试方法

- 每条插件样式以 `<style data-plugin-id="<插件id>" data-plugin-key="<key>">`
  挂在 `document.head` 末尾——DevTools 里按 `data-plugin-id` 过滤即可看到某个
  插件注入了什么；
- 清单 `styles` 文件的 key 是 `file:<路径>`，运行时 API 缺省 key 是 `inline`；
- 改清单声明的 CSS 文件存盘后约 3 秒内自动热生效（宿主监听文件变化），无需
  手动「重新加载插件」。
