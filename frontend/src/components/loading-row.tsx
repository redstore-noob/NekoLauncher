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
/*
 * 共享加载行：Spinner + 文案，居中（"Spinner + 文字"此前被内联手写 12+ 次）。
 * 默认上下留白适配页面内容区；放弹层面板里时用 className 覆盖。
 */
import React from "react";
import { Spinner } from "@heroui/react";

interface LoadingRowProps {
  text: string;
  className?: string;
}

const LoadingRow: React.FC<LoadingRowProps> = ({
  text,
  className = "my-10",
}) => (
  <div
    className={`flex items-center justify-center gap-2 text-xs text-gray-400 ${className}`}
  >
    <Spinner size="sm" /> {text}
  </div>
);

export default LoadingRow;
