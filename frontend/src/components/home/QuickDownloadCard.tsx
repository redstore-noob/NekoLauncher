/*
 * 快速前往下载：一键去下载页装游戏 / 资源，附下载设置与 Java 下载的直达入口。
 */
import React from "react";
import { Button, Chip } from "@heroui/react";
import {
  ArrowDownload20Regular,
  Globe20Regular,
  WindowDevTools20Regular,
} from "@fluentui/react-icons";

import { navigateToPage } from "../../lib/navigation";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

const QuickDownloadCard: React.FC = () => {
  return (
    <HomeCard
      icon={<ArrowDownload20Regular />}
      label={t("快速下载")}
      value={t("去装游戏 / 资源")}
    >
      <div className="flex flex-col gap-2">
        <Button
          fullWidth
          color="primary"
          radius="lg"
          size="sm"
          startContent={<ArrowDownload20Regular />}
          onPress={() => navigateToPage("download")}
        >
          {t("前往下载页")}
        </Button>
        <div className="flex flex-wrap gap-1.5">
          <Chip
            as="button"
            className="cursor-pointer"
            color="default"
            size="sm"
            variant="flat"
            onClick={() => navigateToPage("settings", "download")}
          >
            <span className="flex items-center gap-1">
              <Globe20Regular className="h-3.5 w-3.5" />

              {t("下载设置")}
            </span>
          </Chip>
          <Chip
            as="button"
            className="cursor-pointer"
            color="default"
            size="sm"
            variant="flat"
            onClick={() => navigateToPage("settings", "java")}
          >
            <span className="flex items-center gap-1">
              <WindowDevTools20Regular className="h-3.5 w-3.5" />

              {t("Java 下载")}
            </span>
          </Chip>
        </div>
      </div>
    </HomeCard>
  );
};

export default QuickDownloadCard;
