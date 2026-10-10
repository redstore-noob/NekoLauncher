/*
 * 关于页「开源致谢」的静态数据：本启动器的许可证与主要开源依赖清单。
 *
 * 前端依赖的 SPDX 取自 node_modules 里各包 package.json 的 license 字段；
 * Go 依赖取自各项目仓库声明的许可证。这里只列主要依赖（直接依赖 + 少数
 * 关键间接依赖），完整清单见仓库根目录的 go.mod 与 frontend/package-lock.json。
 */

/** 本启动器仓库（贡献者名单也从这里拉取） */
export const GITHUB_REPO = "redstore-noob/NekoLauncher";
export const GITHUB_REPO_URL = `https://github.com/${GITHUB_REPO}`;

/** 本启动器自身的许可证（仓库根 LICENSE：Apache-2.0） */
export const PROJECT_SPDX = "Apache-2.0";

export type LicenseGroup = "前端" | "后端" | "工具链" | "外部服务与工具";

export interface LicenseEntry {
  name: string;
  spdx: string;
  url: string;
  group: LicenseGroup;
  /** 可选的补充说明（如"这是服务不是代码"），展示在名称下方 */
  note?: string;
}

/** 主要开源依赖（顺序即展示顺序） */
export const LICENSES: LicenseEntry[] = [
  // ---- 前端 ----
  {
    // 场景壁纸的渲染核心：scene.pkg 解析、贴图解码、图层/效果/粒子/模型/
    // 脚本沙箱全在它里面，前端只负责给资源基址与用户属性
    name: "WebWallGL",
    spdx: "MIT",
    url: "https://github.com/oneincase/webwallgl",
    group: "前端",
  },
  {
    name: "React",
    spdx: "MIT",
    url: "https://github.com/facebook/react",
    group: "前端",
  },
  {
    name: "HeroUI",
    spdx: "MIT",
    url: "https://github.com/heroui-inc/heroui",
    group: "前端",
  },
  {
    name: "React Router",
    spdx: "MIT",
    url: "https://github.com/remix-run/react-router",
    group: "前端",
  },
  {
    name: "Fluent UI System Icons",
    spdx: "MIT",
    url: "https://github.com/microsoft/fluentui-system-icons",
    group: "前端",
  },
  {
    name: "Tailwind CSS",
    spdx: "MIT",
    url: "https://github.com/tailwindlabs/tailwindcss",
    group: "前端",
  },
  {
    name: "Framer Motion",
    spdx: "MIT",
    url: "https://github.com/motiondivision/motion",
    group: "前端",
  },
  {
    name: "three.js",
    spdx: "MIT",
    url: "https://github.com/mrdoob/three.js",
    group: "前端",
  },
  {
    name: "clsx",
    spdx: "MIT",
    url: "https://github.com/lukeed/clsx",
    group: "前端",
  },
  {
    name: "skinview3d",
    spdx: "MIT",
    url: "https://github.com/bs-community/skinview3d",
    group: "前端",
  },
  {
    // 插件系统用它在浏览器里转译插件源码（无需外部构建步骤）
    name: "Sucrase",
    spdx: "MIT",
    url: "https://github.com/alangpierce/sucrase",
    group: "前端",
  },
  {
    name: "react-markdown",
    spdx: "MIT",
    url: "https://github.com/remarkjs/react-markdown",
    group: "前端",
  },
  {
    name: "remark-gfm",
    spdx: "MIT",
    url: "https://github.com/remarkjs/remark-gfm",
    group: "前端",
  },
  {
    name: "React Aria (@react-types)",
    spdx: "Apache-2.0",
    url: "https://github.com/adobe/react-spectrum",
    group: "前端",
  },

  // ---- 后端 ----
  {
    name: "Wails",
    spdx: "MIT",
    url: "https://github.com/wailsapp/wails",
    group: "后端",
  },
  {
    name: "gopsutil",
    spdx: "MIT",
    url: "https://github.com/shirou/gopsutil",
    group: "后端",
  },
  {
    name: "Echo",
    spdx: "MIT",
    url: "https://github.com/labstack/echo",
    group: "后端",
  },
  {
    name: "gorilla/websocket",
    spdx: "BSD-2-Clause",
    url: "https://github.com/gorilla/websocket",
    group: "后端",
  },
  {
    name: "google/uuid",
    spdx: "BSD-3-Clause",
    url: "https://github.com/google/uuid",
    group: "后端",
  },
  {
    name: "uniseg",
    spdx: "MIT",
    url: "https://github.com/rivo/uniseg",
    group: "后端",
  },
  {
    name: "samber/lo",
    spdx: "MIT",
    url: "https://github.com/samber/lo",
    group: "后端",
  },
  {
    name: "x/sys",
    spdx: "BSD-3-Clause",
    url: "https://pkg.go.dev/golang.org/x/sys",
    group: "后端",
  },

  // ---- 工具链 ----
  { name: "Go", spdx: "BSD-3-Clause", url: "https://go.dev", group: "工具链" },
  {
    name: "TypeScript",
    spdx: "Apache-2.0",
    url: "https://github.com/microsoft/TypeScript",
    group: "工具链",
  },
  {
    name: "Vite",
    spdx: "MIT",
    url: "https://github.com/vitejs/vite",
    group: "工具链",
  },
  {
    name: "ESLint",
    spdx: "MIT",
    url: "https://github.com/eslint/eslint",
    group: "工具链",
  },

  // ---- 外部服务与工具（不随启动器分发，只是请求接口或驱动它运行） ----
  {
    // Minecraft 各版本清单 / 库 / 资源文件的国内镜像下载源。启动器默认走它，
    // 用户可在「下载设置」切回官方源；服务端实现为开源的 OpenBMCLAPI
    name: "BMCLAPI",
    note: "Minecraft 国内镜像下载源（bangbang93 维护的 OpenBMCLAPI）；启动器默认使用，可在下载设置切回官方源",
    spdx: "MIT",
    url: "https://github.com/bangbang93/openbmclapi",
    group: "外部服务与工具",
  },
  {
    // 联机页「内网穿透」方案的模组侧：启动器只复用它的公开中继协议，
    // 模组本身由用户装进实例，启动器不分发
    name: "RedstoneOnline（红石联机）",
    note: "内网穿透联机模组；装进实例后启动器接管其公开中继协议，把本机服务器发布到公网",
    spdx: "GPL-3.0-or-later",
    url: "https://modrinth.com/mod/redstoneonline",
    group: "外部服务与工具",
  },
  {
    // 联机页「虚拟局域网」方案：启动器自动下载并驱动它的本地接口，
    // 程序本体与它内嵌的 EasyTier 都由用户机器上的 Terracotta 自行管理
    name: "Terracotta（陶瓦联机）",
    note: "基于 EasyTier 的虚拟局域网联机工具；启动器只自动下载并驱动它的本地 HTTP 接口，不修改其二进制",
    spdx: "AGPL-3.0-only",
    url: "https://github.com/burningtnt/Terracotta",
    group: "外部服务与工具",
  },
];
