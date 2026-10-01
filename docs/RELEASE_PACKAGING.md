# 发行包与 CI 构建矩阵

本文件说明 `.github/workflows/ci.yml` 里 `build` job 为什么长这样，以及每个产物是给谁的。
改 CI 之前先看这里，尤其是 `webkit2gtk` 与 glibc 那两条约束 —— 它们不是可以"顺手统一一下"的冗余。

## 构建矩阵

| job | runner | 编出来的二进制链什么 | 产物 |
| --- | --- | --- | --- |
| windows/amd64 | `windows-latest` | WebView2 | `NekoLauncher.exe`、`NekoLauncher-windows-amd64-portable.zip`、`NekoSolo.Installer.exe` |
| linux/amd64 (apt · glibc 2.35) | `ubuntu-22.04` ⚠️ | webkit2gtk **4.0** | `...-linux-amd64-debian-glibc-2.35.deb` |
| linux/amd64 (apt · glibc 2.39) | `ubuntu-24.04` | webkit2gtk **4.1** | `...-linux-amd64-debian-glibc-2.39.deb`、`NekoLauncher-linux-amd64.AppImage`、`...tar.gz` |
| linux/amd64 (rpm) | `ubuntu-24.04` + `fedora:42` 容器 | webkit2gtk **4.1** | `...-linux-amd64-fedora.rpm` |
| linux/amd64 (pacman) | `ubuntu-24.04` + `archlinux:latest` 容器 | webkit2gtk **4.1** | `...-linux-amd64-arch.pkg.tar.zst` |
| darwin/amd64 | `macos-15-intel` | Intel | `NekoLauncher-darwin-amd64.zip` |
| darwin/arm64 | `macos-15` | Apple Silicon | `NekoLauncher-darwin-arm64.zip` |

## Runner 生命周期（会过期，别写死就忘）

GitHub 每个 OS 家族同时维护最多两个 GA 镜像，最老的那个会先进入弃用期（期间有
brownout 时段直接不可用）、然后被摘掉。查当前状态用
[endoflife.date/github-actions-runner-images](https://endoflife.date/github-actions-runner-images)。

本项目用到的标签，写这份文档时（2026-10）的情况：

| 标签 | 状态 |
| --- | --- |
| `windows-latest` | 正常 |
| `ubuntu-24.04` | 正常 |
| `ubuntu-22.04` | ⚠️ **active support 已于 2026-09-17 结束**，2027-04-17 移除 |
| `macos-15` / `macos-15-intel` | 正常 |
| `macos-14` | ⚠️ 2026-07-06 起弃用，**2026-11-02 移除**（所以 arm64 用 `macos-15`） |
| `macos-13` | ❌ 2025-12-04 已移除 |

**`ubuntu-22.04` 到期后怎么办。** 它是唯一能产 webkit2gtk **4.0** 那份 deb 的地方，
所以不能简单删掉。两条路：

- (a) 放弃老 apt 系（Debian 12 / Ubuntu 22.04 用户）；
- (b) 改用 `container: debian:12` 来产这份包 —— **推荐**，Debian 12 的生命周期比
  Ubuntu 22.04 长，且同样提供 `libwebkit2gtk-4.0-dev`。

## 为什么一个发行版一个 job

### 1. webkit2gtk 的 API 版本绑死在编译期

wails v2.15 的 Linux 端是 cgo，`pkg-config` 认的 soname 要么是 `webkit2gtk-4.0`、要么是
`webkit2gtk-4.1`，**两套 API 包不能共存**。所以"哪个环境编出来的"直接决定了这个二进制
能装到哪些发行版上：

- `webkit2gtk-4.0`：Debian 12、Ubuntu 22.04 及更老的 apt 系。
  **4.0 的 API 包在 Ubuntu 24.04 / Debian 13 上已被移除**，所以 22.04 上编的包喂给新系统会缺依赖。
- `webkit2gtk-4.1`：Debian 13、Ubuntu 24.04、Fedora、Arch。

这就是 `.deb` 分 `glibc-2.35` / `glibc-2.39` 两份的原因：**不是**为了兼容 glibc，
而是两份分别链了 4.0 与 4.1，装错那份会报找不到 `libwebkit2gtk-*.so`。

### 2. glibc 基线

二进制不向后兼容 glibc：在 24.04（glibc 2.39）上编的东西在 Debian 12（2.36）上跑不起来。
反过来 22.04 上编的能跑在新系统上（只受 webkit 那条限制）。所以：

- 老 apt 系 → 22.04 编的包
- 新 apt 系 → 24.04 编的包
- Fedora / Arch → 各自编的包（它们的 glibc 比 24.04 还新，只喂给自己）

### 3. Fedora / Arch 走容器，不是因为偏好

**GitHub 没有 Fedora 或 Arch 的 hosted runner**（`runs-on: fedora-42` 是无效标签，
只有 Ubuntu/Windows/macOS 三类），所以这两个 job 是 `runs-on: ubuntu-24.04` 加
`container:`，在官方镜像里构建：

- `fedora:42`：自带 `webkit2gtk4.1`，`dnf` 装 `go nodejs gcc-c++` 等。
- `archlinux:latest`：自带 `webkit2gtk-4.1` 与滚动版工具链，`pacman` 装依赖。

容器里以 root 运行，所以 `pacman`/`dnf`/`go install` 都**不加 `sudo`**，也不走
`actions/setup-go` / `actions/setup-node`（那是给宿主机 runner 用的）。

> 踩过的坑：判断"要不要跑 setup-go"时别写 `matrix.container == ''`。
> 没写 `container` 的 job 里这个键是 `null`，而 GitHub 表达式的 `null == ''` 是
> **false**，会让 Windows/macOS/Ubuntu 的 setup 步骤被静默跳过、构建直接失败。
> 所以矩阵里用一个显式的 `use-setup-actions: 'yes'` 来标记。

> 另一个坑：**别用 `matrix.container` 做 `case` 分支**。
> `container: fedora:42` 这一行在 YAML 层会被解析成**对象** `{image: fedora:42}`，
> `${{ matrix.container }}` 渲染出来不是 `fedora:42` 这个字符串，所以
> `case "${{ matrix.container }}" in fedora*)` 一个分支都匹配不上 ——
> 结果是**什么都不装**，随后 `go install`/`npm` 全都 `command not found`，
> job 在几秒内就挂掉。用另一个纯字符串字段（本项目是 `in-container`）来标记。

### 4. macOS 为什么不用 `macos-latest`

`macos-latest` / `macos-15` / `macos-26` 全是 **arm64**。要出 Intel 包只能用
`macos-15-intel`（`macos-13` 已于 2025-12-04 被 GitHub 退役，
参见 [官方 changelog](https://github.blog/changelog/2025-09-19-github-actions-macos-13-runner-image-is-closing-down/)）。

Apple 已停止支持 x86_64，GitHub 表示 **macOS 15 镜像退役后（2027 秋）不再提供 Intel runner**。
到那时这个 job 要么删掉、要么改成 `darwin/amd64` 交叉编译（wails 支持 `-platform darwin/amd64`，
只是不能再依赖 runner 自带的 Xcode 工具链做本地验证）。

之前的做法是 `darwin/universal`：把 amd64 与 arm64 两个二进制 lipo 进同一个 `.app`。
现在拆开是因为通用包体积翻倍，而任何一台机器只会用到其中一半。

## 打包工具

发行包统一用 [nfpm](https://nfpm.goreleaser.com/) 生成，配置在 `packaging/`：

```
packaging/
  nfpm.yaml                     # 公共元数据（名字/版本/图标/desktop/文件清单）
  neko-launcher.desktop         # AppImage 与发行包共用的 desktop 条目
  depends/
    deb-glibc-2.35.yaml         # glibc 2.35 · webkit2gtk 4.0
    deb-glibc-2.39.yaml         # glibc 2.39 · webkit2gtk 4.1
    rpm.yaml                    # Fedora
    archlinux.yaml              # Arch
```

两个容易踩的坑：

1. **`--config` 只吃一个文件。** 它是普通字符串 flag，传两次是覆盖不是合并。
   所以 CI 把 `depends/*.yaml` 追加到 `nfpm.yaml` 后面拼成 `nfpm-merged.yaml` 再喂给 nfpm。
2. **`depends` 不做环境变量展开。** nfpm 的 `depends` 是数组，写成 `${XXX}` 会直接报
   `cannot unmarshal !!str into []string`。这正是依赖要拆成独立文件的原因，
   而不是（像 `version` 那样）在 `nfpm.yaml` 里用环境变量注入。

`version` 倒是走环境变量（`NPFPM_VERSION`）：tag 构建取 `v0.3.2` → `0.3.2`，
普通分支 push 用 `0.0.0-snapshot`。同时设了 `version_schema: none`，
否则 nfpm 会按各打包器自己的规矩重拼预发布后缀，包名和 tag 对不上。

## 资产命名

Release 资产是拍平在一个目录里的，所以每个 job 的产物名必须唯一：

```
NekoLauncher.exe
NekoLauncher-windows-amd64-portable.zip
NekoLauncher-linux-amd64.AppImage
NekoLauncher-linux-amd64.tar.gz
NekoLauncher-0.3.2-linux-amd64-debian-glibc-2.35.deb
NekoLauncher-0.3.2-linux-amd64-debian-glibc-2.39.deb
NekoLauncher-0.3.2-linux-amd64-fedora.rpm
NekoLauncher-0.3.2-linux-amd64-arch.pkg.tar.zst
NekoLauncher-darwin-amd64.zip
NekoLauncher-darwin-arm64.zip
```

nfpm 默认只给"名字-版本-架构"（两个 deb job 都会是 `nekolauncher_<版本>_amd64.deb`），
所以 CI 打包后统一改名。`internal/update/launcher_update.go` 里 `pickAsset` 的注释
也列了这套命名，**改这里的名字要同步改那边**（它靠名字判断当前平台有没有可下载的资产）。

## 自动更新

只有 Windows 支持就地替换（用裸 `NekoLauncher.exe`）。其它平台一律只给版本页链接：
AppImage 要替换自身文件并加回执行位、macOS 要处理包签名与 quarantine 属性、
发行包应该走包管理器升级 —— 自动替换的风险高于收益。
