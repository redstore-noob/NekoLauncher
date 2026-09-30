# NekoLauncher项目更新版本命名图/更新计划

> 当前阶段:`Public Beta`公开测试版，功能基本完善且可用，但可能会有意想不到的Bug(?)

## 已补齐的功能缺口（0.3.0）

- 整合包导出（.mrpack/.zip 与 NekoSolo .exe）支持中途取消
- 外部启动器实例（MultiMC/Prism/CurseForge/Modrinth/ATLauncher）支持复制为主目录标准版本
- 皮肤站验证码登录：检测到验证码后自动在浏览器打开皮肤站，引导完成网页验证后重试
- CurseForge 独占内容支持版本管理（murmur2 指纹反查，需在设置页配置 API Key）
- 自建包/手动放入文件：版本管理弹窗降级为手工指引（打开目录 + 资源站搜索）
- 桌面壁纸路径：Windows/macOS/Linux 均已实现（FreeBSD 等平台回落默认图）

## 待规划（尚未实现）

- 实例更新检查（content_update_check）对 CurseForge 独占文件的批量指纹识别（目前仅版本管理弹窗支持单个识别）
- 外部实例的直接启动（当前只能管理内容；复制出的标准版本可启动）
- CurseForge 直连下载（免 Key 镜像稳定性待评估）
- Java 运行时自动下载增强
