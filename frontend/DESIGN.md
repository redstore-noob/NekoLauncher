# NyaLauncher 前端设计语言

> 适用范围：`frontend/src` 全部页面、小组件、弹窗与插件页。新增 UI 必须遵守本文档；
> 修改视觉前先确认改动与本文档的约定一致。

## 1. 技术基座

| 层 | 选择 |
| --- | --- |
| 组件库 | HeroUI（`@heroui/react`），**一切可交互控件优先用 HeroUI 组件**，不要手写 `<button>` / `<input>` / 自制弹窗 |
| 样式 | Tailwind CSS v4（CSS-first 配置，见 `styles/globals.css`，HeroUI 插件入口 `hero.ts`） |
| 动效 | framer-motion（全局 `MotionConfig reducedMotion="user"`），动画需有 CSS `prefers-reduced-motion` 兜底 |
| 图标 | Fluent UI System Icons（`@fluentui/react-icons`），常规尺寸 20px |
| 主题 | light / dark / system，class 模式（`.dark` 挂在 `<html>` 上，`theme.tsx` 负责） |

**原则：** 只允许三类"非 HeroUI"代码存在——
1. HeroUI 没有的能力（自定义标题栏、分段拖拽排序、canvas/3D 视图）；
2. 设计语言原语（本文档第 4 节列出的 `nya-*` 表面与 `SegmentedTabs`）；
3. 纯数据 / 无 UI 文件。
其余情况一律回归 HeroUI 组件。

## 2. 颜色与表面（Glassmorphism）

主题色由用户在「外观」中选取，`theme-color.tsx` 从 HeroUI 语义色派生出 HSL 变量：
`--nya-surface-1/2`、`--nya-border-c`、`--nya-shell`（窗口底色）与玻璃强度
`--nya-blur-scale`、`--nya-glass-alpha`。**不要写死十六进制背景色**，一律用变量或
`bg-primary/N`、`bg-default-N` 这类语义类。

标准表面工具类（定义于 `globals.css`，全部带 `backdrop-blur + saturate`）：

| 类 | 用途 |
| --- | --- |
| `nya-sidebar` | 侧边栏 |
| `nya-panel` / `nya-panel-strong` | 内容面板（两级强度） |
| `nya-panel-inner` | 面板内的次级区块 |
| `nya-modal-surface` | 弹窗主体（配合 `modal-shell.tsx`） |
| `nya-neon-card` | 主页小组件卡片：主题色描边 + 光晕；游戏运行中追加 `nya-neon-live` 呼吸动画 |
| `nya-border` | 主题色描边（卡片/滑条补 border 用） |

浮层（Popover / Dropdown / Select / Tooltip 的 `[data-slot="content"]`）由全局选择器
统一玻璃化 + `0.875rem` 圆角，**不要在单处再写浮层背景**。`html[data-panel-blur="off"]`
时全局关闭模糊并提高不透明度，因此表面样式必须同时在不模糊时好看。

Wails 透明窗口：`html, body, #root` 背景保持透明，由 `BackgroundLayer` 负责壁纸与压暗。

**无卡版式**（实例页 / 账户页 / 下载页）：这几页的内容区不铺 `nya-panel` 外壳，列表行也不铺
每行底色（`nya-row` 只该出现在弹窗内部），结构改由留白 + `nya-border` 细线（两栏之间 `md:border-l`
或 `xl:border-l`，窄窗口换成 `border-b` / `border-t`；区块之间同理）+ 小号大写区块标题
（`text-[11px] font-semibold uppercase tracking-wider text-gray-400`）承担；选中态是一条主题色
低透明度色带（`bg-primary/10`）配左侧 3px 强调线（`w-[3px] bg-primary`，账户页列表行），不用边框、
阴影或 `ring` 抬升。分段切换 `SegmentedTabs`（胶囊轨道 + 主色滑块）在这些位置一律换成文字形态：
页面主标签用下划线（`border-b-2` + `text-primary`），行内小筛选（资源平台、Java 提供商）用字重与
灰阶切换（选中 `font-semibold text-gray-900 dark:text-gray-100`，未选 `text-gray-400`）。
整块透明表面靠 `.nya-bg-scrim` 压暗层保证在壁纸上可读，弹窗与浮层仍按上表取玻璃表面。

## 3. 形状、字号、间距

- 圆角阶梯：**表面一律小圆角 `rounded-lg`（0.5rem）**——卡片、主页右侧大面板（启动页 /
  组件盒，两者同一外壳滑动互换，圆角必须一致）、弹窗表面、面板/输入框 `radius="lg"`、
  图标磁贴、面板内的次级区块与浮层通知（下载浮标 / NekoAlert）都走这一档。不再使用
  `rounded-xl` / `rounded-2xl` / `rounded-3xl`；`rounded-md` / `rounded-sm` 只用于气泡尾角
  这类局部收角，不作为整块表面圆角；胶囊与圆形（`rounded-full`）不受此约束。
- 拖动小组件时的删除区覆盖层（`DeleteZoneOverlay`）圆角跟随所在面板，遮罩层用
  `nya-delete-mask` + `backdrop-blur-*`，文字提示层浮在遮罩之上。
- 字号：数值展示 `text-2xl font-bold tabular-nums`；正文 `text-sm`；辅助/说明 `text-[11px]~text-xs`
  且用 `text-gray-400`（暗色 `dark:text-gray-400` 系）；卡片小标题 `text-[11px] font-semibold uppercase tracking-wider`。
- 卡片内距 `p-5`，内部纵向间距 `gap-3`。
- 图标磁贴统一 `size-10 rounded-lg bg-primary/15 text-primary`，不使用彩色渐变底。
- 滚动条全局隐藏，需要滚动的容器加 `nya-scroll`（6px 细滚动条）。

## 4. 动效

- 入场：`nya-enter`（统一淡入上浮）；侧边栏为逐项 stagger + 图标 pop。
- 滑块类切换动画优先用 framer-motion `layoutId` FLIP（见 `SegmentedTabs`）。
- 弹窗背景使用 CSS 关键帧 `nya-modal-backdrop/enter`（framer 卡顿规避），不要改回 JS 驱动。
- 谨慎新增常驻循环动画；目前仅 `nya-neon-live`（游戏运行呼吸光晕）一处。

## 5. 组件规范

- **弹窗**：一律走 `modal-shell.tsx`（`nya-modal-surface` + 统一圆角/动画），不要自开 HeroUI Modal 样式。
- **全局通知**：错误/警告/成功提示用 `overlay` 体系（`NekoAlert` 左下滑入 + `NekoPrompt`），
  语义色随 severity；**不要用 `alert()` 或自制 toast**。
- **设置项**：`layouts/settings/Section.tsx` 的 `Section` + `SettingRow`（label 左、控件右、
  hint 可选），并注册 `aliases` 供设置搜索命中。
- **主页小组件**：外壳一律用 `components/home/HomeCard.tsx`（HeroUI `Card` 基座 +
  `nya-neon-card`），图标、标题、数值的层级由它统一；不要自绘卡片框。
- **分段切换**：用 `components/segmented-tabs.tsx`（对齐 HeroUI Tabs 观感的动效原语，
  多实例时 `layoutId` 必须唯一），不要另造胶囊切换。
- **空状态**：一行居中 `text-xs text-gray-400`，不加插图不加按钮。

## 6. 文案规范（重要）

1. **不写显而易见的提示**。控件已经说明自己的事，不再配一句解释（例：「代理服务器无需认证时留空」、
   「修改后点击保存」）。hint 只保留不看就猜不到的信息（例：格式约束、占位符表达不了的默认值）。
2. **hint 不重复 placeholder**。`placeholder="127.0.0.1:7890"` 就不要再写「例如 127.0.0.1:7890」。
3. **不把内部实现透进 UI**。数据覆盖年份、版本计划之类属于 CHANGELOG，不属于界面。
4. **空状态统一句式**：`暂无X`（暂无目标 / 暂无服务器 / 暂无备份 / 暂无对话）。仅在操作
   入口不可见时才补一短句动作指引（例：`暂无文件，可新建或导入`），不写「点击…按钮」式的坐标导航。
5. **隐藏手势需一次性提示**：拖动排序、拖回移除这类不可见交互保留简短提示（「长按 1 秒排序」），
   但同一手势在多个位置只保留一处主提示。
6. **错误文案 = 结果 + 下一步**：先说失败原因（加粗），再给最短修复路径，不使用 emoji 装饰开头。
7. 语气：简洁、直述、不加「喵/哦/～」等语气词；中英混排时技术名词保持原文（HeroUI、API Key、KB/s）。
