/*
 * 多人功能拆分后的两个独立顶级页面：服务器（开服）与联机。
 *
 * 原先是"多人"壳页面内的两个分段标签，现已各自注册为独立页面
 * （plugin/builtins.tsx：id "servers" / "online"）。两个页面都已改为
 * 指挥中心单页流——横幅自带标题与全局动作，因此直接渲染页面本体。
 * 两个子页功能独立、各走一套后端绑定（ServerHostAPI / OnlineAPI），
 * 都保持挂载不卸载的设计在各自组件内部处理。
 *
 * 外部跳转（主页小组件 / 帮助页）通过 navigateToPage("servers") 或
 * navigateToPage("online") 直达，见 lib/navigation.ts。
 */
import React from "react";

import OnlinePage from "./online";
import ServersPage from "./servers";

export const ServersPageWrapper: React.FC = () => <ServersPage />;

export const OnlinePageWrapper: React.FC = () => <OnlinePage />;
