/*
 * Copyright 2024 Next UI
 * Copyright 2026 烟花
 *
 * Licensed under the Apache License, Version 2.0 (the \"License\");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an \"AS IS\" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
import { Route, Routes } from "react-router-dom";

import OverlayHost from "@/components/overlay/OverlayHost";
import { OobeProvider } from "@/components/oobe/OobeProvider";
import OobeOverlay from "@/components/oobe/OobeOverlay";
import IndexPage from "@/layouts/index";

function App() {
  return (
    <OobeProvider>
      <Routes>
        <Route element={<IndexPage />} path="/" />
      </Routes>
      {/*全局浮层：NekoAlert（底部警示）/ NyaPrompt（提示对话框），随处可调 */}
      <OverlayHost />
      {/*首次启动引导覆盖层（OOBE）：萌新 / 跳过 / 创作者三路线，见 OobeProvider */}
      <OobeOverlay />
    </OobeProvider>
  );
}

export default App;
