# 快速上手：目录格式与第一个插件

从零写出一个能跑的插件。入门路线见 [Extensions_Guide.md](Extensions_Guide.md)。

## 插件文件夹格式

一个完整可用的插件目录：

```
<插件id>/                ← 目录名必须等于清单 id（宿主强校验）
├── plugin.yaml          ← 唯一声明文件（见下方 plugin.yaml 清单）
├── icon.png             ← 图标（固定名，可选）
├── index.js             ← 编译产物 = 清单默认入口
├── theme.css            ← 清单 styles 声明的样式文件（可选，存盘即热生效）
└── src/…                ← 源码（作者保留，随包分发）
```

- 编写语言 **TSX / JSX / 手写 JS**（`h()`）皆可；**分发物永远是编译后的单文件
  ESM `index.js`**，启动器的生产加载路径上没有编译器；
- **打包**：`.nekoex`（zip + 识别后缀）；启动器内的打包工具会把 JSX 等源文件
  编译为 JS 后再打包；
- **源码随包分发是特性**：信任模型要求用户能读到插件在做什么（见
  [TRUST_MODEL.md](TRUST_MODEL.md)）；
- 静态资源放目录内，用 `new URL("./assets/x.png", import.meta.url)` 引用；宿主的
  Tailwind 工具类对插件 DOM 可用，但仅限宿主源码出现过的类，任意值请用 inline
  style。要改**其它控件**的样式（包括宿主自身的），用清单 `styles` 字段或
  `api.styles.inject`（权限见 [PERMISSIONS.md](PERMISSIONS.md)）；受支持的定制锚点
  （CSS 变量与 `nya-*` 语义类）见
  [CSS_STYLE_TABLE.md](CSS_STYLE_TABLE.md)。想做可在外观设置里被选中的**界面主题**
  （整体换肤，`api.registerUiTheme`，需 `styles` 权限），见
  [UI_THEMES.md](UI_THEMES.md)。

## 第一个插件（dev 模式，零工具链）

在 `plugin.yaml` 里加 `dev: true`，入口写 `index.jsx` 源码：宿主会懒加载 Sucrase
现场编译后加载（无 dev 插件时零开销）。保存后点插件页的「重新加载」即生效。

```yaml
# plugin.yaml
id: account-quick-switch
name: 账号快切
version: "1.0.0"
api: "1"
description: 主页小组件：一键切换登录账号
capabilities:
  storage: true
settings:
  showAvatar: "1"        # 默认设置，首次加载种入 api.config
```

```js
// index.js —— 手写 JS 版（dev 模式也可直接写 index.jsx 用 JSX 语法）
export default function activate(api) {
  const { h, ui, icons, notify } = api;

  api.registerWidget({
    id: "account-switcher",       // 实际注册为 "<插件id>:account-switcher"
    title: "账号快切",
    description: "在主页一键切换登录账号",
    icon: h(icons.Key20Regular),  // 注意：icon 要节点不是组件
    tileClass: "from-violet-500 to-fuchsia-500",
    render: function AccountCard({ context }) {
      const { accounts, selectedAccount, onSelectAccount } = context;
      if (accounts.length === 0) {
        return h("div", { className: "text-xs text-gray-400" }, "还没有添加账号");
      }
      return h(ui.Card, { shadow: "sm" },
        h(ui.CardBody, { className: "flex flex-wrap gap-1.5" },
          accounts.map((account) =>
            h(ui.Button, {
              key: account.key,
              size: "sm",
              color: "primary",
              variant: account.key === selectedAccount?.key ? "solid" : "flat",
              onPress: () => void onSelectAccount(account.key),
            }, account.name),
          ),
        ),
      );
    },
  });

  api.notify.success("账号快切已加载");
}
```

发布时用插件模板仓库（esbuild）把 TSX/JSX 编译为 `index.js` 再打包；从手写 `h()`
迁到 JSX 是机械翻译。

> **官方示例**：[`examples/server-status/`](../../examples/server-status/) —— 服务器
> 状态小组件，完整演示 settings 种子、权限声明、轮询 + `onCleanup`、
> `getServerStatus`、`setClipboard` 与 `onJoin` 快速进服。拷进插件目录点
> 「重新加载」即可试用。

## plugin.yaml 清单

```yaml
id: playtime-stats          # 必填，= 目录名
name: 游戏时长统计
version: "1.0.0"            # 所有版本字段必须带引号（防 YAML 类型强转，如 1.20 → 1.2）
api: "1"                    # 目标宿主主版本
description: …
author: …

capabilities:               # 权限声明（见 PERMISSIONS.md）
  storage: true
  launch: true

settings:                   # 默认设置：首次加载种入 api.config（仅空键写入）
  dailyGoalHours: "2"

styles:                     # 样式文件（相对插件目录、限 .css）：加载时自动注入为全局 CSS
  - theme.css               # 改动会被宿主监听，保存即热生效（约 3 秒内），无需重新加载
```

- `entry` / `icon` 字段不存在：入口固定 `index.js`（dev 插件 `index.jsx`），图标
  固定 `icon.png`；
- **不存在"改写启动命令行"的通道**：插件只能通过 `launchSelected` /
  `launchVersion` 走与用户手点完全相同的启动正门。
