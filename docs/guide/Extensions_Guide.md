# NekoLauncher 插件开发指南

欢迎来到 NekoLauncher 的插件开发文档！你可以通过该文档编写属于自己的插件，或者
直接把相关文档输入给 AIGC 工具以生成插件。

本指南同时是 **v1 API 规范**：自发布日起算 v1，此后破坏性改动一律递增主版本号
（`plugin.yaml` 里的 `api` 字段只比主版本号）。

## 从哪读起

| 你想做什么 | 读哪篇 |
|---|---|
| 先搞清插件能做什么、不能做什么 | [TRUST_MODEL.md](TRUST_MODEL.md) —— 信任模型与后端闸门 |
| 十分钟跑起第一个插件 | [QUICKSTART.md](QUICKSTART.md) —— 目录格式、dev 模式、plugin.yaml |
| 查某个能力要不要声明权限 | [PERMISSIONS.md](PERMISSIONS.md) —— 权限表、三层闸、高危动作确认 |
| 查 `api.*` 上有什么方法 | [API_REFERENCE.md](API_REFERENCE.md) —— 运行时 API v1 全表 |
| 按场景写代码（下载 / IPC / 日志 / 卡片壳） | [CONVENTIONS.md](CONVENTIONS.md) —— 各 API 组的使用约定 |
| 改样式 / 换肤 | [CSS_STYLE_TABLE.md](CSS_STYLE_TABLE.md)（CSS 变量与语义类）、[UI_THEMES.md](UI_THEMES.md)（界面主题） |
| 想知道某能力为什么永远等不来 | [DESIGN_NOTES.md](DESIGN_NOTES.md) —— 明确不做的与 API 演进规则 |

推荐路线：**信任模型 → 快速上手**，之后按需查表。写任何调用宿主能力的代码前，
先到权限表确认要声明哪些 `capabilities`——未声明直接抛错，不是静默降级。

官方示例：[`examples/server-status/`](../../examples/server-status/) —— 服务器状态
小组件，完整演示 settings 种子、权限声明、轮询清理与快速进服，拷进插件目录点
「重新加载」即可试用。
