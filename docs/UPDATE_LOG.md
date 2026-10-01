# 0.3.2

- Linux 新增发行版安装包：Debian/Ubuntu 的 `.deb`、Fedora 的 `.rpm`、Arch 的 `pkg.tar.zst`（`deb` 分 glibc 2.35 / 2.39 两份，分别对应 webkit2gtk 4.0 与 4.1 的系统）
- macOS 安装包拆成 `darwin-arm64` 与 `darwin-amd64` 两份（此前是一个体积翻倍的 universal 包）
- 修复 CI 无法启动：工作流里 AppImage 步骤的 `if:` 与 `shell:` 被写在同一行，导致整个 YAML 解析失败、所有 job 一起挂掉
- 修复 Linux 打包后的资产改名会重复处理自身改名结果的问题：改用先取文件快照再改名的写法，并跳过已经是目标名的文件
- 修复 Fedora / Arch 容器构建整段失效：`case "${{ matrix.container }}"` 对不上（`container: fedora:42` 在 YAML 层是对象，不是字符串），导致包管理器一个分支都不进、工具链完全没装，job 6 秒即失败
- 修复构建产物上传失败：artifact 名沿用了带斜杠的 `matrix.name`（如 `windows/amd64`），`upload-artifact@v4` 直接拒绝，改为使用无斜杠的 `matrix.artifact`
- 构建前先用 `npm ci` 按 lock 文件装好前端依赖：此前依赖 `wails build` 自行执行 `npm install`，在 macOS 与容器环境中会拖长甚至被 runner 掐断
- 补齐前端遗留的格式告警（`eslint --max-warnings 0` 下会让前端 job 直接失败）

# 0.3.1

- 修复 MultiMC / Prism 整合包导入：此前读不出 `mmc-pack.json`，不会自动安装对应的 MC 版本与加载器，内容还会被解到共享目录而不是独立实例
- 修复 Modrinth `.mrpack` 导入缺失的规范步骤：`client-overrides/` 的内容此前完全不生效、`server-overrides/` 的服务端专用文件会被错误装进客户端、`env` 声明为仅服务端的文件也会被下载
- 整合包依赖下载现在同时校验清单声明的 SHA-1 与 SHA-512（此前只校验 SHA-1，只声明 sha512 的文件完全得不到校验；清单里的占位/写坏的哈希会被忽略而不是误杀正确的下载）
- 修复 CurseForge 下载失败：官方已要求 CDN 直链携带 API Key，此前只有查询接口带了 Key、真正下载那一步没带
- CurseForge 下载失败不再只抛 401/404：未配置 API Key、Key 失效、作者禁止第三方分发、文件已被删除现在都有明确提示
- 整合包声明的加载器不受支持时（如 rift-loader）不再把内容倒进已有原版实例，改为在写入任何文件前拦下并说明原因
- 修复实例内容校验只认 `overrides/` 前缀，导致 `client-overrides/` 与 `.minecraft/` 布局被整包误报"文件缺失"
- 修复自定义纯红 / 灰阶主题色时侧边栏与面板配色错误的问题
- 修复陶瓦联机下载的版本号长期停留在 0.2.0
- 安全：不再向前端暴露 CurseForge API Key（插件可直接调用宿主接口，此前任何插件都能读走该 Key）
- 清理了一批无人调用的宿主接口与死代码

# 0.3.0 - Public Beta3

- 修复了mod中文名功能部分情况下不正常显示的问题
- 插件系统新增能修改CSS的主题的功能
- 语言设置新增Zh-HK
- NekoSolo从内置Minecraft本体jar更改为安装后自动下载(避免违反eula协议)
- 整合包导出支持中途取消
- 外部启动器实例(MultiMC/Prism/CurseForge/Modrinth/ATLauncher)支持复制为本地版本
- 皮肤站验证码:自动打开浏览器引导完成验证后重试
- CurseForge独占内容支持版本管理(需配置API Key);自建包文件提供手工管理指引
