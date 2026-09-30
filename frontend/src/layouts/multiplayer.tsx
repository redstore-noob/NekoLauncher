/*
 * 多人页：服务器管理与联机的整合壳页面。
 *
 * 两个子页（ServersPage / OnlinePage）功能独立、各走一套后端绑定
 * （ServerHostAPI / OnlineAPI），这里只做外层分段切换，不合并内部逻辑。
 * 两个子页都保持挂载、隐藏非活动侧：servers 页有控制台轮询与大量本地
 * 状态，条件卸载会在切换时丢轮询和界面状态。
 *
 * 外部跳转（主页小组件 / 帮助页）通过 navigateToPage("multiplayer", detail)
 * 的 detail（"servers" / "online"）直达对应分段，见 lib/navigation.ts。
 */
import React, { useEffect, useState } from "react";

import { SegmentedTabs } from "../components/segmented-tabs";
import { consumePendingDetail } from "../lib/navigation";
import { t } from "../i18n";

import OnlinePage from "./online";
import ServersPage from "./servers";

type MultiplayerTab = "servers" | "online";

const TAB_ITEMS = [
  { key: "servers", label: t("服务器") },
  { key: "online", label: t("联机") },
] as const;

const MultiplayerPage: React.FC = () => {
  const [tab, setTab] = useState<MultiplayerTab>("servers");

  // 外部跳转的二级定位参数：直达指定分段（settings 页同款机制）
  useEffect(() => {
    const detail = consumePendingDetail("multiplayer");

    if (detail === "servers" || detail === "online") {
      setTab(detail);
    }
  }, []);

  return (
    <div className="relative h-full w-full flex flex-col overflow-hidden">
      <div className="px-6 pt-5 pb-3 flex flex-shrink-0 items-center gap-4">
        <h1 className="text-xl font-semibold text-gray-800 dark:text-gray-200">
          {t("多人")}
        </h1>
        <SegmentedTabs
          className="flex items-center gap-1 rounded-full border nya-border p-1"
          items={TAB_ITEMS}
          layoutId="nya-multiplayer-tab"
          value={tab}
          onChange={(value) => setTab(value as MultiplayerTab)}
        />
      </div>
      {/* 内容区不做 flex 挤压：两个子页自带各自的滚动与布局，直接占满剩余空间 */}
      <div className="min-h-0 flex-1">
        <div className={tab === "servers" ? "h-full" : "hidden"}>
          <ServersPage />
        </div>
        <div className={tab === "online" ? "h-full" : "hidden"}>
          <OnlinePage />
        </div>
      </div>
    </div>
  );
};

export default MultiplayerPage;
