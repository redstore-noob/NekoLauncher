/*
MIT License

Copyright (c) 2024 Next UI
Copyright (c) 2026 烟花

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tsconfigPaths from "vite-tsconfig-paths";
import tailwindcss from "@tailwindcss/vite";

// wails dev 下前端的资源请求会走 Vite 开发服务器，而 Go 侧 assetserver 的
// /localfile、/plugins、/wwwallpaper 处理器只有在 Vite 返回 404/405 时才会被 Wails 代理回退调用。
// Vite 默认对未知路径回退 index.html（200），会让这些路由永远到不了 Go：
// /localfile 失效（背景图/音频/实例图标），/plugins 拿到 HTML 当 JSX 编译
// （插件报 "Error transforming index.jsx: Unexpected token"），/wwwallpaper 返回
// 启动器 HTML 而非壁纸（网页壁纸空白）。
// 这里让 Vite 对它们明确返回 404，交给 Go 处理。
function backendRoutes(): Plugin {
  return {
    name: "nya-backend-routes-404",
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const pathname = (req.url ?? "").split("?")[0];

        if (
          pathname === "/localfile" ||
          pathname.startsWith("/plugins/") ||
          pathname.startsWith("/wwwallpaper/") ||
          pathname.startsWith("/wescene/")
        ) {
          res.statusCode = 404;
          res.end("served by the Wails asset handler");

          return;
        }
        next();
      });
    },
  };
}

// 后端闸门的宿主侧接线（见 src/plugin/backend-gate.ts）：wails 生成的绑定代码
// 在每次调用时读 window['go']，而闸门已把 window.go 换成只抛错的 getter。
// 这里在构建/开发期把 wailsjs/go/** 里的 window['go'] 重写为 hostGo()——
// 生成文件本身不动（wails generate 会覆盖），dev 服务器与生产构建同一路径。
function rewriteWailsBindings(): Plugin {
  const backendGateId = "/src/plugin/backend-gate";

  return {
    name: "nya-wails-go-gate",
    transform(code, id) {
      // 只处理宿主的 wailsjs 生成绑定；/plugins/ 下的插件模块不经此 transform
      if (!/[/\\]wailsjs[/\\]go[/\\]/.test(id) || !/\.(js|ts)$/.test(id))
        return null;
      if (!code.includes("window['go']")) return null;

      return {
        code: `import { hostGo as __nekoHostGo } from "${backendGateId}";\n${code.replaceAll(
          "window['go']",
          "__nekoHostGo()",
        )}`,
        map: null,
      };
    },
  };
}

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    react(),
    tsconfigPaths(),
    tailwindcss(),
    backendRoutes(),
    rewriteWailsBindings(),
  ],
  server: {
    host: "127.0.0.1",
    port: 5173,
  },
});
