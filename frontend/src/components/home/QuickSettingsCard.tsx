/*
 * 快速设置：常用设置分区的直达磁贴，不用翻设置页的搜索。
 */
import React from "react";
import { Chip } from "@heroui/react";
import {
  Globe20Regular,
  Options20Regular,
  Pulse20Regular,
  Settings20Regular,
  TopSpeed20Regular,
  WindowDevTools20Regular,
  Wrench20Regular,
} from "@fluentui/react-icons";

import { navigateToPage } from "../../lib/navigation";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 常用设置入口（detail 对应设置页各分区的锚点） */
const SHORTCUTS: Array<{
  detail: string;
  label: string;
  icon: React.ReactNode;
}> = [
  {
    detail: "launch",
    label: t("启动"),
    icon: <Options20Regular className="h-3.5 w-3.5" />,
  },
  {
    detail: "memory",
    label: t("内存"),
    icon: <Pulse20Regular className="h-3.5 w-3.5" />,
  },
  {
    detail: "java",
    label: t("Java"),
    icon: <WindowDevTools20Regular className="h-3.5 w-3.5" />,
  },
  {
    detail: "download",
    label: t("下载"),
    icon: <TopSpeed20Regular className="h-3.5 w-3.5" />,
  },
  {
    detail: "network",
    label: t("网络"),
    icon: <Globe20Regular className="h-3.5 w-3.5" />,
  },
  {
    detail: "behavior",
    label: t("行为"),
    icon: <Wrench20Regular className="h-3.5 w-3.5" />,
  },
];

const QuickSettingsCard: React.FC = () => {
  return (
    <HomeCard
      action={
        <Chip
          as="button"
          className="cursor-pointer"
          color="primary"
          size="sm"
          variant="flat"
          onClick={() => navigateToPage("settings")}
        >
          {t("全部")}
        </Chip>
      }
      icon={<Settings20Regular />}
      label={t("快速设置")}
      value={t("直达常用项")}
    >
      <div className="flex flex-wrap gap-1.5">
        {SHORTCUTS.map((shortcut) => (
          <Chip
            key={shortcut.detail}
            as="button"
            className="cursor-pointer"
            color="default"
            size="sm"
            variant="flat"
            onClick={() => navigateToPage("settings", shortcut.detail)}
          >
            <span className="flex items-center gap-1">
              {shortcut.icon}

              {shortcut.label}
            </span>
          </Chip>
        ))}
      </div>
    </HomeCard>
  );
};

export default QuickSettingsCard;
