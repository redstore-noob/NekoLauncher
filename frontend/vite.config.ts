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
// /localfile、/plugins 处理器只有在 Vite 返回 404/405 时才会被 Wails 代理回退调用。
// Vite 默认对未知路径回退 index.html（200），会让这些路由永远到不了 Go：
// /localfile 失效（背景图/音频/实例图标），/plugins 拿到 HTML 当 JSX 编译
// （插件报 "Error transforming index.jsx: Unexpected token"）。
// 这里让 Vite 对它们明确返回 404，交给 Go 处理。
function backendRoutes(): Plugin {
  return {
    name: "nya-backend-routes-404",
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const pathname = (req.url ?? "").split("?")[0];

        if (pathname === "/localfile" || pathname.startsWith("/plugins/")) {
          res.statusCode = 404;
          res.end("served by the Wails asset handler");

          return;
        }
        next();
      });
    },
  };
}

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react(), tsconfigPaths(), tailwindcss(), backendRoutes()],
  server: {
    host: "127.0.0.1",
    port: 5173,
  },
});
