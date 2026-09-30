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
 * 共享空态：渐变圆底图标 + 主文案（全站约 35 种手写空态的统一替代）。
 * 图标缺省用立方体；调用方传 20px 系 Fluent 图标并自行放大。
 */
import React from "react";
import { Box20Regular } from "@fluentui/react-icons";

interface EmptyStateProps {
  text: string;
  icon?: React.ReactNode;
  className?: string;
}

const EmptyState: React.FC<EmptyStateProps> = ({
  text,
  icon,
  className = "",
}) => (
  <div
    className={`my-12 flex flex-col items-center gap-3 text-center text-gray-400 ${className}`}
  >
    <div className="flex size-16 items-center justify-center rounded-3xl bg-gradient-to-br from-default-200 to-default-100 shadow-inner dark:from-gray-800 dark:to-gray-800/50">
      {icon ?? <Box20Regular className="h-8 w-8" />}
    </div>
    <span className="text-[15px] font-semibold text-gray-500 dark:text-gray-400">
      {text}
    </span>
  </div>
);

export default EmptyState;
