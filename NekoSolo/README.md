# NekoSolo 安装包（Windows 专用）

NekoSolo = **NekoLauncher 启动器 + 捆绑 Java（可选）+ 整合包** 三合一的单文件安装程序。
整合包作者在启动器里一键导出 exe，玩家双击即玩——不需要装启动器、不需要装 Java、
不需要懂什么是实例隔离。

## 玩家视角

1. 下载 `XXX-Setup.exe`，双击
2. 选安装目录（默认在用户目录，全程无 UAC），点「安装」
3. 点「完成」→ 启动器自动打开，默认进入 **NekoLauncher-S 简洁模式**
   （只有启动、外观、下载、账号、设置五个页面），整合包实例已自动选中
4. 首次启动游戏时联网补全 Minecraft 客户端本体与缺失的 libraries / assets
   （客户端不随安装包分发，见下方「技术约束」）

安装目录自带 `portable.flag`（便携模式）：数据全部跟随目录，**删除目录即卸载**。

## 已有启动器时装新包

安装器按目标目录现状自动分三种模式（详见 `FORMAT.md` 第 6 节）：

- **全新安装**：目录里没有启动器 → 全量安装
- **添加整合包**：目录里已有 NekoLauncher（别的包装的或独立安装的）→
  **不覆盖启动器本体、不写 portable.flag、已有 Java 不动、配置文件零接触**，
  只装新的整合包实例；首启自动选中并进入游戏目录列表
- **更新整合包**：本包曾装过 → 更新内容，`saves/` 与 `options.txt` 保留

玩家侧的"不覆盖配置"由启动器首启逻辑双保险：S 模式只在从未设置时开启、
游戏目录只在未配置时接管（否则进列表供切换）、Java 只在未配置时注册。

## 作者视角

1. 构建一次安装器模板（见下），把它放到 `NekoSolo/build/`
2. 启动器「创作中心 → 整合包制作」→ 打包格式选 **NekoSolo（.exe）**
   - 勾选「捆绑当前 Java 运行时」→ 玩家无需自备 Java（推荐）
     注意：打进包里的就是你自己配置的那个 JDK，再分发条款随供应商而异
     （Zulu / Temurin 等 GPLv2+CE 可放心分发；Oracle JDK 需自行确认）
   - 不勾 → 玩家首次启动时需要自备 Java（启动器会提示）
   - 勾选「在线安装包」→ exe 只有几 MB：把导出的 payload.zip 上传到
     GitHub Releases 等地址（推荐先建 Release 再导出，填资产直链），
     玩家安装时动态下载并自动校验（格式见 FORMAT.md 第 7 节）
3. 导出得到 `包名-版本-Setup.exe`，扔网盘/QQ群即完成分发
4. 整合包更新后重新导出同名 exe，玩家**重跑安装包即增量更新**
   （存档 `saves/` 与 `options.txt` 自动保留）；在线安装包则只需更新
   托管地址上的 payload.zip

## 构建安装器模板

```powershell
# 需要已安装 .NET SDK（能还原 net48 目标包与 NuGet 依赖）
cd NekoSolo
./publish.ps1
```

产物复制到 `NekoSolo/build/NekoSolo.Installer.exe` 后，启动器导出时会自动找到它。
查找顺序（`internal/solo/stub.go`）：

1. 环境变量 `NEKOSOLO_STUB`
2. `<启动器exe目录>/tools/NekoSolo/NekoSolo.Installer.exe`
3. `<启动器exe目录>/NekoSolo/NekoSolo.Installer.exe`
4. `<工作目录>/NekoSolo/build/NekoSolo.Installer.exe`

> 发布启动器时记得把模板一起放进 `tools/NekoSolo/`，否则用户导出 exe 会提示缺模板。

## 目录结构

```
NekoSolo/
├── README.md                 本文件
├── FORMAT.md                 安装包格式规范（尾标 / 载荷布局 / 标记文件）
├── publish.ps1               构建并发布 stub 到 build/
├── build/                    发布产物（导出时查找的默认位置）
└── NekoSolo.Installer/       C# WPF 安装器源码（.NET Framework 4.8 + Material Design）
    ├── NekoSolo.Installer.csproj
    ├── App.xaml(.cs)           应用入口与 MaterialDesign 资源
    ├── MainWindow.xaml(.cs)    三步向导（欢迎 → 安装 → 完成）
    ├── PayloadReader.cs        尾标解析 / CRC32 / 载荷流
    ├── InstallerEngine.cs      解压布局分发 / 更新模式 / 写首启标记
    └── Manifest.cs             manifest.json 与 neko-solo.json 数据模型
```

## 技术约束

- **载荷不含 Minecraft 客户端本体**（`versions/<id>/<id>.jar`）：客户端是 Mojang
  的版权物，随安装包分发属于再分发。载荷只带版本描述 JSON（内含官方下载地址），
  玩家首次启动时由启动前文件校验从 Mojang 官方地址拉取——与 libraries / assets
  走同一条既有的联网补全路径（见 FORMAT.md 第 3 节）
- 安装器本体只含向导逻辑，**不含任何整合包内容**；内容在导出时以载荷 zip
  追加到 exe 尾部（见 FORMAT.md），因此模板体积恒定、内容大小无上限
- 安装器是**自包含单文件**：MaterialDesign 及其运行时依赖在构建时作为嵌入式
  资源打进 exe（见 csproj 的 EmbedRuntimeDependencies 注释与 App.xaml.cs 的
  AssemblyResolve 自解压加载），旁边不需要任何 DLL——发布产物只需一个 exe
- .NET Framework 4.8：Windows 10 1903+ 自带运行时，无需安装 .NET
- Material Design in XAML（MaterialDesignThemes）提供 UI 组件
