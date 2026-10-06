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
import React from "react";
import { Button, Tooltip } from "@heroui/react";
import { ArrowLeft20Regular } from "@fluentui/react-icons";

import { tooltipMotionProps } from "@/lib/motion";
import { t } from "@/i18n";

export interface CreatorToolShellProps {
  title: string;
  subtitle?: string;
  icon?: React.ReactNode;
  titleExtra?: React.ReactNode;
  onBack: () => void;
  children: React.ReactNode;
}

/**
 * 创作中心专属工具全屏容器外壳：
 * - 左上角提供标准圆角返回按钮回到创作中心主页
 * - 紧凑高雅的顶部导航条（图标 + 标题 + 副标题 + 操作区）
 * - 占满全高与宽度的无遮罩工作区
 */
export const CreatorToolShell: React.FC<CreatorToolShellProps> = ({
  title,
  subtitle,
  icon,
  titleExtra,
  onBack,
  children,
}) => {
  return (
    <div className="flex h-full w-full flex-col overflow-hidden bg-transparent">
      {/* 顶部导航条 */}
      <header className="flex flex-shrink-0 items-center justify-between border-b border-divider/60 px-5 py-3">
        <div className="flex items-center gap-3 min-w-0">
          <Tooltip
            content={t("返回创作中心")}
            delay={300}
            motionProps={tooltipMotionProps}
            placement="bottom"
          >
            <Button
              isIconOnly
              aria-label={t("返回创作中心")}
              className="flex-shrink-0 text-default-600 hover:text-default-900"
              radius="full"
              size="sm"
              variant="flat"
              onPress={onBack}
            >
              <ArrowLeft20Regular />
            </Button>
          </Tooltip>

          {icon ? (
            <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg bg-primary/15 text-primary">
              {icon}
            </div>
          ) : null}

          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="text-xs text-default-400 select-none">
                {t("创作中心")} /
              </span>
              <h2 className="text-sm font-semibold text-foreground truncate">
                {title}
              </h2>
            </div>
            {subtitle ? (
              <p className="text-[11px] text-default-400 truncate mt-0.5">
                {subtitle}
              </p>
            ) : null}
          </div>
        </div>

        {titleExtra ? (
          <div className="flex items-center gap-2 flex-shrink-0">
            {titleExtra}
          </div>
        ) : null}
      </header>

      {/* 工作区主体 */}
      <main className="relative flex-1 min-h-0 w-full overflow-hidden p-4">
        {children}
      </main>
    </div>
  );
};

export default CreatorToolShell;
