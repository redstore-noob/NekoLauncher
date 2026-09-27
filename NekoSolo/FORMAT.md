# NekoSolo 安装包格式规范 v1

本文定义 NekoSolo 安装包（`.exe`）的二进制布局、载荷结构与首启标记。
导出方：NekoLauncher `internal/solo`；消费方：NekoSolo.Installer（C#）。
两端的字段与魔数必须保持一致，破坏性变更时递增版本号。

## 1. 文件布局

```
+---------------------------+
| 安装器模板（stub，PE）     |  ← NekoSolo.Installer.exe 的原始字节
+---------------------------+
| 载荷（payload，zip）      |  ← offset = stub 长度；长度 = 载荷 zip 字节数
+---------------------------+
| 尾标（trailer，32 字节）  |
+---------------------------+
```

## 2. 尾标（32 字节，全部小端）

| 偏移    | 长度 | 含义                                      |
| ------- | ---- | ----------------------------------------- |
| 0..8    | 8    | 魔数 `"NKSOLO1\x01"`（ASCII + 版本字节） |
| 8..16   | 8    | 载荷在文件中的偏移（uint64）              |
| 16..24  | 8    | 载荷长度（uint64）                        |
| 24..28  | 4    | 载荷 CRC32（IEEE 802.3，uint32）          |
| 28..32  | 4    | 保留（0）                                 |

安装器只读自身尾部 32 字节即可定位载荷；读取载荷时按 CRC32 校验完整性
（网盘传输损坏是整合包分发的头号事故）。

## 3. 载荷 zip 条目

| 前缀           | 安装目标                          | 说明                     |
| -------------- | --------------------------------- | ------------------------ |
| （无前缀）     | —                                 | `manifest.json`（必选）、图标（`manifest.iconPath` 指名） |
| `files/`       | `<安装根>/`                       | 启动器本体、`portable.flag` |
| `minecraft/`   | `<安装根>/NekoLauncher-data/minecraft/` | `versions/<id>/`（版本 json/jar + 整合包内容） |
| `jre/`         | `<安装根>/NekoLauncher-data/runtime/jre/` | 可选；`bin/java.exe`、`lib/` 等 |

未知前缀的顶层条目必须跳过（向前兼容）。

### minecraft/ 的约定

- `minecraft/versions/<id>/<id>.json` 必须存在；`<id>.jar`、
  `inheritsFrom` 父版本、`jar` 字段引用的客户端 jar 按需附带——
  这与启动器实例扫描的校验规则完全一致，保证装完即可被识别
- 整合包内容（`mods/`、`config/`、`saves/`、`resourcepacks/`、
  `shaderpacks/`、`options.txt`）直接放在 `versions/<id>/` 内：
  配合首启显式写入的"版本隔离"，开箱即是隔离实例
- 缺失的 libraries / assets 由启动器首次启动时按常规流程补全下载

## 4. manifest.json

```json
{
  "format": 1,
  "packId": "sky-odyssey-2",
  "packName": "天空奥德赛 2",
  "packVersion": "1.2.0",
  "author": "…",
  "description": "…",
  "mcVersion": "1.20.1",
  "loaderName": "Forge",
  "loaderVersion": "47.2.0",
  "versionId": "1.20.1-forge-47.2.0",
  "simpleMode": true,
  "hasJava": true,
  "iconPath": "icon.png",
  "updateLink": "https://…"
}
```

`format` 必须等于 1（当前版本）；`packName`、`versionId` 必填。

## 5. 首启标记 neko-solo.json

安装完成后写在 `<安装根>/NekoLauncher-data/neko-solo.json`：

```json
{
  "format": 1,
  "applied": false,
  "packId": "…", "packName": "…", "packVersion": "…",
  "author": "…", "description": "…",
  "mcVersion": "1.20.1", "loaderName": "…", "loaderVersion": "…",
  "versionId": "1.20.1-forge-47.2.0",
  "simpleMode": true,
  "minecraftDirectory": "<绝对路径>",
  "javaExecutable": "<绝对路径或空>",
  "updateLink": "…"
}
```

启动器每次启动读取（`internal/solo.ApplyStartupDefaults`）：

- `applied == true` → 什么都不做（用户此后的一切修改不被覆盖）
- `applied == false` → 依次应用（配置文件"只在未配置时接管"，绝不覆盖用户设置）：
  1. `simpleMode == true` 且 `simpleMode` 键从未配置 → 写入 `simpleMode: true`
     （NekoLauncher-S 模式；用户主动关闭过的不会被翻回开启）
  2. 游戏目录未配置 → 接管 `minecraftDirectory`；
     已配置且不同 → 将 `minecraftDirectory` 追加进"游戏目录列表"（`AddFolder`），
     当前目录保持不变
  3. 首选 Java 未配置且 `javaExecutable` 存在 → 注册为首选 Java
  4. 写入 `selectedGameInstance = versionId`（首启/新装包后直接停在整合包上）
  5. 为该实例显式开启版本隔离
  6. 回写 `applied: true`

## 6. 安装器模式

安装器按目标目录的现状自动选择模式：

| 模式 | 判定 | 行为 |
| --- | --- | --- |
| 全新安装 | 无 `NekoLauncher.exe` | 全量：启动器 + portable.flag + JRE + 整合包 + 标记 |
| 添加整合包 | 有启动器、无本格式标记 | 只装整合包实例 + 标记；**不覆盖启动器本体**、不写 portable.flag（不改变其便携/配置形态）、已有 JRE 不覆盖 |
| 更新整合包 | 有启动器 + 有本格式标记 | 同上，且同一实例目录内的 `saves/` 与 `options.txt` 保留 |

补充规则：

- 标记落点：便携布局（有 portable.flag 或数据目录）→ 数据目录；
  非便携的独立启动器 → `%USERPROFILE%\NekoLauncher\`（其默认存储目录）
- `applied` 语义：新包/新版本写入 `applied=false`（首启自动选中新实例）；
  **同包同版本**的修复重装沿用原值（不打扰用户当前选择）
- 配置文件（launcher.yaml / accounts.yaml）在任何模式下都**从不触碰**——
  "不覆盖配置"由安装器（只写标记）与启动器（只在未配置时接管）共同保证
- 已知取舍：捆绑 JRE 放在共享的 `runtime/jre`，多包共存时沿用首个包的 Java
  （跨 Java 大版本的多包场景留待后续按包分目录）
