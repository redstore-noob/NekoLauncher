/*
 * NekoAgent 的形象图标：直接用启动器本体的 appicon（与标题栏 / 任务栏同一个），
 * 让 AI 助手看起来就是"启动器自己"而不是第三方机器人。
 */
import React from "react";

import { t } from "../i18n";

const NekoAgentIcon: React.FC<{ className?: string }> = ({ className }) => (
  <img
    alt={t("NekoAgent喵")}
    className={`flex-shrink-0 select-none rounded-md object-contain ${className ?? "h-5 w-5"}`}
    draggable={false}
    src="/appicon.png"
  />
);

export default NekoAgentIcon;
