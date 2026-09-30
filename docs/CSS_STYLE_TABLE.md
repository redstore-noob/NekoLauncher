# 可定制样式速查（插件 CSS 契约）

插件可以通过清单 `styles` 字段（见 [Extensions_Guide.md §4](Extensions_Guide.md)）或
`api.styles.inject`（§6，需 `styles` 权限）注入全局 CSS，自定义启动器任意控件的样式。
本文档列出**宿主承诺支持的定制锚点**——除此之外的类名（Tailwind 工具类、HeroUI 内部
结构）能改但不构成契约，升级可能悄悄失效。

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
| `--nya-radius-scale` | **全局圆角缩放系数**（缺省 `1`；`0.5` 直角化、`1.5` 更圆润） | 无关 |
| `--nya-radius-xs` ~ `--nya-radius-3xl` | 各档圆角基础值（缺省 `0.125`~`1.5rem`，与 Tailwind `rounded-*` 一一对应） | 无关 |
| `--nya-radius-panel` | 弹出面板（popover）圆角（缺省 `0.875rem`） | 无关 |

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

**示例——全局圆角调整：** 所有控件的 `rounded-*` 圆角都走 `--nya-radius-*` 变量
（组件约 400+ 处、含 HeroUI 插槽样式），因此改一个变量即可全局生效：

```css
:root {
  --nya-radius-scale: 0.5; /* 整体圆角减半，观感更硬朗 */
}
:root {
  --nya-radius-xl: 0.375rem; /* 或只调某一档：rounded-xl 单独变小 */
}
```

注意两点：

- 胶囊形（`rounded-full`、进度条/滚动条等的 `9999px`）不参与缩放，保持正圆语义；
  如确需改动请按选择器覆盖（见第 4 档说明）。
- 极小档位（`--nya-radius-xs` ~ `sm`）多用于小控件（代码片段、图片、小按钮），
  全局调太小时它们会先变成直角，属预期行为。

## 3. 第二档：`nya-*` 语义类

宿主自己命名的语义类名，定义集中在 [frontend/src/styles/globals.css](../frontend/src/styles/globals.css)。

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
| `.nya-instance-pill` / `.nya-instance-stagger` | 实例列表胶囊项与逐级进场 |
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
