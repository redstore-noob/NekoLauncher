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

export type LicenseGroup = "前端" | "后端" | "工具链";

export interface LicenseEntry {
  name: string;
  spdx: string;
  url: string;
  group: LicenseGroup;
}

/** 主要开源依赖（顺序即展示顺序） */
export const LICENSES: LicenseEntry[] = [
  // ---- 前端 ----
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
];
