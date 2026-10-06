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
import React from "react";
import { Card } from "@heroui/react";

/**
 * 主页小组件统一外壳：主题色描边卡片 + 图标磁贴 + 大号数值行 + 内容区。
 * 卡片与图标统一走主题色（不再使用彩色渐变背景）。
 */
export interface HomeCardProps {
  /** 保留字段：图标磁贴已移除，传入会被忽略 */
  icon?: React.ReactNode;
  /** 保留字段：标题已移除，传入会被忽略 */
  label?: string;
  /** 主数值或状态文本 */
  value: React.ReactNode;
  /** 保留字段：旧插件传入的磁贴类名，现已忽略（统一主题色） */
  tileClass?: string;
  /** 保留字段：旧插件传入的数值渐变类名，现已忽略 */
  valueClass?: string;
  /** 游戏运行中：卡片呼吸光晕 */
  live?: boolean;
  /** 标题行右侧的操作按钮（如刷新） */
  action?: React.ReactNode;
  children?: React.ReactNode;
}

const HomeCard: React.FC<HomeCardProps> = ({
  value,
  live = false,
  action,
  children,
}) => {
  return (
    <Card
      as="section"
      className={`
        nya-enter pointer-events-auto flex w-full flex-none flex-col gap-3
        rounded-lg p-5 backdrop-blur-md shadow-none bg-transparent
        ${live ? "nya-neon-card nya-neon-live" : "nya-neon-card"}
      `}
      isBlurred={false}
    >
      {/* 主数值（不再显示图标磁贴与标题） */}
      <div className="flex items-center gap-3">
        <div className="flex min-w-0 flex-1 flex-col">
          <span
            className="
              truncate text-2xl font-bold tracking-tight
              text-gray-900 tabular-nums dark:text-gray-100
            "
          >
            {value}
          </span>
        </div>
        {action ? <div className="flex-none">{action}</div> : null}
      </div>

      {children}
    </Card>
  );
};

export default HomeCard;
