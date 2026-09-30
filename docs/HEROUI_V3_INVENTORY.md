# HeroUI v2 → v3 全盘改动清单

> 配套文档：
> - [HEROUI_V3_MIGRATION.md](HEROUI_V3_MIGRATION.md) —— 策略 / 阶段 / 风险 / 回滚
> - [HEROUI_V3_COMPONENT_RESEARCH.md](HEROUI_V3_COMPONENT_RESEARCH.md) —— 15 个显示类组件的逐组件调研原始记录
> - [HEROUI_V3_INFRA_RESEARCH.md](HEROUI_V3_INFRA_RESEARCH.md) —— 主题/构建/工具类基础设施调研原始记录
>
> 本文是**合成后的行动清单**，回答"到底要动哪些地方"。两份 research 是证据底稿，
> 需要追溯某个结论的原始出处时查它们。
>
> 证据等级标注（完整说明见 [§12](#12-四个证据分级说明)）：
> - **[实测]** = 用 headless Chromium 真实渲染 v3 后读 DOM，可信度最高
> - **[产物]** = 直接来自 `@heroui/react@3.2.6` / `@heroui/styles@3.2.6` 已发布文件
> - **[文档]** = 来自 heroui.com 官方迁移文档（**已发现 4 处错误**，见 §3）
> - **[未验证]** = 尚未确认，迁移前必须实测
> - **[推导]** = 根据可用令牌推断，非文档明述
>
> 统计范围：`frontend/src`（83 个文件导入 HeroUI）、`examples/`、`docs/`。

## 0. 一句话结论

**不是升级，是换库。** 组件名有 7 处硬改名、`color`/`variant` 体系按组件各自重排、
整套颜色变量换命名换色空间、Provider 与 framer-motion 集成被移除。
共 **83 个应用文件** + **1 个插件公共契约** + **5 处模板/文档** + **主题层 3 个文件**需要改。

### 为什么会变这么多：v3 建在 react-aria-components 之上

这是理解全部 API 变化的钥匙。v3 不再自己实现交互逻辑，而是包一层 RAC [产物]：

```js
// 每个 v3 组件都是"一层样式 + 透传给 RAC"
import { Select } from 'react-aria-components/Select';
jsx(Select, { "data-slot": "select", ...props })   // props 直接进 RAC
```

由此推出三个后果，它们解释了本文档绝大多数条目：

1. **props 面变小、语义跟随 RAC** —— 所以 `color`/`radius` 这类纯视觉 prop 被删，
   只剩 `className` 交给 Tailwind；而状态类 props（`isDisabled`/`isInvalid`/`selectedKey`…）
   沿用 RAC 命名 —— 这是 §4 里多处"文档与产物冲突"的根源。
2. **复合组件化** —— RAC 的 slot 模式要求显式组合
   （`Modal.Container` + `Modal.Dialog`、`Select.Trigger` + `Select.Popover`…），
   所以 v2 的"一个大组件 + 一堆 props"变成"多个子组件拼装"。
3. **7 个 peer 依赖是硬要求** —— `react-aria-components`、`react-aria`、
   `@react-aria/ssr`、`@react-aria/utils`、`@internationalized/date` 不是可选项，
   组件内部直接 import 它们 [产物]。

副作用：v3 同时导出**命名空间式**（`Modal.Dialog`）与**扁平式**（`ModalDialog`）两套名字，
迁移时可任选其一，但 `ModalContent` / `SelectItem` 是**彻底删除**、没有别名。

---

## 1. 依赖与构建层

```bash
# 1) 换包
npm uninstall @heroui/react @heroui/theme @heroui/system
npm install @heroui/styles @heroui/react
# 2) 装 v3 硬性 peer（组件内部直接 import，不是可选）
npm install react-aria react-aria-components @react-aria/ssr @react-aria/utils @internationalized/date
# 3) 升 React 19
npm install react@19 react-dom@19 && npm install -D @types/react@19 @types/react-dom@19
```

| # | 位置 | 现状 | v3 改法 |
|---|---|---|---|
| 1.1 | `package.json` deps | `@heroui/react@2.6.14`、`@heroui/theme@2.4.26`、`@heroui/system@2.4.27` | → `@heroui/react@3` + `@heroui/styles@3`；**删除 theme/system** [产物] |
| 1.2 | `package.json` overrides | `{"@heroui/theme": "2.4.26"}` | 删除（v3 无 theme 包）[产物] |
| 1.3 | 新增 peer deps | — | `react-aria`、`react-aria-components`、`@react-aria/ssr`、`@react-aria/utils`、`@internationalized/date` [产物] |
| 1.4 | `react` / `react-dom` | 18.3.1 | **升到 19**（v3 peer 为 `>=19.0.0`）[产物] |
| 1.5 | `src/hero.ts` | `export default heroui()` | **删除整个文件**；v3 **没有 Tailwind 插件** [产物] |

`tailwindcss@4.1.11` 已满足 v3 的 `>=4.0.0`，**无需升级 Tailwind** [产物]。

`@heroui/styles` 自己的依赖只有 `tailwind-variants@3.3.1` + `tw-animate-css@1.4.0`，
**不含 React** [产物] —— `tw-animate-css` 就是 §6 里 v3 动画的 utility 来源。

## 2. 样式入口层（`src/styles/globals.css`，4 处）

| 行 | 现状 | v3 改法 |
|---|---|---|
| 17-18 | `@import "tailwindcss";` + `@plugin '../hero.ts';` | 去掉 `@plugin`，加 `@import "@heroui/styles";`（**顺序必须在 tailwindcss 之后**）[文档] |
| 20 | `@source '../../node_modules/@heroui/theme/dist/**/*.{js,ts,jsx,tsx}';` | 改为指向 `@heroui/styles`（或删除，v3 组件样式是显式 CSS 导入） |
| 21 | `@custom-variant dark (&:is(.dark *));` | 保留；但 v3 主题类名还支持 `.dark` / `[data-theme="dark"]` [产物] |
| 312-347 | 4 处 `[data-slot="content"]` 规则 | **必须重写**，见 §5 |

### ⚠️ 最高危：`[data-slot="content"]` 在 v3 被改名

v3 **仍用 `data-slot` 机制**，但整套名字换了，`content` 不再存在。
实测 v3 组件源码 `@heroui/react@3.2.6` [产物]：

- `SelectPopover` 同时挂 class `select__popover` **和** `data-slot="select-popover"`：
  ```js
  jsx(Popover, { ...props, className: slots?.popover(), "data-slot": "select-popover", placement })
  ```
- v2 的 `data-slot="content"` 是 **Popover 的槽**（Dropdown / Select 复用同一套 Popover 机制），
  所以这 4 条规则当时命中 Popover / Dropdown / Select 的浮层，**与 Modal 无关**
  （v2 Modal 不挂 `data-slot="base"`，浮层样式另有 `.nya-modal-surface` 负责）。

#### v2 → v3 槽位对应（[产物]，逐个读的组件源码）

| 组件 | v2 槽位 | v3 槽位 |
|---|---|---|
| Popover 内容 | `content` | **无 `data-slot`** → 只能用 class `.popover` |
| Popover 对话框 | — | `popover-dialog` |
| Dropdown 浮层 | `content` | `dropdown-popover` |
| Select 浮层 | `content` | `select-popover` |
| Modal 面板 | （无） | `modal-dialog` |
| Modal 容器/遮罩 | （无） | `modal-container` / `modal-backdrop` |
| Tooltip 内容 | `content` | **无 `data-slot`** → 只能用 class `.tooltip` |
| 列表 / 列表项 | — | `list-box` / `list-box-item` |
| 箭头 | — | `popover-overlay-arrow` / `overlay-arrow` / `tooltip-arrow` |

> ⚠️ 注意 Popover 与 Tooltip 的内容层**没有任何 `data-slot`**，只能靠 class 选择器。
> 这是最容易被"统一改名"脚本漏掉的地方。
>
> 另一处易混：`[data-slot="popover-overlay-arrow"]` 是**箭头**，不是浮层表面——
> 别把它当成 `content` 的替身。

所以现有 4 条 `[data-slot="content"]` 规则升级后会**静默失效**（不报错、不失败编译，
只是毛玻璃和圆角消失）。正确改法：

```css
/* v3：content 槽已不存在，逐浮层改写（注意 Popover/Tooltip 只能走 class） */
[data-slot="select-popover"],
[data-slot="dropdown-popover"],
[data-slot="modal-dialog"],
.popover,      /* Popover 内容层：无 data-slot */
.tooltip {     /* Tooltip 内容层：无 data-slot */
  /* …毛玻璃… */
}
```

**更稳的做法是变量驱动** —— v3 的浮层表面色由 `--overlay` 控制
（`.popover`/`.select__popover` 都 `@apply bg-overlay`），圆角由 `--radius-3xl` 控制 [产物]：

```css
:root { --overlay: <你的表面色>; }   /* 一处改，所有浮层跟随 */
```

> 完整浮层槽位清单仍需实测补齐（drawer 等 **[未验证]**）。
> 好在本项目已有 `globals.test.ts` 守卫"CSS 用到的槽位必须真实被渲染"，
> 升级后它会失败并列出可用槽位——**这是升级过程中的重要护栏，不要删**。

## 3. 组件名硬改名（[产物]）

这些在 v3 的 `dist/index.js` 导出清单中**不存在同名导出**，改名是强制的：

| v2 | v3 | 本项目用量 |
|---|---|---|
| `ModalContent` | **`ModalDialog`**（配 `ModalContainer`/`ModalBackdrop`） | **26** |
| `SelectItem` | **`ListBoxItem`** | **52** |
| `AutocompleteItem` | **`ListBoxItem`** | 2 |
| `Divider` | **`Separator`** | 3 |
| `Progress` | **`ProgressBar`**（线性）/ `ProgressCircle`（环形） | 8 |
| `CardBody` | **`CardContent`** | 1（+ 插件契约/文档） |
| `Textarea` | **`TextArea`**（注意大写 A） | 9 |
| `HeroUIProvider` | **`RouterProvider`**（re-export 自 `react-aria-components`） | 3 |

**同名但语义已变**（名字在、用法要重写）：`Tab`→`Tabs.Tab`、`AccordionItem`→`Accordion.Item`。

**同名保留、prop 变化**：`Button`、`Chip`、`Input`、`Switch`、`Slider`、`Checkbox`、
`Radio`、`RadioGroup`、`Modal`、`ModalBody`、`ModalHeader`、`ModalFooter`、`Popover`、
`PopoverContent`、`PopoverTrigger`、`Tooltip`、`Select`、`Tabs`、`Accordion`、`Dropdown*`、
`Card`、`Code`、`Kbd`、`Link`、`ScrollShadow`、`Spinner`、`Autocomplete`。

> ⚠️ **官方文档有 4 处与实际产物矛盾，以产物为准** [产物]：
> 1. `migration/code` 说 `Code` "已移除" —— **错**，v3 仍导出 `Code`（来自 `components/typography`），
>    但 API 完全不同（无 `color`/`size`/`radius`）。**不要直接删掉用法**。
> 2. Button 文档示例暗示有 `radius` variant —— `buttonVariants` **没有** `radius` 键。
> 3. Kbd 文档写 `Kbd.Key` —— 3.2.6 **不存在**，只有 `Kbd.Root`/`Kbd.Abbr`/`Kbd.Content`。
> 4. Link 文档的路由示例仍写 `<Link as={NextLink}>` —— 与它自己的结论矛盾，`as`/`asChild`
>    在 3.2.6 **都不存在**（`asChild` 在 beta.3 被移除），要用 **`render` prop**。

## 4. 逐组件 prop 迁移

### 4.1 `Button`（71 处，改动量最大）

v3 的 `buttonVariants` [产物]：

```
variant: primary | secondary | tertiary | outline | ghost | danger | danger-soft
size:    sm | md | lg
其他:    fullWidth, isIconOnly
```

**⚠️ v3 Button 没有 `color` prop** —— 颜色被并入 `variant`。

| v2 用法 | 本项目用量 | v3 | 说明 |
|---|---|---|---|
| `color="primary"` | 116（含其它组件） | 删除，默认即 `variant="primary"` | |
| `color="danger"` | 39 | `variant="danger"` 或 `"danger-soft"` | |
| `color="default"` | 6 | `variant="secondary"` | |
| `color="success"` | 10 | **无对应 variant** | 需自定义或退回 Tailwind 类 |
| `color="warning"` | 10 | **无对应 variant** | 同上 |
| `color="secondary"` | 3 | **v3 已无 secondary 颜色** | 需重新设计 |
| `variant="flat"` | **242** | `secondary` / `tertiary` / `ghost` | 需逐个判断语义 |
| `variant="light"` | **100** | `ghost` / `tertiary` | 同上 |
| `variant="bordered"` | 50 | `outline` | |
| `variant="solid"` | 1 | `primary` | |
| `radius="..."` | 117（全组件） | **移除**，改 Tailwind 类 | |

> `variant="flat"` + `variant="light"` 合计 342 处，是**单项最大工作量**。
> 建议先建一张"本项目语义 → v3 variant"的映射决策表再批量改，
> 否则 342 处会改得前后不一致。

### 4.2 `Chip`（17 处）

v3 `chipVariants` [产物]：**保留了 `color`**，但取值变了：

```
color:   accent | danger | default | success | warning     ← primary 改名 accent
variant: primary | secondary | soft | tertiary
size:    sm | md | lg
```

→ `color="primary"` → `color="accent"`；`variant="flat"` → `soft`。
注意 Chip 与 Button 的 color/variant 模型**不一致**，不能统一批量替换。

### 4.3 `Modal`（26 处）

v3 `modalVariants` [产物]：slots = `backdrop/body/closeTrigger/container/dialog/footer/header/heading/icon/trigger`

```
variant: opaque | blur | transparent      ← 这是"遮罩"选项（v2 的 backdrop）
size:    xs | sm | md | lg | full | cover ← v2 的 2xl/3xl/4xl/5xl 不存在
scroll:  inside | outside                 ← v2 的 scrollBehavior
```

| 本项目 | 用量 | v3 改法 |
|---|---|---|
| `size="5xl"` | 4 | → `full` 或 `cover`（需按观感选） |
| `size="4xl"` | 3 | → `lg` 或 `full` |
| `size="3xl"` | 2 | → `lg` |
| `size="2xl"` | 2 | → `lg` |
| `size="full"` | 1 | 保留 |
| `backdrop="..."` | 4 | → `variant="..."` |
| `scrollBehavior` | 4 | → `scroll` |
| `isDismissable` | 6 | **[未验证]** 对应 v3 哪个 prop |
| `hideCloseButton` | 2 | → 不渲染 `ModalCloseTrigger`（子组件化） |
| `disableAnimation` | 4 | **[未验证]**；v3 动效是 CSS 驱动（`data-entering`/`data-exiting`），可能改为禁用过渡 |
| `motionProps` | 41（全组件） | **移除**，见 §6 |

`<Modal><ModalContent>` → 需改为 `ModalContainer` + `ModalDialog` 组合
（精确嵌套顺序 **[未验证]**，需实测）。

### 4.4 `Select`（39 处 / 23 文件）

v3 结构 [产物]：`SelectRoot` 只接受 `className` / `fullWidth` / `isDisabled` / `onClear` / `variant`，
其余 props **原样透传给 `react-aria-components` 的 `Select`**：

```js
// @heroui/react@3.2.6/dist/components/select/select.js
jsx(Select, { "data-slot": "select", ...props, className: ..., isDisabled, children })
//          ^^^ 来自 react-aria-components/Select
```

**⚠️ 这里有一处资料冲突，必须实测确认**：官方文档说 `selectedKeys`→`value`、
`onSelectionChange`→`onChange`；但产物显示 props 直接进 RAC `Select`，
而 RAC 的命名是 `selectedKey` / `defaultSelectedKey` / `onSelectionChange`。
两者不可能都对，**以 spike 实测为准**。

| v2 | 用量 | v3（文档说 / 产物推测） | 备注 |
|---|---|---|---|
| `<SelectItem key="x">` | 52 | `<ListBoxItem id="x" textValue="...">` | 确定：`SelectItem` 导出已删除 |
| `selectedKeys={new Set([...])}` | 43 | `value={x}` / `selectedKey={x}` | **冲突，待实测**；Set 语义肯定要拆 |
| `onSelectionChange` | 44 | `onChange` / `onSelectionChange` | **冲突，待实测**；回调签名从 Set 变为单值 |
| `defaultSelectedKeys` | 1 | `defaultValue` / `defaultSelectedKey` | 同上 |
| `popoverProps={{ motionProps }}` | 31 | **移除**，props 直接给 `<Select.Popover>` | `SelectPopover` 只接 `placement` + className |
| `disallowEmptySelection` | 8 | **[未验证]** —— 迁移表与 API 表均未出现 | 需实测 |
| `classNames={{...}}` | 62（全组件） | **移除**，改子组件 `className` | `composeSlotClassName` 仍在内部使用 |
| `radius` / `size` | — | **移除**（`SelectRoot` 无此 prop） | 改 Tailwind |
| `variant` | — | 收窄为 `primary\|secondary` | |
| `label` / `description` / `errorMessage` | — | 改为 `<Label>` / `<Description>` / `<FieldError>` 子组件 | |
| `isClearable` | 1 | → 组合 `<Select.ClearButton>` | 且仅在组合了 ClearButton 时才可清空 |

### 4.5 表单类（`Input` 32、`Textarea` 9、`Switch` 18、`Slider` 9、`Checkbox` 3、`Radio` 2）

这一组的调研质量最高：子代理用 **headless Chromium 真实渲染了 `@heroui/react@3.2.6`**
（React 19 + RAC 1.21.1）并读取实际 DOM，所以下面的 `data-slot` 是**实测**而非推测。

**核心变化：`Input` / `TextArea` 变成了"裸原语"。**
v2 的 `Input` 自带 label/description/error；v3 的 `Input` 只剩一个 `<input>`，
完整字段要靠 **`TextField` + `Label` + `Input` + `Description` + `FieldError`** 拼出来 [实测]：

```tsx
// v2
<Input label="邮箱" description="说明" errorMessage="错了" startContent={<Icon/>} isInvalid />

// v3
<TextField isInvalid>
  <Label>邮箱</Label>
  <InputGroup>
    <InputGroup.Prefix><Icon /></InputGroup.Prefix>
    <InputGroup.Input />
  </InputGroup>
  <Description>说明</Description>
  <FieldError>错了</FieldError>
</TextField>
```

| v2 prop | v3 | 判定 |
|---|---|---|
| `label` / `description` / `errorMessage` | `<Label>` / `<Description>` / `<FieldError>` 子组件 | **全部移除**（含 Switch/Checkbox/Radio/Slider/Autocomplete） |
| `startContent` / `endContent`（**135 处**） | `InputGroup.Prefix` / `InputGroup.Suffix` | **移除** |
| `isClearable` | 手写 `CloseButton`（Input）/ `Autocomplete.ClearButton` | **移除** |
| `onValueChange` | **`onChange`**，但**签名各不相同** ↓ | **改名 + 语义变化** |
| `isInvalid` / `isRequired` / `isDisabled` | 上移到 **`TextField`** | **移动** |
| `color` / `size` / `radius` / `classNames` | 全部移除 → Tailwind / 子组件 `className` | **移除** |
| `variant` | 收窄为 `primary \| secondary` | **值减少** |
| `isSelected`（**47 处**） | **保留**（Switch/Checkbox/Radio 上） | 保留 |

> ⚠️ **`onChange` 的签名按组件不同**（容易写错且类型能过）[实测]：
> `TextField` → `(value: string)`；`Input` → 原生事件 `e.target.value`；
> `Switch`/`Checkbox` → `(isSelected: boolean)`；`RadioGroup` → `(value: string)`；
> `Autocomplete` → `Key | Key[] | null`。

**各组件复合结构**（都要显式拼装）[实测]：

| 组件 | v3 结构 |
|---|---|
| `Switch` | `Switch` > `.Content` > `.Control` > `.Thumb`（+ `.Icon`）；`size` 保留，`thumbIcon`→`Switch.Icon` |
| `Checkbox` | `Checkbox` > `.Content` > `.Control` > `.Indicator`；新增 `variant` |
| `Radio` | `Radio` > `.Content` > `.Control` > `.Indicator`；**`RadioGroup` 仍是单体**，不变 |
| `Slider` | `Slider` > `.Output` / `.Track` > `.Fill` / `.Thumb`；`minValue`/`maxValue`/`step`/`onChangeEnd`/`orientation`/`isDisabled` **保留**；`showSteps`/`showTooltip`/`marks`/`showOutline` **移除**；`hideValue`→省略 `.Output` |
| `TextArea` | 与 `Input` 同为裸原语；`minRows`/`maxRows`/`disableAutosize`/`onHeightChange` **移除**（改用 `rows` + CSS） |
| `Autocomplete` | **彻底重构**：`Autocomplete` > `.Trigger` > `.Value`/`.ClearButton`/`.Indicator`，`.Popover` > `.Filter` > `SearchField` + `ListBox`；`AutocompleteItem` **删除**→`ListBox.Item` |

> ⚠️ **`Autocomplete` 需要一次产品决策**：v2 的 `Autocomplete` 在 v3 对应**两个不同组件** ——
> 想做"可搜索的下拉选择"用 v3 `Autocomplete`（搜索框在弹层里），
> 想做"输入框内联过滤"用 v3 `ComboBox`。本项目用在
> `CommandGeneratorDialog`（2 处），需按实际交互选一个。

**表单类 `data-slot`（实测 DOM）**：
`textfield`、`label`、`input`、`textarea`、`description`、`field-error`、
`input-group`/`-prefix`/`-input`/`-suffix`/`-textarea`、
`switch`/`-content`/`-control`/`-thumb`/`-icon`、
`checkbox`/`-content`/`-control`/`-indicator`（+`checkbox-default-indicator--checkmark`）、
`radio-group`、`radio`/`-content`/`-control`/`-indicator`、
`slider`/`-output`/`-track`/`-fill`/`-thumb`。

> **重要提醒**：v3 的官方 CSS **主要用 BEM class**（`.input`、`.checkbox__control`、`.slider__thumb`），
> `data-slot` 只用于嵌套/描述性部件。而且 v2 的 `classNames` 槽位名
> （`base`、`inputWrapper`、`innerWrapper`、`helperWrapper`、`clearButton`、`thumbIcon`、
> `control`、`labelWrapper`、`filler`、`value`、`step`、`trackWrapper`、`mark`）
> **在 v3 完全不存在** —— 本项目虽已无 `content1..4` 用法，但若有 `classNames` 传这些键（共 62 处），
> 全部要重写。

### 4.6 显示/布局类：结构性重写 [产物]

这些"同名保留"的组件其实**内部结构被拆开了**，v2 的隐式渲染在 v3 变成显式组合：

| 组件 | v2 | v3 必须显式写 |
|---|---|---|
| `Tabs`（2 处） | `<Tabs><Tab title>` | `Tabs.ListContainer > Tabs.List > Tabs.Tab`，**且 `<Tabs.Indicator/>` 不再自动渲染**，`Tabs.Panel` 要单独写并与 `Tab` 的 `id` 对应；`title`→children；`size` 移除 |
| `Accordion`（1 处） | `<AccordionItem title subtitle>` | `Accordion.Item > Accordion.Heading > Accordion.Trigger > Accordion.Indicator`，`Accordion.Panel > Accordion.Body`；`title`/`subtitle` **移除**；`onSelectionChange`→`onExpandedChange`、`selectedKeys`→`expandedKeys`、`selectionMode="multiple"`→`allowsMultipleExpanded` |
| `Card`（2 处） | `Card` + `CardBody` | `Card.Header/Title/Description/Content/Footer`，**Body→Content** |
| `Progress`（8 处） | `<Progress value maxValue>` | `ProgressBar.Output` + `ProgressBar.Track > ProgressBar.Fill`（v2 是内部渲染的）；`value`/`maxValue`/`isIndeterminate` **保留** |
| `Badge` | `<Badge content>` 包裹式 | `<Badge.Anchor>{child}<Badge/></Badge.Anchor>` |
| `Skeleton` | 包裹子节点 + `isLoaded` | **不再包裹 children**、`isLoaded` 移除 → 改条件渲染 |
| `Kbd` | `<Kbd keys={[...]}>` | `<Kbd.Abbr keyValue="..."/>`（`Kbd.Key` 不存在） |
| `Avatar` | `src` / `name` / `showFallback` | `<Avatar.Image/>` + `<Avatar.Fallback/>`，**首字母要自己算** |
| `Chip` | `content` / `aside` / `avatar` props | 直接用 children |
| `Link` | `as` / `asChild` | `render` prop；外部链接图标改 `<Link.Icon/>` |

### 4.7 ⚠️ 默认观感变化（**会静默走样，必须显式覆盖**）[产物]

v3 的默认圆角/尺寸与 v2 差很多。这类问题**不报错**，只是"看起来不一样了"：

| 元素 | v2 默认 | v3 默认 | 后果 |
|---|---|---|---|
| **Button** | ~`rounded-lg`，`min-w-16/20/24` | **`rounded-3xl`（胶囊）、`w-fit`（无最小宽）** | 同时两处变化：变圆 + 变窄 |
| **Avatar** | `rounded-full`（圆） | **`rounded-3xl`（圆角方）** | 头像不再是圆形 |
| Card | ~14px 圆角 | `min(32px, var(--radius-3xl))` | 明显更圆 |
| Chip | `rounded-full` + 实色 | `rounded-2xl`，**默认灰底** `var(--default)` | 默认变灰 |
| Badge | `h-5 min-w-5` | `min-h-7 min-w-7` + 1px `var(--background)` 环 | 变大 |
| Tabs | 自动指示器、有 size | 固定 `h-8`、容器胶囊化、指示器要手写 | 见 §4.6 |
| ProgressBar 轨道 | `rounded-full` | `rounded-sm` | 直角化 |
| Link | 无下划线 | **hover 有下划线**、`text-link` | |
| Skeleton | `bg-default-200` | `rounded-sm bg-surface-tertiary/70` | |

> **圆角体系另有陷阱**：v3 用单一 `--radius: 0.5rem` 派生
> （`--radius-sm = radius*0.5 = 4px`、`--radius-lg = radius = 8px` …）。
> 机械改名（`rounded-small`→`rounded-sm`）会让圆角**变小**（8px→4px）。
> 若要"观感不变"，需要 `rounded-small`→`rounded-lg`、`rounded-medium`→`rounded-xl`
> 这类跨级映射。**[未验证]** 其它组件的 v2 默认圆角未逐一反推。

## 5. 颜色与主题系统（改动面可能超过 Select）

### 5.1 变量体系整体更换 [产物]

v3 的变量**没有 `--heroui-` 前缀**，且**没有数字阶梯**：

| v2 | v3 |
|---|---|
| `--heroui-primary` | `--accent` |
| `--heroui-primary-foreground` | `--accent-foreground` |
| `--heroui-primary-50` … `-900` | **不存在**（数字阶梯被删除） |
| `--heroui-danger/success/warning` + `-foreground` | `--danger`/`--success`/`--warning` + `-foreground` |
| `--heroui-secondary` | **不存在**（secondary 颜色被删除） |
| `content1` … `content4` | `--surface` / `--surface-secondary` / `--surface-tertiary` / `--overlay` |
| `--heroui-default-50..900` | `--default` + `--default-hover`/`--default-soft` |
| `--heroui-foreground-500` | `--muted` |
| 格式：**HSL 通道** `212 100% 47%` | 格式：**OKLCH** `oklch(0.6204 0.195 253.83)` |

新增概念：`--field-*`（表单字段）、`--*-soft`（柔和变体）、`--overlay-shadow`、
`--surface-shadow`、`--focus`、`--link`、`--backdrop`、`--radius`/`--field-radius`/`--radius-3xl`。

**两层令牌结构**（理解它才知道该改哪一层）[产物]：

```
themes/default/variables.css   →  源令牌      --accent: oklch(...)          （@layer base）
themes/shared/theme.css        →  Tailwind 令牌 --color-accent: var(--accent) （@theme inline）
                                    ↓ 由 Tailwind 生成 bg-accent / text-accent 等工具类
```

改**源令牌**即可影响组件；但 `bg-accent` 这类**类名**来自 `--color-*`，
所以若要在 JSX 里用 `bg-primary` 这种旧名字，需要在 `@theme` 里自己加 `--color-primary: var(--accent)`。

**数字阶梯没有替代品**，要按用途换成三个语义令牌 [产物]：

| v2 用途 | v3 替换 |
|---|---|
| 浅底/背景色（`primary-50/100`） | `--accent-soft`（= accent 15% + transparent） |
| 主色本体（`primary-500`） | `--accent` |
| 深色/hover（`primary-600~900`） | `--accent-hover` / `--accent-soft-hover`（= accent 20%） |
| 浅色前景文字 | `--accent-soft-foreground` |

> ⚠️ 官方文档里的 `bg-surface-quaternary` 是**文档 bug**：3.2.6 的产物中
> "quaternary" 出现 **0 次**，`content4` 在 v3 **没有对应物** [产物]。

**`--accent` 声明在主题无关的公共块**（`:root, :host`，不在 light/dark 块内）[产物] ——
这是个好消息：**一次覆盖同时生效于明暗两套主题**；而 `--danger`/`--warning` 是分主题的。

### 5.2 `theme-color.tsx` 需要重写（**架构性**）

现状：该文件是完整的**10 级 HSL 调色板生成器**，把 11 个变量
（`--heroui-primary-50..900` + `--heroui-primary` + `--heroui-primary-foreground`）
内联写到 `<html>`，并提供"预设色 / 自定义色 / 跟随背景图取色"三态换肤。

**v3 没有数字阶梯可写** —— `STOPS`、`customToChannels()`、`ramp` 全部失效。

好消息是工作量**大幅缩小**：v3 只需写 **2 个变量**
（`--accent` + `--accent-foreground`，OKLCH 格式），因为派生令牌
（`--accent-hover`/`--accent-soft`…）都是 `color-mix(... var(--accent) ...)`，
会自动跟着变 [产物]。且因 `--accent` 在公共块，**明暗主题一次覆盖**。

仍需自己处理的：`--nya-surface-*` 系列目前由主色 HSL 推导（`surface(0.32, 97)` 等），
这部分逻辑要改为基于 OKLCH 重新推导，否则侧边栏/面板会与主色脱节。

> 该文件是"换肤系统"的地基，建议**单独一次提交**并重点回归。

### 5.3 `globals.css` 的颜色引用需逐条重写（不是改名）

13 处形如 `hsl(var(--heroui-primary-500) / 0.22)` 的写法在 v3 **语法上就不成立**
（`--accent` 是 OKLCH 完整色值，不是 HSL 通道）。需改为：

```css
color-mix(in oklab, var(--accent) 22%, transparent)
/* 或 */ oklch(from var(--accent) l c h / 0.22)
```

### 5.4 Tailwind 工具类（500+ 处）[产物]

`bg-primary`/`text-primary` 等 **308** 处、`primary-<数字>` **38** 处、
`default-<数字>` **166** 处，都依赖 v2 的 heroui Tailwind 插件生成的类。
插件移除后**这些类不再存在**，且 **Tailwind v4 对未知工具类是静默不生成**（不报错）——
旧类名会直接渲染成无样式。**这是最阴的坑之一。**

官方**没有**完整改名表（styling 页只有约 16 行）。已核实的高频映射：

| v2 | v3 |
|---|---|
| `bg-primary` / `text-primary-500` | `bg-accent` / `text-accent` |
| `bg-primary-100` / `bg-primary-900/40` | `bg-accent-soft` |
| `bg-primary-500/15` | `bg-accent/15` |
| `bg-content1`（页面/卡片） | `bg-surface` |
| `bg-content1`（浮层） | `bg-overlay` |
| `bg-content2/3` | `bg-surface-secondary` / `bg-surface-tertiary` |
| `bg-default-100` | `bg-default` **[推导]**，或 `bg-surface-secondary` |
| `bg-default-200` / `hover:bg-default-200` | `bg-default-hover` **[推导]** |
| `text-foreground-400/500` | `text-muted` |
| `border-default-200` | `border-border` |
| `border-default-300` | `border-border-secondary` |
| `text-tiny` / `text-small` | `text-xs` / `text-sm`（字号实际相同） |
| `border-small/medium/large` | `border` / `border-2` / `border-[3px]` |

**v3 已删除的工具类**：所有 `.transition-background/-colors-opacity/-width/-height/-size/-left/-transform-*`、
`.scrollbar-hide`（→ `scrollbar-none`）、`.leading-inherit`、`.tap-highlight-transparent`、
`.input-search-cancel-button-none`。

**推荐策略**：不要全量改名，而是在 `globals.css` 用 Tailwind v4 的 `@theme` 自己
把 `--color-primary: var(--accent)`、`--color-default: var(--default)` 等接上，
**把这 500+ 处旧类名保留下来**，把迁移爆炸半径限制在组件层而非所有布局代码。
这样也顺带保住 `docs/CSS_STYLE_TABLE.md` 里对插件承诺的类名。

## 6. 动效层（framer-motion 解耦）

v3 **不再有 `framer-motion` peer**，`motionProps` 在 Modal/Popover/Tooltip/Dropdown/Select
上**被移除且没有替代 prop**；动效改为 CSS 驱动 [产物]：

```css
/* v3 的浮层动画：由 React Aria 的状态属性驱动 */
.popover[data-entering] { @apply animate-in zoom-in-90 fade-in-0 duration-200; }
.popover[data-exiting]  { @apply animate-out zoom-out-95 fade-out; }
```

- utility 来自 **`tw-animate-css`**（`@heroui/styles` 的依赖，已随包安装）[产物]
- 还支持 `[data-hovered]`/`[data-pressed]`/`[data-focus-visible]`/`[data-disabled]`
- **没有全局动画开关**（v2 的 `disableAnimation` 已移除）
- 想保留自定义弹簧：用 v3 的 **`render` prop** 渲染 motion 元素，或自己包 wrapper

本项目耦合点：

| 位置 | 数量 | 处理 |
|---|---|---|
| `popoverProps={{ motionProps: popoverMotionProps }}` | 30 | **删除**，改由 CSS `data-entering` 或 `render` 实现 |
| `dropdownMotionProps` / `tooltipMotionProps` | 18 | 同上 |
| `src/lib/motion.ts` | 1 文件 | 对 HeroUI 那条通路变为死代码，仅保留自有组件用的 variants |
| `provider.tsx` 的 `MotionConfig` | 1 | **可保留**（framer-motion 仍是直接依赖，12 个文件直接 import） |

> `framer-motion` 不必卸载 —— 它只是不再被 HeroUI 需要。要删的是那 48 处
> **传进 HeroUI 组件**的 motion props。这是**观感会变**的区域，建议先设计确认。

## 7. 路由接线（`provider.tsx`）

v3 的 `HeroUIProvider` 被移除，但**本项目用它传 `navigate`/`useHref`**，
所以不能只是删掉 —— 要换成 `RouterProvider`（v3 从 `react-aria-components` re-export）[产物]：

```tsx
import type { NavigateOptions } from "react-router-dom";
import { MotionConfig } from "framer-motion";
import { RouterProvider } from "@heroui/react";   // = react-aria-components 的 re-export
import { useHref, useNavigate } from "react-router-dom";

declare module "react-aria-components" {           // 注意：不再是 "@react-types/shared"
  interface RouterConfig { routerOptions: NavigateOptions }
}

export function Provider({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate();
  return (
    <RouterProvider navigate={navigate} useHref={useHref}>
      <MotionConfig reducedMotion="user">{children}</MotionConfig>
    </RouterProvider>
  );
}
```

三个易错点：
1. `declare module` 的目标从 `@react-types/shared` 改为 `react-aria-components`
2. `<Routes>` 必须渲染在 `RouterProvider` **内部**
3. `Button` 在 v3 **没有 `href`**（`ButtonRootProps` 就是 RAC Button）。
   "看起来像链接的按钮"要用 `Link` + button 的类名，而不是 `<Button href>`

堆叠层级由 `@heroui/styles` 的 `base.css` 统一约定
（`--z-index-overlay: 100000`），不再需要自己管 portal 层级 [产物]。

## 8. 插件公共契约（对外兼容性，**易漏**）

`plugin/api.ts` 把 **29 个 HeroUI 组件**作为 `api.ui` 白名单暴露给插件，
`docs/Extensions_Guide.md` 与 `docs/CSS_STYLE_TABLE.md` 已将其**声明为兼容契约**。
第三方插件直接用 `ui.Card` / `ui.Button` / `ui.Select` 等，所以这是**破坏性变更**。

| 位置 | 要改什么 |
|---|---|
| `src/plugin/api.ts` | 29 个导入全部重映射（`CardBody`→`CardContent`、`Divider`→`Separator`、`Progress`→`ProgressBar`、`Textarea`→`TextArea`、`SelectItem`→`ListBoxItem`、`ModalContent`→`ModalDialog`） |
| `src/plugin/types.ts` | `ui` 白名单的类型声明与注释 |
| `PluginManifestDialog.tsx` 的 `ENTRY_TEMPLATE` | 模板里 `color:"primary"`/`size:"sm"`/`variant:"flat"` 全部失效 |
| `locales/parts/{en,ja,ru,zh-TW}-0.ts` | 同上模板的 4 份翻译（共 8 处） |
| `examples/server-status/index.jsx` | `<ui.Spinner size="sm">`、`<ui.Button size="sm" variant="flat">` |
| `docs/Extensions_Guide.md` | §3 示例用 `ui.Card{shadow}`、`ui.CardBody`、`variant:"solid"/"flat"` |
| `docs/CSS_STYLE_TABLE.md` | §2「第一档 CSS 变量」整表基于 `--heroui-primary-*`，**需整体重写** |

**需决策**：是否保持 `api.ui` 的组件名向后兼容（在 `plugin/api.ts` 里做一层 v2→v3 适配别名），
否则所有已发布插件的 `ui.SelectItem`、`ui.CardBody` 会直接崩。

## 9. 逐文件工作分组

83 个文件导入 HeroUI；**77 个**需要结构性改动（改名/删除 prop），其余只需 color/variant 调整。

### A. 插件契约（最高优先级，对外可见）
`plugin/api.ts`、`plugin/types.ts`、`PluginManifestDialog.tsx`、4 个 locale 文件、
`examples/server-status/index.jsx`、`docs/Extensions_Guide.md`、`docs/CSS_STYLE_TABLE.md`

### B. 主题地基（单独提交）
`src/hero.ts`（删）、`src/styles/globals.css`、`src/theme-color.tsx`、`src/provider.tsx`、`package.json`

### C. 重灾区文件（改动最多的 8 个）
| 文件 | HeroUI 组件数 | 关键改动 |
|---|---|---|
| `layouts/instances.tsx` | 13 | ModalContent、SelectItem、Divider、Textarea、classNames、motionProps、radius |
| `components/creator/ModpackExportDialog.tsx` | 12 | ModalContent、SelectItem、Progress、Textarea、classNames、motionProps |
| `components/instance/RewindDialog.tsx` | 13 | ModalContent、SelectItem、Popover、motionProps |
| `components/creator/ResourcePackDialog.tsx` | 11 | ModalContent、SelectItem、Dropdown、radius |
| `layouts/ai.tsx` | 13 | ModalContent、SelectItem、Divider、Textarea、classNames |
| `layouts/servers.tsx` | 10 | ModalContent、SelectItem、Progress、Textarea、classNames |
| `layouts/plugins.tsx` | 10 | ModalContent、Divider、Textarea |
| `components/creator/CommandGeneratorDialog.tsx` | 10 | ModalContent、SelectItem、AutocompleteItem、classNames |

### D. 类名/圆角批量项
`radius=` 分布在约 30 个文件、共 117 处（Home 卡片组、pager、overlay 等）；
`classNames={{` 62 处、`motionProps` 41 处。这些适合脚本化 + 人工复核。

## 10. 验收清单

1. `npx tsc --noEmit` 全绿（**这是主要安全网**：改名与删除的 prop 都会被编译器抓到。
   但**抓不到**颜色/圆角/`classNames` 这类"值变了但类型还对"的问题）
2. `npx vitest run` 全绿 —— 尤其 `src/styles/globals.test.ts`：
   它动态读取已安装的 HeroUI 源码，所以升级后会自动反映 v3 的槽位。
   但其中有一条自检断言 `expect(renderedSlots.has("content")).toBe(true)`，
   在 v3 下**会失败**（`content` 已不存在）——这**是预期行为**，
   它在提醒我们去改 §2 的 CSS；改完 CSS 后把该断言换成 v3 的槽位名（如 `select-popover`）即可。
3. `npx vite build` 成功。**注意**：构建成功**不代表样式在** ——
   Tailwind v4 对未知工具类静默跳过（见 §5.4），所以还要抽查产物 CSS 是否含
   `@heroui/styles` 的类（`.select__popover`、`.popover` 等）
4. Wails 真机走查，重点四项（**静默失败都在这四项**）：
   ① 浮层毛玻璃与圆角 ② Button/Avatar/Card 的圆角形状（§4.7）③ 动效手感（§6）④ 换肤（§5.2）
5. 插件回归：装一个用 `ui.*` 的插件，确认白名单可用（§8）

## 11. 仍然未验证、迁移前必须实测的项

经过四轮调研（含一次真实 DOM 渲染），大部分已落实。**剩下这些是真正需要 spike 的**：

| 项 | 为什么重要 | 为什么还没答案 |
|---|---|---|
| `Select` 的受控 prop 到底是 `value`/`onChange` 还是 `selectedKey`/`onSelectionChange` | 43+44 处 | **文档与产物冲突**（§4.4）：文档说前者，但产物把 props 透传给 RAC `Select`（后者）。必须实跑一次 |
| `disallowEmptySelection` 是否被支持 | 8 处依赖 | 迁移表与 API 表均未列出 |
| `Modal` 的 `Container`/`Dialog` 精确嵌套顺序 | 26 处，写错全渲染异常 | 各来源描述不完全一致 |
| `isDismissable` / `disableAnimation` 的落点 | 共 10 处 | 前者移到 `Modal.Backdrop`，后者移除；需确认替代 |
| `ProgressBar` 的 `valueLabel` | 官方 CRT 说"Same"，但类型里没有 | 文档与类型冲突 |
| `Code` 是否是受支持的公开组件 | 1 处 + 文档 | 迁移页说"已移除"，产物里存在 |
| 浮层的完整覆层 class/slot 清单（drawer / combo-box 等） | §2 的玻璃效果重写需要 | 只逐个核了本项目用到的组件 |
| `shouldCloseOnInteractOutside` 等"静默移除"的浮层 props | 少量 | 未在文档中列出，但可能经 RAC 透传生效 |
| `Autocomplete` 的 DOM 槽位 | 2 处 | 渲染探针未能挂载该组件（CSS 内部引用已知） |

> **建议**：先做一次 spike —— 用 v3 起最小页面，渲染 `Select` / `Modal` / `Input`+`TextField` / `Autocomplete`
> 各一个，dump 真实 DOM 与 props 行为。上表 9 项里大部分**一次就能问清**，
> 同时顺便产出 §2 需要的完整覆层清单。这是投产比最高的一步。

## 12. 四个证据分级说明

本文标注的三档含义：

| 标注 | 含义 | 可信度 |
|---|---|---|
| `[实测]` | 子代理用 headless Chromium 真实渲染 v3 并读 DOM | 最高 |
| `[产物]` | 直接读 `@heroui/react@3.2.6` / `@heroui/styles@3.2.6` 已发布文件 | 高 |
| `[文档]` | 官方 heroui.com 迁移文档 | 中（**已发现 4 处错误**，见 §3） |
| `[未验证]` | 尚未确认 | — |
| `[推导]` | 我根据可用令牌推断，非文档明述 | 低 |

> 经验：这次调研中**官方文档有 4 处与产物矛盾**，所以凡是文档与产物冲突的地方，
> 本文一律采信产物并标注冲突。这也是为什么 §11 把"文档与产物冲突"单列出来。
