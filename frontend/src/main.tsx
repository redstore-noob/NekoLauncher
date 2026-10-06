/*
 * Copyright 2024 Next UI
 * Copyright 2026 烟花
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";

// 纯浏览器 dev（无 Wails runtime）时兜底 window.go/window.runtime，见该文件注释
import "./wails-mock.ts";

import { installBackendGate } from "./plugin/backend-gate";

// 后端闸门必须先于一切插件加载：收走 window.go、封堵 WailsInvoke 等原始出口，
// 让插件只能通过注入的 api 对象触达宿主能力（见 plugin/backend-gate.ts）
installBackendGate();

import App from "./App.tsx";
import { Provider } from "./provider.tsx";
import { ThemeProvider, initTheme } from "./theme.tsx";
import { ThemeColorProvider, initThemeColor } from "./theme-color.tsx";
import { I18nProvider, initLocale } from "./i18n.tsx";
import { LogViewerProvider } from "./components/LogViewer";
import { LaunchProvenanceProvider } from "./components/launch/LaunchProvenancePanel";
import { registerBuiltins } from "./plugin";
import "@/styles/globals.css";

// 注意：不使用 React.StrictMode —— dev 模式下 double-mount 会破坏 framer-motion
// 的 AnimatePresence 退出动画，导致 HeroUI Modal 关闭后隐形遮罩残留、整窗无法点击。
// Modal 已改用 disableAnimation + CSS keyframes 入场（见 modal-shell.tsx /
// globals.css），不再依赖 framer 事件派发；关闭仍一律走右上角 X。
// 主题同步读 localStorage，在首帧前应用，否则启动时会先闪一下浅色。
initTheme();
// 主题色同样需要在首帧前应用，避免闪一下默认蓝
initThemeColor();
// 语言同步 localStorage / 浏览器语言，首帧前设置 <html lang>
initLocale();
// 内置小组件与页面走与插件相同的注册通道，必须在渲染前完成注册
registerBuiltins();

ReactDOM.createRoot(document.getElementById("root")!).render(
  <BrowserRouter>
    <I18nProvider>
      <ThemeProvider>
        <ThemeColorProvider>
          <Provider>
            <LogViewerProvider>
              <LaunchProvenanceProvider>
                <App />
              </LaunchProvenanceProvider>
            </LogViewerProvider>
          </Provider>
        </ThemeColorProvider>
      </ThemeProvider>
    </I18nProvider>
  </BrowserRouter>,
);
