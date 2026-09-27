/*
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
/*
 * 全局浮层宿主：把 NekoAlert + NekoPrompt 挂载在应用根部（App.tsx 调用一次）。
 * 挂载后无需 import 组件，任何地方直接调用 components/overlay/dialog 的门面 API。
 */
import React from "react";

import NekoAlert from "./NekoAlert";
import NekoPrompt from "./NekoPrompt";

const OverlayHost: React.FC = () => (
  <>
    <NekoAlert />
    <NekoPrompt />
  </>
);

export default OverlayHost;
