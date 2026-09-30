# NekoLauncher

> 一个现代、跨平台的可拓展 Minecraft 启动器，不再重复造轮子。

![CI](https://img.shields.io/github/actions/workflow/status/redstore-noob/NekoLauncher/ci.yml?label=CI&logo=github) ![Release](https://img.shields.io/github/v/release/redstore-noob/NekoLauncher?include_prereleases&label=%E6%9C%80%E6%96%B0%E7%89%88%E6%9C%AC&logo=github) ![Stars](https://img.shields.io/github/stars/redstore-noob/NekoLauncher?style=flat&logo=github) ![Issues](https://img.shields.io/github/issues/redstore-noob/NekoLauncher?logo=github) ![Last Commit](https://img.shields.io/github/last-commit/redstore-noob/NekoLauncher?logo=github) ![Repo Size](https://img.shields.io/github/repo-size/redstore-noob/NekoLauncher?label=%E4%BB%93%E5%BA%93%E5%A4%A7%E5%B0%8F)<br>
![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white) ![React](https://img.shields.io/badge/React-61DAFB?logo=react&logoColor=black) ![Wails](https://img.shields.io/badge/Wails-BB3E35?logo=wails&logoColor=white) ![License](https://img.shields.io/badge/license-Apache--2.0-blue) ![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20Linux%20%7C%20macOS-lightgrey) ![Minecraft](https://img.shields.io/badge/Minecraft-Launcher-62B47A?logo=minecraft&logoColor=white)

## 🐱项目概览

许可证:NekoLauncher整体全部采用Apache2.0许可证<br>
项目技术栈:整体基于Go+Gowails，前端使用React+HeroUI

## 项目创新

- 基于React的插件系统:编写更简单，同时限制插件部分权力保障数据安全。
- NekoSolo打包格式:三合一安装包，解决了过去整合包制作者科普难与发行难等问题。
- AI Agent融合:修改配置等功能更加便携。
- 现代化UI:支持Wallpaper Engine/必应每日一图/跟随系统桌面作为背景，基于Hero UI，主体效果为毛玻璃+圆角UI。
- Rewind备份系统:参考Git设计，文件级的对存档备份，支持Minecraft存档/实例备份。
- 创作中心:一条龙功能，从NekoLauncher插件制作到Minecraft整合包生成，再到资源包制作。

## 项目未来将会更新的内容&未完成的内容

- CurseForge资源相关:API KEY相关内容正在商讨中，后端功能已完毕，我们将会尽快上线相关内容。
- 插件在线商店:没钱买服务器这个真不一定😭
-

## ❓关于新仓库的问题Q&A

Q:这个项目与之前的NekoLauncher是什么关系?

> A:整体架构移植到Go+Gowails，项目不再被AI完全污染, 确保了占用更低(原来的Avalonia占用要300MB+!!!!!!)以及更易编写的扩展(预计使用js)，同时界面美观度将逐渐改善。

Q:原来的NekoLauncher(Powered by Avalonia)还会更新吗?

> A:大概率不会，目前更新重心已经转移到NekoLauncher(Powered by Golang)，原先的项目已经归档。

Q:除了程序本体，有没有什么其他的不同?

> A:有的，我们对于项目整体管理策略/代码审核等会较于旧项目更严格，贡献者不再拥有完全干涉项目决策的权利，项目版本更新命名规则做出调整。当然，我们支持所有合理的建议与代码提交，真正的和谐健康社区应该是共创且民主的，而不是一言堂/恶劣独裁制。

Q:还跟猫娘有关吗?

> A:当然，这个很可爱🥰

Q:关于代码，与之前是否有联系?

> A:NekoLauncher的go重置版与之前没有任何代码上的联系，只有项目名字、设计原则有些许关联。当然，我们会确保用户体验较之前更好。

Q:插件系统是否会出现新的变化?

> A:会的，迁移至Wails后，插件系统将会以新的Web技术栈重写，确保用户能享受新的热重载/完善的插件功能的同时，插件开发者也能在更好的插件API与更简单的编写难度中获益。

Q:NekoSolo是什么?

> A:NekoSolo是NekoLauncher独创的新整合包安装方式(Only Windows)，为安装方便使用.NET Framework4.8+WPF，点击安装时将NekoLauncher本体+整合包+Java Runtime(打包时可选是否随NekoSolo安装)一同安装，解决了原先整合包制作者科普难等问题。

## ♻️构建教程

> [!CAUTION]
> 请确保你的电脑上已经拥有go，wails，node.js等必要依赖，并确保webview等必要组件已安装在系统中。

```bash

cd NekoLauncher
wails dev #打开开发模式.
wails build #构建发布版
wails build -platform linux/amd64 # 交叉编译(Linux需webkit2gtk)

```

## 🫂社区

QQ群号:1108330006<br>
官方网站(目前托管在Github Pages):https://redstore-noob.github.io(当你看到这个括号的时候，说明还是旧项目(逃)

## 👆相关人员名单

_本名单只关于对于NekoLauncher改革后，含有编写代码/提交pr/提出重要建议的相关名单，改革前的贡献者名单不计入此处。_<br>
_下列名单没有排名，没有先后，我发自内心的感谢每一位为项目做出贡献的人，无论贡献大或小_

[HatsukiYukina](https://github.com/HatsukiYukina)
[TeapotCat](https://github.com/RealReGlaze)
[IrisDream-Cubic](https://github.com/IrisDream-Cubic)

## 🤔版本命名规则

因为项目的整体代码栈已经发生实质性改变，现启用全新的版本更新命名，规则如下:  
1.在正式版发布后，使用类似IOS的系统命名规则，具体为年份+月份+子版本号。例如`26.9.1`为2026年9月第一次更新。  
~~2.在启动器功能全部完善/与之前对齐之前，项目版本号不做更改，统一命名为Nya_Rebuild。~~
_功能已完成。_

## 💵项目目前引用的其他项目

- [BMCLAPI]([apiDoc: BMCLAPI - 0.0.0](https://bmclapidoc.bangbang93.com/)) — 由 [bangbang93](https://github.com/bangbang93) 维护的 Minecraft 国内镜像下载源。
- [Terracotta | 陶瓦联机](https://github.com/burningtnt/Terracotta) — 基于 EasyTier 的联机工具（AGPL-3.0）；联机页通过它公开的本地 HTTP 接口驱动它开房 / 进房（版权归原作者，启动器不修改其二进制）。
- [RedstoneOnline | 红石联机](https://modrinth.com/mod/redstoneonline) — 内网穿透联机模组（GPL-3.0）；联机页复用其公开的中继协议，把本机服务器发布到公网。
- [Wails](https://wails.io) — 基于 Go 的桌面应用框架，利用web技术。
- [gopsutil](https://github.com/shirou/gopsutil) — 跨平台的系统信息采集库，用于进程与资源监控。
- [React](https://react.dev) — 前端 UI 框架。
- [Radix UI](https://www.radix-ui.com) — 无样式的可访问组件原语，构成本项目 UI 组件库的基础。
- [Tailwind CSS](https://tailwindcss.com) — 原子化 CSS 框架。
- [lucide-react](https://lucide.dev) — 图标库。
- [Sonner](https://sonner.emilkowal.ski) — Toast 通知组件。
- [Vite](https://vite.dev) — 前端构建工具。
- [TypeScript](https://www.typescriptlang.org) — 类型化的 JavaScript。

## 📄关于其他文档

[隐私政策](docs/TERMS.md)
[贡献指南](CONTRIBUTING.md)
[许可证(Apache 2.0)](LICENSE)
[更新日志](docs/UPDATE_LOG.md)
