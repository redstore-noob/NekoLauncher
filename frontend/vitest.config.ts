/*
 * vitest 配置：前端测试从"纯函数优先"起步（P3-6）。
 *
 * 为什么单独一个配置文件而不是塞进 vite.config.ts：
 *   - vite.config.ts 里带着 Tailwind / React / wails 开发服务器的中间件，测试用不上；
 *   - 单独配置可以只挂一个轻量 alias（wailsjs 运行时的浏览器依赖），跑得更快也更稳。
 *
 * 环境用 jsdom：`lib/navigation.ts` 走 window 事件、`i18n.tsx` 读 localStorage，
 * 没有 DOM 会直接报错。组件测试暂不引入（不需要 @testing-library）。
 */
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
    setupFiles: ["src/test-setup.ts"],
    // 测试文件与被测模块同目录；打包（vite build）不会包含它们
    reporters: "default",
  },
});
