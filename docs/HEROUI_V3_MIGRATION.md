# HeroUI v2 → v3 迁移方案（评估稿）

> 状态：**仅评估，未执行**。本文档不改变任何依赖或业务代码。
> 目标版本：`@heroui/react@3.2.6`（当前 latest）
> 当前版本：`@heroui/react@2.6.14` + `@heroui/theme@2.4.26` + `@heroui/system@2.4.27`
>
> 配套文档：**[HEROUI_V3_INVENTORY.md](HEROUI_V3_INVENTORY.md)** —— 逐项改动清单
> （组件改名表、逐组件 prop 迁移、颜色变量映射、逐文件工作分组）。
> 本文只讲**策略、阶段与风险**。

## 0. 结论摘要

**不建议现在迁移。** 理由不是"工作量"，而是**收益与风险不匹配**：

- 触发本次调查的 Select 外观异常，根因已定位并修复（见 [§1](#1-本次-select-外观问题的真实根因)），
  **与 v3 无关**。修完后 Select 在 v2 下已恢复官方示例的观感。
- v3 是一次**架构换代**，不是版本升级：它把约 50 个子包合并为 2 个、换掉整套颜色系统、
  移除 Provider 与 framer-motion 集成。本项目在这三处都有深度耦合。
- **v2 线已冻结在 2.6.14**（registry 无维护 tag），意味着留在 v2 不需要持续跟版，
  是一个稳定可预期的状态。

如果确实要迁移，请按 [§6 分阶段方案](#6-分阶段迁移方案) 执行，并务必先完成
[阶段 0：React 19 前置验证](#阶段-0react-19-前置验证必须先做可独立回滚)。

---

## 1. 本次 Select 外观问题的真实根因

`frontend/src/styles/globals.css` 用 `[data-slot="popover"]` 给下拉面板加毛玻璃与圆角。
该属性**从未出现在最终 DOM 上**：

1. `@heroui/select` 把 `"data-slot": "popover"` 传给 `FreeSoloPopover`
   （`@heroui/select/dist/index.js:397`）
2. `FreeSoloPopover` 内部的 `getDialogProps()` 把它**覆盖**为 `"data-slot": "base"`
   （`@heroui/popover/dist/free-solo-popover.js:239`），内容层是 `"data-slot": "content"`（:253）

于是这条规则永不命中，Select 面板保持 `popoverContent` 槽的默认类
`"w-full p-1 overflow-hidden"` —— **直角、无毛玻璃**。这就是"跟官方示例不一样"。

已修复：选择器改为实际存在的 `[data-slot="content"]:not([data-disabled])`，
并新增 `frontend/src/styles/globals.test.ts` 守住「CSS 用到的槽位必须真实被渲染」这一契约
（已验证该测试能捕获原始 bug）。

> 顺带发现的依赖漂移（`@heroui/react` 精确声明 `theme@2.4.6`/`system@2.4.7`，
> 但 `package.json` 的 `overrides` 强行提到 `2.4.26`/`2.4.27`）属于**独立问题**，
> 见 [§7 附：v2 内部的依赖对齐选项](#7-附v2-内部的依赖对齐选项)。

## 2. v3 到底是什么：架构换代

| 维度 | v2（现状） | v3 |
|---|---|---|
| 包数量 | `@heroui/react` + `theme` + `system` + ~50 子包 | `@heroui/react` + `@heroui/styles`（2 个） |
| 样式入口 | Tailwind 插件 `heroui()`（`src/hero.ts`） | 纯 CSS：`@import "@heroui/styles";`，**删除插件与 `hero.ts`** |
| Provider | `HeroUIProvider`（`@heroui/system`） | **移除**，不再需要 |
| 动效 | peer-dep `framer-motion`，接受 `motionProps` | 无 framer-motion peer |
| 颜色系统 | HSL，`primary`/`secondary` + `50~900` 阶梯 | **OKLCH，`primary`→`accent`，`secondary` 与数字阶梯移除** |
| 槽位定制 | `classNames={{ content: ... }}` | **`classNames` prop 移除**，改用子组件 `className` |
| 集合项 | `SelectItem key=` | `ListBox.Item id=` + `textValue=` |
| 数据槽 | `data-slot="content"` 等 | **整套改名**，`content` 不复存在（→ `select-popover`） |

## 3. 硬性前置条件

| 条件 | v3 要求 | 本项目 | 状态 |
|---|---|---|---|
| React | `>=19.0.0` | 18.3.1 | ❌ **阻塞** |
| React DOM | `>=19.0.0` | 18.3.1 | ❌ **阻塞** |
| Tailwind CSS | `>=4.0.0` | 4.1.11 | ✅ 已满足 |
| `react-aria` | `^3.52.1` | 未直接安装 | ⚠️ 需新增 |
| `react-aria-components` | `^1.21.1` | 未安装 | ⚠️ 需新增 |
| `@react-aria/ssr` | `^3.10.1` | 未安装 | ⚠️ 需新增 |
| `@react-aria/utils` | `^3.34.1` | 间接依赖 | ⚠️ 需提升 |
| `@internationalized/date` | `^3.12.4` | 间接依赖 | ⚠️ 需提升 |

**只有 React 19 是真正的阻塞项。** Tailwind v4 前置条件本项目早已满足
（`tailwindcss@4.1.11`，CSS-first 配置），这比一般 v2 项目的情况好很多。

> 更正：早期调研把 Tailwind v4 也列为阻塞项，那是通用结论；
> 对本项目应修正为**已满足**。

## 4. 影响面量化（实测）

统计范围 `frontend/src`，排除 `node_modules`。

### 4.1 组件用量（Top）

| 组件 | 实例数 | 组件 | 实例数 |
|---|---|---|---|
| `Button` | 71 | `Spinner` | 16 |
| `Input` | 32 | `Textarea` | 9 |
| `Modal` / `ModalContent` | 26 / 26 | `Slider` | 9 |
| `Select` | **39** | `Tooltip` | 8 |
| `SelectItem` | **52** | `Progress` | 8 |
| `Switch` | 18 | `Dropdown*` | 3 组 |

`Select` 分布在 **23 个文件**；导入的 `@heroui/*` 包只有 3 个
（`react`、`system`、`theme`），迁移面收敛得很好。

### 4.2 会被 v3 破坏的 API 用量

| v2 用法 | 次数 | v3 对应 |
|---|---|---|
| `onSelectionChange=` | 43 | → `onChange`（Set → 标量/数组） |
| `selectedKeys=` | 42 | → `value` |
| `<SelectItem` | 52 | → `ListBox.Item id=` |
| `popoverProps=`（含 motionProps） | 30 | **移除** |
| `classNames={{ ... }}` | 60 | **移除** |
| `radius="..."` | 115 | **移除**（改用 Tailwind） |
| `color="primary"` | 107 | → `color="accent"` |
| `bg/text/border-primary*` | 307 + 38 | → `accent` 系列 |
| `bg/text/border-default-<n>` | 166 | 调色板重命名 |
| `dropdown/tooltipMotionProps` | 18 | **移除** |
| `disallowEmptySelection` | 6 | 未确认支持（见 §5.2） |
| `HeroUIProvider` | 3 | 移除 |
| `label=`（Select/Input 混计） | 47 | `label` prop → `<Label>` 子组件 |

### 4.3 主题层耦合

- `frontend/src/hero.ts` → v3 需**删除**（插件被 CSS 导入取代）
- `globals.css` 中 `@plugin '../hero.ts'`、`@source .../@heroui/theme/dist/...` → 需重写
- `--heroui-primary-*` 变量引用：`globals.css` 14 处 + `theme-color.tsx` 6 处
- `frontend/src/theme-color.tsx` 通过内联 `--heroui-primary-50~900` 实现换肤 →
  在 v3 需整套改写为 OKLCH + `accent` 命名，且数字阶梯已不存在
- `framer-motion`：12 个文件直接 import（作为直接依赖可保留），
  但 48 处把 motion props **传进 HeroUI 组件**的用法会失效

## 5. 三类「静默失败」风险（最高优先级）

这三项**不报错、不失败编译**，只在运行时表现为外观退化：

### 5.1 `data-slot` 整套改名
v3 的 67 个槽位名中**没有 `content`**。Select 相关为
`select`、`select-trigger`、`select-value`、`select-indicator`、`select-popover`。
本项目 `globals.css` 有 4 处 `[data-slot="content"]` 规则，迁移后会**全部失效且无提示**。

→ 缓解：本次新增的 `globals.test.ts` 正是为此设计，升级依赖后它会失败并给出可用槽位列表。

### 5.2 `classNames` prop 移除
60 处使用。v3 只能把 `className` 挂到对应子组件上，v2 的按槽位定制能力丧失。

### 5.3 颜色系统改写
`primary`→`accent`、`secondary` 删除、`50~900` 阶梯删除、HSL→OKLCH。
影响 107 处 `color=` 与 345+ 处配色类名，且 `theme-color.tsx` 的换肤机制需重做。
**这一项的改动量可能超过 Select 本身。**

### 5.4 未确认项
`disallowEmptySelection`（6 处使用）在 v3 的迁移表与 API 表中均未出现，
**无法确认是透传给 RAC 还是被丢弃**。迁移前必须在运行时验证。

## 6. 分阶段迁移方案

### 阶段 0：React 19 前置验证（必须先做，可独立回滚）
1. 在**独立分支**上把 `react`/`react-dom` 升到 19，装 `@types/react@19`。
2. 跑 `npx tsc --noEmit`、`npx vitest run`、`npx vite build`，记录全部失败点。
3. 重点排查：`React.FC`（约 110 个文件，19 中仍可用但不再隐式带 `children`）、
   `createRoot`（`main.tsx`）、`StrictMode`、`forwardRef`（`CubeText3DPreview.tsx`）。
4. 用 Wails 跑一次真实窗口，确认无一等公民回归。
5. **阶段 0 不引入 HeroUI v3**，据此判断 React 19 本身的成本。

### 阶段 1：依赖与构建层
1. `npm i @heroui/react@3 @heroui/styles@3`，移除 `@heroui/theme`、`@heroui/system`。
2. 安装 v3 全部 peer：`react-aria`、`react-aria-components`、
   `@react-aria/ssr`、`@react-aria/utils`、`@internationalized/date`。
3. 删除 `hero.ts`；`globals.css` 的 `@plugin`/`@source` 改为
   `@import "tailwindcss";` 后接 `@import "@heroui/styles";`（**顺序不可反**）。
4. 删除 `package.json` 的 `overrides` 与 `allowScripts` 中失效项。
5. 删除 `provider.tsx` 的 `HeroUIProvider`（保留 `MotionConfig`）。

### 阶段 2：主题与颜色层（建议独立成一次提交）
1. 建一张 v2→v3 变量映射表，逐项迁移 `--heroui-primary-*` → v3 `accent`。
2. 重写 `theme-color.tsx` 的换肤逻辑（HSL→OKLCH，去掉数字阶梯）。
3. 全量替换配色类名（`bg-primary*`→`bg-accent*`，`default-<n>`→新调色板）。
4. 修 `globals.css` 全部 `data-slot` 选择器指向 v3 槽位；
   让 `globals.test.ts` 转绿作为验收条件。
5. 处理 `classNames`（60 处）与 framer `motionProps`（48 处）的替代方案：
   - 槽位样式 → 子组件 `className`
   - 动效 → v3 内建过渡，或包一层自有 wrapper

### 阶段 3：组件 API 迁移（按组件分批，每批一个提交）
建议顺序（从独立到耦合）：
1. `Button`/`Chip`/`Spinner`/`Divider` 等叶子组件（数量大但改动机械）
2. `Input`/`Textarea`/`Switch`/`Slider`/`Checkbox`
3. **`Select`**（39 处，23 文件）——
   `SelectItem`→`ListBox.Item`、`selectedKeys`→`value`、
   `onSelectionChange`→`onChange`、去 `radius`/`size`/`classNames`/`popoverProps`
4. `Dropdown`/`Popover`/`Tooltip`
5. `Modal`/`Drawer`/`Tabs`/`Accordion` 等复合组件

### 阶段 4：清理与回归
1. 删净 v2 残留（`@heroui/theme`、`@heroui/system` 引用、死变量）。
2. 全量回归：`tsc` + `vitest` + `vite build` + Wails 真机走查。
3. 更新 `docs/CSS_STYLE_TABLE.md`（插件 CSS 契约受颜色/槽位改名影响最大）
   与 `docs/UPDATE_LOG.md`。

## 7. 附：v2 内部的依赖对齐选项

与 v3 无关的独立问题。`@heroui/react@2.6.14` 精确声明
`@heroui/theme@2.4.6` 与 `@heroui/system@2.4.7`，但 `package.json`：

```json
"overrides": { "@heroui/theme": "2.4.26" }
```

且顶层装了 `@heroui/system@2.4.27`，形成「Tailwind 插件按 2.4.26 生成槽定义、
组件按 2.4.6 世代渲染」的错配。注意 `@heroui/select` **并不依赖** `@heroui/theme`
（它只依赖 `react-utils`/`shared-utils`/`popover`），所以这不是"两个 theme 实例打架"，
而是插件与组件的世代不一致。

若要在 v2 内对齐：删除 `overrides`，把 `theme`/`system` 降回 `2.4.6`/`2.4.7`，
让插件与组件同代。这是低风险改动，可作为 v3 迁移前的稳定基线。

## 8. 回滚策略

- 阶段 0 与阶段 1 各自独立分支，可单独 revert。
- 阶段 2/3 按组件分批提交，任一环节失控可回退到最后一次绿色提交。
- v2 线已冻结，回退目标（`2.6.14` + 本文档修复后的 CSS）长期可用，
  不存在「留在 v2 会被上游抛弃」的时间压力。

## 9. 建议的决策点

| 选项 | 适用前提 |
|---|---|
| **A. 留在 v2，只做依赖对齐**（推荐） | 现有外观/功能无阻塞；v2 已冻结，维护成本可控 |
| B. 先做阶段 0（React 19）单独验证 | 想为将来的 React 19 收益铺路，但不急于换 UI 库 |
| C. 全量迁移 v3 | 明确需要 v3 的 RAC 能力或 Tailwind v4 新特性，且愿承担主题层重写 |

## 参考

- [HeroUI v3 迁移总览](https://heroui.com/en/docs/react/migration)
- [Select 迁移](https://heroui.com/en/docs/react/migration/select)
- [样式迁移](https://heroui.com/en/docs/react/migration/styling)
- [@heroui/react@3.2.6 元数据](https://registry.npmjs.org/@heroui/react/3.2.6)
